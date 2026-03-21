package job

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

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

type RunnerConfig struct {
	MaxConcurrentPerTenant int
	LockRenewInterval      time.Duration
}

type Runner struct {
	jobs       *store.JobStore
	eventBus   *bus.EventBus
	distLock   *DistributedLock
	logger     zerolog.Logger
	processors map[string]Processor
	workerID   string
	config     RunnerConfig
	sub        *nats.Subscription
	wg         sync.WaitGroup
	stopCh     chan struct{}

	activeMu    sync.Mutex
	activeCount map[uuid.UUID]int

	cancelMu    sync.RWMutex
	cancelFuncs map[uuid.UUID]context.CancelFunc
}

func NewRunner(jobs *store.JobStore, eventBus *bus.EventBus, distLock *DistributedLock, cfg RunnerConfig, logger zerolog.Logger) *Runner {
	hostname, _ := os.Hostname()

	if cfg.MaxConcurrentPerTenant <= 0 {
		cfg.MaxConcurrentPerTenant = 5
	}
	if cfg.LockRenewInterval <= 0 {
		cfg.LockRenewInterval = 30 * time.Second
	}

	return &Runner{
		jobs:        jobs,
		eventBus:    eventBus,
		distLock:    distLock,
		logger:      logger.With().Str("component", "job_runner").Logger(),
		processors:  make(map[string]Processor),
		workerID:    fmt.Sprintf("worker-%s-%d", hostname, os.Getpid()),
		config:      cfg,
		stopCh:      make(chan struct{}),
		activeCount: make(map[uuid.UUID]int),
		cancelFuncs: make(map[uuid.UUID]context.CancelFunc),
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
	r.logger.Info().Str("worker_id", r.workerID).Int("max_concurrent", r.config.MaxConcurrentPerTenant).Msg("job runner started")
	return nil
}

func (r *Runner) Stop() {
	close(r.stopCh)
	if r.sub != nil {
		r.sub.Unsubscribe()
	}

	r.cancelMu.RLock()
	for _, cancel := range r.cancelFuncs {
		cancel()
	}
	r.cancelMu.RUnlock()

	r.wg.Wait()
	r.logger.Info().Msg("job runner stopped")
}

func (r *Runner) CancelJob(jobID uuid.UUID) {
	r.cancelMu.RLock()
	cancel, ok := r.cancelFuncs[jobID]
	r.cancelMu.RUnlock()

	if ok {
		cancel()
		r.logger.Info().Str("job_id", jobID.String()).Msg("job cancel signal sent")
	}
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

	if !r.tryAcquireSlot(msg.TenantID) {
		r.logger.Warn().
			Str("tenant_id", msg.TenantID.String()).
			Int("max", r.config.MaxConcurrentPerTenant).
			Msg("tenant at max concurrent jobs, message will be redelivered")
		return
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.releaseSlot(msg.TenantID)
		r.processJob(msg, processor)
	}()
}

func (r *Runner) tryAcquireSlot(tenantID uuid.UUID) bool {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()

	current := r.activeCount[tenantID]
	if current >= r.config.MaxConcurrentPerTenant {
		return false
	}
	r.activeCount[tenantID] = current + 1
	return true
}

func (r *Runner) releaseSlot(tenantID uuid.UUID) {
	r.activeMu.Lock()
	defer r.activeMu.Unlock()

	r.activeCount[tenantID]--
	if r.activeCount[tenantID] <= 0 {
		delete(r.activeCount, tenantID)
	}
}

func (r *Runner) processJob(msg JobMessage, processor Processor) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.cancelMu.Lock()
	r.cancelFuncs[msg.JobID] = cancel
	r.cancelMu.Unlock()

	defer func() {
		r.cancelMu.Lock()
		delete(r.cancelFuncs, msg.JobID)
		r.cancelMu.Unlock()
	}()

	log := r.logger.With().
		Str("job_id", msg.JobID.String()).
		Str("type", msg.Type).
		Logger()

	if err := r.jobs.Lock(ctx, msg.JobID, r.workerID); err != nil {
		log.Warn().Err(err).Msg("failed to lock job")
		return
	}
	log.Info().Msg("job locked, processing")

	renewDone := make(chan struct{})
	go r.renewLockLoop(ctx, msg.JobID, renewDone)

	job, err := r.jobs.GetByIDInternal(ctx, msg.JobID)
	if err != nil {
		log.Error().Err(err).Msg("get job for processing")
		close(renewDone)
		return
	}

	if err := processor.Process(ctx, job); err != nil {
		close(renewDone)

		if ctx.Err() != nil {
			log.Info().Msg("job cancelled via context")
			return
		}

		log.Error().Err(err).Msg("job processing failed")
		errReport, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = r.jobs.Fail(ctx, msg.JobID, errReport)

		_ = r.eventBus.Publish(context.Background(), bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":    msg.JobID.String(),
			"tenant_id": msg.TenantID.String(),
			"type":      msg.Type,
			"state":     "failed",
			"error":     err.Error(),
		})
		return
	}

	close(renewDone)
	log.Info().Msg("job completed")
}

func (r *Runner) renewLockLoop(ctx context.Context, jobID uuid.UUID, done chan struct{}) {
	ticker := time.NewTicker(r.config.LockRenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			if err := r.jobs.TouchLock(ctx, jobID, r.workerID); err != nil {
				r.logger.Warn().Err(err).Str("job_id", jobID.String()).Msg("failed to renew job lock")
			}
		}
	}
}
