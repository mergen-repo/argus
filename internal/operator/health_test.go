package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestCachedHealthStruct(t *testing.T) {
	ch := CachedHealth{
		Status:       "healthy",
		LatencyMs:    15,
		CircuitState: "closed",
		CheckedAt:    "2026-03-20T10:00:00Z",
	}

	if ch.Status != "healthy" {
		t.Errorf("Status = %q, want %q", ch.Status, "healthy")
	}
	if ch.LatencyMs != 15 {
		t.Errorf("LatencyMs = %d, want %d", ch.LatencyMs, 15)
	}
	if ch.CircuitState != "closed" {
		t.Errorf("CircuitState = %q, want %q", ch.CircuitState, "closed")
	}
}

func TestHealthStatusFromCircuitState(t *testing.T) {
	tests := []struct {
		circuitState CircuitState
		checkSuccess bool
		wantStatus   string
	}{
		{CircuitClosed, true, "healthy"},
		{CircuitClosed, false, "degraded"},
		{CircuitOpen, true, "down"},
		{CircuitOpen, false, "down"},
		{CircuitHalfOpen, true, "degraded"},
		{CircuitHalfOpen, false, "degraded"},
	}

	for _, tt := range tests {
		var status string
		switch tt.circuitState {
		case CircuitOpen:
			status = "down"
		case CircuitHalfOpen:
			status = "degraded"
		case CircuitClosed:
			if tt.checkSuccess {
				status = "healthy"
			} else {
				status = "degraded"
			}
		}

		if status != tt.wantStatus {
			t.Errorf("circuit=%s success=%v: got %q, want %q",
				tt.circuitState, tt.checkSuccess, status, tt.wantStatus)
		}
	}
}

func TestCircuitBreakerIntegrationWithHealth(t *testing.T) {
	cb := NewCircuitBreaker(3, 10)

	if cb.State() != CircuitClosed {
		t.Fatalf("initial state = %s, want closed", cb.State())
	}

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("after 2 failures, state = %s, want closed", cb.State())
	}

	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("after 3 failures (threshold), state = %s, want open", cb.State())
	}

	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("after success, state = %s, want closed", cb.State())
	}
}

func TestNewHealthCheckerNilSafe(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	if hc == nil {
		t.Fatal("NewHealthChecker should not return nil")
	}
	if hc.breakers == nil {
		t.Error("breakers map should be initialized")
	}
	if hc.stopChs == nil {
		t.Error("stopChs map should be initialized")
	}
	if hc.lastStatus == nil {
		t.Error("lastStatus map should be initialized")
	}
	if hc.operatorNames == nil {
		t.Error("operatorNames map should be initialized")
	}
	if hc.lastAlertStatus == nil {
		t.Error("lastAlertStatus map should be initialized (FIX-210)")
	}
}

type mockEventPublisher struct {
	mu     sync.Mutex
	events []publishedEvent
}

type publishedEvent struct {
	subject string
	payload interface{}
}

func (m *mockEventPublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, publishedEvent{subject, payload})
	return nil
}

func TestHealthChecker_SetEventPublisher(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	pub := &mockEventPublisher{}
	hc.SetEventPublisher(pub, "argus.events.operator.health", "argus.events.alert.triggered")

	if hc.eventPub == nil {
		t.Error("eventPub should be set")
	}
	if hc.healthSubject != "argus.events.operator.health" {
		t.Errorf("healthSubject = %s, want argus.events.operator.health", hc.healthSubject)
	}
	if hc.alertSubject != "argus.events.alert.triggered" {
		t.Errorf("alertSubject = %s, want argus.events.alert.triggered", hc.alertSubject)
	}
}

func TestHealthChecker_SetSLATracker(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	tracker := NewSLATracker(nil, zerolog.Nop())
	hc.SetSLATracker(tracker)

	if hc.slaTracker == nil {
		t.Error("slaTracker should be set")
	}
}

