package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedisForSession(t *testing.T) *redis.Client {
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

func seedTestSession(t *testing.T, rc *redis.Client, sess *Session) {
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

func TestManager_ListActive_Redis(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		sess := &Session{
			ID:           "list-sess-" + string(rune('a'+i)),
			SimID:        "sim-" + string(rune('a'+i)),
			TenantID:     "tenant-001",
			OperatorID:   "op-001",
			IMSI:         "28601010000000" + string(rune('1'+i)),
			SessionState: "active",
			StartedAt:    time.Now().UTC(),
		}
		seedTestSession(t, rc, sess)
	}

	sessions, _, err := mgr.ListActive(ctx, "", 10, SessionFilter{})
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 5 {
		t.Errorf("len = %d, want 5", len(sessions))
	}
}

func TestManager_ListActive_Redis_WithFilter(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	ctx := context.Background()

	sess1 := &Session{
		ID:           "filter-sess-1",
		SimID:        "sim-a",
		TenantID:     "tenant-001",
		OperatorID:   "op-001",
		IMSI:         "286010100000001",
		SessionState: "active",
		StartedAt:    time.Now().UTC(),
	}
	sess2 := &Session{
		ID:           "filter-sess-2",
		SimID:        "sim-b",
		TenantID:     "tenant-001",
		OperatorID:   "op-002",
		IMSI:         "286010100000002",
		SessionState: "active",
		StartedAt:    time.Now().UTC(),
	}
	seedTestSession(t, rc, sess1)
	seedTestSession(t, rc, sess2)

	sessions, _, err := mgr.ListActive(ctx, "", 10, SessionFilter{OperatorID: "op-001"})
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("len = %d, want 1", len(sessions))
	}
	if len(sessions) > 0 && sessions[0].OperatorID != "op-001" {
		t.Errorf("OperatorID = %q, want op-001", sessions[0].OperatorID)
	}
}

func TestManager_Stats_Redis(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	sess1 := &Session{
		ID:           "stats-sess-1",
		SimID:        "sim-s1",
		TenantID:     "tenant-001",
		OperatorID:   "op-001",
		APNID:        "apn-001",
		IMSI:         "286010100000001",
		SessionState: "active",
		BytesIn:      1000,
		BytesOut:     2000,
		StartedAt:    time.Now().UTC().Add(-10 * time.Minute),
	}
	sess2 := &Session{
		ID:           "stats-sess-2",
		SimID:        "sim-s2",
		TenantID:     "tenant-001",
		OperatorID:   "op-002",
		APNID:        "apn-002",
		RATType:      "lte",
		IMSI:         "286010100000002",
		SessionState: "active",
		BytesIn:      3000,
		BytesOut:     4000,
		StartedAt:    time.Now().UTC().Add(-20 * time.Minute),
	}
	seedTestSession(t, rc, sess1)
	seedTestSession(t, rc, sess2)

	stats, err := mgr.Stats(context.Background(), "")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalActive != 2 {
		t.Errorf("TotalActive = %d, want 2", stats.TotalActive)
	}
	if stats.ByOperator["op-001"] != 1 {
		t.Errorf("ByOperator[op-001] = %d, want 1", stats.ByOperator["op-001"])
	}
	if stats.ByOperator["op-002"] != 1 {
		t.Errorf("ByOperator[op-002] = %d, want 1", stats.ByOperator["op-002"])
	}
	if stats.ByAPN["apn-001"] != 1 {
		t.Errorf("ByAPN[apn-001] = %d, want 1", stats.ByAPN["apn-001"])
	}
	if stats.ByRATType["lte"] != 1 {
		t.Errorf("ByRATType[lte] = %d, want 1", stats.ByRATType["lte"])
	}
	if stats.AvgDurationSec <= 0 {
		t.Errorf("AvgDurationSec = %f, want > 0", stats.AvgDurationSec)
	}
	if stats.AvgBytes <= 0 {
		t.Errorf("AvgBytes = %f, want > 0", stats.AvgBytes)
	}
}

func TestManager_Stats_Empty(t *testing.T) {
	logger := zerolog.Nop()
	mgr := NewManager(nil, nil, logger)

	stats, err := mgr.Stats(context.Background(), "")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalActive != 0 {
		t.Errorf("TotalActive = %d, want 0", stats.TotalActive)
	}
}

