package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

// purgeHistoryItem mirrors FE PurgeHistoryItem (web/src/types/admin.ts).
type purgeHistoryItem struct {
	SimID      uuid.UUID  `json:"sim_id"`
	ICCID      string     `json:"iccid"`
	MSISDN     string     `json:"msisdn"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	TenantName string     `json:"tenant_name"`
	PurgedAt   time.Time  `json:"purged_at"`
	Reason     string     `json:"reason"`
	ActorID    *uuid.UUID `json:"actor_id"`
	ActorEmail string     `json:"actor_email,omitempty"`
	ActorName  string     `json:"actor_name,omitempty"`
}

// ListPurgeHistory GET /api/v1/admin/purge-history (super_admin + tenant_admin)
func (h *Handler) ListPurgeHistory(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
		limit = v
	}

	var tenantFilter *uuid.UUID
	if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		if id, err := uuid.Parse(tid); err == nil {
			tenantFilter = &id
		}
	}

	var args []interface{}
	cond := "ssh.to_state = 'purged'"
	argIdx := 1

	if tenantFilter != nil {
		cond += " AND s.tenant_id = $1"
		args = append(args, *tenantFilter)
		argIdx++
	}

	args = append(args, limit)
	query := `
		SELECT ssh.sim_id, COALESCE(s.iccid, ''), COALESCE(s.msisdn, ''),
			s.tenant_id, COALESCE(t.name, ''),
			ssh.created_at, COALESCE(ssh.reason, ''), ssh.user_id,
			COALESCE(u.email, ''), COALESCE(u.name, '')
		FROM sim_state_history ssh
		JOIN sims s ON s.id = ssh.sim_id
		LEFT JOIN tenants t ON t.id = s.tenant_id
		LEFT JOIN users u ON u.id = ssh.user_id
		WHERE ` + cond + `
		ORDER BY ssh.created_at DESC
		LIMIT $` + strconv.Itoa(argIdx)

	rows, err := h.db.Query(r.Context(), query, args...)
	if err != nil {
		h.logger.Error().Err(err).Msg("query purge history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	defer rows.Close()

	items := make([]purgeHistoryItem, 0)
	for rows.Next() {
		var simID, tenantID uuid.UUID
		var iccid, msisdn, tenantName, reason, actorEmail, actorName string
		var createdAt time.Time
		var userID *uuid.UUID

		if err := rows.Scan(&simID, &iccid, &msisdn, &tenantID, &tenantName, &createdAt, &reason, &userID, &actorEmail, &actorName); err != nil {
			continue
		}

		items = append(items, purgeHistoryItem{
			SimID:      simID,
			ICCID:      iccid,
			MSISDN:     msisdn,
			TenantID:   tenantID,
			TenantName: tenantName,
			PurgedAt:   createdAt,
			Reason:     reason,
			ActorID:    userID,
			ActorEmail: actorEmail,
			ActorName:  actorName,
		})
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Limit:   limit,
		HasMore: len(items) == limit,
	})
}