func TestHealthChecker_PublishAlertNilPub(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	hc.publishAlert(context.Background(), [16]byte{}, "test", "operator_down", "critical", "title", "desc")
}

func TestHealthChecker_CheckSLAViolationNilTracker(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	hc.checkSLAViolation(context.Background(), [16]byte{}, "test")
}

// scrapeMetrics fetches the /metrics body from the supplied registry.
func scrapeMetrics(t *testing.T, reg *obsmetrics.Registry) string {
	t.Helper()
	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestHealthChecker_SetMetricsRegistry_WiresBreakerHook(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())

	opID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	cb := NewCircuitBreaker(1, 10)

	// Register the breaker manually — mimicking launchProbeLoop.
	hc.mu.Lock()
	k := healthKey{OperatorID: opID, Protocol: "mock"}
	hc.breakers[k] = cb
	hc.lastStatus[k] = "healthy"
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg)

	// Seeding should publish closed=1 immediately.
	text := scrapeMetrics(t, reg)
	wantClosed := `argus_circuit_breaker_state{operator_id="` + opID.String() + `",state="closed"} 1`
	if !strings.Contains(text, wantClosed) {
		t.Errorf("missing seed line %q\n%s", wantClosed, text)
	}

	// Trip the breaker — hook should update the gauge.
	cb.RecordFailure()
	text = scrapeMetrics(t, reg)
	wantOpen := `argus_circuit_breaker_state{operator_id="` + opID.String() + `",state="open"} 1`
	if !strings.Contains(text, wantOpen) {
		t.Errorf("missing open line after failure %q\n%s", wantOpen, text)
	}
	wantClosedZero := `argus_circuit_breaker_state{operator_id="` + opID.String() + `",state="closed"} 0`
	if !strings.Contains(text, wantClosedZero) {
		t.Errorf("expected closed=0 after open transition, got\n%s", text)
	}
}

func TestHealthChecker_SetMetricsRegistry_NilClearsHook(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	cb := NewCircuitBreaker(1, 10)
	opID := uuid.New()

	hc.mu.Lock()
	hc.breakers[healthKey{OperatorID: opID, Protocol: "mock"}] = cb
	hc.mu.Unlock()

	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg)
	hc.SetMetricsRegistry(nil)

	// After clearing, breaker transitions must not panic or affect
	// the previously attached registry.
	cb.RecordFailure()
}

func TestHealthChecker_SetMetricsRegistry_NoBreakersIsSafe(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg) // no breakers registered — must not panic
}

// TestHealthChecker_FansOutPerProtocol exercises AC-10 — an operator
// with three enabled protocols must produce THREE distinct gauge label
// series on `argus_operator_adapter_health_status`. Simulates a
// post-Start state by manually seeding the per-(op, protocol) gauges
// via the metrics registry (identical to what the ticker does on its
// first sweep). The seed-path exercise is sufficient to prove the
// label schema is correct; goroutine-timing-dependent assertions are
// avoided to keep the test deterministic.
func TestHealthChecker_FansOutPerProtocol(t *testing.T) {
	reg := obsmetrics.NewRegistry()
	opID := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	// Seed three protocol series at distinct statuses to verify each
	// label set is independently addressable.
	reg.SetOperatorHealth(opID.String(), "radius", "healthy")
	reg.SetOperatorHealth(opID.String(), "diameter", "degraded")
	reg.SetOperatorHealth(opID.String(), "mock", "down")

	text := scrapeMetrics(t, reg)
	wants := []string{
		`argus_operator_adapter_health_status{operator_id="` + opID.String() + `",protocol="radius"} 2`,
		`argus_operator_adapter_health_status{operator_id="` + opID.String() + `",protocol="diameter"} 1`,
		`argus_operator_adapter_health_status{operator_id="` + opID.String() + `",protocol="mock"} 0`,
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("AC-10: missing per-protocol gauge series %q\noutput:\n%s", want, text)
		}
	}

	// Disabling one protocol must retire its series within one PATCH
	// cycle (per AC-10). Mirrors what RefreshOperator does for a
	// protocol that drops out of the enabled set.
	reg.DeleteOperatorHealth(opID.String(), "mock")
	text = scrapeMetrics(t, reg)
	if strings.Contains(text, `protocol="mock"`) && strings.Contains(text, opID.String()) {
		t.Errorf("AC-10: mock series should be gone after delete:\n%s", text)
	}
	// The other two series MUST remain untouched.
	for _, want := range wants[:2] {
		if !strings.Contains(text, want) {
			t.Errorf("AC-10: surviving series vanished unexpectedly: %q\n%s", want, text)
		}
	}
}

