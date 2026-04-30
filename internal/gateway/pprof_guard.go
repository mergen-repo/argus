package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

func PprofGuard(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		tok := []byte(token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var candidate string
			if q := r.URL.Query().Get("token"); q != "" {
				candidate = q
			} else if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				candidate = strings.TrimPrefix(auth, "Bearer ")
			}
			if subtle.ConstantTimeCompare([]byte(candidate), tok) != 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "error",
					"error": map[string]string{
						"code":    "PPROF_UNAUTHORIZED",
						"message": "pprof requires token",
					},
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
