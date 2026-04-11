package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	batchSize      = 1000
	asyncThreshold = 100000
)

type SessionInfo struct {
	ID            string
	SimID         string
	NASIP         string
	AcctSessionID string
	IMSI          string
}

type CoARequest struct {
	NASIP         string
	AcctSessionID string
	IMSI          string
	Attributes    map[string]interface{}
}

type CoAResult struct {
	Status  string
	Message string
}

type SessionProvider interface {
	GetSessionsForSIM(ctx context.Context, simID string) ([]SessionInfo, error)
}

type CoADispatcher interface {
	SendCoA(ctx context.Context, req CoARequest) (*CoAResult, error)
}

type RolloutProgressEvent struct {
	RolloutID    string               `json:"rollout_id"`
	TenantID     string               `json:"tenant_id"`
	PolicyID     string               `json:"policy_id,omitempty"`
	VersionID    string               `json:"version_id"`
	State        string               `json:"state"`
	CurrentStage int                  `json:"current_stage"`
	TotalStages  int                  `json:"total_stages"`
	Stages       []store.RolloutStage `json:"stages"`
	TotalSIMs    int                  `json:"total_sims"`
	MigratedSIMs int                  `json:"migrated_sims"`
	ProgressPct  float64              `json:"progress_pct"`
	StartedAt    string               `json:"started_at,omitempty"`
}

type Service struct {
	policyStore     *store.PolicyStore
	simStore        *store.SIMStore
	sessionProvider SessionProvider
	coaDispatcher   CoADispatcher
	eventBus        *bus.EventBus
	jobStore        *store.JobStore
	logger          zerolog.Logger
}

func NewService(
	policyStore *store.PolicyStore,
	simStore *store.SIMStore,
	sessionProvider SessionProvider,
	coaDispatcher CoADispatcher,
	eventBus *bus.EventBus,
	jobStore *store.JobStore,
	logger zerolog.Logger,
) *Service {
	return &Service{
		policyStore:     policyStore,
		simStore:        simStore,
		sessionProvider: sessionProvider,
		coaDispatcher:   coaDispatcher,
		eventBus:        eventBus,
		jobStore:        jobStore,
		logger:          logger.With().Str("component", "rollout_service").Logger(),
	}
}

func (s *Service) SetSessionProvider(sp SessionProvider) {
	s.sessionProvider = sp
}

func (s *Service) SetCoADispatcher(cd CoADispatcher) {
	s.coaDispatcher = cd
}

func (s *Service) StartRollout(ctx context.Context, tenantID, versionID uuid.UUID, stagePcts []int, createdBy *uuid.UUID) (*store.PolicyRollout, error) {
	if len(stagePcts) == 0 {
		stagePcts = []int{1, 10, 100}
	}

	version, err := s.policyStore.GetVersionWithTenant(ctx, versionID, tenantID)
	if err != nil {
		return nil, err
	}

	if version.State != "draft" {
		return nil, store.ErrVersionNotDraft
	}

	existing, err := s.policyStore.GetActiveRolloutForPolicy(ctx, version.PolicyID)
	if err != nil {
		return nil, fmt.Errorf("check active rollout: %w", err)
	}
	if existing != nil {
		return nil, store.ErrRolloutInProgress
	}

	totalSIMs := 0
	if version.AffectedSIMCount != nil {
		totalSIMs = *version.AffectedSIMCount
	}
	if totalSIMs == 0 {
		count, countErr := s.simStore.CountByFilters(ctx, tenantID, store.SIMFleetFilters{})
		if countErr != nil {
			return nil, fmt.Errorf("count affected sims: %w", countErr)
		}
		totalSIMs = count
	}

	activeVersion, err := s.policyStore.GetActiveVersionSummary(ctx, version.PolicyID)
	if err != nil {
		return nil, fmt.Errorf("get active version: %w", err)
	}
	var previousVersionID *uuid.UUID
	if activeVersion != nil {
		previousVersionID = &activeVersion.ID
	}

	stages := make([]store.RolloutStage, len(stagePcts))
	for i, pct := range stagePcts {
		status := "pending"
		if i == 0 {
			status = "in_progress"
		}
		stages[i] = store.RolloutStage{Pct: pct, Status: status}
	}

	stagesJSON, err := json.Marshal(stages)
	if err != nil {
		return nil, fmt.Errorf("marshal stages: %w", err)
	}

	rollout, err := s.policyStore.CreateRollout(ctx, tenantID, store.CreateRolloutParams{
		PolicyVersionID:   versionID,
		PreviousVersionID: previousVersionID,
		Strategy:          "canary",
		Stages:            stagesJSON,
		TotalSIMs:         totalSIMs,
		CreatedBy:         createdBy,
	})
	if err != nil {
		return nil, err
	}

	stageTargetCount := int(math.Ceil(float64(totalSIMs) * float64(stagePcts[0]) / 100.0))

	if stageTargetCount > asyncThreshold {
		if err := s.createStageJob(ctx, tenantID, rollout.ID, 0, createdBy); err != nil {
			s.logger.Error().Err(err).Msg("create async stage job")
		}
	} else {
		if err := s.ExecuteStage(ctx, rollout, 0); err != nil {
			s.logger.Error().Err(err).Msg("execute initial stage")
		}
	}

	updated, err := s.policyStore.GetRolloutByID(ctx, rollout.ID)
	if err != nil {
		return rollout, nil
	}
	return updated, nil
}