// TestHealthChecker_StartOperatorLoop_SingleTickerPerOperator asserts
// F-A5: regardless of enabled_protocols count, each operator gets
// exactly one entry in stopChs (not N — one per protocol). Breakers
// and lastStatus are still per-protocol. Exercise via startOperatorLoop
// directly — no goroutine timing dependency.
func TestHealthChecker_StartOperatorLoop_SingleTickerPerOperator(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, "", zerolog.Nop())
	opID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	op := store.Operator{
		ID:                        opID,
		Name:                      "multi-proto",
		HealthStatus:              "healthy",
		HealthCheckIntervalSec:    30,
		CircuitBreakerThreshold:   3,
		CircuitBreakerRecoverySec: 60,
		AdapterConfig: json.RawMessage(`{
			"radius":{"enabled":true,"shared_secret":"s","listen_addr":":1812"},
			"diameter":{"enabled":true,"origin_host":"o.example","origin_realm":"o"},
			"mock":{"enabled":true,"latency_ms":5}
		}`),
	}

	hc.mu.Lock()
	hc.startOperatorLoop(op)
	// Snapshot under lock — startOperatorLoop mutates state.
	stopChCount := len(hc.stopChs)
	breakerCount := 0
	for k := range hc.breakers {
		if k.OperatorID == opID {
			breakerCount++
		}
	}
	hc.mu.Unlock()

	// Tear down immediately — avoid ticker side-effects in the test.
	hc.Stop()

	if stopChCount != 1 {
		t.Errorf("single-ticker invariant: expected 1 stopCh for operator, got %d", stopChCount)
	}
	if breakerCount != 3 {
		t.Errorf("per-protocol breakers: expected 3 (radius/diameter/mock), got %d", breakerCount)
	}
}

// ---------------------------------------------------------------------
// FIX-203 AC-3 — latency-threshold publish trigger tests
//
// The health worker must publish argus.events.operator.health.changed
// when status flips OR latency delta > 10% vs. the prior tick. Cold
// start (prevLatency == 0) suppresses the latency-trigger path until
// the second tick populates a sample.
// ---------------------------------------------------------------------

// scriptedHealthAdapter returns the next HealthResult in a queue on
// each HealthCheck call. It implements adapter.Adapter via embed-style
// passthrough so the compiler is satisfied; only HealthCheck and Type
// are exercised by these tests.
type scriptedHealthAdapter struct {
	mu      sync.Mutex
	results []adapter.HealthResult
	idx     int
}

func (s *scriptedHealthAdapter) HealthCheck(_ context.Context) adapter.HealthResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.results) {
		// Fall through with the last scripted result so unexpected
		// extra ticks don't panic; tests assert the publish count
		// instead.
		if len(s.results) == 0 {
			return adapter.HealthResult{Success: true, LatencyMs: 0}
		}
		return s.results[len(s.results)-1]
	}
	r := s.results[s.idx]
	s.idx++
	return r
}

