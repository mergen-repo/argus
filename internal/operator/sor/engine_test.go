package sor

import (
	"context"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/operator"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockGrantProvider struct {
	grants []store.GrantWithOperator
}

func (m *mockGrantProvider) ListGrantsWithOperators(_ context.Context, _ uuid.UUID) ([]store.GrantWithOperator, error) {
	return m.grants, nil
}

type mockCBChecker struct {
	breakers map[uuid.UUID]*operator.CircuitBreaker
}

func newMockCBChecker() *mockCBChecker {
	return &mockCBChecker{breakers: make(map[uuid.UUID]*operator.CircuitBreaker)}
}

func (m *mockCBChecker) GetCircuitBreaker(opID uuid.UUID) *operator.CircuitBreaker {
	return m.breakers[opID]
}

func (m *mockCBChecker) addBreaker(opID uuid.UUID, threshold, recoverySec int) *operator.CircuitBreaker {
	cb := operator.NewCircuitBreaker(threshold, recoverySec)
	m.breakers[opID] = cb
	return cb
}

func floatPtr(v float64) *float64 { return &v }

func newTestEngine(grants []store.GrantWithOperator, cbChecker CircuitBreakerChecker) *Engine {
	logger := zerolog.Nop()
	config := DefaultConfig()

	return NewEngine(
		&mockGrantProvider{grants: grants},
		nil,
		cbChecker,
		logger,
		config,
	)
}

func TestSoR_IMSIPrefixRouting(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opA,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.10),
			},
			MCC: "234", MNC: "10",
			OperatorSupportedRATTypes: []string{"4G", "3G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opB,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 2,
				CostPerMB:   floatPtr(0.05),
			},
			MCC: "262", MNC: "01",
			OperatorSupportedRATTypes: []string{"4G", "3G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:     "23410123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected primary operator %s, got %s", opA, decision.PrimaryOperatorID)
	}
	if decision.Reason != ReasonIMSIPrefixMatch {
		t.Errorf("expected reason %s, got %s", ReasonIMSIPrefixMatch, decision.Reason)
	}
}

func TestSoR_IMSIPrefixNoMatch(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opA,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 10,
				CostPerMB:   floatPtr(0.10),
			},
			MCC: "234", MNC: "10",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opB,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 5,
				CostPerMB:   floatPtr(0.05),
			},
			MCC: "262", MNC: "01",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opB {
		t.Errorf("expected default operator %s (lowest priority), got %s", opB, decision.PrimaryOperatorID)
	}
	if decision.Reason != ReasonDefault {
		t.Errorf("expected reason %s, got %s", ReasonDefault, decision.Reason)
	}
}

func TestSoR_CostBasedSelection(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opA,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.03),
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opB,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.10),
			},
			MCC: "286", MNC: "02",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected cheaper operator %s, got %s", opA, decision.PrimaryOperatorID)
	}
}

func TestSoR_CircuitBreakerOpen(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opA,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.03),
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "down",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opB,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 2,
				CostPerMB:   floatPtr(0.10),
			},
			MCC: "286", MNC: "02",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	cbChecker := newMockCBChecker()
	cbA := cbChecker.addBreaker(opA, 1, 60)
	cbA.RecordFailure()
	cbChecker.addBreaker(opB, 5, 60)

	engine := newTestEngine(grants, cbChecker)
	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opB {
		t.Errorf("expected operator B (A has open circuit), got %s", decision.PrimaryOperatorID)
	}
}

func TestSoR_ManualOperatorLock(t *testing.T) {
	lockedOpID := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  uuid.New(),
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.03),
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:     "28601123456789",
		TenantID: tenantID,
		SimMetadata: map[string]interface{}{
			"operator_lock": lockedOpID.String(),
		},
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != lockedOpID {
		t.Errorf("expected locked operator %s, got %s", lockedOpID, decision.PrimaryOperatorID)
	}
	if decision.Reason != ReasonManualLock {
		t.Errorf("expected reason %s, got %s", ReasonManualLock, decision.Reason)
	}
	if len(decision.FallbackOperatorIDs) != 0 {
		t.Errorf("expected no fallbacks for manual lock, got %d", len(decision.FallbackOperatorIDs))
	}
}

func TestSoR_RATPreference(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opA,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.05),
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"3G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opB,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.08),
			},
			MCC: "286", MNC: "02",
			OperatorSupportedRATTypes: []string{"4G", "3G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:         "99999123456789",
		TenantID:     tenantID,
		RequestedRAT: "4G",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opB {
		t.Errorf("expected operator B (supports 4G), got %s", decision.PrimaryOperatorID)
	}
}

