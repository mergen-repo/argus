package enforcer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	policycache "github.com/btopcu/argus/internal/policy/cache"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

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
	}
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

	compiled, ok := e.policyCache.Get(versionID)
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
			e.policyCache.Put(versionID, pv.PolicyID, sim.TenantID, compiled)
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
				Severity:      "warning",
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

		if e.eventBus != nil && (v.Severity == "critical" || v.Severity == "warning") {
			_ = e.eventBus.Publish(ctx, bus.SubjectAlertTriggered, map[string]interface{}{
				"id":            violation.ID.String(),
				"type":          "policy_violation",
				"severity":      v.Severity,
				"state":         "open",
				"message":       fmt.Sprintf("Policy violation: %s on SIM %s", v.ViolationType, sim.ICCID),
				"sim_id":        sim.ID.String(),
				"entity_type":   "sim",
				"entity_id":     sim.ID.String(),
				"detected_at":   time.Now().UTC().Format(time.RFC3339),
			})
		}

		if v.ActionTaken == "notify" && e.eventBus != nil {
			_ = e.eventBus.Publish(ctx, bus.SubjectNotification, map[string]interface{}{
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