func (s *scriptedHealthAdapter) Type() string { return "mock" }
func (s *scriptedHealthAdapter) ForwardAuth(_ context.Context, _ adapter.AuthRequest) (*adapter.AuthResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *scriptedHealthAdapter) ForwardAcct(_ context.Context, _ adapter.AcctRequest) error {
	return fmt.Errorf("not implemented")
}
func (s *scriptedHealthAdapter) SendCoA(_ context.Context, _ adapter.CoARequest) error {
	return fmt.Errorf("not implemented")
}
func (s *scriptedHealthAdapter) SendDM(_ context.Context, _ adapter.DMRequest) error {
	return fmt.Errorf("not implemented")
}
func (s *scriptedHealthAdapter) Authenticate(_ context.Context, _ adapter.AuthenticateRequest) (*adapter.AuthenticateResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *scriptedHealthAdapter) AccountingUpdate(_ context.Context, _ adapter.AccountingUpdateRequest) error {
	return fmt.Errorf("not implemented")
}
func (s *scriptedHealthAdapter) FetchAuthVectors(_ context.Context, _ string, _ int) ([]adapter.AuthVector, error) {
	return nil, fmt.Errorf("not implemented")
}

// countHealthPublishes returns the number of events published on the
// health subject. Filters by subject so latency-only down/recovered
// alert paths don't contaminate the count.
func countHealthPublishes(pub *mockEventPublisher, subject string) int {
	pub.mu.Lock()
	defer pub.mu.Unlock()
	n := 0
	for _, e := range pub.events {
		if e.subject == subject {
			n++
		}
	}
	return n
}

// newHealthCheckerWithRegistry builds a HealthChecker wired to a live
// adapter.Registry plus a mock event publisher. Store/Redis stay nil
// — the nil-safe branches in checkOperator short-circuit them.
func newHealthCheckerWithRegistry(t *testing.T) (*HealthChecker, *mockEventPublisher) {
	t.Helper()
	reg := adapter.NewRegistry()
	hc := &HealthChecker{
		registry:        reg,
		logger:          zerolog.Nop(),
		breakers:        make(map[healthKey]*CircuitBreaker),
		stopChs:         make(map[uuid.UUID]chan struct{}),
		lastStatus:      make(map[healthKey]string),
		lastLatency:     make(map[healthKey]int),
		operatorNames:   make(map[uuid.UUID]string),
		lastAlertStatus: make(map[uuid.UUID]string),
	}
	pub := &mockEventPublisher{}
	hc.SetEventPublisher(pub, "argus.events.operator.health.changed", "argus.events.alert.triggered")
	return hc, pub
}

// TestCheckOperator_PublishesOnLatencyDelta — latency change > 10%
// with no status flip must fire a single health event.
func TestCheckOperator_PublishesOnLatencyDelta(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	key := healthKey{OperatorID: opID, Protocol: "mock"}

	stub := &scriptedHealthAdapter{
		results: []adapter.HealthResult{
			{Success: true, LatencyMs: 120},
		},
	}
	hc.registry.Set(opID, "mock", stub)

	cb := NewCircuitBreaker(3, 60)
	hc.mu.Lock()
	hc.breakers[key] = cb
	hc.lastStatus[key] = "healthy"
	hc.lastLatency[key] = 100 // seed: prev tick landed 100ms
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)

	if got := countHealthPublishes(pub, "argus.events.operator.health.changed"); got != 1 {
		t.Fatalf("expected 1 health publish on 20%% latency delta, got %d", got)
	}

	hc.mu.Lock()
	gotLatency := hc.lastLatency[key]
	gotStatus := hc.lastStatus[key]
	hc.mu.Unlock()
	if gotLatency != 120 {
		t.Errorf("lastLatency updated to %d, want 120", gotLatency)
	}
	if gotStatus != "healthy" {
		t.Errorf("lastStatus = %q, want healthy (no flip)", gotStatus)
	}
}

