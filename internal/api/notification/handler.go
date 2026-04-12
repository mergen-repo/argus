package notification

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
	notifStore  *store.NotificationStore
	configStore *store.NotificationConfigStore
	logger      zerolog.Logger
}

func NewHandler(notifStore *store.NotificationStore, configStore *store.NotificationConfigStore, logger zerolog.Logger) *Handler {
	return &Handler{
		notifStore:  notifStore,
		configStore: configStore,
		logger:      logger.With().Str("component", "notification_handler").Logger(),
	}
}

type notificationDTO struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Title        string   `json:"title"`
	Message      string   `json:"message"`
	Scope        string   `json:"scope"`
	ScopeRefID   *string  `json:"scope_ref_id,omitempty"`
	Severity     string   `json:"severity"`
	ChannelsSent []string `json:"channels_sent"`
	Read         bool     `json:"read"`
	ReadAt       *string  `json:"read_at,omitempty"`
	RetryCount   int      `json:"retry_count"`
	CreatedAt    string   `json:"created_at"`
}

func toNotificationDTO(n store.NotificationRow) notificationDTO {
	dto := notificationDTO{
		ID:           n.ID.String(),
		Type:         n.EventType,
		Title:        n.Title,
		Message:      n.Body,
		Scope:        n.ScopeType,
		Severity:     n.Severity,
		ChannelsSent: n.ChannelsSent,
		Read:         n.State == "read",
		RetryCount:   n.RetryCount,
		CreatedAt:    n.CreatedAt.Format(time.RFC3339),
	}
	if n.ScopeRefID != nil {
		s := n.ScopeRefID.String()
		dto.ScopeRefID = &s
	}
	if n.ReadAt != nil {
		s := n.ReadAt.Format(time.RFC3339)
		dto.ReadAt = &s
	}
	if dto.ChannelsSent == nil {
		dto.ChannelsSent = []string{}
	}
	return dto
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "User context required")
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	unreadOnly := false
	if q.Get("unread_only") == "true" {
		unreadOnly = true
	}

	params := store.ListNotificationParams{
		Cursor:     q.Get("cursor"),
		Limit:      limit,
		UnreadOnly: unreadOnly,
	}

	notifications, nextCursor, err := h.notifStore.ListByUser(r.Context(), tenantID, userID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list notifications")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	unreadCount, _ := h.notifStore.UnreadCount(r.Context(), tenantID, userID)

	dtos := make([]notificationDTO, 0, len(notifications))
	for _, n := range notifications {
		dtos = append(dtos, toNotificationDTO(n))
	}

	type listMeta struct {
		Cursor      string `json:"cursor,omitempty"`
		HasMore     bool   `json:"has_more"`
		Limit       int    `json:"limit"`
		UnreadCount int64  `json:"unread_count"`
	}

	type listResponse struct {
		Status string            `json:"status"`
		Data   []notificationDTO `json:"data"`
		Meta   listMeta          `json:"meta"`
	}

	apierr.WriteJSON(w, http.StatusOK, listResponse{
		Status: "success",
		Data:   dtos,
		Meta: listMeta{
			Cursor:      nextCursor,
			HasMore:     nextCursor != "",
			Limit:       limit,
			UnreadCount: unreadCount,
		},
	})
}

func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "User context required")
		return
	}

	count, err := h.notifStore.UnreadCount(r.Context(), tenantID, userID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get unread count")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"count": count,
	})
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid notification ID")
		return
	}

	n, err := h.notifStore.MarkRead(r.Context(), tenantID, id)
	if errors.Is(err, store.ErrNotificationNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Notification not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("mark notification read")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"id":   n.ID.String(),
		"read": true,
	})
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "User context required")
		return
	}

	count, err := h.notifStore.MarkAllRead(r.Context(), tenantID, userID)
	if err != nil {
		h.logger.Error().Err(err).Msg("mark all notifications read")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"updated_count": count,
	})
}

