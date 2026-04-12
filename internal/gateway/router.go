package gateway

import (
	"net/http"

	analyticsapi "github.com/btopcu/argus/internal/api/analytics"
	anomalyapi "github.com/btopcu/argus/internal/api/anomaly"
	dashboardapi "github.com/btopcu/argus/internal/api/dashboard"
	apikeyapi "github.com/btopcu/argus/internal/api/apikey"
	apnapi "github.com/btopcu/argus/internal/api/apn"
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	cdrapi "github.com/btopcu/argus/internal/api/cdr"
	complianceapi "github.com/btopcu/argus/internal/api/compliance"
	diagapi "github.com/btopcu/argus/internal/api/diagnostics"
	esimapi "github.com/btopcu/argus/internal/api/esim"
	metricsapi "github.com/btopcu/argus/internal/api/metrics"
	notifapi "github.com/btopcu/argus/internal/api/notification"
	ippoolapi "github.com/btopcu/argus/internal/api/ippool"
	jobapi "github.com/btopcu/argus/internal/api/job"
	msisdnapi "github.com/btopcu/argus/internal/api/msisdn"
	operatorapi "github.com/btopcu/argus/internal/api/operator"
	otaapi "github.com/btopcu/argus/internal/api/ota"
	policyapi "github.com/btopcu/argus/internal/api/policy"
	segmentapi "github.com/btopcu/argus/internal/api/segment"
	slaapi "github.com/btopcu/argus/internal/api/sla"
	systemapi "github.com/btopcu/argus/internal/api/system"
	violationapi "github.com/btopcu/argus/internal/api/violation"
	sessionapi "github.com/btopcu/argus/internal/api/session"
	simapi "github.com/btopcu/argus/internal/api/sim"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	userapi "github.com/btopcu/argus/internal/api/user"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
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
	ESimHandler      *esimapi.Handler
	SegmentHandler   *segmentapi.Handler
	BulkHandler      *simapi.BulkHandler
	JobHandler       *jobapi.Handler
	MSISDNHandler    *msisdnapi.Handler
	SessionHandler   *sessionapi.Handler
	PolicyHandler    *policyapi.Handler
	OTAHandler       *otaapi.Handler
	CDRHandler       *cdrapi.Handler
	AnalyticsHandler *analyticsapi.Handler
	AnomalyHandler       *anomalyapi.Handler
	NotificationHandler     *notifapi.Handler
	SMSWebhookHandler       *notifapi.SMSWebhookHandler
	DiagnosticsHandler   *diagapi.Handler
	MetricsHandler     *metricsapi.Handler
	ComplianceHandler  *complianceapi.Handler
	ViolationHandler   *violationapi.Handler
	DashboardHandler     *dashboardapi.Handler
	SLAHandler           *slaapi.Handler
	ReliabilityHandler   *systemapi.ReliabilityHandler
	StatusHandler        *systemapi.StatusHandler
	APIKeyStore      *store.APIKeyStore
	RedisClient      *redis.Client
	RateLimitPerMinute int
	RateLimitPerHour   int
	JWTSecret         string
	JWTSecretPrevious string
	Logger            zerolog.Logger
	MetricsReg    *metrics.Registry

	CORSConfig           *CORSConfig
	SecurityHeadersCfg   *SecurityHeadersConfig
	BruteForceCfg        *BruteForceConfig
	EnableInputSanitizer bool

	RequestBodyMaxMB  int
	RequestBodyAuthMB int
	RequestBodyBulkMB int
}

func NewRouter(health *HealthHandler, authHandler *authapi.AuthHandler, jwtSecret string) http.Handler {
	return NewRouterWithDeps(RouterDeps{
		Health:      health,
		AuthHandler: authHandler,
		JWTSecret:   jwtSecret,
		Logger:      zerolog.Nop(),
	})
}

