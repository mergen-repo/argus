package policy

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
	filename := export.BuildFilename("policies", filters)
	header := []string{"id", "name", "scope", "state", "created_by", "created_at", "updated_at"}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			policies, next, err := h.policyStore.List(r.Context(), tenantID, cursor, 500, q.Get("state"), q.Get("q"))
			if err != nil {
				h.logger.Error().Err(err).Msg("export policies")
				return
			}
			for _, p := range policies {
				createdBy := ""
				if p.CreatedBy != nil {
					createdBy = p.CreatedBy.String()
				}
				if !yield([]string{
					p.ID.String(), p.Name, p.Scope, p.State,
					createdBy,
					p.CreatedAt.Format("2006-01-02T15:04:05Z"),
					p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
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