func (h *Handler) GetConfigs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "User context required")
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	configs, nextCursor, err := h.configStore.ListByUser(r.Context(), tenantID, userID, q.Get("cursor"), limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("get notification configs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	type configDTO struct {
		ID             string          `json:"id"`
		EventType      string          `json:"event_type"`
		ScopeType      string          `json:"scope_type"`
		ScopeRefID     *string         `json:"scope_ref_id,omitempty"`
		Channels       json.RawMessage `json:"channels"`
		ThresholdType  *string         `json:"threshold_type,omitempty"`
		ThresholdValue *float64        `json:"threshold_value,omitempty"`
		Enabled        bool            `json:"enabled"`
		UpdatedAt      string          `json:"updated_at"`
	}

	dtos := make([]configDTO, 0, len(configs))
	for _, c := range configs {
		dto := configDTO{
			ID:             c.ID.String(),
			EventType:      c.EventType,
			ScopeType:      c.ScopeType,
			Channels:       c.Channels,
			ThresholdType:  c.ThresholdType,
			ThresholdValue: c.ThresholdValue,
			Enabled:        c.Enabled,
			UpdatedAt:      c.UpdatedAt.Format(time.RFC3339),
		}
		if c.ScopeRefID != nil {
			s := c.ScopeRefID.String()
			dto.ScopeRefID = &s
		}
		dtos = append(dtos, dto)
	}

	apierr.WriteList(w, http.StatusOK, dtos, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

type updateConfigRequest struct {
	Configs []configEntry `json:"configs"`
}

type configEntry struct {
	EventType      string          `json:"event_type"`
	ScopeType      string          `json:"scope_type"`
	ScopeRefID     *string         `json:"scope_ref_id,omitempty"`
	Channels       json.RawMessage `json:"channels"`
	ThresholdType  *string         `json:"threshold_type,omitempty"`
	ThresholdValue *float64        `json:"threshold_value,omitempty"`
	Enabled        bool            `json:"enabled"`
}

func (h *Handler) UpdateConfigs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || userID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "User context required")
		return
	}

	var req updateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	if len(req.Configs) == 0 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "At least one config entry required")
		return
	}

	validEventTypes := map[string]bool{
		"operator.down": true, "operator.recovered": true, "sim.state_changed": true,
		"job.completed": true, "job.failed": true, "alert.new": true,
		"sla.violation": true, "policy.rollout_completed": true,
		"quota.warning": true, "quota.exceeded": true, "anomaly.detected": true,
	}

	validScopes := map[string]bool{
		"system": true, "sim": true, "apn": true, "operator": true,
	}

	for _, c := range req.Configs {
		if !validEventTypes[c.EventType] {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"Invalid event_type: "+c.EventType)
			return
		}
		if !validScopes[c.ScopeType] {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"Invalid scope_type: "+c.ScopeType)
			return
		}
	}

	for _, c := range req.Configs {
		var scopeRefID *uuid.UUID
		if c.ScopeRefID != nil {
			id, err := uuid.Parse(*c.ScopeRefID)
			if err != nil {
				apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid scope_ref_id")
				return
			}
			scopeRefID = &id
		}

		channels := c.Channels
		if channels == nil {
			channels = json.RawMessage(`{}`)
		}

		_, err := h.configStore.Upsert(r.Context(), store.UpsertNotificationConfigParams{
			TenantID:       tenantID,
			UserID:         &userID,
			EventType:      c.EventType,
			ScopeType:      c.ScopeType,
			ScopeRefID:     scopeRefID,
			Channels:       channels,
			ThresholdType:  c.ThresholdType,
			ThresholdValue: c.ThresholdValue,
			Enabled:        c.Enabled,
		})
		if err != nil {
			h.logger.Error().Err(err).Str("event_type", c.EventType).Msg("upsert notification config")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"updated_at": time.Now().Format(time.RFC3339),
	})
}