func (s *Service) ExecuteStage(ctx context.Context, rollout *store.PolicyRollout, stageIndex int) error {
	tenantID := s.resolveTenantID(ctx, rollout)
	var stages []store.RolloutStage
	if err := json.Unmarshal(rollout.Stages, &stages); err != nil {
		return fmt.Errorf("unmarshal stages: %w", err)
	}

	if stageIndex >= len(stages) {
		return fmt.Errorf("stage index %d out of range", stageIndex)
	}

	stage := stages[stageIndex]
	targetMigrated := int(math.Ceil(float64(rollout.TotalSIMs) * float64(stage.Pct) / 100.0))
	remaining := targetMigrated - rollout.MigratedSIMs
	if remaining <= 0 {
		stages[stageIndex].Status = "completed"
		stagesJSON, _ := json.Marshal(stages)
		return s.policyStore.UpdateRolloutProgress(ctx, rollout.ID, rollout.MigratedSIMs, stageIndex, stagesJSON)
	}

	totalMigrated := rollout.MigratedSIMs

	for remaining > 0 {
		batchCount := batchSize
		if batchCount > remaining {
			batchCount = remaining
		}

		simIDs, err := s.policyStore.SelectSIMsForStage(ctx, tenantID, rollout.ID, rollout.PreviousVersionID, batchCount)
		if err != nil {
			return fmt.Errorf("select sims for stage: %w", err)
		}

		if len(simIDs) == 0 {
			break
		}

		assigned, err := s.policyStore.AssignSIMsToVersion(ctx, simIDs, rollout.PolicyVersionID, rollout.ID)
		if err != nil {
			return fmt.Errorf("assign sims to version: %w", err)
		}

		for _, simID := range simIDs[:assigned] {
			s.sendCoAForSIM(ctx, simID)
		}

		totalMigrated += assigned
		remaining -= assigned

		simCount := totalMigrated
		stages[stageIndex].Status = "in_progress"
		stages[stageIndex].SimCount = &simCount
		stages[stageIndex].Migrated = &totalMigrated

		stagesJSON, _ := json.Marshal(stages)
		if err := s.policyStore.UpdateRolloutProgress(ctx, rollout.ID, totalMigrated, stageIndex, stagesJSON); err != nil {
			s.logger.Error().Err(err).Msg("update rollout progress")
		}

		s.publishProgress(ctx, rollout, stages, totalMigrated, stageIndex)
	}

	stages[stageIndex].Status = "completed"
	finalCount := totalMigrated
	stages[stageIndex].Migrated = &finalCount
	stages[stageIndex].SimCount = &finalCount

	stagesJSON, _ := json.Marshal(stages)
	if err := s.policyStore.UpdateRolloutProgress(ctx, rollout.ID, totalMigrated, stageIndex, stagesJSON); err != nil {
		return fmt.Errorf("update final progress: %w", err)
	}

	if stage.Pct == 100 {
		if err := s.policyStore.CompleteRollout(ctx, rollout.ID); err != nil {
			return fmt.Errorf("complete rollout: %w", err)
		}
		s.publishProgressWithState(ctx, rollout, stages, totalMigrated, stageIndex, "completed")
	} else {
		s.publishProgress(ctx, rollout, stages, totalMigrated, stageIndex)
	}

	return nil
}

