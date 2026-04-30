package rollout_test

// FIX-230 Task 6 — end-to-end integration tests proving the DSL MATCH filter
// is honoured throughout StartRollout + ExecuteStage (AC-7, AC-8).
//
// Both tests are DB-gated: they SKIP cleanly when DATABASE_URL is unset.
//
// Test A: TestRollout_DSLMatchHonored_AC7
//   - 7 SIMs on "data.demo" APN + 5 control SIMs on "corporate.lan"
//   - Version DSL: MATCH { apn = "data.demo" }, affected_sim_count = 7
//   - StartRollout(stagePcts=[1,50,100]): total_sims must be 7
//   - Stage-0 (1%): ceil(7*1/100) = 1 SIM migrated, and that SIM has apn_id=data.demo
//
// Test B: TestRollout_NoDSLMatch_RegressionAC8
//   - 5 SIMs (various APNs), all active
//   - Version DSL: "" (empty) → predicate "TRUE" → apply to all (AC-5)
//   - StartRollout(stagePcts=[100]): total_sims = 5, all 5 migrated

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/policy/rollout"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// dslMatchPool returns a pgxpool connected to DATABASE_URL or nil.
// Each test calls this and t.Skip()s when nil is returned.
func dslMatchPool(t *testing.T) *pgxpool.Pool {
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

// dslMatchFixture holds IDs for the base rows (tenant, operator, policy).
type dslMatchFixture struct {
	tenantID   uuid.UUID
	operatorID uuid.UUID
	policyID   uuid.UUID
}

// seedDSLMatchBase creates tenant + picks an existing operator + creates
// a policy. It registers cleanup for those rows. Caller seeds APNs/SIMs
// and adds their own cleanup before this one runs (LIFO order).
func seedDSLMatchBase(t *testing.T, pool *pgxpool.Pool) dslMatchFixture {
	t.Helper()
	ctx := context.Background()
	var f dslMatchFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix230-dsl-'||gen_random_uuid()::text, 'fix230-dsl@test.argus')
		RETURNING id`).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&f.operatorID); err != nil {
		t.Fatalf("no operator row available: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-dsl-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `UPDATE policies SET current_version_id = NULL WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE policy_id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// seedAPN inserts one APN and returns its ID.
func seedAPN(t *testing.T, pool *pgxpool.Pool, tenantID, operatorID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var apnID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, $3, $3, 'iot', 'active')
		RETURNING id`, tenantID, operatorID, name).Scan(&apnID); err != nil {
		t.Fatalf("seed apn %q: %v", name, err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE apn_id = $1`, apnID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE id = $1`, apnID)
	})
	return apnID
}

// seedActiveSIM inserts one SIM with state='active' and returns its ID.
func seedActiveSIM(t *testing.T, pool *pgxpool.Pool, tenantID, operatorID uuid.UUID, apnID *uuid.UUID, idx int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nonce := uuid.New().ID() % 1_000_000_000
	iccid := fmt.Sprintf("89906%02d%09d", idx%100, nonce)
	imsi := fmt.Sprintf("29096%02d%08d", idx%100, nonce%100_000_000)
	var simID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'active')
		RETURNING id`, tenantID, operatorID, apnID, iccid, imsi).Scan(&simID); err != nil {
		t.Fatalf("seed active sim %d: %v", idx, err)
	}
	return simID
}

// =============================================================================
// Test A — DSL MATCH { apn = "data.demo" } scopes rollout to 7 SIMs (not 12)
// =============================================================================

