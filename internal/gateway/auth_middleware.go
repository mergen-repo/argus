package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/auth"
)

// applyAuthContext sets TenantIDKey (effective), HomeTenantIDKey, UserIDKey,
// RoleKey, and — when a super_admin has an active tenant-context switch —
// ActiveTenantIDKey. The effective tenant equals claims.TenantID unless the
// caller is a super_admin with a non-nil ActiveTenantID override.
func applyAuthContext(ctx context.Context, claims *auth.Claims) context.Context {
	ctx = context.WithValue(ctx, apierr.HomeTenantIDKey, claims.TenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, claims.UserID)
	ctx = context.WithValue(ctx, apierr.RoleKey, claims.Role)

	effectiveTenant := claims.TenantID
	if claims.ActiveTenantID != nil && claims.Role == "super_admin" {
		effectiveTenant = *claims.ActiveTenantID
		ctx = context.WithValue(ctx, apierr.ActiveTenantIDKey, *claims.ActiveTenantID)
	}
	ctx = context.WithValue(ctx, apierr.TenantIDKey, effectiveTenant)
	return ctx
}

func JWTAuth(currentSecret, previousSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing or invalid authorization header")
				return
			}

			claims, err := auth.ValidateTokenMulti(tokenStr, currentSecret, previousSecret)
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

			ctx := applyAuthContext(r.Context(), claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func JWTAuthAllowPartial(currentSecret, previousSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing or invalid authorization header")
				return
			}

			claims, err := auth.ValidateTokenMulti(tokenStr, currentSecret, previousSecret)
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

			ctx := applyAuthContext(r.Context(), claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func JWTAuthAllowForceChange(currentSecret, previousSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractBearerToken(r)
			if tokenStr == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing or invalid authorization header")
				return
			}

			claims, err := auth.ValidateTokenMulti(tokenStr, currentSecret, previousSecret)
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
				reason := claims.Reason
				if reason != auth.ReasonPasswordChangeRequired && reason != auth.ReasonPasswordExpired {
					apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
						"Token does not have permission to access this resource")
					return
				}
			}

			ctx := applyAuthContext(r.Context(), claims)

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
