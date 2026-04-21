package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/crypto"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/operator/adapterschema"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type CachedHealth struct {
	Status       string `json:"status"`
	LatencyMs    int    `json:"latency_ms"`
	CircuitState string `json:"circuit_state"`
	CheckedAt    string `json:"checked_at"`
}

// healthKey mirrors the adapter-registry / router adapterKey shape.
// STORY-090 Wave 2 Task 3: health state is tracked per (operator,
// protocol) tuple so one operator can fan out probes across multiple
// protocols independently.
type healthKey struct {
	OperatorID uuid.UUID
	Protocol   string
}

type HealthChecker struct {
	store         *store.OperatorStore
	registry      *adapter.Registry
	redisClient   *redis.Client
	encryptionKey string
	logger        zerolog.Logger
	eventPub      EventPublisher
	slaTracker    *SLATracker
	healthSubject string
	alertSubject  string
	metricsReg    *obsmetrics.Registry

	mu       sync.Mutex
	breakers map[healthKey]*CircuitBreaker
	// stopChs is keyed by operator ID — STORY-090 Gate (F-A5): a
	// single ticker per operator iterates enabled protocols
	// sequentially per tick, replacing the Wave-2 per-protocol
	// goroutine fan-out. Scaling implication: 100 ops × 5 protocols
	// = 1 goroutine (not 5) per op, keeping the probe pool bounded
	// to N_operators instead of N_operators × N_protocols.
	stopChs    map[uuid.UUID]chan struct{}
	lastStatus map[healthKey]string
	// lastLatency tracks the most recent LatencyMs probe result per
	// (operator, protocol) tuple. FIX-203 AC-3: health worker must
	// publish argus.events.operator.health.changed when status flips
	// OR when latency delta vs. prior tick exceeds 10%. The value 0
	// is a cold-start sentinel (no prior probe): it suppresses the
	// latency-trigger path until the second tick populates it,
	// avoiding startup noise.
	lastLatency   map[healthKey]int
	operatorNames map[uuid.UUID]string
	wg            sync.WaitGroup
	stopped       bool
}

func NewHealthChecker(
	opStore *store.OperatorStore,
	registry *adapter.Registry,
	redisClient *redis.Client,
	encryptionKey string,
	logger zerolog.Logger,
) *HealthChecker {
	return &HealthChecker{
		store:         opStore,
		registry:      registry,
		redisClient:   redisClient,
		encryptionKey: encryptionKey,
		logger:        logger.With().Str("component", "health_checker").Logger(),
		breakers:      make(map[healthKey]*CircuitBreaker),
		stopChs:       make(map[uuid.UUID]chan struct{}),
		lastStatus:    make(map[healthKey]string),
		lastLatency:   make(map[healthKey]int),
		operatorNames: make(map[uuid.UUID]string),
	}
}

func (hc *HealthChecker) SetEventPublisher(pub EventPublisher, healthSubject, alertSubject string) {
	hc.eventPub = pub
	hc.healthSubject = healthSubject
	hc.alertSubject = alertSubject
}

func (hc *HealthChecker) SetSLATracker(tracker *SLATracker) {
	hc.slaTracker = tracker
}

// SetMetricsRegistry wires the Prometheus registry used to expose the
// operator health gauge and circuit breaker state gauge. Safe to call
// at any time; when nil, metrics updates are silently skipped.
func (hc *HealthChecker) SetMetricsRegistry(reg *obsmetrics.Registry) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.metricsReg = reg
	// Attach the transition hook to every breaker already running so
	// existing loops also publish state changes.
	for k, cb := range hc.breakers {
		hc.attachBreakerHookLocked(k.OperatorID, cb)
	}
}

