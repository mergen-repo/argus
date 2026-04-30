package digest

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/severity"
	"github.com/rs/zerolog"
)

// ---------------------------------------------------------------------------
// Mocks for the narrow worker interfaces. Each captures call arguments and
// returns operator-supplied responses so tests express behaviour, not state.
// ---------------------------------------------------------------------------

type mockSimAggregator struct {
	activeReturn     int64
	activeErr        error
	transitionReturn int64
	transitionErr    error
	activeCalls      int
	transitionCalls  int
}

func (m *mockSimAggregator) CountActiveAllTenants(_ context.Context) (int64, error) {
	m.activeCalls++
	return m.activeReturn, m.activeErr
}

func (m *mockSimAggregator) CountStateTransitionsToInactiveAllTenants(_ context.Context, _, _ time.Time) (int64, error) {
	m.transitionCalls++
	return m.transitionReturn, m.transitionErr
}

// mockCDRAggregator answers based on a per-call queue OR a constant fallback.
// The first call to SumBytesAllTenantsInWindow consumes returnQueue[0]; once
// the queue is empty the fallback is returned. This lets a test stage the
// current window distinct from the rolling baseline.
type mockCDRAggregator struct {
	returnQueue []int64
	fallback    int64
	err         error
	calls       int
	mu          sync.Mutex
}

func (m *mockCDRAggregator) SumBytesAllTenantsInWindow(_ context.Context, _, _ time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if len(m.returnQueue) > 0 {
		v := m.returnQueue[0]
		m.returnQueue = m.returnQueue[1:]
		return v, m.err
	}
	return m.fallback, m.err
}

type mockViolationAggregator struct {
	returnQueue []int64
	fallback    int64
	err         error
	calls       int
	mu          sync.Mutex
}

func (m *mockViolationAggregator) CountInWindowAllTenants(_ context.Context, _, _ time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if len(m.returnQueue) > 0 {
		v := m.returnQueue[0]
		m.returnQueue = m.returnQueue[1:]
		return v, m.err
	}
	return m.fallback, m.err
}

// recordingNotifier captures every NotifyRequest passed in. The single emit
// (call count + payload) is the primary assertion surface for the worker.
type recordingNotifier struct {
	mu       sync.Mutex
	requests []notification.NotifyRequest
	err      error
}

func (r *recordingNotifier) Notify(_ context.Context, req notification.NotifyRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return r.err
}

func (r *recordingNotifier) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recordingNotifier) last() *notification.NotifyRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return nil
	}
	c := r.requests[len(r.requests)-1]
	return &c
}

// recordingPublisher captures every (subject, payload) tuple. Tests that
// care about the WS Live Stream surface inspect this; tests that don't can
// pass nil for eventBus and the worker skips the publish path.
type recordingPublisher struct {
	mu       sync.Mutex
	subjects []string
	payloads []interface{}
	err      error
}

func (r *recordingPublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subjects = append(r.subjects, subject)
	r.payloads = append(r.payloads, payload)
	return r.err
}

// silentLogger discards everything; we want tests to assert behaviour, not
// log noise.
func silentLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

// fixedClock returns a deterministic clock function pinned to t.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// defaultThresholds returns the same defaults LoadThresholds would, without
// requiring env-var manipulation in every test.
func defaultThresholds() Thresholds {
	return Thresholds{
		MassOfflinePct:      5.0,
		MassOfflineFloor:    10,
		TrafficSpikeRatio:   3.0,
		QuotaBreachCount:    50,
		QuotaBreachFloor:    10,
		ViolationSurgeRatio: 2.0,
		ViolationSurgeFloor: 10,
	}
}

// newTestWorker assembles a worker with the supplied mocks and default
// thresholds. eventBus is left nil unless a test wires a recordingPublisher
// directly.
func newTestWorker(
	sims simAggregator,
	cdrs cdrAggregator,
	viols violationAggregator,
	notif notifier,
	pub envelopePublisher,
	th Thresholds,
) *Worker {
	return newWorkerWithDeps(sims, cdrs, viols, notif, pub, th, silentLogger(), fixedClock(time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)))
}

// ---------------------------------------------------------------------------
// mass_offline tests
// ---------------------------------------------------------------------------

