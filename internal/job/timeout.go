package job

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type TimeoutDetector struct {
	jobs     *store.JobStore
	eventBus *bus.EventBus
	logger   zerolog.Logger
	interval time.Duration
	timeout  time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewTimeoutDetector(
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	timeout time.Duration,
	interval time.Duration,
	logger zerolog.Logger,
) *TimeoutDetector {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	return &TimeoutDetector{
		jobs:     jobs,
		eventBus: eventBus,
		logger:   logger.With().Str("component", "job_timeout").Logger(),
		interval: interval,
		timeout:  timeout,
		stopCh:   make(chan struct{}),
	}
}

func (td *TimeoutDetector) Start() {
	td.wg.Add(1)
	go func() {
		defer td.wg.Done()
		td.run()
	}()
	td.logger.Info().
		Dur("interval", td.interval).
		Dur("timeout", td.timeout).
		Msg("job timeout detector started")
}

func (td *TimeoutDetector) Stop() {
	close(td.stopCh)
	td.wg.Wait()
	td.logger.Info().Msg("job timeout detector stopped")
}

func (td *TimeoutDetector) run() {
	ticker := time.NewTicker(td.interval)
	defer ticker.Stop()

	for {
		select {
		case <-td.stopCh:
			return
		case <-ticker.C:
			td.sweep()
		}
	}
}

func (td *TimeoutDetector) sweep() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	staleJobs, err := td.jobs.FindTimedOutJobs(ctx, td.timeout)
	if err != nil {
		td.logger.Error().Err(err).Msg("find timed out jobs")
		return
	}

	if len(staleJobs) == 0 {
		return
	}

	td.logger.Warn().Int("count", len(staleJobs)).Msg("found timed out jobs")

	for _, j := range staleJobs {
		errReport, _ := json.Marshal(map[string]string{
			"error": "job timed out — no progress for " + td.timeout.String(),
		})

		if err := td.jobs.Fail(ctx, j.ID, errReport); err != nil {
			td.logger.Error().Err(err).Str("job_id", j.ID.String()).Msg("fail timed out job")
			continue
		}

		if td.eventBus != nil {
			_ = td.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
				"job_id":    j.ID.String(),
				"tenant_id": j.TenantID.String(),
				"type":      j.Type,
				"state":     "failed",
				"error":     "timeout",
			})
		}

		td.logger.Warn().
			Str("job_id", j.ID.String()).
			Str("type", j.Type).
			Msg("job timed out and marked failed")
	}
}
