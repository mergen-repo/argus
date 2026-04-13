package webhooks

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	configStore   *store.WebhookConfigStore
	deliveryStore *store.WebhookDeliveryStore
	dispatcher    *notification.Dispatcher
	logger        zerolog.Logger
}

func NewHandler(
	configStore *store.WebhookConfigStore,
	deliveryStore *store.WebhookDeliveryStore,
	dispatcher *notification.Dispatcher,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		configStore:   configStore,
		deliveryStore: deliveryStore,
		dispatcher:    dispatcher,
		logger:        logger.With().Str("component", "webhooks_handler").Logger(),
	}
}

type createWebhookRequest struct {
	URL        string   `json:"url"`
	Secret     string   `json:"secret"`
	EventTypes []string `json:"event_types"`
	Enabled    *bool    `json:"enabled"`
}

type patchWebhookRequest struct {
	URL        *string   `json:"url"`
	Secret     *string   `json:"secret"`
	EventTypes *[]string `json:"event_types"`
	Enabled    *bool     `json:"enabled"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	configs, nextCursor, err := h.configStore.List(r.Context(), tenantID, cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list webhook configs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if configs == nil {
		configs = []*store.WebhookConfig{}
	}

	apierr.WriteList(w, http.StatusOK, configs, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req createWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.URL == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "url is required")
		return
	}
	if req.Secret == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "secret is required")
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	eventTypes := req.EventTypes
	if eventTypes == nil {
		eventTypes = []string{}
	}

	cfg := &store.WebhookConfig{
		TenantID:   tenantID,
		URL:        req.URL,
		Secret:     req.Secret,
		EventTypes: eventTypes,
		Enabled:    enabled,
	}

	created, err := h.configStore.Create(r.Context(), cfg)
	if err != nil {
		h.logger.Error().Err(err).Msg("create webhook config")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	created.Secret = ""
	apierr.WriteSuccess(w, http.StatusCreated, created)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid webhook ID format")
		return
	}

	existing, err := h.configStore.GetForAPI(r.Context(), id)
	if err != nil {
		if err == store.ErrWebhookConfigNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
			return
		}
		h.logger.Error().Err(err).Msg("get webhook config for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
		return
	}

	var req patchWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	patch := store.WebhookConfigPatch{
		URL:        req.URL,
		Secret:     req.Secret,
		EventTypes: req.EventTypes,
		Enabled:    req.Enabled,
	}

	if err := h.configStore.Update(r.Context(), id, patch); err != nil {
		if err == store.ErrWebhookConfigNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
			return
		}
		h.logger.Error().Err(err).Msg("update webhook config")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	updated, err := h.configStore.GetForAPI(r.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Msg("get updated webhook config")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, updated)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid webhook ID format")
		return
	}

	existing, err := h.configStore.GetForAPI(r.Context(), id)
	if err != nil {
		if err == store.ErrWebhookConfigNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
			return
		}
		h.logger.Error().Err(err).Msg("get webhook config for delete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if existing.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
		return
	}

	if err := h.configStore.Delete(r.Context(), id); err != nil {
		if err == store.ErrWebhookConfigNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
			return
		}
		h.logger.Error().Err(err).Msg("delete webhook config")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListDeliveries(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	configID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid webhook ID format")
		return
	}

	cfg, err := h.configStore.GetForAPI(r.Context(), configID)
	if err != nil {
		if err == store.ErrWebhookConfigNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
			return
		}
		h.logger.Error().Err(err).Msg("get webhook config for deliveries")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if cfg.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
		return
	}

	statusFilter := r.URL.Query().Get("status")
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	deliveries, nextCursor, err := h.deliveryStore.ListByConfig(r.Context(), configID, cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list webhook deliveries")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if statusFilter != "" {
		filtered := make([]*store.WebhookDelivery, 0, len(deliveries))
		for _, d := range deliveries {
			if d.FinalState == statusFilter {
				filtered = append(filtered, d)
			}
		}
		deliveries = filtered
		nextCursor = ""
	}

	if deliveries == nil {
		deliveries = []*store.WebhookDelivery{}
	}

	apierr.WriteList(w, http.StatusOK, deliveries, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *Handler) RetryDelivery(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	configID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid webhook ID format")
		return
	}

	deliveryIDStr := chi.URLParam(r, "delivery_id")
	deliveryID, err := uuid.Parse(deliveryIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid delivery ID format")
		return
	}

	cfg, err := h.configStore.GetForAPI(r.Context(), configID)
	if err != nil {
		if err == store.ErrWebhookConfigNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
			return
		}
		h.logger.Error().Err(err).Msg("get webhook config for retry")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if cfg.TenantID != tenantID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Webhook config not found")
		return
	}

	original, err := h.deliveryStore.GetByID(r.Context(), deliveryID)
	if err != nil {
		if err == store.ErrWebhookDeliveryNotFound {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Delivery not found")
			return
		}
		h.logger.Error().Err(err).Msg("get delivery for retry")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if original.ConfigID != configID {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Delivery not found")
		return
	}

	newDelivery, err := h.dispatcher.ResendDelivery(r.Context(), deliveryID)
	if err != nil {
		h.logger.Error().Err(err).Msg("resend delivery")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusAccepted, map[string]interface{}{
		"delivery_id": newDelivery.ID,
		"final_state": newDelivery.FinalState,
	})
}
