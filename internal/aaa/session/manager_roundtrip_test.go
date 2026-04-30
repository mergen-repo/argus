package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// fakeAuditor — captures CreateEntry calls for test assertions
// ---------------------------------------------------------------------------

type fakeAuditor struct {
	calls []audit.CreateEntryParams
}

func (f *fakeAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	f.calls = append(f.calls, p)
	return &audit.Entry{}, nil
}

// ---------------------------------------------------------------------------
// TestManager_Create_PublishesSessionStartedAudit (FIX-242 T7 #11)
// ---------------------------------------------------------------------------
// The audit publisher in Manager.Create is guarded by `if m.sessionStore != nil`
// because it runs inside the DB-write block (FIX-242 AC-5). A sessionStore
// requires a live PostgreSQL connection; we therefore gate on DATABASE_URL and
// skip cleanly in unit-only environments.

func TestManager_Create_PublishesSessionStartedAudit(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("no test database available (set DATABASE_URL) — audit publisher requires sessionStore (DB-backed)")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	sessionStore := store.NewRadiusSessionStore(pool)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	logger := zerolog.Nop()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	fa := &fakeAuditor{}
	mgr := NewManager(sessionStore, rdb, logger, WithAuditService(fa))

	// We need a real tenant + operator + SIM to satisfy FK constraints in
	// radius_sessions. Look up an existing SIM from the test DB.
	var simID, tenantID, operatorID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT s.id, s.tenant_id, s.operator_id
		FROM sims s
		LIMIT 1`).Scan(&simID, &tenantID, &operatorID)
	if err != nil {
		t.Skipf("no SIM row available in test DB for audit test: %v", err)
	}

	sess := &Session{
		SimID:         simID.String(),
		TenantID:      tenantID.String(),
		OperatorID:    operatorID.String(),
		IMSI:          "286012999999901",
		AcctSessionID: "acct-audit-fix242-" + uuid.New().String()[:8],
		NASIP:         "10.99.0.1",
		SessionState:  "active",
		StartedAt:     time.Now().UTC(),
	}

	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		if sess.ID != "" {
			_, _ = pool.Exec(context.Background(), `DELETE FROM radius_sessions WHERE id = $1`, sess.ID)
		}
	})

	// Audit must have been called exactly once with action=session.started
	if len(fa.calls) != 1 {
		t.Fatalf("audit CreateEntry called %d times, want 1", len(fa.calls))
	}
	call := fa.calls[0]
	if call.Action != "session.started" {
		t.Errorf("Action = %q, want session.started", call.Action)
	}
	if call.EntityType != "session" {
		t.Errorf("EntityType = %q, want session", call.EntityType)
	}
	if call.EntityID == "" {
		t.Error("EntityID is empty, want non-empty session ID")
	}
	if call.AfterData == nil {
		t.Error("AfterData is nil, want non-nil (must contain session fields)")
	}
}

// ---------------------------------------------------------------------------

func TestManagerRoundtrip_RestartSafe(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	logger := zerolog.Nop()

	rdb1 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb1.Close()

	mgr := NewManager(nil, rdb1, logger)

	sess := &Session{
		ID:             "roundtrip-sess-001",
		SimID:          "sim-rt-001",
		TenantID:       "tenant-rt-001",
		OperatorID:     "op-rt-001",
		IMSI:           "286010700000001",
		AcctSessionID:  "acct-rt-001",
		SessionState:   "active",
		SessionTimeout: 3600,
		StartedAt:      time.Now().UTC(),
	}

	ctx := context.Background()
	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rdb2 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb2.Close()
	mgr2 := NewManager(nil, rdb2, logger)

	got, err := mgr2.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after simulated restart: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil — session not persisted to Redis")
	}
	if got.ID != sess.ID {
		t.Errorf("ID = %q, want %q", got.ID, sess.ID)
	}
	if got.IMSI != sess.IMSI {
		t.Errorf("IMSI = %q, want %q", got.IMSI, sess.IMSI)
	}
	if got.TenantID != sess.TenantID {
		t.Errorf("TenantID = %q, want %q", got.TenantID, sess.TenantID)
	}
	if got.SessionState != "active" {
		t.Errorf("SessionState = %q, want active", got.SessionState)
	}
}
