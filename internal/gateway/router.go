package gateway

import (
	apikeyapi "github.com/btopcu/argus/internal/api/apikey"
	apnapi "github.com/btopcu/argus/internal/api/apn"
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	ippoolapi "github.com/btopcu/argus/internal/api/ippool"
	jobapi "github.com/btopcu/argus/internal/api/job"
	msisdnapi "github.com/btopcu/argus/internal/api/msisdn"
	operatorapi "github.com/btopcu/argus/internal/api/operator"
	policyapi "github.com/btopcu/argus/internal/api/policy"
	segmentapi "github.com/btopcu/argus/internal/api/segment"
	sessionapi "github.com/btopcu/argus/internal/api/session"
	simapi "github.com/btopcu/argus/internal/api/sim"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	userapi "github.com/btopcu/argus/internal/api/user"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type RouterDeps struct {
	Health        *HealthHandler
	AuthHandler   *authapi.AuthHandler
	TenantHandler *tenantapi.Handler
	UserHandler   *userapi.Handler
	AuditHandler  *auditapi.Handler
	APIKeyHandler    *apikeyapi.Handler
	OperatorHandler  *operatorapi.Handler
	APNHandler       *apnapi.Handler
	IPPoolHandler    *ippoolapi.Handler
	SIMHandler       *simapi.Handler
	SegmentHandler   *segmentapi.Handler
	BulkHandler      *simapi.BulkHandler
	JobHandler       *jobapi.Handler
	MSISDNHandler    *msisdnapi.Handler
	SessionHandler   *sessionapi.Handler
	PolicyHandler    *policyapi.Handler
	APIKeyStore      *store.APIKeyStore
	RedisClient      *redis.Client
	RateLimitPerMinute int
	RateLimitPerHour   int
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

	if deps.RedisClient != nil {
		perMin := deps.RateLimitPerMinute
		if perMin <= 0 {
			perMin = 1000
		}
		perHour := deps.RateLimitPerHour
		if perHour <= 0 {
			perHour = 30000
		}
		r.Use(RateLimiter(deps.RedisClient, perMin, perHour, deps.Logger))
	}

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

	if deps.OperatorHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/operators", deps.OperatorHandler.List)
			r.Post("/api/v1/operators", deps.OperatorHandler.Create)
			r.Patch("/api/v1/operators/{id}", deps.OperatorHandler.Update)
			r.Post("/api/v1/operators/{id}/test", deps.OperatorHandler.TestConnection)
			r.Post("/api/v1/operator-grants", deps.OperatorHandler.CreateGrant)
			r.Delete("/api/v1/operator-grants/{id}", deps.OperatorHandler.DeleteGrant)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("operator_manager"))
			r.Get("/api/v1/operators/{id}/health", deps.OperatorHandler.GetHealth)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/operator-grants", deps.OperatorHandler.ListGrants)
		})
	}

	if deps.APNHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/apns", deps.APNHandler.List)
			r.Get("/api/v1/apns/{id}", deps.APNHandler.Get)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/apns", deps.APNHandler.Create)
			r.Patch("/api/v1/apns/{id}", deps.APNHandler.Update)
			r.Delete("/api/v1/apns/{id}", deps.APNHandler.Archive)
		})
	}

	if deps.IPPoolHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("operator_manager"))
			r.Get("/api/v1/ip-pools", deps.IPPoolHandler.List)
			r.Get("/api/v1/ip-pools/{id}", deps.IPPoolHandler.Get)
			r.Get("/api/v1/ip-pools/{id}/addresses", deps.IPPoolHandler.ListAddresses)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/ip-pools", deps.IPPoolHandler.Create)
			r.Patch("/api/v1/ip-pools/{id}", deps.IPPoolHandler.Update)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Post("/api/v1/ip-pools/{id}/addresses/reserve", deps.IPPoolHandler.ReserveIP)
		})
	}

	if deps.APIKeyHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/api-keys", deps.APIKeyHandler.List)
			r.Post("/api/v1/api-keys", deps.APIKeyHandler.Create)
			r.Patch("/api/v1/api-keys/{id}", deps.APIKeyHandler.Update)
			r.Post("/api/v1/api-keys/{id}/rotate", deps.APIKeyHandler.Rotate)
			r.Delete("/api/v1/api-keys/{id}", deps.APIKeyHandler.Delete)
		})
	}

	if deps.SIMHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/sims", deps.SIMHandler.List)
			r.Post("/api/v1/sims", deps.SIMHandler.Create)
			r.Get("/api/v1/sims/{id}", deps.SIMHandler.Get)
			r.Get("/api/v1/sims/{id}/history", deps.SIMHandler.GetHistory)
			r.Post("/api/v1/sims/{id}/activate", deps.SIMHandler.Activate)
			r.Post("/api/v1/sims/{id}/suspend", deps.SIMHandler.Suspend)
			r.Post("/api/v1/sims/{id}/resume", deps.SIMHandler.Resume)
			r.Post("/api/v1/sims/{id}/report-lost", deps.SIMHandler.ReportLost)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/sims/{id}/terminate", deps.SIMHandler.Terminate)
		})
	}

	if deps.SegmentHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/sim-segments", deps.SegmentHandler.List)
			r.Post("/api/v1/sim-segments", deps.SegmentHandler.Create)
			r.Get("/api/v1/sim-segments/{id}", deps.SegmentHandler.GetByID)
			r.Delete("/api/v1/sim-segments/{id}", deps.SegmentHandler.Delete)
			r.Get("/api/v1/sim-segments/{id}/count", deps.SegmentHandler.Count)
			r.Get("/api/v1/sim-segments/{id}/summary", deps.SegmentHandler.StateSummary)
		})
	}

	if deps.BulkHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Post("/api/v1/sims/bulk/import", deps.BulkHandler.Import)
		})
	}

	if deps.JobHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/jobs", deps.JobHandler.List)
			r.Get("/api/v1/jobs/{id}", deps.JobHandler.Get)
			r.Post("/api/v1/jobs/{id}/retry", deps.JobHandler.Retry)
			r.Get("/api/v1/jobs/{id}/errors", deps.JobHandler.ErrorReport)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/jobs/{id}/cancel", deps.JobHandler.Cancel)
		})
	}

	if deps.MSISDNHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/msisdn-pool", deps.MSISDNHandler.List)
			r.Post("/api/v1/msisdn-pool/{id}/assign", deps.MSISDNHandler.Assign)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/msisdn-pool/import", deps.MSISDNHandler.Import)
		})
	}

	if deps.PolicyHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("policy_editor"))
			r.Get("/api/v1/policies", deps.PolicyHandler.List)
			r.Post("/api/v1/policies", deps.PolicyHandler.Create)
			r.Get("/api/v1/policies/{id}", deps.PolicyHandler.Get)
			r.Patch("/api/v1/policies/{id}", deps.PolicyHandler.Update)
			r.Delete("/api/v1/policies/{id}", deps.PolicyHandler.Delete)
			r.Post("/api/v1/policies/{id}/versions", deps.PolicyHandler.CreateVersion)
			r.Patch("/api/v1/policy-versions/{id}", deps.PolicyHandler.UpdateVersion)
			r.Post("/api/v1/policy-versions/{id}/activate", deps.PolicyHandler.ActivateVersion)
			r.Post("/api/v1/policy-versions/{id}/dry-run", deps.PolicyHandler.DryRun)
			r.Post("/api/v1/policy-versions/{id}/rollout", deps.PolicyHandler.StartRollout)
			r.Get("/api/v1/policy-versions/{id1}/diff/{id2}", deps.PolicyHandler.DiffVersions)
			r.Post("/api/v1/policy-rollouts/{id}/advance", deps.PolicyHandler.AdvanceRollout)
			r.Post("/api/v1/policy-rollouts/{id}/rollback", deps.PolicyHandler.RollbackRollout)
			r.Get("/api/v1/policy-rollouts/{id}", deps.PolicyHandler.GetRollout)
		})
	}

	if deps.SessionHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/sessions", deps.SessionHandler.List)
			r.Post("/api/v1/sessions/{id}/disconnect", deps.SessionHandler.Disconnect)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/sessions/stats", deps.SessionHandler.Stats)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/sessions/bulk/disconnect", deps.SessionHandler.BulkDisconnect)
		})
	}

	return r
}
