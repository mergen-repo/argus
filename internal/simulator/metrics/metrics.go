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
