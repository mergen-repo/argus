package job

// WebhookRetryProcessor sweeps webhook_deliveries rows whose next_retry_at has
// elapsed and re-sends them through the dispatcher. Backoff between attempts:
// 30s, 2m, 10m, 30m, 60m. After the 5th failure the row is marked dead_letter
// and a webhook.dead_letter notification event is published.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	webhookMaxAttempts = 5
	webhookSweepLimit  = 200
)

// webhookRetryBackoffs holds the wait between attempts. Index 0 is unused so
// that backoff[attemptCount] returns the wait AFTER attemptCount attempts have
// been made (i.e. wait before attempt 2, 3, 4, 5).
var webhookRetryBackoffs = []time.Duration{
	0,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
	60 * time.Minute,
}

// WebhookRetryResult is written to jobs.result when the processor completes.
type WebhookRetryResult struct {
	Swept      int      `json:"swept"`
	Succeeded  int      `json:"succeeded"`
	Retrying   int      `json:"retrying"`
	DeadLetter int      `json:"dead_letter"`
	Errors     []string `json:"errors,omitempty"`
}

type webhookRetryDeliveryStore interface {
	ListDueForRetry(ctx context.Context, now time.Time, limit int) ([]*store.WebhookDelivery, error)
	UpdateAttempt(ctx context.Context, id uuid.UUID, attemptCount int, nextRetryAt *time.Time, responseStatus *int, responseBody *string) error
	MarkFinal(ctx context.Context, id uuid.UUID, state string) error
}

type webhookRetryConfigStore interface {
	Get(ctx context.Context, id uuid.UUID) (*store.WebhookConfig, error)
	BumpSuccess(ctx context.Context, id uuid.UUID, at time.Time) error
	BumpFailure(ctx context.Context, id uuid.UUID, at time.Time) error
}

type webhookRetryMetrics interface {
	IncWebhookRetry(result string)
}

// WebhookRetryProcessor implements the Processor interface for JobTypeWebhookRetry.
type WebhookRetryProcessor struct {
	deliveries webhookRetryDeliveryStore
	configs    webhookRetryConfigStore
	jobs       jobProgressTracker
	eventBus   busPublisher
	client     *http.Client
	metrics    webhookRetryMetrics
	now        func() time.Time
	logger     zerolog.Logger
}

func NewWebhookRetryProcessor(
	deliveries *store.WebhookDeliveryStore,
	configs *store.WebhookConfigStore,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	reg *metrics.Registry,
	logger zerolog.Logger,
) *WebhookRetryProcessor {
	return &WebhookRetryProcessor{
		deliveries: deliveries,
		configs:    configs,
		jobs:       jobs,
		eventBus:   eventBus,
		client:     &http.Client{Timeout: 10 * time.Second},
		metrics:    reg,
		now:        func() time.Time { return time.Now().UTC() },
		logger:     logger.With().Str("processor", JobTypeWebhookRetry).Logger(),
	}
}

func (p *WebhookRetryProcessor) Type() string {
	return JobTypeWebhookRetry
}

func (p *WebhookRetryProcessor) Process(ctx context.Context, job *store.Job) error {
	due, err := p.deliveries.ListDueForRetry(ctx, p.now(), webhookSweepLimit)
	if err != nil {
		return fmt.Errorf("webhook_retry: list due: %w", err)
	}

	result := WebhookRetryResult{Swept: len(due)}

	for _, d := range due {
		state, errs := p.attemptOne(ctx, d)
		switch state {
		case "succeeded":
			result.Succeeded++
		case "dead_letter":
			result.DeadLetter++
		default:
			result.Retrying++
		}
		if len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
		}
	}

	resultJSON, _ := json.Marshal(result)
	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("webhook_retry: complete job: %w", err)
	}
	p.logger.Info().
		Int("swept", result.Swept).
		Int("succeeded", result.Succeeded).
		Int("retrying", result.Retrying).
		Int("dead_letter", result.DeadLetter).
		Msg("webhook retry sweep done")
	return nil
}

func (p *WebhookRetryProcessor) attemptOne(ctx context.Context, d *store.WebhookDelivery) (string, []string) {
	cfg, err := p.configs.Get(ctx, d.ConfigID)
	if err != nil {
		p.logger.Warn().Err(err).Str("delivery_id", d.ID.String()).Msg("get config")
		_ = p.deliveries.MarkFinal(ctx, d.ID, "dead_letter")
		p.metrics.IncWebhookRetry("dead_letter")
		return "dead_letter", []string{fmt.Sprintf("delivery=%s: get config: %s", d.ID, err.Error())}
	}

	payload := []byte(d.PayloadPreview)
	if len(payload) == 0 {
		payload = []byte("{}")
	}

	outcome := notification.SendWebhook(ctx, p.client, cfg, d.EventType, payload)

	newAttempt := d.AttemptCount + 1
	var respStatus *int
	var respBody *string
	if outcome.StatusCode != 0 {
		sc := outcome.StatusCode
		respStatus = &sc
	}
	if outcome.ResponseBody != "" {
		rb := outcome.ResponseBody
		respBody = &rb
	}

	if outcome.Success {
		_ = p.deliveries.UpdateAttempt(ctx, d.ID, newAttempt, nil, respStatus, respBody)
		_ = p.deliveries.MarkFinal(ctx, d.ID, "succeeded")
		_ = p.configs.BumpSuccess(ctx, cfg.ID, p.now())
		p.metrics.IncWebhookRetry("succeeded")
		return "succeeded", nil
	}

	_ = p.configs.BumpFailure(ctx, cfg.ID, p.now())

	if newAttempt >= webhookMaxAttempts {
		_ = p.deliveries.UpdateAttempt(ctx, d.ID, newAttempt, nil, respStatus, respBody)
		_ = p.deliveries.MarkFinal(ctx, d.ID, "dead_letter")
		p.publishDeadLetter(ctx, cfg, d)
		p.metrics.IncWebhookRetry("dead_letter")
		return "dead_letter", nil
	}

	wait := webhookRetryBackoffs[newAttempt-1]
	next := p.now().Add(wait)
	_ = p.deliveries.UpdateAttempt(ctx, d.ID, newAttempt, &next, respStatus, respBody)
	p.metrics.IncWebhookRetry("retrying")
	return "retrying", nil
}

func (p *WebhookRetryProcessor) publishDeadLetter(ctx context.Context, cfg *store.WebhookConfig, d *store.WebhookDelivery) {
	if p.eventBus == nil {
		return
	}
	payload := map[string]any{
		"tenant_id":     cfg.TenantID.String(),
		"config_id":     cfg.ID.String(),
		"delivery_id":   d.ID.String(),
		"event_type":    d.EventType,
		"url":           cfg.URL,
		"attempt_count": webhookMaxAttempts,
	}
	_ = p.eventBus.Publish(ctx, bus.SubjectNotification, map[string]any{
		"event_type": "webhook.dead_letter",
		"tenant_id":  cfg.TenantID.String(),
		"data":       payload,
	})
}
