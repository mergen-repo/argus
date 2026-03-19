package session

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestTimeoutSweeper_IdleTimeout(t *testing.T) {
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := NewManager(rc, logger)

	ctx := context.Background()
	sess := &Session{
		ID:            "sweep-idle-001",
		SimID:         "sim-sweep-001",
		TenantID:      "tenant-001",
		OperatorID:    "operator-001",
		IMSI:          "286010999999001",
		AcctSessionID: "acct-sweep-001",
		NASIP:         "10.0.0.1",
		StartedAt:     time.Now().UTC().Add(-2 * time.Hour),
		LastInterimAt: time.Now().UTC().Add(-2 * time.Hour),
		IdleTimeout:   60,
	}

	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sweeper := NewTimeoutSweeper(mgr, nil, nil, rc, logger)
	sweeper.sweep()

	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after sweep: %v", err)
	}
	if got == nil {
		t.Fatal("session should still exist (terminated, not deleted)")
	}
	if got.SessionState != "terminated" {
		t.Errorf("SessionState = %q, want terminated", got.SessionState)
	}
	if got.TerminateCause != "idle_timeout" {
		t.Errorf("TerminateCause = %q, want idle_timeout", got.TerminateCause)
	}
}

func TestTimeoutSweeper_HardTimeout(t *testing.T) {
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := NewManager(rc, logger)

	ctx := context.Background()
	sess := &Session{
		ID:             "sweep-hard-001",
		SimID:          "sim-sweep-002",
		TenantID:       "tenant-001",
		OperatorID:     "operator-001",
		IMSI:           "286010999999002",
		AcctSessionID:  "acct-sweep-002",
		NASIP:          "10.0.0.1",
		StartedAt:      time.Now().UTC().Add(-25 * time.Hour),
		LastInterimAt:  time.Now().UTC(),
		SessionTimeout: 3600,
		IdleTimeout:    86400,
	}

	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sweeper := NewTimeoutSweeper(mgr, nil, nil, rc, logger)
	sweeper.sweep()

	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after sweep: %v", err)
	}
	if got == nil {
		t.Fatal("session should still exist")
	}
	if got.SessionState != "terminated" {
		t.Errorf("SessionState = %q, want terminated", got.SessionState)
	}
	if got.TerminateCause != "session_timeout" {
		t.Errorf("TerminateCause = %q, want session_timeout", got.TerminateCause)
	}
}

func TestTimeoutSweeper_ActiveSessionNotSwept(t *testing.T) {
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := NewManager(rc, logger)

	ctx := context.Background()
	sess := &Session{
		ID:            "sweep-active-001",
		SimID:         "sim-sweep-003",
		TenantID:      "tenant-001",
		OperatorID:    "operator-001",
		IMSI:          "286010999999003",
		AcctSessionID: "acct-sweep-003",
		NASIP:         "10.0.0.1",
		StartedAt:     time.Now().UTC(),
		LastInterimAt: time.Now().UTC(),
		IdleTimeout:   3600,
	}

	if err := mgr.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	sweeper := NewTimeoutSweeper(mgr, nil, nil, rc, logger)
	sweeper.sweep()

	got, err := mgr.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after sweep: %v", err)
	}
	if got == nil {
		t.Fatal("session should still exist")
	}
	if got.SessionState != "active" {
		t.Errorf("SessionState = %q, want active (should not be swept)", got.SessionState)
	}
}
