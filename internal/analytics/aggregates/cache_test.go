package aggregates

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/btopcu/argus/internal/store"
)

// fakeAggregates is a test double for the Aggregates interface.
type fakeAggregates struct {
	calls map[string]int

	simCountByTenant   int
	simCountByOperator map[uuid.UUID]int
	simCountByAPN      map[uuid.UUID]int64
	simCountByPolicy   int
	simStateTotal      int
	simStateByState    []store.SIMStateCount
	sessionStats       *store.SessionStatsResult
	trafficByOperator  map[uuid.UUID]int64

	errSIMCountByTenant   error
	errSIMCountByOperator error
	errSIMCountByAPN      error
	errSIMCountByPolicy   error
	errSIMCountByState    error
	errActiveSessionStats error
	errTrafficByOperator  error
}

func newFakeAggregates() *fakeAggregates {
	return &fakeAggregates{calls: make(map[string]int)}
}

func (f *fakeAggregates) SIMCountByTenant(_ context.Context, _ uuid.UUID) (int, error) {
	f.calls["SIMCountByTenant"]++
	return f.simCountByTenant, f.errSIMCountByTenant
}

func (f *fakeAggregates) SIMCountByOperator(_ context.Context, _ uuid.UUID) (map[uuid.UUID]int, error) {
	f.calls["SIMCountByOperator"]++
	return f.simCountByOperator, f.errSIMCountByOperator
}

func (f *fakeAggregates) SIMCountByAPN(_ context.Context, _ uuid.UUID) (map[uuid.UUID]int64, error) {
	f.calls["SIMCountByAPN"]++
	return f.simCountByAPN, f.errSIMCountByAPN
}

func (f *fakeAggregates) SIMCountByPolicy(_ context.Context, _, _ uuid.UUID) (int, error) {
	f.calls["SIMCountByPolicy"]++
	return f.simCountByPolicy, f.errSIMCountByPolicy
}

func (f *fakeAggregates) SIMCountByState(_ context.Context, _ uuid.UUID) (int, []store.SIMStateCount, error) {
	f.calls["SIMCountByState"]++
	return f.simStateTotal, f.simStateByState, f.errSIMCountByState
}

func (f *fakeAggregates) ActiveSessionStats(_ context.Context, _ uuid.UUID) (*store.SessionStatsResult, error) {
	f.calls["ActiveSessionStats"]++
	return f.sessionStats, f.errActiveSessionStats
}

func (f *fakeAggregates) TrafficByOperator(_ context.Context, _ uuid.UUID) (map[uuid.UUID]int64, error) {
	f.calls["TrafficByOperator"]++
	return f.trafficByOperator, f.errTrafficByOperator
}

// fakeMetrics records calls for assertion in tests.
type fakeMetrics struct {
	hits      map[string]int
	misses    map[string]int
	durations []string
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{hits: make(map[string]int), misses: make(map[string]int)}
}

func (m *fakeMetrics) IncAggregatesCacheHit(method string)   { m.hits[method]++ }
func (m *fakeMetrics) IncAggregatesCacheMiss(method string)  { m.misses[method]++ }
func (m *fakeMetrics) ObserveAggregatesCallDuration(method, cache string, _ time.Duration) {
	m.durations = append(m.durations, method+":"+cache)
}

func newTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

// TestCachedAggregates_SIMCountByTenant_MissThenHit verifies that the first
// call hits the inner store (miss) and the second call is served from cache (hit).
func TestCachedAggregates_SIMCountByTenant_MissThenHit(t *testing.T) {
	_, rdb := newTestRedis(t)
	inner := newFakeAggregates()
	inner.simCountByTenant = 42
	metrics := newFakeMetrics()

	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: defaultTTL, reg: metrics}

	tenantID := uuid.New()
	ctx := context.Background()

	v1, err := svc.SIMCountByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if v1 != 42 {
		t.Fatalf("first call: want 42, got %d", v1)
	}
	if inner.calls["SIMCountByTenant"] != 1 {
		t.Fatalf("first call: inner should be called once, was %d", inner.calls["SIMCountByTenant"])
	}

	v2, err := svc.SIMCountByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if v2 != 42 {
		t.Fatalf("second call: want 42, got %d", v2)
	}
	if inner.calls["SIMCountByTenant"] != 1 {
		t.Fatalf("second call: inner should not be called again, was %d", inner.calls["SIMCountByTenant"])
	}

	if metrics.misses["sim_count_by_tenant"] != 1 {
		t.Fatalf("expected 1 miss, got %d", metrics.misses["sim_count_by_tenant"])
	}
	if metrics.hits["sim_count_by_tenant"] != 1 {
		t.Fatalf("expected 1 hit, got %d", metrics.hits["sim_count_by_tenant"])
	}
}

