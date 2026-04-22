package cdr

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/export"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// ExportCSV streams CDR rows as a CSV download (admin quick-export path).
// Enforces the same 30-day cap as the job-based export; wider ranges require
// super_admin with override_range=true. Filters mirror the list endpoint.
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	filters := map[string]string{}
	for _, k := range []string{"operator_id", "sim_id", "apn_id", "session_id", "record_type", "rat_type"} {
		if v := q.Get(k); v != "" {
			filters[k] = v
		}
	}
	filename := export.BuildFilename("cdrs", filters)
	header := []string{"id", "session_id", "sim_id", "operator_id", "apn_id", "rat_type", "record_type", "bytes_in", "bytes_out", "duration_sec", "usage_cost", "timestamp"}

	params := store.ListCDRParams{}
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'operator_id' format")
			return
		}
		params.OperatorID = &id
	}
	if v := q.Get("sim_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'sim_id' format")
			return
		}
		params.SimID = &id
	}
	if v := q.Get("apn_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'apn_id' format")
			return
		}
		params.APNID = &id
	}
	if v := q.Get("session_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'session_id' format")
			return
		}
		params.SessionID = &id
	}
	if v := q.Get("record_type"); v != "" {
		if _, ok := allowedRecordTypes[v]; !ok {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'record_type' value")
			return
		}
		params.RecordType = v
	}
	if v := q.Get("rat_type"); v != "" {
		if _, ok := allowedRATTypes[v]; !ok {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'rat_type' value")
			return
		}
		params.RATType = v
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'from' date format, expected RFC3339")
			return
		}
		params.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'to' date format, expected RFC3339")
			return
		}
		params.To = &t
	}
	if v := q.Get("min_cost"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'min_cost' format, expected float")
			return
		}
		params.MinCost = &f
	}

	// Date range required — without it a hypertable scan would cover all chunks.
	if params.From == nil || params.To == nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "from,to", "message": "Both 'from' and 'to' are required", "code": "required"}})
		return
	}
	if params.From.After(*params.To) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "from", "message": "'from' must be before 'to'", "code": "invalid"}})
		return
	}

	// Unconditional 30-day cap (F-A2/F-A18). super_admin may override.
	override := q.Get("override_range") == "true"
	if override {
		role, _ := r.Context().Value(apierr.RoleKey).(string)
		if role != "super_admin" {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole, "override_range requires super_admin")
			return
		}
	}
	if !override && params.To.Sub(*params.From) > maxCDRQueryRange {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Date range exceeds 30 days")
		return
	}

	export.StreamCSV(w, filename, header, func(yield func([]string) bool) {
		streamErr := h.cdrStore.StreamForExportFiltered(r.Context(), tenantID, params, func(c store.CDR) error {
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
				return fmt.Errorf("export: client closed")
			}
			return nil
		})
		if streamErr != nil {
			h.logger.Error().Err(streamErr).Msg("export cdrs")
		}
	})
}
