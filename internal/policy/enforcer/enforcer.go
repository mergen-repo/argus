package enforcer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	policycache "github.com/btopcu/argus/internal/policy/cache"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// enforcerAlertKey is the (policy_id, sim_id) tuple used as the
// rate-limiter map key. Struct with UUID values (not pointers) so it is
// comparable and usable as a map key.
type enforcerAlertKey struct {
	PolicyID uuid.UUID
	SIMID    uuid.UUID
}

// defaultEnforcerMinInterval is the minimum interval between two alert
// publishes for the same (policy_id, sim_id) tuple. FIX-210 Task 4:
// prevents a rapid-violation loop from spamming the alert bus — the DB
// would dedup them anyway, but this saves the NATS round-trip per
// suppressed event.
const defaultEnforcerMinInterval = 60 * time.Second

type EnforcementResult struct {
	Allow          bool
	SessionTimeout int
	IdleTimeout    int
	BandwidthDown  int64
	BandwidthUp    int64
	FilterID       string
	Actions        []dsl.ActionResult
	Violations     []ViolationRecord
	PolicyID       uuid.UUID
	VersionID      uuid.UUID
}

type ViolationRecord struct {
	ViolationType string
	ActionTaken   string
	Severity      string
	RuleIndex     int
	Details       map[string]interface{}
}

type Enforcer struct {
	policyCache    *policycache.Cache
	policyStore    *store.PolicyStore
	violationStore *store.PolicyViolationStore
	eventBus       *bus.EventBus
	rdb            *redis.Client
	evaluator      *dsl.Evaluator
	logger         zerolog.Logger

	// FIX-210 Task 4 — per-(policy, sim) rate limiter for alert publishes.
	// Map of last-publish time; entries are never evicted (10K SIMs ×
	// 10 policies = ~100K entries, trivial memory). Guarded by rlMu.
	rlMu          sync.Mutex
	rlLastEmitted map[enforcerAlertKey]time.Time
	rlMinInterval time.Duration
	rlNow         func() time.Time // injection point for tests
	metricsReg    *obsmetrics.Registry
}

func New(
	policyCache *policycache.Cache,
	policyStore *store.PolicyStore,
	violationStore *store.PolicyViolationStore,
	eventBus *bus.EventBus,
	rdb *redis.Client,
	logger zerolog.Logger,
) *Enforcer {
	return &Enforcer{
		policyCache:    policyCache,
		policyStore:    policyStore,
		violationStore: violationStore,
		eventBus:       eventBus,
		rdb:            rdb,
		evaluator:      dsl.NewEvaluator(),
		logger:         logger.With().Str("component", "policy_enforcer").Logger(),
		rlLastEmitted:  make(map[enforcerAlertKey]time.Time),
		rlMinInterval:  defaultEnforcerMinInterval,
		rlNow:          time.Now,
	}
}

// SetMetricsRegistry wires the Prometheus registry used to emit
// argus_alerts_rate_limited_publishes_total when a suppressed publish
// is detected. Safe to leave unset — increments become no-ops.
func (e *Enforcer) SetMetricsRegistry(reg *obsmetrics.Registry) {
	e.metricsReg = reg
}

// shouldPublishViolationAlert returns true when at least rlMinInterval
// has elapsed since the last alert publish for this (policy, sim) tuple.
// When it returns false the caller must skip the publish and log it as
// rate-limited (metric increment is the caller's responsibility).
//
// Defensive fallthrough: if policyID is uuid.Nil (a degenerate state
// possible when policyStore is nil and cache misses), all SIMs would
// collapse to a single rate-limit bucket and mask unrelated violations.
// Always allow the publish in that case — the DB dedup_key still
// collapses duplicate rows, so correctness is preserved.
func (e *Enforcer) shouldPublishViolationAlert(policyID, simID uuid.UUID) bool {
	if policyID == uuid.Nil {
		return true
	}
	key := enforcerAlertKey{PolicyID: policyID, SIMID: simID}
	now := e.rlNow()

	e.rlMu.Lock()
	defer e.rlMu.Unlock()
	if last, ok := e.rlLastEmitted[key]; ok && now.Sub(last) < e.rlMinInterval {
		return false
	}
	e.rlLastEmitted[key] = now
	return true
}

