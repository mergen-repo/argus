package violation

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
	if v := q.Get("violation_type"); v != "" {
		filters["violation_type"] = v
	}
	filename := export.BuildFilename("violations", filters)
	header := []string{"id", "sim_id", "policy_id", "violation_type", "action_taken", "severity", "acknowledged_at", "created_at"}

	params := store.ListViolationsParams{Limit: 500}
	if v := q.Get("violation_type"); v != "" {
		params.ViolationType = v
	}
	if v := q.Get("severity"); v != "" {
		params.Severity = v
	}
	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.SimID = &id
		}
	}
	if v := q.Get("policy_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.PolicyID = &id
		}
	}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		cursor := ""
		for {
			params.Cursor = cursor
			violations, next, err := h.violationStore.List(r.Context(), tenantID, params)
			if err != nil {
				h.logger.Error().Err(err).Msg("export violations")
				return
			}
			for _, v := range violations {
				acknowledgedAt := ""
				if v.AcknowledgedAt != nil {
					acknowledgedAt = v.AcknowledgedAt.Format("2006-01-02T15:04:05Z")
				}
				if !yield([]string{
					v.ID.String(), v.SimID.String(), v.PolicyID.String(),
					v.ViolationType, v.ActionTaken, v.Severity,
					acknowledgedAt,
					v.CreatedAt.Format("2006-01-02T15:04:05Z"),
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
