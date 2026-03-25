package violation

import (
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	violationStore *store.PolicyViolationStore
	logger         zerolog.Logger
}

func NewHandler(violationStore *store.PolicyViolationStore, logger zerolog.Logger) *Handler {
	return &Handler{
		violationStore: violationStore,
		logger:         logger.With().Str("handler", "violations").Logger(),
	}
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

	params := store.ListViolationsParams{
		Cursor:        q.Get("cursor"),
		Limit:         limit,
		ViolationType: q.Get("violation_type"),
		Severity:      q.Get("severity"),
		SimID:         simID,
		PolicyID:      policyID,
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
