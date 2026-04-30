package events

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type stubSims struct {
	iccid string
	err   error
	calls int
}

func (s *stubSims) GetICCID(ctx context.Context, id uuid.UUID) (string, error) {
	s.calls++
	return s.iccid, s.err
}

type stubOps struct {
	name  string
	err   error
	calls int
}

func (s *stubOps) GetName(ctx context.Context, id uuid.UUID) (string, error) {
	s.calls++
	return s.name, s.err
}

type stubAPNs struct {
	name  string
	err   error
	calls int
}

func (s *stubAPNs) GetName(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	s.calls++
	return s.name, s.err
}

type countingMetrics struct {
	hits   map[string]int
	misses map[string]int
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{hits: map[string]int{}, misses: map[string]int{}}
}

func (c *countingMetrics) IncResolverHit(kind string) {
	c.hits[kind]++
}

func (c *countingMetrics) IncResolverMiss(kind, reason string) {
	c.misses[kind+"/"+reason]++
}

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func newResolver(t *testing.T, sims SimNameLookup, ops OperatorLookup, apns APNLookup) (*RedisResolver, *countingMetrics) {
	t.Helper()
	rdb := newTestRedis(t)
	m := newCountingMetrics()
	r := NewRedisResolver(rdb, sims, ops, apns, m, zerolog.Nop())
	return r, m
}

func TestResolver_ResolveOperator_CacheMiss_HitsDB(t *testing.T) {
	ops := &stubOps{name: "turkcell"}
	r, metrics := newResolver(t, nil, ops, nil)

	opID := uuid.New()
	name, err := r.ResolveOperator(context.Background(), opID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "turkcell" {
		t.Fatalf("got %q, want turkcell", name)
	}
	if ops.calls != 1 {
		t.Fatalf("DB calls = %d, want 1", ops.calls)
	}
	if metrics.misses["operator/cold"] != 1 {
		t.Fatalf("expected 1 cold miss, got %+v", metrics.misses)
	}
}

func TestResolver_ResolveOperator_CacheHit(t *testing.T) {
	ops := &stubOps{name: "turkcell"}
	r, metrics := newResolver(t, nil, ops, nil)

	opID := uuid.New()
	// Prime cache.
	_, _ = r.ResolveOperator(context.Background(), opID)

	// Second call: hit.
	name, err := r.ResolveOperator(context.Background(), opID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "turkcell" {
		t.Fatalf("got %q", name)
	}
	if ops.calls != 1 {
		t.Fatalf("expected 1 DB call total (cache hit on 2nd), got %d", ops.calls)
	}
	if metrics.hits["operator"] != 1 {
		t.Fatalf("expected 1 hit, got %+v", metrics.hits)
	}
}

func TestResolver_ResolveOperator_DBError_ReturnsEmpty(t *testing.T) {
	ops := &stubOps{err: errors.New("db down")}
	r, metrics := newResolver(t, nil, ops, nil)

	name, err := r.ResolveOperator(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("DB errors must be swallowed, got %v", err)
	}
	if name != "" {
		t.Fatalf("expected empty name on DB error, got %q", name)
	}
	if metrics.misses["operator/db_error"] != 1 {
		t.Fatalf("expected 1 db_error miss, got %+v", metrics.misses)
	}
}

func TestResolver_ResolveICCID_Happy(t *testing.T) {
	sims := &stubSims{iccid: "8990011234"}
	r, _ := newResolver(t, sims, nil, nil)

	iccid, err := r.ResolveICCID(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if iccid != "8990011234" {
		t.Fatalf("got %q", iccid)
	}
}

func TestResolver_ResolveICCID_NilUUID_ReturnsEmpty(t *testing.T) {
	sims := &stubSims{iccid: "x"}
	r, _ := newResolver(t, sims, nil, nil)

	iccid, err := r.ResolveICCID(context.Background(), uuid.Nil)
	if err != nil || iccid != "" {
		t.Fatalf("expected empty for Nil UUID, got (%q, %v)", iccid, err)
	}
	if sims.calls != 0 {
		t.Fatal("must not call store for Nil UUID")
	}
}

func TestResolver_ResolveAPN_Happy(t *testing.T) {
	apns := &stubAPNs{name: "iot.argus"}
	r, _ := newResolver(t, nil, nil, apns)

	name, err := r.ResolveAPN(context.Background(), uuid.New(), uuid.New())
	if err != nil || name != "iot.argus" {
		t.Fatalf("got (%q, %v)", name, err)
	}
}

func TestResolver_HandleCacheInvalidate_ClearsKey(t *testing.T) {
	ops := &stubOps{name: "old"}
	r, _ := newResolver(t, nil, ops, nil)

	opID := uuid.New()
	// Prime cache with "old".
	_, _ = r.ResolveOperator(context.Background(), opID)

	// Invalidate.
	r.HandleCacheInvalidate(context.Background(), "operator:"+opID.String())

	// Switch stub's return value and reuse the resolver — next call must
	// miss the cache and return the new value.
	ops.name = "new"
	name, _ := r.ResolveOperator(context.Background(), opID)
	if name != "new" {
		t.Fatalf("after invalidate expected refreshed name 'new', got %q", name)
	}
}

func TestResolver_HandleCacheInvalidate_UnknownPrefix_NoOp(t *testing.T) {
	ops := &stubOps{name: "x"}
	r, _ := newResolver(t, nil, ops, nil)
	// Should not panic.
	r.HandleCacheInvalidate(context.Background(), "unknown:abc")
	r.HandleCacheInvalidate(context.Background(), "")
}

func TestFormatSimDisplayName(t *testing.T) {
	if got := FormatSimDisplayName("8990"); got != "ICCID 8990" {
		t.Fatalf("got %q", got)
	}
	if got := FormatSimDisplayName(""); got != "" {
		t.Fatalf("empty iccid must yield empty display_name, got %q", got)
	}
}
