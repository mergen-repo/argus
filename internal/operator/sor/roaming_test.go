package sor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

type mockAgreementProvider struct {
	active  []store.RoamingAgreement
	expired []store.RoamingAgreement
	err     error
}

func (m *mockAgreementProvider) ListActiveByTenant(_ context.Context, _ uuid.UUID, _ time.Time) ([]store.RoamingAgreement, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.active, nil
}

func (m *mockAgreementProvider) ListRecentlyExpiredByTenant(_ context.Context, _ uuid.UUID, _ time.Time, _ time.Duration) ([]store.RoamingAgreement, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.expired, nil
}

func costTermsJSON(costPerMB float64, currency string) json.RawMessage {
	b, _ := json.Marshal(store.CostTerms{
		CostPerMB:        costPerMB,
		Currency:         currency,
		SettlementPeriod: "monthly",
	})
	return b
}

func makeActiveAgreement(operatorID uuid.UUID, costPerMB float64) store.RoamingAgreement {
	now := time.Now()
	return store.RoamingAgreement{
		ID:                  uuid.New(),
		TenantID:            uuid.New(),
		OperatorID:          operatorID,
		PartnerOperatorName: "Test Partner",
		AgreementType:       "international",
		SLATerms:            json.RawMessage(`{}`),
		CostTerms:           costTermsJSON(costPerMB, "USD"),
		StartDate:           now.AddDate(-1, 0, 0),
		EndDate:             now.AddDate(1, 0, 0),
		AutoRenew:           false,
		State:               "active",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func TestSoR_AgreementOverridesCost(t *testing.T) {
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
				CostPerMB:   floatPtr(0.20),
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

	agA := makeActiveAgreement(opA, 0.02)
	agA.TenantID = tenantID
	provider := &mockAgreementProvider{
		active: []store.RoamingAgreement{agA},
	}
	engine.SetAgreementProvider(provider)

	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA (agreement cost 0.02 < opB grant cost 0.10), got %s", decision.PrimaryOperatorID)
	}

	if decision.AgreementID == nil {
		t.Error("AgreementID should be set when agreement override is applied")
	} else if *decision.AgreementID != agA.ID {
		t.Errorf("AgreementID = %s, want %s", *decision.AgreementID, agA.ID)
	}

	if decision.Reason != ReasonRoamingAgreement {
		t.Errorf("Reason = %q, want %q", decision.Reason, ReasonRoamingAgreement)
	}
}

func TestSoR_NoAgreement_DefaultBehaviorUnchanged(t *testing.T) {
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

	provider := &mockAgreementProvider{
		active: []store.RoamingAgreement{},
	}
	engine.SetAgreementProvider(provider)

	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA (cheaper at 0.03), got %s", decision.PrimaryOperatorID)
	}
	if decision.AgreementID != nil {
		t.Error("AgreementID should be nil when no agreement applies")
	}
	if decision.Reason == ReasonRoamingAgreement {
		t.Error("Reason should not be roaming_agreement when no agreement is active")
	}
}

func TestSoR_NilAgreementProvider_NoError(t *testing.T) {
	opA := uuid.New()
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
		t.Fatalf("nil agreementProvider should not cause error: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA, got %s", decision.PrimaryOperatorID)
	}
	if decision.AgreementID != nil {
		t.Error("AgreementID should be nil without agreementProvider")
	}
}

func TestSoR_AgreementProviderError_FallsBackGracefully(t *testing.T) {
	opA := uuid.New()
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
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)

	provider := &mockAgreementProvider{
		err: errors.New("db connection lost"),
	}
	engine.SetAgreementProvider(provider)

	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("agreement provider error should degrade gracefully, got: %v", err)
	}
	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA as fallback, got %s", decision.PrimaryOperatorID)
	}
}

func TestSoR_AgreementCostOverride_MultipleOperators(t *testing.T) {
	opA := uuid.New()
	opB := uuid.New()
	opC := uuid.New()
	tenantID := uuid.New()

	grants := []store.GrantWithOperator{
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opA,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.50),
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
				CostPerMB:   floatPtr(0.30),
			},
			MCC: "286", MNC: "02",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
		{
			OperatorGrant: store.OperatorGrant{
				OperatorID:  opC,
				TenantID:    tenantID,
				Enabled:     true,
				SoRPriority: 1,
				CostPerMB:   floatPtr(0.40),
			},
			MCC: "286", MNC: "03",
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)

	agA := makeActiveAgreement(opA, 0.01)
	agA.TenantID = tenantID

	provider := &mockAgreementProvider{
		active: []store.RoamingAgreement{agA},
	}
	engine.SetAgreementProvider(provider)

	req := SoRRequest{
		IMSI:     "99999123456789",
		TenantID: tenantID,
	}

	decision, err := engine.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.PrimaryOperatorID != opA {
		t.Errorf("expected opA with agreement cost 0.01 (lowest), got %s", decision.PrimaryOperatorID)
	}
	if decision.AgreementID == nil || *decision.AgreementID != agA.ID {
		t.Error("AgreementID should reference the active agreement for opA")
	}
}

func TestSoR_SetAgreementProvider(t *testing.T) {
	opA := uuid.New()
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
			OperatorSupportedRATTypes: []string{"4G"},
			HealthStatus:              "healthy",
			OperatorState:             "active",
		},
	}

	engine := newTestEngine(grants, nil)

	if engine.agreementProvider != nil {
		t.Error("agreementProvider should be nil before SetAgreementProvider")
	}

	provider := &mockAgreementProvider{}
	engine.SetAgreementProvider(provider)

	if engine.agreementProvider == nil {
		t.Error("agreementProvider should be set after SetAgreementProvider")
	}
}

func TestSoR_ReasonRoamingAgreementConstant(t *testing.T) {
	if ReasonRoamingAgreement != "roaming_agreement_applied" {
		t.Errorf("ReasonRoamingAgreement = %q, want %q", ReasonRoamingAgreement, "roaming_agreement_applied")
	}
}
