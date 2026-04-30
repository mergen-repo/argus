package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPolicyPool returns a pgxpool.Pool bound to the test database when
// DATABASE_URL is set; otherwise returns nil so callers can t.Skip. Mirrors
// testIPPoolPool in ippool_test.go.
func testPolicyPool(t *testing.T) *pgxpool.Pool {
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

func TestPolicyStruct(t *testing.T) {
	desc := "Test policy description"
	p := Policy{
		Name:        "test-policy",
		Description: &desc,
		Scope:       "global",
		State:       "active",
	}

	if p.Name != "test-policy" {
		t.Errorf("Name = %q, want %q", p.Name, "test-policy")
	}
	if p.Description == nil || *p.Description != desc {
		t.Errorf("Description = %v, want %q", p.Description, desc)
	}
	if p.Scope != "global" {
		t.Errorf("Scope = %q, want %q", p.Scope, "global")
	}
	if p.State != "active" {
		t.Errorf("State = %q, want %q", p.State, "active")
	}
	if p.CurrentVersionID != nil {
		t.Error("CurrentVersionID should be nil by default")
	}
	if p.ScopeRefID != nil {
		t.Error("ScopeRefID should be nil by default")
	}
}

func TestPolicyVersionStruct(t *testing.T) {
	compiled := json.RawMessage(`{"name":"test","version":"1.0","match":{},"rules":{}}`)
	v := PolicyVersion{
		Version:       1,
		DSLContent:    `POLICY "test" { RULES { bandwidth_down = 1mbps } }`,
		CompiledRules: compiled,
		State:         "draft",
	}

	if v.Version != 1 {
		t.Errorf("Version = %d, want %d", v.Version, 1)
	}
	if v.DSLContent == "" {
		t.Error("DSLContent should not be empty")
	}
	if v.CompiledRules == nil {
		t.Error("CompiledRules should not be nil")
	}
	if v.State != "draft" {
		t.Errorf("State = %q, want %q", v.State, "draft")
	}
	if v.ActivatedAt != nil {
		t.Error("ActivatedAt should be nil for draft")
	}
	if v.RolledBackAt != nil {
		t.Error("RolledBackAt should be nil for draft")
	}
	if v.AffectedSIMCount != nil {
		t.Error("AffectedSIMCount should be nil")
	}
}

func TestCreatePolicyParams(t *testing.T) {
	desc := "A policy"
	p := CreatePolicyParams{
		Name:          "my-policy",
		Description:   &desc,
		Scope:         "apn",
		DSLContent:    `POLICY "my-policy" {}`,
		CompiledRules: json.RawMessage(`{}`),
	}

	if p.Name != "my-policy" {
		t.Errorf("Name = %q, want %q", p.Name, "my-policy")
	}
	if p.Scope != "apn" {
		t.Errorf("Scope = %q, want %q", p.Scope, "apn")
	}
	if p.CreatedBy != nil {
		t.Error("CreatedBy should be nil by default")
	}
}

func TestUpdatePolicyParams(t *testing.T) {
	name := "updated-name"
	p := UpdatePolicyParams{
		Name: &name,
	}

	if p.Name == nil || *p.Name != "updated-name" {
		t.Error("Name should be set")
	}
	if p.Description != nil {
		t.Error("Description should be nil")
	}
	if p.State != nil {
		t.Error("State should be nil")
	}
}

func TestCreateVersionParams(t *testing.T) {
	p := CreateVersionParams{
		DSLContent:    `POLICY "test" {}`,
		CompiledRules: json.RawMessage(`{}`),
	}

	if p.DSLContent == "" {
		t.Error("DSLContent should not be empty")
	}
	if p.CompiledRules == nil {
		t.Error("CompiledRules should not be nil")
	}
}

func TestErrPolicyNotFound(t *testing.T) {
	if ErrPolicyNotFound.Error() != "store: policy not found" {
		t.Errorf("ErrPolicyNotFound = %q", ErrPolicyNotFound.Error())
	}
}

func TestErrPolicyNameExists(t *testing.T) {
	if ErrPolicyNameExists.Error() != "store: policy name already exists for this tenant" {
		t.Errorf("ErrPolicyNameExists = %q", ErrPolicyNameExists.Error())
	}
}

func TestErrPolicyVersionNotFound(t *testing.T) {
	if ErrPolicyVersionNotFound.Error() != "store: policy version not found" {
		t.Errorf("ErrPolicyVersionNotFound = %q", ErrPolicyVersionNotFound.Error())
	}
}

func TestErrPolicyInUse(t *testing.T) {
	if ErrPolicyInUse.Error() != "store: policy has assigned SIMs" {
		t.Errorf("ErrPolicyInUse = %q", ErrPolicyInUse.Error())
	}
}

func TestErrVersionNotDraft(t *testing.T) {
	if ErrVersionNotDraft.Error() != "store: version is not in draft state" {
		t.Errorf("ErrVersionNotDraft = %q", ErrVersionNotDraft.Error())
	}
}

func TestPolicyVersionCompiledRulesJSON(t *testing.T) {
	compiled := json.RawMessage(`{"name":"iot-fleet","version":"1.0","match":{"conditions":[{"field":"apn","op":"in","values":["iot.fleet"]}]},"rules":{"defaults":{"bandwidth_down":1000000},"when_blocks":[]}}`)
	v := PolicyVersion{
		CompiledRules: compiled,
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(v.CompiledRules, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal compiled rules: %v", err)
	}
	if parsed["name"] != "iot-fleet" {
		t.Errorf("parsed name = %v, want %q", parsed["name"], "iot-fleet")
	}
}

func TestPolicyVersionStates(t *testing.T) {
	validStates := []string{"draft", "active", "superseded", "archived"}
	for _, state := range validStates {
		v := PolicyVersion{State: state}
		if v.State != state {
			t.Errorf("State = %q, want %q", v.State, state)
		}
	}
}

func TestPolicyScopeValues(t *testing.T) {
	validScopes := []string{"global", "operator", "apn", "sim"}
	for _, scope := range validScopes {
		p := Policy{Scope: scope}
		if p.Scope != scope {
			t.Errorf("Scope = %q, want %q", p.Scope, scope)
		}
	}
}

func TestNewPolicyStore(t *testing.T) {
	s := NewPolicyStore(nil)
	if s == nil {
		t.Fatal("NewPolicyStore returned nil")
	}
}

// TestCompleteRollout_SupersedesAllPriorActiveVersionsOfPolicy (FIX-231 DEV-348)
// proves CompleteRollout demotes EVERY prior active version of the same policy,
// not just rollout.previous_version_id. The two-active pre-state is impossible
// under the production `policy_active_version` partial unique index — that is
// the WHOLE POINT: this test exercises the defence-in-depth supersede logic
// for the case where a prior release without the index left drift behind.
// Dropping the index here is unavoidable to construct the precondition; the
// other state-machine tests (TestCompleteRollout_AtomicTransition,
// TestStuckRolloutReaper_HappyPath) run with the index intact and verify the
// new supersede-first ordering directly. Index is recreated on cleanup.
func TestCompleteRollout_SupersedesAllPriorActiveVersionsOfPolicy(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	store := NewPolicyStore(pool)

	// Drop the partial unique index that would otherwise reject two active
	// versions, then recreate it in cleanup.
	if _, err := pool.Exec(ctx, `DROP INDEX IF EXISTS policy_active_version`); err != nil {
		t.Fatalf("drop policy_active_version: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`CREATE UNIQUE INDEX IF NOT EXISTS policy_active_version
			   ON policy_versions (policy_id) WHERE state = 'active'`)
	})

	// tenant
	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-supersede-'||gen_random_uuid()::text, 'fix231@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// policy
	var policyID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	// v1 + v2 both 'active' (the deliberate violation)
	var v1ID, v2ID, v3ID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state, activated_at)
		VALUES ($1, 1, 'allow all;', '{}', 'active', NOW() - INTERVAL '2 days')
		RETURNING id`, policyID).Scan(&v1ID); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state, activated_at)
		VALUES ($1, 2, 'allow all;', '{}', 'active', NOW() - INTERVAL '1 day')
		RETURNING id`, policyID).Scan(&v2ID); err != nil {
		t.Fatalf("seed v2: %v", err)
	}
	// v3 is 'rolling_out' — the target of the rollout we're about to complete.
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 3, 'allow all;', '{}', 'rolling_out')
		RETURNING id`, policyID).Scan(&v3ID); err != nil {
		t.Fatalf("seed v3: %v", err)
	}

	// matching rollout for v3
	var rolloutID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, previous_version_id,
			strategy, stages, total_sims, migrated_sims, state, started_at)
		VALUES ($1, $2, $3, 'staged', '[]', 0, 0, 'in_progress', NOW())
		RETURNING id`, policyID, v3ID, v2ID).Scan(&rolloutID); err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE id = $1`, rolloutID)
		_, _ = pool.Exec(cctx, `UPDATE policies SET current_version_id = NULL WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	if err := store.CompleteRollout(ctx, rolloutID); err != nil {
		t.Fatalf("CompleteRollout: %v", err)
	}

	// Assert state of all three versions.
	stateOf := func(id uuid.UUID) string {
		var s string
		if err := pool.QueryRow(ctx, `SELECT state FROM policy_versions WHERE id = $1`, id).Scan(&s); err != nil {
			t.Fatalf("read state for %s: %v", id, err)
		}
		return s
	}
	if got := stateOf(v1ID); got != "superseded" {
		t.Errorf("v1 state = %q, want superseded", got)
	}
	if got := stateOf(v2ID); got != "superseded" {
		t.Errorf("v2 state = %q, want superseded", got)
	}
	if got := stateOf(v3ID); got != "active" {
		t.Errorf("v3 state = %q, want active", got)
	}

	// policies.current_version_id must point to v3.
	var currentVersionID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT current_version_id FROM policies WHERE id = $1`, policyID).Scan(&currentVersionID); err != nil {
		t.Fatalf("read current_version_id: %v", err)
	}
	if currentVersionID == nil || *currentVersionID != v3ID {
		t.Errorf("current_version_id = %v, want %v", currentVersionID, v3ID)
	}
}

