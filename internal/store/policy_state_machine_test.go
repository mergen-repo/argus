package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FIX-231 Task 9 — store-layer integration tests for the policy state machine.
//
// All tests are DB-gated: testPolicyPool() returns nil when DATABASE_URL is
// unset, in which case each test t.Skip()s cleanly. No network mocks; we hit
// real Postgres so the migration-installed trigger and partial unique indexes
// are exercised end-to-end.
//
// Coverage:
//   - TestSimsPolicyVersionSync_BulkInsert    — trigger correctness on bulk INSERT (1k rows)
//   - TestSimsPolicyVersionSync_Delete         — trigger correctness on DELETE
//   - TestCompleteRollout_AtomicTransition     — 4-row atomic transition (rollout/v_new/v_old/policies)
//   - TestPolicyActiveVersionUniqueIndex       — partial unique index rejects 2nd active version
//
// Concurrent StartRollout is already covered by
// TestStartRollout_ConcurrentReturns422 in policy_test.go (FIX-231 Task 4).

// stateMachineFixture is the minimal DB graph needed for sims-pointer trigger
// tests: tenant, operator (existing global row), one APN, policy + two
// versions. Returns IDs and registers cleanup for everything it created.
type stateMachineFixture struct {
	tenantID   uuid.UUID
	operatorID uuid.UUID
	apnID      uuid.UUID
	policyID   uuid.UUID
	versionID1 uuid.UUID
	versionID2 uuid.UUID
}

func seedStateMachineFixture(t *testing.T, pool *pgxpool.Pool) stateMachineFixture {
	t.Helper()
	ctx := context.Background()
	var f stateMachineFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-sm-'||gen_random_uuid()::text, 'fix231-sm@test.argus')
		RETURNING id`).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&f.operatorID); err != nil {
		t.Fatalf("no operator row available (seed prerequisite): %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
		VALUES ($1, $2, 'iot.'||gen_random_uuid()::text, 'iot.fix231', 'iot', 'active')
		RETURNING id`, f.tenantID, f.operatorID).Scan(&f.apnID); err != nil {
		t.Fatalf("seed apn: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-sm-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'draft')
		RETURNING id`, f.policyID).Scan(&f.versionID1); err != nil {
		t.Fatalf("seed v1: %v", err)
	}

	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 2, 'allow all;', '{}', 'draft')
		RETURNING id`, f.policyID).Scan(&f.versionID2); err != nil {
		t.Fatalf("seed v2: %v", err)
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_assignments WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id = $1)`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `UPDATE policies SET current_version_id = NULL WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// insertSIMRows inserts n SIMs with a NULL initial policy_version_id and
// returns the created IDs. Identifiers are randomised to avoid collisions
// with the global ICCID/IMSI unique indexes.
func insertSIMRows(t *testing.T, pool *pgxpool.Pool, f stateMachineFixture, n int) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	ids := make([]uuid.UUID, 0, n)

	// Bulk insert in batches so we don't blow the parameter limit.
	const batch = 200
	for start := 0; start < n; start += batch {
		end := start + batch
		if end > n {
			end = n
		}

		rows := make([]any, 0, (end-start)*5)
		valuesSQL := ""
		for i := start; i < end; i++ {
			argBase := (i-start)*5 + 1
			if i > start {
				valuesSQL += ", "
			}
			// $1=tenant, $2=operator, $3=apn, $4=iccid, $5=imsi for each row
			valuesSQL += sprintfRow(argBase)
			nonce := uuid.New().ID()
			iccid := makeICCID(i, nonce)
			imsi := makeIMSI(i, nonce)
			rows = append(rows, f.tenantID, f.operatorID, f.apnID, iccid, imsi)
		}

		query := `
			INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
			VALUES ` + valuesSQL + `
			RETURNING id`
		rs, err := pool.Query(ctx, query, rows...)
		if err != nil {
			t.Fatalf("insert sims batch [%d,%d): %v", start, end, err)
		}
		for rs.Next() {
			var id uuid.UUID
			if err := rs.Scan(&id); err != nil {
				rs.Close()
				t.Fatalf("scan sim id: %v", err)
			}
			ids = append(ids, id)
		}
		rs.Close()
	}
	return ids
}

