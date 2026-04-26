package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/dsl"
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
	TenantID      string
	NASIP         string
	AcctSessionID string
	IMSI          string
}

type CoARequest struct {
	NASIP         string
	AcctSessionID string
	IMSI          string
	SessionID     string
	TenantID      string
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

// coaStatusUpdater is a narrow interface satisfied by *store.PolicyStore.
// Exposed as a seam so tests can inject a mock without a real DB.
type coaStatusUpdater interface {
	UpdateAssignmentCoAStatus(ctx context.Context, simID uuid.UUID, status string) error
	UpdateAssignmentCoAStatusWithReason(ctx context.Context, simID uuid.UUID, status string, failureReason *string) error
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
	policyStore       *store.PolicyStore
	simStore          *store.SIMStore
	sessionProvider   SessionProvider
	coaDispatcher     CoADispatcher
	coaStatusUpdater  coaStatusUpdater
	eventBus          *bus.EventBus
	jobStore          *store.JobStore
	logger            zerolog.Logger
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
	svc := &Service{
		policyStore:     policyStore,
		simStore:        simStore,
		sessionProvider: sessionProvider,
		coaDispatcher:   coaDispatcher,
		eventBus:        eventBus,
		jobStore:        jobStore,
		logger:          logger.With().Str("component", "rollout_service").Logger(),
	}
	if policyStore != nil {
		svc.coaStatusUpdater = policyStore
	}
	return svc
}

func (s *Service) SetSessionProvider(sp SessionProvider) {
	s.sessionProvider = sp
}

func (s *Service) SetCoADispatcher(cd CoADispatcher) {
	s.coaDispatcher = cd
}

// compiledMatchFromVersion extracts the CompiledMatch from a stored policy
// version by re-compiling its DSL source. Returns (nil, nil) when DSLContent
// is empty — `dsl.ToSQLPredicate` already maps a nil match to "TRUE".
//
// Re-compiling from DSLContent (rather than deserializing CompiledRules JSONB)
// keeps a single source of truth and avoids JSON-shape drift risk.
// The version was already validated at CreateVersion time, so any compile
// error here indicates corruption and MUST fail closed: surface the error to
// the caller so the rollout aborts instead of silently degrading to "TRUE"
// (which would migrate ALL active tenant SIMs — see FIX-230 Gate F-A6).
//
// dsl.CompileSource returns (nil, errs, nil) when the parser produces any
// "error"-severity diagnostic. We must inspect both return paths: the err
// channel for compiler-stage failures, AND the errs slice for parse-stage
// failures.
func compiledMatchFromVersion(version *store.PolicyVersion) (*dsl.CompiledMatch, error) {
	if version == nil || strings.TrimSpace(version.DSLContent) == "" {
		return nil, nil
	}
	compiled, errs, err := dsl.CompileSource(version.DSLContent)
	if err != nil {
		return nil, fmt.Errorf("rollout: re-compile dsl for version %s: %w", version.ID, err)
	}
	for _, e := range errs {
		if e.Severity == "error" {
			return nil, fmt.Errorf("rollout: stored dsl for version %s has parse error at line %d: %s", version.ID, e.Line, e.Message)
		}
	}
	if compiled == nil {
		return nil, fmt.Errorf("rollout: stored dsl for version %s did not compile (no error returned)", version.ID)
	}
	return &compiled.Match, nil
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

	// FIX-230 AC-3 + Gate F-A1: prefer the cached affected_sim_count when it
	// has been computed (CreateVersion populates it; a non-nil pointer is
	// authoritative — including an explicit zero meaning "no SIMs match").
	// Only fall back to a live predicate count when the cache is unset
	// (nil pointer) — this avoids a redundant count for tenants whose policy
	// legitimately matches zero SIMs.
	var totalSIMs int
	if version.AffectedSIMCount != nil {
		totalSIMs = *version.AffectedSIMCount
	} else {
		// Cache miss: translate the version's DSL MATCH into a parameterized
		// SQL predicate and count active SIMs that satisfy it. Empty MATCH →
		// predicate "TRUE" (counts ALL active tenant SIMs — preserves AC-5).
		match, mErr := compiledMatchFromVersion(version)
		if mErr != nil {
			return nil, fmt.Errorf("rollout: compile match for version %s: %w", version.ID, mErr)
		}
		predicate, predArgs, _, predErr := dsl.ToSQLPredicate(match, 1, 2)
		if predErr != nil {
			return nil, fmt.Errorf("rollout: translate dsl predicate: %w", predErr)
		}
		count, countErr := s.simStore.CountWithPredicate(ctx, tenantID, predicate, predArgs)
		if countErr != nil {
			return nil, fmt.Errorf("rollout: count sims with predicate: %w", countErr)
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
		PolicyID:          version.PolicyID,
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

	// FIX-230 AC-2/AC-6: compute the DSL→SQL predicate ONCE per ExecuteStage
	// invocation — it is identical for every batch of the rollout.
	//
	// Argument numbering note: SelectSIMsForStage binds $1=tenant, $2=rolloutID
	// unconditionally, then conditionally binds $3=previousVersionID when it is
	// non-nil. So the DSL args start at $3 (no prevVer) or $4 (with prevVer).
	version, vErr := s.policyStore.GetVersionWithTenant(ctx, rollout.PolicyVersionID, tenantID)
	if vErr != nil {
		return fmt.Errorf("rollout: load version for stage: %w", vErr)
	}
	match, mErr := compiledMatchFromVersion(version)
	if mErr != nil {
		return fmt.Errorf("rollout: compile match for version %s: %w", version.ID, mErr)
	}
	startArgIdx := 3
	if rollout.PreviousVersionID != nil {
		startArgIdx = 4
	}
	predicate, predArgs, _, predErr := dsl.ToSQLPredicate(match, 1, startArgIdx)
	if predErr != nil {
		return fmt.Errorf("rollout: translate dsl predicate: %w", predErr)
	}

	totalMigrated := rollout.MigratedSIMs
	targetReached := false

	for remaining > 0 {
		batchCount := batchSize
		if batchCount > remaining {
			batchCount = remaining
		}

		simIDs, err := s.policyStore.SelectSIMsForStage(ctx, tenantID, rollout.ID, rollout.PreviousVersionID, predicate, predArgs, batchCount)
		if err != nil {
			return fmt.Errorf("select sims for stage: %w", err)
		}

		if len(simIDs) == 0 {
			break
		}

		assigned, err := s.policyStore.AssignSIMsToVersion(ctx, simIDs, rollout.PolicyVersionID, rollout.ID, stage.Pct)
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

	targetReached = totalMigrated >= targetMigrated

	if targetReached {
		stages[stageIndex].Status = "completed"
	} else {
		stages[stageIndex].Status = "pending"
	}
	finalCount := totalMigrated
	stages[stageIndex].Migrated = &finalCount
	stages[stageIndex].SimCount = &finalCount

	stagesJSON, _ := json.Marshal(stages)
	if err := s.policyStore.UpdateRolloutProgress(ctx, rollout.ID, totalMigrated, stageIndex, stagesJSON); err != nil {
		return fmt.Errorf("update final progress: %w", err)
	}

	if targetReached && stage.Pct == 100 {
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
	// FIX-232 DEV-357: aborted is terminal — refuse to advance an aborted rollout.
	if rollout.State == "aborted" {
		return nil, store.ErrRolloutAborted
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
		if rollout.MigratedSIMs < rollout.TotalSIMs {
			nextStage = len(stages) - 1
			stages[nextStage].Status = "pending"
		} else {
			return nil, store.ErrRolloutCompleted
		}
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
	// FIX-232 DEV-357: aborted is terminal — refuse to rollback an already-aborted
	// rollout (assignments were intentionally retained at abort time).
	if rollout.State == "aborted" {
		return nil, 0, store.ErrRolloutAborted
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

// AbortRollout transitions a rollout to terminal state 'aborted' (FIX-232).
// Tenant scoping is enforced via GetRolloutByIDWithTenant before the global-by-id
// store call. Unlike RollbackRollout, abort does NOT revert assignments —
// already-migrated SIMs stay on the new version and the operator must create a
// new draft to retry. Errors propagate as typed sentinels for HTTP 422 mapping.
//
// reason is recorded in the audit log by the handler; the bus envelope reuses
// the existing policy.rollout_progress shape with state='aborted' (DEV-359).
func (s *Service) AbortRollout(ctx context.Context, tenantID, rolloutID uuid.UUID, reason string) (*store.PolicyRollout, error) {
	rollout, err := s.policyStore.GetRolloutByIDWithTenant(ctx, rolloutID, tenantID)
	if err != nil {
		return nil, err
	}

	switch rollout.State {
	case "completed":
		return nil, store.ErrRolloutCompleted
	case "rolled_back":
		return nil, store.ErrRolloutRolledBack
	case "aborted":
		return nil, store.ErrRolloutAborted
	}

	aborted, err := s.policyStore.AbortRollout(ctx, rolloutID)
	if err != nil {
		return nil, err
	}

	var stages []store.RolloutStage
	_ = json.Unmarshal(aborted.Stages, &stages)
	s.publishProgressWithState(ctx, aborted, stages, aborted.MigratedSIMs, aborted.CurrentStage, "aborted")

	return aborted, nil
}

func (s *Service) GetProgress(ctx context.Context, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error) {
	return s.policyStore.GetRolloutByIDWithTenant(ctx, rolloutID, tenantID)
}

func (s *Service) sendCoAForSIM(ctx context.Context, simID uuid.UUID) {
	if s.sessionProvider == nil || s.coaDispatcher == nil {
		s.writeCoAStatus(ctx, simID, CoAStatusNoSession)
		return
	}

	sessions, err := s.sessionProvider.GetSessionsForSIM(ctx, simID.String())
	if err != nil {
		s.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("get sessions for CoA")
		return
	}

	if len(sessions) == 0 {
		s.writeCoAStatus(ctx, simID, CoAStatusNoSession)
		return
	}

	s.writeCoAStatusWithReason(ctx, simID, CoAStatusQueued, nil)

	for _, sess := range sessions {
		result, coaErr := s.coaDispatcher.SendCoA(ctx, CoARequest{
			NASIP:         sess.NASIP,
			AcctSessionID: sess.AcctSessionID,
			IMSI:          sess.IMSI,
			SessionID:     sess.ID,
			TenantID:      sess.TenantID,
		})

		var status string
		var reason *string
		if coaErr != nil {
			s.logger.Warn().Err(coaErr).
				Str("sim_id", simID.String()).
				Str("session_id", sess.ID).
				Str("coa_status", CoAStatusFailed).
				Msg("CoA send failed")
			status = CoAStatusFailed
			r := classifyCoAError(coaErr)
			reason = &r
		} else if result != nil && result.Status != "ack" {
			status = CoAStatusFailed
			r := "coa rejected: " + truncateReason(result.Status)
			reason = &r
		} else {
			status = CoAStatusAcked
		}

		s.writeCoAStatusWithReason(ctx, simID, status, reason)
	}
}

// ResendCoA re-fires CoA for a SIM whose policy_assignments row was previously marked no_session.
// Public entry-point used by the session-started subscriber. The dedup gate (60s) is enforced
// upstream in the resender; this wrapper is a thin pass-through to the existing unexported helper.
func (s *Service) ResendCoA(ctx context.Context, simID uuid.UUID) error {
	s.sendCoAForSIM(ctx, simID)
	return nil
}

// writeCoAStatus persists a coa_status transition. Silently skips when no updater is wired
// (e.g., integration tests that don't need DB assertions).
func (s *Service) writeCoAStatus(ctx context.Context, simID uuid.UUID, status string) {
	if s.coaStatusUpdater == nil {
		return
	}
	if err := s.coaStatusUpdater.UpdateAssignmentCoAStatus(ctx, simID, status); err != nil {
		s.logger.Warn().Err(err).
			Str("sim_id", simID.String()).
			Str("coa_status", status).
			Msg("update CoA status")
	}
}

// writeCoAStatusWithReason persists a coa_status transition with an optional failure reason.
// reason is nil for non-failure states (clears any prior stale reason).
func (s *Service) writeCoAStatusWithReason(ctx context.Context, simID uuid.UUID, status string, reason *string) {
	if s.coaStatusUpdater == nil {
		return
	}
	if err := s.coaStatusUpdater.UpdateAssignmentCoAStatusWithReason(ctx, simID, status, reason); err != nil {
		s.logger.Warn().Err(err).
			Str("sim_id", simID.String()).
			Str("coa_status", status).
			Msg("update CoA status with reason")
	}
}

// truncateReason caps reason strings to 200 bytes to guard against oversized error messages.
func truncateReason(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// classifyCoAError returns a short human-readable failure reason from a CoA dispatch error.
func classifyCoAError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case containsAny(msg, "timeout", "timed out"):
		return "diameter timeout"
	case containsAny(msg, "no session", "session not found"):
		return "no session"
	case containsAny(msg, "unreachable", "connection refused", "connect: "):
		return "nas unreachable"
	case containsAny(msg, "nak", "rejected"):
		return "coa rejected"
	default:
		return truncateReason(msg)
	}
}

func containsAny(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
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

	// FIX-212 AC-6: policy.name lookup would require a cross-package resolver
	// that the rollout service doesn't own today. Fall back to a short tag
	// formed from the version id — FE can still render a distinct label
	// ("policy <short>") and the matching rollout row is retrievable via the
	// rollout_id meta field. Proper policy-name resolution is tracked in
	// FIX-240 (Notification Preferences) where the catalog wire-up happens.
	policyDisplay := shortPolicyTag(rollout.PolicyVersionID)
	env := bus.NewEnvelope("policy.rollout_progress", tenantID.String(), "info").
		WithSource("policy").
		WithTitle("Policy rollout progress").
		SetEntity("policy", rollout.PolicyVersionID.String(), policyDisplay).
		WithMeta("rollout_id", rollout.ID.String()).
		WithMeta("version_id", rollout.PolicyVersionID.String()).
		WithMeta("state", state).
		WithMeta("current_stage", currentStage).
		WithMeta("total_stages", len(stages)).
		WithMeta("total_sims", rollout.TotalSIMs).
		WithMeta("migrated_sims", migrated).
		WithMeta("progress_pct", progressPct).
		WithMeta("started_at", startedAt).
		WithMeta("stages", stages)

	if err := s.eventBus.Publish(ctx, bus.SubjectPolicyRolloutProgress, env); err != nil {
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

// shortPolicyTag returns a compact human-ish label for a policy-version UUID
// suitable for event entity.display_name. Format: "policy <first-8-chars>".
// A full policy.name resolver is out of scope for FIX-212 (FIX-240 adds the
// subscriber-side enrichment path once the catalog wire-up lands).
func shortPolicyTag(pvID uuid.UUID) string {
	s := pvID.String()
	if len(s) >= 8 {
		return "policy " + s[:8]
	}
	return "policy " + s
}
