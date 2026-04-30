package rollout_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// FIX-231 Task 9 — service-layer integration tests.
//
// Coverage:
//   - TestStuckRolloutReaper_HappyPath        — reaper drives a finished-but-not-flipped rollout to terminal state
//   - TestStuckRolloutReaper_GraceNotElapsed   — reaper does NOT touch in-grace rollouts
//
// Both tests SKIP cleanly when DATABASE_URL is unset.
//
// We exercise the real *store.PolicyStore + *store.JobStore + the
// StuckRolloutReaperProcessor.Process() entrypoint, so the test path covers
// the full SQL the production code executes (ListStuckRollouts,
// CompleteRollout transaction, Complete on jobs).

// testReaperPool returns a pgxpool.Pool bound to DATABASE_URL or nil.
// Mirror of testPolicyPool from internal/store/policy_test.go (different
// package, so we duplicate the helper rather than export it).
func testReaperPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// reaperFixture is the minimum DB graph needed: tenant + policy + active v1
// (the supersede target) + rolling_out v2 + a stuck rollout pointed at v2.
type reaperFixture struct {
	tenantID  uuid.UUID
	policyID  uuid.UUID
	v1ID      uuid.UUID
	v2ID      uuid.UUID
	rolloutID uuid.UUID
}

// seedReaperFixture wires the four parent rows. The rollout row's started_at
// / created_at are NOT set here — each test sets them via INTERVAL math so
// the grace test can exercise both sides of the cutoff.
func seedReaperFixture(t *testing.T, pool *pgxpool.Pool) reaperFixture {
	t.Helper()
	ctx := context.Background()
	var f reaperFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-reaper-'||gen_random_uuid()::text, 'fix231-reaper@test.argus')
		RETURNING id`).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-reaper-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state, activated_at)
		VALUES ($1, 1, 'allow all;', '{}', 'active', NOW() - INTERVAL '1 day')
		RETURNING id`, f.policyID).Scan(&f.v1ID); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 2, 'allow all;', '{}', 'rolling_out')
		RETURNING id`, f.policyID).Scan(&f.v2ID); err != nil {
		t.Fatalf("seed v2: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE policies SET current_version_id = $1 WHERE id = $2`, f.v1ID, f.policyID); err != nil {
		t.Fatalf("set current_version_id v1: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE policy_id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `UPDATE policies SET current_version_id = NULL WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// seedStuckRollout inserts a rollout for f.v2ID with the given INTERVAL
// expression for both started_at and created_at (so ListStuckRollouts'
// `COALESCE(completed_at, created_at) < NOW() - make_interval(mins => $1)`
// branch is exercised deterministically).
func seedStuckRollout(t *testing.T, pool *pgxpool.Pool, f *reaperFixture, ageInterval string) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, previous_version_id,
			strategy, stages, total_sims, migrated_sims, state, started_at, created_at)
		VALUES ($1, $2, $3, 'canary', '[]', 10, 10, 'in_progress',
		        NOW() - $4::interval, NOW() - $4::interval)
		RETURNING id`,
		f.policyID, f.v2ID, f.v1ID, ageInterval).Scan(&f.rolloutID); err != nil {
		t.Fatalf("seed rollout (age=%s): %v", ageInterval, err)
	}
}

// seedReaperJob creates a queued job row. The reaper's Process() needs a
// real Job whose ID exists in the jobs table because Complete() updates by id.
func seedReaperJob(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID) *store.Job {
	t.Helper()
	ctx := context.Background()
	js := store.NewJobStore(pool)
	j, err := js.CreateWithTenantID(ctx, tenantID, store.CreateJobParams{
		Type:     job.JobTypeStuckRolloutReaper,
		Priority: 3,
		Payload:  json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create reaper job: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM jobs WHERE id = $1`, j.ID)
	})
	return j
}

