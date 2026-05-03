package dsl

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// IMEIPoolLookuper is the minimal contract the evaluator needs from the IMEI
// pool store. Defined here so the dsl package does NOT import internal/store
// (avoids an import cycle and keeps the evaluator's surface area narrow —
// matches the same pattern already used for the tenant/sim narrow interfaces).
//
// Implementations MUST return (false, nil) for an empty/short imei rather
// than an error, mirroring store.IMEIPoolStore.LookupKind's behaviour.
type IMEIPoolLookuper interface {
	LookupKind(ctx context.Context, tenantID uuid.UUID, imei string, pool string) (bool, error)
}

type SessionContext struct {
	SIMID           string            `json:"sim_id"`
	TenantID        string            `json:"tenant_id"`
	Operator        string            `json:"operator"`
	APN             string            `json:"apn"`
	RATType         string            `json:"rat_type"`
	Usage           int64             `json:"usage"`
	TimeOfDay       string            `json:"time_of_day"`
	DayOfWeek       string            `json:"day_of_week"`
	SessionCount    int               `json:"session_count"`
	BandwidthUsed   int64             `json:"bandwidth_used"`
	SessionDuration int64             `json:"session_duration"`
	Metadata        map[string]string `json:"metadata"`
	SimType         string            `json:"sim_type"`
	// Phase 11 STORY-093 — IMEI capture (flat fields, zero-value safe)
	IMEI            string `json:"imei,omitempty"`
	SoftwareVersion string `json:"software_version,omitempty"`
	// Phase 11 STORY-094 — IMEI/SIM binding fields (flat, zero-value safe).
	// Hot-path AAA WILL NOT populate BindingStatus / BindingVerifiedAt in
	// this story (no enforcement); they remain zero-valued so dry-run
	// policies that reference them evaluate to "" against synthetic ctx.
	BindingMode       string `json:"binding_mode,omitempty"`
	BoundIMEI         string `json:"bound_imei,omitempty"`
	BindingStatus     string `json:"binding_status,omitempty"`
	BindingVerifiedAt string `json:"binding_verified_at,omitempty"` // RFC3339 string for DSL string compare

	// Phase 11 STORY-095 Task 6 — per-evaluation-pass cache for the
	// device.imei_in_pool() DSL predicate. Maps `<pool>:<imei>` → bool.
	// SessionContext is passed by value through evaluation; map values are
	// reference types so the same backing store is shared across copies,
	// giving us a free per-pass cache without threading a separate state
	// struct. Reset on every entry to Evaluate(). Excluded from JSON so
	// it never leaks into wire payloads.
	poolCache map[string]bool `json:"-"`
	// runtimeCtx carries the request-scoped context.Context into the
	// evaluator so the IMEI pool lookup honors deadlines/cancellation.
	// nil-safe: when missing we fall back to context.Background(); the
	// store query is a single indexed EXISTS — fast enough that a missing
	// deadline does not jeopardise the AAA hot path.
	runtimeCtx context.Context `json:"-"`
}

// WithContext returns a copy of the SessionContext with the supplied
// context.Context attached. The context is consulted by the IMEI pool
// lookup so callers in request-scoped paths (handlers, dryrun, enforcer)
// can propagate deadlines/cancellation.
func (c SessionContext) WithContext(ctx context.Context) SessionContext {
	c.runtimeCtx = ctx
	return c
}

type PolicyResult struct {
	Allow          bool                   `json:"allow"`
	QoSAttributes  map[string]interface{} `json:"qos_attributes"`
	ChargingParams *ChargingResult        `json:"charging_params,omitempty"`
	Actions        []ActionResult         `json:"actions"`
	MatchedRules   int                    `json:"matched_rules"`
}

type ChargingResult struct {
	Model            string  `json:"model"`
	RatePerMB        float64 `json:"rate_per_mb"`
	RatePerSession   float64 `json:"rate_per_session,omitempty"`
	BillingCycle     string  `json:"billing_cycle"`
	Quota            int64   `json:"quota"`
	OverageAction    string  `json:"overage_action,omitempty"`
	OverageRatePerMB float64 `json:"overage_rate_per_mb,omitempty"`
	RATMultiplier    float64 `json:"rat_multiplier"`
}

type ActionResult struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params,omitempty"`
}

