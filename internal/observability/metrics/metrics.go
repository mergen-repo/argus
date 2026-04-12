package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Registry struct {
	Reg *prometheus.Registry

	HTTPRequestsTotal     *prometheus.CounterVec
	HTTPRequestDuration   *prometheus.HistogramVec
	AAAAuthRequestsTotal  *prometheus.CounterVec
	AAAAuthLatency        *prometheus.HistogramVec
	ActiveSessions        *prometheus.GaugeVec
	DBQueryDuration       *prometheus.HistogramVec
	DBPoolConnections     *prometheus.GaugeVec
	NATSPublishedTotal    *prometheus.CounterVec
	NATSConsumedTotal     *prometheus.CounterVec
	NATSPendingMessages   *prometheus.GaugeVec
	RedisOpsTotal         *prometheus.CounterVec
	RedisCacheHitsTotal   *prometheus.CounterVec
	RedisCacheMissesTotal *prometheus.CounterVec
	JobRunsTotal          *prometheus.CounterVec
	JobDuration           *prometheus.HistogramVec
	OperatorHealth        *prometheus.GaugeVec
	CircuitBreakerState   *prometheus.GaugeVec
}

func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	r := &Registry{Reg: reg}

	r.HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "route", "status", "tenant_id"})
	reg.MustRegister(r.HTTPRequestsTotal)

	r.HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "argus_http_request_duration_seconds",
		Help:    "Duration of HTTP requests in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
	}, []string{"method", "route", "tenant_id"})
	reg.MustRegister(r.HTTPRequestDuration)

	r.AAAAuthRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_aaa_auth_requests_total",
		Help: "Total number of AAA authentication requests.",
	}, []string{"protocol", "operator_id", "result", "tenant_id"})
	reg.MustRegister(r.AAAAuthRequestsTotal)

	r.AAAAuthLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "argus_aaa_auth_latency_seconds",
		Help:    "Latency of AAA authentication requests in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0},
	}, []string{"protocol", "operator_id", "tenant_id"})
	reg.MustRegister(r.AAAAuthLatency)

	r.ActiveSessions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_active_sessions",
		Help: "Number of currently active sessions.",
	}, []string{"tenant_id", "operator_id"})
	reg.MustRegister(r.ActiveSessions)

	r.DBQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "argus_db_query_duration_seconds",
		Help:    "Duration of database queries in seconds.",
		Buckets: []float64{0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5},
	}, []string{"operation", "table"})
	reg.MustRegister(r.DBQueryDuration)

	r.DBPoolConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_db_pool_connections",
		Help: "Number of database pool connections by state.",
	}, []string{"state"})
	reg.MustRegister(r.DBPoolConnections)

	r.NATSPublishedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_nats_published_total",
		Help: "Total number of NATS messages published.",
	}, []string{"subject"})
	reg.MustRegister(r.NATSPublishedTotal)

	r.NATSConsumedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_nats_consumed_total",
		Help: "Total number of NATS messages consumed.",
	}, []string{"subject"})
	reg.MustRegister(r.NATSConsumedTotal)

	r.NATSPendingMessages = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_nats_pending_messages",
		Help: "Number of pending NATS messages.",
	}, []string{"subject"})
	reg.MustRegister(r.NATSPendingMessages)

	r.RedisOpsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_redis_ops_total",
		Help: "Total number of Redis operations.",
	}, []string{"op", "result"})
	reg.MustRegister(r.RedisOpsTotal)

	r.RedisCacheHitsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_redis_cache_hits_total",
		Help: "Total number of Redis cache hits.",
	}, []string{"cache"})
	reg.MustRegister(r.RedisCacheHitsTotal)

	r.RedisCacheMissesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_redis_cache_misses_total",
		Help: "Total number of Redis cache misses.",
	}, []string{"cache"})
	reg.MustRegister(r.RedisCacheMissesTotal)

	r.JobRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_job_runs_total",
		Help: "Total number of job runs.",
	}, []string{"job_type", "result"})
	reg.MustRegister(r.JobRunsTotal)

	r.JobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "argus_job_duration_seconds",
		Help:    "Duration of job runs in seconds.",
		Buckets: []float64{0.5, 1.0, 5.0, 15.0, 30.0, 60.0, 120.0, 300.0, 600.0, 1800.0},
	}, []string{"job_type"})
	reg.MustRegister(r.JobDuration)

	r.OperatorHealth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_operator_health",
		Help: "Health status of operators: 0=down, 1=degraded, 2=healthy.",
	}, []string{"operator_id"})
	reg.MustRegister(r.OperatorHealth)

	r.CircuitBreakerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_circuit_breaker_state",
		Help: "Circuit breaker state per operator (0=closed/ok, 1=open/tripped).",
	}, []string{"operator_id", "state"})
	reg.MustRegister(r.CircuitBreakerState)

	return r
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.Reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		Registry:          r.Reg,
	})
}

// SetOperatorHealth updates the argus_operator_health gauge for an
// operator using the canonical numeric mapping:
//
//	down     -> 0
//	degraded -> 1
//	healthy  -> 2
//
// Unknown status strings are treated as down (0) to fail safely.
// Safe to call on a nil Registry (no-op).
func (r *Registry) SetOperatorHealth(operatorID, status string) {
	if r == nil || r.OperatorHealth == nil {
		return
	}
	r.OperatorHealth.WithLabelValues(operatorID).Set(operatorHealthValue(status))
}

// SetCircuitBreakerState updates the argus_circuit_breaker_state gauge
// for an operator, setting the supplied state to 1 and the other two
// known states to 0 so exactly one label series is active per operator.
// Safe to call on a nil Registry (no-op).
func (r *Registry) SetCircuitBreakerState(operatorID, state string) {
	if r == nil || r.CircuitBreakerState == nil {
		return
	}
	states := []string{"closed", "open", "half_open"}
	for _, s := range states {
		value := 0.0
		if s == state {
			value = 1.0
		}
		r.CircuitBreakerState.WithLabelValues(operatorID, s).Set(value)
	}
}

func operatorHealthValue(status string) float64 {
	switch status {
	case "healthy":
		return 2
	case "degraded":
		return 1
	case "down":
		return 0
	default:
		return 0
	}
}
