package cost

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Suggestion struct {
	Type            string  `json:"type"`
	Description     string  `json:"description"`
	AffectedSIMCount int64  `json:"affected_sim_count"`
	PotentialSavings float64 `json:"potential_savings"`
	Action          string  `json:"action"`
}

type CostAnalytics struct {
	TotalCost       float64                   `json:"total_cost"`
	Currency        string                    `json:"currency"`
	ByOperator      []OperatorCostDTO         `json:"by_operator"`
	CostPerMB       []CostPerMBDTO            `json:"cost_per_mb"`
	TopExpensiveSIMs []TopExpensiveSIMDTO      `json:"top_expensive_sims"`
	Trend           []CostTrendDTO            `json:"trend"`
	Comparison      *CostComparisonDTO        `json:"comparison,omitempty"`
	Suggestions     []Suggestion              `json:"suggestions"`
}

type OperatorCostDTO struct {
	OperatorID       string  `json:"operator_id"`
	TotalUsageCost   float64 `json:"total_usage_cost"`
	TotalCarrierCost float64 `json:"total_carrier_cost"`
	TotalBytes       int64   `json:"total_bytes"`
	CDRCount         int64   `json:"cdr_count"`
	Percentage       float64 `json:"percentage"`
}

type CostPerMBDTO struct {
	OperatorID string  `json:"operator_id"`
	RATType    string  `json:"rat_type"`
	AvgCostMB  float64 `json:"avg_cost_per_mb"`
	TotalCost  float64 `json:"total_cost"`
	TotalMB    float64 `json:"total_mb"`
}

type TopExpensiveSIMDTO struct {
	SimID          string  `json:"sim_id"`
	TotalUsageCost float64 `json:"total_usage_cost"`
	TotalBytes     int64   `json:"total_bytes"`
	CDRCount       int64   `json:"cdr_count"`
	OperatorID     string  `json:"operator_id"`
}

type CostTrendDTO struct {
	Timestamp        string  `json:"ts"`
	TotalUsageCost   float64 `json:"total_usage_cost"`
	TotalCarrierCost float64 `json:"total_carrier_cost"`
	TotalBytes       int64   `json:"total_bytes"`
	ActiveSims       int64   `json:"active_sims"`
}

type CostComparisonDTO struct {
	PreviousTotalCost float64 `json:"previous_total_cost"`
	CostDeltaPct      float64 `json:"cost_delta_pct"`
	PreviousBytes     int64   `json:"previous_bytes"`
	BytesDeltaPct     float64 `json:"bytes_delta_pct"`
	PreviousSims      int64   `json:"previous_sims"`
	SimsDeltaPct      float64 `json:"sims_delta_pct"`
}

type Service struct {
	costStore *store.CostAnalyticsStore
	logger    zerolog.Logger
}

func NewService(costStore *store.CostAnalyticsStore, logger zerolog.Logger) *Service {
	return &Service{
		costStore: costStore,
		logger:    logger.With().Str("component", "cost_analytics").Logger(),
	}
}

func (s *Service) GetCostAnalytics(ctx context.Context, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID, apnID *uuid.UUID, ratType *string) (*CostAnalytics, error) {
	p := store.CostQueryParams{
		TenantID:   tenantID,
		From:       from,
		To:         to,
		OperatorID: operatorID,
		APNID:      apnID,
		RATType:    ratType,
	}

	totals, err := s.costStore.GetCostTotals(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("cost totals: %w", err)
	}

	byOperator, err := s.costStore.GetCostByOperator(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("cost by operator: %w", err)
	}

	costPerMB, err := s.costStore.GetCostPerMB(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("cost per mb: %w", err)
	}

	topSIMs, err := s.costStore.GetTopExpensiveSIMs(ctx, p, 20)
	if err != nil {
		return nil, fmt.Errorf("top expensive sims: %w", err)
	}

	bucketInterval := resolveCostBucket(from, to)
	trend, err := s.costStore.GetCostTrend(ctx, p, bucketInterval)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to get cost trend, continuing without")
		trend = nil
	}

	duration := to.Sub(from)
	prevFrom := from.Add(-duration)
	prevTo := from
	prevParams := store.CostQueryParams{
		TenantID:   tenantID,
		From:       prevFrom,
		To:         prevTo,
		OperatorID: operatorID,
		APNID:      apnID,
		RATType:    ratType,
	}
	prevTotals, err := s.costStore.GetCostTotals(ctx, prevParams)
	var comparison *CostComparisonDTO
	if err == nil {
		comparison = &CostComparisonDTO{
			PreviousTotalCost: prevTotals.TotalUsageCost,
			CostDeltaPct:      deltaPercentF(totals.TotalUsageCost, prevTotals.TotalUsageCost),
			PreviousBytes:     prevTotals.TotalBytes,
			BytesDeltaPct:     deltaPercent(totals.TotalBytes, prevTotals.TotalBytes),
			PreviousSims:      prevTotals.UniqueSims,
			SimsDeltaPct:      deltaPercent(totals.UniqueSims, prevTotals.UniqueSims),
		}
	} else {
		s.logger.Warn().Err(err).Msg("failed to get comparison totals")
	}

	suggestions := s.generateSuggestions(ctx, tenantID, from, to)

	result := &CostAnalytics{
		TotalCost:        totals.TotalUsageCost,
		Currency:         "USD",
		ByOperator:       toOperatorCostDTOs(byOperator),
		CostPerMB:        toCostPerMBDTOs(costPerMB),
		TopExpensiveSIMs: toTopExpensiveSIMDTOs(topSIMs),
		Trend:            toCostTrendDTOs(trend),
		Comparison:       comparison,
		Suggestions:      suggestions,
	}

	return result, nil
}

