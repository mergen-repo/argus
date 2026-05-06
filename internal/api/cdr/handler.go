package cdr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/analytics/aggregates"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// maxCDRQueryRange — D6: regular users cannot query more than 30 days.
// super_admin may override via `?override_range=true`.
const maxCDRQueryRange = 30 * 24 * time.Hour

// allowedRecordTypes / allowedRATTypes — whitelist per plan D2 + CDR taxonomy.
// Consumer emits start/interim/stop; anomaly analytics also reads auth/auth_fail/reject.
var allowedRecordTypes = map[string]struct{}{
	"start": {}, "interim": {}, "stop": {},
	"auth": {}, "auth_fail": {}, "reject": {},
}

var allowedRATTypes = map[string]struct{}{
	"nb_iot": {}, "lte_m": {}, "lte": {}, "nr_5g": {},
	"eutra": {}, "nr": {}, "wlan": {}, "utran": {}, "geran": {},
}

type cdrAggregates interface {
	CDRStatsInWindow(ctx context.Context, tenantID uuid.UUID, f aggregates.CDRFilter) (*store.CDRStats, error)
}

type Handler struct {
	cdrStore *store.CDRStore
	jobStore *store.JobStore
	eventBus *bus.EventBus
	auditSvc audit.Auditor
	aggSvc   cdrAggregates
	logger   zerolog.Logger
}

