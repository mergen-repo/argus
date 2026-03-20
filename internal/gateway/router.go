package gateway

import (
	authapi "github.com/btopcu/argus/internal/api/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(health *HealthHandler, authHandler *authapi.AuthHandler, jwtSecret string) *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)

	r.Get("/api/health", health.Check)

	r.Group(func(r chi.Router) {
		r.Post("/api/v1/auth/login", authHandler.Login)
		r.Post("/api/v1/auth/refresh", authHandler.Refresh)
	})

	r.Group(func(r chi.Router) {
		r.Use(JWTAuth(jwtSecret))
		r.Use(RequireRole("api_user"))
		r.Post("/api/v1/auth/logout", authHandler.Logout)
		r.Post("/api/v1/auth/2fa/setup", authHandler.Setup2FA)
	})

	r.Group(func(r chi.Router) {
		r.Use(JWTAuthAllowPartial(jwtSecret))
		r.Post("/api/v1/auth/2fa/verify", authHandler.Verify2FA)
	})

	return r
}