// TestGetActiveRolloutForPolicy_UsesNewPolicyIdColumn (FIX-231 DEV-345) verifies
// the rewritten query reads from policy_rollouts.policy_id directly rather than
// joining policy_versions.
func TestGetActiveRolloutForPolicy_UsesNewPolicyIdColumn(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	store := NewPolicyStore(pool)

	var tenantID, policyID, versionID, rolloutID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-active-rollout-'||gen_random_uuid()::text, 'fix231@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'rolling_out')
		RETURNING id`, policyID).Scan(&versionID); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at)
		VALUES ($1, $2, 'staged', '[]', 0, 0, 'in_progress', NOW())
		RETURNING id`, policyID, versionID).Scan(&rolloutID); err != nil {
		t.Fatalf("seed rollout: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE id = $1`, rolloutID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE id = $1`, versionID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	got, err := store.GetActiveRolloutForPolicy(ctx, policyID)
	if err != nil {
		t.Fatalf("GetActiveRolloutForPolicy: %v", err)
	}
	if got == nil {
		t.Fatal("expected rollout, got nil")
	}
	if got.ID != rolloutID {
		t.Errorf("rollout.ID = %s, want %s", got.ID, rolloutID)
	}
	if got.PolicyVersionID != versionID {
		t.Errorf("rollout.PolicyVersionID = %s, want %s", got.PolicyVersionID, versionID)
	}
	if got.State != "in_progress" {
		t.Errorf("rollout.State = %q, want in_progress", got.State)
	}
}

// TestListStuckRollouts_FiltersByGracePeriod (FIX-231 DEV-348 / Task 5) verifies
// that ListStuckRollouts returns only rollouts that are 'in_progress',
// migrated_sims >= total_sims, and inactive longer than the grace window.
func TestListStuckRollouts_FiltersByGracePeriod(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	store := NewPolicyStore(pool)

	var tenantID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-stuck-'||gen_random_uuid()::text, 'fix231@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Three separate policies — each gets exactly one in_progress rollout,
	// satisfying the policy_active_rollout partial unique index.
	seedPolicyAndVersion := func(label string) (pID, vID uuid.UUID) {
		if err := pool.QueryRow(ctx, `
			INSERT INTO policies (tenant_id, name, scope, state)
			VALUES ($1, $2||'-'||gen_random_uuid()::text, 'global', 'active')
			RETURNING id`, tenantID, label).Scan(&pID); err != nil {
			t.Fatalf("seed policy (%s): %v", label, err)
		}
		if err := pool.QueryRow(ctx, `
			INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
			VALUES ($1, 1, 'allow all;', '{}', 'rolling_out')
			RETURNING id`, pID).Scan(&vID); err != nil {
			t.Fatalf("seed policy_version (%s): %v", label, err)
		}
		return
	}

	pStuckOldID, vStuckOldID := seedPolicyAndVersion("stuck-old")
	pStuckNewID, vStuckNewID := seedPolicyAndVersion("stuck-new")
	pNotStuckID, vNotStuckID := seedPolicyAndVersion("not-stuck")

	// Rollout 1: stuck > grace (created 2 hours ago, grace = 60 min).
	var rolloutStuckOldID, rolloutStuckNewID, rolloutNotStuckID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at, created_at)
		VALUES ($1, $2, 'staged', '[]', 100, 100, 'in_progress',
		        NOW() - INTERVAL '2 hours', NOW() - INTERVAL '2 hours')
		RETURNING id`, pStuckOldID, vStuckOldID).Scan(&rolloutStuckOldID); err != nil {
		t.Fatalf("seed rollout (stuck old): %v", err)
	}
	// Rollout 2: stuck < grace (created 5 minutes ago).
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at, created_at)
		VALUES ($1, $2, 'staged', '[]', 100, 100, 'in_progress',
		        NOW() - INTERVAL '5 minutes', NOW() - INTERVAL '5 minutes')
		RETURNING id`, pStuckNewID, vStuckNewID).Scan(&rolloutStuckNewID); err != nil {
		t.Fatalf("seed rollout (stuck new): %v", err)
	}
	// Rollout 3: not stuck — total_sims > migrated_sims (still actively migrating).
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at, created_at)
		VALUES ($1, $2, 'staged', '[]', 100, 50, 'in_progress',
		        NOW() - INTERVAL '2 hours', NOW() - INTERVAL '2 hours')
		RETURNING id`, pNotStuckID, vNotStuckID).Scan(&rolloutNotStuckID); err != nil {
		t.Fatalf("seed rollout (not stuck): %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		for _, pID := range []uuid.UUID{pStuckOldID, pStuckNewID, pNotStuckID} {
			_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE policy_id = $1`, pID)
			_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, pID)
			_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, pID)
		}
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	ids, err := store.ListStuckRollouts(ctx, 60)
	if err != nil {
		t.Fatalf("ListStuckRollouts: %v", err)
	}

	// Only rolloutStuckOldID should be present (in our seeded set). Other
	// pre-existing stuck rollouts in the DB are out of scope of this assertion;
	// we filter to the ones we created.
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[rolloutStuckOldID] {
		t.Errorf("expected rolloutStuckOldID %s in result, got %v", rolloutStuckOldID, ids)
	}
	if seen[rolloutStuckNewID] {
		t.Errorf("rolloutStuckNewID %s should NOT be returned (within grace window)", rolloutStuckNewID)
	}
	if seen[rolloutNotStuckID] {
		t.Errorf("rolloutNotStuckID %s should NOT be returned (still migrating)", rolloutNotStuckID)
	}
}

