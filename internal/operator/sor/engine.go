package sor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/operator"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type CircuitBreakerChecker interface {
	GetCircuitBreaker(opID uuid.UUID) *operator.CircuitBreaker
}

type GrantProvider interface {
	ListGrantsWithOperators(ctx context.Context, tenantID uuid.UUID) ([]store.GrantWithOperator, error)
}

type Engine struct {
	grantProvider     GrantProvider
	cache             *SoRCache
	cbCheck           CircuitBreakerChecker
	logger            zerolog.Logger
	config            SoRConfig
}

func NewEngine(
	grantProvider GrantProvider,
	cache *SoRCache,
	cbCheck CircuitBreakerChecker,
	logger zerolog.Logger,
	config SoRConfig,
) *Engine {
	if len(config.RATPreferenceOrder) == 0 {
		config.RATPreferenceOrder = DefaultRATPreferenceOrder
	}
	if config.CacheTTL <= 0 {
		config.CacheTTL = time.Hour
	}
	return &Engine{
		grantProvider: grantProvider,
		cache:         cache,
		cbCheck:       cbCheck,
		logger:        logger.With().Str("component", "sor_engine").Logger(),
		config:        config,
	}
}

func (e *Engine) Evaluate(ctx context.Context, req SoRRequest) (*SoRDecision, error) {
	if lockedOp, ok := e.checkOperatorLock(req); ok {
		e.logger.Debug().
			Str("imsi", req.IMSI).
			Str("locked_operator", lockedOp.String()).
			Msg("SoR bypassed: manual operator lock")
		return &SoRDecision{
			PrimaryOperatorID:  lockedOp,
			FallbackOperatorIDs: nil,
			Reason:             ReasonManualLock,
			EvaluatedAt:        time.Now(),
			Cached:             false,
		}, nil
	}

	if e.cache != nil {
		cached, err := e.cache.Get(ctx, req.TenantID, req.IMSI)
		if err != nil {
			e.logger.Warn().Err(err).Str("imsi", req.IMSI).Msg("SoR cache get error")
		}
		if cached != nil {
			e.logger.Debug().Str("imsi", req.IMSI).Msg("SoR cache hit")
			return cached, nil
		}
	}

	grants, err := e.grantProvider.ListGrantsWithOperators(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("sor: list grants: %w", err)
	}

	if len(grants) == 0 {
		return nil, fmt.Errorf("sor: no operator grants available for tenant %s", req.TenantID)
	}

	candidates := e.buildCandidates(grants)
	candidates = e.filterByCircuitBreaker(candidates)

	if len(candidates) == 0 {
		return nil, fmt.Errorf("sor: all operators have open circuit breakers for tenant %s", req.TenantID)
	}

	now := time.Now()

	imsiMatched := e.filterByIMSIPrefix(candidates, req.IMSI)

	var working []CandidateOperator
	var reason string
	if len(imsiMatched) > 0 {
		working = imsiMatched
		reason = ReasonIMSIPrefixMatch
	} else {
		working = candidates
		reason = ReasonDefault
	}

	if req.RequestedRAT != "" {
		ratFiltered := e.filterByRAT(working, req.RequestedRAT)
		if len(ratFiltered) > 0 {
			working = ratFiltered
			if reason == ReasonDefault {
				reason = ReasonRATPreference
			}
		}
	}

	e.sortCandidates(working)

	if len(working) > 1 && working[0].SoRPriority == working[1].SoRPriority && working[0].CostPerMB < working[1].CostPerMB {
		reason = ReasonCostOptimized
	}

	decision := &SoRDecision{
		PrimaryOperatorID: working[0].OperatorID,
		Reason:            reason,
		RATType:           req.RequestedRAT,
		CostPerMB:         working[0].CostPerMB,
		EvaluatedAt:       now,
		Cached:            false,
	}

	if len(imsiMatched) > 0 {
		decision.IMSIPrefix = working[0].MCC + working[0].MNC
	}

	if len(working) > 1 {
		fallbacks := make([]uuid.UUID, 0, len(working)-1)
		for _, c := range working[1:] {
			fallbacks = append(fallbacks, c.OperatorID)
		}
		decision.FallbackOperatorIDs = fallbacks
	}

	if e.cache != nil {
		if err := e.cache.Set(ctx, req.TenantID, req.IMSI, decision, e.config.CacheTTL); err != nil {
			e.logger.Warn().Err(err).Str("imsi", req.IMSI).Msg("SoR cache set error")
		}
	}

	e.logger.Info().
		Str("imsi", req.IMSI).
		Str("primary_operator", decision.PrimaryOperatorID.String()).
		Int("fallback_count", len(decision.FallbackOperatorIDs)).
		Str("reason", decision.Reason).
		Msg("SoR decision made")

	return decision, nil
}

func (e *Engine) InvalidateCache(ctx context.Context, tenantID uuid.UUID, imsi string) error {
	if e.cache == nil {
		return nil
	}
	return e.cache.Delete(ctx, tenantID, imsi)
}

