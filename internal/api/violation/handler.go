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