type Evaluator struct {
	// pools, when non-nil, satisfies device.imei_in_pool() predicates
	// against the live pool store. nil keeps the placeholder behaviour
	// (always false) so call sites that haven't been migrated yet — and
	// the synthetic-context tests — keep working unchanged.
	pools IMEIPoolLookuper
}

func NewEvaluator() *Evaluator {
	return &Evaluator{}
}

// NewEvaluatorWithPools returns an Evaluator wired to a live IMEI pool
// lookup. Call sites in production hot paths (RADIUS/Diameter enforcer,
// dryrun, simulator) MUST use this constructor; tests and synthetic
// contexts can keep using NewEvaluator() to preserve the placeholder
// behaviour.
func NewEvaluatorWithPools(pools IMEIPoolLookuper) *Evaluator {
	return &Evaluator{pools: pools}
}

// WithIMEIPoolLookuper installs (or replaces) the pool lookup on an
// existing Evaluator. Provided for call sites that construct the
// evaluator lazily and want to layer in the lookup post-hoc without
// changing the constructor signature.
func (e *Evaluator) WithIMEIPoolLookuper(pools IMEIPoolLookuper) *Evaluator {
	e.pools = pools
	return e
}

func (e *Evaluator) Evaluate(ctx SessionContext, compiled *CompiledPolicy) (*PolicyResult, error) {
	if compiled == nil {
		return nil, fmt.Errorf("nil compiled policy")
	}

	// Reset the per-pass IMEI pool cache. Map values are reference types,
	// so the freshly-allocated map propagates through the value-typed
	// SessionContext copies that the evaluator threads through eval()
	// without us needing to convert ctx to a pointer everywhere.
	ctx.poolCache = map[string]bool{}

	if !e.matchesPolicy(ctx, compiled) {
		return &PolicyResult{
			Allow:         true,
			QoSAttributes: make(map[string]interface{}),
			Actions:       []ActionResult{},
			MatchedRules:  0,
		}, nil
	}

	result := e.evaluateRules(ctx, &compiled.Rules)

	if compiled.Charging != nil {
		result.ChargingParams = e.evaluateCharging(ctx, compiled.Charging)
	}

	return result, nil
}

func (e *Evaluator) matchesPolicy(ctx SessionContext, compiled *CompiledPolicy) bool {
	for _, cond := range compiled.Match.Conditions {
		if !e.evaluateMatchCondition(ctx, cond) {
			return false
		}
	}
	return true
}

func (e *Evaluator) evaluateMatchCondition(ctx SessionContext, cond CompiledMatchCondition) bool {
	fieldVal := e.getMatchFieldValue(ctx, cond.Field)

	switch cond.Op {
	case "in":
		for _, v := range cond.Values {
			if matchValues(fieldVal, v) {
				return true
			}
		}
		return false
	case "eq":
		return matchValues(fieldVal, cond.Value)
	case "neq":
		return !matchValues(fieldVal, cond.Value)
	default:
		return false
	}
}

func (e *Evaluator) getMatchFieldValue(ctx SessionContext, field string) interface{} {
	switch field {
	case "apn":
		return ctx.APN
	case "operator":
		return ctx.Operator
	case "rat_type":
		return ctx.RATType
	case "sim_type":
		return ctx.SimType
	default:
		if strings.HasPrefix(field, "metadata.") {
			key := strings.TrimPrefix(field, "metadata.")
			if ctx.Metadata != nil {
				return ctx.Metadata[key]
			}
			return ""
		}
		return nil
	}
}

func (e *Evaluator) evaluateRules(ctx SessionContext, rules *CompiledRules) *PolicyResult {
	result := &PolicyResult{
		Allow:         true,
		QoSAttributes: make(map[string]interface{}),
		Actions:       []ActionResult{},
	}

	for k, v := range rules.Defaults {
		result.QoSAttributes[k] = v
	}

	for _, wb := range rules.WhenBlocks {
		if e.evaluateCondition(ctx, wb.Condition) {
			result.MatchedRules++
			for k, v := range wb.Assignments {
				result.QoSAttributes[k] = v
			}
			for _, action := range wb.Actions {
				result.Actions = append(result.Actions, ActionResult{
					Type:   action.Type,
					Params: action.Params,
				})
			}
		}
	}

	for _, action := range result.Actions {
		if action.Type == "block" || action.Type == "disconnect" || action.Type == "suspend" {
			result.Allow = false
			break
		}
	}

	return result
}

