package cost

import (
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestResolveCostBucket(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want string
	}{
		{"3_days", now.Add(-3 * 24 * time.Hour), now, "1 day"},
		{"7_days", now.Add(-7 * 24 * time.Hour), now, "1 day"},
		{"30_days", now.Add(-30 * 24 * time.Hour), now, "1 day"},
		{"90_days", now.Add(-90 * 24 * time.Hour), now, "1 month"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCostBucket(tt.from, tt.to)
			if got != tt.want {
				t.Errorf("resolveCostBucket = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeltaPercent(t *testing.T) {
	tests := []struct {
		name     string
		current  int64
		previous int64
		want     float64
	}{
		{"zero_to_zero", 0, 0, 0},
		{"zero_to_positive", 100, 0, 100.0},
		{"double", 200, 100, 100.0},
		{"half", 50, 100, -50.0},
		{"same", 100, 100, 0},
		{"decrease_to_zero", 0, 100, -100.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deltaPercent(tt.current, tt.previous)
			if got != tt.want {
				t.Errorf("deltaPercent(%d, %d) = %v, want %v", tt.current, tt.previous, got, tt.want)
			}
		})
	}
}

func TestDeltaPercentF(t *testing.T) {
	tests := []struct {
		name     string
		current  float64
		previous float64
		want     float64
	}{
		{"zero_to_zero", 0, 0, 0},
		{"zero_to_positive", 100, 0, 100.0},
		{"double", 200, 100, 100.0},
		{"half", 50, 100, -50.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deltaPercentF(tt.current, tt.previous)
			if got != tt.want {
				t.Errorf("deltaPercentF(%v, %v) = %v, want %v", tt.current, tt.previous, got, tt.want)
			}
		})
	}
}

func TestRoundTo(t *testing.T) {
	tests := []struct {
		val    float64
		places int
		want   float64
	}{
		{1.23456, 2, 1.23},
		{1.23556, 2, 1.24},
		{0.0, 2, 0.0},
		{100.0, 0, 100.0},
	}
	for _, tt := range tests {
		got := roundTo(tt.val, tt.places)
		if got != tt.want {
			t.Errorf("roundTo(%v, %d) = %v, want %v", tt.val, tt.places, got, tt.want)
		}
	}
}

func TestSuggestOperatorSwitch_NotEnoughOperators(t *testing.T) {
	s := &Service{}
	result := s.suggestOperatorSwitch(nil)
	if len(result) != 0 {
		t.Errorf("expected no suggestions for nil, got %d", len(result))
	}

	result = s.suggestOperatorSwitch([]store.OperatorCostComparison{
		{OperatorID: uuid.New(), AvgCostMB: 0.01, SimCount: 100},
	})
	if len(result) != 0 {
		t.Errorf("expected no suggestions for 1 operator, got %d", len(result))
	}
}

func TestSuggestOperatorSwitch_CheaperAvailable(t *testing.T) {
	s := &Service{}
	cheapOp := uuid.New()
	expensiveOp := uuid.New()

	ops := []store.OperatorCostComparison{
		{OperatorID: expensiveOp, AvgCostMB: 0.10, TotalCost: 1000.0, SimCount: 500},
		{OperatorID: cheapOp, AvgCostMB: 0.02, TotalCost: 200.0, SimCount: 300},
	}

	result := s.suggestOperatorSwitch(ops)
	if len(result) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(result))
	}
	if result[0].Type != "operator_switch" {
		t.Errorf("type = %q, want operator_switch", result[0].Type)
	}
	if result[0].AffectedSIMCount != 500 {
		t.Errorf("affected_sim_count = %d, want 500", result[0].AffectedSIMCount)
	}
	if result[0].PotentialSavings <= 0 {
		t.Errorf("potential_savings = %v, want > 0", result[0].PotentialSavings)
	}
	if result[0].Action != "operator_switch" {
		t.Errorf("action = %q, want operator_switch", result[0].Action)
	}
}

func TestSuggestOperatorSwitch_SimilarCost(t *testing.T) {
	s := &Service{}
	op1 := uuid.New()
	op2 := uuid.New()

	ops := []store.OperatorCostComparison{
		{OperatorID: op1, AvgCostMB: 0.101, TotalCost: 1000.0, SimCount: 500},
		{OperatorID: op2, AvgCostMB: 0.100, TotalCost: 900.0, SimCount: 400},
	}

	result := s.suggestOperatorSwitch(ops)
	if len(result) != 0 {
		t.Errorf("expected no suggestions for similar costs, got %d", len(result))
	}
}

func TestToOperatorCostDTOs(t *testing.T) {
	opID := uuid.New()
	items := []store.CostByOperator{
		{OperatorID: opID, TotalUsageCost: 100.5, TotalCarrierCost: 80.0, TotalBytes: 1024, CDRCount: 10, Percentage: 65.5},
	}
	dtos := toOperatorCostDTOs(items)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if dtos[0].OperatorID != opID.String() {
		t.Errorf("operator_id = %q, want %q", dtos[0].OperatorID, opID.String())
	}
	if dtos[0].TotalUsageCost != 100.5 {
		t.Errorf("total_usage_cost = %v, want 100.5", dtos[0].TotalUsageCost)
	}
}

func TestToCostPerMBDTOs(t *testing.T) {
	opID := uuid.New()
	items := []store.CostPerMBRow{
		{OperatorID: opID, RATType: "lte", AvgCostMB: 0.01, TotalCost: 100.0, TotalMB: 10000.0},
	}
	dtos := toCostPerMBDTOs(items)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if dtos[0].RATType != "lte" {
		t.Errorf("rat_type = %q, want lte", dtos[0].RATType)
	}
}

func TestToTopExpensiveSIMDTOs(t *testing.T) {
	simID := uuid.New()
	opID := uuid.New()
	items := []store.TopExpensiveSIM{
		{SimID: simID, TotalUsageCost: 50.0, TotalBytes: 2048, CDRCount: 5, OperatorID: opID},
	}
	dtos := toTopExpensiveSIMDTOs(items)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if dtos[0].SimID != simID.String() {
		t.Errorf("sim_id = %q, want %q", dtos[0].SimID, simID.String())
	}
}

func TestToCostTrendDTOs(t *testing.T) {
	bucket := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	points := []store.CostTrendPoint{
		{Bucket: bucket, TotalUsageCost: 100.0, TotalCarrierCost: 80.0, TotalBytes: 1024, ActiveSims: 10},
	}
	dtos := toCostTrendDTOs(points)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 dto, got %d", len(dtos))
	}
	if dtos[0].Timestamp != "2026-03-01T00:00:00Z" {
		t.Errorf("timestamp = %q, want 2026-03-01T00:00:00Z", dtos[0].Timestamp)
	}
}

func TestToCostTrendDTOs_Empty(t *testing.T) {
	dtos := toCostTrendDTOs(nil)
	if len(dtos) != 0 {
		t.Errorf("expected 0 dtos for nil, got %d", len(dtos))
	}
}
