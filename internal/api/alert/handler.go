package alert

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var validAlertStates = map[string]bool{
	"open":         true,
	"acknowledged": true,
	"resolved":     true,
	"suppressed":   true,
}

var validAlertSources = map[string]bool{
	"sim":      true,
	"operator": true,
	"infra":    true,
	"policy":   true,
	"system":   true,
}

var allowedUpdateStates = map[string]bool{
	"acknowledged": true,
	"resolved":     true,
}

type Handler struct {
	alertStore *store.AlertStore
	auditSvc   audit.Auditor
	logger     zerolog.Logger
}

func NewHandler(alertStore *store.AlertStore, auditSvc audit.Auditor, logger zerolog.Logger) *Handler {
	return &Handler{
		alertStore: alertStore,
		auditSvc:   auditSvc,
		logger:     logger.With().Str("component", "alert_handler").Logger(),
	}
}

type alertDTO struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	Type           string          `json:"type"`
	Severity       string          `json:"severity"`
	Source         string          `json:"source"`
	State          string          `json:"state"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Meta           json.RawMessage `json:"meta"`
	SimID          *string         `json:"sim_id"`
	OperatorID     *string         `json:"operator_id"`
	APNID          *string         `json:"apn_id"`
	DedupKey       *string         `json:"dedup_key"`
	FiredAt        string          `json:"fired_at"`
	AcknowledgedAt *string         `json:"acknowledged_at"`
	AcknowledgedBy *string         `json:"acknowledged_by"`
	ResolvedAt     *string         `json:"resolved_at"`
}

func toAlertDTO(a *store.Alert) alertDTO {
	dto := alertDTO{
		ID:          a.ID.String(),
		TenantID:    a.TenantID.String(),
		Type:        a.Type,
		Severity:    a.Severity,
		Source:      a.Source,
		State:       a.State,
		Title:       a.Title,
		Description: a.Description,
		Meta:        a.Meta,
		DedupKey:    a.DedupKey,
		FiredAt:     a.FiredAt.UTC().Format(time.RFC3339),
	}
	if a.SimID != nil {
		s := a.SimID.String()
		dto.SimID = &s
	}
	if a.OperatorID != nil {
		s := a.OperatorID.String()
		dto.OperatorID = &s
	}
	if a.APNID != nil {
		s := a.APNID.String()
		dto.APNID = &s
	}
	if a.AcknowledgedAt != nil {
		s := a.AcknowledgedAt.UTC().Format(time.RFC3339)
		dto.AcknowledgedAt = &s
	}
	if a.AcknowledgedBy != nil {
		s := a.AcknowledgedBy.String()
		dto.AcknowledgedBy = &s
	}
	if a.ResolvedAt != nil {
		s := a.ResolvedAt.UTC().Format(time.RFC3339)
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

	sevFilter := q.Get("severity")
	if sevFilter != "" {
		if err := severity.Validate(sevFilter); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidSeverity,
				"severity must be one of: critical, high, medium, low, info; got '"+sevFilter+"'")
			return
		}
	}

	stateFilter := q.Get("state")
	if stateFilter != "" && !validAlertStates[stateFilter] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"state must be one of: open, acknowledged, resolved, suppressed; got '"+stateFilter+"'")
		return
	}

	sourceFilter := q.Get("source")
	if sourceFilter != "" && !validAlertSources[sourceFilter] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"source must be one of: sim, operator, infra, policy, system; got '"+sourceFilter+"'")
		return
	}

	params := store.ListAlertsParams{
		Type:     q.Get("type"),
		Severity: sevFilter,
		Source:   sourceFilter,
		State:    stateFilter,
		Q:        q.Get("q"),
		Limit:    limit,
	}

	if v := q.Get("sim_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format")
			return
		}
		params.SimID = &id
	}
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
			return
		}
		params.OperatorID = &id
	}
	if v := q.Get("apn_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
			return
		}
		params.APNID = &id
	}
	if v := q.Get("cursor"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid cursor format")
			return
		}
		params.Cursor = &id
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid 'from' date format; expected RFC3339")
			return
		}
		params.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid 'to' date format; expected RFC3339")
			return
		}
		params.To = &t
	}

	alerts, nextCursor, err := h.alertStore.ListByTenant(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list alerts")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]alertDTO, 0, len(alerts))
	for i := range alerts {
		dtos = append(dtos, toAlertDTO(&alerts[i]))
	}

	meta := apierr.ListMeta{
		HasMore: nextCursor != nil,
		Limit:   limit,
	}
	if nextCursor != nil {
		meta.Cursor = nextCursor.String()
	}
	apierr.WriteList(w, http.StatusOK, dtos, meta)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid alert ID")
		return
	}

	a, err := h.alertStore.GetByID(r.Context(), tenantID, id)
	if errors.Is(err, store.ErrAlertNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNotFound, "alert not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get alert")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toAlertDTO(a))
}

type updateStateRequest struct {
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
}

func (h *Handler) UpdateState(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid alert ID")
		return
	}

	var req updateStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	if !allowedUpdateStates[req.State] {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeInvalidStateTransition,
			"invalid alert state transition; allowed: acknowledged, resolved")
		return
	}
	if len(req.Note) > 2000 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "note must be 2000 characters or fewer")
		return
	}

	var userPtr *uuid.UUID
	if userID != uuid.Nil && req.State == "acknowledged" {
		uid := userID
		userPtr = &uid
	}

	a, err := h.alertStore.UpdateState(r.Context(), tenantID, id, req.State, userPtr)
	if errors.Is(err, store.ErrAlertNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeAlertNotFound, "alert not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAlertTransition) {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeInvalidStateTransition,
			"invalid alert state transition; allowed: acknowledged, resolved")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("update alert state")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	afterData := map[string]string{"state": req.State}
	if req.Note != "" {
		afterData["note"] = req.Note
	}
	audit.Emit(r, h.logger, h.auditSvc, "alert.update", "alert", id.String(), nil, afterData)

	apierr.WriteSuccess(w, http.StatusOK, toAlertDTO(a))
}
