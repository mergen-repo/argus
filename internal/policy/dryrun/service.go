package dryrun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	cachePrefix = "dryrun:"
	cacheTTL    = 5 * time.Minute
	sampleLimit = 10
	asyncThreshold = 100000
)

type DryRunRequest struct {
	VersionID uuid.UUID
	TenantID  uuid.UUID
	SegmentID *uuid.UUID
}

type DryRunResult struct {
	VersionID         string             `json:"version_id"`
	TotalAffected     int                `json:"total_affected"`
	ByOperator        map[string]int     `json:"by_operator"`
	ByAPN             map[string]int     `json:"by_apn"`
	ByRAT             map[string]int     `json:"by_rat"`
	BehavioralChanges []BehavioralChange `json:"behavioral_changes"`
	SampleSIMs        []SampleSIM        `json:"sample_sims"`
	EvaluatedAt       string             `json:"evaluated_at"`
}

type BehavioralChange struct {
	Type          string      `json:"type"`
	Description   string      `json:"description"`
	AffectedCount int         `json:"affected_count"`
	Field         string      `json:"field"`
	OldValue      interface{} `json:"old_value"`
	NewValue      interface{} `json:"new_value"`
}

type SampleSIM struct {
	SimID    string           `json:"sim_id"`
	ICCID    string           `json:"iccid"`
	Operator string           `json:"operator"`
	APN      string           `json:"apn"`
	RATType  string           `json:"rat_type"`
	Before   *dsl.PolicyResult `json:"before"`
	After    *dsl.PolicyResult `json:"after"`
}

type DryRunFilters struct {
	OperatorIDs []uuid.UUID
	APNNames    []string
	RATTypes    []string
	SegmentID   *uuid.UUID
}

type Service struct {
	policyStore *store.PolicyStore
	simStore    *store.SIMStore
	db          *pgxpool.Pool
	cache       *redis.Client
	logger      zerolog.Logger
}

func NewService(
	policyStore *store.PolicyStore,
	simStore *store.SIMStore,
	db *pgxpool.Pool,
	cache *redis.Client,
	logger zerolog.Logger,
) *Service {
	return &Service{
		policyStore: policyStore,
		simStore:    simStore,
		db:          db,
		cache:       cache,
		logger:      logger.With().Str("component", "dryrun_service").Logger(),
	}
}

func (s *Service) cacheKey(versionID uuid.UUID, segmentID *uuid.UUID) string {
	seg := "all"
	if segmentID != nil {
		seg = segmentID.String()
	}
	return cachePrefix + versionID.String() + ":" + seg
}

func (s *Service) dslCacheKey(versionID uuid.UUID, segmentID *uuid.UUID, dslContent string) string {
	h := sha256.Sum256([]byte(dslContent))
	seg := "all"
	if segmentID != nil {
		seg = segmentID.String()
	}
	return cachePrefix + versionID.String() + ":" + seg + ":" + hex.EncodeToString(h[:8])
}

