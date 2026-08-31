package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ale/fera/internal/service"
	"github.com/ale/fera/internal/sim"
)

// A interface fica AQUI, no consumidor, e tem dois métodos. O handler não
// conhece pgx, não conhece cache e não conhece singleflight.
type petService interface {
	Get(ctx context.Context, petID string) (sim.View, error)
	Ingest(ctx context.Context, petID string, evs []sim.Event) (service.IngestResult, error)
	Events(ctx context.Context, petID string, cursor int64, limit int) ([]sim.Event, int64, error)
}

type PetHandler struct {
	svc   petService
	clock func() time.Time
}

func NewPetHandler(svc petService, clock func() time.Time) *PetHandler {
	return &PetHandler{svc: svc, clock: clock}
}

// IngestEvents faz três coisas: decodifica, chama o service, codifica.
//
// Duplicata devolve 200 com o contador preenchido, nunca 409. Reenviar o lote
// é o caminho feliz de um retry, e responder erro faria o device tratar
// sucesso como falha e reagendar pra sempre.
func (h *PetHandler) IngestEvents(w http.ResponseWriter, r *http.Request) {
	petID, ok := petIDDaURL(w, r)
	if !ok {
		return
	}

	// io.LimitReader sempre. Sem ele um body de 4 GB derruba o processo.
	var req ingestRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, codeInvalidBody, "corpo inválido")
		return
	}
	if err := req.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, codeInvalidEvent, err.Error())
		return
	}

	evs, rejeitados := req.toDomain(h.clock())
	if len(rejeitados) > 0 {
		// O contrato só devolve a contagem, então o detalhe do que caiu fora
		// tem que existir em algum lugar. Fica aqui.
		slog.WarnContext(r.Context(), "eventos rejeitados na borda",
			"pet_id", petID, "ids", rejeitados)
	}

	res, err := h.svc.Ingest(r.Context(), petID, evs)
	if err != nil {
		if status, code := statusFor(err); status != 0 {
			writeErr(w, status, code, "não foi possível gravar o lote")
			return
		}
		writeInternal(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, ingestResponse{
		Accepted:   res.Accepted,
		Duplicates: res.Duplicates,
		Rejected:   len(rejeitados),
		Cursor:     res.Cursor,
	})
}

func (h *PetHandler) GetPet(w http.ResponseWriter, r *http.Request) {
	petID, ok := petIDDaURL(w, r)
	if !ok {
		return
	}

	v, err := h.svc.Get(r.Context(), petID)
	if err != nil {
		if status, code := statusFor(err); status != 0 {
			writeErr(w, status, code, "pet não encontrado")
			return
		}
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, fromView(v))
}

// petIDDaURL valida o formato antes de descer. Sem isso um id torto chega no
// pgx e volta como erro de driver, que é exatamente o que não pode vazar
// pro cliente.
func petIDDaURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	petID := chi.URLParam(r, "petID")
	if _, err := uuid.Parse(petID); err != nil {
		writeErr(w, http.StatusBadRequest, codeInvalidPetID, "pet_id não é um UUID")
		return "", false
	}
	return petID, true
}

// ListEvents é o pull por cursor. Paginação por `since`, nunca por offset:
// offset pula e repete quando entra linha nova no meio da varredura, e o log
// está sempre crescendo.
func (h *PetHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	petID, ok := petIDDaURL(w, r)
	if !ok {
		return
	}

	since, err := intDaQuery(r, "since", 0)
	if err != nil || since < 0 {
		writeErr(w, http.StatusBadRequest, codeInvalidQuery, "since inválido")
		return
	}
	limit, err := intDaQuery(r, "limit", maxEventos)
	if err != nil || limit < 0 {
		writeErr(w, http.StatusBadRequest, codeInvalidQuery, "limit inválido")
		return
	}

	evs, cursor, err := h.svc.Events(r.Context(), petID, since, int(limit))
	if err != nil {
		if status, code := statusFor(err); status != 0 {
			writeErr(w, status, code, "não foi possível ler os eventos")
			return
		}
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, eventsResponse{Events: fromDomain(evs), Cursor: cursor})
}

func intDaQuery(r *http.Request, nome string, padrao int64) (int64, error) {
	v := r.URL.Query().Get(nome)
	if v == "" {
		return padrao, nil
	}
	return strconv.ParseInt(v, 10, 64)
}
