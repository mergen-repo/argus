package undo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	undopkg "github.com/btopcu/argus/internal/undo"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type ExecutorFn func(ctx context.Context, tenantID, userID uuid.UUID, payload json.RawMessage) error

type Handler struct {
	registry  *undopkg.Registry
	auditSvc  audit.Auditor
	logger    zerolog.Logger
	executors map[string]ExecutorFn
}

func NewHandler(registry *undopkg.Registry, auditSvc audit.Auditor, logger zerolog.Logger) *Handler {
	return &Handler{
		registry:  registry,
		auditSvc:  auditSvc,
		logger:    logger.With().Str("component", "undo_handler").Logger(),
		executors: make(map[string]ExecutorFn),
	}
}

func (h *Handler) RegisterExecutor(action string, fn ExecutorFn) {
	h.executors[action] = fn
}

func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	actionID := chi.URLParam(r, "action_id")
	if actionID == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "action_id required")
		return
	}

	entry, err := h.registry.Consume(r.Context(), tenantID, actionID)
	if err != nil {
		if errors.Is(err, undopkg.ErrExpired) {
			w.WriteHeader(http.StatusGone)
			return
		}
		if errors.Is(err, undopkg.ErrTenantDenied) {
			apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "undo not available")
			return
		}
		h.logger.Error().Err(err).Str("action_id", actionID).Msg("consume undo entry")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "failed to process undo")
		return
	}

	executor, exists := h.executors[entry.Action]
	if !exists {
		h.logger.Warn().Str("action", entry.Action).Msg("no executor registered for undo action")
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInternalError, "unsupported undo action")
		return
	}

	if err := executor(r.Context(), tenantID, userID, entry.Payload); err != nil {
		h.logger.Error().Err(err).Str("action", entry.Action).Msg("execute undo")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "undo failed")
		return
	}

	afterData, _ := json.Marshal(map[string]interface{}{
		"action":        entry.Action,
		"original_user": entry.UserID,
		"issued_at":     entry.IssuedAt.Format(time.RFC3339),
	})
	_, _ = h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:   tenantID,
		UserID:     &userID,
		Action:     "undo." + entry.Action,
		EntityType: "undo",
		EntityID:   actionID,
		AfterData:  afterData,
	})

	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"status": "undone", "action": entry.Action})
}
