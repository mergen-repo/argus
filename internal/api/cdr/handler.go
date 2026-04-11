package cdr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	cdrStore *store.CDRStore
	jobStore *store.JobStore
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewHandler(cdrStore *store.CDRStore, jobStore *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *Handler {
	return &Handler{
		cdrStore: cdrStore,
		jobStore: jobStore,
		eventBus: eventBus,
		logger:   logger.With().Str("component", "cdr_handler").Logger(),
	}
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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	cursor := q.Get("cursor")

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	params := store.ListCDRParams{
		Cursor: cursor,
		Limit:  limit,
	}

	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.SimID = &id
		}
	}
	if v := q.Get("operator_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.OperatorID = &id
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			params.From = &t
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'from' date format, expected RFC3339")
			return
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			params.To = &t
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'to' date format, expected RFC3339")
			return
		}
	}
	if v := q.Get("min_cost"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			params.MinCost = &f
		}
	}

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
	From       string  `json:"from"`
	To         string  `json:"to"`
	OperatorID *string `json:"operator_id,omitempty"`
	Format     string  `json:"format"`
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

	payload, _ := json.Marshal(map[string]interface{}{
		"from":        req.From,
		"to":          req.To,
		"operator_id": req.OperatorID,
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

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: exportResponse{
			JobID:  job.ID.String(),
			Status: "queued",
		},
	})
}