// sprintfRow returns "($N, $N+1, ..., 'physical', 'ordered')" with no fmt
// dependency at the call site (avoid pulling fmt into the helper for clarity).
func sprintfRow(base int) string {
	return "($" + itoa(base) + ", $" + itoa(base+1) + ", $" + itoa(base+2) + ", $" + itoa(base+3) + ", $" + itoa(base+4) + ", 'physical', 'ordered')"
}

// itoa is the minimal integer formatter for sprintfRow; avoids strconv import
// noise in tests (consistent with other ad-hoc helpers in this package).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// makeICCID returns a deterministic-but-unique ICCID < 22 chars.
// Pattern: 89911 + 2 idx digits + 9 nonce digits = 16 chars.
func makeICCID(idx int, nonce uint32) string {
	return "89911" + pad(idx%100, 2) + pad(int(nonce%1_000_000_000), 9)
}

// makeIMSI returns a deterministic-but-unique IMSI <= 15 chars.
// Pattern: 28611 + 2 idx digits + 8 nonce digits = 15 chars.
func makeIMSI(idx int, nonce uint32) string {
	return "28611" + pad(idx%100, 2) + pad(int(nonce%100_000_000), 8)
}

func pad(n, width int) string {
	s := itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// seedRollout inserts a minimal in-progress rollout row referencing the given
// policy_version_id. policy_assignments.rollout_id has FK fk_policy_assignments_rollout
// REFERENCES policy_rollouts(id), so the trigger tests need a real rollout id
// to attach assignments to. Returns the rollout id.
func seedRollout(t *testing.T, pool *pgxpool.Pool, policyID, versionID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var rolloutID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at)
		VALUES ($1, $2, 'canary', '[]', 0, 0, 'in_progress', NOW())
		RETURNING id`, policyID, versionID).Scan(&rolloutID); err != nil {
		t.Fatalf("seed rollout: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM policy_assignments WHERE rollout_id = $1`, rolloutID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_rollouts WHERE id = $1`, rolloutID)
	})
	return rolloutID
}

// =============================================================================
// Test 1 — Bulk INSERT into policy_assignments propagates to sims via trigger
// =============================================================================
//
// Insert 1000 sims (NULL policy_version_id), then INSERT 1000 policy_assignments
// rows in one statement. The trigger trg_sims_policy_version_sync (migration
// 20260427000001) must update every matching sims.policy_version_id to the
// new version. We measure wall-clock and log it; <1s on local pg is the target
// but we don't fail on slowness — that's an environmental concern, not a
// correctness concern.
func TestSimsPolicyVersionSync_BulkInsert(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	f := seedStateMachineFixture(t, pool)
	rolloutID := seedRollout(t, pool, f.policyID, f.versionID1)

	const n = 1000
	simIDs := insertSIMRows(t, pool, f, n)

	// Confirm NULL pointer initially.
	var nullCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM sims WHERE tenant_id = $1 AND policy_version_id IS NULL`,
		f.tenantID).Scan(&nullCount); err != nil {
		t.Fatalf("count nulls: %v", err)
	}
	if nullCount != n {
		t.Fatalf("expected %d sims with NULL policy_version_id, got %d", n, nullCount)
	}

	// Bulk INSERT 1000 assignments in a single statement (UNNEST keeps the
	// parameter count tiny — only 3 params: array of sim_ids + version_id +
	// rollout_id). One transaction so the trigger fires per row but commits
	// once.
	assignments := make([]uuid.UUID, n)
	copy(assignments, simIDs)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	start := time.Now()
	tag, err := tx.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, assigned_at, coa_status)
		SELECT unnest($1::uuid[]), $2, $3, NOW(), 'pending'`,
		assignments, f.versionID1, rolloutID,
	)
	if err != nil {
		t.Fatalf("bulk insert assignments: %v", err)
	}
	if tag.RowsAffected() != int64(n) {
		t.Fatalf("RowsAffected = %d, want %d", tag.RowsAffected(), n)
	}

	// Assert sims pointer was synced for every row, INSIDE the same tx so we
	// see the trigger's effect.
	var syncedCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM sims
		 WHERE tenant_id = $1 AND policy_version_id = $2`,
		f.tenantID, f.versionID1).Scan(&syncedCount); err != nil {
		t.Fatalf("count synced: %v", err)
	}
	elapsed := time.Since(start)

	if syncedCount != n {
		t.Errorf("synced count = %d, want %d (trg_sims_policy_version_sync did not propagate)", syncedCount, n)
	}

	t.Logf("bulk insert + trigger sync of %d rows: %s", n, elapsed)
	if elapsed > time.Second {
		t.Logf("WARN: trigger sync took %s (>1s); investigate if this regresses on CI", elapsed)
	}

	// Rollback so we don't persist 1000 sims into a shared dev DB. The
	// stateMachineFixture cleanup also DELETEs any committed leftovers,
	// belt + braces.
	_ = tx.Rollback(ctx)
}

