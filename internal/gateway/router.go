package gateway

import (
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	userapi "github.com/btopcu/argus/internal/api/user"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

type RouterDeps struct {
	Health        *HealthHandler
	AuthHandler   *authapi.AuthHandler
	TenantHandler *tenantapi.Handler
	UserHandler   *userapi.Handler
	AuditHandler  *auditapi.Handler
	JWTSecret     string
	Logger        zerolog.Logger
}

func NewRouter(health *HealthHandler, authHandler *authapi.AuthHandler, jwtSecret string) *chi.Mux {
	return NewRouterWithDeps(RouterDeps{
		Health:      health,
		AuthHandler: authHandler,
		JWTSecret:   jwtSecret,
		Logger:      zerolog.Nop(),
	})
}

func NewRouterWithDeps(deps RouterDeps) *chi.Mux {
	r := chi.NewRouter()

	r.Use(RecoveryWithZerolog(deps.Logger))
	r.Use(CorrelationID())
	r.Use(chimiddleware.RealIP)
	r.Use(ZerologRequestLogger(deps.Logger))

	r.Get("/api/health", deps.Health.Check)

	r.Group(func(r chi.Router) {
		r.Post("/api/v1/auth/login", deps.AuthHandler.Login)
		r.Post("/api/v1/auth/refresh", deps.AuthHandler.Refresh)
	})

	r.Group(func(r chi.Router) {
		r.Use(JWTAuth(deps.JWTSecret))
		r.Use(RequireRole("api_user"))
		r.Post("/api/v1/auth/logout", deps.AuthHandler.Logout)
		r.Post("/api/v1/auth/2fa/setup", deps.AuthHandler.Setup2FA)
	})

	r.Group(func(r chi.Router) {
		r.Use(JWTAuthAllowPartial(deps.JWTSecret))
		r.Post("/api/v1/auth/2fa/verify", deps.AuthHandler.Verify2FA)
	})

	if deps.TenantHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/tenants", deps.TenantHandler.List)
			r.Post("/api/v1/tenants", deps.TenantHandler.Create)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/tenants/{id}", deps.TenantHandler.Get)
			r.Patch("/api/v1/tenants/{id}", deps.TenantHandler.Update)
			r.Get("/api/v1/tenants/{id}/stats", deps.TenantHandler.Stats)
		})
	}

	if deps.UserHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/users", deps.UserHandler.List)
			r.Post("/api/v1/users", deps.UserHandler.Create)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("api_user"))
			r.Patch("/api/v1/users/{id}", deps.UserHandler.Update)
		})
	}

	if deps.AuditHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/audit-logs", deps.AuditHandler.List)
			r.Get("/api/v1/audit-logs/verify", deps.AuditHandler.Verify)
			r.Post("/api/v1/audit-logs/export", deps.AuditHandler.Export)
		})
	}

	return r
}
