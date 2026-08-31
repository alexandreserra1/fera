package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Pinger é o que o /readyz precisa saber fazer, e nada mais. O pool inteiro
// não precisa entrar aqui só pra responder um health check.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Timeouts em camadas, cada um menor que o de fora. Se invertesse, o timeout
// externo estouraria enquanto o trabalho interno continuasse rodando e
// segurando conexão de banco.
const (
	timeoutLeitura = 5 * time.Second
	timeoutIngest  = 15 * time.Second
)

func NewRouter(pets *PetHandler, devs *DeviceHandler, auth authenticator, agora func() time.Time, db Pinger) http.Handler {
	if agora == nil {
		agora = time.Now
	}
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(logRequest)

	// Sem banco no liveness de propósito: se o Postgres cair, o processo
	// continua vivo e não deve ser reiniciado por causa disso.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db != nil {
			if err := db.Ping(ctx); err != nil {
				writeErr(w, http.StatusServiceUnavailable, "not_ready", "banco indisponível")
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Único endpoint aberto: é por onde a credencial nasce.
	r.With(middleware.Timeout(timeoutLeitura)).Post("/v1/devices/register", devs.Register)

	// Autenticação e autorização são middlewares separados e nesta ordem.
	// requireDevice diz QUEM é; requireOwnPet diz se esse quem pode mexer
	// NESTE pet. Ter token válido não é ter direito a qualquer pet_id.
	r.Route("/v1/pets/{petID}", func(r chi.Router) {
		r.Use(requireDevice(auth, agora))
		r.Use(requireOwnPet)

		r.With(middleware.Timeout(timeoutLeitura)).Get("/", pets.GetPet)
		r.With(middleware.Timeout(timeoutIngest)).Post("/events", pets.IngestEvents)
		r.With(middleware.Timeout(timeoutLeitura)).Get("/events", pets.ListEvents)
	})

	return r
}