func TestWorker_MassOfflineThresholdNotMet_NoEmit(t *testing.T) {
	// 12 SIMs offline of 200 active = 6%. Floor is 20 (raised), so we are
	// above the percentage but below the absolute floor → no emit.
	sims := &mockSimAggregator{activeReturn: 200, transitionReturn: 12}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	th := defaultThresholds()
	th.MassOfflineFloor = 20

	w := newTestWorker(sims, cdrs, viols, notif, nil, th)

	if err := w.checkMassOffline(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkMassOffline returned error: %v", err)
	}
	if got := notif.calls(); got != 0 {
		t.Errorf("notify calls = %d, want 0 (below absolute floor)", got)
	}
}

func TestWorker_MassOfflineThresholdMet_EmitsTierMedium(t *testing.T) {
	// 12 of 200 = 6% — above 5% pct, above default floor=10 → emit medium.
	sims := &mockSimAggregator{activeReturn: 200, transitionReturn: 12}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkMassOffline(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkMassOffline returned error: %v", err)
	}
	if got := notif.calls(); got != 1 {
		t.Fatalf("notify calls = %d, want 1", got)
	}
	req := notif.last()
	if req.Severity != severity.Medium {
		t.Errorf("severity = %s, want medium (6%% should band as medium)", req.Severity)
	}
	if req.Source != digestSource {
		t.Errorf("source = %q, want %q (digest tier guard requires it)", req.Source, digestSource)
	}
	if string(req.EventType) != "fleet.mass_offline" {
		t.Errorf("event_type = %s, want fleet.mass_offline", req.EventType)
	}
	if req.ScopeType != notification.ScopeSystem {
		t.Errorf("scope_type = %s, want system", req.ScopeType)
	}
}

func TestWorker_MassOfflineCritical_EmitsTierCritical(t *testing.T) {
	// 80 of 200 offline = 40% → critical band.
	sims := &mockSimAggregator{activeReturn: 200, transitionReturn: 80}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkMassOffline(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkMassOffline returned error: %v", err)
	}
	if notif.calls() != 1 {
		t.Fatalf("notify calls = %d, want 1", notif.calls())
	}
	if got := notif.last().Severity; got != severity.Critical {
		t.Errorf("severity = %s, want critical (40%% should band as critical)", got)
	}
}

func TestWorker_MassOffline_ZeroActiveFleet_NoEmit(t *testing.T) {
	sims := &mockSimAggregator{activeReturn: 0, transitionReturn: 50}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkMassOffline(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkMassOffline returned error: %v", err)
	}
	if notif.calls() != 0 {
		t.Errorf("notify calls = %d, want 0 (zero fleet must not divide by zero)", notif.calls())
	}
}

func TestWorker_MassOffline_StoreError_PropagatesAsCheckError(t *testing.T) {
	sims := &mockSimAggregator{activeErr: errors.New("db down")}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	err := w.checkMassOffline(context.Background(), time.Now().Add(-15*time.Minute), time.Now())
	if err == nil {
		t.Fatal("expected error from checkMassOffline when store fails")
	}
	if notif.calls() != 0 {
		t.Errorf("notify calls = %d, want 0 on store error", notif.calls())
	}
}

// ---------------------------------------------------------------------------
// traffic_spike tests
// ---------------------------------------------------------------------------

func TestWorker_TrafficSpike_ThresholdMet_Emits(t *testing.T) {
	// Current = 50 MB; baseline 6 windows each 10 MB → avg 10 MB; ratio = 5 → high.
	const mb = int64(1) << 20
	sims := &mockSimAggregator{}
	cdrs := &mockCDRAggregator{
		returnQueue: []int64{50 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb},
	}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkTrafficSpike(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkTrafficSpike returned error: %v", err)
	}
	if notif.calls() != 1 {
		t.Fatalf("notify calls = %d, want 1", notif.calls())
	}
	req := notif.last()
	if req.Severity != severity.High {
		t.Errorf("severity = %s, want high (5x ratio should band as high)", req.Severity)
	}
	if string(req.EventType) != "fleet.traffic_spike" {
		t.Errorf("event_type = %s, want fleet.traffic_spike", req.EventType)
	}
}

func TestWorker_TrafficSpike_BaselineBelowFloor_NoEmit(t *testing.T) {
	// Tiny baseline (well under 1 MB floor) — should NOT fire even with a
	// large ratio.
	sims := &mockSimAggregator{}
	cdrs := &mockCDRAggregator{
		returnQueue: []int64{500_000, 100, 100, 100, 100, 100, 100},
	}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkTrafficSpike(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkTrafficSpike returned error: %v", err)
	}
	if notif.calls() != 0 {
		t.Errorf("notify calls = %d, want 0 (baseline below 1MB floor must suppress)", notif.calls())
	}
}