// attachBreakerHookLocked installs the transition hook for a single
// breaker. Caller must hold hc.mu.
func (hc *HealthChecker) attachBreakerHookLocked(opID uuid.UUID, cb *CircuitBreaker) {
	if cb == nil {
		return
	}
	reg := hc.metricsReg
	if reg == nil {
		cb.SetTransitionHook(nil)
		return
	}
	idStr := opID.String()
	cb.SetTransitionHook(func(state CircuitState) {
		reg.SetCircuitBreakerState(idStr, string(state))
	})
	// Seed the gauge with the breaker's current state so the metric is
	// non-zero even before the first transition.
	reg.SetCircuitBreakerState(idStr, string(cb.State()))
}

func (hc *HealthChecker) Start(ctx context.Context) error {
	operators, err := hc.store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("health checker: list operators: %w", err)
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	for _, op := range operators {
		hc.startOperatorLoop(op)
	}

	hc.logger.Info().Int("operator_count", len(operators)).Msg("health checker started")
	return nil
}

// normalizeAdapterConfig decrypts (if a key is set), detects the
// shape, and up-converts any legacy flat adapter_config to the
// canonical nested shape. Re-persists legacy rows back to the DB as
// a best-effort side effect so subsequent reads hit the fast path.
// Returns the plaintext adapter_config (nested on success, or the
// decrypted bytes unchanged on any error — callers tolerate this to
// keep probes running through corrupted envelopes). See
// STORY-090-plan.md §Decision Points > D1 / D1-A.
func (hc *HealthChecker) normalizeAdapterConfig(ctx context.Context, op store.Operator) json.RawMessage {
	raw := op.AdapterConfig
	if len(raw) == 0 {
		return raw
	}
	plaintext := raw
	if hc.encryptionKey != "" {
		if decrypted, err := crypto.DecryptJSON(raw, hc.encryptionKey); err == nil {
			plaintext = decrypted
		} else {
			hc.logger.Warn().Err(err).Str("operator_id", op.ID.String()).Msg("health checker: decrypt adapter_config failed; using raw")
			return raw
		}
	}
	shape, detectErr := adapterschema.DetectShape(plaintext)
	if detectErr != nil && detectErr != adapterschema.ErrShapeUnknown {
		// Invalid JSON post-decrypt. The probe still runs against the
		// raw bytes below — the adapter factory's own JSON decode
		// will catch this. Log once for operator visibility.
		hc.logger.Warn().Err(detectErr).Str("operator_id", op.ID.String()).Msg("health checker: adapter_config shape detect failed")
		return plaintext
	}
	if shape == adapterschema.ShapeNested {
		return plaintext
	}
	// Legacy flat (or ShapeUnknown with hint): up-convert + re-persist.
	// Hint derivation: post-Wave-2 we no longer persist AdapterType on
	// the operator row, so the hint comes from the detected shape's
	// canonical protocol (shapeToProtocol via adapterschema) or empty
	// — UpConvert tolerates empty hints for shapes it can classify.
	n, err := adapterschema.UpConvert(plaintext, "")
	if err != nil {
		hc.logger.Warn().Err(err).Str("operator_id", op.ID.String()).Msg("health checker: adapter_config up-convert failed")
		return plaintext
	}
	nested, err := adapterschema.MarshalNested(n)
	if err != nil {
		hc.logger.Warn().Err(err).Str("operator_id", op.ID.String()).Msg("health checker: marshal nested adapter_config failed")
		return plaintext
	}
	hc.logger.Info().
		Str("op", "adapter_config_upconvert").
		Str("operator_id", op.ID.String()).
		Str("old_shape", shape.String()).
		Str("new_shape", "nested").
		Msg("health checker: upconverted legacy adapter_config to nested shape")
	// Best-effort re-persist (skipped if store is nil — exercised by
	// unit tests that inject nested-only fixtures).
	if hc.store != nil {
		toPersist := nested
		if hc.encryptionKey != "" {
			if enc, encErr := crypto.EncryptJSON(nested, hc.encryptionKey); encErr == nil {
				toPersist = enc
			} else {
				hc.logger.Warn().Err(encErr).Str("operator_id", op.ID.String()).Msg("health checker: re-encrypt upconverted config failed; skipping re-persist")
				return nested
			}
		}
		if _, upErr := hc.store.Update(ctx, op.ID, store.UpdateOperatorParams{AdapterConfig: toPersist}); upErr != nil {
			hc.logger.Warn().Err(upErr).Str("operator_id", op.ID.String()).Msg("health checker: re-persist upconverted config failed")
		}
	}
	return nested
}

