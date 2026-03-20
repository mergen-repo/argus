package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/auth"
)

func JWTAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing or invalid authorization header")
				return
			}

			claims, err := auth.ValidateToken(tokenStr, secret)
			if err != nil {
				code := apierr.CodeInvalidCredentials
				msg := "Invalid authentication token"
				if err == auth.ErrTokenExpired {
					code = apierr.CodeTokenExpired
					msg = "Access token has expired. Use refresh token to obtain a new one."
				}
				apierr.WriteError(w, http.StatusUnauthorized, code, msg)
				return
			}

			if claims.Partial {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"2FA verification required before accessing this resource")
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, claims.TenantID)
			ctx = context.WithValue(ctx, apierr.UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, apierr.RoleKey, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func JWTAuthAllowPartial(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing or invalid authorization header")
				return
			}

			claims, err := auth.ValidateToken(tokenStr, secret)
			if err != nil {
				code := apierr.CodeInvalidCredentials
				msg := "Invalid authentication token"
				if err == auth.ErrTokenExpired {
					code = apierr.CodeTokenExpired
					msg = "Access token has expired. Use refresh token to obtain a new one."
				}
				apierr.WriteError(w, http.StatusUnauthorized, code, msg)
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, claims.TenantID)
			ctx = context.WithValue(ctx, apierr.UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, apierr.RoleKey, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