// TestStartRollout_ConcurrentReturns422 (FIX-231 Task 4 / DEV-345) verifies that
// when two concurrent CreateRollout calls race past the service-layer precheck,
// exactly one succeeds and the other returns ErrRolloutInProgress via the
// policy_active_rollout partial unique index → pgconn 23505 mapping.
func TestStartRollout_ConcurrentReturns422(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)

	var tenantID, policyID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-concurrent-'||gen_random_uuid()::text, 'fix231@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-concurrent-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	var v1ID, v2ID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'draft')
		RETURNING id`, policyID).Scan(&v1ID); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 2, 'allow all;', '{}', 'draft')
		RETURNING id`, policyID).Scan(&v2ID); err != nil {
		t.Fatalf("seed v2: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE policy_id = $1`, policyID)
		_, _ = pool.Exec(cctx, `UPDATE policy_versions SET state = 'draft' WHERE policy_id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	stages, _ := json.Marshal([]RolloutStage{{Pct: 10, Status: "in_progress"}, {Pct: 50, Status: "pending"}, {Pct: 100, Status: "pending"}})

	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = st.CreateRollout(ctx, tenantID, CreateRolloutParams{
			PolicyID:        policyID,
			PolicyVersionID: v1ID,
			Strategy:        "canary",
			Stages:          stages,
			TotalSIMs:       100,
		})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = st.CreateRollout(ctx, tenantID, CreateRolloutParams{
			PolicyID:        policyID,
			PolicyVersionID: v2ID,
			Strategy:        "canary",
			Stages:          stages,
			TotalSIMs:       100,
		})
	}()
	wg.Wait()

	successes := 0
	inProgress := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrRolloutInProgress) {
			inProgress++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successes != 1 {
		t.Errorf("expected exactly 1 success, got %d (errs: %v, %v)", successes, errs[0], errs[1])
	}
	if inProgress != 1 {
		t.Errorf("expected exactly 1 ErrRolloutInProgress, got %d", inProgress)
	}
}

// =============================================================================
// FIX-232 DEV-357 — AbortRollout state-machine coverage
// =============================================================================
//
// These tests are DB-gated (skip cleanly when DATABASE_URL is unset). They
// exercise the new ErrRolloutAborted sentinel, the atomic state transition,
// and the cross-state guards (completed / rolled_back / aborted are mutually
// exclusive terminal states).

// seedAbortFixture builds the minimum graph for an AbortRollout test:
// tenant + policy + rolling_out version + in_progress rollout. Returns a
// cleanup-registered fixture; caller can mutate state via further SQL.
type abortFixture struct {
	tenantID  uuid.UUID
	policyID  uuid.UUID
	versionID uuid.UUID
	rolloutID uuid.UUID
}

func seedAbortFixture(t *testing.T, pool *pgxpool.Pool, label string) abortFixture {
	t.Helper()
	ctx := context.Background()
	var f abortFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix232-'||$1||'-'||gen_random_uuid()::text, 'fix232@test.argus')
		RETURNING id`, label).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-'||$2||'-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID, label).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'rolling_out')
		RETURNING id`, f.policyID).Scan(&f.versionID); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at)
		VALUES ($1, $2, 'canary', '[]', 100, 50, 'in_progress', NOW())
		RETURNING id`, f.policyID, f.versionID).Scan(&f.rolloutID); err != nil {
		t.Fatalf("seed rollout: %v", err)
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

// TestAbortRollout_HappyPath verifies an in_progress rollout transitions to
// state='aborted' with aborted_at set, and the returned struct reflects the new
// state. Migrated counter is unchanged (no revert).
func TestAbortRollout_HappyPath(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "happy")

	got, err := st.AbortRollout(ctx, f.rolloutID)
	if err != nil {
		t.Fatalf("AbortRollout: %v", err)
	}
	if got == nil {
		t.Fatal("expected rollout, got nil")
	}
	if got.State != "aborted" {
		t.Errorf("State = %q, want aborted", got.State)
	}
	if got.AbortedAt == nil {
		t.Error("AbortedAt is nil, want non-nil")
	}
	if got.MigratedSIMs != 50 {
		t.Errorf("MigratedSIMs = %d, want 50 (abort must not revert)", got.MigratedSIMs)
	}

	// Re-read the row to confirm DB state matches the returned struct.
	var dbState string
	var dbAbortedAt *uuid.UUID // dummy — we just want non-NULL
	_ = dbAbortedAt
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_rollouts WHERE id = $1`, f.rolloutID).Scan(&dbState); err != nil {
		t.Fatalf("read back state: %v", err)
	}
	if dbState != "aborted" {
		t.Errorf("DB state = %q, want aborted", dbState)
	}
}

// TestAbortRollout_AlreadyCompleted_ReturnsErrCompleted verifies that calling
// abort on a completed rollout returns ErrRolloutCompleted (terminal state
// guard — completed wins, abort cannot override).
func TestAbortRollout_AlreadyCompleted_ReturnsErrCompleted(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "completed")

	if _, err := pool.Exec(ctx, `
		UPDATE policy_rollouts SET state = 'completed', completed_at = NOW()
		WHERE id = $1`, f.rolloutID); err != nil {
		t.Fatalf("manually set completed: %v", err)
	}

	_, err := st.AbortRollout(ctx, f.rolloutID)
	if !errors.Is(err, ErrRolloutCompleted) {
		t.Errorf("err = %v, want ErrRolloutCompleted", err)
	}
}

// TestAbortRollout_AlreadyAborted_ReturnsErrAborted verifies idempotency —
// the second abort call returns ErrRolloutAborted instead of silently
// re-stamping aborted_at.
func TestAbortRollout_AlreadyAborted_ReturnsErrAborted(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "twice")

	if _, err := st.AbortRollout(ctx, f.rolloutID); err != nil {
		t.Fatalf("first AbortRollout: %v", err)
	}

	_, err := st.AbortRollout(ctx, f.rolloutID)
	if !errors.Is(err, ErrRolloutAborted) {
		t.Errorf("second abort err = %v, want ErrRolloutAborted", err)
	}
}

// TestAbortRollout_AlreadyRolledBack_ReturnsErrRolledBack verifies the
// rolled_back terminal state also blocks abort.
func TestAbortRollout_AlreadyRolledBack_ReturnsErrRolledBack(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "rolledback")

	if _, err := pool.Exec(ctx, `
		UPDATE policy_rollouts SET state = 'rolled_back', rolled_back_at = NOW()
		WHERE id = $1`, f.rolloutID); err != nil {
		t.Fatalf("manually set rolled_back: %v", err)
	}

	_, err := st.AbortRollout(ctx, f.rolloutID)
	if !errors.Is(err, ErrRolloutRolledBack) {
		t.Errorf("err = %v, want ErrRolloutRolledBack", err)
	}
}

// TestAbortRollout_FromPending verifies that a rollout still in 'pending'
// state (no stage advanced yet, started_at NULL) can be aborted. This is the
// pre-flight cancel branch — the operator clicks Stop before the first stage
// kicks off; the abort must succeed and stamp aborted_at without regard to
// started_at being NULL.
func TestAbortRollout_FromPending(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "pending")

	// Force state back to 'pending' and clear started_at — seedAbortFixture
	// inserts in_progress for the dominant case, so we override here.
	if _, err := pool.Exec(ctx, `
		UPDATE policy_rollouts
		   SET state = 'pending', started_at = NULL, migrated_sims = 0
		 WHERE id = $1`, f.rolloutID); err != nil {
		t.Fatalf("force pending: %v", err)
	}

	got, err := st.AbortRollout(ctx, f.rolloutID)
	if err != nil {
		t.Fatalf("AbortRollout from pending: %v", err)
	}
	if got == nil {
		t.Fatal("expected rollout, got nil")
	}
	if got.State != "aborted" {
		t.Errorf("State = %q, want aborted", got.State)
	}
	if got.AbortedAt == nil {
		t.Error("AbortedAt is nil, want non-nil")
	}
	if got.MigratedSIMs != 0 {
		t.Errorf("MigratedSIMs = %d, want 0 (no SIMs migrated yet)", got.MigratedSIMs)
	}
}

// TestAbortRollout_NotFound_ReturnsErrNotFound verifies a non-existent
// rollout id returns ErrRolloutNotFound (not a generic SQL error).
func TestAbortRollout_NotFound_ReturnsErrNotFound(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)

	_, err := st.AbortRollout(ctx, uuid.New())
	if !errors.Is(err, ErrRolloutNotFound) {
		t.Errorf("err = %v, want ErrRolloutNotFound", err)
	}
}

// TestRollbackRollout_RejectsAborted verifies the new aborted-state guard in
// RollbackRollout. After abort, rollback returns ErrRolloutAborted instead of
// re-flipping the rollout to rolled_back (which would also revert assignments
// — undesired since the operator already chose to keep them).
func TestRollbackRollout_RejectsAborted(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "rb-aborted")

	if _, err := st.AbortRollout(ctx, f.rolloutID); err != nil {
		t.Fatalf("AbortRollout: %v", err)
	}

	err := st.RollbackRollout(ctx, f.rolloutID)
	if !errors.Is(err, ErrRolloutAborted) {
		t.Errorf("RollbackRollout after abort: err = %v, want ErrRolloutAborted", err)
	}
}

// TestCompleteRollout_RejectsAborted verifies the new aborted-state guard in
// CompleteRollout. After abort, complete returns ErrRolloutAborted instead of
// activating the partially-rolled-out version.
func TestCompleteRollout_RejectsAborted(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "cm-aborted")

	if _, err := st.AbortRollout(ctx, f.rolloutID); err != nil {
		t.Fatalf("AbortRollout: %v", err)
	}

	err := st.CompleteRollout(ctx, f.rolloutID)
	if !errors.Is(err, ErrRolloutAborted) {
		t.Errorf("CompleteRollout after abort: err = %v, want ErrRolloutAborted", err)
	}
}

// ---------------------------------------------------------------------------
// FIX-233 T8 — AssignSIMsToVersion persists stage_pct (upsert)
// ---------------------------------------------------------------------------

// TestPolicyStore_AssignSIMsToVersion_StagePct verifies that AssignSIMsToVersion
// writes stage_pct to policy_assignments and that a second call with a different
// stagePct updates (upserts) the existing row rather than inserting a duplicate.
func TestPolicyStore_AssignSIMsToVersion_StagePct(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedAbortFixture(t, pool, "stage-pct")

	// We need an operator and a SIM to assign.
	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator row available: %v", err)
	}

	// Insert a minimal APN to satisfy the SIM FK (may be nil; use apn_id=NULL variant).
	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, apn_type, state)
		VALUES ($1, $2, 'test-apn-stg-'||gen_random_uuid()::text, 'iot', 'active')
		RETURNING id`, f.tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM apns WHERE id = $1`, apnID)
	})

	// Insert a SIM (policy_version_id starts NULL; the trigger will set it after assignment).
	var simID uuid.UUID
	nonce := uuid.New().ID() % 1_000_000_000
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered')
		RETURNING id`,
		f.tenantID, operatorID, apnID,
		fmt.Sprintf("89933%09d", nonce),
		fmt.Sprintf("28633%08d", nonce%100_000_000),
	).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM policy_assignments WHERE sim_id = $1`, simID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sims WHERE id = $1`, simID)
	})

	// First assignment: stagePct=10
	n, err := st.AssignSIMsToVersion(ctx, []uuid.UUID{simID}, f.versionID, f.rolloutID, 10)
	if err != nil {
		t.Fatalf("AssignSIMsToVersion(stagePct=10): %v", err)
	}
	if n != 1 {
		t.Errorf("assigned = %d, want 1", n)
	}

	// Verify stage_pct=10 was persisted and exactly one row exists.
	var stagePct int
	var rowCount int
	if err := pool.QueryRow(ctx,
		`SELECT stage_pct FROM policy_assignments WHERE rollout_id = $1 AND sim_id = $2`,
		f.rolloutID, simID).Scan(&stagePct); err != nil {
		t.Fatalf("read stage_pct after first assign: %v", err)
	}
	if stagePct != 10 {
		t.Errorf("stage_pct = %d, want 10", stagePct)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM policy_assignments WHERE sim_id = $1`, simID).Scan(&rowCount); err != nil {
		t.Fatalf("count rows after first assign: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("row count after first assign = %d, want 1", rowCount)
	}

	// Second assignment: same simID, stagePct=100 → ON CONFLICT must UPDATE.
	n2, err := st.AssignSIMsToVersion(ctx, []uuid.UUID{simID}, f.versionID, f.rolloutID, 100)
	if err != nil {
		t.Fatalf("AssignSIMsToVersion(stagePct=100): %v", err)
	}
	if n2 != 1 {
		t.Errorf("assigned (upsert) = %d, want 1", n2)
	}

	// Verify stage_pct updated to 100 and still exactly one row.
	if err := pool.QueryRow(ctx,
		`SELECT stage_pct FROM policy_assignments WHERE rollout_id = $1 AND sim_id = $2`,
		f.rolloutID, simID).Scan(&stagePct); err != nil {
		t.Fatalf("read stage_pct after upsert: %v", err)
	}
	if stagePct != 100 {
		t.Errorf("stage_pct after upsert = %d, want 100", stagePct)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM policy_assignments WHERE sim_id = $1`, simID).Scan(&rowCount); err != nil {
		t.Fatalf("count rows after upsert: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("row count after upsert = %d, want 1 (must be upsert, not double-insert)", rowCount)
	}
}

