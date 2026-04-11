package sla

import (
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
	store  *store.SLAReportStore
	logger zerolog.Logger
}

func NewHandler(s *store.SLAReportStore, logger zerolog.Logger) *Handler {
	return &Handler{
		store:  s,
		logger: logger.With().Str("component", "sla_handler").Logger(),
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'from' parameter: must be RFC3339")
			return
		}
		from = t.UTC()
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'to' parameter: must be RFC3339")
			return
		}
		to = t.UTC()
	}

	if from.After(to) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "'from' must be before 'to'")
		return
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'operator_id' parameter")
			return
		}
		operatorID = &id
	}

	cursor := q.Get("cursor")

	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid 'limit' parameter")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	rows, nextCursor, err := h.store.ListByTenant(r.Context(), tenantID, from, to, operatorID, cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list sla reports")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to list SLA reports")
		return
	}

	apierr.WriteList(w, http.StatusOK, rows, apierr.ListMeta{
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

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid SLA report ID")
		return
	}

	row, err := h.store.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if err == store.ErrSLAReportNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SLA report not found")
			return
		}
		h.logger.Error().Err(err).Str("id", id.String()).Msg("get sla report")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to get SLA report")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, row)
}
