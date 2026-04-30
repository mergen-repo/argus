package apn

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

	q := r.URL.Query()
	filters := map[string]string{}
	if v := q.Get("state"); v != "" {
		filters["state"] = v
	}
	filename := export.BuildFilename("apns", filters)
	header := []string{"id", "name", "display_name", "apn_type", "state", "operator_id", "created_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			apns, next, err := h.apnStore.List(r.Context(), tenantID, cursor, 500, q.Get("state"), nil)
			if err != nil {
				h.logger.Error().Err(err).Msg("export apns")
				return
			}
			for _, a := range apns {
				displayName := ""
				if a.DisplayName != nil {
					displayName = *a.DisplayName
				}
				if !yield([]string{
					a.ID.String(), a.Name, displayName, a.APNType, a.State,
					a.OperatorID.String(), a.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