// TestCachedAggregates_KeyFormat_MatchesSpec verifies that cache keys follow
// the spec pattern argus:aggregates:v1:{tenant_id}:{method}[:{args}].
func TestCachedAggregates_KeyFormat_MatchesSpec(t *testing.T) {
	mr, rdb := newTestRedis(t)
	inner := newFakeAggregates()
	inner.simCountByTenant = 1
	inner.simCountByPolicy = 5
	inner.simStateTotal = 10
	inner.simStateByState = []store.SIMStateCount{{State: "active", Count: 10}}

	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: defaultTTL}

	tenantID := uuid.New()
	policyID := uuid.New()
	ctx := context.Background()

	_, _ = svc.SIMCountByTenant(ctx, tenantID)
	expectedKey := fmt.Sprintf("argus:aggregates:v1:%s:sim_count_by_tenant", tenantID.String())
	if !mr.Exists(expectedKey) {
		t.Errorf("key not found in Redis: %s", expectedKey)
	}

	_, _ = svc.SIMCountByPolicy(ctx, tenantID, policyID)
	expectedPolicyKey := fmt.Sprintf("argus:aggregates:v1:%s:sim_count_by_policy:%s", tenantID.String(), policyID.String())
	if !mr.Exists(expectedPolicyKey) {
		t.Errorf("policy key not found in Redis: %s", expectedPolicyKey)
	}

	_, _, _ = svc.SIMCountByState(ctx, tenantID)
	expectedStateKey := fmt.Sprintf("argus:aggregates:v1:%s:sim_count_by_state", tenantID.String())
	if !mr.Exists(expectedStateKey) {
		t.Errorf("state key not found in Redis: %s", expectedStateKey)
	}
}

// TestCachedAggregates_InvalidateThenRefetch verifies that after TTL expiry
// the cache re-fetches from the inner store.
func TestCachedAggregates_InvalidateThenRefetch(t *testing.T) {
	mr, rdb := newTestRedis(t)
	inner := newFakeAggregates()
	inner.simCountByTenant = 100

	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: 30 * time.Second}

	tenantID := uuid.New()
	ctx := context.Background()

	_, _ = svc.SIMCountByTenant(ctx, tenantID)
	if inner.calls["SIMCountByTenant"] != 1 {
		t.Fatalf("want 1 inner call after first fetch, got %d", inner.calls["SIMCountByTenant"])
	}

	mr.FastForward(31 * time.Second)

	inner.simCountByTenant = 200
	v, err := svc.SIMCountByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("refetch after expiry: unexpected error: %v", err)
	}
	if v != 200 {
		t.Fatalf("refetch after expiry: want 200, got %d", v)
	}
	if inner.calls["SIMCountByTenant"] != 2 {
		t.Fatalf("want 2 inner calls after expiry refetch, got %d", inner.calls["SIMCountByTenant"])
	}
}

// TestCachedAggregates_MapEncoding_Roundtrip verifies that UUID-keyed maps
// survive a JSON serialize→deserialize cycle through Redis.
func TestCachedAggregates_MapEncoding_Roundtrip(t *testing.T) {
	_, rdb := newTestRedis(t)

	op1 := uuid.New()
	op2 := uuid.New()

	original := map[uuid.UUID]int{op1: 10, op2: 20}

	inner := newFakeAggregates()
	inner.simCountByOperator = original

	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: defaultTTL}

	tenantID := uuid.New()
	ctx := context.Background()

	v1, err := svc.SIMCountByOperator(ctx, tenantID)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if v1[op1] != 10 || v1[op2] != 20 {
		t.Fatalf("first call: unexpected values: %v", v1)
	}

	inner.simCountByOperator = map[uuid.UUID]int{op1: 999}
	v2, err := svc.SIMCountByOperator(ctx, tenantID)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if v2[op1] != 10 || v2[op2] != 20 {
		t.Fatalf("second call (cache hit): expected original values, got %v", v2)
	}
	if inner.calls["SIMCountByOperator"] != 1 {
		t.Fatalf("inner should be called once for cache hit, was %d", inner.calls["SIMCountByOperator"])
	}
}

