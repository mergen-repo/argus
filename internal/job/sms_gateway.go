package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var smsOutboundSentTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "argus_sms_outbound_sent_total",
		Help: "Total SMS outbound send attempts by status.",
	},
	[]string{"status"},
)

// SMSSender abstracts the Twilio/gateway adapter so the processor is testable.
// SendSMS returns (providerMessageID, error).
type SMSSender interface {
	SendSMSWithResult(ctx context.Context, msisdn, text string) (string, error)
}

type smsOutboundStoreJob interface {
	GetByID(ctx context.Context, id uuid.UUID) (*store.SMSOutbound, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status, providerMsgID, errorCode string, sentAt *time.Time) error
}

type smsEventBus interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

// SMSGatewayProcessor processes sms_outbound_send jobs.
// The full message text is NOT stored in the DB (GDPR trade-off).
// Instead the API handler caches it in Redis at key sms_text:{sms_id}
// with a 1-hour TTL. This processor reads and deletes that key.
// If the key is missing (expired or never written), the send is aborted
// and the row is marked failed.
type SMSGatewayProcessor struct {
	smsStore smsOutboundStoreJob
	sender   SMSSender
	redis    *redis.Client
	eventBus smsEventBus
	logger   zerolog.Logger
}

func NewSMSGatewayProcessor(
	smsStore smsOutboundStoreJob,
	sender SMSSender,
	redis *redis.Client,
	eventBus smsEventBus,
	logger zerolog.Logger,
) *SMSGatewayProcessor {
	return &SMSGatewayProcessor{
		smsStore: smsStore,
		sender:   sender,
		redis:    redis,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", JobTypeSMSOutboundSend).Logger(),
	}
}

func (p *SMSGatewayProcessor) Type() string {
	return JobTypeSMSOutboundSend
}

type smsOutboundPayload struct {
	SMSID string `json:"sms_id"`
}

func (p *SMSGatewayProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload smsOutboundPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("sms_gateway: unmarshal payload: %w", err)
	}

	smsID, err := uuid.Parse(payload.SMSID)
	if err != nil {
		return fmt.Errorf("sms_gateway: invalid sms_id %q: %w", payload.SMSID, err)
	}

	row, err := p.smsStore.GetByID(ctx, smsID)
	if err != nil {
		return fmt.Errorf("sms_gateway: get sms_outbound: %w", err)
	}

	if row.Status != "queued" {
		p.logger.Info().
			Str("sms_id", smsID.String()).
			Str("status", row.Status).
			Msg("sms not queued, skipping (idempotent)")
		return nil
	}

	textKey := fmt.Sprintf("sms_text:%s", smsID.String())
	text, err := p.redis.GetDel(ctx, textKey).Result()
	if err != nil {
		p.logger.Error().Err(err).Str("sms_id", smsID.String()).Msg("sms text not found in redis")
		errCode := "TEXT_EXPIRED"
		_ = p.smsStore.UpdateStatus(ctx, smsID, "failed", "", errCode, nil)
		smsOutboundSentTotal.WithLabelValues("failed").Inc()
		p.emitDeliveryFailed(ctx, job.TenantID, smsID, errCode)
		return nil
	}

	providerMsgID, sendErr := p.sender.SendSMSWithResult(ctx, row.MSISDN, text)
	if sendErr != nil {
		p.logger.Error().Err(sendErr).Str("sms_id", smsID.String()).Msg("sms send failed")
		errCode := "SEND_FAILED"
		_ = p.smsStore.UpdateStatus(ctx, smsID, "failed", "", errCode, nil)
		smsOutboundSentTotal.WithLabelValues("failed").Inc()
		p.emitDeliveryFailed(ctx, job.TenantID, smsID, errCode)
		return nil
	}

	now := time.Now().UTC()
	if err := p.smsStore.UpdateStatus(ctx, smsID, "sent", providerMsgID, "", &now); err != nil {
		p.logger.Error().Err(err).Str("sms_id", smsID.String()).Msg("update sms status to sent")
		return fmt.Errorf("sms_gateway: update status: %w", err)
	}

	smsOutboundSentTotal.WithLabelValues("sent").Inc()

	p.logger.Info().
		Str("sms_id", smsID.String()).
		Str("provider_msg_id", providerMsgID).
		Msg("SMS sent successfully")

	return nil
}

func (p *SMSGatewayProcessor) emitDeliveryFailed(ctx context.Context, tenantID, smsID uuid.UUID, errCode string) {
	if p.eventBus == nil {
		return
	}
	_ = p.eventBus.Publish(ctx, bus.SubjectNotification, map[string]interface{}{
		"event_type": "sms_delivery_failed",
		"tenant_id":  tenantID.String(),
		"sms_id":     smsID.String(),
		"error_code": errCode,
	})
}
