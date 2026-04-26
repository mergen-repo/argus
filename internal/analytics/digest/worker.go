// Package digest implements the FIX-237 fleet-level digest worker. The worker
// scans Tier 1 metric sources on a fixed cadence (default every 15 min),
// computes a small set of fleet-wide aggregates, and emits Tier 2 fleet.*
// events when operator-tunable thresholds cross.
//
// Tier 1 events (sim.state_changed, session.started, etc.) NEVER persist a
// notification row — they exist only as observability data. Tier 2 events are
// the aggregated alerts that humans actually see; the digest worker is the
// sole publisher allowed to emit them (NotifyRequest.Source must be "digest"
// per notification.Service's tier guard at service.go:391-412).
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// JobTypeFleetDigest is the cron-scheduled job type registered by the runner
// to invoke Worker.Process on each tick. Matches internal/job/types.go naming.
const JobTypeFleetDigest = "fleet_digest"

// digestSource is the value the notification.Service tier guard checks to
// allow Tier 2 fleet.* events through. Any other source value is rejected
// with metric argus_events_tier_filtered{reason="digest_no_source"}.
const digestSource = "digest"

// fleetSourceLabel is the publisher identity used on the bus.Envelope.Source
// field. Matches the publisherSourceMap convention in notification/service.go.
const fleetSourceLabel = "digest"

// ---------------------------------------------------------------------------
// Narrow store interfaces — concrete *store.SIMStore (etc.) satisfy these.
// Defined locally so worker_test.go can inject mocks without spinning up a
// PostgreSQL test container. Mirrors the slaTenantLister/slaOperatorQuerier
// pattern in internal/job/sla_report.go.
// ---------------------------------------------------------------------------

type simAggregator interface {
	CountActiveAllTenants(ctx context.Context) (int64, error)
	CountStateTransitionsToInactiveAllTenants(ctx context.Context, from, to time.Time) (int64, error)
}

type cdrAggregator interface {
	SumBytesAllTenantsInWindow(ctx context.Context, from, to time.Time) (int64, error)
}

type violationAggregator interface {
	CountInWindowAllTenants(ctx context.Context, from, to time.Time) (int64, error)
}

// notifier is the narrow contract the worker uses to emit Tier 2 events.
// *notification.Service satisfies it; tests inject a recording fake.
type notifier interface {
	Notify(ctx context.Context, req notification.NotifyRequest) error
}

// envelopePublisher is the narrow contract the worker uses to publish raw
// envelopes onto NATS for the WebSocket Live Stream tab. *bus.EventBus
// satisfies it; tests inject a no-op or recording fake.
type envelopePublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

// Worker computes fleet-level digest aggregates on a fixed cadence and emits
// Tier 2 fleet.* events when thresholds cross. Worker is intended to be
// registered as a job.Processor by the cron scheduler (C3 wires it in
// cmd/argus/main.go); Process() is the entry point per tick.
//
// PAT-006: constructor signature is stable — C3's wiring expects exactly the
// argument order in NewWorker.
// PAT-011 + PAT-017: thresholds are captured at construction (config snapshot
// at boot); Process never re-reads env vars.
type Worker struct {
	simStore       simAggregator
	cdrStore       cdrAggregator
	violationStore violationAggregator
	notifService   notifier
	eventBus       envelopePublisher
	thresholds     Thresholds
	windowMinutes  int // 15 min default; bounds SQL window arithmetic
	baselineCount  int // number of preceding windows used for rolling baseline
	logger         zerolog.Logger
	clock          func() time.Time // injected for testability; defaults to time.Now
}

// NewWorker constructs a Worker. Concrete *store.SIMStore, *store.CDRStore,
// *store.PolicyViolationStore, *notification.Service, and *bus.EventBus all
// satisfy the local narrow interfaces. eventBus may be nil in tests; nil-safe
// at the publish call site. clock may be nil → defaults to time.Now.
func NewWorker(
	simStore *store.SIMStore,
	cdrStore *store.CDRStore,
	violationStore *store.PolicyViolationStore,
	notifService *notification.Service,
	eventBus *bus.EventBus,
	thresholds Thresholds,
	logger zerolog.Logger,
	clock func() time.Time,
) *Worker {
	return newWorkerWithDeps(simStore, cdrStore, violationStore, notifService, eventBus, thresholds, logger, clock)
}

