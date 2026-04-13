package user

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) ListViews(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := h.tenantUser(w, r)
	if !ok {
		return
	}

	page := r.URL.Query().Get("page")
	if page == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "page query param required")
		return
	}
	if !store.IsValidViewPage(page) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "invalid page value")
		return
	}

	views, err := h.viewStore.List(r.Context(), userID, tenantID, page)
	if err != nil {
		h.logger.Error().Err(err).Msg("list user views")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to list views")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, views)
}

func (h *Handler) CreateView(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := h.tenantUser(w, r)
	if !ok {
		return
	}

	var body struct {
		Page        string          `json:"page"`
		Name        string          `json:"name"`
		FiltersJSON json.RawMessage `json:"filters_json"`
		ColumnsJSON json.RawMessage `json:"columns_json"`
		SortJSON    json.RawMessage `json:"sort_json"`
		IsDefault   bool            `json:"is_default"`
		Shared      bool            `json:"shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}
	if body.Page == "" || body.Name == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "page and name are required")
		return
	}
	if !store.IsValidViewPage(body.Page) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "invalid page value")
		return
	}

	view, err := h.viewStore.Create(r.Context(), store.CreateUserViewParams{
		TenantID:    tenantID,
		UserID:      userID,
		Page:        body.Page,
		Name:        body.Name,
		FiltersJSON: body.FiltersJSON,
		ColumnsJSON: body.ColumnsJSON,
		SortJSON:    body.SortJSON,
		IsDefault:   body.IsDefault,
		Shared:      body.Shared,
	})
	if err != nil {
		if errors.Is(err, store.ErrUserViewLimitReached) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, err.Error())
			return
		}
		h.logger.Error().Err(err).Msg("create user view")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to create view")
		return
	}

	apierr.WriteSuccess(w, http.StatusCreated, view)
}

func (h *Handler) UpdateView(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := h.tenantUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid view id")
		return
	}

	var body struct {
		Name        *string         `json:"name"`
		FiltersJSON json.RawMessage `json:"filters_json"`
		ColumnsJSON json.RawMessage `json:"columns_json"`
		SortJSON    json.RawMessage `json:"sort_json"`
		IsDefault   *bool           `json:"is_default"`
		Shared      *bool           `json:"shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}

	view, err := h.viewStore.Update(r.Context(), id, userID, store.UpdateUserViewParams{
		Name:        body.Name,
		FiltersJSON: body.FiltersJSON,
		ColumnsJSON: body.ColumnsJSON,
		SortJSON:    body.SortJSON,
		IsDefault:   body.IsDefault,
		Shared:      body.Shared,
	})
	if err != nil {
		if errors.Is(err, store.ErrUserViewNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "view not found")
			return
		}
		h.logger.Error().Err(err).Msg("update user view")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to update view")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, view)
}

func (h *Handler) DeleteView(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := h.tenantUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid view id")
		return
	}

	if err := h.viewStore.Delete(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrUserViewNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "view not found")
			return
		}
		h.logger.Error().Err(err).Msg("delete user view")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to delete view")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SetDefaultView(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := h.tenantUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid view id")
		return
	}

	page := r.URL.Query().Get("page")
	if page == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "page query param required")
		return
	}

	if err := h.viewStore.SetDefault(r.Context(), id, userID, page); err != nil {
		if errors.Is(err, store.ErrUserViewNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "view not found")
			return
		}
		h.logger.Error().Err(err).Msg("set default user view")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to set default view")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) tenantUser(w http.ResponseWriter, r *http.Request) (tenantID uuid.UUID, userID uuid.UUID, ok bool) {
	tenantID, tOk := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	userID, uOk := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !tOk || tenantID == uuid.Nil || !uOk || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "auth context required")
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, userID, true
}