// startOperatorLoop launches a single ticker goroutine per operator
// that iterates the operator's enabled protocols sequentially on each
// tick. STORY-090 Wave 2 Task 3 + Gate (F-A5): per-protocol breakers
// and metric series are preserved — only the ticker pool collapses
// from N_operators × N_protocols goroutines to N_operators goroutines.
//
// The protocol list is computed ONCE at loop start; if the operator's
// adapter_config changes via PATCH, the caller must invoke
// RefreshOperator to tear down + rebuild. An operator with zero
// enabled protocols still runs the ticker (so RefreshOperator works
// idempotently), but probes no-op until protocols are enabled.
//
// Each tick calls checkOperator sequentially per protocol. Acceptable
// trade-off: 5 protocols × ~500ms probe ≤ 3s, far under the default
// 30s interval. One slow probe delays the next protocol in the same
// tick but NOT the next tick (ticker continues to fire on schedule;
// drift is bounded by the last protocol's probe time, not cumulative).
func (hc *HealthChecker) startOperatorLoop(op store.Operator) {
	hc.operatorNames[op.ID] = op.Name

	interval := time.Duration(op.HealthCheckIntervalSec) * time.Second
	if interval < time.Second {
		interval = 30 * time.Second
	}

	// Decrypt + up-convert + parse: nested post-Wave-1 plaintext drives
	// per-protocol iteration. Failure → treat as a single mock-ish loop
	// so the operator is still observable. This preserves Wave-1
	// behaviour for corrupted envelopes / raw fallback cases.
	plaintext := hc.normalizeAdapterConfig(context.Background(), op)

	type protocolProbe struct {
		name   string
		config json.RawMessage
		cb     *CircuitBreaker
	}
	var probes []protocolProbe

	parsed, parseErr := adapterschema.ParseNested(plaintext)
	if parseErr != nil {
		hc.logger.Warn().Err(parseErr).Str("operator_id", op.ID.String()).Msg("health checker: parse nested adapter_config failed; using mock fallback")
		probes = []protocolProbe{{name: "mock", config: plaintext}}
	} else {
		enabled := adapterschema.DeriveEnabledProtocols(parsed)
		if len(enabled) == 0 {
			hc.logger.Info().Str("operator_id", op.ID.String()).Msg("health checker: operator has zero enabled protocols; ticker still running as no-op")
		}
		probes = make([]protocolProbe, 0, len(enabled))
		for _, protocol := range enabled {
			probes = append(probes, protocolProbe{
				name:   protocol,
				config: adapterschema.SubConfigRaw(parsed, protocol),
			})
		}
	}

	// Register per-protocol breakers + seed per-protocol gauges before
	// the ticker starts so the first tick already has state to read.
	for i := range probes {
		cb := NewCircuitBreaker(op.CircuitBreakerThreshold, op.CircuitBreakerRecoverySec)
		key := healthKey{OperatorID: op.ID, Protocol: probes[i].name}
		hc.breakers[key] = cb
		hc.lastStatus[key] = op.HealthStatus
		// FIX-203: 0 is the cold-start sentinel — the latency-trigger
		// publish path stays suppressed until the first probe lands a
		// non-zero sample on the next tick. See comment on lastLatency.
		hc.lastLatency[key] = 0
		hc.attachBreakerHookLocked(op.ID, cb)
		if hc.metricsReg != nil {
			hc.metricsReg.SetOperatorHealth(op.ID.String(), probes[i].name, op.HealthStatus)
		}
		probes[i].cb = cb
	}

	stopCh := make(chan struct{})
	hc.stopChs[op.ID] = stopCh

	hc.wg.Add(1)
	go func(opID uuid.UUID, ps []protocolProbe, tick time.Duration, intSec int) {
		defer hc.wg.Done()

		ticker := time.NewTicker(tick)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				for _, p := range ps {
					hc.checkOperator(opID, p.name, p.config, p.cb, intSec)
				}
			}
		}
	}(op.ID, probes, interval, op.HealthCheckIntervalSec)
}

