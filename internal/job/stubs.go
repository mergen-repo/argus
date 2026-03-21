package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type StubProcessor struct {
	jobType  string
	jobs     *store.JobStore
	eventBus *bus.EventBus
	logger   zerolog.Logger
}

func NewStubProcessor(jobType string, jobs *store.JobStore, eventBus *bus.EventBus, logger zerolog.Logger) *StubProcessor {
	return &StubProcessor{
		jobType:  jobType,
		jobs:     jobs,
		eventBus: eventBus,
		logger:   logger.With().Str("processor", jobType).Logger(),
	}
}

func (p *StubProcessor) Type() string {
	return p.jobType
}

func (p *StubProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Warn().
		Str("job_id", job.ID.String()).
		Str("type", p.jobType).
		Msg("stub processor invoked — not yet implemented")

	result, _ := json.Marshal(map[string]string{
		"status":  "stub",
		"message": fmt.Sprintf("processor for %s not yet implemented", p.jobType),
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, result); err != nil {
		return fmt.Errorf("complete stub job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":    job.ID.String(),
			"tenant_id": job.TenantID.String(),
			"type":      p.jobType,
			"state":     "completed",
			"stub":      true,
		})
	}

	return nil
}
