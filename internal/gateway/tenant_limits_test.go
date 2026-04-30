package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// mockTenantLookup records how many times GetByID was called so the
// caching tests can assert that a warm cache bypasses the DB.
type mockTenantLookup struct {
	tenant *store.Tenant
	err    error
	calls  atomic.Int32
}

func (m *mockTenantLookup) GetByID(_ context.Context, _ uuid.UUID) (*store.Tenant, error) {
	m.calls.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	return m.tenant, nil
}

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// passthroughHandler is the terminal handler we wrap with the middleware — a 200
// OK here means the middleware let the request through.
func passthroughHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func requestWithTenant(tenantID uuid.UUID) *http.Request {
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)
	return httptest.NewRequest(http.MethodPost, "/api/v1/sims", nil).WithContext(ctx)
}

func decodeError(t *testing.T, body []byte) apierr.ErrorResponse {
	t.Helper()
	var resp apierr.ErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return resp
}

// TestEnforce_LimitReached verifies the primary rejection path:
// current == limit (>= check) must produce 422 TENANT_LIMIT_EXCEEDED
// with a details payload containing resource/current/limit.
func TestEnforce_LimitReached(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{ID: tenantID, MaxSims: 5}}
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) { return 5, nil },
		},
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	resp := decodeError(t, rec.Body.Bytes())
	if resp.Error.Code != apierr.CodeTenantLimitExceeded {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeTenantLimitExceeded)
	}

	// The details field is encoded as a JSON array of objects; decode
	// generically and check resource/current/limit.
	details, ok := resp.Error.Details.([]interface{})
	if !ok || len(details) == 0 {
		t.Fatalf("expected details array, got %#v", resp.Error.Details)
	}
	first, ok := details[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected details[0] to be object, got %#v", details[0])
	}
	if first["resource"] != "sims" {
		t.Errorf("resource = %v, want sims", first["resource"])
	}
	if first["current"].(float64) != 5 {
		t.Errorf("current = %v, want 5", first["current"])
	}
	if first["limit"].(float64) != 5 {
		t.Errorf("limit = %v, want 5", first["limit"])
	}
}

// TestEnforce_BelowLimit — the happy path: count < limit passes through
// to the next handler.
func TestEnforce_BelowLimit(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{ID: tenantID, MaxSims: 10}}
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) { return 5, nil },
		},
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestEnforce_UnlimitedZero — limit == 0 means "unlimited". No count
// call, no rejection. Covers the explicit provisioning convention
// documented on the middleware.
func TestEnforce_UnlimitedZero(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{ID: tenantID, MaxApns: 0}}
	counterCalled := false
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitAPNs: func(_ context.Context, _ uuid.UUID) (int, error) {
				counterCalled = true
				return 99999, nil
			},
		},
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitAPNs)(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if counterCalled {
		t.Error("counter must not be called when limit is 0 (unlimited)")
	}
}

// TestEnforce_CacheMissThenHit verifies the Redis cache behavior end to
// end: first request loads from DB and writes to Redis, second request
// reads from Redis without touching the DB (calls == 1, not 2).
func TestEnforce_CacheMissThenHit(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{ID: tenantID, MaxUsers: 3}}
	rdb := newTestRedis(t)

	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitUsers: func(_ context.Context, _ uuid.UUID) (int, error) { return 1, nil },
		},
		rdb, 5*time.Minute, zerolog.Nop(),
	)

	// First call: miss → DB → cache populate.
	rec1 := httptest.NewRecorder()
	mw.Enforce(LimitUsers)(passthroughHandler()).ServeHTTP(rec1, requestWithTenant(tenantID))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want 200", rec1.Code)
	}
	if got := lookup.calls.Load(); got != 1 {
		t.Fatalf("after first call, GetByID calls = %d, want 1", got)
	}

	// Second call: hit → no DB.
	rec2 := httptest.NewRecorder()
	mw.Enforce(LimitUsers)(passthroughHandler()).ServeHTTP(rec2, requestWithTenant(tenantID))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second call status = %d, want 200", rec2.Code)
	}
	if got := lookup.calls.Load(); got != 1 {
		t.Errorf("after second call, GetByID calls = %d, want 1 (cache hit expected)", got)
	}

	// Sanity: the cache key must now exist with the four-field payload.
	raw, err := rdb.Get(context.Background(), "tenant:limits:"+tenantID.String()).Bytes()
	if err != nil {
		t.Fatalf("cache entry missing after miss path: %v", err)
	}
	var cached cachedLimits
	if err := json.Unmarshal(raw, &cached); err != nil {
		t.Fatalf("cached payload unmarshal: %v", err)
	}
	if cached.MaxUsers != 3 {
		t.Errorf("cached MaxUsers = %d, want 3", cached.MaxUsers)
	}
}