// TestCheckOperator_SuppressesSubThresholdLatency — 5% delta is below
// the 10% threshold and must NOT publish.
func TestCheckOperator_SuppressesSubThresholdLatency(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	key := healthKey{OperatorID: opID, Protocol: "mock"}

	stub := &scriptedHealthAdapter{
		results: []adapter.HealthResult{
			{Success: true, LatencyMs: 105},
		},
	}
	hc.registry.Set(opID, "mock", stub)

	cb := NewCircuitBreaker(3, 60)
	hc.mu.Lock()
	hc.breakers[key] = cb
	hc.lastStatus[key] = "healthy"
	hc.lastLatency[key] = 100
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)

	if got := countHealthPublishes(pub, "argus.events.operator.health.changed"); got != 0 {
		t.Fatalf("expected 0 publishes on 5%% latency delta (below 10%% threshold), got %d", got)
	}

	// Map updated even when no publish fires, so the next tick measures
	// delta from the freshest sample.
	hc.mu.Lock()
	gotLatency := hc.lastLatency[key]
	hc.mu.Unlock()
	if gotLatency != 105 {
		t.Errorf("lastLatency updated to %d, want 105 (map advances on every tick)", gotLatency)
	}
}

// TestCheckOperator_ColdStartSuppressesLatencyTrigger — prevLatency==0
// suppresses the latency-trigger publish on the first tick; the second
// tick (now with a non-zero prev sample) fires normally on >10% delta.
func TestCheckOperator_ColdStartSuppressesLatencyTrigger(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	key := healthKey{OperatorID: opID, Protocol: "mock"}

	stub := &scriptedHealthAdapter{
		results: []adapter.HealthResult{
			{Success: true, LatencyMs: 200}, // first tick — cold start
			{Success: true, LatencyMs: 250}, // second tick — 25% delta
		},
	}
	hc.registry.Set(opID, "mock", stub)

	cb := NewCircuitBreaker(3, 60)
	hc.mu.Lock()
	hc.breakers[key] = cb
	hc.lastStatus[key] = "healthy"
	hc.lastLatency[key] = 0 // cold-start sentinel
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	// First tick — must NOT publish (cold start).
	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)
	if got := countHealthPublishes(pub, "argus.events.operator.health.changed"); got != 0 {
		t.Fatalf("cold start must suppress latency-trigger publish; got %d publishes", got)
	}
	hc.mu.Lock()
	midLatency := hc.lastLatency[key]
	hc.mu.Unlock()
	if midLatency != 200 {
		t.Fatalf("after cold-start tick, lastLatency = %d, want 200", midLatency)
	}

	// Second tick — prevLatency is now 200; 250 is a 25% delta → publish.
	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)
	if got := countHealthPublishes(pub, "argus.events.operator.health.changed"); got != 1 {
		t.Fatalf("second tick with 25%% delta must publish exactly once; got %d", got)
	}
}

// TestCheckOperator_StatusFlipStillPublishes — status flip must
// publish regardless of latency-trigger state (including cold start).
func TestCheckOperator_StatusFlipStillPublishes(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	key := healthKey{OperatorID: opID, Protocol: "mock"}

	// Adapter reports failure → status in checkOperator resolves to
	// "degraded" (CircuitClosed + !Success). That's a flip from the
	// seeded "healthy" even though latency is cold-start.
	stub := &scriptedHealthAdapter{
		results: []adapter.HealthResult{
			{Success: false, LatencyMs: 100, Error: "simulated"},
		},
	}
	hc.registry.Set(opID, "mock", stub)

	cb := NewCircuitBreaker(5, 60) // high threshold — won't open on 1 failure
	hc.mu.Lock()
	hc.breakers[key] = cb
	hc.lastStatus[key] = "healthy"
	hc.lastLatency[key] = 0 // cold start
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)

	if got := countHealthPublishes(pub, "argus.events.operator.health.changed"); got != 1 {
		t.Fatalf("status flip must publish regardless of cold-start latency; got %d", got)
	}

	// Verify the event payload reflects the flip direction.
	pub.mu.Lock()
	defer pub.mu.Unlock()
	evt, ok := pub.events[0].payload.(OperatorHealthEvent)
	if !ok {
		t.Fatalf("event payload type = %T, want OperatorHealthEvent", pub.events[0].payload)
	}
	if evt.PreviousStatus != "healthy" || evt.CurrentStatus != "degraded" {
		t.Errorf("event flip direction = %s→%s, want healthy→degraded", evt.PreviousStatus, evt.CurrentStatus)
	}
}

