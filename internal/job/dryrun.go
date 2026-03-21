package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/dryrun"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type DryRunProcessor struct {
	dryRunSvc *dryrun.Service
	jobs      *store.JobStore
	eventBus  *bus.EventBus
	logger    zerolog.Logger
}

func NewDryRunProcessor(
	dryRunSvc *dryrun.Service,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *DryRunProcessor {
	return &DryRunProcessor{
		dryRunSvc: dryRunSvc,
		jobs:      jobs,
		eventBus:  eventBus,
		logger:    logger.With().Str("component", "dryrun_processor").Logger(),
	}
}

func (p *DryRunProcessor) Type() string {
	return JobTypePolicyDryRun
}

type dryRunPayload struct {
	VersionID string  `json:"version_id"`
	SegmentID *string `json:"segment_id"`
}

func (p *DryRunProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload dryRunPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal dry-run payload: %w", err)
	}

	versionID, err := uuid.Parse(payload.VersionID)
	if err != nil {
		return fmt.Errorf("parse version_id: %w", err)
	}

	var segmentID *uuid.UUID
	if payload.SegmentID != nil && *payload.SegmentID != "" {
		parsed, parseErr := uuid.Parse(*payload.SegmentID)
		if parseErr != nil {
			return fmt.Errorf("parse segment_id: %w", parseErr)
		}
		segmentID = &parsed
	}

	req := dryrun.DryRunRequest{
		VersionID: versionID,
		TenantID:  job.TenantID,
		SegmentID: segmentID,
	}

	result, err := p.dryRunSvc.Execute(ctx, req)
	if err != nil {
		return fmt.Errorf("execute dry-run: %w", err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal dry-run result: %w", err)
	}

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete job: %w", err)
	}

	_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
		"job_id":    job.ID.String(),
		"tenant_id": job.TenantID.String(),
		"type":      JobTypePolicyDryRun,
		"state":     "completed",
	})

	p.logger.Info().
		Str("job_id", job.ID.String()).
		Str("version_id", versionID.String()).
		Int("total_affected", result.TotalAffected).
		Msg("dry-run job completed")

	return nil
}