func (e *Engine) InvalidateTenantCache(ctx context.Context, tenantID uuid.UUID) error {
	if e.cache == nil {
		return nil
	}
	return e.cache.DeleteAllForTenant(ctx, tenantID)
}

func (e *Engine) InvalidateByOperator(ctx context.Context, tenantID uuid.UUID, operatorID uuid.UUID) error {
	if e.cache == nil {
		return nil
	}
	return e.cache.DeleteByOperator(ctx, tenantID, operatorID)
}

func (e *Engine) BulkRecalculate(ctx context.Context, tenantID uuid.UUID) error {
	e.logger.Info().Str("tenant_id", tenantID.String()).Msg("SoR bulk recalculation triggered")
	return e.InvalidateTenantCache(ctx, tenantID)
}

func (e *Engine) checkOperatorLock(req SoRRequest) (uuid.UUID, bool) {
	if req.SimMetadata == nil {
		return uuid.Nil, false
	}

	lockVal, ok := req.SimMetadata["operator_lock"]
	if !ok {
		return uuid.Nil, false
	}

	lockStr, ok := lockVal.(string)
	if !ok {
		return uuid.Nil, false
	}

	opID, err := uuid.Parse(lockStr)
	if err != nil {
		return uuid.Nil, false
	}

	return opID, true
}

func (e *Engine) buildCandidates(grants []store.GrantWithOperator) []CandidateOperator {
	candidates := make([]CandidateOperator, 0, len(grants))
	for _, g := range grants {
		costPerMB := 0.0
		if g.CostPerMB != nil {
			costPerMB = *g.CostPerMB
		}
		rats := g.SupportedRATTypes
		if len(rats) == 0 {
			rats = g.OperatorSupportedRATTypes
		}
		candidates = append(candidates, CandidateOperator{
			OperatorID:    g.OperatorID,
			MCC:           g.MCC,
			MNC:           g.MNC,
			SupportedRATs: rats,
			SoRPriority:   g.SoRPriority,
			CostPerMB:     costPerMB,
			HealthStatus:  g.HealthStatus,
		})
	}
	return candidates
}

func (e *Engine) filterByCircuitBreaker(candidates []CandidateOperator) []CandidateOperator {
	if e.cbCheck == nil {
		return candidates
	}

	filtered := make([]CandidateOperator, 0, len(candidates))
	for _, c := range candidates {
		cb := e.cbCheck.GetCircuitBreaker(c.OperatorID)
		if cb == nil || cb.ShouldAllow() {
			filtered = append(filtered, c)
		} else {
			e.logger.Debug().
				Str("operator_id", c.OperatorID.String()).
				Msg("SoR: operator excluded by circuit breaker")
		}
	}
	return filtered
}

func (e *Engine) filterByIMSIPrefix(candidates []CandidateOperator, imsi string) []CandidateOperator {
	if imsi == "" {
		return nil
	}

	matched := make([]CandidateOperator, 0)
	for _, c := range candidates {
		if matchIMSIPrefix(imsi, c.MCC, c.MNC) {
			matched = append(matched, c)
		}
	}
	return matched
}

func matchIMSIPrefix(imsi, mcc, mnc string) bool {
	if len(imsi) < 5 {
		return false
	}
	prefix := mcc + mnc
	return strings.HasPrefix(imsi, prefix)
}

func (e *Engine) filterByRAT(candidates []CandidateOperator, requestedRAT string) []CandidateOperator {
	if requestedRAT == "" {
		return candidates
	}

	ratUpper := strings.ToUpper(requestedRAT)
	filtered := make([]CandidateOperator, 0)
	for _, c := range candidates {
		for _, supported := range c.SupportedRATs {
			if strings.EqualFold(supported, ratUpper) {
				filtered = append(filtered, c)
				break
			}
		}
	}
	return filtered
}

func (e *Engine) sortCandidates(candidates []CandidateOperator) {
	ratRank := make(map[string]int)
	for i, rat := range e.config.RATPreferenceOrder {
		ratRank[strings.ToUpper(rat)] = i
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].SoRPriority != candidates[j].SoRPriority {
			return candidates[i].SoRPriority < candidates[j].SoRPriority
		}

		iRank := e.bestRATRank(candidates[i].SupportedRATs, ratRank)
		jRank := e.bestRATRank(candidates[j].SupportedRATs, ratRank)
		if iRank != jRank {
			return iRank < jRank
		}

		return candidates[i].CostPerMB < candidates[j].CostPerMB
	})
}

func (e *Engine) bestRATRank(supportedRATs []string, ratRank map[string]int) int {
	best := len(e.config.RATPreferenceOrder) + 1
	for _, rat := range supportedRATs {
		if rank, ok := ratRank[strings.ToUpper(rat)]; ok && rank < best {
			best = rank
		}
	}
	return best
}
