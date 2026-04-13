package anomaly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type NotificationSender interface {
	Notify(ctx context.Context, req notification.NotifyRequest) error
}

type Handler struct {
	anomalyStore   *store.AnomalyStore
	commentStore   *store.AnomalyCommentStore
	userStore      *store.UserStore
	notifier       NotificationSender
	auditSvc       audit.Auditor
	logger         zerolog.Logger
}

type HandlerOption func(*Handler)

func WithCommentStore(cs *store.AnomalyCommentStore) HandlerOption {
	return func(h *Handler) { h.commentStore = cs }
}

func WithUserStore(us *store.UserStore) HandlerOption {
	return func(h *Handler) { h.userStore = us }
}

func WithNotifier(n NotificationSender) HandlerOption {
	return func(h *Handler) { h.notifier = n }
}

func NewHandler(anomalyStore *store.AnomalyStore, auditSvc audit.Auditor, logger zerolog.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{
		anomalyStore: anomalyStore,
		auditSvc:     auditSvc,
		logger:       logger.With().Str("component", "anomaly_handler").Logger(),
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

type anomalyDTO struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	SimID          *string         `json:"sim_id,omitempty"`
	SimICCID       string          `json:"sim_iccid,omitempty"`
	Type           string          `json:"type"`
	Severity       string          `json:"severity"`
	State          string          `json:"state"`
	Details        json.RawMessage `json:"details"`
	Source         *string         `json:"source,omitempty"`
	DetectedAt     string          `json:"detected_at"`
	AcknowledgedAt *string         `json:"acknowledged_at,omitempty"`
	ResolvedAt     *string         `json:"resolved_at,omitempty"`
}

func toAnomalyDTO(a store.Anomaly, iccid string) anomalyDTO {
	dto := anomalyDTO{
		ID:         a.ID.String(),
		TenantID:   a.TenantID.String(),
		Type:       a.Type,
		Severity:   a.Severity,
		State:      a.State,
		Details:    a.Details,
		Source:     a.Source,
		DetectedAt: a.DetectedAt.Format(time.RFC3339),
		SimICCID:   iccid,
	}
	if a.SimID != nil {
		s := a.SimID.String()
		dto.SimID = &s
	}
	if a.AcknowledgedAt != nil {
		s := a.AcknowledgedAt.Format(time.RFC3339)
		dto.AcknowledgedAt = &s
	}
	if a.ResolvedAt != nil {
		s := a.ResolvedAt.Format(time.RFC3339)
		dto.ResolvedAt = &s
	}
	return dto
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()

	limit := 50
	if v := q.Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	params := store.ListAnomalyParams{
		Cursor:   q.Get("cursor"),
		Limit:    limit,
		Type:     q.Get("type"),
		Severity: q.Get("severity"),
		State:    q.Get("state"),
	}

	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			params.SimID = &id
		} else {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format")
			return
		}
	}

	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'from' date format")
			return
		}
		params.From = &t
	}

	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid 'to' date format")
			return
		}
		params.To = &t
	}

	anomalies, nextCursor, err := h.anomalyStore.ListByTenant(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list anomalies")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]anomalyDTO, 0, len(anomalies))
	for _, a := range anomalies {
		var iccid string
		if a.SimID != nil {
			iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
		}
		dtos = append(dtos, toAnomalyDTO(a, iccid))
	}

	apierr.WriteList(w, http.StatusOK, dtos, apierr.ListMeta{
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

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}

	a, err := h.anomalyStore.GetByID(r.Context(), tenantID, id)
	if errors.Is(err, store.ErrAnomalyNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Anomaly not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get anomaly")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var iccid string
	if a.SimID != nil {
		iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
	}

	apierr.WriteSuccess(w, http.StatusOK, toAnomalyDTO(*a, iccid))
}

type updateStateRequest struct {
	State string `json:"state"`
	Note  string `json:"note,omitempty"`
}

func (h *Handler) UpdateState(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}

	var req updateStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	validStates := map[string]bool{
		"acknowledged":   true,
		"resolved":       true,
		"false_positive": true,
	}
	if !validStates[req.State] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Invalid state. Must be: acknowledged, resolved, or false_positive")
		return
	}
	if len(req.Note) > 2000 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Note must be 2000 characters or fewer")
		return
	}

	a, err := h.anomalyStore.UpdateState(r.Context(), tenantID, id, req.State)
	if errors.Is(err, store.ErrAnomalyNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Anomaly not found")
		return
	}
	if errors.Is(err, store.ErrInvalidAnomalyTransition) {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeInvalidStateTransition,
			"Invalid state transition")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("update anomaly state")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Persist the operator's note as a comment so the alert thread retains
	// ack/resolve context (AC-11). Best-effort — do not fail the state
	// transition if the comment write fails.
	if req.Note != "" && h.commentStore != nil && userID != uuid.Nil {
		tag := strings.ToUpper(req.State)
		body := fmt.Sprintf("[%s] %s", tag, req.Note)
		if _, cErr := h.commentStore.Create(r.Context(), tenantID, id, userID, body); cErr != nil {
			h.logger.Warn().Err(cErr).Str("anomaly_id", id.String()).Msg("state-transition comment write failed")
		}
	}

	audit.Emit(r, h.logger, h.auditSvc, "anomaly.update", "anomaly", id.String(), nil, map[string]string{"state": req.State})

	var iccid string
	if a.SimID != nil {
		iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
	}

	apierr.WriteSuccess(w, http.StatusOK, toAnomalyDTO(*a, iccid))
}