// newWorkerWithDeps is the internal constructor that accepts the narrow
// interface types directly. Used by both NewWorker (concrete deps) and tests
// (mock deps). Keeps the public surface stable while allowing test injection.
func newWorkerWithDeps(
	simStore simAggregator,
	cdrStore cdrAggregator,
	violationStore violationAggregator,
	notifService notifier,
	eventBus envelopePublisher,
	thresholds Thresholds,
	logger zerolog.Logger,
	clock func() time.Time,
) *Worker {
	if clock == nil {
		clock = time.Now
	}
	return &Worker{
		simStore:       simStore,
		cdrStore:       cdrStore,
		violationStore: violationStore,
		notifService:   notifService,
		eventBus:       eventBus,
		thresholds:     thresholds,
		windowMinutes:  15,
		baselineCount:  6, // rolling baseline = avg over last 6 prior 15-min windows (1.5h lookback)
		logger:         logger.With().Str("processor", JobTypeFleetDigest).Logger(),
		clock:          clock,
	}
}

// Type returns the job type string used by the runner registry.
func (w *Worker) Type() string { return JobTypeFleetDigest }

// Process runs one digest tick. Each aggregate check runs independently so
// one failure does not abort the others; per-aggregate errors are warn-logged
// and the tick still returns nil so the cron scheduler reports success and
// schedules the next tick. A failure to publish either NATS or notification
// for a single aggregate is also non-fatal (logged warn).
//
// The job parameter may be nil in unit tests; we treat it as advisory only
// (logging) since the digest worker is cron-driven and does not consume
// job-specific payload data.
func (w *Worker) Process(ctx context.Context, j *store.Job) error {
	start := w.clock()
	windowEnd := start
	windowStart := windowEnd.Add(-time.Duration(w.windowMinutes) * time.Minute)

	jobID := ""
	if j != nil {
		jobID = j.ID.String()
	}
	w.logger.Info().
		Str("job_id", jobID).
		Time("window_start", windowStart).
		Time("window_end", windowEnd).
		Int("window_minutes", w.windowMinutes).
		Msg("fleet digest tick: starting")

	if err := w.checkMassOffline(ctx, windowStart, windowEnd); err != nil {
		w.logger.Warn().Err(err).Msg("digest: mass_offline check failed")
	}
	if err := w.checkTrafficSpike(ctx, windowStart, windowEnd); err != nil {
		w.logger.Warn().Err(err).Msg("digest: traffic_spike check failed")
	}
	if err := w.checkQuotaBreachCount(ctx, windowStart, windowEnd); err != nil {
		w.logger.Warn().Err(err).Msg("digest: quota_breach_count check failed")
	}
	if err := w.checkViolationSurge(ctx, windowStart, windowEnd); err != nil {
		w.logger.Warn().Err(err).Msg("digest: violation_surge check failed")
	}

	w.logger.Debug().
		Dur("elapsed", time.Since(start)).
		Msg("fleet digest tick: complete")
	return nil
}

// ---------------------------------------------------------------------------
// Aggregate checks.
// ---------------------------------------------------------------------------

