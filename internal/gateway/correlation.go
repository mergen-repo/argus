package gateway

import (
	"context"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

func CorrelationID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := uuid.New().String()
			w.Header().Set("X-Request-ID", id)
			ctx := context.WithValue(r.Context(), apierr.CorrelationIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(apierr.CorrelationIDKey).(string); ok {
		return id
	}
	return ""
}