func TestSoR_SortByPriorityThenCost(t *testing.T) {
	op1 := uuid.New()
	op2 := uuid.New()
	op3 := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{OperatorID: op1, TenantID: tenantID, Enabled: true, SoRPriority: 2, CostPerMB: floatPtr(0.05)},
			MCC:           "286", MNC: "01", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "healthy", OperatorState: "active",
		},
		{
			OperatorGrant: store.OperatorGrant{OperatorID: op2, TenantID: tenantID, Enabled: true, SoRPriority: 1, CostPerMB: floatPtr(0.10)},
			MCC:           "286", MNC: "02", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "healthy", OperatorState: "active",
		},
		{
			OperatorGrant: store.OperatorGrant{OperatorID: op3, TenantID: tenantID, Enabled: true, SoRPriority: 1, CostPerMB: floatPtr(0.03)},
			MCC:           "286", MNC: "03", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "healthy", OperatorState: "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{IMSI: "99999123456789", TenantID: tenantID}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != op3 {
		t.Errorf("expected op3 (priority=1, cost=0.03), got %s", decision.PrimaryOperatorID)
	}
	if len(decision.FallbackOperatorIDs) != 2 {
		t.Fatalf("expected 2 fallbacks, got %d", len(decision.FallbackOperatorIDs))
	}
	if decision.FallbackOperatorIDs[0] != op2 {
		t.Errorf("expected first fallback op2 (priority=1, cost=0.10), got %s", decision.FallbackOperatorIDs[0])
	}
	if decision.FallbackOperatorIDs[1] != op1 {
		t.Errorf("expected second fallback op1 (priority=2), got %s", decision.FallbackOperatorIDs[1])
	}
}

func TestSoR_NoAvailableOperators_AllCircuitOpen(t *testing.T) {
	opA := uuid.New()
	tenantID := uuid.New()

	cbChecker := newMockCBChecker()
	cbA := cbChecker.addBreaker(opA, 1, 60)
	cbA.RecordFailure()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{OperatorID: opA, TenantID: tenantID, Enabled: true, SoRPriority: 1, CostPerMB: floatPtr(0.05)},
			MCC:           "286", MNC: "01", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "down", OperatorState: "active",
		},
	}

	engine := newTestEngine(grants, cbChecker)
	req := SoRRequest{IMSI: "99999123456789", TenantID: tenantID}

	_, err := engine.Evaluate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when all operators have open circuits")
	}
}

func TestSoR_NoGrants(t *testing.T) {
	engine := newTestEngine(nil, nil)
	req := SoRRequest{IMSI: "99999123456789", TenantID: uuid.New()}

	_, err := engine.Evaluate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when no grants available")
	}
}

func TestSoR_MatchIMSIPrefix(t *testing.T) {
	tests := []struct {
		name  string
		imsi  string
		mcc   string
		mnc   string
		match bool
	}{
		{"exact match 5 digit", "23410123456789", "234", "10", true},
		{"exact match 6 digit", "262013123456789", "262", "013", true},
		{"no match", "28601123456789", "234", "10", false},
		{"short IMSI", "2341", "234", "10", false},
		{"empty IMSI", "", "234", "10", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchIMSIPrefix(tt.imsi, tt.mcc, tt.mnc)
			if got != tt.match {
				t.Errorf("matchIMSIPrefix(%q, %q, %q) = %v, want %v", tt.imsi, tt.mcc, tt.mnc, got, tt.match)
			}
		})
	}
}

func TestSoR_FilterByRAT(t *testing.T) {
	engine := &Engine{config: DefaultConfig(), logger: zerolog.Nop()}

	candidates := []CandidateOperator{
		{OperatorID: uuid.New(), SupportedRATs: []string{"4G", "3G"}},
		{OperatorID: uuid.New(), SupportedRATs: []string{"3G"}},
		{OperatorID: uuid.New(), SupportedRATs: []string{"5G", "4G"}},
	}

	filtered := engine.filterByRAT(candidates, "4G")
	if len(filtered) != 2 {
		t.Errorf("expected 2 operators supporting 4G, got %d", len(filtered))
	}

	filtered5G := engine.filterByRAT(candidates, "5G")
	if len(filtered5G) != 1 {
		t.Errorf("expected 1 operator supporting 5G, got %d", len(filtered5G))
	}

	filteredNone := engine.filterByRAT(candidates, "6G")
	if len(filteredNone) != 0 {
		t.Errorf("expected 0 operators supporting 6G, got %d", len(filteredNone))
	}
}

