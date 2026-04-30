package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testOTAPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("testOTAPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func setupOTATenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	tenantID := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1,$2,$3,'free')`,
		tenantID, "ota-test-"+tenantID.String()[:8], "ota-slug-"+tenantID.String()[:8],
	)
	if err != nil {
		t.Fatalf("setupOTATenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM esim_ota_commands WHERE tenant_id=$1`, tenantID)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id=$1`, tenantID)
	})
	return tenantID
}

func insertTestEsimOTACommand(t *testing.T, store *EsimOTACommandStore, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := store.Insert(context.Background(), InsertEsimOTACommandParams{
		TenantID:    tenantID,
		EID:         "89000000000000000001",
		CommandType: "switch",
	})
	if err != nil {
		t.Fatalf("insertTestEsimOTACommand: %v", err)
	}
	return id
}

func TestEsimOTACommandStore_InsertAndGet(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)

	id, err := store.Insert(context.Background(), InsertEsimOTACommandParams{
		TenantID:    tenantID,
		EID:         "89000000000000000001",
		CommandType: "switch",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	cmd, err := store.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if cmd.Status != "queued" {
		t.Errorf("expected status 'queued', got %s", cmd.Status)
	}
	if cmd.EID != "89000000000000000001" {
		t.Errorf("expected EID to be '89000000000000000001', got %s", cmd.EID)
	}
}

func TestEsimOTACommandStore_TransitionQueuedToSent(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	err := store.MarkSent(context.Background(), id, "smsr-ext-id-001")
	if err != nil {
		t.Fatalf("MarkSent: %v", err)
	}

	cmd, _ := store.GetByID(context.Background(), id)
	if cmd.Status != "sent" {
		t.Errorf("expected 'sent', got %s", cmd.Status)
	}
	if cmd.SMSRCommandID == nil || *cmd.SMSRCommandID != "smsr-ext-id-001" {
		t.Error("expected smsr_command_id to be set")
	}
	if cmd.SentAt == nil {
		t.Error("expected sent_at to be set")
	}
}

func TestEsimOTACommandStore_TransitionSentToAcked(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	store.MarkSent(context.Background(), id, "smsr-id")
	if err := store.MarkAcked(context.Background(), id); err != nil {
		t.Fatalf("MarkAcked: %v", err)
	}

	cmd, _ := store.GetByID(context.Background(), id)
	if cmd.Status != "acked" {
		t.Errorf("expected 'acked', got %s", cmd.Status)
	}
	if cmd.AckedAt == nil {
		t.Error("expected acked_at to be set")
	}
}

func TestEsimOTACommandStore_TransitionToFailed(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	if err := store.MarkFailed(context.Background(), id, "SM-SR rejected"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	cmd, _ := store.GetByID(context.Background(), id)
	if cmd.Status != "failed" {
		t.Errorf("expected 'failed', got %s", cmd.Status)
	}
	if cmd.LastError == nil || *cmd.LastError != "SM-SR rejected" {
		t.Error("expected last_error to be set")
	}
}

func TestEsimOTACommandStore_TransitionSentToTimeout(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	store.MarkSent(context.Background(), id, "smsr-id")
	if err := store.MarkTimeout(context.Background(), id); err != nil {
		t.Fatalf("MarkTimeout: %v", err)
	}

	cmd, _ := store.GetByID(context.Background(), id)
	if cmd.Status != "timeout" {
		t.Errorf("expected 'timeout', got %s", cmd.Status)
	}
}

func TestEsimOTACommandStore_IncrementRetry(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	nextRetry := time.Now().Add(30 * time.Second)
	if err := store.IncrementRetry(context.Background(), id, nextRetry); err != nil {
		t.Fatalf("IncrementRetry: %v", err)
	}

	cmd, _ := store.GetByID(context.Background(), id)
	if cmd.RetryCount != 1 {
		t.Errorf("expected retry_count=1, got %d", cmd.RetryCount)
	}
	if cmd.Status != "queued" {
		t.Errorf("expected status 'queued', got %s", cmd.Status)
	}
	if cmd.NextRetryAt == nil {
		t.Error("expected next_retry_at to be set")
	}
}

func TestEsimOTACommandStore_InvalidTransition_AckedToSent(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	store.MarkSent(context.Background(), id, "smsr-id")
	store.MarkAcked(context.Background(), id)

	err := store.MarkSent(context.Background(), id, "smsr-id-2")
	if err != ErrEsimOTAInvalidTransition {
		t.Errorf("expected ErrEsimOTAInvalidTransition, got: %v", err)
	}
}

func TestEsimOTACommandStore_InvalidTransition_QueuedToAcked(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	err := store.MarkAcked(context.Background(), id)
	if err != ErrEsimOTAInvalidTransition {
		t.Errorf("expected ErrEsimOTAInvalidTransition for queued→acked, got: %v", err)
	}
}

// TestEsimOTACommandStore_InvalidTransition_FailedToSent verifies that the failed
// terminal state rejects further transitions — MarkSent on a failed command must
// return ErrEsimOTAInvalidTransition (state-machine terminal guarantee).
func TestEsimOTACommandStore_InvalidTransition_FailedToSent(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)
	id := insertTestEsimOTACommand(t, store, tenantID)

	if err := store.MarkSent(context.Background(), id, "smsr-initial"); err != nil {
		t.Fatalf("MarkSent (queued→sent): %v", err)
	}
	if err := store.MarkFailed(context.Background(), id, "permanent rejection"); err != nil {
		t.Fatalf("MarkFailed (sent→failed): %v", err)
	}

	err := store.MarkSent(context.Background(), id, "smsr-retry")
	if err != ErrEsimOTAInvalidTransition {
		t.Errorf("expected ErrEsimOTAInvalidTransition for failed→sent (terminal stays terminal), got: %v", err)
	}
}

func TestEsimOTACommandStore_ListQueued_Ordering(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)

	id1 := insertTestEsimOTACommand(t, store, tenantID)
	id2 := insertTestEsimOTACommand(t, store, tenantID)
	id3 := insertTestEsimOTACommand(t, store, tenantID)

	store.MarkSent(context.Background(), id2, "smsr-id")

	commands, err := store.ListQueued(context.Background(), 10, time.Now())
	if err != nil {
		t.Fatalf("ListQueued: %v", err)
	}

	queuedIDs := make(map[uuid.UUID]bool)
	for _, c := range commands {
		queuedIDs[c.ID] = true
	}
	if !queuedIDs[id1] || !queuedIDs[id3] {
		t.Error("expected id1 and id3 in queued list")
	}
	if queuedIDs[id2] {
		t.Error("id2 should not be in queued list (it is 'sent')")
	}
}

