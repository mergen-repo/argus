package session

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

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