// TestCachedAggregates_NilRedis_Passthrough verifies that rdb=nil causes all
// calls to pass through to inner without caching.
func TestCachedAggregates_NilRedis_Passthrough(t *testing.T) {
	inner := newFakeAggregates()
	inner.simCountByTenant = 7

	svc := &cachedAggregates{inner: inner, rdb: nil, ttl: defaultTTL}

	tenantID := uuid.New()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		v, err := svc.SIMCountByTenant(ctx, tenantID)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
		if v != 7 {
			t.Fatalf("call %d: want 7, got %d", i+1, v)
		}
	}
	if inner.calls["SIMCountByTenant"] != 3 {
		t.Fatalf("want 3 inner calls (no cache), got %d", inner.calls["SIMCountByTenant"])
	}
}

// TestCachedAggregates_ActiveSessionStats_Roundtrip verifies that
// *SessionStatsResult (no UUID map, string keys in ByOperator/ByAPN) caches correctly.
func TestCachedAggregates_ActiveSessionStats_Roundtrip(t *testing.T) {
	_, rdb := newTestRedis(t)
	inner := newFakeAggregates()
	inner.sessionStats = &store.SessionStatsResult{
		TotalActive:    55,
		ByOperator:     map[string]int64{"op-a": 30, "op-b": 25},
		ByAPN:          map[string]int64{"internet": 55},
		AvgDurationSec: 120.5,
		AvgBytes:       4096.0,
	}

	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: defaultTTL}

	tenantID := uuid.New()
	ctx := context.Background()

	v1, err := svc.ActiveSessionStats(ctx, tenantID)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if v1.TotalActive != 55 {
		t.Fatalf("first call: want TotalActive=55, got %d", v1.TotalActive)
	}

	inner.sessionStats = &store.SessionStatsResult{TotalActive: 999}
	v2, err := svc.ActiveSessionStats(ctx, tenantID)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if v2.TotalActive != 55 {
		t.Fatalf("second call (cache hit): want TotalActive=55, got %d", v2.TotalActive)
	}
	if v2.ByOperator["op-a"] != 30 {
		t.Fatalf("second call: ByOperator roundtrip failed, want 30, got %d", v2.ByOperator["op-a"])
	}
	if inner.calls["ActiveSessionStats"] != 1 {
		t.Fatalf("inner should be called once, was %d", inner.calls["ActiveSessionStats"])
	}
}

// TestCachedAggregates_SIMCountByState_Roundtrip verifies the composite
// struct cache encoding for SIMCountByState.
func TestCachedAggregates_SIMCountByState_Roundtrip(t *testing.T) {
	_, rdb := newTestRedis(t)
	inner := newFakeAggregates()
	inner.simStateTotal = 500
	inner.simStateByState = []store.SIMStateCount{
		{State: "active", Count: 400},
		{State: "inactive", Count: 100},
	}

	svc := &cachedAggregates{inner: inner, rdb: rdb, ttl: defaultTTL}

	tenantID := uuid.New()
	ctx := context.Background()

	total1, states1, err := svc.SIMCountByState(ctx, tenantID)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if total1 != 500 || len(states1) != 2 {
		t.Fatalf("first call: unexpected values total=%d states=%v", total1, states1)
	}

	inner.simStateTotal = 999
	total2, states2, err := svc.SIMCountByState(ctx, tenantID)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if total2 != 500 {
		t.Fatalf("second call (cache hit): want total=500, got %d", total2)
	}
	if len(states2) != 2 || states2[0].State != "active" {
		t.Fatalf("second call: unexpected states: %v", states2)
	}
	if inner.calls["SIMCountByState"] != 1 {
		t.Fatalf("inner should be called once, was %d", inner.calls["SIMCountByState"])
	}
}

// TestCachedAggregates_WithTTL_Clamp verifies that TTL values below minTTL
// are clamped to minTTL.
func TestCachedAggregates_WithTTL_Clamp(t *testing.T) {
	opts := Options{TTL: defaultTTL}
	WithTTL(1 * time.Second)(&opts)
	if opts.TTL != minTTL {
		t.Fatalf("expected TTL clamped to %v, got %v", minTTL, opts.TTL)
	}

	WithTTL(30 * time.Second)(&opts)
	if opts.TTL != 30*time.Second {
		t.Fatalf("expected TTL=30s, got %v", opts.TTL)
	}
}
