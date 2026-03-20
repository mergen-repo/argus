package job

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

type Processor interface {
	Type() string
	Process(ctx context.Context, job *store.Job) error
}

type JobMessage struct {
	JobID    uuid.UUID `json:"job_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	Type     string    `json:"type"`
}

type Runner struct {
	jobs       *store.JobStore
	eventBus   *bus.EventBus
	logger     zerolog.Logger
	processors map[string]Processor
	workerID   string
	sub        *nats.Subscription
	wg         sync.WaitGroup
	stopCh     chan struct{}
}

func NewRunner(jobs *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *Runner {
	hostname, _ := os.Hostname()
	return &Runner{
		jobs:       jobs,
		eventBus:   eventBus,
		logger:     logger.With().Str("component", "job_runner").Logger(),
		processors: make(map[string]Processor),
		workerID:   fmt.Sprintf("worker-%s-%d", hostname, os.Getpid()),
		stopCh:     make(chan struct{}),
	}
}

func (r *Runner) Register(p Processor) {
	r.processors[p.Type()] = p
}

func (r *Runner) Start() error {
	sub, err := r.eventBus.QueueSubscribe(
		bus.SubjectJobQueue,
		"job-runners",
		r.handleMessage,
	)
	if err != nil {
		return fmt.Errorf("subscribe to job queue: %w", err)
	}
	r.sub = sub
	r.logger.Info().Str("worker_id", r.workerID).Msg("job runner started")
	return nil
}

func (r *Runner) Stop() {
	close(r.stopCh)
	if r.sub != nil {
		r.sub.Unsubscribe()
	}
	r.wg.Wait()
	r.logger.Info().Msg("job runner stopped")
}

func (r *Runner) handleMessage(subject string, data []byte) {
	var msg JobMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		r.logger.Error().Err(err).Msg("unmarshal job message")
		return
	}

	processor, ok := r.processors[msg.Type]
	if !ok {
		r.logger.Warn().Str("type", msg.Type).Msg("no processor for job type")
		return
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.processJob(msg, processor)
	}()
}

func (r *Runner) processJob(msg JobMessage, processor Processor) {
	ctx := context.Background()
	log := r.logger.With().
		Str("job_id", msg.JobID.String()).
		Str("type", msg.Type).
		Logger()

	if err := r.jobs.Lock(ctx, msg.JobID, r.workerID); err != nil {
		log.Warn().Err(err).Msg("failed to lock job")
		return
	}
	log.Info().Msg("job locked, processing")

	job, err := r.jobs.GetByIDInternal(ctx, msg.JobID)
	if err != nil {
		log.Error().Err(err).Msg("get job for processing")
		return
	}

	if err := processor.Process(ctx, job); err != nil {
		log.Error().Err(err).Msg("job processing failed")
		errReport, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = r.jobs.Fail(ctx, msg.JobID, errReport)

		_ = r.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":    msg.JobID.String(),
			"tenant_id": msg.TenantID.String(),
			"type":      msg.Type,
			"state":     "failed",
			"error":     err.Error(),
		})
		return
	}

	log.Info().Msg("job completed")
}