func (s *Service) Execute(ctx context.Context, req DryRunRequest) (*DryRunResult, error) {
	version, err := s.policyStore.GetVersionWithTenant(ctx, req.VersionID, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}

	if s.cache != nil {
		key := s.dslCacheKey(req.VersionID, req.SegmentID, version.DSLContent)
		cached, err := s.cache.Get(ctx, key).Bytes()
		if err == nil && len(cached) > 0 {
			var result DryRunResult
			if json.Unmarshal(cached, &result) == nil {
				return &result, nil
			}
		}
	}

	compiled, dslErrors, compileErr := dsl.CompileSource(version.DSLContent)
	if compileErr != nil {
		return nil, fmt.Errorf("compile DSL: %w", compileErr)
	}
	for _, e := range dslErrors {
		if e.Severity == "error" {
			return nil, &DSLError{Message: fmt.Sprintf("DSL error at line %d: %s", e.Line, e.Message)}
		}
	}

	filters := s.buildFiltersFromMatch(compiled)
	filters.SegmentID = req.SegmentID

	storeFilters := store.SIMFleetFilters{
		OperatorIDs: filters.OperatorIDs,
		RATTypes:    filters.RATTypes,
		SegmentID:   filters.SegmentID,
	}

	if len(filters.APNNames) > 0 {
		apnIDs, apnErr := s.resolveAPNNamesByTenant(ctx, req.TenantID, filters.APNNames)
		if apnErr != nil {
			s.logger.Warn().Err(apnErr).Msg("resolve APN names")
		}
		storeFilters.APNIDs = apnIDs
	}

	totalAffected, err := s.simStore.CountByFilters(ctx, req.TenantID, storeFilters)
	if err != nil {
		return nil, fmt.Errorf("count sims: %w", err)
	}

	byOperator, err := s.simStore.AggregateByOperator(ctx, req.TenantID, storeFilters)
	if err != nil {
		return nil, fmt.Errorf("aggregate by operator: %w", err)
	}

	byAPN, err := s.simStore.AggregateByAPN(ctx, req.TenantID, storeFilters)
	if err != nil {
		return nil, fmt.Errorf("aggregate by apn: %w", err)
	}

	byRAT, err := s.simStore.AggregateByRATType(ctx, req.TenantID, storeFilters)
	if err != nil {
		return nil, fmt.Errorf("aggregate by rat: %w", err)
	}

	operatorMap := make(map[string]int, len(byOperator))
	for _, oc := range byOperator {
		operatorMap[oc.Name] = oc.Count
	}

	apnMap := make(map[string]int, len(byAPN))
	for _, ac := range byAPN {
		apnMap[ac.Name] = ac.Count
	}

	ratMap := make(map[string]int, len(byRAT))
	for _, rc := range byRAT {
		ratMap[rc.Name] = rc.Count
	}

	sampleSIMs, err := s.simStore.FetchSample(ctx, req.TenantID, storeFilters, sampleLimit)
	if err != nil {
		return nil, fmt.Errorf("fetch sample sims: %w", err)
	}

	samples, behavioralChanges := s.evaluateSamples(ctx, compiled, sampleSIMs, totalAffected)

	result := &DryRunResult{
		VersionID:         req.VersionID.String(),
		TotalAffected:     totalAffected,
		ByOperator:        operatorMap,
		ByAPN:             apnMap,
		ByRAT:             ratMap,
		BehavioralChanges: behavioralChanges,
		SampleSIMs:        samples,
		EvaluatedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	resultJSON, err := json.Marshal(result)
	if err == nil {
		_ = s.policyStore.UpdateDryRunResult(ctx, req.VersionID, resultJSON, totalAffected)

		if s.cache != nil {
			key := s.dslCacheKey(req.VersionID, req.SegmentID, version.DSLContent)
			_ = s.cache.Set(ctx, key, resultJSON, cacheTTL).Err()
		}
	}

	return result, nil
}

func (s *Service) CountMatchingSIMs(ctx context.Context, tenantID uuid.UUID, versionID uuid.UUID, segmentID *uuid.UUID) (int, error) {
	version, err := s.policyStore.GetVersionWithTenant(ctx, versionID, tenantID)
	if err != nil {
		return 0, fmt.Errorf("get version: %w", err)
	}

	compiled, _, compileErr := dsl.CompileSource(version.DSLContent)
	if compileErr != nil {
		return 0, fmt.Errorf("compile DSL: %w", compileErr)
	}

	filters := s.buildFiltersFromMatch(compiled)
	storeFilters := store.SIMFleetFilters{
		OperatorIDs: filters.OperatorIDs,
		RATTypes:    filters.RATTypes,
		SegmentID:   segmentID,
	}

	if len(filters.APNNames) > 0 {
		apnIDs, apnErr := s.resolveAPNNamesByTenant(ctx, tenantID, filters.APNNames)
		if apnErr == nil {
			storeFilters.APNIDs = apnIDs
		}
	}

	return s.simStore.CountByFilters(ctx, tenantID, storeFilters)
}

func (s *Service) buildFiltersFromMatch(compiled *dsl.CompiledPolicy) DryRunFilters {
	var filters DryRunFilters

	if compiled == nil {
		return filters
	}

	for _, cond := range compiled.Match.Conditions {
		switch cond.Field {
		case "operator":
			filters.OperatorIDs = extractUUIDs(cond)
		case "apn":
			filters.APNNames = extractStrings(cond)
		case "rat_type":
			filters.RATTypes = extractStrings(cond)
		}
	}

	return filters
}

func extractUUIDs(cond dsl.CompiledMatchCondition) []uuid.UUID {
	var ids []uuid.UUID
	values := cond.Values
	if cond.Value != nil {
		values = append(values, cond.Value)
	}
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		id, err := uuid.Parse(s)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func extractStrings(cond dsl.CompiledMatchCondition) []string {
	var strs []string
	values := cond.Values
	if cond.Value != nil {
		values = append(values, cond.Value)
	}
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			continue
		}
		strs = append(strs, s)
	}
	return strs
}

