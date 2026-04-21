package gateway

import (
	"net/http"

	adminapi "github.com/btopcu/argus/internal/api/admin"
	alertapi "github.com/btopcu/argus/internal/api/alert"
	analyticsapi "github.com/btopcu/argus/internal/api/analytics"
	announcementapi "github.com/btopcu/argus/internal/api/announcement"
	anomalyapi "github.com/btopcu/argus/internal/api/anomaly"
	apikeyapi "github.com/btopcu/argus/internal/api/apikey"
	apnapi "github.com/btopcu/argus/internal/api/apn"
	auditapi "github.com/btopcu/argus/internal/api/audit"
	authapi "github.com/btopcu/argus/internal/api/auth"
	cdrapi "github.com/btopcu/argus/internal/api/cdr"
	complianceapi "github.com/btopcu/argus/internal/api/compliance"
	dashboardapi "github.com/btopcu/argus/internal/api/dashboard"
	diagapi "github.com/btopcu/argus/internal/api/diagnostics"
	esimapi "github.com/btopcu/argus/internal/api/esim"
	eventsapi "github.com/btopcu/argus/internal/api/events"
	ippoolapi "github.com/btopcu/argus/internal/api/ippool"
	jobapi "github.com/btopcu/argus/internal/api/job"
	metricsapi "github.com/btopcu/argus/internal/api/metrics"
	msisdnapi "github.com/btopcu/argus/internal/api/msisdn"
	notifapi "github.com/btopcu/argus/internal/api/notification"
	onboardingapi "github.com/btopcu/argus/internal/api/onboarding"
	operatorapi "github.com/btopcu/argus/internal/api/operator"
	opsapi "github.com/btopcu/argus/internal/api/ops"
	otaapi "github.com/btopcu/argus/internal/api/ota"
	policyapi "github.com/btopcu/argus/internal/api/policy"
	reportsapi "github.com/btopcu/argus/internal/api/reports"
	roamingapi "github.com/btopcu/argus/internal/api/roaming"
	searchapi "github.com/btopcu/argus/internal/api/search"
	segmentapi "github.com/btopcu/argus/internal/api/segment"
	sessionapi "github.com/btopcu/argus/internal/api/session"
	simapi "github.com/btopcu/argus/internal/api/sim"
	slaapi "github.com/btopcu/argus/internal/api/sla"
	smsapi "github.com/btopcu/argus/internal/api/sms"
	systemapi "github.com/btopcu/argus/internal/api/system"
	tenantapi "github.com/btopcu/argus/internal/api/tenant"
	undoapi "github.com/btopcu/argus/internal/api/undo"
	userapi "github.com/btopcu/argus/internal/api/user"
	violationapi "github.com/btopcu/argus/internal/api/violation"
	webhookapi "github.com/btopcu/argus/internal/api/webhooks"
	mw "github.com/btopcu/argus/internal/middleware"
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
	Health                *HealthHandler
	AuthHandler           *authapi.AuthHandler
	TenantHandler         *tenantapi.Handler
	UserHandler           *userapi.Handler
	AuditHandler          *auditapi.Handler
	APIKeyHandler         *apikeyapi.Handler
	OperatorHandler       *operatorapi.Handler
	APNHandler            *apnapi.Handler
	IPPoolHandler         *ippoolapi.Handler
	SIMHandler            *simapi.Handler
	ESimHandler           *esimapi.Handler
	SegmentHandler        *segmentapi.Handler
	BulkHandler           *simapi.BulkHandler
	JobHandler            *jobapi.Handler
	MSISDNHandler         *msisdnapi.Handler
	SessionHandler        *sessionapi.Handler
	PolicyHandler         *policyapi.Handler
	OTAHandler            *otaapi.Handler
	CDRHandler            *cdrapi.Handler
	AnalyticsHandler      *analyticsapi.Handler
	AnomalyHandler        *anomalyapi.Handler
	AlertHandler          *alertapi.Handler
	EventsCatalogHandler  *eventsapi.Handler
	NotificationHandler   *notifapi.Handler
	SMSWebhookHandler     *notifapi.SMSWebhookHandler
	DiagnosticsHandler    *diagapi.Handler
	MetricsHandler        *metricsapi.Handler
	ComplianceHandler     *complianceapi.Handler
	ViolationHandler      *violationapi.Handler
	DashboardHandler      *dashboardapi.Handler
	SLAHandler            *slaapi.Handler
	ReportsHandler        *reportsapi.Handler
	ReliabilityHandler    *systemapi.ReliabilityHandler
	StatusHandler         *systemapi.StatusHandler
	SystemConfigHandler   *systemapi.ConfigHandler
	RevokeSessionsHandler *systemapi.RevokeSessionsHandler
	CapacityHandler       *systemapi.CapacityHandler
	OnboardingHandler     *onboardingapi.Handler
	RoamingHandler        *roamingapi.Handler
	WebhookHandler        *webhookapi.Handler
	SMSHandler            *smsapi.Handler
	OpsHandler            *opsapi.Handler
	AdminHandler          *adminapi.Handler
	SearchHandler         *searchapi.Handler
	AnnouncementHandler   *announcementapi.Handler
	UndoHandler           *undoapi.Handler
	KillSwitchSvc         killSwitchChecker
	APIKeyStore           *store.APIKeyStore
	TenantLimits          *TenantLimitsMiddleware
	BulkRateLimiter       *BulkRateLimiter
	RedisClient           *redis.Client
	RateLimitPerMinute    int
	RateLimitPerHour      int
	JWTSecret             string
	JWTSecretPrevious     string
	Logger                zerolog.Logger
	MetricsReg            *metrics.Registry

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

	// limitFor returns the tenant-limits enforcement middleware for a given
	// resource, or a no-op pass-through when the deps don't include the
	// middleware (tests and lightweight setups). Wrapping individual POST
	// routes with r.With(limitFor(...)) keeps the existing GET/PATCH/DELETE
	// handlers in the same group free of the overhead.
	limitFor := func(resource LimitKey) func(http.Handler) http.Handler {
		if deps.TenantLimits == nil {
			return func(next http.Handler) http.Handler { return next }
		}
		return deps.TenantLimits.Enforce(resource)
	}

	var bulkRL func(http.Handler) http.Handler
	if deps.BulkRateLimiter != nil {
		bulkRL = deps.BulkRateLimiter.Middleware()
	} else {
		bulkRL = func(next http.Handler) http.Handler { return next }
	}

	r.Use(RecoveryWithZerolog(deps.Logger))
	r.Use(CorrelationID())
	r.Use(chimiddleware.RealIP)

	if deps.KillSwitchSvc != nil {
		r.Use(KillSwitchMiddleware(deps.KillSwitchSvc, []string{
			"/api/v1/auth/",
			"/api/v1/admin/kill-switches",
			"/health",
			"/api/health",
		}))
	}

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
			r.Post("/api/v1/oauth/token", deps.AuthHandler.OAuthToken)
		})

		r.Group(func(r chi.Router) {
			if deps.RequestBodyAuthMB > 0 {
				r.Use(BodyLimit(deps.RequestBodyAuthMB))
			}
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Post("/api/v1/auth/logout", deps.AuthHandler.Logout)
			r.Post("/api/v1/auth/2fa/setup", deps.AuthHandler.Setup2FA)
			r.Post("/api/v1/auth/2fa/backup-codes", deps.AuthHandler.GenerateBackupCodes)
			r.Get("/api/v1/auth/2fa/backup-codes/remaining", deps.AuthHandler.BackupCodesRemaining)
			r.Get("/api/v1/auth/sessions", deps.AuthHandler.ListSessions)
			r.Delete("/api/v1/auth/sessions/{id}", deps.AuthHandler.RevokeSession)
		})

		r.Group(func(r chi.Router) {
			if deps.RequestBodyAuthMB > 0 {
				r.Use(BodyLimit(deps.RequestBodyAuthMB))
			}
			r.Use(JWTAuthAllowPartial(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Post("/api/v1/auth/2fa/verify", deps.AuthHandler.Verify2FA)
		})

		r.Group(func(r chi.Router) {
			if deps.RequestBodyAuthMB > 0 {
				r.Use(BodyLimit(deps.RequestBodyAuthMB))
			}
			r.Use(JWTAuthAllowForceChange(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Post("/api/v1/auth/password/change", deps.AuthHandler.ChangePassword)
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
				r.With(limitFor(LimitUsers)).Post("/api/v1/users", deps.UserHandler.Create)
				r.Get("/api/v1/users/export.csv", deps.UserHandler.ExportCSV)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("api_user"))
				r.Patch("/api/v1/users/{id}", deps.UserHandler.Update)
				// Revoke sessions: self or tenant_admin (self-check enforced in handler).
				r.Post("/api/v1/users/{id}/revoke-sessions", deps.UserHandler.RevokeSessions)
				// Saved views
				r.Get("/api/v1/users/me/views", deps.UserHandler.ListViews)
				r.Post("/api/v1/users/me/views", deps.UserHandler.CreateView)
				r.Patch("/api/v1/users/me/views/{view_id}", deps.UserHandler.UpdateView)
				r.Delete("/api/v1/users/me/views/{view_id}", deps.UserHandler.DeleteView)
				r.Post("/api/v1/users/me/views/{view_id}/default", deps.UserHandler.SetDefaultView)
				// Preferences
				r.Patch("/api/v1/users/me/preferences", deps.UserHandler.UpdatePreferences)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				// GDPR erasure — requires ?gdpr=1 query param (enforced in handler).
				r.Delete("/api/v1/users/{id}", deps.UserHandler.Delete)
				r.Post("/api/v1/users/{id}/unlock", deps.UserHandler.Unlock)
				r.Post("/api/v1/users/{id}/reset-password", deps.UserHandler.ResetPassword)
				r.Get("/api/v1/users/{id}", deps.UserHandler.GetUser)
				r.Get("/api/v1/users/{id}/activity", deps.UserHandler.Activity)
			})
		}

		if deps.AuditHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Get("/api/v1/audit-logs", deps.AuditHandler.List)
				r.Post("/api/v1/audit-logs/export", deps.AuditHandler.Export)
				r.Get("/api/v1/audit-logs/export.csv", deps.AuditHandler.ExportCSV)
				r.Get("/api/v1/audit", deps.AuditHandler.List)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("super_admin"))
				r.Get("/api/v1/audit-logs/verify", deps.AuditHandler.Verify)
				r.Post("/api/v1/audit/system-events", deps.AuditHandler.EmitSystemEvent)
			})
		}

		if deps.OperatorHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("api_user"))
				r.Get("/api/v1/operators", deps.OperatorHandler.List)
				r.Get("/api/v1/operators/export.csv", deps.OperatorHandler.ExportCSV)
				r.Get("/api/v1/operators/{id}", deps.OperatorHandler.Get)
				r.Get("/api/v1/operators/{id}/sessions", deps.OperatorHandler.GetSessions)
				r.Get("/api/v1/operators/{id}/traffic", deps.OperatorHandler.GetTraffic)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("super_admin"))
				r.Post("/api/v1/operators", deps.OperatorHandler.Create)
				r.Patch("/api/v1/operators/{id}", deps.OperatorHandler.Update)
				r.Post("/api/v1/operator-grants", deps.OperatorHandler.CreateGrant)
				r.Delete("/api/v1/operator-grants/{id}", deps.OperatorHandler.DeleteGrant)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Post("/api/v1/operators/{id}/test", deps.OperatorHandler.TestConnection)
				r.Post("/api/v1/operators/{id}/test/{protocol}", deps.OperatorHandler.TestConnectionForProtocol)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("operator_manager"))
				r.Get("/api/v1/operators/{id}/health", deps.OperatorHandler.GetHealth)
				r.Get("/api/v1/operators/{id}/health-history", deps.OperatorHandler.GetHealthHistory)
				r.Get("/api/v1/operators/{id}/metrics", deps.OperatorHandler.GetMetrics)
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
				r.Get("/api/v1/apns/export.csv", deps.APNHandler.ExportCSV)
				r.Get("/api/v1/apns/{id}", deps.APNHandler.Get)
				r.Get("/api/v1/apns/{id}/sims", deps.APNHandler.ListSIMs)
				r.Get("/api/v1/apns/{id}/traffic", deps.APNHandler.GetTraffic)
				r.Get("/api/v1/apns/{id}/referencing-policies", deps.APNHandler.ListReferencingPolicies)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.With(limitFor(LimitAPNs)).Post("/api/v1/apns", deps.APNHandler.Create)
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
				r.Get("/api/v1/api-keys/export.csv", deps.APIKeyHandler.ExportCSV)
				r.With(limitFor(LimitAPIKeys)).Post("/api/v1/api-keys", deps.APIKeyHandler.Create)
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
				r.Get("/api/v1/sims/export.csv", deps.SIMHandler.ExportCSV)
				r.With(limitFor(LimitSIMs)).Post("/api/v1/sims", deps.SIMHandler.Create)
				r.Get("/api/v1/sims/{id}", deps.SIMHandler.Get)
				r.Patch("/api/v1/sims/{id}", deps.SIMHandler.Patch)
				r.Get("/api/v1/sims/{id}/history", deps.SIMHandler.GetHistory)
				r.Get("/api/v1/sims/{id}/sessions", deps.SIMHandler.GetSessions)
				r.Get("/api/v1/sims/{id}/ip-current", deps.SIMHandler.GetCurrentIP)
				r.Post("/api/v1/sims/{id}/activate", deps.SIMHandler.Activate)
				r.Post("/api/v1/sims/{id}/suspend", deps.SIMHandler.Suspend)
				r.Post("/api/v1/sims/{id}/resume", deps.SIMHandler.Resume)
				r.Post("/api/v1/sims/{id}/report-lost", deps.SIMHandler.ReportLost)
				r.Post("/api/v1/sims/compare", deps.SIMHandler.Compare)
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
				r.With(bulkRL).Post("/api/v1/sims/bulk/state-change", deps.BulkHandler.StateChange)
			})

			r.Group(func(r chi.Router) {
				if deps.RequestBodyBulkMB > 0 {
					r.Use(BodyLimit(deps.RequestBodyBulkMB))
				}
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("policy_editor"))
				r.With(bulkRL).Post("/api/v1/sims/bulk/policy-assign", deps.BulkHandler.PolicyAssign)
			})

			r.Group(func(r chi.Router) {
				if deps.RequestBodyBulkMB > 0 {
					r.Use(BodyLimit(deps.RequestBodyBulkMB))
				}
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.With(bulkRL).Post("/api/v1/sims/bulk/operator-switch", deps.BulkHandler.OperatorSwitch)
			})
		}

		if deps.JobHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("sim_manager"))
				r.Get("/api/v1/jobs", deps.JobHandler.List)
				r.Get("/api/v1/jobs/export.csv", deps.JobHandler.ExportCSV)
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
				r.Get("/api/v1/policies/export.csv", deps.PolicyHandler.ExportCSV)
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
				r.Get("/api/v1/cdrs/export.csv", deps.CDRHandler.ExportCSV)
				r.Post("/api/v1/cdrs/export", deps.CDRHandler.Export)
			})
		}

		if deps.AnalyticsHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("analyst"))
				r.Get("/api/v1/analytics/usage", deps.AnalyticsHandler.GetUsage)
				r.Get("/api/v1/analytics/cost", deps.AnalyticsHandler.GetCost)
				r.Post("/api/v1/analytics/charts/{chart_key}/annotations", deps.AnalyticsHandler.CreateChartAnnotation)
				r.Get("/api/v1/analytics/charts/{chart_key}/annotations", deps.AnalyticsHandler.ListChartAnnotations)
				r.Delete("/api/v1/analytics/charts/{chart_key}/annotations/{annotation_id}", deps.AnalyticsHandler.DeleteChartAnnotation)
			})
		}

		if deps.AnomalyHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("analyst"))
				r.Get("/api/v1/analytics/anomalies", deps.AnomalyHandler.List)
				r.Get("/api/v1/analytics/anomalies/export.csv", deps.AnomalyHandler.ExportCSV)
				r.Get("/api/v1/analytics/anomalies/{id}", deps.AnomalyHandler.Get)
				r.Patch("/api/v1/analytics/anomalies/{id}", deps.AnomalyHandler.UpdateState)
			})
		}

		// FIX-209 — unified alerts endpoints. List/Get are analyst-readable; PATCH
		// updates state (acknowledged/resolved) and emits an audit log. See
		// internal/api/alert/handler.go for taxonomy validation.
		if deps.AlertHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("analyst"))
				r.Get("/api/v1/alerts", deps.AlertHandler.List)
				r.Get("/api/v1/alerts/{id}", deps.AlertHandler.Get)
				r.Patch("/api/v1/alerts/{id}", deps.AlertHandler.UpdateState)
			})
		}

		// FIX-212 AC-5 — event catalog (read-only, tenant-scoped).
		if deps.EventsCatalogHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Get("/api/v1/events/catalog", deps.EventsCatalogHandler.List)
			})
		}

		if deps.ViolationHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Get("/api/v1/policy-violations", deps.ViolationHandler.List)
				r.Get("/api/v1/policy-violations/export.csv", deps.ViolationHandler.ExportCSV)
				r.Get("/api/v1/policy-violations/counts", deps.ViolationHandler.CountByType)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("policy_editor"))
				r.Get("/api/v1/policy-violations/{id}", deps.ViolationHandler.Get)
				r.Post("/api/v1/policy-violations/{id}/acknowledge", deps.ViolationHandler.Acknowledge)
				r.Post("/api/v1/policy-violations/{id}/remediate", deps.ViolationHandler.Remediate)
			})
		}

		if deps.SessionHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("sim_manager"))
				r.Get("/api/v1/sessions", deps.SessionHandler.List)
				r.Get("/api/v1/sessions/export.csv", deps.SessionHandler.ExportCSV)
				r.Get("/api/v1/sessions/{id}", deps.SessionHandler.Get)
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
				r.Get("/api/v1/notifications/export.csv", deps.NotificationHandler.ExportCSV)
				r.Get("/api/v1/notifications/unread-count", deps.NotificationHandler.UnreadCount)
				r.Patch("/api/v1/notifications/{id}/read", deps.NotificationHandler.MarkRead)
				r.Post("/api/v1/notifications/read-all", deps.NotificationHandler.MarkAllRead)
				r.Get("/api/v1/notification-configs", deps.NotificationHandler.GetConfigs)
				r.Put("/api/v1/notification-configs", deps.NotificationHandler.UpdateConfigs)
				r.Get("/api/v1/notification-templates", deps.NotificationHandler.ListTemplates)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Get("/api/v1/notification-preferences", deps.NotificationHandler.GetPreferences)
				r.Put("/api/v1/notification-preferences", deps.NotificationHandler.UpdatePreferences)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("super_admin"))
				r.Put("/api/v1/notification-templates/{event_type}/{locale}", deps.NotificationHandler.UpsertTemplate)
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

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("api_user"))
				r.Post("/api/v1/compliance/data-portability/{user_id}", deps.ComplianceHandler.RequestDataPortability)
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

		if deps.ReportsHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("api_user"))
				r.Get("/api/v1/reports/definitions", deps.ReportsHandler.ListDefinitions)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("analyst"))
				r.Post("/api/v1/reports/generate", deps.ReportsHandler.Generate)
				r.Get("/api/v1/reports/scheduled", deps.ReportsHandler.ListScheduled)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Post("/api/v1/reports/scheduled", deps.ReportsHandler.CreateScheduled)
				r.Patch("/api/v1/reports/scheduled/{id}", deps.ReportsHandler.PatchScheduled)
				r.Delete("/api/v1/reports/scheduled/{id}", deps.ReportsHandler.DeleteScheduled)
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

		if deps.SystemConfigHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("super_admin"))
				r.Get("/api/v1/system/config", deps.SystemConfigHandler.Serve)
			})
		}

		if deps.RevokeSessionsHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Post("/api/v1/system/revoke-all-sessions", deps.RevokeSessionsHandler.RevokeAll)
			})
		}

		if deps.CapacityHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("analyst"))
				r.Get("/api/v1/system/capacity", deps.CapacityHandler.Get)
			})
		}

		if deps.OnboardingHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Route("/api/v1", deps.OnboardingHandler.Mount)
			})
		}

		if deps.WebhookHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("tenant_admin"))
				r.Get("/api/v1/webhooks", deps.WebhookHandler.List)
				r.Post("/api/v1/webhooks", deps.WebhookHandler.Create)
				r.Patch("/api/v1/webhooks/{id}", deps.WebhookHandler.Update)
				r.Delete("/api/v1/webhooks/{id}", deps.WebhookHandler.Delete)
				r.Get("/api/v1/webhooks/{id}/deliveries", deps.WebhookHandler.ListDeliveries)
				r.Post("/api/v1/webhooks/{id}/deliveries/{delivery_id}/retry", deps.WebhookHandler.RetryDelivery)
			})
		}

		if deps.SMSHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("sim_manager"))
				r.Post("/api/v1/sms/send", deps.SMSHandler.Send)
				r.Get("/api/v1/sms/history", deps.SMSHandler.History)
			})
		}

		if deps.RoamingHandler != nil {
			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("api_user"))
				r.Get("/api/v1/roaming-agreements", deps.RoamingHandler.List)
				r.Get("/api/v1/roaming-agreements/{id}", deps.RoamingHandler.Get)
			})

			r.Group(func(r chi.Router) {
				r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
				r.Use(RequireRole("operator_manager"))
				r.Post("/api/v1/roaming-agreements", deps.RoamingHandler.Create)
				r.Patch("/api/v1/roaming-agreements/{id}", deps.RoamingHandler.Update)
				r.Delete("/api/v1/roaming-agreements/{id}", deps.RoamingHandler.Terminate)
			})

			if deps.OperatorHandler != nil {
				r.Group(func(r chi.Router) {
					r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
					r.Use(RequireRole("api_user"))
					r.Get("/api/v1/operators/{id}/roaming-agreements", deps.RoamingHandler.ListForOperator)
				})
			}
		}

	})

	if deps.OpsHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/ops/metrics/snapshot", deps.OpsHandler.Snapshot)
			r.Get("/api/v1/ops/infra-health", deps.OpsHandler.InfraHealth)
			r.Get("/api/v1/ops/incidents", deps.OpsHandler.Incidents)
		})
	}

	if deps.AnomalyHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/analytics/anomalies/{id}/comments", deps.AnomalyHandler.ListComments)
			r.Post("/api/v1/analytics/anomalies/{id}/comments", deps.AnomalyHandler.AddComment)
			r.Post("/api/v1/analytics/anomalies/{id}/escalate", deps.AnomalyHandler.Escalate)
		})
	}

	if deps.AdminHandler != nil {
		// super_admin-only admin endpoints
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/admin/tenants/resources", deps.AdminHandler.ListTenantResources)
			r.Get("/api/v1/admin/cost/by-tenant", deps.AdminHandler.ListCostByTenant)
			r.Get("/api/v1/admin/sessions/active", deps.AdminHandler.ListActiveSessions)
			r.Post("/api/v1/admin/sessions/{session_id}/revoke", deps.AdminHandler.ForceLogoutSession)
			r.Get("/api/v1/admin/api-keys/usage", deps.AdminHandler.ListAPIKeyUsage)
			r.Get("/api/v1/admin/kill-switches", deps.AdminHandler.ListKillSwitches)
			r.Patch("/api/v1/admin/kill-switches/{key}", deps.AdminHandler.ToggleKillSwitch)
			r.Get("/api/v1/admin/maintenance-windows", deps.AdminHandler.ListMaintenanceWindows)
			r.Post("/api/v1/admin/maintenance-windows", deps.AdminHandler.CreateMaintenanceWindow)
			r.Delete("/api/v1/admin/maintenance-windows/{id}", deps.AdminHandler.DeleteMaintenanceWindow)
			r.Get("/api/v1/admin/delivery/status", deps.AdminHandler.GetDeliveryStatus)
			r.Get("/api/v1/admin/purge-history", deps.AdminHandler.ListPurgeHistory)
			// Impersonation
			r.Post("/api/v1/admin/impersonate/{user_id}", deps.AdminHandler.Impersonate)
			// Tenant context switch (super_admin only — target tenant picker
			// in topbar). Exits below in its own group since exit works
			// even when no context is active.
			r.Post("/api/v1/auth/switch-tenant", deps.AdminHandler.SwitchTenant)
			r.Post("/api/v1/auth/exit-tenant-context", deps.AdminHandler.ExitTenantContext)
		})

		// super_admin + tenant_admin scoped endpoints
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("tenant_admin"))
			r.Get("/api/v1/admin/tenants/quotas", deps.AdminHandler.ListTenantQuotas)
			r.Get("/api/v1/admin/dsar/queue", deps.AdminHandler.ListDSARQueue)
		})

		// Impersonation exit (any authenticated user can call this to drop impersonation)
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Post("/api/v1/admin/impersonate/exit", deps.AdminHandler.ImpersonateExit)
		})
	}

	if deps.AnnouncementHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Get("/api/v1/announcements/active", deps.AnnouncementHandler.GetActive)
			r.Post("/api/v1/announcements/{id}/dismiss", deps.AnnouncementHandler.Dismiss)
		})

		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("super_admin"))
			r.Get("/api/v1/announcements", deps.AnnouncementHandler.List)
			r.Post("/api/v1/announcements", deps.AnnouncementHandler.Create)
			r.Patch("/api/v1/announcements/{id}", deps.AnnouncementHandler.Update)
			r.Delete("/api/v1/announcements/{id}", deps.AnnouncementHandler.Delete)
		})
	}

	if deps.UndoHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("api_user"))
			r.Use(mw.ImpersonationReadOnly)
			r.Post("/api/v1/undo/{action_id}", deps.UndoHandler.Execute)
		})
	}

	if deps.SearchHandler != nil {
		r.Group(func(r chi.Router) {
			r.Use(JWTAuth(deps.JWTSecret, deps.JWTSecretPrevious))
			r.Use(RequireRole("analyst"))
			r.Get("/api/v1/search", deps.SearchHandler.Search)
		})
	}

	var handler http.Handler = r
	handler = otelhttp.NewHandler(handler, "argus.http",
		otelhttp.WithPropagators(otel.GetTextMapPropagator()),
	)
	return handler
}