// =============================================================================
// Test 2 — DELETE FROM policy_assignments nullifies sims.policy_version_id
// =============================================================================
//
// Insert one sim + one assignment; assert sims pointer is set. Delete the
// assignment; assert sims pointer becomes NULL. Exercises the TG_OP='DELETE'
// branch of trg_sims_policy_version_sync.
func TestSimsPolicyVersionSync_Delete(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	f := seedStateMachineFixture(t, pool)
	rolloutID := seedRollout(t, pool, f.policyID, f.versionID1)

	simIDs := insertSIMRows(t, pool, f, 1)
	simID := simIDs[0]

	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, assigned_at, coa_status)
		VALUES ($1, $2, $3, NOW(), 'pending')`,
		simID, f.versionID1, rolloutID); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}

	var got *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT policy_version_id FROM sims WHERE id = $1`, simID).Scan(&got); err != nil {
		t.Fatalf("read sim pointer (post-insert): %v", err)
	}
	if got == nil || *got != f.versionID1 {
		t.Fatalf("after INSERT: sim.policy_version_id = %v, want %v", got, f.versionID1)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM policy_assignments WHERE sim_id = $1`, simID); err != nil {
		t.Fatalf("delete assignment: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT policy_version_id FROM sims WHERE id = $1`, simID).Scan(&got); err != nil {
		t.Fatalf("read sim pointer (post-delete): %v", err)
	}
	if got != nil {
		t.Errorf("after DELETE: sim.policy_version_id = %v, want NULL", got)
	}
}

