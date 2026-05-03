package dsl

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// STORY-095 Phase 11 Task 6 — DSL device.imei_in_pool() real lookup wiring.
//
// These tests cover the placeholder→real-lookup transition:
//
//   - Nil-lookup fallback preserves the STORY-094 placeholder behaviour
//     (returns false; existing TestEvaluator_DeviceImeiInPool_Placeholder
//     in evaluator_device_test.go also covers this from the synthetic-ctx
//     angle, but kept here as a focused unit).
//   - Empty/short IMEI short-circuits without consulting the lookup.
//   - Hit/miss propagation from the lookup to the evaluator return value.
//   - Per-evaluation-pass cache: same predicate twice in one Evaluate()
//     call → one lookup invocation. Different pools or different IMEIs
//     correctly miss the cache.
//   - Lookup error → fail-open (returns false) per plan §DSL.

// mockIMEIPoolLookuper is a counting stub that lets us assert how many
// LookupKind calls reach the wire from a single Evaluate() pass.
type mockIMEIPoolLookuper struct {
	hits  map[string]bool // key = "<pool>:<imei>"
	calls int
	err   error
	// observed per-call inputs (last-wins) for tenant-id propagation asserts
	lastTenant uuid.UUID
	lastPool   string
	lastIMEI   string
}

func (m *mockIMEIPoolLookuper) LookupKind(_ context.Context, tenantID uuid.UUID, imei string, pool string) (bool, error) {
	m.calls++
	m.lastTenant = tenantID
	m.lastPool = pool
	m.lastIMEI = imei
	if m.err != nil {
		return false, m.err
	}
	return m.hits[pool+":"+imei], nil
}

func TestEvaluator_DeviceImeiInPool_NilLookup_ReturnsFalse(t *testing.T) {
	e := NewEvaluator() // no pools wired

	ctx := SessionContext{
		TenantID:  uuid.New().String(),
		IMEI:      "359211089765432",
		poolCache: map[string]bool{},
	}

	got := e.getConditionFieldValue(ctx, "device.imei_in_pool(blacklist)")
	b, ok := got.(bool)
	if !ok || b != false {
		t.Fatalf("nil-lookup branch: got %v (%T), want false", got, got)
	}
}

func TestEvaluator_DeviceImeiInPool_EmptyIMEI_ReturnsFalse(t *testing.T) {
	mock := &mockIMEIPoolLookuper{}
	e := NewEvaluatorWithPools(mock)

	ctx := SessionContext{
		TenantID:  uuid.New().String(),
		IMEI:      "", // empty IMEI MUST short-circuit
		poolCache: map[string]bool{},
	}

	got := e.getConditionFieldValue(ctx, "device.imei_in_pool(whitelist)")
	if b, ok := got.(bool); !ok || b != false {
		t.Fatalf("empty-IMEI branch: got %v (%T), want false", got, got)
	}
	if mock.calls != 0 {
		t.Errorf("empty IMEI must NOT invoke lookup; got %d calls", mock.calls)
	}

	// Short IMEI (not 15 digits) — same short-circuit.
	ctx.IMEI = "12345"
	got = e.getConditionFieldValue(ctx, "device.imei_in_pool(whitelist)")
	if b, ok := got.(bool); !ok || b != false {
		t.Fatalf("short-IMEI branch: got %v (%T), want false", got, got)
	}
	if mock.calls != 0 {
		t.Errorf("short IMEI must NOT invoke lookup; got %d calls", mock.calls)
	}
}

func TestEvaluator_DeviceImeiInPool_LookupHit_ReturnsTrue(t *testing.T) {
	tenantID := uuid.New()
	imei := "359211089765432"

	mock := &mockIMEIPoolLookuper{
		hits: map[string]bool{
			"blacklist:" + imei: true,
		},
	}
	e := NewEvaluatorWithPools(mock)

	ctx := SessionContext{
		TenantID:  tenantID.String(),
		IMEI:      imei,
		poolCache: map[string]bool{},
	}

	got := e.getConditionFieldValue(ctx, "device.imei_in_pool(blacklist)")
	if b, ok := got.(bool); !ok || b != true {
		t.Fatalf("lookup hit: got %v (%T), want true", got, got)
	}
	if mock.calls != 1 {
		t.Errorf("expected exactly 1 lookup call, got %d", mock.calls)
	}
	if mock.lastTenant != tenantID {
		t.Errorf("tenant_id propagation: got %s, want %s", mock.lastTenant, tenantID)
	}
	if mock.lastPool != "blacklist" {
		t.Errorf("pool propagation: got %q, want %q", mock.lastPool, "blacklist")
	}
	if mock.lastIMEI != imei {
		t.Errorf("imei propagation: got %q, want %q", mock.lastIMEI, imei)
	}
}