// checkMassOffline fires fleet.mass_offline when a meaningful fraction of the
// active fleet has transitioned out of 'active' state (suspended, stolen_lost,
// terminated) within the window. Severity scales with the percentage.
//
// Rationale on data source: Argus does not currently track per-SIM heartbeats
// or last-seen timestamps in the SIM table; the closest available signal is
// the sim_state_history audit trail. Operationally, a wave of suspensions or
// terminations in a 15-min window is what an admin would call "mass offline"
// — a fleet-wide event that warrants an aggregate alert. When per-SIM
// heartbeat tracking ships, swap the data source here without changing the
// emit surface.
func (w *Worker) checkMassOffline(ctx context.Context, start, end time.Time) error {
	activeCount, err := w.simStore.CountActiveAllTenants(ctx)
	if err != nil {
		return fmt.Errorf("count active sims: %w", err)
	}
	if activeCount == 0 {
		w.logger.Debug().Msg("mass_offline: no active SIMs in fleet, skipping")
		return nil
	}

	offlineCount, err := w.simStore.CountStateTransitionsToInactiveAllTenants(ctx, start, end)
	if err != nil {
		return fmt.Errorf("count offline transitions: %w", err)
	}
	if offlineCount < int64(w.thresholds.MassOfflineFloor) {
		w.logger.Debug().
			Int64("offline_count", offlineCount).
			Int("floor", w.thresholds.MassOfflineFloor).
			Msg("mass_offline: below absolute floor, skipping")
		return nil
	}

	pct := float64(offlineCount) / float64(activeCount) * 100.0
	if pct < w.thresholds.MassOfflinePct {
		w.logger.Debug().
			Float64("pct", pct).
			Float64("threshold_pct", w.thresholds.MassOfflinePct).
			Msg("mass_offline: below percentage threshold, skipping")
		return nil
	}

	sev := severityForPercentage(pct)
	pctRounded := math.Round(pct*100) / 100
	summary := fmt.Sprintf("%d SIMs left active state in last %d min (%.1f%% of fleet)",
		offlineCount, w.windowMinutes, pctRounded)

	return w.emitFleetEvent(ctx,
		bus.SubjectFleetMassOffline,
		"fleet.mass_offline",
		sev,
		summary,
		map[string]interface{}{
			"offline_count":  offlineCount,
			"active_count":   activeCount,
			"offline_pct":    pctRounded,
			"window_minutes": w.windowMinutes,
		},
	)
}

// checkTrafficSpike fires fleet.traffic_spike when fleet-wide bytes in the
// current window exceed the rolling baseline (avg of the previous N windows)
// by Thresholds.TrafficSpikeRatio. A floor of 1MB on the baseline avoids
// divide-by-near-zero noise on idle fleets.
func (w *Worker) checkTrafficSpike(ctx context.Context, start, end time.Time) error {
	currentBytes, err := w.cdrStore.SumBytesAllTenantsInWindow(ctx, start, end)
	if err != nil {
		return fmt.Errorf("sum current bytes: %w", err)
	}
	if currentBytes <= 0 {
		w.logger.Debug().Msg("traffic_spike: no current traffic, skipping")
		return nil
	}

	// Rolling baseline: average of the previous baselineCount windows ending
	// at `start`. The baseline window itself is left-open to avoid double-
	// counting `start`.
	windowDur := time.Duration(w.windowMinutes) * time.Minute
	var baselineSum int64
	for i := 1; i <= w.baselineCount; i++ {
		bEnd := start.Add(-time.Duration(i-1) * windowDur)
		bStart := bEnd.Add(-windowDur)
		bytes, sumErr := w.cdrStore.SumBytesAllTenantsInWindow(ctx, bStart, bEnd)
		if sumErr != nil {
			w.logger.Warn().Err(sumErr).
				Time("baseline_window_start", bStart).
				Msg("traffic_spike: baseline window query failed, treating as 0")
			continue
		}
		baselineSum += bytes
	}
	baselineAvg := float64(baselineSum) / float64(w.baselineCount)

	// Floor on baseline to avoid noise on near-idle fleets (1 MB).
	const baselineFloorBytes = 1 << 20
	if baselineAvg < baselineFloorBytes {
		w.logger.Debug().
			Float64("baseline_avg_bytes", baselineAvg).
			Msg("traffic_spike: baseline below floor, skipping")
		return nil
	}

	ratio := float64(currentBytes) / baselineAvg
	if ratio < w.thresholds.TrafficSpikeRatio {
		w.logger.Debug().
			Float64("ratio", ratio).
			Float64("threshold", w.thresholds.TrafficSpikeRatio).
			Msg("traffic_spike: below ratio threshold, skipping")
		return nil
	}

	sev := severityForRatio(ratio)
	ratioRounded := math.Round(ratio*100) / 100
	summary := fmt.Sprintf("Fleet traffic %.2fx baseline in last %d min (%d bytes vs %d avg)",
		ratioRounded, w.windowMinutes, currentBytes, int64(baselineAvg))

	return w.emitFleetEvent(ctx,
		bus.SubjectFleetTrafficSpike,
		"fleet.traffic_spike",
		sev,
		summary,
		map[string]interface{}{
			"current_bytes":  currentBytes,
			"baseline_bytes": int64(baselineAvg),
			"ratio":          ratioRounded,
			"baseline_count": w.baselineCount,
			"window_minutes": w.windowMinutes,
		},
	)
}