func TestManager_ListActive_Limit(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	for i := 0; i < 10; i++ {
		sess := &Session{
			ID:           "limit-sess-" + string(rune('a'+i)),
			SimID:        "sim-" + string(rune('a'+i)),
			TenantID:     "tenant-001",
			OperatorID:   "op-001",
			IMSI:         "28601010000000" + string(rune('0'+i)),
			SessionState: "active",
			StartedAt:    time.Now().UTC(),
		}
		seedTestSession(t, rc, sess)
	}

	sessions, _, err := mgr.ListActive(context.Background(), "", 3, SessionFilter{})
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) > 3 {
		t.Errorf("len = %d, want <= 3", len(sessions))
	}
}

func TestManager_Create_And_Get_Redis(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	sess := &Session{
		ID:             "create-get-test",
		SimID:          "sim-cg",
		TenantID:       "tenant-001",
		OperatorID:     "op-001",
		IMSI:           "286010100000099",
		AcctSessionID:  "acct-cg-001",
		SessionState:   "active",
		SessionTimeout: 3600,
		StartedAt:      time.Now().UTC(),
	}

	err := mgr.Create(context.Background(), sess)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := mgr.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.IMSI != "286010100000099" {
		t.Errorf("IMSI = %q, want 286010100000099", got.IMSI)
	}
	if got.SessionState != "active" {
		t.Errorf("SessionState = %q, want active", got.SessionState)
	}
}

func TestManager_UpdateCounters_Redis(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	sess := &Session{
		ID:             "counters-test",
		SimID:          "sim-ct",
		TenantID:       "tenant-001",
		OperatorID:     "op-001",
		IMSI:           "286010100000050",
		SessionState:   "active",
		SessionTimeout: 3600,
		BytesIn:        1000,
		BytesOut:       2000,
		StartedAt:      time.Now().UTC(),
	}

	if err := mgr.Create(context.Background(), sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.UpdateCounters(context.Background(), sess.ID, 5000, 10000); err != nil {
		t.Fatalf("UpdateCounters: %v", err)
	}

	got, err := mgr.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BytesIn != 5000 {
		t.Errorf("BytesIn = %d, want 5000", got.BytesIn)
	}
	if got.BytesOut != 10000 {
		t.Errorf("BytesOut = %d, want 10000", got.BytesOut)
	}
}

func TestManager_Terminate_Redis(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	sess := &Session{
		ID:             "terminate-test",
		SimID:          "sim-tt",
		TenantID:       "tenant-001",
		OperatorID:     "op-001",
		IMSI:           "286010100000060",
		AcctSessionID:  "acct-tt-001",
		SessionState:   "active",
		SessionTimeout: 3600,
		StartedAt:      time.Now().UTC(),
	}

	if err := mgr.Create(context.Background(), sess); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := mgr.Terminate(context.Background(), sess.ID, "admin-disconnect"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	got, err := mgr.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Error("expected nil after terminate, session should be removed from Redis")
	}
}

func TestManager_Get_NonExistent(t *testing.T) {
	logger := zerolog.Nop()
	mgr := NewManager(nil, nil, logger)

	got, err := mgr.Get(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent session")
	}
}

func TestManager_ListActive_FilterClosedSessions(t *testing.T) {
	rc := newTestRedisForSession(t)
	logger := zerolog.Nop()
	mgr := NewManager(nil, rc, logger)

	active := &Session{
		ID:           "active-sess",
		SimID:        "sim-a",
		TenantID:     "tenant-001",
		OperatorID:   "op-001",
		IMSI:         "286010100000001",
		SessionState: "active",
		StartedAt:    time.Now().UTC(),
	}
	closed := &Session{
		ID:           "closed-sess",
		SimID:        "sim-b",
		TenantID:     "tenant-001",
		OperatorID:   "op-001",
		IMSI:         "286010100000002",
		SessionState: "closed",
		StartedAt:    time.Now().UTC(),
	}
	seedTestSession(t, rc, active)
	seedTestSession(t, rc, closed)

	sessions, _, err := mgr.ListActive(context.Background(), "", 10, SessionFilter{})
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("len = %d, want 1 (only active)", len(sessions))
	}
	if len(sessions) > 0 && sessions[0].ID != "active-sess" {
		t.Errorf("ID = %q, want active-sess", sessions[0].ID)
	}
}
