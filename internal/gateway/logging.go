package gateway

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

type responseCapture struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.status = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	n, err := rc.ResponseWriter.Write(b)
	rc.bytes += n
	return n, err
}

func ZerologRequestLogger(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rc := &responseCapture{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rc, r)

			duration := time.Since(start)
			correlationID := GetCorrelationID(r.Context())

			var event *zerolog.Event
			switch {
			case rc.status >= 500:
				event = logger.Error()
			case rc.status >= 400:
				event = logger.Warn()
			default:
				event = logger.Info()
			}

			event.
				Str("correlation_id", correlationID).
				Str("tenant_id", TenantLabel(r.Context())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", rc.status).
				Int64("duration_ms", duration.Milliseconds()).
				Int("bytes", rc.bytes).
				Str("remote_addr", r.RemoteAddr).
				Msg("http request")
		})
	}
}

func RecoveryWithZerolog(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					correlationID := GetCorrelationID(r.Context())
					stack := string(debug.Stack())

					logger.Error().
						Str("correlation_id", correlationID).
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Str("remote_addr", r.RemoteAddr).
						Str("panic", fmt.Sprintf("%v", rec)).
						Str("stack", stack).
						Msg("panic recovered")

					apierr.WriteError(w, http.StatusInternalServerError,
						apierr.CodeInternalError, "Internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
