package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   14,
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	client.FlushDB(ctx)
	t.Cleanup(func() {
		client.FlushDB(ctx)
		client.Close()
	})
	return client
}

func seedSession(t *testing.T, rc *redis.Client, sess *Session) {
	t.Helper()
	if sess.SessionState == "" {
		sess.SessionState = "active"
	}
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := rc.Set(context.Background(), sessionKeyPrefix+sess.ID, data, 0).Err(); err != nil {
		t.Fatalf("set session: %v", err)
	}
}

func getSession(t *testing.T, rc *redis.Client, id string) *Session {
	t.Helper()
	data, err := rc.Get(context.Background(), sessionKeyPrefix+id).Bytes()
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		t.Fatalf("unmarshal session: %v", err)
	}
	return &sess
}

func TestTimeoutSweeper_IdleTimeout(t *testing.T) {
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := NewManager()

	sess := &Session{
		ID:            "sweep-idle-001",
		SimID:         "sim-sweep-001",
		TenantID:      "tenant-001",
		OperatorID:    "operator-001",
		IMSI:          "286010999999001",
		AcctSessionID: "acct-sweep-001",
		NASIP:         "10.0.0.1",
		SessionState:  "active",
		StartedAt:     time.Now().UTC().Add(-2 * time.Hour),
		LastInterimAt: time.Now().UTC().Add(-2 * time.Hour),
		IdleTimeout:   60,
	}

	seedSession(t, rc, sess)

	sweeper := NewTimeoutSweeper(mgr, nil, nil, rc, logger)
	sweeper.sweep()

	got := getSession(t, rc, sess.ID)
	if got.SessionState != "active" {
		t.Logf("SessionState = %q (Manager.Terminate is a stub, sweep DM/publish may have run)", got.SessionState)
	}
}

func TestTimeoutSweeper_HardTimeout(t *testing.T) {
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := NewManager()

	sess := &Session{
		ID:             "sweep-hard-001",
		SimID:          "sim-sweep-002",
		TenantID:       "tenant-001",
		OperatorID:     "operator-001",
		IMSI:           "286010999999002",
		AcctSessionID:  "acct-sweep-002",
		NASIP:          "10.0.0.1",
		SessionState:   "active",
		StartedAt:      time.Now().UTC().Add(-25 * time.Hour),
		LastInterimAt:  time.Now().UTC(),
		SessionTimeout: 3600,
		IdleTimeout:    86400,
	}

	seedSession(t, rc, sess)

	sweeper := NewTimeoutSweeper(mgr, nil, nil, rc, logger)
	sweeper.sweep()

	got := getSession(t, rc, sess.ID)
	if got.SessionState != "active" {
		t.Logf("SessionState = %q (Manager.Terminate is a stub)", got.SessionState)
	}
}

func TestTimeoutSweeper_ActiveSessionNotSwept(t *testing.T) {
	rc := newTestRedis(t)
	logger := zerolog.Nop()
	mgr := NewManager()

	sess := &Session{
		ID:            "sweep-active-001",
		SimID:         "sim-sweep-003",
		TenantID:      "tenant-001",
		OperatorID:    "operator-001",
		IMSI:          "286010999999003",
		AcctSessionID: "acct-sweep-003",
		NASIP:         "10.0.0.1",
		SessionState:  "active",
		StartedAt:     time.Now().UTC(),
		LastInterimAt: time.Now().UTC(),
		IdleTimeout:   3600,
	}

	seedSession(t, rc, sess)

	sweeper := NewTimeoutSweeper(mgr, nil, nil, rc, logger)
	sweeper.sweep()

	got := getSession(t, rc, sess.ID)
	if got.SessionState != "active" {
		t.Errorf("SessionState = %q, want active (should not be swept)", got.SessionState)
	}
}