func NewHandler(cdrStore *store.CDRStore, jobStore *store.JobStore, eventBus *bus.EventBus, auditSvc audit.Auditor, logger zerolog.Logger, opts ...func(*Handler)) *Handler {
	h := &Handler{
		cdrStore: cdrStore,
		jobStore: jobStore,
		eventBus: eventBus,
		auditSvc: auditSvc,
		logger:   logger.With().Str("component", "cdr_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithAggregates wires the cross-surface aggregates facade (FIX-214 / FIX-208).
func WithAggregates(a cdrAggregates) func(*Handler) {
	return func(h *Handler) { h.aggSvc = a }
}

type cdrDTO struct {
	ID            int64   `json:"id"`
	SessionID     string  `json:"session_id"`
	SimID         string  `json:"sim_id"`
	OperatorID    string  `json:"operator_id"`
	APNID         string  `json:"apn_id,omitempty"`
	RATType       string  `json:"rat_type,omitempty"`
	RecordType    string  `json:"record_type"`
	BytesIn       int64   `json:"bytes_in"`
	BytesOut      int64   `json:"bytes_out"`
	DurationSec   int     `json:"duration_sec"`
	UsageCost     *string `json:"usage_cost"`
	CarrierCost   *string `json:"carrier_cost"`
	RatePerMB     *string `json:"rate_per_mb"`
	RATMultiplier *string `json:"rat_multiplier"`
	Timestamp     string  `json:"timestamp"`
}

func toCDRDTO(c store.CDR) cdrDTO {
	dto := cdrDTO{
		ID:          c.ID,
		SessionID:   c.SessionID.String(),
		SimID:       c.SimID.String(),
		OperatorID:  c.OperatorID.String(),
		RecordType:  c.RecordType,
		BytesIn:     c.BytesIn,
		BytesOut:    c.BytesOut,
		DurationSec: c.DurationSec,
		Timestamp:   c.Timestamp.Format(time.RFC3339),
	}
	if c.APNID != nil {
		dto.APNID = c.APNID.String()
	}
	if c.RATType != nil {
		dto.RATType = *c.RATType
	}
	if c.UsageCost != nil {
		s := fmt.Sprintf("%.4f", *c.UsageCost)
		dto.UsageCost = &s
	}
	if c.CarrierCost != nil {
		s := fmt.Sprintf("%.4f", *c.CarrierCost)
		dto.CarrierCost = &s
	}
	if c.RatePerMB != nil {
		s := fmt.Sprintf("%.4f", *c.RatePerMB)
		dto.RatePerMB = &s
	}
	if c.RATMultiplier != nil {
		s := fmt.Sprintf("%.2f", *c.RATMultiplier)
		dto.RATMultiplier = &s
	}
	return dto
}

// parseFilters extracts the CDR filter set from a URL query and enforces the
// 30-day range cap (bypassable for super_admin with override_range=true).
// Returns a populated ListCDRParams or writes the error envelope and returns
// ok=false. Date range is required when requireRange=true (AC-2 list endpoint).
func (h *Handler) parseFilters(w http.ResponseWriter, r *http.Request, requireRange bool) (store.ListCDRParams, bool) {
	q := r.URL.Query()
	var params store.ListCDRParams

	if v := q.Get("sim_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'sim_id' format")
			return params, false
		}
		params.SimID = &id
	}
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'operator_id' format")
			return params, false
		}
		params.OperatorID = &id
	}
	if v := q.Get("apn_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'apn_id' format")
			return params, false
		}
		params.APNID = &id
	}
	if v := q.Get("session_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'session_id' format")
			return params, false
		}
		params.SessionID = &id
	}
	if v := q.Get("record_type"); v != "" {
		if _, ok := allowedRecordTypes[v]; !ok {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'record_type' value")
			return params, false
		}
		params.RecordType = v
	}
	if v := q.Get("rat_type"); v != "" {
		if _, ok := allowedRATTypes[v]; !ok {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'rat_type' value")
			return params, false
		}
		params.RATType = v
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'from' date format, expected RFC3339")
			return params, false
		}
		params.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'to' date format, expected RFC3339")
			return params, false
		}
		params.To = &t
	}
	if v := q.Get("min_cost"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'min_cost' format, expected float")
			return params, false
		}
		params.MinCost = &f
	}

	if requireRange && (params.From == nil || params.To == nil) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "from,to", "message": "Both 'from' and 'to' are required", "code": "required"}})
		return params, false
	}
	if params.From != nil && params.To != nil {
		if params.From.After(*params.To) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
				[]map[string]interface{}{{"field": "from", "message": "'from' must be before 'to'", "code": "invalid"}})
			return params, false
		}
		override := q.Get("override_range") == "true"
		if override {
			role, _ := r.Context().Value(apierr.RoleKey).(string)
			if role != "super_admin" {
				apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole, "override_range requires super_admin")
				return params, false
			}
		}
		if !override && params.To.Sub(*params.From) > maxCDRQueryRange {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
				[]map[string]interface{}{{"field": "from,to", "message": "Date range exceeds 30 days — narrow the range or use override_range=true (super_admin only)", "code": "invalid"}})
			return params, false
		}
	}
	return params, true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	hasEntityScope := q.Get("sim_id") != "" || q.Get("msisdn") != "" || q.Get("imsi") != "" || q.Get("session_id") != ""
	params, ok := h.parseFilters(w, r, !hasEntityScope)
	if !ok {
		return
	}
	params.Cursor = q.Get("cursor")
	params.Limit = limit

	cdrs, nextCursor, err := h.cdrStore.ListByTenant(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list cdrs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]cdrDTO, 0, len(cdrs))
	for _, c := range cdrs {
		items = append(items, toCDRDTO(c))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

type exportRequest struct {
	From       string   `json:"from"`
	To         string   `json:"to"`
	OperatorID *string  `json:"operator_id,omitempty"`
	SimID      *string  `json:"sim_id,omitempty"`
	APNID      *string  `json:"apn_id,omitempty"`
	SessionID  *string  `json:"session_id,omitempty"`
	RecordType *string  `json:"record_type,omitempty"`
	RATType    *string  `json:"rat_type,omitempty"`
	MinCost    *float64 `json:"min_cost,omitempty"`
	Format     string   `json:"format"`
}

type exportResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.From == "" || req.To == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "from,to", "message": "Both 'from' and 'to' are required", "code": "required"}})
		return
	}

	fromTime, err := time.Parse(time.RFC3339, req.From)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'from' date format, expected RFC3339")
		return
	}
	toTime, err := time.Parse(time.RFC3339, req.To)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'to' date format, expected RFC3339")
		return
	}

	if fromTime.After(toTime) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "from", "message": "'from' must be before 'to'", "code": "invalid"}})
		return
	}

	if req.Format != "csv" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "format", "message": "Only 'csv' format is supported", "code": "invalid"}})
		return
	}

	// 30d cap (with super_admin override via query ?override_range=true).
	if toTime.Sub(fromTime) > maxCDRQueryRange {
		override := r.URL.Query().Get("override_range") == "true"
		if !override {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
				[]map[string]interface{}{{"field": "from,to", "message": "Date range exceeds 30 days", "code": "invalid"}})
			return
		}
		role, _ := r.Context().Value(apierr.RoleKey).(string)
		if role != "super_admin" {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole, "override_range requires super_admin")
			return
		}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"from":        req.From,
		"to":          req.To,
		"operator_id": req.OperatorID,
		"sim_id":      req.SimID,
		"apn_id":      req.APNID,
		"session_id":  req.SessionID,
		"record_type": req.RecordType,
		"rat_type":    req.RATType,
		"min_cost":    req.MinCost,
		"format":      req.Format,
	})

	var userID *uuid.UUID
	if uidStr, ok := r.Context().Value(apierr.UserIDKey).(string); ok {
		if uid, parseErr := uuid.Parse(uidStr); parseErr == nil {
			userID = &uid
		}
	}

	job, err := h.jobStore.CreateWithTenantID(r.Context(), tenantID, store.CreateJobParams{
		Type:      "cdr_export",
		Priority:  5,
		Payload:   payload,
		CreatedBy: userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create cdr export job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.eventBus != nil {
		h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, map[string]interface{}{
			"job_id":    job.ID.String(),
			"tenant_id": tenantID.String(),
			"type":      "cdr_export",
		})
	}

	audit.Emit(r, h.logger, h.auditSvc, "cdr.export", "cdr_export", job.ID.String(), nil, map[string]interface{}{
		"from":        req.From,
		"to":          req.To,
		"operator_id": req.OperatorID,
		"sim_id":      req.SimID,
		"apn_id":      req.APNID,
		"session_id":  req.SessionID,
		"format":      req.Format,
	})

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: exportResponse{
			JobID:  job.ID.String(),
			Status: "queued",
		},
	})
}