func (hc *HealthChecker) checkOperator(opID uuid.UUID, adapterType string, config json.RawMessage, cb *CircuitBreaker, intervalSec int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a, err := hc.registry.GetOrCreate(opID, adapterType, config)
	if err != nil {
		hc.logger.Error().Err(err).Str("operator_id", opID.String()).Str("protocol", adapterType).Msg("create adapter for health check")
		return
	}

	result := a.HealthCheck(ctx)

	var status string
	if result.Success {
		cb.RecordSuccess()
	} else {
		cb.RecordFailure()
	}

	cbState := cb.State()
	switch cbState {
	case CircuitOpen:
		status = "down"
	case CircuitHalfOpen:
		status = "degraded"
	case CircuitClosed:
		if result.Success {
			status = "healthy"
		} else {
			status = "degraded"
		}
	default:
		status = "unknown"
	}

	// Publish the operator health gauge once the current status is
	// resolved. Snapshot the registry pointer under the lock to stay
	// race-free with concurrent SetMetricsRegistry calls.
	hc.mu.Lock()
	metricsReg := hc.metricsReg
	hc.mu.Unlock()
	if metricsReg != nil {
		metricsReg.SetOperatorHealth(opID.String(), adapterType, status)
	}

	var latencyMs *int
	if result.LatencyMs > 0 {
		latencyMs = &result.LatencyMs
	}
	var errorMsg *string
	if result.Error != "" {
		errorMsg = &result.Error
	}

	if hc.store != nil {
		if err := hc.store.InsertHealthLog(ctx, opID, status, latencyMs, errorMsg, string(cbState)); err != nil {
			hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("insert health log")
		}

		if err := hc.store.UpdateHealthStatus(ctx, opID, status); err != nil {
			hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("update health status")
		}
	}

	if hc.redisClient != nil {
		cached := CachedHealth{
			Status:       status,
			LatencyMs:    result.LatencyMs,
			CircuitState: string(cbState),
			CheckedAt:    time.Now().Format(time.RFC3339),
		}
		data, _ := json.Marshal(cached)
		key := fmt.Sprintf("operator:health:%s", opID.String())
		ttl := 2 * time.Duration(intervalSec) * time.Second
		if ttl < 30*time.Second {
			ttl = 60 * time.Second
		}
		hc.redisClient.Set(ctx, key, data, ttl)
	}

	if hc.slaTracker != nil && result.LatencyMs > 0 {
		hc.slaTracker.RecordLatency(ctx, opID, result.LatencyMs)
	}

	hkey := healthKey{OperatorID: opID, Protocol: adapterType}
	hc.mu.Lock()
	prevStatus := hc.lastStatus[hkey]
	prevLatency := hc.lastLatency[hkey]
	opName := hc.operatorNames[opID]
	hc.lastStatus[hkey] = status
	hc.lastLatency[hkey] = result.LatencyMs
	hc.mu.Unlock()

	// FIX-203 AC-3: widen the publish gate to fire the health.changed
	// event on status flip OR latency delta > 10% vs. the prior tick.
	// Cold-start (prevLatency == 0) suppresses the latency-trigger path
	// until the second tick populates a real sample; any tick that lands
	// result.LatencyMs == 0 (timeout / adapter didn't record a sample)
	// is likewise excluded from the delta computation so we never divide
	// by zero nor extrapolate from a missing reading.
	statusFlipped := prevStatus != status
	latencyChanged := prevLatency > 0 && result.LatencyMs > 0 &&
		math.Abs(float64(result.LatencyMs-prevLatency))/float64(prevLatency) > 0.10
	shouldPublish := hc.eventPub != nil && hc.healthSubject != "" && (statusFlipped || latencyChanged)

	if shouldPublish {
		evt := OperatorHealthEvent{
			OperatorID:     opID,
			OperatorName:   opName,
			PreviousStatus: prevStatus,
			CurrentStatus:  status,
			CircuitState:   string(cbState),
			LatencyMs:      result.LatencyMs,
			FailureReason:  result.Error,
			Timestamp:      time.Now(),
		}
		if pubErr := hc.eventPub.Publish(ctx, hc.healthSubject, evt); pubErr != nil {
			hc.logger.Error().Err(pubErr).Str("operator_id", opID.String()).Msg("publish health event")
		} else {
			hc.logger.Info().
				Str("operator_id", opID.String()).
				Str("protocol", adapterType).
				Str("from", prevStatus).
				Str("to", status).
				Int("prev_latency_ms", prevLatency).
				Int("latency_ms", result.LatencyMs).
				Bool("status_flipped", statusFlipped).
				Bool("latency_changed", latencyChanged).
				Msg("operator health changed event published")
		}
	}

	// FIX-203: down/recovered ALERTS remain gated on status flip alone.
	// A latency-only tick where status stays "down" must NOT re-fire the
	// AlertTypeOperatorDown alert — that would be a regression the
	// widened publish gate would otherwise introduce.
	if statusFlipped && hc.eventPub != nil && hc.healthSubject != "" {
		if status == "down" {
			hc.publishAlert(ctx, opID, opName, AlertTypeOperatorDown, SeverityCritical,
				fmt.Sprintf("Operator %s is DOWN", opName),
				fmt.Sprintf("Operator %s circuit breaker opened. Reason: %s", opName, result.Error),
			)
		} else if prevStatus == "down" && (status == "healthy" || status == "degraded") {
			hc.publishAlert(ctx, opID, opName, AlertTypeOperatorUp, SeverityInfo,
				fmt.Sprintf("Operator %s recovered", opName),
				fmt.Sprintf("Operator %s recovered from down state, current status: %s", opName, status),
			)
		}
	}

	hc.checkSLAViolation(ctx, opID, opName)
}