func (s *Service) resolveAPNNamesByTenant(ctx context.Context, tenantID uuid.UUID, names []string) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}

	args := []interface{}{tenantID}
	placeholders := make([]string, len(names))
	for i, name := range names {
		args = append(args, name)
		placeholders[i] = fmt.Sprintf("$%d", i+2)
	}

	query := fmt.Sprintf(
		`SELECT id FROM apns WHERE tenant_id = $1 AND name IN (%s)`,
		strings.Join(placeholders, ", "),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve apn names: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan apn id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) evaluateSamples(ctx context.Context, compiled *dsl.CompiledPolicy, sims []store.SIM, totalAffected int) ([]SampleSIM, []BehavioralChange) {
	evaluator := dsl.NewEvaluator()
	var samples []SampleSIM
	changeAggregator := make(map[string]*BehavioralChange)

	for _, sim := range sims {
		sessCtx := buildSessionContext(sim)

		afterResult, err := evaluator.Evaluate(sessCtx, compiled)
		if err != nil {
			s.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("evaluate after policy")
			continue
		}

		var beforeResult *dsl.PolicyResult
		if sim.PolicyVersionID != nil {
			beforeResult = s.evaluateExistingPolicy(ctx, *sim.PolicyVersionID, sessCtx)
		}

		operatorName := s.resolveOperatorName(ctx, sim.OperatorID)
		apnName := s.resolveAPNName(ctx, sim.APNID)
		ratType := ""
		if sim.RATType != nil {
			ratType = *sim.RATType
		}

		samples = append(samples, SampleSIM{
			SimID:    sim.ID.String(),
			ICCID:    sim.ICCID,
			Operator: operatorName,
			APN:      apnName,
			RATType:  ratType,
			Before:   beforeResult,
			After:    afterResult,
		})

		if beforeResult != nil {
			changes := DetectBehavioralChanges(beforeResult, afterResult)
			for i := range changes {
				key := changes[i].Type + ":" + changes[i].Field
				if existing, ok := changeAggregator[key]; ok {
					existing.AffectedCount++
				} else {
					ch := changes[i]
					ch.AffectedCount = 1
					changeAggregator[key] = &ch
				}
			}
		}
	}

	var behavioralChanges []BehavioralChange
	for _, ch := range changeAggregator {
		if totalAffected > 0 && len(sims) > 0 {
			ch.AffectedCount = int(float64(ch.AffectedCount) / float64(len(sims)) * float64(totalAffected))
		}
		behavioralChanges = append(behavioralChanges, *ch)
	}

	return samples, behavioralChanges
}

func buildSessionContext(sim store.SIM) dsl.SessionContext {
	ratType := ""
	if sim.RATType != nil {
		ratType = *sim.RATType
	}

	metadata := make(map[string]string)
	if sim.Metadata != nil && len(sim.Metadata) > 0 {
		_ = json.Unmarshal(sim.Metadata, &metadata)
	}

	return dsl.SessionContext{
		SIMID:    sim.ID.String(),
		TenantID: sim.TenantID.String(),
		Operator: sim.OperatorID.String(),
		RATType:  ratType,
		SimType:  sim.SimType,
		Metadata: metadata,
	}
}

func (s *Service) evaluateExistingPolicy(ctx context.Context, versionID uuid.UUID, sessCtx dsl.SessionContext) *dsl.PolicyResult {
	version, err := s.policyStore.GetVersionByID(ctx, versionID)
	if err != nil {
		return nil
	}

	compiled, _, compileErr := dsl.CompileSource(version.DSLContent)
	if compileErr != nil {
		return nil
	}

	evaluator := dsl.NewEvaluator()
	result, err := evaluator.Evaluate(sessCtx, compiled)
	if err != nil {
		return nil
	}
	return result
}

func (s *Service) resolveOperatorName(ctx context.Context, operatorID uuid.UUID) string {
	var name string
	err := s.db.QueryRow(ctx, `SELECT name FROM operators WHERE id = $1`, operatorID).Scan(&name)
	if err != nil {
		return operatorID.String()
	}
	return name
}

func (s *Service) resolveAPNName(ctx context.Context, apnID *uuid.UUID) string {
	if apnID == nil {
		return ""
	}
	var name string
	err := s.db.QueryRow(ctx, `SELECT name FROM apns WHERE id = $1`, *apnID).Scan(&name)
	if err != nil {
		return apnID.String()
	}
	return name
}