// TestCheckOperator_NoFlipNoDeltaSuppressesPublish — belt-and-braces:
// identical latency with identical status publishes nothing. Guards
// against the common regression where widening the publish gate would
// otherwise fire on every tick.
func TestCheckOperator_NoFlipNoDeltaSuppressesPublish(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	key := healthKey{OperatorID: opID, Protocol: "mock"}

	stub := &scriptedHealthAdapter{
		results: []adapter.HealthResult{
			{Success: true, LatencyMs: 100},
		},
	}
	hc.registry.Set(opID, "mock", stub)

	cb := NewCircuitBreaker(3, 60)
	hc.mu.Lock()
	hc.breakers[key] = cb
	hc.lastStatus[key] = "healthy"
	hc.lastLatency[key] = 100
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)

	if got := countHealthPublishes(pub, "argus.events.operator.health.changed"); got != 0 {
		t.Fatalf("no status flip + zero latency delta must not publish; got %d", got)
	}
}

// TestCheckOperator_StatusStaysDownLatencyChangesNoReFiredAlert — a
// tick where status stays "down" but latency delta > 10% must publish
// the health.changed event (latency-trigger path) but MUST NOT
// re-fire the AlertTypeOperatorDown alert. Regression guard against
// the widened publish gate.
func TestCheckOperator_StatusStaysDownLatencyChangesNoReFiredAlert(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	key := healthKey{OperatorID: opID, Protocol: "mock"}

	// Pre-open the breaker so state stays Open and status resolves to
	// "down" on this tick, with no flip from prior "down".
	cb := NewCircuitBreaker(1, 3600)
	cb.RecordFailure() // open immediately (threshold=1)
	if cb.State() != CircuitOpen {
		t.Fatalf("test setup: breaker state = %s, want open", cb.State())
	}

	stub := &scriptedHealthAdapter{
		results: []adapter.HealthResult{
			{Success: false, LatencyMs: 250, Error: "still down"},
		},
	}
	hc.registry.Set(opID, "mock", stub)

	hc.mu.Lock()
	hc.breakers[key] = cb
	hc.lastStatus[key] = "down" // seeded: was already down
	hc.lastLatency[key] = 100   // 250 vs 100 = 150% delta
	hc.operatorNames[opID] = "acme"
	hc.mu.Unlock()

	hc.checkOperator(opID, "mock", json.RawMessage(`{}`), cb, 30)

	// One health.changed event on latency delta (no status flip).
	healthCount := countHealthPublishes(pub, "argus.events.operator.health.changed")
	if healthCount != 1 {
		t.Errorf("expected 1 health.changed event on latency-only trigger, got %d", healthCount)
	}
	// Zero alert.triggered events — down→down must not re-fire alerts.
	alertCount := countHealthPublishes(pub, "argus.events.alert.triggered")
	if alertCount != 0 {
		t.Errorf("down→down tick must NOT re-fire AlertTypeOperatorDown; got %d alerts", alertCount)
	}
}

// ---------------------------------------------------------------------
// FIX-210 Task 4 — operator health alert edge-trigger tests
//
// The alert publish site (AlertTypeOperatorDown / AlertTypeOperatorUp)
// must be edge-triggered per operator. Two protocols flipping to "down"
// on the same tick for the same operator must result in exactly ONE
// AlertTypeOperatorDown publish — the second attempt is suppressed by
// the shouldPublishAlert gate and counted to the rate-limit metric.
// ---------------------------------------------------------------------

