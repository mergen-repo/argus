package middleware

import (
	"context"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
)

type contextKey string

const ImpersonatedKey contextKey = "impersonated"

func ImpersonationReadOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		impersonated, _ := r.Context().Value(ImpersonatedKey).(bool)
		if impersonated {
			method := r.Method
			if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
				apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden,
					"Impersonation is read-only — mutations are not allowed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func WithImpersonated(ctx context.Context, impersonated bool) context.Context {
	return context.WithValue(ctx, ImpersonatedKey, impersonated)
}
