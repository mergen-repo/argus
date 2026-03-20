package gateway

import (
	"fmt"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
)

func RoleLevel(role string) int {
	return apierr.RoleLevel(role)
}

func HasRole(userRole, requiredRole string) bool {
	return apierr.HasRole(userRole, requiredRole)
}

func RequireRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(apierr.RoleKey).(string)
			if role == "" {
				apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden,
					"No role assigned to current user")
				return
			}

			if !HasRole(role, minRole) {
				apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
					fmt.Sprintf("This action requires %s role or higher", minRole),
					[]map[string]string{{"required_role": minRole, "current_role": role}})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authType, _ := r.Context().Value(apierr.AuthTypeKey).(string)
			if authType != "api_key" {
				next.ServeHTTP(w, r)
				return
			}

			scopes, _ := r.Context().Value(apierr.ScopesKey).([]string)
			for _, s := range scopes {
				if s == scope {
					next.ServeHTTP(w, r)
					return
				}
			}

			apierr.WriteError(w, http.StatusForbidden, apierr.CodeScopeDenied,
				"API key does not have the required scope",
				[]map[string]interface{}{{"required_scope": scope, "available_scopes": scopes}})
		})
	}
}
