package middleware

import (
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// Logger records HTTP access logs at debug level to keep normal app logs focused.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		defer func() {
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
				"request_id", chimw.GetReqID(r.Context()),
			}
			switch {
			case ww.Status() >= http.StatusInternalServerError:
				slog.Error("http request", attrs...)
			case ww.Status() >= http.StatusBadRequest:
				slog.Warn("http request", attrs...)
			default:
				slog.Debug("http request", attrs...)
			}
		}()

		next.ServeHTTP(ww, r)
	})
}
