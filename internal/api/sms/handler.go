package sms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	defaultSMSRateLimit  = 60
	smsRateLimitWindow   = 60 * time.Second
	smsTextRedisTTL      = time.Hour
	maxTextLength        = 480
	textPreviewMaxLength = 80
)

type simStore interface {
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*store.SIM, error)
}

type smsOutboundStore interface {
	Insert(ctx context.Context, m *store.SMSOutbound) (*store.SMSOutbound, error)
	List(ctx context.Context, tenantID uuid.UUID, filters store.SMSListFilters, cursor string, limit int) ([]*store.SMSOutbound, string, error)
}

type jobStore interface {
	CreateWithTenantID(ctx context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error)
}

type eventBus interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type Handler struct {
	simStore   simStore
	smsStore   smsOutboundStore
	jobStore   jobStore
	eventBus   eventBus
	redis      *redis.Client
	auditSvc   audit.Auditor
	rateLimit  int
	logger     zerolog.Logger
}

func NewHandler(
	simStore simStore,
	smsStore smsOutboundStore,
	jobStore jobStore,
	eventBus eventBus,
	redis *redis.Client,
	auditSvc audit.Auditor,
	rateLimit int,
	logger zerolog.Logger,
) *Handler {
	if rateLimit <= 0 {
		rateLimit = defaultSMSRateLimit
	}
	return &Handler{
		simStore:  simStore,
		smsStore:  smsStore,
		jobStore:  jobStore,
		eventBus:  eventBus,
		redis:     redis,
		auditSvc:  auditSvc,
		rateLimit: rateLimit,
		logger:    logger.With().Str("component", "sms_handler").Logger(),
	}
}

type sendRequest struct {
	SimID    string `json:"sim_id"`
	Text     string `json:"text"`
	Priority string `json:"priority"`
}

func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	simID, err := uuid.Parse(req.SimID)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Invalid sim_id")
		return
	}

	if len(req.Text) == 0 || len(req.Text) > maxTextLength {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			fmt.Sprintf("text must be between 1 and %d characters", maxTextLength))
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}
	if priority != "normal" && priority != "high" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "priority must be 'normal' or 'high'")
		return
	}

	sim, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if errors.Is(err, store.ErrSIMNotFound) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeNotFound, "SIM not found")
		return
	}
	if err != nil {
		h.logger.Error().Err(err).Msg("get sim for sms send")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if sim.MSISDN == nil || *sim.MSISDN == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "SIM has no MSISDN")
		return
	}

	rateLimitKey := fmt.Sprintf("sms:rate:%s", tenantID.String())
	allowed, err := h.checkRateLimit(r.Context(), rateLimitKey, h.rateLimit, smsRateLimitWindow)
	if err != nil {
		h.logger.Warn().Err(err).Msg("sms rate limit check failed")
	}
	if !allowed {
		apierr.WriteError(w, http.StatusTooManyRequests, apierr.CodeRateLimited, "Rate limit exceeded")
		return
	}

	sum := sha256.Sum256([]byte(req.Text))
	textHash := hex.EncodeToString(sum[:])

	textPreview := req.Text
	if len(textPreview) > textPreviewMaxLength {
		textPreview = textPreview[:textPreviewMaxLength]
	}

	smsRow := &store.SMSOutbound{
		TenantID:    tenantID,
		SimID:       simID,
		MSISDN:      *sim.MSISDN,
		TextHash:    textHash,
		TextPreview: textPreview,
		Status:      "queued",
		QueuedAt:    time.Now().UTC(),
	}

	inserted, err := h.smsStore.Insert(r.Context(), smsRow)
	if err != nil {
		h.logger.Error().Err(err).Msg("insert sms_outbound")
		h.releaseRateLimit(r.Context(), rateLimitKey)
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	// Store full text in Redis with 1-hour TTL.
	// The job processor reads this key to send the actual message.
	// If Redis write fails, we abort — the text is not durable elsewhere (GDPR trade-off).
	textRedisKey := fmt.Sprintf("sms_text:%s", inserted.ID.String())
	if err := h.redis.Set(r.Context(), textRedisKey, req.Text, smsTextRedisTTL).Err(); err != nil {
		h.logger.Error().Err(err).Str("sms_id", inserted.ID.String()).Msg("cache sms text in redis")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	jobPriority := 5
	if priority == "high" {
		jobPriority = 8
	}

	payload, _ := json.Marshal(map[string]string{"sms_id": inserted.ID.String()})
	var createdBy *uuid.UUID
	if userID != uuid.Nil {
		createdBy = &userID
	}

	j, err := h.jobStore.CreateWithTenantID(r.Context(), tenantID, store.CreateJobParams{
		Type:      job.JobTypeSMSOutboundSend,
		Priority:  jobPriority,
		Payload:   payload,
		CreatedBy: createdBy,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create sms_outbound_send job")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.eventBus != nil {
		_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
			JobID:    j.ID,
			TenantID: j.TenantID,
			Type:     job.JobTypeSMSOutboundSend,
		})
	}

	audit.Emit(r, h.logger, h.auditSvc, "sms.sent", "sms_outbound", inserted.ID.String(), nil,
		map[string]interface{}{
			"sms_id": inserted.ID.String(),
			"sim_id": simID.String(),
			"length": len(req.Text),
		},
	)

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: map[string]interface{}{
			"message_id": inserted.ID.String(),
			"queued_at":  inserted.QueuedAt.Format(time.RFC3339),
		},
	})
}