// checkQuotaBreachCount fires fleet.quota_breach_count when the number of
// SIMs that crossed their quota in the window exceeds the configured count.
//
// FIX-237 NO-OP: a per-tenant quota_state table with breach timestamps does
// not yet exist in the schema; the closest signal — policy_violations rows
// with violation_type='quota_exceeded' — is already covered by the dedicated
// violation_surge aggregate below. To avoid double-firing on the same
// underlying signal, this check is a deliberate no-op until a dedicated
// quota_breach event source ships. Keep the worker operational; the emit
// surface stays in place so flipping the data source is mechanical.
func (w *Worker) checkQuotaBreachCount(ctx context.Context, start, end time.Time) error {
	w.logger.Debug().
		Time("window_start", start).
		Time("window_end", end).
		Msg("quota_breach_count: NO-OP — awaiting dedicated quota_state breach signal (FIX-237)")
	return nil
}

// checkViolationSurge fires fleet.violation_surge when policy_violations
// inserts in the current window are >= ViolationSurgeRatio × the rolling
// baseline (avg of the previous N windows) AND >= ViolationSurgeFloor.
func (w *Worker) checkViolationSurge(ctx context.Context, start, end time.Time) error {
	currentCount, err := w.violationStore.CountInWindowAllTenants(ctx, start, end)
	if err != nil {
		return fmt.Errorf("count current violations: %w", err)
	}
	if currentCount < int64(w.thresholds.ViolationSurgeFloor) {
		w.logger.Debug().
			Int64("current", currentCount).
			Int("floor", w.thresholds.ViolationSurgeFloor).
			Msg("violation_surge: below absolute floor, skipping")
		return nil
	}

	windowDur := time.Duration(w.windowMinutes) * time.Minute
	var baselineSum int64
	for i := 1; i <= w.baselineCount; i++ {
		bEnd := start.Add(-time.Duration(i-1) * windowDur)
		bStart := bEnd.Add(-windowDur)
		c, cErr := w.violationStore.CountInWindowAllTenants(ctx, bStart, bEnd)
		if cErr != nil {
			w.logger.Warn().Err(cErr).
				Time("baseline_window_start", bStart).
				Msg("violation_surge: baseline window query failed, treating as 0")
			continue
		}
		baselineSum += c
	}
	baselineAvg := float64(baselineSum) / float64(w.baselineCount)

	// Floor on baseline (avoid divide-by-zero); treat <1 as 1 so a sudden
	// burst from a quiet baseline still produces a meaningful ratio.
	if baselineAvg < 1 {
		baselineAvg = 1
	}

	ratio := float64(currentCount) / baselineAvg
	if ratio < w.thresholds.ViolationSurgeRatio {
		w.logger.Debug().
			Float64("ratio", ratio).
			Float64("threshold", w.thresholds.ViolationSurgeRatio).
			Msg("violation_surge: below ratio threshold, skipping")
		return nil
	}

	sev := severityForRatio(ratio)
	ratioRounded := math.Round(ratio*100) / 100
	summary := fmt.Sprintf("Policy violations surged %.2fx baseline in last %d min (%d vs %d avg)",
		ratioRounded, w.windowMinutes, currentCount, int64(baselineAvg))

	return w.emitFleetEvent(ctx,
		bus.SubjectFleetViolationSurge,
		"fleet.violation_surge",
		sev,
		summary,
		map[string]interface{}{
			"current_count":   currentCount,
			"baseline_count":  int64(baselineAvg),
			"ratio":           ratioRounded,
			"baseline_window": w.baselineCount,
			"window_minutes":  w.windowMinutes,
		},
	)
}