func (e *Enforcer) Evaluate(ctx context.Context, sim *store.SIM, sessionCtx dsl.SessionContext) (*EnforcementResult, error) {
	result := &EnforcementResult{
		Allow:          true,
		SessionTimeout: sim.SessionHardTimeoutSec,
		IdleTimeout:    sim.SessionIdleTimeoutSec,
		FilterID:       "default",
	}

	if sim.PolicyVersionID == nil {
		return result, nil
	}

	versionID := *sim.PolicyVersionID

	var compiled *dsl.CompiledPolicy
	var ok bool
	if e.policyCache != nil {
		compiled, ok = e.policyCache.Get(versionID)
	}
	if !ok {
		if e.policyStore != nil {
			pv, err := e.policyStore.GetVersionByID(ctx, versionID)
			if err != nil {
				e.logger.Warn().Err(err).Str("version_id", versionID.String()).Msg("failed to fetch policy version from DB")
				return result, nil
			}
			var cp dsl.CompiledPolicy
			if err := json.Unmarshal(pv.CompiledRules, &cp); err != nil {
				e.logger.Warn().Err(err).Str("version_id", versionID.String()).Msg("failed to unmarshal compiled policy")
				return result, nil
			}
			compiled = &cp
			if e.policyCache != nil {
				e.policyCache.Put(versionID, pv.PolicyID, sim.TenantID, compiled)
			}
			result.PolicyID = pv.PolicyID
		} else {
			return result, nil
		}
	}

	policyResult, err := e.evaluator.Evaluate(sessionCtx, compiled)
	if err != nil {
		e.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("policy evaluation error")
		return result, nil
	}

	result.Allow = policyResult.Allow
	result.Actions = policyResult.Actions
	result.VersionID = versionID

	if result.PolicyID == uuid.Nil {
		if pv, _ := e.policyStore.GetVersionByID(ctx, versionID); pv != nil {
			result.PolicyID = pv.PolicyID
		}
	}

	if v, ok := policyResult.QoSAttributes["session_timeout"]; ok {
		if timeout, ok := toInt(v); ok && timeout > 0 {
			result.SessionTimeout = timeout
		}
	}
	if v, ok := policyResult.QoSAttributes["idle_timeout"]; ok {
		if timeout, ok := toInt(v); ok && timeout > 0 {
			result.IdleTimeout = timeout
		}
	}
	if v, ok := policyResult.QoSAttributes["bandwidth_down"]; ok {
		if bw, ok := toInt64(v); ok {
			result.BandwidthDown = bw
		}
	}
	if v, ok := policyResult.QoSAttributes["bandwidth_up"]; ok {
		if bw, ok := toInt64(v); ok {
			result.BandwidthUp = bw
		}
	}
	if v, ok := policyResult.QoSAttributes["filter_id"]; ok {
		if s, ok := v.(string); ok && s != "" {
			result.FilterID = s
		}
	}

	for i, action := range policyResult.Actions {
		switch action.Type {
		case "block", "disconnect", "suspend":
			result.Violations = append(result.Violations, ViolationRecord{
				ViolationType: action.Type,
				ActionTaken:   action.Type,
				Severity:      "critical",
				RuleIndex:     i,
				Details:       action.Params,
			})

		case "throttle":
			result.Violations = append(result.Violations, ViolationRecord{
				ViolationType: "throttle",
				ActionTaken:   "throttle",
				Severity:      severity.Medium,
				RuleIndex:     i,
				Details:       action.Params,
			})
			if v, ok := action.Params["rate"]; ok {
				if rate, ok := toInt64(v); ok {
					result.BandwidthDown = rate
					result.BandwidthUp = rate
				}
			}

		case "notify":
			result.Violations = append(result.Violations, ViolationRecord{
				ViolationType: "policy_notify",
				ActionTaken:   "notify",
				Severity:      "info",
				RuleIndex:     i,
				Details:       action.Params,
			})

		case "log":
			result.Violations = append(result.Violations, ViolationRecord{
				ViolationType: "policy_log",
				ActionTaken:   "log",
				Severity:      "info",
				RuleIndex:     i,
				Details:       action.Params,
			})

		case "tag":
			result.Violations = append(result.Violations, ViolationRecord{
				ViolationType: "policy_tag",
				ActionTaken:   "tag",
				Severity:      "info",
				RuleIndex:     i,
				Details:       action.Params,
			})
		}
	}

	return result, nil
}

