package admin

import (
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/killswitch"
	"github.com/btopcu/argus/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Handler aggregates all admin sub-handlers. Every method requires super_admin
// (enforced at router layer) unless explicitly noted for tenant_admin.
type Handler struct {
	ksStore    *store.KillSwitchStore
	mwStore    *store.MaintenanceWindowStore
	tenantStore *store.TenantStore
	sessionStore *store.SessionStore
	apiKeyStore  *store.APIKeyStore
	jobStore     *store.JobStore
	webhookDeliveryStore *store.WebhookDeliveryStore
	notifStore   *store.NotificationStore
	smsOutboundStore *store.SMSOutboundStore
	auditStore   *store.AuditStore
	ksSvc      *killswitch.Service
	auditSvc   audit.Auditor
	db         *pgxpool.Pool
	redis      *redis.Client
	logger     zerolog.Logger
}

func NewHandler(
	ksStore *store.KillSwitchStore,
	mwStore *store.MaintenanceWindowStore,
	tenantStore *store.TenantStore,
	sessionStore *store.SessionStore,
	apiKeyStore *store.APIKeyStore,
	jobStore *store.JobStore,
	webhookDeliveryStore *store.WebhookDeliveryStore,
	notifStore *store.NotificationStore,
	smsOutboundStore *store.SMSOutboundStore,
	auditStore *store.AuditStore,
	ksSvc *killswitch.Service,
	auditSvc audit.Auditor,
	db *pgxpool.Pool,
	redis *redis.Client,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		ksStore:    ksStore,
		mwStore:    mwStore,
		tenantStore: tenantStore,
		sessionStore: sessionStore,
		apiKeyStore:  apiKeyStore,
		jobStore:     jobStore,
		webhookDeliveryStore: webhookDeliveryStore,
		notifStore:   notifStore,
		smsOutboundStore: smsOutboundStore,
		auditStore:   auditStore,
		ksSvc:      ksSvc,
		auditSvc:   auditSvc,
		db:         db,
		redis:      redis,
		logger:     logger.With().Str("component", "admin_handler").Logger(),
	}
}
