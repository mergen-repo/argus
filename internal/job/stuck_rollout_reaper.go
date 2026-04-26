package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	minStuckRolloutGraceMinutes = 5
	maxStuckRolloutGraceMinutes = 120
)

// stuckRolloutPolicyStore is the subset of *store.PolicyStore the reaper
// depends on. Defining it as an interface (PAT-019) lets unit tests inject
// fakes without spinning up Postgres. Production wires `*store.PolicyStore`
// (concrete) — the constructor takes the interface, not a typed-nil pointer,
// so there is no nil-interface-with-nil-typed-pointer hazard.
type stuckRolloutPolicyStore interface {
	ListStuckRollouts(ctx context.Context, graceMinutes int) ([]uuid.UUID, error)
	CompleteRollout(ctx context.Context, rolloutID uuid.UUID) error
	GetRolloutByID(ctx context.Context, rolloutID uuid.UUID) (*store.PolicyRollout, error)
	GetTenantIDForRollout(ctx context.Context, rolloutID uuid.UUID) (uuid.UUID, error)
}

type StuckRolloutReaperProcessor struct {
	jobs         *store.JobStore
	policyStore  stuckRolloutPolicyStore
	eventBus     *bus.EventBus
	graceMinutes int
	logger       zerolog.Logger
}

// NewStuckRolloutReaperProcessor wires the stuck-rollout reaper.
//
// PAT-017 wiring trace: env ARGUS_STUCK_ROLLOUT_GRACE_MINUTES →
// cfg.StuckRolloutGraceMinutes → this constructor → p.graceMinutes →
// ListStuckRollouts SQL ($1 of make_interval(mins => $1)).
//
// The grace minutes value is clamped to [5, 120] defensively here even
// though the config layer also clamps it — a misconfigured deploy or a
// test passing an out-of-range value should never reach the SQL.
func NewStuckRolloutReaperProcessor(
	jobs *store.JobStore,
	policyStore stuckRolloutPolicyStore,
	eventBus *bus.EventBus,
	graceMinutes int,
	logger zerolog.Logger,
) *StuckRolloutReaperProcessor {
	if graceMinutes < minStuckRolloutGraceMinutes {
		graceMinutes = minStuckRolloutGraceMinutes
	}
	if graceMinutes > maxStuckRolloutGraceMinutes {
		graceMinutes = maxStuckRolloutGraceMinutes
	}
	return &StuckRolloutReaperProcessor{
		jobs:         jobs,
		policyStore:  policyStore,
		eventBus:     eventBus,
		graceMinutes: graceMinutes,
		logger:       logger.With().Str("processor", JobTypeStuckRolloutReaper).Logger(),
	}
}

func (p *StuckRolloutReaperProcessor) Type() string {
	return JobTypeStuckRolloutReaper
}