func TestEvaluator_DeviceImeiInPool_CacheHit(t *testing.T) {
	// AC-9 cache trace: two `device.imei_in_pool('blacklist')` predicates
	// inside the same WHEN clause MUST share a single lookup invocation.
	imei := "359211089765432"
	tenantID := uuid.New()

	mock := &mockIMEIPoolLookuper{
		hits: map[string]bool{"blacklist:" + imei: true},
	}
	e := NewEvaluatorWithPools(mock)

	src := `POLICY "ac-9-cache-blacklist-OR-blacklist" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN device.imei_in_pool("blacklist") OR device.imei_in_pool("blacklist") {
            ACTION block()
        }
    }
}`
	compiled, errs, err := CompileSource(src)
	if err != nil {
		t.Fatalf("CompileSource: %v", err)
	}
	for _, e := range errs {
		if e.Severity == "error" {
			t.Fatalf("compile error: %s", e.Error())
		}
	}

	ctx := SessionContext{
		APN:      "iot.data",
		TenantID: tenantID.String(),
		IMEI:     imei,
	}

	res, err := e.Evaluate(ctx, compiled)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.MatchedRules != 1 {
		t.Errorf("matched rules: got %d, want 1 (one WHEN block fired)", res.MatchedRules)
	}
	if res.Allow != false {
		t.Errorf("Allow: got %v, want false (block action)", res.Allow)
	}
	if mock.calls != 1 {
		t.Errorf("cache trace: same predicate twice → got %d calls, want 1", mock.calls)
	}
}

func TestEvaluator_DeviceImeiInPool_DifferentPoolsNoCache(t *testing.T) {
	// Different pool names MUST keep distinct cache entries.
	imei := "359211089765432"
	tenantID := uuid.New()

	mock := &mockIMEIPoolLookuper{
		hits: map[string]bool{
			"blacklist:" + imei: true,
			"whitelist:" + imei: false,
		},
	}
	e := NewEvaluatorWithPools(mock)

	src := `POLICY "different-pools-no-cache" {
    MATCH { apn = "iot.data" }
    RULES {
        WHEN device.imei_in_pool("blacklist") OR device.imei_in_pool("whitelist") {
            ACTION block()
        }
    }
}`
	compiled, errs, err := CompileSource(src)
	if err != nil {
		t.Fatalf("CompileSource: %v", err)
	}
	for _, e := range errs {
		if e.Severity == "error" {
			t.Fatalf("compile error: %s", e.Error())
		}
	}

	ctx := SessionContext{
		APN:      "iot.data",
		TenantID: tenantID.String(),
		IMEI:     imei,
	}

	if _, err := e.Evaluate(ctx, compiled); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Two distinct cache keys → two lookup invocations.
	// Note: short-circuit OR may stop after the first true. To force both
	// predicates to evaluate, the first one must miss; we set blacklist=true
	// so OR short-circuits at the FIRST predicate and skips the whitelist
	// branch entirely → 1 call. We instead test the inverse: blacklist
	// false, whitelist true (or both false) → both branches evaluated.
	// Re-run with both miss to force dual evaluation.
	mock.calls = 0
	mock.hits = map[string]bool{} // both miss → OR forces both branches
	if _, err := e.Evaluate(ctx, compiled); err != nil {
		t.Fatalf("Evaluate (both-miss): %v", err)
	}
	if mock.calls != 2 {
		t.Errorf("different pools both-miss: got %d calls, want 2", mock.calls)
	}
}

func TestEvaluator_DeviceImeiInPool_LookupError_ReturnsFalse(t *testing.T) {
	mock := &mockIMEIPoolLookuper{
		err: errors.New("simulated db outage"),
	}
	e := NewEvaluatorWithPools(mock)

	ctx := SessionContext{
		TenantID:  uuid.New().String(),
		IMEI:      "359211089765432",
		poolCache: map[string]bool{},
	}

	got := e.getConditionFieldValue(ctx, "device.imei_in_pool(blacklist)")
	if b, ok := got.(bool); !ok || b != false {
		t.Fatalf("lookup error: got %v (%T), want false (fail-open)", got, got)
	}
	if mock.calls != 1 {
		t.Errorf("error path must invoke lookup once, got %d", mock.calls)
	}

	// Subsequent call with the same key MUST hit the negative cache and
	// not re-issue another lookup, even though the first call errored.
	got = e.getConditionFieldValue(ctx, "device.imei_in_pool(blacklist)")
	if b, ok := got.(bool); !ok || b != false {
		t.Fatalf("repeat after error: got %v (%T), want false", got, got)
	}
	if mock.calls != 1 {
		t.Errorf("repeat call must hit cache; got %d total calls, want 1", mock.calls)
	}
}

func TestEvaluator_DeviceImeiInPool_InvalidTenantID_ReturnsFalse(t *testing.T) {
	// Defensive guard: a malformed tenant_id string MUST NOT panic and MUST
	// short-circuit to false without invoking the lookup.
	mock := &mockIMEIPoolLookuper{}
	e := NewEvaluatorWithPools(mock)

	ctx := SessionContext{
		TenantID:  "not-a-uuid",
		IMEI:      "359211089765432",
		poolCache: map[string]bool{},
	}

	got := e.getConditionFieldValue(ctx, "device.imei_in_pool(blacklist)")
	if b, ok := got.(bool); !ok || b != false {
		t.Fatalf("invalid tenant_id: got %v (%T), want false", got, got)
	}
	if mock.calls != 0 {
		t.Errorf("invalid tenant_id must NOT invoke lookup; got %d calls", mock.calls)
	}
}
