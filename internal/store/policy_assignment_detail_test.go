package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type detailFixture struct {
	tenantID  uuid.UUID
	policyID  uuid.UUID
	versionID uuid.UUID
	rolloutID uuid.UUID
	simID     uuid.UUID
	apnID     uuid.UUID
}

// seedDetailFixture seeds tenant → policy → version → rollout → operator → APN → SIM.
// It does NOT insert a policy_assignment row; callers do that themselves.
// Cleanup is registered via t.Cleanup (child-first order).
func seedDetailFixture(t *testing.T, pool *pgxpool.Pool, label string) detailFixture {
	t.Helper()
	ctx := context.Background()
	var f detailFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix242-'||$1||'-'||gen_random_uuid()::text, 'fix242@test.argus')
		RETURNING id`, label).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant (%s): %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, scope, state)
		VALUES ($1, 'p-fix242-'||$2||'-'||gen_random_uuid()::text, 'global', 'active')
		RETURNING id`, f.tenantID, label).Scan(&f.policyID); err != nil {
		t.Fatalf("seed policy (%s): %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
		VALUES ($1, 1, 'allow all;', '{}', 'rolling_out')
		RETURNING id`, f.policyID).Scan(&f.versionID); err != nil {
		t.Fatalf("seed version (%s): %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, strategy, stages,
			total_sims, migrated_sims, state, started_at)
		VALUES ($1, $2, 'canary', '[]', 10, 0, 'in_progress', NOW())
		RETURNING id`, f.policyID, f.versionID).Scan(&f.rolloutID); err != nil {
		t.Fatalf("seed rollout (%s): %v", label, err)
	}

	var operatorID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM operators LIMIT 1`).Scan(&operatorID); err != nil {
		t.Fatalf("no operator available (%s): %v", label, err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO apns (tenant_id, operator_id, name, apn_type, state)
		VALUES ($1, $2, 'fix242-apn-'||$3||'-'||gen_random_uuid()::text, 'iot', 'active')
		RETURNING id`, f.tenantID, operatorID, label).Scan(&f.apnID); err != nil {
		t.Fatalf("seed apn (%s): %v", label, err)
	}
	nonce := uuid.New().ID() % 1_000_000_000
	if err := pool.QueryRow(ctx, `
		INSERT INTO sims (tenant_id, operator_id, apn_id, iccid, imsi, sim_type, state)
		VALUES ($1, $2, $3, $4, $5, 'physical', 'active')
		RETURNING id`,
		f.tenantID, operatorID, f.apnID,
		fmt.Sprintf("89010%010d", nonce),
		fmt.Sprintf("28601%010d", nonce),
	).Scan(&f.simID); err != nil {
		t.Fatalf("seed sim (%s): %v", label, err)
	}

	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM policy_assignments WHERE sim_id = $1`, f.simID)
		_, _ = pool.Exec(c, `DELETE FROM sims WHERE id = $1`, f.simID)
		_, _ = pool.Exec(c, `DELETE FROM apns WHERE id = $1`, f.apnID)
		_, _ = pool.Exec(c, `DELETE FROM policy_rollouts WHERE id = $1`, f.rolloutID)
		_, _ = pool.Exec(c, `DELETE FROM policy_versions WHERE id = $1`, f.versionID)
		_, _ = pool.Exec(c, `DELETE FROM policies WHERE id = $1`, f.policyID)
		_, _ = pool.Exec(c, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// ---------------------------------------------------------------------------
// TestPolicyStore_GetAssignmentDetailBySIM_Found (FIX-242 T7 #1)
// ---------------------------------------------------------------------------

func TestPolicyStore_GetAssignmentDetailBySIM_Found(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedDetailFixture(t, pool, "found")

	reason := "diameter timeout"
	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, coa_status, coa_sent_at, coa_failure_reason)
		VALUES ($1, $2, $3, 'failed', NOW(), $4)`,
		f.simID, f.versionID, f.rolloutID, reason); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}

	detail, err := st.GetAssignmentDetailBySIM(ctx, f.tenantID, f.simID)
	if err != nil {
		t.Fatalf("GetAssignmentDetailBySIM: %v", err)
	}
	if detail == nil {
		t.Fatal("expected non-nil detail, got nil")
	}
	if detail.PolicyID != f.policyID {
		t.Errorf("PolicyID = %v, want %v", detail.PolicyID, f.policyID)
	}
	if detail.PolicyVersionID != f.versionID {
		t.Errorf("PolicyVersionID = %v, want %v", detail.PolicyVersionID, f.versionID)
	}
	if detail.VersionNumber != 1 {
		t.Errorf("VersionNumber = %d, want 1", detail.VersionNumber)
	}
	if detail.CoAStatus != "failed" {
		t.Errorf("CoAStatus = %q, want failed", detail.CoAStatus)
	}
	if detail.CoASentAt == nil {
		t.Error("CoASentAt is nil, want non-nil")
	}
	if detail.CoAFailureReason == nil {
		t.Error("CoAFailureReason is nil, want non-nil")
	} else if *detail.CoAFailureReason != reason {
		t.Errorf("CoAFailureReason = %q, want %q", *detail.CoAFailureReason, reason)
	}
}

