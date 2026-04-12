package system

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/notification"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// TenantSessionRevoker revokes all active sessions for every user in a tenant.
type TenantSessionRevoker interface {
	RevokeAllByTenant(ctx context.Context, tenantID uuid.UUID) (affectedUsers int64, sessionsRevoked int64, err error)
}

// TenantWSDropper disconnects all WebSocket connections for a tenant.
type TenantWSDropper interface {
	DisconnectTenant(tenantID uuid.UUID) []uuid.UUID
}

// TenantNotifier sends a notification scoped to a tenant.
type TenantNotifier interface {
	Notify(ctx context.Context, req notification.NotifyRequest) error
}

type RevokeSessionsHandler struct {
	sessionStore TenantSessionRevoker
	wsHub        TenantWSDropper
	notifSvc     TenantNotifier
	auditSvc     audit.Auditor
	logger       zerolog.Logger
}

func NewRevokeSessionsHandler(
	sessionStore TenantSessionRevoker,
	wsHub TenantWSDropper,
	notifSvc TenantNotifier,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *RevokeSessionsHandler {
	return &RevokeSessionsHandler{
		sessionStore: sessionStore,
		wsHub:        wsHub,
		notifSvc:     notifSvc,
		auditSvc:     auditSvc,
		logger:       logger.With().Str("component", "revoke_sessions_handler").Logger(),
	}
}

type revokeSessionsResp struct {
	AffectedUsers   int64 `json:"affected_users"`
	SessionsRevoked int64 `json:"sessions_revoked"`
	Notified        bool  `json:"notified"`
}

func (h *RevokeSessionsHandler) RevokeAll(w http.ResponseWriter, r *http.Request) {
	tenantParam := r.URL.Query().Get("tenant")
	if tenantParam == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "missing required query parameter: tenant")
		return
	}

	targetTenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "invalid tenant id format")
		return
	}

	callerRole, _ := r.Context().Value(apierr.RoleKey).(string)
	callerTenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	if callerRole != "super_admin" {
		if callerTenantID != targetTenantID {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
				"tenant_admin may only revoke sessions for their own tenant")
			return
		}
	}

	notifyParam := r.URL.Query().Get("notify")
	doNotify := notifyParam == "true"

	ctx := r.Context()

	affectedUsers, sessionsRevoked, err := h.sessionStore.RevokeAllByTenant(ctx, targetTenantID)
	if err != nil {
		h.logger.Error().Err(err).Str("tenant_id", targetTenantID.String()).Msg("revoke all tenant sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to revoke sessions")
		return
	}

	if h.wsHub != nil {
		h.wsHub.DisconnectTenant(targetTenantID)
	}

	notified := false
	if doNotify && h.notifSvc != nil {
		notified = true
		go func() {
			notifReq := notification.NotifyRequest{
				TenantID:  targetTenantID,
				EventType: "security_revoke_all",
				ScopeType: notification.ScopeSystem,
				Title:     "Security Alert: All Sessions Revoked",
				Body:      "All active sessions for your account have been revoked by an administrator. Please log in again.",
				Severity:  "warning",
			}
			if notifErr := h.notifSvc.Notify(context.Background(), notifReq); notifErr != nil {
				h.logger.Warn().Err(notifErr).Str("tenant_id", targetTenantID.String()).Msg("send revoke-all-sessions notification")
			}
		}()
	}

	callerUserIDStr, _ := r.Context().Value(apierr.UserIDKey).(string)
	var callerUserID *uuid.UUID
	if uid, parseErr := uuid.Parse(callerUserIDStr); parseErr == nil {
		callerUserID = &uid
	}

	ip := r.RemoteAddr
	ua := r.UserAgent()

	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, parseErr := uuid.Parse(cidStr); parseErr == nil {
			correlationID = &cid
		}
	}

	afterData, _ := json.Marshal(map[string]interface{}{
		"tenant_id":        targetTenantID.String(),
		"affected_users":   affectedUsers,
		"sessions_revoked": sessionsRevoked,
		"notified":         notified,
	})

	if h.auditSvc != nil {
		_, auditErr := h.auditSvc.CreateEntry(ctx, audit.CreateEntryParams{
			TenantID:      targetTenantID,
			UserID:        callerUserID,
			Action:        "system.tenant_sessions_revoked",
			EntityType:    "tenant",
			EntityID:      targetTenantID.String(),
			AfterData:     afterData,
			IPAddress:     &ip,
			UserAgent:     &ua,
			CorrelationID: correlationID,
		})
		if auditErr != nil {
			h.logger.Warn().Err(auditErr).Msg("audit entry for revoke-all-sessions failed")
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, revokeSessionsResp{
		AffectedUsers:   affectedUsers,
		SessionsRevoked: sessionsRevoked,
		Notified:        notified,
	})
}
