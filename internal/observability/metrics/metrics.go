package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// recent5xxWindowSeconds is the sliding-window length covered by the
// errorRingBuffer (5 minutes). Changing this value changes the semantics
// of the /api/v1/status/details.recent_error_5m field and its tests.
const recent5xxWindowSeconds = 300

type errorRingBuffer struct {
	mu     sync.Mutex
	slots  [recent5xxWindowSeconds]int64
	stamps [recent5xxWindowSeconds]int64 // unix second each slot represents
	now    func() time.Time
}

func newErrorRingBuffer() *errorRingBuffer {
	return &errorRingBuffer{now: time.Now}
}

func (r *errorRingBuffer) record() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().Unix()
	i := int(now % recent5xxWindowSeconds)
	if r.stamps[i] != now {
		r.slots[i] = 0
		r.stamps[i] = now
	}
	r.slots[i]++
}

func (r *errorRingBuffer) sum() int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().Unix()
	var total int64
	for i := 0; i < recent5xxWindowSeconds; i++ {
		if r.stamps[i] == 0 {
			continue
		}
		if now-r.stamps[i] < recent5xxWindowSeconds {
			total += r.slots[i]
		}
	}
	return total
}

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
	DiskUsagePercent      *prometheus.GaugeVec
	JWTVerifyTotal        *prometheus.CounterVec

	NATSConsumerLag       *prometheus.GaugeVec
	NATSConsumerLagAlerts *prometheus.CounterVec

	BackupLastSuccessTimestamp *prometheus.GaugeVec
	BackupRunsTotal            *prometheus.CounterVec

	IPGraceReleasedTotal prometheus.Counter

	FramedIPPoolMismatchTotal *prometheus.CounterVec

	NASIPMissingTotal prometheus.Counter

	IMSIInvalidTotal *prometheus.CounterVec

	DataIntegrityViolationsTotal *prometheus.CounterVec

	KVKKPurgeRowsTotal *prometheus.CounterVec

	WebhookRetriesTotal       *prometheus.CounterVec
	ScheduledReportRunsTotal  *prometheus.CounterVec

	BuildInfo *prometheus.GaugeVec

	recent5xx *errorRingBuffer
}

func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	r := &Registry{Reg: reg, recent5xx: newErrorRingBuffer()}

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

	// STORY-090 AC-10: per-(operator, protocol) fan-out. Previous
	// `argus_operator_health{operator_id}` single-label series was
	// insufficient when one operator probes multiple protocols — the
	// last goroutine to tick would clobber the others' state.
	// Renamed + label-expanded to `argus_operator_adapter_health_status
	// {operator_id, protocol}` (breaking change, see decisions.md).
	r.OperatorHealth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_operator_adapter_health_status",
		Help: "Per-protocol health status of operator adapters: 0=down, 1=degraded, 2=healthy.",
	}, []string{"operator_id", "protocol"})
	reg.MustRegister(r.OperatorHealth)

	r.CircuitBreakerState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_circuit_breaker_state",
		Help: "Circuit breaker state per operator (0=closed/ok, 1=open/tripped).",
	}, []string{"operator_id", "state"})
	reg.MustRegister(r.CircuitBreakerState)

	r.DiskUsagePercent = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_disk_usage_percent",
		Help: "Disk usage percent per mount point.",
	}, []string{"mount"})
	reg.MustRegister(r.DiskUsagePercent)

	r.JWTVerifyTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_jwt_verify_total",
		Help: "Total number of JWT verification attempts.",
	}, []string{"key_slot"})
	reg.MustRegister(r.JWTVerifyTotal)

	r.NATSConsumerLag = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_nats_consumer_lag",
		Help: "Number of pending messages (lag) per NATS JetStream consumer.",
	}, []string{"stream", "consumer"})
	reg.MustRegister(r.NATSConsumerLag)

	r.NATSConsumerLagAlerts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_nats_consumer_lag_alerts_total",
		Help: "Total number of NATS consumer lag alerts emitted.",
	}, []string{"consumer"})
	reg.MustRegister(r.NATSConsumerLagAlerts)

	r.BackupLastSuccessTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_backup_last_success_timestamp_seconds",
		Help: "Unix timestamp of the last successful backup run by kind.",
	}, []string{"kind"})
	reg.MustRegister(r.BackupLastSuccessTimestamp)

	r.BackupRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_backup_runs_total",
		Help: "Total number of backup runs by kind and state.",
	}, []string{"kind", "state"})
	reg.MustRegister(r.BackupRunsTotal)

	r.IPGraceReleasedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "argus_ip_grace_released_total",
		Help: "Total number of IP addresses released from grace period by the ip_grace_release cron job.",
	})
	reg.MustRegister(r.IPGraceReleasedTotal)

	r.FramedIPPoolMismatchTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_framed_ip_pool_mismatch_total",
		Help: "Total number of session framed_ip pool validation mismatches detected (AC-3). Does not block session creation.",
	}, []string{"reason"})
	reg.MustRegister(r.FramedIPPoolMismatchTotal)

	r.NASIPMissingTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "argus_radius_nas_ip_missing_total",
		Help: "RADIUS Acct-Start packets received without NAS-IP-Address AVP. FIX-207 AC-7: closure signal for simulator coverage (FIX-226).",
	})
	reg.MustRegister(r.NASIPMissingTotal)

	r.IMSIInvalidTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_imsi_invalid_total",
		Help: "Malformed IMSIs rejected by IMSI_STRICT_VALIDATION. Labels: source.",
	}, []string{"source"})
	reg.MustRegister(r.IMSIInvalidTotal)

	r.DataIntegrityViolationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_data_integrity_violations_total",
		Help: "Daily data-integrity scan violation counts by kind.",
	}, []string{"kind"})
	reg.MustRegister(r.DataIntegrityViolationsTotal)

	r.KVKKPurgeRowsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_kvkk_purge_rows_total",
		Help: "Total number of rows pseudonymized/redacted by the KVKK purge job.",
	}, []string{"table", "dry_run"})
	reg.MustRegister(r.KVKKPurgeRowsTotal)

	r.WebhookRetriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_webhook_retries_total",
		Help: "Total webhook delivery retry attempts grouped by result (succeeded, retrying, dead_letter).",
	}, []string{"result"})
	reg.MustRegister(r.WebhookRetriesTotal)

	r.ScheduledReportRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "argus_scheduled_report_runs_total",
		Help: "Total scheduled report executions grouped by report type and result.",
	}, []string{"type", "result"})
	reg.MustRegister(r.ScheduledReportRunsTotal)

	r.BuildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "argus_build_info",
		Help: "Argus build information (always 1).",
	}, []string{"version", "git_sha", "build_time"})
	reg.MustRegister(r.BuildInfo)

	return r
}

