// Package metrics exposes Prometheus counters/histograms/gauges for the
// simulator. All metrics carry an `operator` label so dashboards can
// compare Turkcell vs Vodafone vs Türk Telekom output.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RadiusRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_radius_requests_total",
			Help: "RADIUS requests sent by the simulator.",
		},
		[]string{"operator", "type"}, // type: auth|acct_start|acct_interim|acct_stop
	)

	RadiusResponsesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_radius_responses_total",
			Help: "RADIUS responses received by the simulator, bucketed by result.",
		},
		[]string{"operator", "type", "result"}, // result: accept|reject|timeout|error
	)

	RadiusLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "simulator_radius_latency_seconds",
			Help:    "RADIUS request/response round-trip latency.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms .. ~4s
		},
		[]string{"operator", "type"},
	)

	ActiveSessions = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "simulator_active_sessions",
			Help: "Currently-active simulated sessions.",
		},
		[]string{"operator"},
	)

	ScenarioStartsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_scenario_starts_total",
			Help: "Scenario invocations bucketed by operator and scenario name.",
		},
		[]string{"operator", "scenario"},
	)

	DiameterRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_diameter_requests_total",
			Help: "Diameter requests sent by the simulator.",
		},
		[]string{"operator", "app", "type"}, // app: gx|gy; type: ccr_i|ccr_u|ccr_t|cer|dwr
	)

	DiameterResponsesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_diameter_responses_total",
			Help: "Diameter responses received by the simulator, bucketed by result.",
		},
		[]string{"operator", "app", "type", "result"}, // result: success|error_<code>|timeout|peer_down
	)

	DiameterLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "simulator_diameter_latency_seconds",
			Help:    "Diameter request/response round-trip latency.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms .. ~4s
		},
		[]string{"operator", "app", "type"},
	)

	DiameterPeerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "simulator_diameter_peer_state",
			Help: "Diameter peer state per operator (0=closed,1=connecting,2=wait_cea,3=open,4=closing).",
		},
		[]string{"operator"},
	)

	DiameterSessionAbortedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_diameter_session_aborted_total",
			Help: "Diameter sessions aborted before completion, bucketed by reason.",
		},
		[]string{"operator", "reason"}, // reason: ccr_i_failed|peer_down|timeout|reject
	)

	SBARequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_sba_requests_total",
			Help: "5G SBA HTTP requests sent by the simulator.",
		},
		[]string{"operator", "service", "endpoint"}, // service: ausf|udm; endpoint: authenticate|confirm|register|security-info|auth-events
	)

	SBAResponsesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_sba_responses_total",
			Help: "5G SBA HTTP responses received by the simulator, bucketed by result.",
		},
		[]string{"operator", "service", "endpoint", "result"}, // result: success|error_4xx|error_5xx|timeout|transport
	)

	SBALatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "simulator_sba_latency_seconds",
			Help:    "5G SBA HTTP request/response round-trip latency.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms .. ~4s
		},
		[]string{"operator", "service", "endpoint"},
	)

	SBASessionAbortedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_sba_session_aborted_total",
			Help: "5G SBA sessions aborted before completion, bucketed by reason.",
		},
		[]string{"operator", "reason"}, // reason: auth_failed|confirm_failed|register_failed|transport|timeout
	)

	SBAServiceErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_sba_service_errors_total",
			Help: "5G SBA service errors broken down by ProblemDetails.Cause (e.g. MANDATORY_IE_INCORRECT, AUTH_REJECTED, SNSSAI_NOT_ALLOWED). Falls back to 'unknown' when the body is not application/problem+json.",
		},
		[]string{"operator", "service", "cause"},
	)

	// STORY-085 — reactive behavior (approach B)

	SimulatorReactiveTerminationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_reactive_terminations_total",
			Help: "Sessions terminated by the reactive subsystem, labelled by cause. Engine is the single writer (PAT-001).",
		},
		[]string{"operator", "cause"},
		// cause ∈ {session_timeout, disconnect, coa_deadline, reject_suspend, scenario_end, shutdown}
	)

	SimulatorReactiveRejectBackoffsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_reactive_reject_backoffs_total",
			Help: "Access-Reject events that triggered backoff or suspension, labelled by outcome.",
		},
		[]string{"operator", "outcome"},
		// outcome ∈ {backoff_set, suspended}
	)

	SimulatorReactiveIncomingTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "simulator_reactive_incoming_total",
			Help: "UDP packets received by the CoA/DM listener, labelled by kind and result.",
		},
		[]string{"operator", "kind", "result"},
		// kind ∈ {dm, coa, unknown}; result ∈ {ack, unknown_session, bad_secret, malformed, unsupported}.
		// Note: "nak" is not a distinct result — unknown_session IS the NAK branch
		// (code 41 for DM / 45 for CoA with Error-Cause 503 Session-Context-Not-Found).
	)
)

// MustRegister wires every simulator metric into the provided registerer.
// Panics on duplicate registration — call once at startup.
func MustRegister(reg prometheus.Registerer) {
	reg.MustRegister(
		RadiusRequestsTotal,
		RadiusResponsesTotal,
		RadiusLatencySeconds,
		ActiveSessions,
		ScenarioStartsTotal,
		DiameterRequestsTotal,
		DiameterResponsesTotal,
		DiameterLatencySeconds,
		DiameterPeerState,
		DiameterSessionAbortedTotal,
		SBARequestsTotal,
		SBAResponsesTotal,
		SBALatencySeconds,
		SBASessionAbortedTotal,
		SBAServiceErrorsTotal,
		SimulatorReactiveTerminationsTotal,
		SimulatorReactiveRejectBackoffsTotal,
		SimulatorReactiveIncomingTotal,
	)
}

// Handler returns an http.Handler exposing /metrics and a /-/health probe.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/-/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