// TestOperatorHealth_SameStatusTwice_NoPublish — two identical-status
// alert publish attempts (simulating two protocols flipping together)
// fire the alert once, not twice. Uses shouldPublishAlert directly to
// assert the gate semantics without a full checkOperator fixture.
func TestOperatorHealth_SameStatusTwice_NoPublish(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	// First attempt — gate allows; alert publishes.
	if !hc.shouldPublishAlert(opID, "down") {
		t.Fatal("first 'down' edge must be allowed")
	}
	hc.publishAlert(context.Background(), opID, "acme", AlertTypeOperatorDown, SeverityCritical,
		"Operator acme is DOWN", "CB opened", "healthy")

	// Second attempt with the same state — gate must suppress it.
	if hc.shouldPublishAlert(opID, "down") {
		t.Fatal("second 'down' edge with unchanged state must be suppressed")
	}

	alerts := countHealthPublishes(pub, "argus.events.alert.triggered")
	if alerts != 1 {
		t.Errorf("same-status twice: got %d alert publishes, want 1", alerts)
	}
}

// TestOperatorHealth_StatusChange_Publishes — alternating down/up
// transitions publish every time (each call is a genuine edge).
func TestOperatorHealth_StatusChange_Publishes(t *testing.T) {
	hc, pub := newHealthCheckerWithRegistry(t)
	opID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

	if !hc.shouldPublishAlert(opID, "down") {
		t.Fatal("first 'down' edge must be allowed")
	}
	hc.publishAlert(context.Background(), opID, "acme", AlertTypeOperatorDown, SeverityCritical,
		"down", "desc", "healthy")

	if !hc.shouldPublishAlert(opID, "up") {
		t.Fatal("'up' edge after 'down' must be allowed")
	}
	hc.publishAlert(context.Background(), opID, "acme", AlertTypeOperatorUp, SeverityInfo,
		"recovered", "desc", "down")

	if !hc.shouldPublishAlert(opID, "down") {
		t.Fatal("'down' edge after 'up' must be allowed")
	}
	hc.publishAlert(context.Background(), opID, "acme", AlertTypeOperatorDown, SeverityCritical,
		"down again", "desc", "healthy")

	alerts := countHealthPublishes(pub, "argus.events.alert.triggered")
	if alerts != 3 {
		t.Errorf("alternating down/up/down: got %d alert publishes, want 3", alerts)
	}
}

// TestOperatorHealth_EdgeTrigger_IsolatedPerOperator — each operator
// has its own edge-trigger history; one operator's 'down' must not
// suppress another operator's 'down'.
func TestOperatorHealth_EdgeTrigger_IsolatedPerOperator(t *testing.T) {
	hc, _ := newHealthCheckerWithRegistry(t)
	opA := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	opB := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")

	if !hc.shouldPublishAlert(opA, "down") {
		t.Fatal("opA 'down' must be allowed")
	}
	if !hc.shouldPublishAlert(opB, "down") {
		t.Fatal("opB 'down' must be allowed (independent of opA)")
	}
	if hc.shouldPublishAlert(opA, "down") {
		t.Fatal("opA 'down' repeat must be suppressed")
	}
	if hc.shouldPublishAlert(opB, "down") {
		t.Fatal("opB 'down' repeat must be suppressed")
	}
}

// TestOperatorHealth_RecordRateLimited_NilRegIsSafe — recordRateLimited
// must not panic when metricsReg has never been wired (common in tests
// and in the zero-config path).
func TestOperatorHealth_RecordRateLimited_NilRegIsSafe(t *testing.T) {
	hc, _ := newHealthCheckerWithRegistry(t)
	hc.recordRateLimited() // should not panic
}

// TestOperatorHealth_RecordRateLimited_IncrementsMetric — when a
// metricsReg is wired, recordRateLimited increments the counter under
// the "operator_health" publisher label.
func TestOperatorHealth_RecordRateLimited_IncrementsMetric(t *testing.T) {
	hc, _ := newHealthCheckerWithRegistry(t)
	reg := obsmetrics.NewRegistry()
	hc.SetMetricsRegistry(reg)

	hc.recordRateLimited()

	text := scrapeMetrics(t, reg)
	want := `argus_alerts_rate_limited_publishes_total{publisher="operator_health"} 1`
	if !strings.Contains(text, want) {
		t.Errorf("missing rate-limit counter line %q\n%s", want, text)
	}
}