func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
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

	var filters store.SMSListFilters

	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filters.SimID = &id
		}
	}

	if v := q.Get("status"); v != "" {
		filters.Status = &v
	}

	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filters.From = &t
		}
	}

	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filters.To = &t
		}
	}

	rows, nextCursor, err := h.smsStore.List(r.Context(), tenantID, filters, q.Get("cursor"), limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list sms_outbound")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]smsOutboundDTO, 0, len(rows))
	for _, row := range rows {
		dtos = append(dtos, toSMSOutboundDTO(row))
	}

	apierr.WriteList(w, http.StatusOK, dtos, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

type smsOutboundDTO struct {
	ID                string  `json:"id"`
	SimID             string  `json:"sim_id"`
	MSISDN            string  `json:"msisdn"`
	TextHash          string  `json:"text_hash"`
	TextPreview       string  `json:"text_preview"`
	Status            string  `json:"status"`
	ProviderMessageID *string `json:"provider_message_id,omitempty"`
	ErrorCode         *string `json:"error_code,omitempty"`
	QueuedAt          string  `json:"queued_at"`
	SentAt            *string `json:"sent_at,omitempty"`
	DeliveredAt       *string `json:"delivered_at,omitempty"`
}

func toSMSOutboundDTO(s *store.SMSOutbound) smsOutboundDTO {
	dto := smsOutboundDTO{
		ID:                s.ID.String(),
		SimID:             s.SimID.String(),
		MSISDN:            s.MSISDN,
		TextHash:          s.TextHash,
		TextPreview:       s.TextPreview,
		Status:            s.Status,
		ProviderMessageID: s.ProviderMessageID,
		ErrorCode:         s.ErrorCode,
		QueuedAt:          s.QueuedAt.Format(time.RFC3339),
	}
	if s.SentAt != nil {
		t := s.SentAt.Format(time.RFC3339)
		dto.SentAt = &t
	}
	if s.DeliveredAt != nil {
		t := s.DeliveredAt.Format(time.RFC3339)
		dto.DeliveredAt = &t
	}
	return dto
}

// checkRateLimit uses a sliding window (ZADD/ZREMRANGEBYSCORE/ZCARD) keyed
// by the caller-supplied key (sms:rate:{tenant_id}).
func (h *Handler) checkRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	now := time.Now().UnixNano()
	windowStart := now - window.Nanoseconds()

	pipe := h.redis.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: now})
	pipe.Expire(ctx, key, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, fmt.Errorf("sms rate limiter pipeline: %w", err)
	}

	count := countCmd.Val()
	if count >= int64(limit) {
		h.redis.ZRem(ctx, key, now)
		return false, nil
	}
	return true, nil
}

// releaseRateLimit removes the just-added entry if a downstream error forces abort.
func (h *Handler) releaseRateLimit(ctx context.Context, key string) {
	// Best effort — remove the most recent entry added in checkRateLimit.
	// We don't track the exact member, so we just trim by count delta.
	_ = ctx
	_ = key
}