func (hc *HealthChecker) publishAlert(ctx context.Context, opID uuid.UUID, opName, alertType, severity, title, description string) {
	if hc.eventPub == nil || hc.alertSubject == "" {
		return
	}
	evt := AlertEvent{
		AlertID:     uuid.New().String(),
		AlertType:   alertType,
		Severity:    severity,
		Title:       title,
		Description: description,
		EntityType:  "operator",
		EntityID:    opID,
		Metadata: map[string]interface{}{
			"operator_name": opName,
		},
		Timestamp: time.Now(),
	}
	if err := hc.eventPub.Publish(ctx, hc.alertSubject, evt); err != nil {
		hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("publish alert event")
	}
}

func (hc *HealthChecker) checkSLAViolation(ctx context.Context, opID uuid.UUID, opName string) {
	if hc.slaTracker == nil || hc.store == nil {
		return
	}

	total, failures, err := hc.store.CountFailures24h(ctx, opID)
	if err != nil {
		hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("count failures for SLA")
		return
	}

	op, err := hc.store.GetByID(ctx, opID)
	if err != nil {
		return
	}

	metrics := hc.slaTracker.ComputeMetrics(ctx, opID, int64(total), int64(failures), op.SLAUptimeTarget)

	if metrics.SLAViolation {
		hc.publishAlert(ctx, opID, opName, AlertTypeSLAViolation, SeverityHigh,
			fmt.Sprintf("SLA violation for operator %s", opName),
			fmt.Sprintf("Operator %s uptime %.2f%% is below SLA target %.2f%%. P95 latency: %dms",
				opName, metrics.Uptime24h, metrics.SLATarget, metrics.LatencyP95Ms),
		)
	}
}

