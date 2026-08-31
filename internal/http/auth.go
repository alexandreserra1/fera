package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ale/fera/internal/service"
	"github.com/ale/fera/internal/sig"
)

type authenticator interface {
	Authenticate(ctx context.Context, token string) (deviceID, petID string, err error)
	// AuthenticateSigned é o caminho do device: ele ASSINA a requisição em vez
	// de mandar o segredo. Existe porque TLS no ESP32 não cabe na RAM (ver
	// docs/06 e internal/sig).
	AuthenticateSigned(ctx context.Context, req service.Assinada) (petID string, err error)
}

// Chave tipada e não exportada. NUNCA context.WithValue(ctx, "device", d) com
// string: qualquer pacote no processo pode escrever a mesma string e
// sobrescrever a identidade do requisitante sem que nada acuse.
type ctxKey int

const deviceKey ctxKey = iota

type device struct {
	ID    string
	PetID string
}

func deviceDoCtx(ctx context.Context) (device, bool) {
	d, ok := ctx.Value(deviceKey).(device)
	return d, ok
}

// requireDevice aceita DOIS caminhos, e os dois existem por motivo:
//
//   - Bearer, pro app: roda sobre HTTPS, onde mandar o segredo é seguro.
//   - Assinatura, pro device: TLS não cabe na RAM do ESP32 (+168 KB contra
//     +672 B de assinar), então o segredo não pode cruzar o fio.
//
// Sem credencial, credencial torta e credencial desconhecida dão todas a MESMA
// resposta: distinguir conta pra quem está sondando qual dos casos aconteceu.
func requireDevice(auth authenticator, agora func() time.Time) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				id, petID string
				err       error
			)
			if r.Header.Get(sig.HeaderDevice) != "" {
				id, petID, err = autenticaAssinada(auth, w, r, agora)
			} else {
				id, petID, err = autenticaBearer(auth, r)
			}
			if err != nil {
				if status, _ := statusFor(err); status != 0 {
					naoAutorizado(w, agora)
					return
				}
				writeInternal(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), deviceKey, device{ID: id, PetID: petID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func autenticaBearer(auth authenticator, r *http.Request) (string, string, error) {
	token, ok := bearer(r)
	if !ok {
		return "", "", service.ErrForbidden
	}
	return auth.Authenticate(r.Context(), token)
}

// autenticaAssinada precisa do corpo pra conferir o MAC, então ele é lido
// aqui e devolvido ao handler. io.LimitReader porque o corpo ainda não passou
// por validação nenhuma neste ponto.
func autenticaAssinada(auth authenticator, w http.ResponseWriter, r *http.Request, agora func() time.Time) (string, string, error) {
	corpo, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return "", "", service.ErrForbidden
	}
	r.Body = io.NopCloser(bytes.NewReader(corpo))

	seg, err := strconv.ParseInt(r.Header.Get(sig.HeaderTimestamp), 10, 64)
	if err != nil {
		return "", "", service.ErrForbidden
	}

	deviceID := r.Header.Get(sig.HeaderDevice)
	petID, err := auth.AuthenticateSigned(r.Context(), service.Assinada{
		DeviceID: deviceID,
		Metodo:   r.Method,
		// RequestURI e não Path: a query faz parte da requisição, e sem ela
		// no MAC dava pra trocar o ?since= de um pull sem invalidar nada.
		Caminho:    r.URL.RequestURI(),
		Quando:     time.Unix(seg, 0).UTC(),
		Corpo:      corpo,
		Assinatura: r.Header.Get(sig.HeaderAssinatura),
	})
	if err != nil {
		return "", "", err
	}
	return deviceID, petID, nil
}

// requireOwnPet é a autorização, e ela é separada da autenticação de propósito.
// Ter um token válido não é ter direito a QUALQUER pet: sem esta checagem,
// qualquer device registrado leria e escreveria no log de todo mundo.
func requireOwnPet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := deviceDoCtx(r.Context())
		if !ok {
			naoAutorizado(w, time.Now)
			return
		}
		if chi.URLParam(r, "petID") != d.PetID {
			// 404 e não 403: responder "proibido" confirma que aquele pet_id
			// existe, e pet_id é justamente o que não se deve conseguir sondar.
			writeErr(w, http.StatusNotFound, codeNotFound, "pet não encontrado")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefixo = "Bearer "
	if len(h) <= len(prefixo) || !strings.EqualFold(h[:len(prefixo)], prefixo) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefixo):]), true
}

func naoAutorizado(w http.ResponseWriter, agora func() time.Time) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	// Devolve o relógio do servidor. Device sem RTC mente depois de um reboot
	// a frio, e sem esta pista ele ficaria fora da janela de assinatura pra
	// sempre, sem meio de descobrir por quê.
	w.Header().Set(sig.HeaderHora, agora().UTC().Format(time.RFC3339))
	writeErr(w, http.StatusUnauthorized, codeUnauthorized, "credencial ausente ou inválida")
}
