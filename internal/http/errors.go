package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ale/fera/internal/service"
)

// Formato de erro único. `code` é estável e o cliente pode programar em cima;
// `message` é pra humano e pode mudar. Nunca stack trace, nome de tabela ou
// erro de driver: isso vaza estrutura interna pra quem não deveria vê-la.
type errBody struct {
	Error errDetail `json:"error"`
}

type errDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	codeInvalidBody  = "invalid_body"
	codeInvalidEvent = "invalid_event"
	codeInvalidPetID = "invalid_pet_id"
	codeInvalidQuery = "invalid_query"
	codeUnauthorized = "unauthorized"
	codeNotFound     = "not_found"
	codeInternal     = "internal"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// header já foi, não dá pra trocar o status. Só registra.
		slog.Error("codificar resposta", "erro", err)
	}
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errBody{errDetail{Code: code, Message: msg}})
}

// writeInternal é o único caminho pra erro não classificado. O erro de verdade
// vai pro log com contexto; o cliente recebe uma frase genérica.
func writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "erro no handler",
		"erro", err, "rota", r.URL.Path, "metodo", r.Method)
	writeErr(w, http.StatusInternalServerError, codeInternal, "erro interno")
}

// statusFor mapeia as sentinelas do service. errors.Is, nunca comparação de
// string: o erro chega embrulhado em %w e a igualdade direta falharia.
func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound, codeNotFound
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict, "conflict"
	default:
		return 0, ""
	}
}