func TestEsimOTACommandStore_ListQueued_NextRetryAt_Filtered(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)

	id := insertTestEsimOTACommand(t, store, tenantID)
	futureRetry := time.Now().Add(1 * time.Hour)
	store.IncrementRetry(context.Background(), id, futureRetry)

	commands, err := store.ListQueued(context.Background(), 10, time.Now())
	if err != nil {
		t.Fatalf("ListQueued: %v", err)
	}

	for _, c := range commands {
		if c.ID == id {
			t.Error("command with future next_retry_at should not appear in ListQueued")
		}
	}
}

func TestEsimOTACommandStore_BatchInsert(t *testing.T) {
	pool := testOTAPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ota store test")
	}
	store := NewEsimOTACommandStore(pool)
	tenantID := setupOTATenant(t, pool)

	params := []InsertEsimOTACommandParams{
		{TenantID: tenantID, EID: "eid-batch-1", CommandType: "switch"},
		{TenantID: tenantID, EID: "eid-batch-2", CommandType: "enable"},
		{TenantID: tenantID, EID: "eid-batch-3", CommandType: "disable"},
	}

	count, err := store.BatchInsert(context.Background(), params)
	if err != nil {
		t.Fatalf("BatchInsert: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}

	for _, eid := range []string{"eid-batch-1", "eid-batch-2", "eid-batch-3"} {
		cmds, _, err := store.ListByEID(context.Background(), tenantID, eid, "", 10)
		if err != nil {
			t.Fatalf("ListByEID(%s): %v", eid, err)
		}
		if len(cmds) != 1 {
			t.Errorf("expected 1 command for EID %s, got %d", eid, len(cmds))
		}
	}
}