// ---------------------------------------------------------------------------
// TestPolicyStore_GetAssignmentDetailBySIM_NotFound (FIX-242 T7 #2)
// ---------------------------------------------------------------------------

func TestPolicyStore_GetAssignmentDetailBySIM_NotFound(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)

	detail, err := st.GetAssignmentDetailBySIM(ctx, uuid.New(), uuid.New())
	if !errors.Is(err, ErrAssignmentNotFound) {
		t.Errorf("err = %v, want ErrAssignmentNotFound", err)
	}
	if detail != nil {
		t.Errorf("detail = %v, want nil", detail)
	}
}

// ---------------------------------------------------------------------------
// TestPolicyStore_GetAssignmentDetailBySIM_TenantScoped (FIX-242 T7 #3)
// ---------------------------------------------------------------------------

func TestPolicyStore_GetAssignmentDetailBySIM_TenantScoped(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)

	// Seed a fixture in tenant B (which owns the assignment)
	fB := seedDetailFixture(t, pool, "scoped-b")

	// Seed tenant A separately (no data)
	var tenantA uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('fix242-scoped-a-'||gen_random_uuid()::text, 'fix242a@test.argus')
		RETURNING id`).Scan(&tenantA); err != nil {
		t.Fatalf("seed tenant A: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, tenantA)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, coa_status)
		VALUES ($1, $2, $3, 'pending')`,
		fB.simID, fB.versionID, fB.rolloutID); err != nil {
		t.Fatalf("seed assignment (tenant B): %v", err)
	}

	// Querying with tenant A must return ErrAssignmentNotFound (policy belongs to tenant B)
	detail, err := st.GetAssignmentDetailBySIM(ctx, tenantA, fB.simID)
	if !errors.Is(err, ErrAssignmentNotFound) {
		t.Errorf("tenant-scoped: err = %v, want ErrAssignmentNotFound", err)
	}
	if detail != nil {
		t.Errorf("tenant-scoped: detail = %v, want nil (must not cross tenant boundary)", detail)
	}
}

// ---------------------------------------------------------------------------
// TestPolicyStore_UpdateAssignmentCoAStatusWithReason_PopulatesReason (FIX-242 T7 #4)
// ---------------------------------------------------------------------------

func TestPolicyStore_UpdateAssignmentCoAStatusWithReason_PopulatesReason(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedDetailFixture(t, pool, "upd-reason")

	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, coa_status)
		VALUES ($1, $2, $3, 'pending')`, f.simID, f.versionID, f.rolloutID); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}

	reason := "diameter timeout"
	if err := st.UpdateAssignmentCoAStatusWithReason(ctx, f.simID, "failed", &reason); err != nil {
		t.Fatalf("UpdateAssignmentCoAStatusWithReason: %v", err)
	}

	var dbStatus string
	var dbReason *string
	if err := pool.QueryRow(ctx, `
		SELECT coa_status, coa_failure_reason FROM policy_assignments WHERE sim_id = $1`, f.simID,
	).Scan(&dbStatus, &dbReason); err != nil {
		t.Fatalf("read back assignment: %v", err)
	}
	if dbStatus != "failed" {
		t.Errorf("coa_status = %q, want failed", dbStatus)
	}
	if dbReason == nil {
		t.Error("coa_failure_reason is NULL, want non-nil")
	} else if *dbReason != reason {
		t.Errorf("coa_failure_reason = %q, want %q", *dbReason, reason)
	}
}

// ---------------------------------------------------------------------------
// TestPolicyStore_UpdateAssignmentCoAStatusWithReason_ClearsReasonOnSuccess (FIX-242 T7 #5)
// ---------------------------------------------------------------------------

func TestPolicyStore_UpdateAssignmentCoAStatusWithReason_ClearsReasonOnSuccess(t *testing.T) {
	pool := testPolicyPool(t)
	if pool == nil {
		t.Skip("no test database available (set DATABASE_URL)")
	}
	ctx := context.Background()
	st := NewPolicyStore(pool)
	f := seedDetailFixture(t, pool, "clr-reason")

	priorReason := "prior diameter timeout"
	if _, err := pool.Exec(ctx, `
		INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, coa_status, coa_failure_reason)
		VALUES ($1, $2, $3, 'failed', $4)`, f.simID, f.versionID, f.rolloutID, priorReason); err != nil {
		t.Fatalf("seed assignment with failed: %v", err)
	}

	// Update with acked + nil reason → must clear coa_failure_reason
	if err := st.UpdateAssignmentCoAStatusWithReason(ctx, f.simID, "acked", nil); err != nil {
		t.Fatalf("UpdateAssignmentCoAStatusWithReason (acked, nil): %v", err)
	}

	var dbStatus string
	var dbReason *string
	if err := pool.QueryRow(ctx, `
		SELECT coa_status, coa_failure_reason FROM policy_assignments WHERE sim_id = $1`, f.simID,
	).Scan(&dbStatus, &dbReason); err != nil {
		t.Fatalf("read back assignment: %v", err)
	}
	if dbStatus != "acked" {
		t.Errorf("coa_status = %q, want acked", dbStatus)
	}
	if dbReason != nil {
		t.Errorf("coa_failure_reason = %q, want NULL (should be cleared on success)", *dbReason)
	}
}
