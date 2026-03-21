package ota

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	jobtypes "github.com/btopcu/argus/internal/job"
	otapkg "github.com/btopcu/argus/internal/ota"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	CodeOTARateLimit = "OTA_RATE_LIMIT"
)

type Handler struct {
	otaStore    *store.OTAStore
	simStore    *store.SIMStore
	jobStore    *store.JobStore
	eventBus    *bus.EventBus
	rateLimiter *otapkg.RateLimiter
	auditSvc    audit.Auditor
	logger      zerolog.Logger
}

func NewHandler(
	otaStore *store.OTAStore,
	simStore *store.SIMStore,
	jobStore *store.JobStore,
	eventBus *bus.EventBus,
	rateLimiter *otapkg.RateLimiter,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		otaStore:    otaStore,
		simStore:    simStore,
		jobStore:    jobStore,
		eventBus:    eventBus,
		rateLimiter: rateLimiter,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "ota_handler").Logger(),
	}
}

type sendOTARequest struct {
	CommandType  string          `json:"command_type"`
	Channel      string          `json:"channel"`
	SecurityMode string          `json:"security_mode"`
	Payload      json.RawMessage `json:"payload"`
	MaxRetries   int             `json:"max_retries"`
}

type bulkOTARequest struct {
	SimIDs       []string        `json:"sim_ids"`
	SegmentID    *string         `json:"segment_id,omitempty"`
	CommandType  string          `json:"command_type"`
	Channel      string          `json:"channel"`
	SecurityMode string          `json:"security_mode"`
	Payload      json.RawMessage `json:"payload"`
	MaxRetries   int             `json:"max_retries"`
}