func (s *Service) generateSuggestions(ctx context.Context, tenantID uuid.UUID, from, to time.Time) []Suggestion {
	var suggestions []Suggestion

	opComparison, err := s.costStore.GetOperatorCostComparison(ctx, tenantID, from, to)
	if err == nil && len(opComparison) >= 2 {
		sg := s.suggestOperatorSwitch(opComparison)
		suggestions = append(suggestions, sg...)
	}

	inactiveSIMs, err := s.costStore.GetInactiveSIMsCost(ctx, tenantID, from, to, 30)
	if err == nil && inactiveSIMs.SimCount > 0 {
		suggestions = append(suggestions, Suggestion{
			Type:             "inactive_sims",
			Description:      fmt.Sprintf("%d active SIMs with no data usage in the last 30 days are still incurring costs (total: $%.2f)", inactiveSIMs.SimCount, inactiveSIMs.TotalCost),
			AffectedSIMCount: inactiveSIMs.SimCount,
			PotentialSavings: inactiveSIMs.TotalCost,
			Action:           "terminate",
		})
	}

	lowUsage, err := s.costStore.GetLowUsageSIMs(ctx, tenantID, from, to, 1*1024*1024)
	if err == nil && lowUsage.SimCount > 0 {
		savings := lowUsage.TotalCost * 0.5
		suggestions = append(suggestions, Suggestion{
			Type:             "low_usage",
			Description:      fmt.Sprintf("%d SIMs used less than 1 MB in the period, costing $%.2f total. Consider a lower-tier plan.", lowUsage.SimCount, lowUsage.TotalCost),
			AffectedSIMCount: lowUsage.SimCount,
			PotentialSavings: roundTo(savings, 2),
			Action:           "plan_downgrade",
		})
	}

	return suggestions
}

func (s *Service) suggestOperatorSwitch(ops []store.OperatorCostComparison) []Suggestion {
	if len(ops) < 2 {
		return nil
	}

	sort.Slice(ops, func(i, j int) bool {
		return ops[i].AvgCostMB < ops[j].AvgCostMB
	})

	cheapest := ops[0]
	var suggestions []Suggestion

	for _, op := range ops[1:] {
		if op.SimCount == 0 || op.AvgCostMB <= 0 {
			continue
		}
		if cheapest.AvgCostMB <= 0 {
			continue
		}
		savingPerMB := op.AvgCostMB - cheapest.AvgCostMB
		if savingPerMB <= 0 {
			continue
		}
		savingsRatio := savingPerMB / op.AvgCostMB
		if savingsRatio < 0.1 {
			continue
		}

		totalSavings := op.TotalCost * savingsRatio
		suggestions = append(suggestions, Suggestion{
			Type:             "operator_switch",
			Description:      fmt.Sprintf("%d SIMs on operator %s could save $%.2f/period by switching to cheaper operator %s (%.1f%% savings)", op.SimCount, op.OperatorID.String()[:8], totalSavings, cheapest.OperatorID.String()[:8], savingsRatio*100),
			AffectedSIMCount: op.SimCount,
			PotentialSavings: roundTo(totalSavings, 2),
			Action:           "operator_switch",
		})
	}

	return suggestions
}

func resolveCostBucket(from, to time.Time) string {
	duration := to.Sub(from)
	switch {
	case duration <= 7*24*time.Hour:
		return "1 day"
	case duration <= 31*24*time.Hour:
		return "1 day"
	default:
		return "1 month"
	}
}

func toOperatorCostDTOs(items []store.CostByOperator) []OperatorCostDTO {
	dtos := make([]OperatorCostDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, OperatorCostDTO{
			OperatorID:       item.OperatorID.String(),
			TotalUsageCost:   item.TotalUsageCost,
			TotalCarrierCost: item.TotalCarrierCost,
			TotalBytes:       item.TotalBytes,
			CDRCount:         item.CDRCount,
			Percentage:       roundTo(item.Percentage, 2),
		})
	}
	return dtos
}

func toCostPerMBDTOs(items []store.CostPerMBRow) []CostPerMBDTO {
	dtos := make([]CostPerMBDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, CostPerMBDTO{
			OperatorID: item.OperatorID.String(),
			RATType:    item.RATType,
			AvgCostMB:  roundTo(item.AvgCostMB, 6),
			TotalCost:  item.TotalCost,
			TotalMB:    roundTo(item.TotalMB, 4),
		})
	}
	return dtos
}

func toTopExpensiveSIMDTOs(items []store.TopExpensiveSIM) []TopExpensiveSIMDTO {
	dtos := make([]TopExpensiveSIMDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, TopExpensiveSIMDTO{
			SimID:          item.SimID.String(),
			TotalUsageCost: item.TotalUsageCost,
			TotalBytes:     item.TotalBytes,
			CDRCount:       item.CDRCount,
			OperatorID:     item.OperatorID.String(),
		})
	}
	return dtos
}

func toCostTrendDTOs(points []store.CostTrendPoint) []CostTrendDTO {
	dtos := make([]CostTrendDTO, 0, len(points))
	for _, pt := range points {
		dtos = append(dtos, CostTrendDTO{
			Timestamp:        pt.Bucket.Format(time.RFC3339),
			TotalUsageCost:   pt.TotalUsageCost,
			TotalCarrierCost: pt.TotalCarrierCost,
			TotalBytes:       pt.TotalBytes,
			ActiveSims:       pt.ActiveSims,
		})
	}
	return dtos
}

func deltaPercent(current, previous int64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100.0
	}
	return float64(current-previous) / float64(previous) * 100.0
}

func deltaPercentF(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100.0
	}
	return (current - previous) / previous * 100.0
}

func roundTo(val float64, places int) float64 {
	pow := math.Pow(10, float64(places))
	return math.Round(val*pow) / pow
}