// =============================================================================
// Test 1 — Reaper happy path: stuck rollout > grace is reaped to completion
// =============================================================================
//
// Setup: in_progress rollout, total=migrated=10, age 15min (grace=10).
// Run: NewStuckRolloutReaperProcessor(grace=10).Process(ctx, fakeJob).
// Assert:
//   - rollout.state = 'completed'
//   - target version (v2) is now 'active'
//   - prior active version (v1) is now 'superseded'
//   - jobs.result JSON has reaped:1 (and ids includes our rollout id)
func TestStuckRolloutReaper_HappyPath(t *testing.T) {
	pool := testReaperPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	// FIX-231 F-A1 (Gate): no index workaround. The reaper drives
	// CompleteRollout, whose new supersede-first-then-activate ordering keeps
	// the `policy_active_version` partial unique index satisfied at every
	// statement boundary. This test now exercises the real production schema.
	f := seedReaperFixture(t, pool)
	seedStuckRollout(t, pool, &f, "15 minutes")
	jobRow := seedReaperJob(t, pool, f.tenantID)

	policyStore := store.NewPolicyStore(pool)
	jobStore := store.NewJobStore(pool)

	processor := job.NewStuckRolloutReaperProcessor(jobStore, policyStore, nil, 10, zerolog.Nop())

	if err := processor.Process(ctx, jobRow); err != nil {
		t.Fatalf("Process: %v", err)
	}

	var rolloutState string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_rollouts WHERE id = $1`, f.rolloutID).
		Scan(&rolloutState); err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	if rolloutState != "completed" {
		t.Errorf("rollout.state = %q, want 'completed'", rolloutState)
	}

	var v2State string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_versions WHERE id = $1`, f.v2ID).
		Scan(&v2State); err != nil {
		t.Fatalf("read v2: %v", err)
	}
	if v2State != "active" {
		t.Errorf("v2.state = %q, want 'active' (CompleteRollout should have flipped target)", v2State)
	}

	var v1State string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_versions WHERE id = $1`, f.v1ID).
		Scan(&v1State); err != nil {
		t.Fatalf("read v1: %v", err)
	}
	if v1State != "superseded" {
		t.Errorf("v1.state = %q, want 'superseded' (prior active should have been demoted)", v1State)
	}

	var jobState string
	var resultJSON json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT state, result FROM jobs WHERE id = $1`, jobRow.ID).
		Scan(&jobState, &resultJSON); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if jobState != "completed" {
		t.Errorf("job.state = %q, want 'completed'", jobState)
	}

	var result struct {
		Reaped  int      `json:"reaped"`
		Skipped int      `json:"skipped"`
		Failed  int      `json:"failed"`
		IDs     []string `json:"ids"`
	}
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	// On a shared dev DB the reaper may sweep other stuck rollouts that
	// pre-existed, so we assert a lower-bound (>=1) and confirm OUR rollout
	// is in the IDs list rather than pinning to an exact count.
	if result.Reaped < 1 {
		t.Errorf("result.reaped = %d, want >= 1; full=%+v", result.Reaped, result)
	}
	if result.Failed != 0 {
		t.Errorf("result.failed = %d, want 0; full=%+v", result.Failed, result)
	}
	found := false
	for _, id := range result.IDs {
		if id == f.rolloutID.String() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("our rollout id %s not in result.ids %v", f.rolloutID, result.IDs)
	}
}

// =============================================================================
// Test 2 — Reaper grace respected: rollout newer than grace is left alone
// =============================================================================
//
// Setup: in_progress rollout, total=migrated=10, age 5min, grace=10.
// Assert: ListStuckRollouts excludes the rollout (its age is inside grace),
// so Process() leaves rollout in_progress and v2 stays rolling_out.
//
// We assert state on OUR specific rollout to remain isolated from any other
// rollouts a shared dev DB might already contain.
func TestStuckRolloutReaper_GraceNotElapsed(t *testing.T) {
	pool := testReaperPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	f := seedReaperFixture(t, pool)
	seedStuckRollout(t, pool, &f, "5 minutes")
	jobRow := seedReaperJob(t, pool, f.tenantID)

	policyStore := store.NewPolicyStore(pool)
	jobStore := store.NewJobStore(pool)

	processor := job.NewStuckRolloutReaperProcessor(jobStore, policyStore, nil, 10, zerolog.Nop())

	if err := processor.Process(ctx, jobRow); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// Our rollout must still be in_progress.
	var rolloutState string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_rollouts WHERE id = $1`, f.rolloutID).
		Scan(&rolloutState); err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	if rolloutState != "in_progress" {
		t.Errorf("rollout.state = %q, want 'in_progress' (grace not elapsed)", rolloutState)
	}

	// v2 must remain rolling_out (untouched).
	var v2State string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_versions WHERE id = $1`, f.v2ID).
		Scan(&v2State); err != nil {
		t.Fatalf("read v2: %v", err)
	}
	if v2State != "rolling_out" {
		t.Errorf("v2.state = %q, want 'rolling_out'", v2State)
	}

	// v1 must remain active (no supersede).
	var v1State string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_versions WHERE id = $1`, f.v1ID).
		Scan(&v1State); err != nil {
		t.Fatalf("read v1: %v", err)
	}
	if v1State != "active" {
		t.Errorf("v1.state = %q, want 'active' (no supersede in grace window)", v1State)
	}

	// jobs.result must show OUR rollout was not reaped — but the result is a
	// global count so we only assert: no result IDs include our rollout id.
	var resultJSON json.RawMessage
	if err := pool.QueryRow(ctx, `SELECT result FROM jobs WHERE id = $1`, jobRow.ID).
		Scan(&resultJSON); err != nil {
		t.Fatalf("read job: %v", err)
	}
	var result struct {
		Reaped int      `json:"reaped"`
		IDs    []string `json:"ids"`
	}
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	for _, id := range result.IDs {
		if id == f.rolloutID.String() {
			t.Errorf("our in-grace rollout %s appeared in reaped ids %v", f.rolloutID, result.IDs)
		}
	}
}