func DetectBehavioralChanges(before, after *dsl.PolicyResult) []BehavioralChange {
	var changes []BehavioralChange

	if before == nil || after == nil {
		return changes
	}

	for key, newVal := range after.QoSAttributes {
		oldVal, existed := before.QoSAttributes[key]
		if !existed {
			changes = append(changes, BehavioralChange{
				Type:        "qos_added",
				Description: fmt.Sprintf("%s set to %v", key, newVal),
				Field:       key,
				OldValue:    nil,
				NewValue:    newVal,
			})
			continue
		}

		if fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
			changeType := classifyQoSChange(key, oldVal, newVal)
			changes = append(changes, BehavioralChange{
				Type:        changeType,
				Description: fmt.Sprintf("%s changed from %v to %v", key, oldVal, newVal),
				Field:       key,
				OldValue:    oldVal,
				NewValue:    newVal,
			})
		}
	}

	for key, oldVal := range before.QoSAttributes {
		if _, exists := after.QoSAttributes[key]; !exists {
			changes = append(changes, BehavioralChange{
				Type:        "qos_removed",
				Description: fmt.Sprintf("%s removed (was %v)", key, oldVal),
				Field:       key,
				OldValue:    oldVal,
				NewValue:    nil,
			})
		}
	}

	if before.ChargingParams != nil && after.ChargingParams != nil {
		chargingChanges := detectChargingChanges(before.ChargingParams, after.ChargingParams)
		changes = append(changes, chargingChanges...)
	} else if before.ChargingParams == nil && after.ChargingParams != nil {
		changes = append(changes, BehavioralChange{
			Type:        "charging_added",
			Description: fmt.Sprintf("charging model %s added", after.ChargingParams.Model),
			Field:       "charging",
			NewValue:    after.ChargingParams.Model,
		})
	} else if before.ChargingParams != nil && after.ChargingParams == nil {
		changes = append(changes, BehavioralChange{
			Type:        "charging_removed",
			Description: fmt.Sprintf("charging model %s removed", before.ChargingParams.Model),
			Field:       "charging",
			OldValue:    before.ChargingParams.Model,
		})
	}

	if before.Allow != after.Allow {
		if after.Allow {
			changes = append(changes, BehavioralChange{
				Type:        "access_granted",
				Description: "access changed from denied to allowed",
				Field:       "allow",
				OldValue:    false,
				NewValue:    true,
			})
		} else {
			changes = append(changes, BehavioralChange{
				Type:        "access_denied",
				Description: "access changed from allowed to denied",
				Field:       "allow",
				OldValue:    true,
				NewValue:    false,
			})
		}
	}

	return changes
}

func classifyQoSChange(field string, oldVal, newVal interface{}) string {
	oldF := toFloat(oldVal)
	newF := toFloat(newVal)

	switch {
	case strings.Contains(field, "bandwidth") || strings.Contains(field, "max_sessions") || strings.Contains(field, "priority"):
		if newF > oldF {
			return "qos_upgrade"
		}
		return "qos_downgrade"
	case strings.Contains(field, "timeout") || strings.Contains(field, "limit"):
		if newF < oldF {
			return "qos_downgrade"
		}
		return "qos_upgrade"
	default:
		return "qos_change"
	}
}

func detectChargingChanges(before, after *dsl.ChargingResult) []BehavioralChange {
	var changes []BehavioralChange

	if before.Model != after.Model {
		changes = append(changes, BehavioralChange{
			Type:        "charging_change",
			Description: fmt.Sprintf("charging model changed from %s to %s", before.Model, after.Model),
			Field:       "charging_model",
			OldValue:    before.Model,
			NewValue:    after.Model,
		})
	}

	if before.RatePerMB != after.RatePerMB {
		changeType := "charging_change"
		if after.RatePerMB > before.RatePerMB {
			changeType = "rate_increase"
		} else {
			changeType = "rate_decrease"
		}
		changes = append(changes, BehavioralChange{
			Type:        changeType,
			Description: fmt.Sprintf("rate_per_mb changed from %.4f to %.4f", before.RatePerMB, after.RatePerMB),
			Field:       "rate_per_mb",
			OldValue:    before.RatePerMB,
			NewValue:    after.RatePerMB,
		})
	}

	if before.Quota != after.Quota {
		changes = append(changes, BehavioralChange{
			Type:        "quota_change",
			Description: fmt.Sprintf("quota changed from %d to %d", before.Quota, after.Quota),
			Field:       "quota",
			OldValue:    before.Quota,
			NewValue:    after.Quota,
		})
	}

	return changes
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case json.Number:
		f, _ := val.Float64()
		return f
	default:
		return 0
	}
}

type DSLError struct {
	Message string
}

func (e *DSLError) Error() string {
	return e.Message
}

func IsDSLError(err error) bool {
	_, ok := err.(*DSLError)
	return ok
}

func AsyncThreshold() int {
	return asyncThreshold
}