// stuckReaperResult is the aggregate result JSON written to jobs.result
// when the reaper finishes. Keys are stable contract: dashboards / alert
// rules may key on `failed > 0` so the field names must not change without
// a migration on the consumer side.
type stuckReaperResult struct {
	Reaped  int      `json:"reaped"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	IDs     []string `json:"ids,omitempty"`
}

func (p *StuckRolloutReaperProcessor) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().
		Str("job_id", j.ID.String()).
		Int("grace_minutes", p.graceMinutes).
		Msg("starting stuck rollout reaper sweep")

	ids, err := p.policyStore.ListStuckRollouts(ctx, p.graceMinutes)
	if err != nil {
		return fmt.Errorf("stuck_rollout_reaper: list: %w", err)
	}

	var reaped, skipped, failed int
	var reapedIDs []string

	for _, id := range ids {
		if cerr := p.policyStore.CompleteRollout(ctx, id); cerr != nil {
			// Race signals after FIX-231 F-A2 (Gate) idempotency guard:
			//   - ErrRolloutNotFound: rollout row deleted (manual rollback
			//     hard-removed it)
			//   - ErrRolloutRolledBack: rollout was rolled back between
			//     ListStuckRollouts and our CompleteRollout call —
			//     correctly NOT reaped, must NOT count as failure
			// All three "skip" outcomes (including F-A2's nil return on
			// already-completed) collapse to skipped++ so the failed count
			// remains a true error signal that dashboards/alerts can key on.
			if errors.Is(cerr, store.ErrRolloutNotFound) ||
				errors.Is(cerr, store.ErrRolloutRolledBack) {
				p.logger.Info().
					Str("rollout_id", id.String()).
					Str("reason", cerr.Error()).
					Msg("stuck_rollout_reaper: rollout in non-reapable terminal state; skipping")
				skipped++
				continue
			}
			p.logger.Error().
				Err(cerr).
				Str("rollout_id", id.String()).
				Msg("stuck_rollout_reaper: complete failed; continuing with next")
			failed++
			continue
		}
		reaped++
		reapedIDs = append(reapedIDs, id.String())

		// Best-effort: publish a rollout_progress event with state=completed
		// so connected operators get the WebSocket update without waiting
		// for the next page reload. Failure here must NOT roll back the
		// reap — the database row is already in its terminal state.
		p.publishCompletion(ctx, id)
	}

	result := stuckReaperResult{
		Reaped:  reaped,
		Skipped: skipped,
		Failed:  failed,
		IDs:     reapedIDs,
	}
	resultJSON, mErr := json.Marshal(result)
	if mErr != nil {
		return fmt.Errorf("stuck_rollout_reaper: marshal result: %w", mErr)
	}

	// FIX-231 F-A8 (Gate): when at least one CompleteRollout call failed,
	// surface that into jobs.error_report so /jobs dashboards and alert
	// rules can distinguish a perfect sweep from a partial one. The result
	// JSON still carries per-bucket counts (reaped/skipped/failed) for
	// detail. JobStore.Complete takes json.RawMessage for the error report
	// so we wrap the message in a small JSON object for consistency with
	// other processors.
	var errorReport json.RawMessage
	if failed > 0 {
		errReportPayload := map[string]interface{}{
			"message": fmt.Sprintf("stuck_rollout_reaper: %d rollout(s) failed during sweep", failed),
			"failed":  failed,
			"reaped":  reaped,
			"skipped": skipped,
		}
		erBytes, erErr := json.Marshal(errReportPayload)
		if erErr != nil {
			return fmt.Errorf("stuck_rollout_reaper: marshal error report: %w", erErr)
		}
		errorReport = erBytes
	}
	if err := p.jobs.Complete(ctx, j.ID, errorReport, resultJSON); err != nil {
		return fmt.Errorf("stuck_rollout_reaper: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":  j.ID.String(),
			"type":    JobTypeStuckRolloutReaper,
			"state":   "completed",
			"reaped":  reaped,
			"skipped": skipped,
			"failed":  failed,
		})
	}

	p.logger.Info().
		Int("reaped", reaped).
		Int("skipped", skipped).
		Int("failed", failed).
		Msg("stuck rollout reaper sweep completed")

	return nil
}

// publishCompletion emits a policy.rollout_progress event mirroring the
// shape from rollout/service.go:publishProgressWithState. The helper there
// is unexported and lives in another package, so we inline the minimal
// subset (rollout_id, version_id, state=completed, totals). On any failure
// we log and return — the rollout is already complete in the database.
func (p *StuckRolloutReaperProcessor) publishCompletion(ctx context.Context, rolloutID uuid.UUID) {
	if p.eventBus == nil {
		return
	}
	r, gerr := p.policyStore.GetRolloutByID(ctx, rolloutID)
	if gerr != nil || r == nil {
		p.logger.Warn().
			Err(gerr).
			Str("rollout_id", rolloutID.String()).
			Msg("stuck_rollout_reaper: post-reap rollout fetch failed; skipping bus publish")
		return
	}
	tenantID, terr := p.policyStore.GetTenantIDForRollout(ctx, rolloutID)
	if terr != nil {
		p.logger.Warn().
			Err(terr).
			Str("rollout_id", rolloutID.String()).
			Msg("stuck_rollout_reaper: tenant resolve failed; skipping bus publish")
		return
	}

	progressPct := 0.0
	if r.TotalSIMs > 0 {
		progressPct = math.Round(float64(r.MigratedSIMs)/float64(r.TotalSIMs)*10000) / 100
	}
	startedAt := ""
	if r.StartedAt != nil {
		startedAt = r.StartedAt.Format(time.RFC3339)
	}

	// FIX-231 F-A3 (Gate): entity ref is the policy id (from r.PolicyID, the
	// denorm column scanned in F-A4), NOT the policy_version_id. Bus
	// envelopes claim that the entity id identifies the policy entity, so
	// downstream consumers (notifications, audit, FE drill-down) can resolve
	// it via the policy catalog. version_id is preserved as meta for
	// detail-level views.
	env := bus.NewEnvelope("policy.rollout_progress", tenantID.String(), "info").
		WithSource("policy").
		WithTitle("Policy rollout progress").
		SetEntity("policy", r.PolicyID.String(), "policy "+r.PolicyID.String()[:8]).
		WithMeta("policy_id", r.PolicyID.String()).
		WithMeta("rollout_id", r.ID.String()).
		WithMeta("version_id", r.PolicyVersionID.String()).
		WithMeta("state", "completed").
		WithMeta("current_stage", r.CurrentStage).
		WithMeta("total_sims", r.TotalSIMs).
		WithMeta("migrated_sims", r.MigratedSIMs).
		WithMeta("progress_pct", progressPct).
		WithMeta("started_at", startedAt).
		WithMeta("reaped_by", "stuck_rollout_reaper")

	if err := p.eventBus.Publish(ctx, bus.SubjectPolicyRolloutProgress, env); err != nil {
		p.logger.Warn().
			Err(err).
			Str("rollout_id", rolloutID.String()).
			Msg("stuck_rollout_reaper: publish completion failed (rollout still completed in DB)")
	}
}