func TestSoR_SortCandidates(t *testing.T) {
	engine := &Engine{config: DefaultConfig(), logger: zerolog.Nop()}

	op1 := CandidateOperator{OperatorID: uuid.New(), SoRPriority: 2, CostPerMB: 0.05, SupportedRATs: []string{"3G"}}
	op2 := CandidateOperator{OperatorID: uuid.New(), SoRPriority: 1, CostPerMB: 0.10, SupportedRATs: []string{"4G"}}
	op3 := CandidateOperator{OperatorID: uuid.New(), SoRPriority: 1, CostPerMB: 0.03, SupportedRATs: []string{"5G"}}

	candidates := []CandidateOperator{op1, op2, op3}
	engine.sortCandidates(candidates)

	if candidates[0].OperatorID != op3.OperatorID {
		t.Errorf("first should be op3 (priority=1, 5G, cost=0.03)")
	}
	if candidates[1].OperatorID != op2.OperatorID {
		t.Errorf("second should be op2 (priority=1, 4G, cost=0.10)")
	}
	if candidates[2].OperatorID != op1.OperatorID {
		t.Errorf("third should be op1 (priority=2)")
	}
}

func TestSoR_DefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.CacheTTL != time.Hour {
		t.Errorf("default cache TTL = %v, want 1h", config.CacheTTL)
	}
	if len(config.RATPreferenceOrder) != 7 {
		t.Errorf("default RAT preference order length = %d, want 7", len(config.RATPreferenceOrder))
	}
}

func TestSoR_ManualLockInvalidUUID(t *testing.T) {
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  uuid.New(),
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.05),
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:     "28601123456789",
		TenantID: tenantID,
		SimMetadata: map[string]interface{}{
			"operator_lock": "not-a-uuid",
		},
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Reason == ReasonManualLock {
		t.Error("invalid UUID should not trigger manual lock")
	}
}

func TestSoR_FallbackOperatorsList(t *testing.T) {
	op1 := uuid.New()
	op2 := uuid.New()
	op3 := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{OperatorID: op1, TenantID: tenantID, Enabled: true, SoRPriority: 1, CostPerMB: floatPtr(0.05)},
			MCC:           "234", MNC: "10", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "healthy", OperatorState: "active",
		},
		{
			OperatorGrant: store.OperatorGrant{OperatorID: op2, TenantID: tenantID, Enabled: true, SoRPriority: 2, CostPerMB: floatPtr(0.08)},
			MCC:           "262", MNC: "01", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "healthy", OperatorState: "active",
		},
		{
			OperatorGrant: store.OperatorGrant{OperatorID: op3, TenantID: tenantID, Enabled: true, SoRPriority: 3, CostPerMB: floatPtr(0.12)},
			MCC:           "310", MNC: "01", OperatorSupportedRATTypes: []string{"4G"}, HealthStatus: "healthy", OperatorState: "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{IMSI: "23410123456789", TenantID: tenantID}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != op1 {
		t.Errorf("expected primary op1 (IMSI prefix match), got %s", decision.PrimaryOperatorID)
	}
}

func TestSoR_RATPreference_NoSupportedFallsBackToAll(t *testing.T) {
	opA := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{OperatorID: opA, TenantID: tenantID, Enabled: true, SoRPriority: 1, CostPerMB: floatPtr(0.05)},
			MCC:           "286", MNC: "01", OperatorSupportedRATTypes: []string{"3G"}, HealthStatus: "healthy", OperatorState: "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:         "99999123456789",
		TenantID:     tenantID,
		RequestedRAT: "5G",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("when no RAT matches, should fall back to all candidates: got %s", decision.PrimaryOperatorID)
	}
}

func TestSoR_GrantLevelRATOverridesOperator(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:        opA,
				TenantID:          tenantID,
				Enabled:           true,
				SoRPriority:       1,
				CostPerMB:         floatPtr(0.05),
				SupportedRATTypes: []string{"5G_SA", "LTE"},
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"LTE", "3G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opB,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.05),
			},
			MCC: "286", MNC: "02",
			OperatorSupportedRATTypes: []string{"LTE", "3G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:         "99999123456789",
		TenantID:     tenantID,
		RequestedRAT: "5G_SA",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA (grant-level RAT 5G_SA overrides operator LTE/3G), got %s", decision.PrimaryOperatorID)
	}
}

func TestSoR_GrantEmptyRATFallsBackToOperator(t *testing.T) {
	opA := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:        opA,
				TenantID:          tenantID,
				Enabled:           true,
				SoRPriority:       1,
				CostPerMB:         floatPtr(0.05),
				SupportedRATTypes: []string{},
			},
			MCC: "286", MNC: "01",
			OperatorSupportedRATTypes: []string{"LTE", "5G_SA"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)
	req := SoRRequest{
		IMSI:         "99999123456789",
		TenantID:     tenantID,
		RequestedRAT: "5G_SA",
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA via operator-level RAT fallback when grant RAT is empty, got %s", decision.PrimaryOperatorID)
	}
}
