package enforcer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// newTestEnforcer builds a minimally-wired Enforcer for unit tests.
// Store / bus / cache all nil — the FIX-210 rate limiter does not
// depend on them and Evaluate/RecordViolations are exercised via the
// rate-limit gate directly.
func newTestEnforcer(t *testing.T) *Enforcer {
	t.Helper()
	return &Enforcer{
		logger:        zerolog.Nop(),
		rlLastEmitted: make(map[enforcerAlertKey]time.Time),
		rlMinInterval: defaultEnforcerMinInterval,
		rlNow:         time.Now,
	}
}

// TestEnforcer_WithinMinInterval_NoPublish — two emits for the same
// (policy, sim) within the 60s window: the first call wins, the second
// is suppressed.
func TestEnforcer_WithinMinInterval_NoPublish(t *testing.T) {
	e := newTestEnforcer(t)

	base := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	e.rlNow = func() time.Time { return base }

	policyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	simID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	if !e.shouldPublishViolationAlert(policyID, simID) {
		t.Fatal("first attempt must be allowed")
	}

	// Advance by 30s — still inside the 60s window.
	e.rlNow = func() time.Time { return base.Add(30 * time.Second) }
	if e.shouldPublishViolationAlert(policyID, simID) {
		t.Fatal("second attempt at +30s must be suppressed")
	}

	// Advance by 59s — still inside (strict less-than).
	e.rlNow = func() time.Time { return base.Add(59 * time.Second) }
	if e.shouldPublishViolationAlert(policyID, simID) {
		t.Fatal("third attempt at +59s must be suppressed")
	}
}

// TestEnforcer_AfterMinInterval_Publishes — once 60s have elapsed a
// new alert publish is allowed.
func TestEnforcer_AfterMinInterval_Publishes(t *testing.T) {
	e := newTestEnforcer(t)

	base := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	e.rlNow = func() time.Time { return base }

	policyID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	simID := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	if !e.shouldPublishViolationAlert(policyID, simID) {
		t.Fatal("first attempt must be allowed")
	}

	// Advance by 61s — past the window → allowed again.
	e.rlNow = func() time.Time { return base.Add(61 * time.Second) }
	if !e.shouldPublishViolationAlert(policyID, simID) {
		t.Fatal("attempt at +61s must be allowed")
	}

	// And again — resetting the window.
	e.rlNow = func() time.Time { return base.Add(65 * time.Second) }
	if e.shouldPublishViolationAlert(policyID, simID) {
		t.Fatal("attempt at +65s (within 4s of previous) must be suppressed")
	}
}

// TestEnforcer_IndependentKeys — different (policy, sim) tuples have
// independent rate-limit windows. An active window on key A must not
// suppress key B.
func TestEnforcer_IndependentKeys(t *testing.T) {
	e := newTestEnforcer(t)

	base := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	e.rlNow = func() time.Time { return base }

	policyA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	policyB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	simA := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	simB := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

	if !e.shouldPublishViolationAlert(policyA, simA) {
		t.Fatal("first (A,A) must be allowed")
	}
	if !e.shouldPublishViolationAlert(policyB, simA) {
		t.Fatal("first (B,A) must be allowed (different policy)")
	}
	if !e.shouldPublishViolationAlert(policyA, simB) {
		t.Fatal("first (A,B) must be allowed (different sim)")
	}

	// Repeat (A,A) at +30s — suppressed independently of B/B above.
	e.rlNow = func() time.Time { return base.Add(30 * time.Second) }
	if e.shouldPublishViolationAlert(policyA, simA) {
		t.Fatal("second (A,A) at +30s must be suppressed")
	}
}

// TestEnforcer_SetMetricsRegistry_NilSafe — SetMetricsRegistry with nil
// registry must not panic nor change behaviour.
func TestEnforcer_SetMetricsRegistry_NilSafe(t *testing.T) {
	e := newTestEnforcer(t)
	e.SetMetricsRegistry(nil)
	// Exercise a suppressed increment path.
	e.metricsReg.IncAlertsRateLimitedPublishes("enforcer")
}

// TestEnforcer_MetricsIncrement — when the registry is wired, the
// rate-limit-suppressed path increments the counter under the
// "enforcer" label.
func TestEnforcer_MetricsIncrement(t *testing.T) {
	e := newTestEnforcer(t)
	reg := obsmetrics.NewRegistry()
	e.SetMetricsRegistry(reg)

	e.metricsReg.IncAlertsRateLimitedPublishes("enforcer")
	e.metricsReg.IncAlertsRateLimitedPublishes("enforcer")

	// Scrape and assert.
	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	want := `argus_alerts_rate_limited_publishes_total{publisher="enforcer"} 2`
	if !strings.Contains(string(body), want) {
		t.Errorf("missing counter line %q in output", want)
	}
}
