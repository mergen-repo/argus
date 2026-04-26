package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
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