type commentDTO struct {
	ID        string `json:"id"`
	AnomalyID string `json:"anomaly_id"`
	UserID    string `json:"user_id"`
	UserEmail string `json:"user_email"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

func toCommentDTO(c store.AnomalyComment) commentDTO {
	return commentDTO{
		ID:        c.ID.String(),
		AnomalyID: c.AnomalyID.String(),
		UserID:    c.UserID.String(),
		UserEmail: c.UserEmail,
		Body:      c.Body,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
}

func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	if h.commentStore == nil {
		apierr.WriteError(w, http.StatusNotImplemented, apierr.CodeInternalError, "Comment store not configured")
		return
	}
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}
	comments, err := h.commentStore.ListByAnomaly(r.Context(), tenantID, id)
	if err != nil {
		h.logger.Error().Err(err).Msg("list anomaly comments")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	dtos := make([]commentDTO, 0, len(comments))
	for _, c := range comments {
		dtos = append(dtos, toCommentDTO(c))
	}
	apierr.WriteSuccess(w, http.StatusOK, dtos)
}

type addCommentRequest struct {
	Body string `json:"body"`
}

func (h *Handler) AddComment(w http.ResponseWriter, r *http.Request) {
	if h.commentStore == nil {
		apierr.WriteError(w, http.StatusNotImplemented, apierr.CodeInternalError, "Comment store not configured")
		return
	}
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}
	var req addCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}
	if len(req.Body) < 1 || len(req.Body) > 2000 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Body must be between 1 and 2000 characters")
		return
	}
	c, err := h.commentStore.Create(r.Context(), tenantID, id, userID, req.Body)
	if err != nil {
		h.logger.Error().Err(err).Msg("add anomaly comment")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	audit.Emit(r, h.logger, h.auditSvc, "anomaly.comment", "anomaly", id.String(), nil, map[string]string{"comment_id": c.ID.String()})
	apierr.WriteSuccess(w, http.StatusCreated, toCommentDTO(*c))
}

type escalateRequest struct {
	Note         string  `json:"note"`
	OnCallUserID *string `json:"on_call_user_id,omitempty"`
}

func (h *Handler) Escalate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid anomaly ID")
		return
	}
	var req escalateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}
	if len(req.Note) > 500 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Note must be 500 characters or fewer")
		return
	}

	a, err := h.anomalyStore.GetByID(r.Context(), tenantID, id)
	if errors.Is(err, store.ErrAnomalyNotFound) {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Anomaly not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get anomaly for escalate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if a.State == "resolved" || a.State == "false_positive" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("Cannot escalate anomaly in state %q", a.State))
		return
	}

	var notifID string
	if h.notifier != nil {
		notifErr := h.notifier.Notify(r.Context(), notification.NotifyRequest{
			TenantID:  tenantID,
			EventType: notification.EventAnomalyDetected,
			ScopeType: notification.ScopeSystem,
			Title:     fmt.Sprintf("[ESCALATED] %s anomaly", a.Type),
			Body:      fmt.Sprintf("Anomaly %s escalated. Note: %s", a.ID, req.Note),
			Severity:  a.Severity,
		})
		if notifErr != nil {
			h.logger.Warn().Err(notifErr).Str("anomaly_id", id.String()).Msg("escalation notification failed")
		}
		notifID = fmt.Sprintf("notif-%s", id.String())
	}

	if h.commentStore != nil {
		commentBody := fmt.Sprintf("[ESCALATED] %s", req.Note)
		if req.Note == "" {
			commentBody = "[ESCALATED]"
		}
		_, _ = h.commentStore.Create(r.Context(), tenantID, id, userID, commentBody)
	}

	audit.Emit(r, h.logger, h.auditSvc, "anomaly.escalate", "anomaly", id.String(), nil, map[string]string{"note": req.Note})

	var iccid string
	if a.SimID != nil {
		iccid, _ = h.anomalyStore.GetSimICCID(r.Context(), *a.SimID)
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"anomaly":         toAnomalyDTO(*a, iccid),
		"notification_id": notifID,
	})
}