// ---------------------------------------------------------------------------
// Emit helper.
// ---------------------------------------------------------------------------

// emitFleetEvent is the single egress point for Tier 2 fleet.* events. It:
//  1. Builds a canonical bus.Envelope (FIX-212) and publishes it to NATS so
//     the WebSocket Live Stream tab sees the event in real time.
//  2. Calls notification.Service.Notify with Source="digest" so the tier
//     guard at service.go:401-411 admits the event into the persistence +
//     dispatch path. Without Source="digest" the event would be dropped with
//     the digest_no_source filter reason.
//
// NATS publish failures are warn-logged but do not block the notification
// path; notification failures bubble up as the function's return error.
func (w *Worker) emitFleetEvent(
	ctx context.Context,
	subject string,
	eventType string,
	sev string,
	summary string,
	meta map[string]interface{},
) error {
	tenantID := bus.SystemTenantID
	title := fmt.Sprintf("Fleet alert: %s", eventType)

	if w.eventBus != nil {
		env := bus.NewEnvelope(eventType, tenantID.String(), sev).
			WithSource(fleetSourceLabel).
			WithTitle(title).
			WithMessage(summary)
		for k, v := range meta {
			env.WithMeta(k, v)
		}
		if pubErr := w.eventBus.Publish(ctx, subject, env); pubErr != nil {
			w.logger.Warn().Err(pubErr).
				Str("subject", subject).
				Msg("digest: NATS publish failed (continuing to notification path)")
		}
	}

	body := summary
	if len(meta) > 0 {
		if metaJSON, err := json.Marshal(meta); err == nil {
			body = summary + "\nDetails: " + string(metaJSON)
		}
	}

	req := notification.NotifyRequest{
		TenantID:  tenantID,
		EventType: notification.EventType(eventType),
		ScopeType: notification.ScopeSystem,
		Severity:  sev,
		Title:     title,
		Body:      body,
		Source:    digestSource,
	}
	if err := w.notifService.Notify(ctx, req); err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	w.logger.Info().
		Str("event_type", eventType).
		Str("severity", sev).
		Str("subject", subject).
		Msg("digest emitted fleet event")
	return nil
}

// ---------------------------------------------------------------------------
// Severity helpers — pure functions, exhaustively tested in worker_test.go.
// ---------------------------------------------------------------------------

// severityForPercentage maps the offline percentage to a canonical severity.
// Used by checkMassOffline. Bands tuned so a 5% trigger ships as low/medium
// and a 30%+ wave escalates to critical.
func severityForPercentage(pct float64) string {
	switch {
	case pct >= 30.0:
		return severity.Critical
	case pct >= 15.0:
		return severity.High
	case pct >= 5.0:
		return severity.Medium
	default:
		return severity.Low
	}
}

// severityForRatio maps a current/baseline ratio to a canonical severity.
// Used by checkTrafficSpike and checkViolationSurge. Bands tuned so the
// minimum-trigger ratio (e.g. 2x or 3x) lands as low/medium and a 10x+ burst
// escalates to critical.
func severityForRatio(ratio float64) string {
	switch {
	case ratio >= 10.0:
		return severity.Critical
	case ratio >= 5.0:
		return severity.High
	case ratio >= 3.0:
		return severity.Medium
	default:
		return severity.Low
	}
}

// Compile-time assertion that *Worker satisfies the job.Processor contract.
// The runner package imports digest at wire time (cmd/argus/main.go); if the
// signature ever drifts, the build breaks here — not at runtime.
var _ interface {
	Type() string
	Process(ctx context.Context, j *store.Job) error
} = (*Worker)(nil)

// ensure uuid import is referenced (used indirectly via bus.SystemTenantID
// which is uuid.UUID — keep the import explicit so future changes that drop
// uuid usage do not break the build silently).
var _ = uuid.Nil
