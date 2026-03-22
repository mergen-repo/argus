package gateway

import (
	"net/http"
	"strings"

	"github.com/rs/zerolog"
)

type CORSConfig struct {
	AllowAllOrigins  bool
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Authorization", "Content-Type",
			"X-Correlation-ID", "X-Request-ID",
		},
		ExposedHeaders: []string{
			"X-Correlation-ID", "X-RateLimit-Limit",
			"X-RateLimit-Remaining", "X-RateLimit-Reset",
		},
		AllowCredentials: true,
		MaxAge:           86400,
	}
}

func CORS(cfg CORSConfig, logger zerolog.Logger) func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		originSet[strings.ToLower(strings.TrimRight(o, "/"))] = true
	}

	methods := strings.Join(cfg.AllowedMethods, ", ")
	headers := strings.Join(cfg.AllowedHeaders, ", ")
	exposed := strings.Join(cfg.ExposedHeaders, ", ")
	maxAge := itoa(cfg.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := cfg.AllowAllOrigins || originSet[strings.ToLower(strings.TrimRight(origin, "/"))]

			if !allowed {
				logger.Warn().
					Str("origin", origin).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("remote_addr", r.RemoteAddr).
					Msg("CORS violation: origin not allowed")

				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Access-Control-Allow-Origin", origin)
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if exposed != "" {
				w.Header().Set("Access-Control-Expose-Headers", exposed)
			}
			w.Header().Set("Vary", "Origin")

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				w.Header().Set("Access-Control-Max-Age", maxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
