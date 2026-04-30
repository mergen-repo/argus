package admin

import (
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/geoip"
	"github.com/btopcu/argus/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Handler aggregates all admin sub-handlers. Every method requires super_admin
// (enforced at router layer) unless explicitly noted for tenant_admin.
type Handler struct {
	tenantStore *store.TenantStore
	cdrStore    *store.CDRStore
	sessionStore *store.SessionStore
	apiKeyStore  *store.APIKeyStore
	jobStore     *store.JobStore
	webhookDeliveryStore *store.WebhookDeliveryStore
	notifStore   *store.NotificationStore
	smsOutboundStore *store.SMSOutboundStore
	auditStore   *store.AuditStore
	auditSvc   audit.Auditor
	db         *pgxpool.Pool
	redis      *redis.Client
	logger     zerolog.Logger
	userStore  *store.UserStore
	jwtSecret  string
	announcementStore *store.AnnouncementStore
	geoipLookup *geoip.Lookup
}

func (h *Handler) WithGeoIP(l *geoip.Lookup) *Handler {
	h.geoipLookup = l
	return h
}

func NewHandler(
	tenantStore *store.TenantStore,
	sessionStore *store.SessionStore,
	apiKeyStore *store.APIKeyStore,
	jobStore *store.JobStore,
	webhookDeliveryStore *store.WebhookDeliveryStore,
	notifStore *store.NotificationStore,
	smsOutboundStore *store.SMSOutboundStore,
	auditStore *store.AuditStore,
	auditSvc audit.Auditor,
	db *pgxpool.Pool,
	redis *redis.Client,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		tenantStore: tenantStore,
		sessionStore: sessionStore,
		apiKeyStore:  apiKeyStore,
		jobStore:     jobStore,
		webhookDeliveryStore: webhookDeliveryStore,
		notifStore:   notifStore,
		smsOutboundStore: smsOutboundStore,
		auditStore:   auditStore,
		auditSvc:   auditSvc,
		db:         db,
		redis:      redis,
		logger:     logger.With().Str("component", "admin_handler").Logger(),
	}
}

func (h *Handler) WithUserStore(s *store.UserStore) *Handler {
	h.userStore = s
	return h
}

func (h *Handler) WithCDRStore(s *store.CDRStore) *Handler {
	h.cdrStore = s
	return h
}

func (h *Handler) WithJWTSecret(secret string) *Handler {
	h.jwtSecret = secret
	return h
}

func (h *Handler) WithAnnouncementStore(s *store.AnnouncementStore) *Handler {
	h.announcementStore = s
	return h
}