func (s *Service) AdvanceRollout(ctx context.Context, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error) {
	rollout, err := s.policyStore.GetRolloutByIDWithTenant(ctx, rolloutID, tenantID)
	if err != nil {
		return nil, err
	}

	if rollout.State == "completed" {
		return nil, store.ErrRolloutCompleted
	}
	if rollout.State == "rolled_back" {
		return nil, store.ErrRolloutRolledBack
	}
	if rollout.State != "in_progress" {
		return nil, fmt.Errorf("rollout is in unexpected state: %s", rollout.State)
	}

	var stages []store.RolloutStage
	if err := json.Unmarshal(rollout.Stages, &stages); err != nil {
		return nil, fmt.Errorf("unmarshal stages: %w", err)
	}

	nextStage := -1
	for i, st := range stages {
		if st.Status == "pending" {
			nextStage = i
			break
		}
		if st.Status == "in_progress" {
			return nil, store.ErrStageInProgress
		}
	}

	if nextStage == -1 {
		return nil, store.ErrRolloutCompleted
	}

	stagePct := stages[nextStage].Pct
	stageTargetCount := int(math.Ceil(float64(rollout.TotalSIMs)*float64(stagePct)/100.0)) - rollout.MigratedSIMs

	if stageTargetCount > asyncThreshold {
		if err := s.createStageJob(ctx, tenantID, rollout.ID, nextStage, nil); err != nil {
			return nil, fmt.Errorf("create async stage job: %w", err)
		}
		stages[nextStage].Status = "in_progress"
		stagesJSON, _ := json.Marshal(stages)
		_ = s.policyStore.UpdateRolloutProgress(ctx, rollout.ID, rollout.MigratedSIMs, nextStage, stagesJSON)
	} else {
		if err := s.ExecuteStage(ctx, rollout, nextStage); err != nil {
			return nil, fmt.Errorf("execute stage: %w", err)
		}
	}

	updated, err := s.policyStore.GetRolloutByID(ctx, rollout.ID)
	if err != nil {
		return rollout, nil
	}
	return updated, nil
}

func (s *Service) RollbackRollout(ctx context.Context, tenantID, rolloutID uuid.UUID, reason string) (*store.PolicyRollout, int, error) {
	rollout, err := s.policyStore.GetRolloutByIDWithTenant(ctx, rolloutID, tenantID)
	if err != nil {
		return nil, 0, err
	}

	if rollout.State == "completed" {
		return nil, 0, store.ErrRolloutCompleted
	}
	if rollout.State == "rolled_back" {
		return nil, 0, store.ErrRolloutRolledBack
	}

	simIDs, err := s.policyStore.GetRolloutSimIDs(ctx, rolloutID)
	if err != nil {
		return nil, 0, fmt.Errorf("get rollout sim ids: %w", err)
	}

	revertedCount, err := s.policyStore.RevertRolloutAssignments(ctx, rolloutID, rollout.PreviousVersionID)
	if err != nil {
		return nil, 0, fmt.Errorf("revert assignments: %w", err)
	}

	for i := 0; i < len(simIDs); i += batchSize {
		end := i + batchSize
		if end > len(simIDs) {
			end = len(simIDs)
		}
		for _, simID := range simIDs[i:end] {
			s.sendCoAForSIM(ctx, simID)
		}
	}

	if err := s.policyStore.RollbackRollout(ctx, rolloutID); err != nil {
		return nil, revertedCount, fmt.Errorf("rollback rollout: %w", err)
	}

	var stages []store.RolloutStage
	_ = json.Unmarshal(rollout.Stages, &stages)
	s.publishProgressWithState(ctx, rollout, stages, 0, rollout.CurrentStage, "rolled_back")

	updated, err := s.policyStore.GetRolloutByID(ctx, rolloutID)
	if err != nil {
		return rollout, revertedCount, nil
	}
	return updated, revertedCount, nil
}

func (s *Service) GetProgress(ctx context.Context, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error) {
	return s.policyStore.GetRolloutByIDWithTenant(ctx, rolloutID, tenantID)
}

