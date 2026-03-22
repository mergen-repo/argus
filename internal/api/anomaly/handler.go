package anomaly

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	anomalyStore *store.AnomalyStore
	logger       zerolog.Logger
}

func NewHandler(anomalyStore *store.AnomalyStore, logger zerolog.Logger) *Handler {
	return &Handler{
		anomalyStore: anomalyStore,
		logger:       logger.With().Str("component", "anomaly_handler").Logger(),
	}
}

type anomalyDTO struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	SimID          *string         `json:"sim_id,omitempty"`
	SimICCID       string          `json:"sim_iccid,omitempty"`
	Type           string          `json:"type"`
	Severity       string          `json:"severity"`
	State          string          `json:"state"`
	Details        json.RawMessage `json:"details"`
	Source         *string         `json:"source,omitempty"`
	DetectedAt     string          `json:"detected_at"`
	AcknowledgedAt *string         `json:"acknowledged_at,omitempty"`
	ResolvedAt     *string         `json:"resolved_at,omitempty"`
}

func toAnomalyDTO(a store.Anomaly, iccid string) anomalyDTO {
	dto := anomalyDTO{
		ID:         a.ID.String(),
		TenantID:   a.TenantID.String(),
		Type:       a.Type,
		Severity:   a.Severity,
		State:      a.State,
		Details:    a.Details,
		Source:     a.Source,
		DetectedAt: a.DetectedAt.Format(time.RFC3339),
		SimICCID:   iccid,
	}
	if a.SimID != nil {
		s := a.SimID.String()
		dto.SimID = &s
	}
	if a.AcknowledgedAt != nil {
		s := a.AcknowledgedAt.Format(time.RFC3339)
		dto.AcknowledgedAt = &s
	}
	if a.ResolvedAt != nil {
		s := a.ResolvedAt.Format(time.RFC3339)
		dto.ResolvedAt = &s
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

	limit := 50
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	params := store.ListAnomalyParams{
		Cursor:   q.Get("cursor"),
		Limit:    limit,
		Type:     q.Get("type"),
		Severity: q.Get("severity"),
		State:    q.Get("state"),
	}

	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.SimID = &id
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format")
			return
		}
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'from' date format")
			return
		}
		params.From = &t
	}

	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'to' date format")
			return
		}
		params.To = &t
	}

	anomalies, nextCursor, err := h.anomalyStore.ListByTenant(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list anomalies")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]anomalyDTO, 0, len(anomalies))
	for _, a := range anomalies {
		var iccid string
		if a.SimID != nil {
			iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
		}
		dtos = append(dtos, toAnomalyDTO(a, iccid))
	}

	apierr.WriteList(w, http.StatusOK, dtos, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}

	a, err := h.anomalyStore.GetByID(r.Context(), tenantID, id)
	if errors.Is(err, store.ErrAnomalyNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Anomaly not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get anomaly")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var iccid string
	if a.SimID != nil {
		iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
	}

	apierr.WriteSuccess(w, http.StatusOK, toAnomalyDTO(*a, iccid))
}

type updateStateRequest struct {
	State string `json:"state"`
}

func (h *Handler) UpdateState(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}

	var req updateStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	validStates := map[string]bool{
		"acknowledged":   true,
		"resolved":       true,
		"false_positive": true,
	}
	if !validStates[req.State] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid state. Must be: acknowledged, resolved, or false_positive")
		return
	}

	a, err := h.anomalyStore.UpdateState(r.Context(), tenantID, id, req.State)
	if errors.Is(err, store.ErrAnomalyNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Anomaly not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAnomalyTransition) {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeInvalidStateTransition,
			"Invalid state transition")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("update anomaly state")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var iccid string
	if a.SimID != nil {
		iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
	}

	apierr.WriteSuccess(w, http.StatusOK, toAnomalyDTO(*a, iccid))
}