// ---------------------------------------------------------------------------
// FIX-234 T7 AC-2 — chk_coa_status CHECK constraint rejects invalid values
// ---------------------------------------------------------------------------

// TestPolicyStore_CoAStatusCheckConstraint_RejectsInvalid verifies that the
// chk_coa_status CHECK constraint (migration 20260430000001) rejects any
// coa_status value outside the canonical enum set.
// DB-gated: skips cleanly when DATABASE_URL is unset.
func TestPolicyStore_CoAStatusCheckConstraint_RejectsInvalid(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	// Seed a minimal row we can try to UPDATE.
	// Re-use seedAbortFixture which creates tenant + policy + version + rollout.
	f := seedAbortFixture(t, pool, "chk-coa-status")

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator row available: %v", err)
	}

	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, apn_type, state)
		VALUES ($1, $2, 'test-apn-chk-'||gen_random_uuid()::text, 'iot', 'active')
		RETURNING id`, f.tenantID, operatorID).Scan(&apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM apns WHERE id = $1`, apnID)
	})

	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("8901%016d", nonce)
	imsi := fmt.Sprintf("2340%011d", nonce)
	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'ordered')
		RETURNING id`, f.tenantID, operatorID, apnID, iccid, imsi).Scan(&simID); err != nil {
		t.Fatalf("seed sim: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM policy_assignments WHERE sim_id = $1`, simID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM sims WHERE id = $1`, simID)
	})

	// Insert a valid policy_assignment row for this SIM.
	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, coa_status)
		VALUES ($1, $2, $3, 'pending')`, simID, f.versionID, f.rolloutID); err != nil {
		t.Fatalf("seed policy_assignment: %v", err)
	}

	// Attempt to UPDATE with an invalid coa_status value — must be rejected by chk_coa_status.
	_, err := pool.Exec(ctx, `
		UPDATE policy_assignments SET coa_status = 'invalid_state' WHERE sim_id = $1`, simID)
	if err == nil {
		t.Fatal("expected check constraint violation for coa_status='invalid_state', got nil error")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got: %T %v", err, err)
	}
	// SQLSTATE 23514 = check_violation
	if pgErr.Code != "23514" {
		t.Errorf("expected SQLSTATE 23514 (check_violation), got %q", pgErr.Code)
	}
	if pgErr.ConstraintName != "chk_coa_status" {
		t.Errorf("expected constraint name %q, got %q", "chk_coa_status", pgErr.ConstraintName)
	}
}