func (e *Evaluator) evaluateCondition(ctx SessionContext, cond *CompiledCondition) bool {
	if cond == nil {
		return true
	}

	switch cond.Op {
	case "and":
		return e.evaluateCondition(ctx, cond.Left) && e.evaluateCondition(ctx, cond.Right)
	case "or":
		return e.evaluateCondition(ctx, cond.Left) || e.evaluateCondition(ctx, cond.Right)
	case "not":
		return !e.evaluateCondition(ctx, cond.Inner)
	default:
		return e.evaluateSimpleCondition(ctx, cond)
	}
}

func (e *Evaluator) evaluateSimpleCondition(ctx SessionContext, cond *CompiledCondition) bool {
	fieldVal := e.getConditionFieldValue(ctx, cond.Field)

	switch cond.Op {
	case "gt":
		return compareNumeric(fieldVal, cond.Value) > 0
	case "gte":
		return compareNumeric(fieldVal, cond.Value) >= 0
	case "lt":
		return compareNumeric(fieldVal, cond.Value) < 0
	case "lte":
		return compareNumeric(fieldVal, cond.Value) <= 0
	case "eq":
		return matchValues(fieldVal, cond.Value)
	case "neq":
		return !matchValues(fieldVal, cond.Value)
	case "in":
		if cond.Field == "time_of_day" {
			timeStr, ok := fieldVal.(string)
			if !ok {
				return false
			}
			for _, v := range cond.Values {
				rangeStr, ok := v.(string)
				if !ok {
					continue
				}
				if isTimeInRange(timeStr, rangeStr) {
					return true
				}
			}
			return false
		}
		for _, v := range cond.Values {
			if matchValues(fieldVal, v) {
				return true
			}
		}
		return false
	case "between":
		if cond.Value != nil && len(cond.Values) > 0 {
			lower := cond.Value
			upper := cond.Values[0]
			return compareNumeric(fieldVal, lower) >= 0 && compareNumeric(fieldVal, upper) <= 0
		}
		return false
	default:
		return false
	}
}

func (e *Evaluator) getConditionFieldValue(ctx SessionContext, field string) interface{} {
	// STORY-094 — function-call dispatch: tac(<inner>), device.imei_in_pool(<pool>).
	// Function-call Field strings are encoded by the parser as "<funcname>(<arg>)".
	if strings.HasPrefix(field, "tac(") && strings.HasSuffix(field, ")") {
		inner := field[len("tac(") : len(field)-1]
		innerVal := e.getConditionFieldValue(ctx, inner)
		innerStr, _ := innerVal.(string)
		return tac(innerStr)
	}
	if strings.HasPrefix(field, "device.imei_in_pool(") && strings.HasSuffix(field, ")") {
		return e.evalIMEIInPool(ctx, field)
	}

	switch field {
	case "usage":
		return ctx.Usage
	case "time_of_day":
		return ctx.TimeOfDay
	case "rat_type":
		return ctx.RATType
	case "apn":
		return ctx.APN
	case "operator":
		return ctx.Operator
	case "session_count":
		return int64(ctx.SessionCount)
	case "bandwidth_used":
		return ctx.BandwidthUsed
	case "session_duration":
		return ctx.SessionDuration
	case "day_of_week":
		return ctx.DayOfWeek
	case "sim_type":
		return ctx.SimType
	// STORY-094 — device & SIM binding fields
	case "device.imei":
		return ctx.IMEI
	case "device.imeisv":
		if ctx.IMEI != "" && ctx.SoftwareVersion != "" {
			return ctx.IMEI + ctx.SoftwareVersion
		}
		return ""
	case "device.software_version":
		return ctx.SoftwareVersion
	case "device.tac":
		return tac(ctx.IMEI)
	case "device.binding_status":
		return ctx.BindingStatus
	case "sim.binding_mode":
		return ctx.BindingMode
	case "sim.bound_imei":
		return ctx.BoundIMEI
	case "sim.binding_verified_at":
		return ctx.BindingVerifiedAt
	default:
		if strings.HasPrefix(field, "metadata.") {
			key := strings.TrimPrefix(field, "metadata.")
			if ctx.Metadata != nil {
				return ctx.Metadata[key]
			}
			return ""
		}
		return nil
	}
}