func (e *Enforcer) RecordViolations(ctx context.Context, sim *store.SIM, result *EnforcementResult, sessionID *uuid.UUID) {
	if len(result.Violations) == 0 {
		return
	}

	var operatorID *uuid.UUID
	if sim.OperatorID != uuid.Nil {
		operatorID = &sim.OperatorID
	}

	for _, v := range result.Violations {
		violation, err := e.violationStore.Create(ctx, store.CreateViolationParams{
			TenantID:      sim.TenantID,
			SimID:         sim.ID,
			PolicyID:      result.PolicyID,
			VersionID:     result.VersionID,
			RuleIndex:     v.RuleIndex,
			ViolationType: v.ViolationType,
			ActionTaken:   v.ActionTaken,
			Details:       v.Details,
			SessionID:     sessionID,
			OperatorID:    operatorID,
			APNID:         sim.APNID,
			Severity:      v.Severity,
		})
		if err != nil {
			e.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("failed to record policy violation")
			continue
		}

		if e.eventBus != nil && (v.Severity == severity.Critical || v.Severity == severity.High || v.Severity == severity.Medium) {
			// FIX-210 Task 4: rate-limit per (policy_id, sim_id). A
			// rapid-violation loop would otherwise flood the alert bus
			// on every tick; the DB dedup_key collapses the rows but
			// network and log overhead is still paid. 60s min-interval
			// is a good default — critical issues still surface
			// promptly; pathological loops are bounded.
			if !e.shouldPublishViolationAlert(result.PolicyID, sim.ID) {
				e.metricsReg.IncAlertsRateLimitedPublishes("enforcer")
				e.logger.Debug().
					Str("policy_id", result.PolicyID.String()).
					Str("sim_id", sim.ID.String()).
					Str("violation_type", v.ViolationType).
					Msg("enforcer: alert publish rate-limited (60s min-interval)")
			} else {
				_ = e.eventBus.Publish(ctx, bus.SubjectAlertTriggered, map[string]interface{}{
					"id":          violation.ID.String(),
					"tenant_id":   sim.TenantID.String(),
					"type":        "policy_violation",
					"severity":    v.Severity,
					"state":       "open",
					"message":     fmt.Sprintf("Policy violation: %s on SIM %s", v.ViolationType, sim.ICCID),
					"sim_id":      sim.ID.String(),
					"entity_type": "sim",
					"entity_id":   sim.ID.String(),
					"detected_at": time.Now().UTC().Format(time.RFC3339),
				})
			}
		}

		if v.ActionTaken == "notify" && e.eventBus != nil {
			_ = e.eventBus.Publish(ctx, bus.SubjectNotification, map[string]interface{}{
				"tenant_id":     sim.TenantID.String(),
				"type":          "policy_violation",
				"category":      "policy",
				"title":         fmt.Sprintf("Policy Violation: %s", v.ViolationType),
				"message":       fmt.Sprintf("SIM %s triggered %s action", sim.ICCID, v.ViolationType),
				"severity":      v.Severity,
				"resource_type": "sim",
				"resource_id":   sim.ID.String(),
			})
		}
	}
}

func (e *Enforcer) RecordUsageCheck(ctx context.Context, sim *store.SIM, currentUsageBytes int64) (*EnforcementResult, error) {
	sessCtx := dsl.SessionContext{
		SIMID:    sim.ID.String(),
		TenantID: sim.TenantID.String(),
		Usage:    currentUsageBytes,
		SimType:  sim.SimType,
	}
	if sim.APNID != nil {
		sessCtx.APN = sim.APNID.String()
	}

	now := time.Now()
	sessCtx.TimeOfDay = now.Format("15:04")
	sessCtx.DayOfWeek = now.Weekday().String()

	return e.Evaluate(ctx, sim, sessCtx)
}

func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	case json.Number:
		n, err := val.Int64()
		return int(n), err == nil
	}
	return 0, false
}

func toInt64(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case float64:
		return int64(val), true
	case int:
		return int64(val), true
	case int64:
		return val, true
	case json.Number:
		n, err := val.Int64()
		return n, err == nil
	}
	return 0, false
}