func TestWorker_TrafficSpike_NoCurrentTraffic_NoEmit(t *testing.T) {
	sims := &mockSimAggregator{}
	cdrs := &mockCDRAggregator{returnQueue: []int64{0}}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkTrafficSpike(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkTrafficSpike returned error: %v", err)
	}
	if notif.calls() != 0 {
		t.Errorf("notify calls = %d, want 0 (zero current bytes must skip)", notif.calls())
	}
}

// ---------------------------------------------------------------------------
// quota_breach_count: documented NO-OP
// ---------------------------------------------------------------------------

func TestWorker_QuotaBreachCount_NoOp_NeverEmits(t *testing.T) {
	// FIX-237 NO-OP: quota_state breach signal not yet wired. Worker must
	// stay operational (no error) and emit nothing regardless of inputs.
	sims := &mockSimAggregator{}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{}
	notif := &recordingNotifier{}

	th := defaultThresholds()
	th.QuotaBreachCount = 1
	th.QuotaBreachFloor = 1

	w := newTestWorker(sims, cdrs, viols, notif, nil, th)

	if err := w.checkQuotaBreachCount(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkQuotaBreachCount returned error: %v", err)
	}
	if notif.calls() != 0 {
		t.Errorf("notify calls = %d, want 0 (NO-OP must never emit)", notif.calls())
	}
}

// ---------------------------------------------------------------------------
// violation_surge tests
// ---------------------------------------------------------------------------

func TestWorker_ViolationSurge_AboveBaseline_Emits(t *testing.T) {
	// Current = 30 violations; baseline 6 windows each 10 → avg 10; ratio = 3 → medium.
	sims := &mockSimAggregator{}
	cdrs := &mockCDRAggregator{}
	viols := &mockViolationAggregator{
		returnQueue: []int64{30, 10, 10, 10, 10, 10, 10},
	}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.checkViolationSurge(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkViolationSurge returned error: %v", err)
	}
	if notif.calls() != 1 {
		t.Fatalf("notify calls = %d, want 1", notif.calls())
	}
	req := notif.last()
	if req.Severity != severity.Medium {
		t.Errorf("severity = %s, want medium (3x ratio bands as medium)", req.Severity)
	}
	if string(req.EventType) != "fleet.violation_surge" {
		t.Errorf("event_type = %s, want fleet.violation_surge", req.EventType)
	}
}

func TestWorker_ViolationSurge_BelowFloor_NoEmit(t *testing.T) {
	// Below the absolute floor (10) → no emit even if the ratio is huge.
	viols := &mockViolationAggregator{
		returnQueue: []int64{5, 0, 0, 0, 0, 0, 0},
	}
	notif := &recordingNotifier{}

	w := newTestWorker(&mockSimAggregator{}, &mockCDRAggregator{}, viols, notif, nil, defaultThresholds())

	if err := w.checkViolationSurge(context.Background(), time.Now().Add(-15*time.Minute), time.Now()); err != nil {
		t.Fatalf("checkViolationSurge returned error: %v", err)
	}
	if notif.calls() != 0 {
		t.Errorf("notify calls = %d, want 0 (below absolute floor)", notif.calls())
	}
}

// ---------------------------------------------------------------------------
// Process integration test — end-to-end tick with mocks.
// ---------------------------------------------------------------------------

func TestWorker_Process_RunsAllChecksAndPublishes(t *testing.T) {
	// Construct a state where every aggregate fires:
	//  - mass_offline: 80 of 200 = 40% → critical
	//  - traffic_spike: 50 MB vs 10 MB baseline → 5x → high
	//  - violation_surge: 30 vs 10 baseline → 3x → medium
	//  - quota_breach_count: NO-OP (never emits)
	const mb = int64(1) << 20
	sims := &mockSimAggregator{activeReturn: 200, transitionReturn: 80}
	cdrs := &mockCDRAggregator{
		returnQueue: []int64{50 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb},
	}
	viols := &mockViolationAggregator{
		returnQueue: []int64{30, 10, 10, 10, 10, 10, 10},
	}
	notif := &recordingNotifier{}
	pub := &recordingPublisher{}

	w := newTestWorker(sims, cdrs, viols, notif, pub, defaultThresholds())

	if err := w.Process(context.Background(), nil); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if got := notif.calls(); got != 3 {
		t.Errorf("notify calls = %d, want 3 (mass_offline + traffic_spike + violation_surge; quota is NO-OP)", got)
	}
	pub.mu.Lock()
	pubCount := len(pub.subjects)
	pub.mu.Unlock()
	if pubCount != 3 {
		t.Errorf("publish calls = %d, want 3 (one per emitted fleet event)", pubCount)
	}
}

