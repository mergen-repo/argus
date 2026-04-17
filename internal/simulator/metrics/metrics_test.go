package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// TestMustRegister_AllVectorsPresent verifies that every simulator metric is
// registered with its plan-contracted name + label set. Vectors are exercised
// by the live SBA client path, but this unit test asserts the contract
// independently so dashboard/alert authors can rely on the label schema
// without reading source.
//
// Plan AC-4: `/metrics` endpoint exposes all five `simulator_sba_*` vectors.
func TestMustRegister_AllVectorsPresent(t *testing.T) {
	reg := prometheus.NewRegistry()
	MustRegister(reg)

	// Touch each counter/histogram with a dummy label set so that the vector
	// emits at least one series (Prometheus doesn't expose empty vectors in the
	// scrape output).
	RadiusRequestsTotal.WithLabelValues("op", "auth").Inc()
	RadiusResponsesTotal.WithLabelValues("op", "auth", "accept").Inc()
	RadiusLatencySeconds.WithLabelValues("op", "auth").Observe(0.1)
	ActiveSessions.WithLabelValues("op").Inc()
	ScenarioStartsTotal.WithLabelValues("op", "scenario").Inc()
	DiameterRequestsTotal.WithLabelValues("op", "gy", "ccr_i").Inc()
	DiameterResponsesTotal.WithLabelValues("op", "gy", "ccr_i", "success").Inc()
	DiameterLatencySeconds.WithLabelValues("op", "gy", "ccr_i").Observe(0.1)
	DiameterPeerState.WithLabelValues("op").Set(3)
	DiameterSessionAbortedTotal.WithLabelValues("op", "peer_down").Inc()
	SBARequestsTotal.WithLabelValues("op", "ausf", "authenticate").Inc()
	SBAResponsesTotal.WithLabelValues("op", "ausf", "authenticate", "success").Inc()
	SBALatencySeconds.WithLabelValues("op", "ausf", "authenticate").Observe(0.1)
	SBASessionAbortedTotal.WithLabelValues("op", "auth_failed").Inc()
	SBAServiceErrorsTotal.WithLabelValues("op", "ausf", "MANDATORY_IE_INCORRECT").Inc()

	// Scrape via a handler bound to the test registry so we don't depend on
	// DefaultRegistry (which may carry state across tests running in the same
	// binary).
	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)

	wantVectors := []string{
		"simulator_radius_requests_total",
		"simulator_radius_responses_total",
		"simulator_radius_latency_seconds",
		"simulator_active_sessions",
		"simulator_scenario_starts_total",
		"simulator_diameter_requests_total",
		"simulator_diameter_responses_total",
		"simulator_diameter_latency_seconds",
		"simulator_diameter_peer_state",
		"simulator_diameter_session_aborted_total",
		"simulator_sba_requests_total",
		"simulator_sba_responses_total",
		"simulator_sba_latency_seconds",
		"simulator_sba_session_aborted_total",
		"simulator_sba_service_errors_total",
	}
	for _, v := range wantVectors {
		if !strings.Contains(out, v) {
			t.Errorf("metric %q missing from /metrics output", v)
		}
	}

	// Plan-contracted label names (F-A4 regression guard).
	// `endpoint` and `cause` must appear — not `op` or `http_status`.
	wantLabels := []string{
		`endpoint="authenticate"`,
		`service="ausf"`,
		`cause="MANDATORY_IE_INCORRECT"`,
	}
	for _, l := range wantLabels {
		if !strings.Contains(out, l) {
			t.Errorf("expected plan-contract label %q in /metrics output; body excerpt: %q", l, snippet(out, l))
		}
	}

	// Negative assertions: plan-retired label names MUST NOT appear on any SBA
	// metric. Catches accidental reintroduction of the old `op` / `http_status`
	// schema (F-A4 / F-A2 regression guard).
	//
	// Scope is restricted to lines beginning with `simulator_sba_` so that
	// RADIUS/Diameter metrics (which have their own `type`/`app` label schemas
	// and are unaffected by this story's rename) do not trigger false positives.
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "simulator_sba_") {
			continue
		}
		if strings.Contains(line, ` op="`) {
			t.Errorf("retired label `op` appears on SBA metric; expected `endpoint`: %q", line)
		}
		if strings.Contains(line, `http_status="`) {
			t.Errorf("retired label `http_status` appears on SBA metric; expected `cause`: %q", line)
		}
	}
}

// snippet returns a short window of the metric body around the expected
// label, for debug output only.
func snippet(body, needle string) string {
	idx := strings.Index(body, needle)
	if idx < 0 {
		return "(not found; body length=" + itoa(len(body)) + ")"
	}
	start := idx - 60
	if start < 0 {
		start = 0
	}
	end := idx + 80
	if end > len(body) {
		end = len(body)
	}
	return body[start:end]
}

// TestRegister_ReactiveVectors verifies that all three simulator_reactive_*
// metric names are registered (STORY-085 AC).
func TestRegister_ReactiveVectors(t *testing.T) {
	reg := prometheus.NewRegistry()
	MustRegister(reg)

	SimulatorReactiveTerminationsTotal.WithLabelValues("op", "session_timeout").Inc()
	SimulatorReactiveRejectBackoffsTotal.WithLabelValues("op", "backoff_set").Inc()
	SimulatorReactiveIncomingTotal.WithLabelValues("op", "coa", "ack").Inc()

	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)

	wantVectors := []string{
		"simulator_reactive_terminations_total",
		"simulator_reactive_reject_backoffs_total",
		"simulator_reactive_incoming_total",
	}
	for _, v := range wantVectors {
		if !strings.Contains(out, v) {
			t.Errorf("metric %q missing from /metrics output", v)
		}
	}
}

// TestReactive_LabelNames verifies that each reactive vector exposes exactly
// the planned label names (STORY-085 label contract).
func TestReactive_LabelNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	MustRegister(reg)

	SimulatorReactiveTerminationsTotal.WithLabelValues("turkcell", "disconnect").Inc()
	SimulatorReactiveRejectBackoffsTotal.WithLabelValues("vodafone", "suspended").Inc()
	SimulatorReactiveIncomingTotal.WithLabelValues("unknown", "dm", "bad_secret").Inc()

	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)

	wantLabels := []string{
		`cause="disconnect"`,
		`outcome="suspended"`,
		`kind="dm"`,
		`result="bad_secret"`,
	}
	for _, l := range wantLabels {
		if !strings.Contains(out, l) {
			t.Errorf("expected label %q in /metrics output; excerpt: %q", l, snippet(out, l))
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