type otaCommandResponse struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	SimID        string          `json:"sim_id"`
	CommandType  string          `json:"command_type"`
	Channel      string          `json:"channel"`
	Status       string          `json:"status"`
	SecurityMode string          `json:"security_mode"`
	Payload      json.RawMessage `json:"payload"`
	ResponseData json.RawMessage `json:"response_data,omitempty"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	JobID        *string         `json:"job_id,omitempty"`
	RetryCount   int             `json:"retry_count"`
	MaxRetries   int             `json:"max_retries"`
	CreatedBy    *string         `json:"created_by,omitempty"`
	SentAt       *string         `json:"sent_at,omitempty"`
	DeliveredAt  *string         `json:"delivered_at,omitempty"`
	ExecutedAt   *string         `json:"executed_at,omitempty"`
	CompletedAt  *string         `json:"completed_at,omitempty"`
	CreatedAt    string          `json:"created_at"`
}

func toOTAResponse(cmd *store.OTACommand) otaCommandResponse {
	resp := otaCommandResponse{
		ID:           cmd.ID.String(),
		TenantID:     cmd.TenantID.String(),
		SimID:        cmd.SimID.String(),
		CommandType:  cmd.CommandType,
		Channel:      cmd.Channel,
		Status:       cmd.Status,
		SecurityMode: cmd.SecurityMode,
		Payload:      cmd.Payload,
		ResponseData: cmd.ResponseData,
		ErrorMessage: cmd.ErrorMessage,
		RetryCount:   cmd.RetryCount,
		MaxRetries:   cmd.MaxRetries,
		CreatedAt:    cmd.CreatedAt.Format(time.RFC3339Nano),
	}
	if cmd.JobID != nil {
		v := cmd.JobID.String()
		resp.JobID = &v
	}
	if cmd.CreatedBy != nil {
		v := cmd.CreatedBy.String()
		resp.CreatedBy = &v
	}
	if cmd.SentAt != nil {
		v := cmd.SentAt.Format(time.RFC3339Nano)
		resp.SentAt = &v
	}
	if cmd.DeliveredAt != nil {
		v := cmd.DeliveredAt.Format(time.RFC3339Nano)
		resp.DeliveredAt = &v
	}
	if cmd.ExecutedAt != nil {
		v := cmd.ExecutedAt.Format(time.RFC3339Nano)
		resp.ExecutedAt = &v
	}
	if cmd.CompletedAt != nil {
		v := cmd.CompletedAt.Format(time.RFC3339Nano)
		resp.CompletedAt = &v
	}
	return resp
}

func (h *Handler) SendToSIM(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	simIDStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	if _, err := h.simStore.GetByID(r.Context(), tenantID, simID); err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simIDStr).Msg("get sim for ota")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var req sendOTARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if validationErr := h.validateOTARequest(req); validationErr != "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, validationErr)
		return
	}

	if h.rateLimiter != nil {
		allowed, remaining, err := h.rateLimiter.Allow(r.Context(), simID)
		if err != nil {
			h.logger.Error().Err(err).Msg("rate limit check")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		if !allowed {
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(0))
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(h.rateLimiter.MaxPerHour()))
			apierr.WriteError(w, http.StatusTooManyRequests, CodeOTARateLimit,
				"OTA rate limit exceeded for this SIM",
				map[string]interface{}{
					"sim_id":    simID.String(),
					"limit":     h.rateLimiter.MaxPerHour(),
					"remaining": remaining,
				})
			return
		}
	}

	apduData, err := otapkg.BuildAPDU(otapkg.CommandType(req.CommandType), req.Payload)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, err.Error())
		return
	}

	userID := userIDFromCtx(r)

	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	securityMode := req.SecurityMode
	if securityMode == "" {
		securityMode = string(otapkg.SecurityNone)
	}
	channel := req.Channel
	if channel == "" {
		channel = string(otapkg.ChannelSMSPP)
	}

	cmd, err := h.otaStore.Create(r.Context(), tenantID, store.CreateOTACommandParams{
		SimID:        simID,
		CommandType:  req.CommandType,
		Channel:      channel,
		SecurityMode: securityMode,
		APDUData:     apduData,
		Payload:      req.Payload,
		MaxRetries:   maxRetries,
		CreatedBy:    userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create ota command")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "ota.send", cmd.ID.String(), nil, cmd, userID)

	apierr.WriteSuccess(w, http.StatusCreated, toOTAResponse(cmd))
}

func (h *Handler) BulkSend(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req bulkOTARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if len(req.SimIDs) == 0 && req.SegmentID == nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"Either sim_ids or segment_id is required")
		return
	}

	otaReq := sendOTARequest{
		CommandType:  req.CommandType,
		Channel:      req.Channel,
		SecurityMode: req.SecurityMode,
		Payload:      req.Payload,
	}
	if validationErr := h.validateOTARequest(otaReq); validationErr != "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, validationErr)
		return
	}

	userID := userIDFromCtx(r)

	maxRetries := req.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	payload, _ := json.Marshal(otapkg.BulkOTAPayload{
		SimIDs:       req.SimIDs,
		SegmentID:    req.SegmentID,
		CommandType:  otapkg.CommandType(req.CommandType),
		Channel:      otapkg.DeliveryChannel(req.Channel),
		SecurityMode: otapkg.SecurityMode(req.SecurityMode),
		Payload:      req.Payload,
		MaxRetries:   maxRetries,
	})

	job, err := h.jobStore.Create(r.Context(), store.CreateJobParams{
		Type:       jobtypes.JobTypeOTACommand,
		Priority:   5,
		Payload:    payload,
		TotalItems: len(req.SimIDs),
		CreatedBy:  userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk ota job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, jobtypes.JobMessage{
		JobID:    job.ID,
		TenantID: tenantID,
		Type:     jobtypes.JobTypeOTACommand,
	})

	h.createAuditEntry(r, "ota.bulk_send", job.ID.String(), nil, map[string]interface{}{
		"job_id":       job.ID.String(),
		"command_type": req.CommandType,
		"sim_count":    len(req.SimIDs),
	}, userID)

	apierr.WriteSuccess(w, http.StatusAccepted, map[string]interface{}{
		"job_id":     job.ID.String(),
		"state":      "queued",
		"total_sims": len(req.SimIDs),
	})
}

func (h *Handler) GetCommand(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "commandId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid command ID format")
		return
	}

	cmd, err := h.otaStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrOTACommandNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "OTA command not found")
			return
		}
		h.logger.Error().Err(err).Str("command_id", idStr).Msg("get ota command")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toOTAResponse(cmd))
}

func (h *Handler) ListHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	simIDStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(simIDStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	if _, err := h.simStore.GetByID(r.Context(), tenantID, simID); err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simIDStr).Msg("get sim for ota history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	q := r.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	filter := store.OTACommandFilter{
		CommandType: q.Get("command_type"),
		Status:      q.Get("status"),
		Channel:     q.Get("channel"),
	}

	commands, nextCursor, err := h.otaStore.ListBySimID(r.Context(), tenantID, simID, q.Get("cursor"), limit, filter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list ota history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]otaCommandResponse, 0, len(commands))
	for i := range commands {
		items = append(items, toOTAResponse(&commands[i]))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) validateOTARequest(req sendOTARequest) string {
	if req.CommandType == "" {
		return "command_type is required"
	}
	if err := otapkg.CommandType(req.CommandType).Validate(); err != nil {
		return err.Error()
	}

	if req.Channel == "" {
		req.Channel = string(otapkg.ChannelSMSPP)
	}
	if err := otapkg.DeliveryChannel(req.Channel).Validate(); err != nil {
		return err.Error()
	}

	if req.SecurityMode == "" {
		req.SecurityMode = string(otapkg.SecurityNone)
	}

	if req.Payload == nil || string(req.Payload) == "" {
		return "payload is required"
	}

	return ""
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	ip := r.RemoteAddr
	ua := r.UserAgent()

	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	var beforeData, afterData json.RawMessage
	if before != nil {
		beforeData, _ = json.Marshal(before)
	}
	if after != nil {
		afterData, _ = json.Marshal(after)
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    "ota_command",
		EntityID:      entityID,
		BeforeData:    beforeData,
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}

func userIDFromCtx(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