// =============================================================================
// Test 3 — CompleteRollout atomic 4-row transition
// =============================================================================
//
// Setup: policy + v1 active + v2 rolling_out + matching rollout (in_progress).
// Call CompleteRollout(rolloutID). Assert all four downstream rows reach the
// expected state in one transaction:
//   - policy_rollouts.state = 'completed'
//   - policy_versions(v2).state = 'active' AND activated_at IS NOT NULL
//   - policy_versions(v1).state = 'superseded'
//   - policies.current_version_id = v2
//
// Atomicity is implicit: BEGIN..COMMIT in the store function. If any step
// failed, we'd see a partial state and the assertions would catch it.
func TestCompleteRollout_AtomicTransition(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)

	// FIX-231 F-A1 (Gate): no index workaround. CompleteRollout now does
	// supersede-first, then activate, so the `policy_active_version` partial
	// unique index stays satisfied at every statement boundary. This is the
	// real production code path — the test previously dropped the index to
	// mask the bug; the fix lets it run under the actual schema.
	var tenantID, policyID, v1ID, v2ID, rolloutID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-atomic-'||gen_random_uuid()::text, 'fix231@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-atomic-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state, activated_at)
		VALUES ($1, 1, 'allow all;', '{}', 'active', NOW() - INTERVAL '1 day')
		RETURNING id`, policyID).Scan(&v1ID); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 2, 'allow all;', '{}', 'rolling_out')
		RETURNING id`, policyID).Scan(&v2ID); err != nil {
		t.Fatalf("seed v2: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE policies SET current_version_id = $1 WHERE id = $2`, v1ID, policyID); err != nil {
		t.Fatalf("set current_version_id v1: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, previous_version_id,
			strategy, stages, total_sims, migrated_sims, state, started_at)
		VALUES ($1, $2, $3, 'canary', '[]', 100, 100, 'in_progress', NOW())
		RETURNING id`, policyID, v2ID, v1ID).Scan(&rolloutID); err != nil {
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

	if err := st.CompleteRollout(ctx, rolloutID); err != nil {
		t.Fatalf("CompleteRollout: %v", err)
	}

	var rolloutState string
	var rolloutCompletedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT state, completed_at FROM policy_rollouts WHERE id = $1`, rolloutID).
		Scan(&rolloutState, &rolloutCompletedAt); err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	if rolloutState != "completed" {
		t.Errorf("rollout.state = %q, want 'completed'", rolloutState)
	}
	if rolloutCompletedAt == nil {
		t.Error("rollout.completed_at should be set")
	}

	var v2State string
	var v2ActivatedAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT state, activated_at FROM policy_versions WHERE id = $1`, v2ID).
		Scan(&v2State, &v2ActivatedAt); err != nil {
		t.Fatalf("read v2: %v", err)
	}
	if v2State != "active" {
		t.Errorf("v2.state = %q, want 'active'", v2State)
	}
	if v2ActivatedAt == nil {
		t.Error("v2.activated_at should be set after CompleteRollout")
	}

	var v1State string
	if err := pool.QueryRow(ctx,
		`SELECT state FROM policy_versions WHERE id = $1`, v1ID).Scan(&v1State); err != nil {
		t.Fatalf("read v1: %v", err)
	}
	if v1State != "superseded" {
		t.Errorf("v1.state = %q, want 'superseded'", v1State)
	}

	var currentVersionID *uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT current_version_id FROM policies WHERE id = $1`, policyID).
		Scan(&currentVersionID); err != nil {
		t.Fatalf("read policies: %v", err)
	}
	if currentVersionID == nil || *currentVersionID != v2ID {
		t.Errorf("policies.current_version_id = %v, want %v", currentVersionID, v2ID)
	}
}

// =============================================================================
// Test 4 — policy_active_version partial unique index rejects 2nd active
// =============================================================================
//
// The index was created in migration 20260427000001 step 5. With v1 already
// 'active', UPDATE'ing v2 to 'active' must fail with SQLSTATE 23505 and the
// constraint name 'policy_active_version'.
//
// Note: this test depends on the migration being applied to the test DB.
// If the test DB is fresh per run (the make db-migrate convention), this
// works. If you SKIP-run against an old DB without the index, the test will
// fail with a clearer "expected unique violation" message — that itself is
// useful regression signal.
func TestPolicyActiveVersionUniqueIndex(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()

	var tenantID, policyID, v1ID, v2ID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix231-uniq-'||gen_random_uuid()::text, 'fix231@test.argus')
		RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-uniq-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, tenantID).Scan(&policyID); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state, activated_at)
		VALUES ($1, 1, 'allow all;', '{}', 'active', NOW() - INTERVAL '1 day')
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
		_, _ = pool.Exec(cctx, `UPDATE policies SET current_version_id = NULL WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE id = $1`, policyID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, tenantID)
	})

	// Promote v2 to active while v1 is still active — must violate the
	// partial unique index.
	_, err := pool.Exec(ctx,
		`UPDATE policy_versions SET state = 'active', activated_at = NOW() WHERE id = $1`, v2ID)
	if err == nil {
		t.Fatal("expected unique_violation when promoting a second version to 'active' while another is active, got nil error")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
	}
	if pgErr.Code != "23505" {
		t.Errorf("expected SQLSTATE 23505 (unique_violation), got %q (%s): %s",
			pgErr.Code, pgErr.ConstraintName, pgErr.Message)
	}
	if pgErr.ConstraintName != "policy_active_version" {
		t.Errorf("expected constraint 'policy_active_version', got %q", pgErr.ConstraintName)
	}
}
