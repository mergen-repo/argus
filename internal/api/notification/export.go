package notification

import (
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/export"
	"github.com/google/uuid"
)

func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	filename := export.BuildFilename("notifications", nil)
	header := []string{"id", "event_type", "scope_type", "title", "severity", "state", "sent_at", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			notifs, next, err := h.notifStore.ListByTenant(r.Context(), tenantID, cursor, 500)
			if err != nil {
				h.logger.Error().Err(err).Msg("export notifications")
				return
			}
			for _, n := range notifs {
				sentAt := ""
				if n.SentAt != nil {
					sentAt = n.SentAt.Format("2006-01-02T15:04:05Z")
				}
				if !yield([]string{
					n.ID.String(), n.EventType, n.ScopeType, n.Title,
					n.Severity, n.State, sentAt,
					n.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
