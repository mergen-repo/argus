package rollout_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/btopcu/argus/internal/policy/rollout"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// FIX-232 DEV-357 — service-layer AbortRollout integration tests.
//
// Coverage:
//   - TestService_AbortRollout_TenantScoped         — tenant mismatch returns ErrRolloutNotFound
//   - TestService_AbortRollout_HappyPath            — abort persists state without reverting assignments
//   - TestService_AdvanceRollout_RejectsAborted     — Advance after abort returns ErrRolloutAborted
//   - TestService_RollbackRollout_RejectsAborted    — Rollback after abort returns ErrRolloutAborted
//
// All tests SKIP cleanly when DATABASE_URL is unset.

// testAbortPool mirrors testReaperPool/testPolicyPool — duplicated because the
// helper is per-package (different test binaries) and the original lives in
// internal/store.
func testAbortPool(t *testing.T) *pgxpool.Pool {
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

type serviceAbortFixture struct {
	tenantID  uuid.UUID
	policyID  uuid.UUID
	versionID uuid.UUID
	rolloutID uuid.UUID
}

// seedServiceAbortFixture wires tenant + policy + rolling_out version +
// in_progress rollout. Mirrors the store-side seedAbortFixture but lives in the
// rollout_test package.
func seedServiceAbortFixture(t *testing.T, pool *pgxpool.Pool, label string) serviceAbortFixture {
	t.Helper()
	ctx := context.Background()
	var f serviceAbortFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix232-svc-'||$1||'-'||gen_random_uuid()::text, 'fix232-svc@test.argus')
		RETURNING id`, label).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-svc-'||$2||'-'||gen_random_uuid()::text, 'global', 'active')
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
			total_sims, migrated_sims, current_stage, state, started_at)
		VALUES ($1, $2, 'canary', '[]', 100, 50, 1, 'in_progress', NOW())
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

// newAbortService builds a Service with real *PolicyStore and nil deps for
// session/CoA/bus/job — AbortRollout uses none of those (the bus call is a
// no-op when eventBus is nil, see publishProgressWithState).
func newAbortService(pool *pgxpool.Pool) *rollout.Service {
	return rollout.NewService(
		store.NewPolicyStore(pool),
		nil, // simStore — unused by AbortRollout
		nil, // sessionProvider
		nil, // coaDispatcher
		nil, // eventBus — publishProgressWithState short-circuits on nil
		nil, // jobStore
		zerolog.Nop(),
	)
}

// TestService_AbortRollout_HappyPath confirms the service-layer wrapper
// transitions state to 'aborted' and leaves assignments intact (count of
// migrated SIMs unchanged on the persisted row).
func TestService_AbortRollout_HappyPath(t *testing.T) {
	pool := testAbortPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	svc := newAbortService(pool)
	f := seedServiceAbortFixture(t, pool, "happy")

	got, err := svc.AbortRollout(ctx, f.tenantID, f.rolloutID, "operator-decision")
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
		t.Errorf("MigratedSIMs = %d, want 50 (no revert)", got.MigratedSIMs)
	}
}

// TestService_AbortRollout_TenantScoped verifies that aborting a rollout owned
// by a different tenant returns ErrRolloutNotFound — the service uses
// GetRolloutByIDWithTenant, which scopes by tenant, so a foreign tenant gets
// the same response as a non-existent rollout id.
func TestService_AbortRollout_TenantScoped(t *testing.T) {
	pool := testAbortPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	svc := newAbortService(pool)
	f := seedServiceAbortFixture(t, pool, "tenant")

	// Different tenant — should not see this rollout.
	otherTenantID := uuid.New()

	_, err := svc.AbortRollout(ctx, otherTenantID, f.rolloutID, "wrong-tenant")
	if !errors.Is(err, store.ErrRolloutNotFound) {
		t.Errorf("err = %v, want ErrRolloutNotFound", err)
	}

	// And the rollout state must remain in_progress (cross-tenant call must
	// not have transitioned it).
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM policy_rollouts WHERE id = $1`, f.rolloutID).Scan(&state); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state != "in_progress" {
		t.Errorf("state = %q, want in_progress (cross-tenant abort must be a no-op)", state)
	}
}

// TestService_AdvanceRollout_RejectsAborted verifies that AdvanceRollout sees
// the new aborted-state guard and returns ErrRolloutAborted instead of trying
// to advance a terminal rollout.
func TestService_AdvanceRollout_RejectsAborted(t *testing.T) {
	pool := testAbortPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	svc := newAbortService(pool)
	f := seedServiceAbortFixture(t, pool, "advance")

	if _, err := svc.AbortRollout(ctx, f.tenantID, f.rolloutID, "stop"); err != nil {
		t.Fatalf("AbortRollout: %v", err)
	}

	_, err := svc.AdvanceRollout(ctx, f.tenantID, f.rolloutID)
	if !errors.Is(err, store.ErrRolloutAborted) {
		t.Errorf("AdvanceRollout after abort: err = %v, want ErrRolloutAborted", err)
	}
}

// TestService_RollbackRollout_RejectsAborted verifies that RollbackRollout
// surfaces the aborted-state guard from the service layer (also enforced
// defensively at the store layer).
func TestService_RollbackRollout_RejectsAborted(t *testing.T) {
	pool := testAbortPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	svc := newAbortService(pool)
	f := seedServiceAbortFixture(t, pool, "rollback")

	if _, err := svc.AbortRollout(ctx, f.tenantID, f.rolloutID, "stop"); err != nil {
		t.Fatalf("AbortRollout: %v", err)
	}

	_, _, err := svc.RollbackRollout(ctx, f.tenantID, f.rolloutID, "after-abort")
	if !errors.Is(err, store.ErrRolloutAborted) {
		t.Errorf("RollbackRollout after abort: err = %v, want ErrRolloutAborted", err)
	}
}
