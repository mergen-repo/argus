package session

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisCounter(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return client, mr
}

func TestCounter_IncrOnSessionStarted(t *testing.T) {
	rc, _ := newTestRedisCounter(t)
	ctx := context.Background()
	tenantID := "tenant-aaa-111"
	key := sessionCountKeyPrefix + tenantID

	c := &Counter{rc: rc}

	for i := 0; i < 5; i++ {
		if err := rc.Incr(ctx, key).Err(); err != nil {
			t.Fatalf("INCR %d: %v", i, err)
		}
	}

	got, err := c.GetActiveCount(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetActiveCount: %v", err)
	}
	if got != 5 {
		t.Errorf("expected 5 after 5 INCRs, got %d", got)
	}
}

func TestCounter_DecrOnSessionEnded(t *testing.T) {
	rc, _ := newTestRedisCounter(t)
	ctx := context.Background()
	tenantID := "tenant-bbb-222"
	key := sessionCountKeyPrefix + tenantID

	c := &Counter{rc: rc}

	rc.Set(ctx, key, 3, sessionCountTTL)

	rc.Decr(ctx, key)

	got, err := c.GetActiveCount(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetActiveCount: %v", err)
	}
	if got != 2 {
		t.Errorf("expected 2 after DECR from 3, got %d", got)
	}
}

func TestCounter_GetActiveCount_MissingKey(t *testing.T) {
	rc, _ := newTestRedisCounter(t)
	ctx := context.Background()
	c := &Counter{rc: rc}

	got, err := c.GetActiveCount(ctx, "nonexistent-tenant")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != -1 {
		t.Errorf("expected -1 for missing key, got %d", got)
	}
}

func TestCounter_ReconcileFixesDrift(t *testing.T) {
	rc, _ := newTestRedisCounter(t)
	ctx := context.Background()

	tenants := map[string]int64{
		"tenant-ccc-333": 10,
		"tenant-ddd-444": 25,
	}

	for tenantID, count := range tenants {
		key := sessionCountKeyPrefix + tenantID
		rc.Set(ctx, key, 999, sessionCountTTL)
		_ = count
	}

	store := &stubSessionStore{counts: tenants}
	c := &Counter{rc: rc, store: store}

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	for tenantID, expected := range tenants {
		got, err := c.GetActiveCount(ctx, tenantID)
		if err != nil {
			t.Fatalf("GetActiveCount(%s): %v", tenantID, err)
		}
		if got != expected {
			t.Errorf("tenant %s: expected %d after reconcile, got %d", tenantID, expected, got)
		}
	}
}

func TestCounter_ReconcileSetsNewKey(t *testing.T) {
	rc, _ := newTestRedisCounter(t)
	ctx := context.Background()
	tenantID := "tenant-eee-555"

	c := &Counter{
		rc:    rc,
		store: &stubSessionStore{counts: map[string]int64{tenantID: 7}},
	}

	val, _ := c.GetActiveCount(ctx, tenantID)
	if val != -1 {
		t.Fatal("expected key absent before reconcile")
	}

	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, err := c.GetActiveCount(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetActiveCount: %v", err)
	}
	if got != 7 {
		t.Errorf("expected 7, got %d", got)
	}
}

func TestCounter_KeyTTLSet(t *testing.T) {
	rc, mr := newTestRedisCounter(t)
	ctx := context.Background()
	tenantID := "tenant-fff-666"
	key := sessionCountKeyPrefix + tenantID

	rc.Incr(ctx, key)
	rc.Expire(ctx, key, sessionCountTTL)

	ttl := mr.TTL(key)
	if ttl <= 0 || ttl > sessionCountTTL+time.Second {
		t.Errorf("unexpected TTL %v, want ~%v", ttl, sessionCountTTL)
	}
}

func TestCounter_DriftBudget(t *testing.T) {
	rc, _ := newTestRedisCounter(t)
	ctx := context.Background()

	tenantID := "tenant-drift-999"
	key := sessionCountKeyPrefix + tenantID

	groundTruth := int64(100)
	rc.Set(ctx, key, groundTruth+5, sessionCountTTL)

	store := &stubSessionStore{counts: map[string]int64{tenantID: groundTruth}}
	c := &Counter{rc: rc, store: store}
	if err := c.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	got, _ := c.GetActiveCount(ctx, tenantID)
	drift := float64(got-groundTruth) / float64(groundTruth) * 100
	if drift < 0 {
		drift = -drift
	}
	if drift > 1.0 {
		t.Errorf("drift %.2f%% exceeds 1%% budget", drift)
	}
}

type stubSessionStore struct {
	counts map[string]int64
}

func (s *stubSessionStore) ListActiveTenantCounts(_ context.Context) (map[string]int64, error) {
	return s.counts, nil
}
