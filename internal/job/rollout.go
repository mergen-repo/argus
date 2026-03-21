package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/rollout"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type RolloutStageProcessor struct {
	rolloutSvc  *rollout.Service
	policyStore *store.PolicyStore
	jobs        *store.JobStore
	eventBus    *bus.EventBus
	logger      zerolog.Logger
}

func NewRolloutStageProcessor(
	rolloutSvc *rollout.Service,
	policyStore *store.PolicyStore,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *RolloutStageProcessor {
	return &RolloutStageProcessor{
		rolloutSvc:  rolloutSvc,
		policyStore: policyStore,
		jobs:        jobs,
		eventBus:    eventBus,
		logger:      logger.With().Str("component", "rollout_stage_processor").Logger(),
	}
}

func (p *RolloutStageProcessor) Type() string {
	return JobTypeRolloutStage
}

type rolloutStagePayload struct {
	RolloutID  string `json:"rollout_id"`
	StageIndex int    `json:"stage_index"`
	TenantID   string `json:"tenant_id"`
}

func (p *RolloutStageProcessor) Process(ctx context.Context, job *store.Job) error {
	var payload rolloutStagePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal rollout stage payload: %w", err)
	}

	rolloutID, err := uuid.Parse(payload.RolloutID)
	if err != nil {
		return fmt.Errorf("parse rollout_id: %w", err)
	}

	ro, err := p.policyStore.GetRolloutByID(ctx, rolloutID)
	if err != nil {
		return fmt.Errorf("get rollout: %w", err)
	}

	if err := p.rolloutSvc.ExecuteStage(ctx, ro, payload.StageIndex); err != nil {
		return fmt.Errorf("execute stage %d: %w", payload.StageIndex, err)
	}

	return nil
}