func NewRouterWithDeps(deps RouterDeps) http.Handler {
	r := chi.NewRouter()

	r.Use(RecoveryWithZerolog(deps.Logger))
	r.Use(CorrelationID())
	r.Use(chimiddleware.RealIP)

	r.Get("/health/live", deps.Health.Live)
	r.Get("/health/ready", deps.Health.Ready)
	r.Get("/health/startup", deps.Health.Startup)

	r.Group(func(r chi.Router) {
		if deps.SecurityHeadersCfg != nil {
			r.Use(SecurityHeaders(*deps.SecurityHeadersCfg))
		}

		if deps.CORSConfig != nil {
			r.Use(CORS(*deps.CORSConfig, deps.Logger))
		}

		r.Use(ZerologRequestLogger(deps.Logger))

		if deps.MetricsReg != nil {
			r.Use(PrometheusHTTPMetrics(deps.MetricsReg))
		}

		if deps.EnableInputSanitizer {
			r.Use(InputSanitizer(deps.Logger))
		}

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

			if deps.BruteForceCfg != nil {
				r.Use(BruteForceProtection(deps.RedisClient, *deps.BruteForceCfg, deps.Logger))
			}
		}

		if deps.RequestBodyMaxMB > 0 {
			r.Use(BodyLimit(deps.RequestBodyMaxMB))
		}

		if deps.MetricsReg != nil {
			r.Handle("/metrics", deps.MetricsReg.Handler())
		}

		r.Get("/api/health", deps.Health.Check)
		r.Get("/api/v1/health", deps.Health.Check)

	if deps.StatusHandler != nil {
		r.Get("/api/v1/status", deps.StatusHandler.Serve)
	}

	if deps.SMSWebhookHandler != nil {
		r.Post("/api/v1/notifications/sms/status", deps.SMSWebhookHandler.HandleStatusCallback)
	}

	r.Group(func(r chi.Router) {
		if deps.RequestBodyAuthMB > 0 {
			r.Use(BodyLimit(deps.RequestBodyAuthMB))
		}
		r.Post("/api/v1/auth/login", deps.AuthHandler.Login)
		r.Post("/api/v1/auth/refresh", deps.AuthHandler.Refresh)
	})

	r.Group(func(r chi.Router) {
		if deps.RequestBodyAuthMB > 0 {
			r.Use(BodyLimit(deps.RequestBodyAuthMB))
		}
		r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
		r.Use(RequireRole("api_user"))
		r.Post("/api/v1/auth/logout", deps.AuthHandler.Logout)
		r.Post("/api/v1/auth/2fa/setup", deps.AuthHandler.Setup2FA)
		r.Get("/api/v1/auth/sessions", deps.AuthHandler.ListSessions)
	})

	r.Group(func(r chi.Router) {
		if deps.RequestBodyAuthMB > 0 {
			r.Use(BodyLimit(deps.RequestBodyAuthMB))
		}
		r.Use(JWTAuthAllowPartial(deps.JWTSecret, deps.JWTSecretPrevious))
		r.Post("/api/v1/auth/2fa/verify", deps.AuthHandler.Verify2FA)
	})

	if deps.TenantHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/tenants", deps.TenantHandler.List)
			r.Post("/api/v1/tenants", deps.TenantHandler.Create)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/tenants/{id}", deps.TenantHandler.Get)
			r.Patch("/api/v1/tenants/{id}", deps.TenantHandler.Update)
			r.Get("/api/v1/tenants/{id}/stats", deps.TenantHandler.Stats)
		})
	}

	if deps.UserHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/users", deps.UserHandler.List)
			r.Post("/api/v1/users", deps.UserHandler.Create)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Patch("/api/v1/users/{id}", deps.UserHandler.Update)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			// GDPR erasure — requires ?gdpr=1 query param (enforced in handler).
			r.Delete("/api/v1/users/{id}", deps.UserHandler.Delete)
		})
	}

	if deps.AuditHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/audit-logs", deps.AuditHandler.List)
			r.Get("/api/v1/audit-logs/verify", deps.AuditHandler.Verify)
			r.Post("/api/v1/audit-logs/export", deps.AuditHandler.Export)
			r.Get("/api/v1/audit", deps.AuditHandler.List)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Post("/api/v1/audit/system-events", deps.AuditHandler.EmitSystemEvent)
		})
	}

	if deps.OperatorHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/operators", deps.OperatorHandler.List)
			r.Post("/api/v1/operators", deps.OperatorHandler.Create)
			r.Patch("/api/v1/operators/{id}", deps.OperatorHandler.Update)
			r.Post("/api/v1/operators/{id}/test", deps.OperatorHandler.TestConnection)
			r.Post("/api/v1/operator-grants", deps.OperatorHandler.CreateGrant)
			r.Delete("/api/v1/operator-grants/{id}", deps.OperatorHandler.DeleteGrant)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("operator_manager"))
			r.Get("/api/v1/operators/{id}/health", deps.OperatorHandler.GetHealth)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/operator-grants", deps.OperatorHandler.ListGrants)
		})
	}

	if deps.APNHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/apns", deps.APNHandler.List)
			r.Get("/api/v1/apns/{id}", deps.APNHandler.Get)
			r.Get("/api/v1/apns/{id}/sims", deps.APNHandler.ListSIMs)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/apns", deps.APNHandler.Create)
			r.Patch("/api/v1/apns/{id}", deps.APNHandler.Update)
			r.Delete("/api/v1/apns/{id}", deps.APNHandler.Archive)
		})
	}

	if deps.IPPoolHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("operator_manager"))
			r.Get("/api/v1/ip-pools", deps.IPPoolHandler.List)
			r.Get("/api/v1/ip-pools/{id}", deps.IPPoolHandler.Get)
			r.Get("/api/v1/ip-pools/{id}/addresses", deps.IPPoolHandler.ListAddresses)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/ip-pools", deps.IPPoolHandler.Create)
			r.Patch("/api/v1/ip-pools/{id}", deps.IPPoolHandler.Update)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Post("/api/v1/ip-pools/{id}/addresses/reserve", deps.IPPoolHandler.ReserveIP)
		})
	}

	if deps.APIKeyHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
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
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/sims", deps.SIMHandler.List)
			r.Post("/api/v1/sims", deps.SIMHandler.Create)
			r.Get("/api/v1/sims/{id}", deps.SIMHandler.Get)
			r.Patch("/api/v1/sims/{id}", deps.SIMHandler.Patch)
			r.Get("/api/v1/sims/{id}/history", deps.SIMHandler.GetHistory)
			r.Get("/api/v1/sims/{id}/sessions", deps.SIMHandler.GetSessions)
			r.Post("/api/v1/sims/{id}/activate", deps.SIMHandler.Activate)
			r.Post("/api/v1/sims/{id}/suspend", deps.SIMHandler.Suspend)
			r.Post("/api/v1/sims/{id}/resume", deps.SIMHandler.Resume)
			r.Post("/api/v1/sims/{id}/report-lost", deps.SIMHandler.ReportLost)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/sims/{id}/terminate", deps.SIMHandler.Terminate)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/sims/{id}/usage", deps.SIMHandler.GetUsage)
		})
	}

	if deps.DiagnosticsHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Post("/api/v1/sims/{id}/diagnose", deps.DiagnosticsHandler.Diagnose)
		})
	}

	if deps.ESimHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/esim-profiles", deps.ESimHandler.List)
			r.Post("/api/v1/esim-profiles", deps.ESimHandler.Create)
			r.Get("/api/v1/esim-profiles/{id}", deps.ESimHandler.Get)
			r.Delete("/api/v1/esim-profiles/{id}", deps.ESimHandler.Delete)
			r.Post("/api/v1/esim-profiles/{id}/enable", deps.ESimHandler.Enable)
			r.Post("/api/v1/esim-profiles/{id}/disable", deps.ESimHandler.Disable)
			r.Post("/api/v1/esim-profiles/{id}/switch", deps.ESimHandler.Switch)
		})
	}

	if deps.SegmentHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
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
			if deps.RequestBodyBulkMB > 0 {
				r.Use(BodyLimit(deps.RequestBodyBulkMB))
			}
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Post("/api/v1/sims/bulk/import", deps.BulkHandler.Import)
			r.Post("/api/v1/sims/bulk/state-change", deps.BulkHandler.StateChange)
		})

		r.Group(func(r chi.Router) {
			if deps.RequestBodyBulkMB > 0 {
				r.Use(BodyLimit(deps.RequestBodyBulkMB))
			}
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("policy_editor"))
			r.Post("/api/v1/sims/bulk/policy-assign", deps.BulkHandler.PolicyAssign)
		})

		r.Group(func(r chi.Router) {
			if deps.RequestBodyBulkMB > 0 {
				r.Use(BodyLimit(deps.RequestBodyBulkMB))
			}
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/sims/bulk/operator-switch", deps.BulkHandler.OperatorSwitch)
		})
	}

	if deps.JobHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/jobs", deps.JobHandler.List)
			r.Get("/api/v1/jobs/{id}", deps.JobHandler.Get)
			r.Post("/api/v1/jobs/{id}/retry", deps.JobHandler.Retry)
			r.Get("/api/v1/jobs/{id}/errors", deps.JobHandler.ErrorReport)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/jobs/{id}/cancel", deps.JobHandler.Cancel)
		})
	}

	if deps.MSISDNHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/msisdn-pool", deps.MSISDNHandler.List)
			r.Post("/api/v1/msisdn-pool/{id}/assign", deps.MSISDNHandler.Assign)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/msisdn-pool/import", deps.MSISDNHandler.Import)
		})
	}

	if deps.PolicyHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
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

	if deps.OTAHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Post("/api/v1/sims/{id}/ota", deps.OTAHandler.SendToSIM)
			r.Get("/api/v1/sims/{id}/ota", deps.OTAHandler.ListHistory)
			r.Get("/api/v1/ota-commands/{commandId}", deps.OTAHandler.GetCommand)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/sims/bulk/ota", deps.OTAHandler.BulkSend)
		})
	}

	if deps.CDRHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/cdrs", deps.CDRHandler.List)
			r.Post("/api/v1/cdrs/export", deps.CDRHandler.Export)
		})
	}

	if deps.AnalyticsHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/analytics/usage", deps.AnalyticsHandler.GetUsage)
			r.Get("/api/v1/analytics/cost", deps.AnalyticsHandler.GetCost)
		})
	}

	if deps.AnomalyHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/analytics/anomalies", deps.AnomalyHandler.List)
			r.Get("/api/v1/analytics/anomalies/{id}", deps.AnomalyHandler.Get)
			r.Patch("/api/v1/analytics/anomalies/{id}", deps.AnomalyHandler.UpdateState)
		})
	}

	if deps.ViolationHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Get("/api/v1/policy-violations", deps.ViolationHandler.List)
			r.Get("/api/v1/policy-violations/counts", deps.ViolationHandler.CountByType)
		})
	}

	if deps.SessionHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("sim_manager"))
			r.Get("/api/v1/sessions", deps.SessionHandler.List)
			r.Post("/api/v1/sessions/{id}/disconnect", deps.SessionHandler.Disconnect)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/sessions/stats", deps.SessionHandler.Stats)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Post("/api/v1/sessions/bulk/disconnect", deps.SessionHandler.BulkDisconnect)
		})
	}

	if deps.NotificationHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/notifications", deps.NotificationHandler.List)
			r.Get("/api/v1/notifications/unread-count", deps.NotificationHandler.UnreadCount)
			r.Patch("/api/v1/notifications/{id}/read", deps.NotificationHandler.MarkRead)
			r.Post("/api/v1/notifications/read-all", deps.NotificationHandler.MarkAllRead)
			r.Get("/api/v1/notification-configs", deps.NotificationHandler.GetConfigs)
			r.Put("/api/v1/notification-configs", deps.NotificationHandler.UpdateConfigs)
		})
	}

	if deps.MetricsHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/system/metrics", deps.MetricsHandler.GetSystemMetrics)
		})

	}

	if deps.DashboardHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/dashboard", deps.DashboardHandler.GetDashboard)
		})
	}

	if deps.ComplianceHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/compliance/dashboard", deps.ComplianceHandler.Dashboard)
			r.Get("/api/v1/compliance/btk-report", deps.ComplianceHandler.BTKReport)
			r.Put("/api/v1/compliance/retention", deps.ComplianceHandler.UpdateRetention)
			r.Get("/api/v1/compliance/dsar/{simId}", deps.ComplianceHandler.DataSubjectAccess)
			r.Post("/api/v1/compliance/erasure/{simId}", deps.ComplianceHandler.RightToErasure)
		})
	}

	if deps.SLAHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/sla-reports", deps.SLAHandler.List)
			r.Get("/api/v1/sla-reports/{id}", deps.SLAHandler.Get)
		})
	}

	if deps.ReliabilityHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/system/backup-status", deps.ReliabilityHandler.BackupStatus)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/system/jwt-rotation-history", deps.ReliabilityHandler.JWTRotationHistory)
		})
	}

	if deps.StatusHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/status/details", deps.StatusHandler.ServeDetails)
		})
	}

	})

	var handler http.Handler = r
	handler = otelhttp.NewHandler(handler, "argus.http",
		otelhttp.WithPropagators(otel.GetTextMapPropagator()),
	)
	return handler
}
