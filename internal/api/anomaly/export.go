package anomaly

import (
	"net/http"

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
	if v := q.Get("severity"); v != "" {
		filters["severity"] = v
	}
	if v := q.Get("type"); v != "" {
		filters["type"] = v
	}
	filename := export.BuildFilename("anomalies", filters)
	header := []string{"id", "sim_id", "type", "severity", "state", "source", "detected_at", "acknowledged_at", "resolved_at", "created_at"}

	params := store.ListAnomalyParams{Limit: 500}
	if v := q.Get("type"); v != "" {
		params.Type = v
	}
	if v := q.Get("severity"); v != "" {
		params.Severity = v
	}
	if v := q.Get("state"); v != "" {
		params.State = v
	}
	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.SimID = &id
		}
	}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			params.Cursor = cursor
			anomalies, next, err := h.anomalyStore.ListByTenant(r.Context(), tenantID, params)
			if err != nil {
				h.logger.Error().Err(err).Msg("export anomalies")
				return
			}
			for _, a := range anomalies {
				simID := ""
				if a.SimID != nil {
					simID = a.SimID.String()
				}
				source := ""
				if a.Source != nil {
					source = *a.Source
				}
				acknowledgedAt := ""
				if a.AcknowledgedAt != nil {
					acknowledgedAt = a.AcknowledgedAt.Format("2006-01-02T15:04:05Z")
				}
				resolvedAt := ""
				if a.ResolvedAt != nil {
					resolvedAt = a.ResolvedAt.Format("2006-01-02T15:04:05Z")
				}
				if !yield([]string{
					a.ID.String(), simID, a.Type, a.Severity, a.State, source,
					a.DetectedAt.Format("2006-01-02T15:04:05Z"),
					acknowledgedAt, resolvedAt,
					a.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