func TestRollout_DSLMatchHonored_AC7(t *testing.T) {
	pool := dslMatchPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	f := seedDSLMatchBase(t, pool)

	// Seed two APNs — only "data.demo" is in scope.
	dataDemoID := seedAPN(t, pool, f.tenantID, f.operatorID, "data.demo")
	corpLanID := seedAPN(t, pool, f.tenantID, f.operatorID, "corporate.lan")

	// 7 SIMs on data.demo (in-scope).
	for i := 0; i < 7; i++ {
		seedActiveSIM(t, pool, f.tenantID, f.operatorID, &dataDemoID, i)
	}
	// 5 SIMs on corporate.lan (control — must NOT be migrated).
	for i := 0; i < 5; i++ {
		seedActiveSIM(t, pool, f.tenantID, f.operatorID, &corpLanID, 100+i)
	}

	// policy_assignments cleanup (written by ExecuteStage via AssignSIMsToVersion)
	// must run before APN cleanup (FK: policy_assignments.sim_id → sims.apn_id).
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `
			DELETE FROM policy_assignments
			WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id = $1)`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, f.tenantID)
	})

	// Build valid DSL with MATCH { apn = "data.demo" }.
	dslSrc := `POLICY "dsl-match-test" {
		MATCH { apn = "data.demo" }
		RULES { bandwidth_down = 1mbps }
	}`

	// FIX-230 Gate F-A5: persist genuine compiled_rules JSON via the production
	// compiler instead of a placeholder `{}` literal. Future code paths that
	// deserialize compiled_rules must see the real CompiledPolicy shape.
	compiled, dslErrs, compileErr := dsl.CompileSource(dslSrc)
	if compileErr != nil {
		t.Fatalf("CompileSource: %v", compileErr)
	}
	for _, e := range dslErrs {
		if e.Severity == "error" {
			t.Fatalf("CompileSource diagnostics: %s (line %d)", e.Message, e.Line)
		}
	}
	compiledJSON, err := json.Marshal(compiled)
	if err != nil {
		t.Fatalf("marshal compiled rules: %v", err)
	}

	affectedCount := 7
	policyStore := store.NewPolicyStore(pool)
	version, err := policyStore.CreateVersion(ctx, store.CreateVersionParams{
		PolicyID:         f.policyID,
		DSLContent:       dslSrc,
		CompiledRules:    compiledJSON,
		AffectedSIMCount: &affectedCount,
	})
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// Verify that affected_sim_count was stored.
	if version.AffectedSIMCount == nil || *version.AffectedSIMCount != 7 {
		t.Fatalf("affected_sim_count = %v, want 7", version.AffectedSIMCount)
	}

	simStore := store.NewSIMStore(pool)
	svc := rollout.NewService(policyStore, simStore, nil, nil, nil, nil, zerolog.Nop())

	stagePcts := []int{1, 50, 100}
	ro, err := svc.StartRollout(ctx, f.tenantID, version.ID, stagePcts, nil)
	if err != nil {
		t.Fatalf("StartRollout: %v", err)
	}

	// AC-7: total_sims must be exactly 7 (not 12).
	if ro.TotalSIMs != 7 {
		t.Errorf("total_sims = %d, want 7 (DSL MATCH should exclude corporate.lan SIMs)", ro.TotalSIMs)
	}

	// Stage-0 (1%) is executed synchronously inside StartRollout (stageTarget = ceil(7*1/100) = 1).
	// Verify migrated_sims = 1.
	if ro.MigratedSIMs != 1 {
		t.Errorf("migrated_sims = %d, want 1 after stage-0 (1%% of 7)", ro.MigratedSIMs)
	}

	// Verify the 1 migrated SIM belongs to data.demo (DSL filter was honoured).
	var migrated int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM policy_assignments pa
		JOIN sims s ON s.id = pa.sim_id
		WHERE pa.policy_version_id = $1
		  AND s.apn_id = $2`, version.ID, dataDemoID).Scan(&migrated); err != nil {
		t.Fatalf("count migrated data.demo SIMs: %v", err)
	}
	if migrated != 1 {
		t.Errorf("migrated data.demo SIMs = %d, want 1", migrated)
	}

	// Verify no corporate.lan SIM was migrated.
	var corpMigrated int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM policy_assignments pa
		JOIN sims s ON s.id = pa.sim_id
		WHERE pa.policy_version_id = $1
		  AND s.apn_id = $2`, version.ID, corpLanID).Scan(&corpMigrated); err != nil {
		t.Fatalf("count migrated corporate.lan SIMs: %v", err)
	}
	if corpMigrated != 0 {
		t.Errorf("corporate.lan SIMs migrated = %d, want 0 (should be excluded by DSL MATCH)", corpMigrated)
	}
}

// =============================================================================
// Test B — No DSL MATCH (empty DSL) → apply to all 5 active SIMs (AC-8/AC-5)
// =============================================================================

func TestRollout_NoDSLMatch_RegressionAC8(t *testing.T) {
	pool := dslMatchPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	f := seedDSLMatchBase(t, pool)

	// One APN for variety (we don't MATCH on it — empty DSL → TRUE predicate).
	apnID := seedAPN(t, pool, f.tenantID, f.operatorID, "any.apn")

	// 5 SIMs, all active.
	for i := 0; i < 5; i++ {
		seedActiveSIM(t, pool, f.tenantID, f.operatorID, &apnID, i)
	}

	// policy_assignments cleanup must run before APN/SIM cleanup.
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `
			DELETE FROM policy_assignments
			WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id = $1)`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, f.tenantID)
	})

	// Empty DSL → compiledMatchFromVersion returns (nil, nil) → ToSQLPredicate → "TRUE"
	// → all active tenant SIMs are counted and migrated (AC-5 explicit design).
	affectedCount := 5
	policyStore := store.NewPolicyStore(pool)
	version, err := policyStore.CreateVersion(ctx, store.CreateVersionParams{
		PolicyID:         f.policyID,
		DSLContent:       "",
		CompiledRules:    json.RawMessage(`{}`),
		AffectedSIMCount: &affectedCount,
	})
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// Verify affected_sim_count stored correctly.
	if version.AffectedSIMCount == nil || *version.AffectedSIMCount != 5 {
		t.Fatalf("affected_sim_count = %v, want 5", version.AffectedSIMCount)
	}

	simStore := store.NewSIMStore(pool)
	svc := rollout.NewService(policyStore, simStore, nil, nil, nil, nil, zerolog.Nop())

	// Single stage at 100% — all 5 SIMs should be migrated in one shot.
	stagePcts := []int{100}
	ro, err := svc.StartRollout(ctx, f.tenantID, version.ID, stagePcts, nil)
	if err != nil {
		t.Fatalf("StartRollout: %v", err)
	}

	// AC-8: total_sims = 5.
	if ro.TotalSIMs != 5 {
		t.Errorf("total_sims = %d, want 5", ro.TotalSIMs)
	}

	// All 5 migrated (100% single stage, synchronous).
	if ro.MigratedSIMs != 5 {
		t.Errorf("migrated_sims = %d, want 5 (empty MATCH should apply to all active SIMs)", ro.MigratedSIMs)
	}

	// Confirm via DB: 5 policy_assignments for this version.
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM policy_assignments
		WHERE policy_version_id = $1`, version.ID).Scan(&count); err != nil {
		t.Fatalf("count policy_assignments: %v", err)
	}
	if count != 5 {
		t.Errorf("policy_assignments count = %d, want 5", count)
	}
}
