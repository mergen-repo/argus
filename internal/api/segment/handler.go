package segment

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	segments *store.SegmentStore
	logger   zerolog.Logger
}

func NewHandler(segments *store.SegmentStore, logger zerolog.Logger) *Handler {
	return &Handler{
		segments: segments,
		logger:   logger,
	}
}

type segmentDTO struct {
	ID               uuid.UUID       `json:"id"`
	TenantID         uuid.UUID       `json:"tenant_id"`
	Name             string          `json:"name"`
	FilterDefinition json.RawMessage `json:"filter_definition"`
	CreatedBy        *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt        string          `json:"created_at"`
}

type createSegmentRequest struct {
	Name             string          `json:"name"`
	FilterDefinition json.RawMessage `json:"filter_definition"`
}

type countDTO struct {
	SegmentID uuid.UUID `json:"segment_id"`
	Count     int64     `json:"count"`
}

type summaryDTO struct {
	SegmentID uuid.UUID        `json:"segment_id"`
	Total     int64            `json:"total"`
	ByState   map[string]int64 `json:"by_state"`
}

const timeFmt = "2006-01-02T15:04:05Z07:00"

func toSegmentDTO(s *store.SimSegment) segmentDTO {
	filterDef := s.FilterDefinition
	if filterDef == nil {
		filterDef = json.RawMessage(`{}`)
	}
	return segmentDTO{
		ID:               s.ID,
		TenantID:         s.TenantID,
		Name:             s.Name,
		FilterDefinition: filterDef,
		CreatedBy:        s.CreatedBy,
		CreatedAt:        s.CreatedAt.Format(timeFmt),
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")

	limit := 20
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	results, nextCursor, err := h.segments.List(r.Context(), cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list segments")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]segmentDTO, 0, len(results))
	for i := range results {
		items = append(items, toSegmentDTO(&results[i]))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createSegmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]interface{}
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]interface{}{"field": "name", "message": "Name is required", "code": "required"})
	}
	if req.FilterDefinition == nil || string(req.FilterDefinition) == "" {
		validationErrors = append(validationErrors, map[string]interface{}{"field": "filter_definition", "message": "Filter definition is required", "code": "required"})
	} else {
		var fd map[string]interface{}
		if err := json.Unmarshal(req.FilterDefinition, &fd); err != nil {
			validationErrors = append(validationErrors, map[string]interface{}{"field": "filter_definition", "message": "Filter definition must be a valid JSON object", "code": "format"})
		}
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	seg, err := h.segments.Create(r.Context(), store.CreateSegmentParams{
		Name:             req.Name,
		FilterDefinition: req.FilterDefinition,
	})
	if err != nil {
		if errors.Is(err, store.ErrSegmentNameExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"A segment with this name already exists",
				[]map[string]interface{}{{"field": "name", "value": req.Name}},
			)
			return
		}
		h.logger.Error().Err(err).Msg("create segment")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusCreated, toSegmentDTO(seg))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid segment ID")
		return
	}

	err = h.segments.Delete(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("delete segment")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Count(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid segment ID")
		return
	}

	count, err := h.segments.CountMatchingSIMs(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("count segment sims")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, countDTO{
		SegmentID: id,
		Count:     count,
	})
}

func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid segment ID")
		return
	}

	seg, err := h.segments.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("get segment")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toSegmentDTO(seg))
}

func (h *Handler) StateSummary(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid segment ID")
		return
	}

	byState, total, err := h.segments.StateSummary(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Segment not found")
			return
		}
		h.logger.Error().Err(err).Msg("segment state summary")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, summaryDTO{
		SegmentID: id,
		Total:     total,
		ByState:   byState,
	})
}
