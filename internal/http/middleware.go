package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// logRequest registra uma linha por requisição em JSON estruturado.
// log/slog da stdlib: sem logrus, sem zap.
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inicio := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		slog.InfoContext(r.Context(), "requisição",
			"metodo", r.Method,
			"rota", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"ms", time.Since(inicio).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}
