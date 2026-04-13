package cdr

import (
	"fmt"
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
	if v := q.Get("operator_id"); v != "" {
		filters["operator_id"] = v
	}
	filename := export.BuildFilename("cdrs", filters)
	header := []string{"id", "session_id", "sim_id", "operator_id", "apn_id", "rat_type", "record_type", "bytes_in", "bytes_out", "duration_sec", "usage_cost", "timestamp"}

	params := store.ListCDRParams{Limit: 500}
	if v := q.Get("operator_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.OperatorID = &id
		}
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
			cdrs, next, err := h.cdrStore.ListByTenant(r.Context(), tenantID, params)
			if err != nil {
				h.logger.Error().Err(err).Msg("export cdrs")
				return
			}
			for _, c := range cdrs {
				apnID := ""
				if c.APNID != nil {
					apnID = c.APNID.String()
				}
				ratType := ""
				if c.RATType != nil {
					ratType = *c.RATType
				}
				usageCost := ""
				if c.UsageCost != nil {
					usageCost = fmt.Sprintf("%.6f", *c.UsageCost)
				}
				if !yield([]string{
					strconv.FormatInt(c.ID, 10), c.SessionID.String(), c.SimID.String(),
					c.OperatorID.String(), apnID, ratType, c.RecordType,
					strconv.FormatInt(c.BytesIn, 10),
					strconv.FormatInt(c.BytesOut, 10),
					strconv.Itoa(c.DurationSec),
					usageCost,
					c.Timestamp.Format("2006-01-02T15:04:05Z"),
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