// TestEnforce_CacheInvalidationViaDel simulates the TenantStore.Update
// callback: after the cache key is deleted, the next request must
// re-hit the DB. This guards the end-to-end freshness contract even
// though the store's Update is tested separately.
func TestEnforce_CacheInvalidationViaDel(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{ID: tenantID, MaxSims: 10}}
	rdb := newTestRedis(t)

	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) { return 1, nil },
		},
		rdb, 5*time.Minute, zerolog.Nop(),
	)

	// Prime the cache.
	rec1 := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec1, requestWithTenant(tenantID))
	if got := lookup.calls.Load(); got != 1 {
		t.Fatalf("GetByID calls = %d, want 1 after prime", got)
	}

	// Simulate the store.Update cache-invalidation side effect.
	if err := rdb.Del(context.Background(), "tenant:limits:"+tenantID.String()).Err(); err != nil {
		t.Fatalf("redis del: %v", err)
	}

	rec2 := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec2, requestWithTenant(tenantID))
	if got := lookup.calls.Load(); got != 2 {
		t.Errorf("GetByID calls = %d, want 2 after invalidation", got)
	}
}

// TestEnforce_MissingTenantContext — when no tenant is bound to the
// request context, the middleware must not 500 or short-circuit; it
// passes through so downstream auth returns the canonical 401.
func TestEnforce_MissingTenantContext(t *testing.T) {
	lookup := &mockTenantLookup{tenant: &store.Tenant{MaxSims: 5}}
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) { return 0, nil },
		},
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims", nil)
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (pass-through)", rec.Code)
	}
	if lookup.calls.Load() != 0 {
		t.Error("GetByID should not be called without a tenant in context")
	}
}

// TestEnforce_TenantLookupFailure — DB error during lookup must surface
// as 500 INTERNAL_ERROR, not silently bypass the limit.
func TestEnforce_TenantLookupFailure(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{err: errors.New("db down")}
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) { return 0, nil },
		},
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestEnforce_CounterFailure — counter error must also produce 500,
// never an implicit pass-through.
func TestEnforce_CounterFailure(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{MaxSims: 5}}
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) {
				return 0, errors.New("count failed")
			},
		},
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestEnforce_CorruptCacheEntry — if the cached JSON is unreadable, the
// middleware must fall back to the DB, not bubble up a 500.
func TestEnforce_CorruptCacheEntry(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{MaxSims: 5}}
	rdb := newTestRedis(t)

	// Seed Redis with garbage for this tenant.
	if err := rdb.Set(context.Background(), "tenant:limits:"+tenantID.String(), "not-json", time.Minute).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{
			LimitSIMs: func(_ context.Context, _ uuid.UUID) (int, error) { return 1, nil },
		},
		rdb, time.Minute, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitSIMs)(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fallback to DB)", rec.Code)
	}
	if lookup.calls.Load() != 1 {
		t.Errorf("GetByID calls = %d, want 1 (corrupt cache should miss)", lookup.calls.Load())
	}
}

// TestEnforce_UnknownResource — a misconfigured route passing a
// LimitKey with no registered counter should log and bypass rather
// than reject or 500.
func TestEnforce_UnknownResource(t *testing.T) {
	tenantID := uuid.New()
	lookup := &mockTenantLookup{tenant: &store.Tenant{MaxSims: 5}}
	mw := NewTenantLimitsMiddleware(
		lookup,
		map[LimitKey]CountFn{}, // no counters
		nil, 0, zerolog.Nop(),
	)

	rec := httptest.NewRecorder()
	mw.Enforce(LimitKey("mystery"))(passthroughHandler()).ServeHTTP(rec, requestWithTenant(tenantID))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (bypass on unknown resource)", rec.Code)
	}
}

// TestEnforce_AllResourceKeys — sanity check that every documented
// LimitKey maps to the correct tenant field in cachedLimits.forResource.
func TestCachedLimits_ForResource(t *testing.T) {
	c := cachedLimits{MaxSims: 1, MaxApns: 2, MaxUsers: 3, MaxAPIKeys: 4}
	cases := []struct {
		key  LimitKey
		want int
	}{
		{LimitSIMs, 1},
		{LimitAPNs, 2},
		{LimitUsers, 3},
		{LimitAPIKeys, 4},
	}
	for _, tc := range cases {
		got, ok := c.forResource(tc.key)
		if !ok {
			t.Errorf("forResource(%q) ok=false, want true", tc.key)
		}
		if got != tc.want {
			t.Errorf("forResource(%q) = %d, want %d", tc.key, got, tc.want)
		}
	}
	if _, ok := c.forResource(LimitKey("bogus")); ok {
		t.Error("forResource(bogus) should return ok=false")
	}
}

// TestTenantStore_Interface is a compile-time assertion that the real
// store satisfies TenantLookup. If someone breaks the signature this
// fails at build, not runtime.
var _ TenantLookup = (*store.TenantStore)(nil)
