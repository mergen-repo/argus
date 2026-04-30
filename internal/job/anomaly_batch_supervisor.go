package job

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/btopcu/argus/internal/bus"
	sev "github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

const (
	supervisorMaxRetries  = 5
	supervisorBaseBackoff = 500 * time.Millisecond
	supervisorCapBackoff  = 30 * time.Second
)

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type CrashSafeProcessor struct {
	inner       Processor
	maxRetries  int
	baseBackoff time.Duration
	eventBus    EventPublisher
	logger      zerolog.Logger
	sleepFunc   func(ctx context.Context, d time.Duration)
}

func NewCrashSafeProcessor(inner Processor, eb EventPublisher, logger zerolog.Logger) *CrashSafeProcessor {
	return &CrashSafeProcessor{
		inner:       inner,
		maxRetries:  supervisorMaxRetries,
		baseBackoff: supervisorBaseBackoff,
		eventBus:    eb,
		logger:      logger.With().Str("component", "crash_safe_processor").Str("inner_type", inner.Type()).Logger(),
	}
}

func (p *CrashSafeProcessor) sleep(ctx context.Context, d time.Duration) {
	if p.sleepFunc != nil {
		p.sleepFunc(ctx, d)
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (p *CrashSafeProcessor) Type() string {
	return p.inner.Type()
}

func (p *CrashSafeProcessor) Process(ctx context.Context, j *store.Job) error {
	var lastErr error

	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := p.backoffDuration(attempt)
			p.sleep(ctx, backoff)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}

		err := p.safeProcess(ctx, j)
		if err == nil {
			if attempt > 0 {
				p.logger.Info().
					Int("attempt", attempt+1).
					Str("job_id", j.ID.String()).
					Msg("crash-safe processor: retry succeeded")
			}
			return nil
		}

		lastErr = err
		p.logger.Warn().
			Err(err).
			Int("attempt", attempt+1).
			Int("max_retries", p.maxRetries).
			Str("job_id", j.ID.String()).
			Msg("crash-safe processor: attempt failed")
	}

	p.logger.Error().
		Err(lastErr).
		Str("job_id", j.ID.String()).
		Msg("crash-safe processor: all retries exhausted")

	if p.eventBus != nil {
		env := bus.NewEnvelope("anomaly_batch_crash", bus.SystemTenantID.String(), sev.High).
			WithSource("infra").
			WithTitle(fmt.Sprintf("Anomaly batch crashed: job %s", j.ID.String())).
			WithMessage(lastErr.Error()).
			SetEntity("job", j.ID.String(), j.ID.String()).
			WithMeta("job_id", j.ID.String()).
			WithMeta("error", lastErr.Error())
		_ = p.eventBus.Publish(context.Background(), bus.SubjectAlertTriggered, env)
	}

	return lastErr
}

func (p *CrashSafeProcessor) safeProcess(ctx context.Context, j *store.Job) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			const maxStack = 4096
			stackStr := string(stack)
			if len(stackStr) > maxStack {
				stackStr = stackStr[:maxStack] + "... [truncated]"
			}

			p.logger.Error().
				Str("panic", fmt.Sprintf("%v", r)).
				Str("stack", stackStr).
				Str("job_id", j.ID.String()).
				Msg("crash-safe processor: panic recovered")

			retErr = fmt.Errorf("panic: %v", r)
		}
	}()

	return p.inner.Process(ctx, j)
}

func (p *CrashSafeProcessor) backoffDuration(attempt int) time.Duration {
	backoff := p.baseBackoff
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff > supervisorCapBackoff {
			backoff = supervisorCapBackoff
			break
		}
	}
	return backoff
}
