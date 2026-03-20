package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/api/apikey"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

func APIKeyAuth(apiKeyStore *store.APIKeyStore, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing X-API-Key header")
				return
			}

			prefix, _, ok := apikey.ParseAPIKey(key)
			if !ok {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Invalid API key format")
				return
			}

			k, err := apiKeyStore.GetByPrefix(r.Context(), prefix)
			if err != nil {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Invalid API key")
				return
			}

			if k.RevokedAt != nil {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"API key has been revoked")
				return
			}

			if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"API key has expired")
				return
			}

			keyHash := apikey.HashAPIKey(key)
			valid := keyHash == k.KeyHash

			if !valid && k.PreviousKeyHash != nil {
				if keyHash == *k.PreviousKeyHash && k.KeyRotatedAt != nil {
					gracePeriodEnd := k.KeyRotatedAt.Add(24 * time.Hour)
					if time.Now().Before(gracePeriodEnd) {
						valid = true
					}
				}
			}

			if !valid {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Invalid API key")
				return
			}

			ctx := r.Context()
			ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
			ctx = context.WithValue(ctx, apierr.TenantIDKey, k.TenantID)
			ctx = context.WithValue(ctx, apierr.APIKeyIDKey, k.ID.String())
			ctx = context.WithValue(ctx, apierr.ScopesKey, k.Scopes)
			ctx = context.WithValue(ctx, RateLimitPerMinuteKey, k.RateLimitPerMinute)
			ctx = context.WithValue(ctx, RateLimitPerHourKey, k.RateLimitPerHour)

			go func() {
				bgCtx := context.Background()
				if uErr := apiKeyStore.UpdateUsage(bgCtx, k.ID); uErr != nil {
					logger.Warn().Err(uErr).Str("api_key_id", k.ID.String()).Msg("update api key usage failed")
				}
			}()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CombinedAuth(jwtSecret string, apiKeyStore *store.APIKeyStore, logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearerToken := extractBearerToken(r)
			apiKey := r.Header.Get("X-API-Key")

			if bearerToken != "" {
				claims, err := auth.ValidateToken(bearerToken, jwtSecret)
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
				ctx = context.WithValue(ctx, apierr.AuthTypeKey, "jwt")
				ctx = context.WithValue(ctx, apierr.TenantIDKey, claims.TenantID)
				ctx = context.WithValue(ctx, apierr.UserIDKey, claims.UserID)
				ctx = context.WithValue(ctx, apierr.RoleKey, claims.Role)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if apiKey != "" {
				apiKeyMiddleware := APIKeyAuth(apiKeyStore, logger)
				apiKeyMiddleware(next).ServeHTTP(w, r)
				return
			}

			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
				"Missing authentication. Provide Authorization header or X-API-Key header.")
		})
	}
}

func hasScopeAccess(scopes []string, required string) bool {
	for _, s := range scopes {
		if s == "*" {
			return true
		}
		if s == required {
			return true
		}
		parts := strings.SplitN(required, ":", 2)
		if len(parts) == 2 {
			if s == parts[0]+":*" {
				return true
			}
		}
	}
	return false
}