func (r *Registry) IncJWTVerify(slot string) {
	if r == nil || r.JWTVerifyTotal == nil {
		return
	}
	r.JWTVerifyTotal.WithLabelValues(slot).Inc()
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.Reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		Registry:          r.Reg,
	})
}

// SetOperatorHealth updates the argus_operator_adapter_health_status
// gauge for an (operator, protocol) pair using the canonical numeric
// mapping:
//
//	down     -> 0
//	degraded -> 1
//	healthy  -> 2
//
// Unknown status strings are treated as down (0) to fail safely.
// Safe to call on a nil Registry (no-op).
//
// STORY-090 Wave 3 Gate (F-A1): signature gained the `protocol` label
// to support per-protocol fan-out (AC-10). Previously a single-label
// gauge — see decisions.md entry "AC-10 gauge label schema breaking
// change" for migration notes.
func (r *Registry) SetOperatorHealth(operatorID, protocol, status string) {
	if r == nil || r.OperatorHealth == nil {
		return
	}
	r.OperatorHealth.WithLabelValues(operatorID, protocol).Set(operatorHealthValue(status))
}

// DeleteOperatorHealth retires a single (operator, protocol) label
// series. Used when a protocol is disabled via PATCH or when the
// HealthChecker tears down its loop for that key; without this the
// gauge continues reporting stale values after the probe stops.
// Safe to call on a nil Registry (no-op).
func (r *Registry) DeleteOperatorHealth(operatorID, protocol string) {
	if r == nil || r.OperatorHealth == nil {
		return
	}
	r.OperatorHealth.DeleteLabelValues(operatorID, protocol)
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

// SetNATSConsumerLag updates the lag gauge for a specific stream/consumer pair.
// Safe to call on a nil Registry (no-op).
func (r *Registry) SetNATSConsumerLag(stream, consumer string, lag float64) {
	if r == nil || r.NATSConsumerLag == nil {
		return
	}
	r.NATSConsumerLag.WithLabelValues(stream, consumer).Set(lag)
}

// IncNATSConsumerLagAlert increments the alert counter for the given consumer.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncNATSConsumerLagAlert(consumer string) {
	if r == nil || r.NATSConsumerLagAlerts == nil {
		return
	}
	r.NATSConsumerLagAlerts.WithLabelValues(consumer).Inc()
}