func (s *Service) sendCoAForSIM(ctx context.Context, simID uuid.UUID) {
	if s.sessionProvider == nil || s.coaDispatcher == nil {
		return
	}

	sessions, err := s.sessionProvider.GetSessionsForSIM(ctx, simID.String())
	if err != nil {
		s.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("get sessions for CoA")
		return
	}

	for _, sess := range sessions {
		result, coaErr := s.coaDispatcher.SendCoA(ctx, CoARequest{
			NASIP:         sess.NASIP,
			AcctSessionID: sess.AcctSessionID,
			IMSI:          sess.IMSI,
		})

		status := "sent"
		if coaErr != nil {
			s.logger.Warn().Err(coaErr).
				Str("sim_id", simID.String()).
				Str("session_id", sess.ID).
				Msg("CoA send failed")
			status = "failed"
		} else if result != nil && result.Status != "ack" {
			status = "failed"
		} else {
			status = "acked"
		}

		if s.policyStore != nil {
			if updateErr := s.policyStore.UpdateAssignmentCoAStatus(ctx, simID, status); updateErr != nil {
				s.logger.Warn().Err(updateErr).Str("sim_id", simID.String()).Msg("update CoA status")
			}
		}
	}
}

func (s *Service) publishProgress(ctx context.Context, rollout *store.PolicyRollout, stages []store.RolloutStage, migrated, currentStage int) {
	s.publishProgressWithState(ctx, rollout, stages, migrated, currentStage, rollout.State)
}

func (s *Service) publishProgressWithState(ctx context.Context, rollout *store.PolicyRollout, stages []store.RolloutStage, migrated, currentStage int, state string) {
	if s.eventBus == nil {
		return
	}

	progressPct := 0.0
	if rollout.TotalSIMs > 0 {
		progressPct = math.Round(float64(migrated)/float64(rollout.TotalSIMs)*10000) / 100
	}

	startedAt := ""
	if rollout.StartedAt != nil {
		startedAt = rollout.StartedAt.Format(time.RFC3339)
	}

	tenantID := s.resolveTenantID(ctx, rollout)
	event := RolloutProgressEvent{
		RolloutID:    rollout.ID.String(),
		TenantID:     tenantID.String(),
		VersionID:    rollout.PolicyVersionID.String(),
		State:        state,
		CurrentStage: currentStage,
		TotalStages:  len(stages),
		Stages:       stages,
		TotalSIMs:    rollout.TotalSIMs,
		MigratedSIMs: migrated,
		ProgressPct:  progressPct,
		StartedAt:    startedAt,
	}

	if err := s.eventBus.Publish(ctx, bus.SubjectPolicyRolloutProgress, event); err != nil {
		s.logger.Warn().Err(err).Msg("publish rollout progress")
	}
}

func (s *Service) createStageJob(ctx context.Context, tenantID, rolloutID uuid.UUID, stageIndex int, createdBy *uuid.UUID) error {
	if s.jobStore == nil || s.eventBus == nil {
		return fmt.Errorf("async processing not available")
	}

	payload := map[string]interface{}{
		"rollout_id":  rolloutID.String(),
		"stage_index": stageIndex,
		"tenant_id":   tenantID.String(),
	}
	payloadJSON, _ := json.Marshal(payload)

	job, err := s.jobStore.CreateWithTenantID(ctx, tenantID, store.CreateJobParams{
		Type:      "policy_rollout_stage",
		Priority:  3,
		Payload:   payloadJSON,
		CreatedBy: createdBy,
	})
	if err != nil {
		return fmt.Errorf("create stage job: %w", err)
	}

	return s.eventBus.Publish(ctx, bus.SubjectJobQueue, map[string]interface{}{
		"job_id":    job.ID.String(),
		"tenant_id": tenantID.String(),
		"type":      "policy_rollout_stage",
	})
}

func (s *Service) resolveTenantID(ctx context.Context, rollout *store.PolicyRollout) uuid.UUID {
	if s.policyStore == nil {
		return uuid.Nil
	}
	tenantID, err := s.policyStore.GetTenantIDForRollout(ctx, rollout.ID)
	if err != nil {
		s.logger.Warn().Err(err).Str("rollout_id", rollout.ID.String()).Msg("resolve tenant_id for rollout")
		return uuid.Nil
	}
	return tenantID
}