// tac returns the TAC (first 8 digits) of a 15-digit IMEI, or "" if not
// 15 chars long. Defensive: parsers already validate digits, but we
// re-check len here so a malformed runtime ctx never produces a partial
// TAC.
func tac(imei string) string {
	if len(imei) != 15 {
		return ""
	}
	return imei[:8]
}

// evalIMEIInPool resolves device.imei_in_pool(<pool>) by consulting the
// per-pass cache first, then the wired IMEIPoolLookuper. Returns false
// for any condition that prevents a deterministic answer:
//   - empty/non-15-digit IMEI in the session context
//   - empty / unparseable tenant_id
//   - lookup not wired (pools == nil) — preserves placeholder semantics
//   - lookup error — fail-open per STORY-095 plan §DSL (STORY-096 owns
//     enforcement default-deny posture; the policy evaluator stays
//     side-effect-free on infrastructure errors).
//
// Cache key is `<pool>:<imei>`. The cache is owned by SessionContext
// (per-pass / per-tenant safety — one tenant's lookup never leaks to
// another evaluation pass) and is reset at the top of Evaluate().
func (e *Evaluator) evalIMEIInPool(ctx SessionContext, field string) bool {
	pool := strings.TrimSpace(field[len("device.imei_in_pool(") : len(field)-1])
	// Defensive: the parser already strips quotes, but if a future caller
	// hands us `device.imei_in_pool("blacklist")` verbatim, normalise.
	pool = strings.Trim(pool, `"'`)
	if pool == "" {
		return false
	}
	if len(ctx.IMEI) != 15 {
		return false
	}

	cacheKey := pool + ":" + ctx.IMEI
	if cached, ok := ctx.poolCache[cacheKey]; ok {
		return cached
	}

	if e.pools == nil {
		if ctx.poolCache != nil {
			ctx.poolCache[cacheKey] = false
		}
		return false
	}

	tenantID, err := uuid.Parse(ctx.TenantID)
	if err != nil {
		if ctx.poolCache != nil {
			ctx.poolCache[cacheKey] = false
		}
		return false
	}

	lookupCtx := ctx.runtimeCtx
	if lookupCtx == nil {
		lookupCtx = context.Background()
	}

	hit, err := e.pools.LookupKind(lookupCtx, tenantID, ctx.IMEI, pool)
	if err != nil {
		// Fail-open at the evaluator boundary; STORY-096 enforcer policy
		// decides whether infrastructure errors translate into deny.
		if ctx.poolCache != nil {
			ctx.poolCache[cacheKey] = false
		}
		return false
	}
	if ctx.poolCache != nil {
		ctx.poolCache[cacheKey] = hit
	}
	return hit
}

func (e *Evaluator) evaluateCharging(ctx SessionContext, ch *CompiledCharging) *ChargingResult {
	result := &ChargingResult{
		Model:            ch.Model,
		RatePerMB:        ch.RatePerMB,
		RatePerSession:   ch.RatePerSession,
		BillingCycle:     ch.BillingCycle,
		Quota:            ch.Quota,
		OverageAction:    ch.OverageAction,
		OverageRatePerMB: ch.OverageRatePerMB,
		RATMultiplier:    1.0,
	}

	if ch.RATMultiplier != nil {
		if mult, ok := ch.RATMultiplier[ctx.RATType]; ok {
			result.RATMultiplier = mult
		}
	}

	return result
}

func matchValues(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}

	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return strings.EqualFold(aStr, bStr)
}

func compareNumeric(a, b interface{}) int {
	aNum := toFloat64(a)
	bNum := toFloat64(b)

	if aNum < bNum {
		return -1
	}
	if aNum > bNum {
		return 1
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

func isTimeInRange(timeStr, rangeStr string) bool {
	parts := strings.SplitN(rangeStr, "-", 2)
	if len(parts) != 2 {
		return timeStr == rangeStr
	}
	start := strings.TrimSpace(parts[0])
	end := strings.TrimSpace(parts[1])

	if start <= end {
		return timeStr >= start && timeStr <= end
	}
	return timeStr >= start || timeStr <= end
}
