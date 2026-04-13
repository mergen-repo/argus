package violation

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	violationStore *store.PolicyViolationStore
	simStore       *store.SIMStore
	auditSvc       audit.Auditor
	logger         zerolog.Logger
}

func NewHandler(violationStore *store.PolicyViolationStore, logger zerolog.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{
		violationStore: violationStore,
		logger:         logger.With().Str("handler", "violations").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type HandlerOption func(*Handler)

func WithAuditSvc(a audit.Auditor) HandlerOption {
	return func(h *Handler) { h.auditSvc = a }
}

func WithSIMStore(s *store.SIMStore) HandlerOption {
	return func(h *Handler) { h.simStore = s }
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid violation ID")
		return
	}

	v, err := h.violationStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrViolationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
			return
		}
		h.logger.Error().Err(err).Msg("get violation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to get violation")
		return
	}

	if v.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, v)
}

type remediateRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (h *Handler) Remediate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid violation ID")
		return
	}

	var req remediateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	validActions := map[string]bool{"suspend_sim": true, "escalate": true, "dismiss": true}
	if !validActions[req.Action] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid action. Allowed: suspend_sim, escalate, dismiss",
			[]map[string]interface{}{{"field": "action", "message": "must be suspend_sim, escalate, or dismiss", "code": "invalid_enum"}})
		return
	}

	v, err := h.violationStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrViolationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
			return
		}
		h.logger.Error().Err(err).Msg("get violation for remediate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if v.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
		return
	}

	response := map[string]interface{}{"violation": v}

	switch req.Action {
	case "suspend_sim":
		if h.simStore == nil {
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "SIM store not available")
			return
		}
		reason := req.Reason
		if reason == "" {
			reason = "violation_remediation"
		}
		sim, suspendErr := h.simStore.Suspend(r.Context(), tenantID, v.SimID, &userID, &reason)
		if suspendErr != nil {
			if errors.Is(suspendErr, store.ErrSIMNotFound) {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
				return
			}
			if errors.Is(suspendErr, store.ErrInvalidStateTransition) {
				apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "SIM cannot be suspended in its current state")
				return
			}
			h.logger.Error().Err(suspendErr).Msg("suspend sim for violation remediate")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		response["sim"] = sim

		audit.Emit(r, h.logger, h.auditSvc, "violation.remediated", "policy_violation", id.String(), nil, map[string]interface{}{
			"action":          req.Action,
			"reason":          req.Reason,
			"remediated_by":   userID.String(),
			"sim_id":          v.SimID.String(),
		})

		if _, ackErr := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Reason); ackErr != nil && !errors.Is(ackErr, store.ErrAlreadyAcknowledged) {
			h.logger.Warn().Err(ackErr).Msg("acknowledge violation after suspend")
		}

	case "escalate":
		audit.Emit(r, h.logger, h.auditSvc, "violation.escalated", "policy_violation", id.String(), nil, map[string]interface{}{
			"action":        req.Action,
			"reason":        req.Reason,
			"escalated_by":  userID.String(),
		})

	case "dismiss":
		updatedV, ackErr := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Reason)
		if ackErr != nil {
			if errors.Is(ackErr, store.ErrAlreadyAcknowledged) {
				apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "Violation already acknowledged")
				return
			}
			if errors.Is(ackErr, store.ErrViolationNotFound) {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
				return
			}
			h.logger.Error().Err(ackErr).Msg("dismiss violation")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		response["violation"] = updatedV
		audit.Emit(r, h.logger, h.auditSvc, "violation.dismissed", "policy_violation", id.String(), nil, map[string]interface{}{
			"reason":       req.Reason,
			"dismissed_by": userID.String(),
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, response)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	var simID *uuid.UUID
	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			simID = &id
		}
	}

	var policyID *uuid.UUID
	if v := q.Get("policy_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			policyID = &id
		}
	}

	var ackFilter *bool
	if v := q.Get("acknowledged"); v != "" {
		b := v == "true"
		ackFilter = &b
	}

	params := store.ListViolationsParams{
		Cursor:        q.Get("cursor"),
		Limit:         limit,
		ViolationType: q.Get("violation_type"),
		Severity:      q.Get("severity"),
		SimID:         simID,
		PolicyID:      policyID,
		Acknowledged:  ackFilter,
	}

	violations, nextCursor, err := h.violationStore.List(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list violations")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to list violations")
		return
	}

	apierr.WriteList(w, http.StatusOK, violations, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) CountByType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	counts, err := h.violationStore.CountByType(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("count violations")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count violations")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, counts)
}

type acknowledgeRequest struct {
	Note string `json:"note"`
}

func (h *Handler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid violation ID")
		return
	}

	var req acknowledgeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	v, err := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Note)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyAcknowledged) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "Violation already acknowledged")
			return
		}
		if errors.Is(err, store.ErrViolationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
			return
		}
		h.logger.Error().Err(err).Msg("acknowledge violation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to acknowledge violation")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "violation.acknowledge", "policy_violation", id.String(), nil, map[string]interface{}{
		"acknowledged_by": userID.String(),
		"note":            req.Note,
	})

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"id":               v.ID,
		"acknowledged_at":  v.AcknowledgedAt,
		"acknowledged_by":  v.AcknowledgedBy,
		"note":             v.AcknowledgmentNote,
	})
}