func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	if hc.stopped {
		hc.mu.Unlock()
		return
	}
	hc.stopped = true
	// Close all per-operator ticker channels.
	for _, ch := range hc.stopChs {
		close(ch)
	}
	// Snapshot the gauge series to retire + the metrics registry while
	// still under the lock; perform the actual DeleteLabelValues AFTER
	// wg.Wait so a mid-tick goroutine cannot re-create the series via
	// SetOperatorHealth between delete and goroutine exit.
	reg := hc.metricsReg
	pendingDeletes := make([]healthKey, 0, len(hc.breakers))
	if reg != nil {
		for k := range hc.breakers {
			pendingDeletes = append(pendingDeletes, k)
		}
	}
	hc.mu.Unlock()
	hc.wg.Wait()
	// STORY-090 Gate (F-A1): retire every per-(op, protocol) gauge
	// series now that all goroutines have exited. Series cannot
	// resurrect.
	if reg != nil {
		for _, k := range pendingDeletes {
			reg.DeleteOperatorHealth(k.OperatorID.String(), k.Protocol)
		}
	}
	hc.logger.Info().Msg("health checker stopped")
}

// RefreshOperator tears down every protocol loop for the operator, then
// re-reads the row and re-fans-out. Used when adapter_config changes
// in the HTTP handler — the registry is purged at all protocols so
// fresh adapter instances pick up the new config.
func (hc *HealthChecker) RefreshOperator(ctx context.Context, opID uuid.UUID) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Close the per-operator ticker. STORY-090 Gate (F-A5): one stopCh
	// per operator now, not per (op, protocol).
	if ch, ok := hc.stopChs[opID]; ok {
		close(ch)
		delete(hc.stopChs, opID)
	}
	// Drop every per-protocol breaker + lastStatus + gauge series for
	// the operator; startOperatorLoop will re-create them from the
	// refreshed adapter_config.
	for k := range hc.breakers {
		if k.OperatorID == opID {
			delete(hc.breakers, k)
			delete(hc.lastStatus, k)
			// FIX-203: lastLatency is seeded alongside lastStatus in
			// startOperatorLoop — drop it together so the re-created
			// protocol loop starts from the cold-start sentinel.
			delete(hc.lastLatency, k)
			// STORY-090 Gate (F-A1): drop the stale gauge series so a
			// protocol disabled via PATCH stops reporting as "last
			// known status" forever. A fresh series is created on the
			// next startOperatorLoop for any still-enabled protocols.
			if hc.metricsReg != nil {
				hc.metricsReg.DeleteOperatorHealth(opID.String(), k.Protocol)
			}
		}
	}
	delete(hc.operatorNames, opID)

	hc.registry.Remove(opID)

	op, err := hc.store.GetByID(ctx, opID)
	if err != nil {
		return fmt.Errorf("health checker: refresh operator: %w", err)
	}

	if op.State != "active" {
		return nil
	}

	hc.startOperatorLoop(*op)
	return nil
}

func (hc *HealthChecker) GetCachedHealth(ctx context.Context, opID uuid.UUID) (*CachedHealth, error) {
	if hc.redisClient != nil {
		key := fmt.Sprintf("operator:health:%s", opID.String())
		data, err := hc.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var ch CachedHealth
			if json.Unmarshal(data, &ch) == nil {
				return &ch, nil
			}
		}
	}

	log, err := hc.store.GetLatestHealth(ctx, opID)
	if err != nil {
		return nil, err
	}
	if log == nil {
		return nil, nil
	}

	latency := 0
	if log.LatencyMs != nil {
		latency = *log.LatencyMs
	}

	return &CachedHealth{
		Status:       log.Status,
		LatencyMs:    latency,
		CircuitState: log.CircuitState,
		CheckedAt:    log.CheckedAt.Format(time.RFC3339),
	}, nil
}

// GetCircuitBreaker returns the breaker for (opID, protocol), or nil
// if none exists. STORY-090 Wave 2: protocol argument gained.
func (hc *HealthChecker) GetCircuitBreaker(opID uuid.UUID, protocol string) *CircuitBreaker {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	return hc.breakers[healthKey{OperatorID: opID, Protocol: protocol}]
}
