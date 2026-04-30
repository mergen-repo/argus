package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/store"
)

// TestSIMSessionTerminator_FinalizesSessions covers the FIX-305 invariant:
// when a SIM is suspended (or otherwise terminated), TerminateSIMSessions
// must finalize every active session row in the DB regardless of whether
// the DM packet succeeded. Skips when DATABASE_URL is unset.
func TestSIMSessionTerminator_FinalizesSessions(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping terminator integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	sessionStore := store.NewRadiusSessionStore(pool)

	// Seed a SIM + APN + Operator that satisfy FK constraints. Use existing
	// seed UUIDs from migrations/seed/003_comprehensive_seed.sql:
	//   tenant 10000000-0000-0000-0000-000000000001 (Narlık)
	//   any seeded operator + APN under that tenant.
	tenantID := uuid.MustParse("10000000-0000-0000-0000-000000000001")

	var simID, operatorID, apnID uuid.UUID
	row := pool.QueryRow(ctx, `
		SELECT id, operator_id, apn_id FROM sims
		WHERE tenant_id = $1 AND state = 'active'
		LIMIT 1`, tenantID)
	if err := row.Scan(&simID, &operatorID, &apnID); err != nil {
		t.Skipf("no seeded active SIM under tenant — skipping: %v", err)
	}

	// Truncate any pre-existing sessions for this SIM so the test starts clean.
	_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE sim_id = $1`, simID)

	nasIP := "10.0.0.99"
	created, err := sessionStore.Create(ctx, store.CreateRadiusSessionParams{
		SimID:        simID,
		TenantID:     tenantID,
		OperatorID:   operatorID,
		APNID:        &apnID,
		NASIP:        &nasIP,
		ProtocolType: "radius",
	})
	if err != nil {
		t.Fatalf("create test session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, created.ID)
	})

	// dmSender is nil so SendDM is skipped; the test focuses on the
	// finalize-row guarantee, which is independent of NAS reachability.
	terminator := NewSIMSessionTerminator(sessionStore, nil, zerolog.Nop())

	count, err := terminator.TerminateSIMSessions(ctx, simID, tenantID, "sim_suspended")
	if err != nil {
		t.Fatalf("terminate: %v", err)
	}
	if count != 1 {
		t.Fatalf("terminated count = %d, want 1", count)
	}

	// Confirm the session is now closed.
	after, err := sessionStore.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get session after terminate: %v", err)
	}
	if after.SessionState != "closed" {
		t.Errorf("session_state = %q, want %q", after.SessionState, "closed")
	}
	if after.TerminateCause == nil || *after.TerminateCause != "sim_suspended" {
		got := "<nil>"
		if after.TerminateCause != nil {
			got = *after.TerminateCause
		}
		t.Errorf("terminate_cause = %q, want %q", got, "sim_suspended")
	}
	if after.EndedAt == nil {
		t.Errorf("ended_at not set after terminate")
	}
}

func TestSIMSessionTerminator_NoActiveSessions(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping terminator integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	sessionStore := store.NewRadiusSessionStore(pool)
	terminator := NewSIMSessionTerminator(sessionStore, nil, zerolog.Nop())

	// Random SIM ID — no sessions exist for it. Should return 0 with no error.
	count, err := terminator.TerminateSIMSessions(ctx, uuid.New(), uuid.New(), "sim_suspended")
	if err != nil {
		t.Fatalf("terminate (no sessions): %v", err)
	}
	if count != 0 {
		t.Errorf("terminated count = %d, want 0", count)
	}
}