func TestWorker_Process_AggregateErrors_DoNotAbortOtherChecks(t *testing.T) {
	// mass_offline check fails (store error) — traffic_spike and violation_
	// surge must still run and may still emit.
	const mb = int64(1) << 20
	sims := &mockSimAggregator{activeErr: errors.New("db down")}
	cdrs := &mockCDRAggregator{
		returnQueue: []int64{50 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb},
	}
	viols := &mockViolationAggregator{
		returnQueue: []int64{30, 10, 10, 10, 10, 10, 10},
	}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())

	if err := w.Process(context.Background(), nil); err != nil {
		t.Fatalf("Process returned error (must swallow per-check errors): %v", err)
	}
	if got := notif.calls(); got != 2 {
		t.Errorf("notify calls = %d, want 2 (mass_offline failed, others still emit)", got)
	}
}

// ---------------------------------------------------------------------------
// Wire-format / contract assertions on the emitted NotifyRequest.
// ---------------------------------------------------------------------------

func TestWorker_EmittedRequests_AlwaysCarryDigestSource(t *testing.T) {
	// Without Source="digest", notification.Service.Notify drops Tier 2
	// events at service.go:401-411. This is an architectural invariant —
	// every fleet emit must satisfy it.
	const mb = int64(1) << 20
	sims := &mockSimAggregator{activeReturn: 200, transitionReturn: 80}
	cdrs := &mockCDRAggregator{
		returnQueue: []int64{50 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb, 10 * mb},
	}
	viols := &mockViolationAggregator{
		returnQueue: []int64{30, 10, 10, 10, 10, 10, 10},
	}
	notif := &recordingNotifier{}

	w := newTestWorker(sims, cdrs, viols, notif, nil, defaultThresholds())
	if err := w.Process(context.Background(), nil); err != nil {
		t.Fatalf("Process error: %v", err)
	}
	notif.mu.Lock()
	defer notif.mu.Unlock()
	if len(notif.requests) == 0 {
		t.Fatal("expected at least one emit")
	}
	for i, req := range notif.requests {
		if req.Source != digestSource {
			t.Errorf("request[%d].Source = %q, want %q", i, req.Source, digestSource)
		}
		if req.TenantID.String() != "00000000-0000-0000-0000-000000000001" {
			t.Errorf("request[%d].TenantID = %s, want bus.SystemTenantID", i, req.TenantID)
		}
	}
}

// ---------------------------------------------------------------------------
// Severity helper coverage — bands are publicly observable behaviour.
// ---------------------------------------------------------------------------

func TestSeverityForPercentage_Banding(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{4.9, severity.Low},
		{5.0, severity.Medium},
		{14.9, severity.Medium},
		{15.0, severity.High},
		{29.9, severity.High},
		{30.0, severity.Critical},
		{99.0, severity.Critical},
	}
	for _, c := range cases {
		if got := severityForPercentage(c.pct); got != c.want {
			t.Errorf("severityForPercentage(%.2f) = %s, want %s", c.pct, got, c.want)
		}
	}
}

func TestSeverityForRatio_Banding(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{2.99, severity.Low},
		{3.0, severity.Medium},
		{4.99, severity.Medium},
		{5.0, severity.High},
		{9.99, severity.High},
		{10.0, severity.Critical},
		{50.0, severity.Critical},
	}
	for _, c := range cases {
		if got := severityForRatio(c.ratio); got != c.want {
			t.Errorf("severityForRatio(%.2f) = %s, want %s", c.ratio, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// AC-2 (load assertion) NOTE: the "100K Tier 1 events → 0 notification rows"
// guarantee is a property of notification.Service.Notify's tier guard, not
// the digest worker. The guard already exists at service.go:391-412 and is
// covered by TestService_Notify_TierInternal_NeverPersists at
// internal/notification/service_test.go:~1900. The worker's contribution to
// AC-2 is the inverse: it MUST set Source="digest" on every Tier 2 emit so
// the guard admits them — verified above by
// TestWorker_EmittedRequests_AlwaysCarryDigestSource.
// ---------------------------------------------------------------------------