// SetBackupLastSuccess records the unix timestamp of the last successful backup for the given kind.
// Safe to call on a nil Registry (no-op).
func (r *Registry) SetBackupLastSuccess(kind string, ts time.Time) {
	if r == nil || r.BackupLastSuccessTimestamp == nil {
		return
	}
	r.BackupLastSuccessTimestamp.WithLabelValues(kind).Set(float64(ts.Unix()))
}

// IncBackupRun increments the backup runs counter for the given kind and state.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncBackupRun(kind, state string) {
	if r == nil || r.BackupRunsTotal == nil {
		return
	}
	r.BackupRunsTotal.WithLabelValues(kind, state).Inc()
}

// IncIPGraceReleased increments the ip grace released counter by n.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncIPGraceReleased(n int) {
	if r == nil || r.IPGraceReleasedTotal == nil {
		return
	}
	for i := 0; i < n; i++ {
		r.IPGraceReleasedTotal.Inc()
	}
}

// IncFramedIPPoolMismatch increments the framed_ip pool mismatch counter for the given reason.
// Label values: unparseable_framed_ip, mismatch_assigned_address, outside_apn_pools.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncFramedIPPoolMismatch(reason string) {
	if r == nil || r.FramedIPPoolMismatchTotal == nil {
		return
	}
	r.FramedIPPoolMismatchTotal.WithLabelValues(reason).Inc()
}

// IncNASIPMissing increments the counter for Acct-Start packets that arrived
// without a NAS-IP-Address AVP. FIX-207 AC-7: the missing AVP is a simulator
// gap (FIX-226); this counter surfaces it as an ops-visible signal.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncNASIPMissing() {
	if r == nil || r.NASIPMissingTotal == nil {
		return
	}
	r.NASIPMissingTotal.Inc()
}

// IncIMSIInvalid increments the malformed-IMSI counter for the given source label.
// Label values: radius_auth, radius_acct, api_sim, cdr.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncIMSIInvalid(source string) {
	if r == nil || r.IMSIInvalidTotal == nil {
		return
	}
	r.IMSIInvalidTotal.WithLabelValues(source).Inc()
}

// IncKVKKPurgeRows increments the KVKK purge rows counter for the given table and dry_run flag.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncKVKKPurgeRows(table string, dryRun bool, n int) {
	if r == nil || r.KVKKPurgeRowsTotal == nil {
		return
	}
	dryRunLabel := "false"
	if dryRun {
		dryRunLabel = "true"
	}
	r.KVKKPurgeRowsTotal.WithLabelValues(table, dryRunLabel).Add(float64(n))
}

// IncWebhookRetry increments the webhook retry counter for the given result label.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncWebhookRetry(result string) {
	if r == nil || r.WebhookRetriesTotal == nil {
		return
	}
	r.WebhookRetriesTotal.WithLabelValues(result).Inc()
}

// IncScheduledReportRun increments the scheduled report counter for the given report type and result.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncScheduledReportRun(reportType, result string) {
	if r == nil || r.ScheduledReportRunsTotal == nil {
		return
	}
	r.ScheduledReportRunsTotal.WithLabelValues(reportType, result).Inc()
}

// RecordHTTPStatus records a 5xx HTTP response for the recent_error_5m
// sliding window. Non-5xx statuses and nil receiver are no-ops.
func (r *Registry) RecordHTTPStatus(status int) {
	if r == nil || r.recent5xx == nil {
		return
	}
	if status >= 500 && status < 600 {
		r.recent5xx.record()
	}
}

// Recent5xxCount returns the number of 5xx responses recorded in the last
// 300 seconds (5 minutes) — matches the recent_error_5m field exposed via
// /api/v1/status/details. Safe to call on a nil Registry.
func (r *Registry) Recent5xxCount() int64 {
	if r == nil || r.recent5xx == nil {
		return 0
	}
	return r.recent5xx.sum()
}

// IncDataIntegrity adds by to the argus_data_integrity_violations_total counter for the given kind.
// Label values: neg_duration_session, neg_duration_cdr, framed_ip_outside_pool, imsi_malformed.
// Safe to call on a nil Registry (no-op).
func (r *Registry) IncDataIntegrity(kind string, by float64) {
	if r == nil || r.DataIntegrityViolationsTotal == nil {
		return
	}
	r.DataIntegrityViolationsTotal.WithLabelValues(kind).Add(by)
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
