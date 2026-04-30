package announcement

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	store    *store.AnnouncementStore
	auditSvc audit.Auditor
	logger   zerolog.Logger
}

func NewHandler(s *store.AnnouncementStore, auditSvc audit.Auditor, logger zerolog.Logger) *Handler {
	return &Handler{
		store:    s,
		auditSvc: auditSvc,
		logger:   logger.With().Str("component", "announcement_handler").Logger(),
	}
}

func (h *Handler) GetActive(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	list, err := h.store.GetActive(r.Context(), tenantID, userID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get active announcements")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to fetch announcements")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, list)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}

	list, err := h.store.List(r.Context(), page, 20)
	if err != nil {
		h.logger.Error().Err(err).Msg("list announcements")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to list announcements")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, list)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	var body struct {
		Title       string    `json:"title"`
		Body        string    `json:"body"`
		Type        string    `json:"type"`
		Target      string    `json:"target"`
		StartsAt    time.Time `json:"starts_at"`
		EndsAt      time.Time `json:"ends_at"`
		Dismissible *bool     `json:"dismissible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}
	if body.Title == "" || body.Body == "" || body.Type == "" || body.Target == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "title, body, type, target are required")
		return
	}
	if body.Type != "info" && body.Type != "warning" && body.Type != "critical" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "type must be info, warning, or critical")
		return
	}

	dismissible := true
	if body.Dismissible != nil {
		dismissible = *body.Dismissible
	}

	a, err := h.store.Create(r.Context(), store.CreateAnnouncementParams{
		Title:       body.Title,
		Body:        body.Body,
		Type:        body.Type,
		Target:      body.Target,
		StartsAt:    body.StartsAt,
		EndsAt:      body.EndsAt,
		Dismissible: dismissible,
		CreatedBy:   &userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create announcement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to create announcement")
		return
	}

	afterData, _ := json.Marshal(a)
	_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:   uuid.Nil,
		UserID:     &userID,
		Action:     "announcement.create",
		EntityType: "announcement",
		EntityID:   a.ID.String(),
		AfterData:  afterData,
	})

	apierr.WriteSuccess(w, http.StatusCreated, a)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid id")
		return
	}

	var body struct {
		Title       *string    `json:"title"`
		Body        *string    `json:"body"`
		Type        *string    `json:"type"`
		Target      *string    `json:"target"`
		StartsAt    *time.Time `json:"starts_at"`
		EndsAt      *time.Time `json:"ends_at"`
		Dismissible *bool      `json:"dismissible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid request body")
		return
	}

	if body.Type != nil && *body.Type != "info" && *body.Type != "warning" && *body.Type != "critical" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "type must be info, warning, or critical")
		return
	}

	a, err := h.store.Update(r.Context(), id, store.UpdateAnnouncementParams{
		Title:       body.Title,
		Body:        body.Body,
		Type:        body.Type,
		Target:      body.Target,
		StartsAt:    body.StartsAt,
		EndsAt:      body.EndsAt,
		Dismissible: body.Dismissible,
	})
	if err != nil {
		if errors.Is(err, store.ErrAnnouncementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "announcement not found")
			return
		}
		h.logger.Error().Err(err).Msg("update announcement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to update announcement")
		return
	}

	afterData, _ := json.Marshal(a)
	_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:   uuid.Nil,
		UserID:     &userID,
		Action:     "announcement.update",
		EntityType: "announcement",
		EntityID:   a.ID.String(),
		AfterData:  afterData,
	})

	apierr.WriteSuccess(w, http.StatusOK, a)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid id")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrAnnouncementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "announcement not found")
			return
		}
		h.logger.Error().Err(err).Msg("delete announcement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to delete announcement")
		return
	}

	beforeData, _ := json.Marshal(map[string]string{"id": id.String()})
	_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:   uuid.Nil,
		UserID:     &userID,
		Action:     "announcement.delete",
		EntityType: "announcement",
		EntityID:   id.String(),
		BeforeData: beforeData,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Dismiss(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "auth context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "invalid id")
		return
	}

	if err := h.store.Dismiss(r.Context(), id, userID); err != nil {
		h.logger.Error().Err(err).Msg("dismiss announcement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to dismiss")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
