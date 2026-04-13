package gateway

import (
	"net/http"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
)

// killSwitchChecker is satisfied by *killswitch.Service.
type killSwitchChecker interface {
	IsEnabled(key string) bool
}

// KillSwitchMiddleware returns an HTTP middleware that intercepts non-GET
// mutations when the "read_only_mode" kill switch is active, returning
// 503 SERVICE_DEGRADED. The allowPrefixes list is always passed through
// regardless of the kill-switch state (e.g., /api/v1/auth/, /api/v1/admin/kill-switches).
func KillSwitchMiddleware(ks killSwitchChecker, allowPrefixes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ks == nil || !ks.IsEnabled("read_only_mode") {
				next.ServeHTTP(w, r)
				return
			}

			// GET / HEAD / OPTIONS always allowed
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// Check allowlist prefixes
			for _, prefix := range allowPrefixes {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)
					return
				}
			}

			apierr.WriteError(w, http.StatusServiceUnavailable, "SERVICE_DEGRADED",
				"The system is in read-only mode. Mutations are not allowed.",
				map[string]string{"key": "read_only_mode"})
		})
	}
}
