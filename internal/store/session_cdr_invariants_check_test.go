package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestSessionCDRInvariants_CHECKConstraints_RejectsBadInserts is a DB-gated
// test (skips when postgres isn't reachable) that probes the two CHECK
// constraints added in migration 20260421000002_session_cdr_invariants.up.sql:
//
//  1. chk_sessions_ended_after_started: ended_at IS NULL OR ended_at >= started_at
//  2. chk_cdrs_duration_nonneg: duration_sec >= 0
//
// Both should reject bad INSERTs with PostgreSQL SQLSTATE 23514 (check_violation).
// FIX-207 Gate F-B2: coverage for Migration B CHECK rejection at the Go layer
// (was previously verified only via live psql during dev).
func TestSessionCDRInvariants_CHECKConstraints_RejectsBadInserts(t *testing.T) {
	pool := testIPPoolPool(t) // reuses the pattern: returns nil when DB unreachable
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated CHECK-constraint probe")
	}
	ctx := context.Background()

	tenantID := uuid.New()
	operatorID := uuid.New()
	simID := uuid.New()
	now := time.Now()

	t.Run("sessions rejects ended_at<started_at with 23514", func(t *testing.T) {
		// ended_at is one second BEFORE started_at — violates the CHECK.
		_, err := pool.Exec(ctx, `
			INSERT INTO sessions (
				id, sim_id, tenant_id, operator_id,
				session_state, started_at, ended_at
			) VALUES ($1, $2, $3, $4, 'closed', $5, $6)
		`, uuid.New(), simID, tenantID, operatorID, now, now.Add(-1*time.Second))

		if err == nil {
			t.Fatal("expected CHECK violation when ended_at < started_at, got nil error")
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
		}
		if pgErr.Code != "23514" {
			t.Errorf("expected SQLSTATE 23514 (check_violation), got %q (%s): %s",
				pgErr.Code, pgErr.ConstraintName, pgErr.Message)
		}
		if pgErr.ConstraintName != "chk_sessions_ended_after_started" {
			t.Errorf("expected constraint chk_sessions_ended_after_started, got %q",
				pgErr.ConstraintName)
		}
	})

	t.Run("cdrs rejects duration_sec<0 with 23514", func(t *testing.T) {
		// duration_sec=-5 violates the CHECK.
		_, err := pool.Exec(ctx, `
			INSERT INTO cdrs (
				session_id, sim_id, tenant_id, operator_id,
				record_type, duration_sec, timestamp
			) VALUES ($1, $2, $3, $4, 'final', $5, $6)
		`, uuid.New(), simID, tenantID, operatorID, -5, now)

		if err == nil {
			t.Fatal("expected CHECK violation when duration_sec < 0, got nil error")
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("expected *pgconn.PgError, got %T: %v", err, err)
		}
		if pgErr.Code != "23514" {
			t.Errorf("expected SQLSTATE 23514 (check_violation), got %q (%s): %s",
				pgErr.Code, pgErr.ConstraintName, pgErr.Message)
		}
		if pgErr.ConstraintName != "chk_cdrs_duration_nonneg" {
			t.Errorf("expected constraint chk_cdrs_duration_nonneg, got %q",
				pgErr.ConstraintName)
		}
	})

	t.Run("sessions accepts ended_at=NULL (active session)", func(t *testing.T) {
		// NULL ended_at is the other leg of the CHECK (ended_at IS NULL OR ...).
		// This is the happy-path INSERT that MUST succeed.
		id := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO sessions (
				id, sim_id, tenant_id, operator_id,
				session_state, started_at, ended_at
			) VALUES ($1, $2, $3, $4, 'active', $5, NULL)
		`, id, simID, tenantID, operatorID, now)
		if err != nil {
			t.Fatalf("expected NULL ended_at to pass CHECK, got: %v", err)
		}
		// Cleanup — this row was inserted with a synthetic tenant/operator/sim
		// that has no FK counterpart; the DELETE tidies it up.
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	})

	t.Run("cdrs accepts duration_sec=0 (boundary)", func(t *testing.T) {
		// duration_sec=0 is the inclusive boundary of the CHECK (>= 0).
		_, err := pool.Exec(ctx, `
			INSERT INTO cdrs (
				session_id, sim_id, tenant_id, operator_id,
				record_type, duration_sec, timestamp
			) VALUES ($1, $2, $3, $4, 'interim', $5, $6)
		`, uuid.New(), simID, tenantID, operatorID, 0, now)
		if err != nil {
			t.Fatalf("expected duration_sec=0 to pass CHECK, got: %v", err)
		}
	})
}
