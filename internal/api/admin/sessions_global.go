package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/geoip"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// activeSessionItem matches FE ActiveSession (web/src/types/admin.ts): flat
// browser/os strings (rather than nested ua_parsed), last_seen_at, ip_address.
type activeSessionItem struct {
	SessionID   uuid.UUID           `json:"session_id"`
	UserID      uuid.UUID           `json:"user_id"`
	UserEmail   string              `json:"user_email"`
	TenantID    uuid.UUID           `json:"tenant_id"`
	TenantName  string              `json:"tenant_name"`
	IPAddress   string              `json:"ip_address"`
	Browser     string              `json:"browser"`
	OS          string              `json:"os"`
	Device      string              `json:"device"`
	IdleSeconds int64               `json:"idle_seconds"`
	CreatedAt   time.Time           `json:"created_at"`
	LastSeenAt  time.Time           `json:"last_seen_at"`
	Location    *geoip.LocationInfo `json:"location,omitempty"`
}

type uaParsed struct {
	Device  string `json:"device"`
	OS      string `json:"os"`
	Browser string `json:"browser"`
}

func parseUA(ua string) uaParsed {
	if ua == "" {
		return uaParsed{Device: "Unknown", OS: "Unknown", Browser: "Unknown"}
	}
	parsed := uaParsed{Device: "Desktop", OS: "Unknown", Browser: "Unknown"}

	switch {
	case strings.Contains(ua, "Chrome"):
		parsed.Browser = "Chrome"
	case strings.Contains(ua, "Firefox"):
		parsed.Browser = "Firefox"
	case strings.Contains(ua, "Safari"):
		parsed.Browser = "Safari"
	default:
		parsed.Browser = "Other"
	}

	switch {
	case strings.Contains(ua, "Windows"):
		parsed.OS = "Windows"
	case strings.Contains(ua, "Mac OS"):
		parsed.OS = "Mac"
	case strings.Contains(ua, "Linux"):
		parsed.OS = "Linux"
	case strings.Contains(ua, "Android"):
		parsed.OS = "Android"
		parsed.Device = "Mobile"
	case strings.Contains(ua, "iPhone"):
		parsed.OS = "iOS"
		parsed.Device = "Mobile"
	}

	return parsed
}

// ListActiveSessions GET /api/v1/admin/sessions/active (super_admin global; tenant_admin scoped)
func (h *Handler) ListActiveSessions(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
		limit = v
	}

	role, _ := r.Context().Value(apierr.RoleKey).(string)
	callerTenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	// tenant_admin is locked to their own tenant
	var tenantFilter *uuid.UUID
	if role != "super_admin" {
		tenantFilter = &callerTenantID
	} else if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		if id, err := uuid.Parse(tid); err == nil {
			tenantFilter = &id
		}
	}

	type row struct {
		SessionID  uuid.UUID
		UserID     uuid.UUID
		UserEmail  string
		TenantID   uuid.UUID
		TenantName string
		IPAddress  *string
		UserAgent  *string
		CreatedAt  time.Time
		ExpiresAt  time.Time
	}

	var args []interface{}
	cond := "us.revoked_at IS NULL AND us.expires_at > NOW()"
	argIdx := 1

	if tenantFilter != nil {
		cond += " AND u.tenant_id = $1"
		args = append(args, *tenantFilter)
		argIdx++
	}

	args = append(args, limit)
	query := `
		SELECT us.id, us.user_id, u.email, u.tenant_id, t.name,
			us.ip_address::text, us.user_agent, us.created_at, us.expires_at
		FROM user_sessions us
		JOIN users u ON u.id = us.user_id
		JOIN tenants t ON t.id = u.tenant_id
		WHERE ` + cond + `
		ORDER BY us.created_at DESC
		LIMIT $` + strconv.Itoa(argIdx)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		h.logger.Error().Err(err).Msg("list active sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	items := make([]activeSessionItem, 0)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.SessionID, &r.UserID, &r.UserEmail, &r.TenantID, &r.TenantName,
			&r.IPAddress, &r.UserAgent, &r.CreatedAt, &r.ExpiresAt); err != nil {
			continue
		}
		ua := ""
		if r.UserAgent != nil {
			ua = *r.UserAgent
		}
		ip := ""
		if r.IPAddress != nil {
			ip = *r.IPAddress
		}
		idleSeconds := int64(now.Sub(r.CreatedAt).Seconds())
		parsed := parseUA(ua)
		item := activeSessionItem{
			SessionID:   r.SessionID,
			UserID:      r.UserID,
			UserEmail:   r.UserEmail,
			TenantID:    r.TenantID,
			TenantName:  r.TenantName,
			IPAddress:   ip,
			Browser:     parsed.Browser,
			OS:          parsed.OS,
			Device:      parsed.Device,
			IdleSeconds: idleSeconds,
			CreatedAt:   r.CreatedAt,
			LastSeenAt:  r.CreatedAt,
		}
		if h.geoipLookup != nil && ip != "" {
			item.Location = h.geoipLookup.Lookup(ip)
		}
		items = append(items, item)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Limit:   limit,
		HasMore: len(items) == limit,
	})
}

// ForceLogoutSession POST /api/v1/admin/sessions/:session_id/revoke (super_admin)
func (h *Handler) ForceLogoutSession(w http.ResponseWriter, r *http.Request) {
	sidStr := chi.URLParam(r, "session_id")
	sid, err := uuid.Parse(sidStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid session ID")
		return
	}

	if err := h.sessionStore.RevokeSession(r.Context(), sid); err != nil {
		h.logger.Error().Err(err).Str("session_id", sidStr).Msg("force logout session")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.auditSvc != nil {
		tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		actorID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
		var actorPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorPtr = &actorID
		}
		_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
			TenantID:   tenantID,
			UserID:     actorPtr,
			Action:     "session.force_logout",
			EntityType: "user_session",
			EntityID:   sidStr,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"status": "revoked"})
}
