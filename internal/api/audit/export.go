package audit

import (
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/export"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	filters := map[string]string{}
	if v := q.Get("action"); v != "" {
		filters["action"] = v
	}
	if v := q.Get("entity_type"); v != "" {
		filters["entity_type"] = v
	}
	filename := export.BuildFilename("audit_events", filters)
	header := []string{"id", "action", "entity_type", "entity_id", "user_id", "ip_address", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			params := store.ListAuditParams{
				Cursor:     cursor,
				Limit:      500,
				Action:     q.Get("action"),
				EntityType: q.Get("entity_type"),
				EntityID:   q.Get("entity_id"),
			}
			entries, next, err := h.auditStore.List(r.Context(), tenantID, params)
			if err != nil {
				h.logger.Error().Err(err).Msg("export audit events")
				return
			}
			for _, e := range entries {
				userID := ""
				if e.UserID != nil {
					userID = e.UserID.String()
				}
				ip := ""
				if e.IPAddress != nil {
					ip = *e.IPAddress
				}
				if !yield([]string{
					strconv.FormatInt(e.ID, 10), e.Action, e.EntityType, e.EntityID,
					userID, ip,
					e.CreatedAt.Format("2006-01-02T15:04:05Z"),
				}) {
					return
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	})
}
