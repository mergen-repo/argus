package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRoamingAgreementStruct(t *testing.T) {
	now := time.Now().UTC()
	start := now.AddDate(-1, 0, 0)
	end := now.AddDate(1, 0, 0)
	tenantID := uuid.New()
	operatorID := uuid.New()
	id := uuid.New()
	notes := "test notes"

	a := RoamingAgreement{
		ID:                  id,
		TenantID:            tenantID,
		OperatorID:          operatorID,
		PartnerOperatorName: "Vodafone TR",
		AgreementType:       "international",
		SLATerms:            json.RawMessage(`{"uptime_pct":99.9}`),
		CostTerms:           json.RawMessage(`{"cost_per_mb":0.05,"currency":"USD","settlement_period":"monthly"}`),
		StartDate:           start,
		EndDate:             end,
		AutoRenew:           true,
		State:               "active",
		Notes:               &notes,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if a.ID != id {
		t.Errorf("ID = %s, want %s", a.ID, id)
	}
	if a.TenantID != tenantID {
		t.Errorf("TenantID = %s, want %s", a.TenantID, tenantID)
	}
	if a.OperatorID != operatorID {
		t.Errorf("OperatorID = %s, want %s", a.OperatorID, operatorID)
	}
	if a.PartnerOperatorName != "Vodafone TR" {
		t.Errorf("PartnerOperatorName = %q, want %q", a.PartnerOperatorName, "Vodafone TR")
	}
	if a.AgreementType != "international" {
		t.Errorf("AgreementType = %q, want %q", a.AgreementType, "international")
	}
	if !a.AutoRenew {
		t.Error("AutoRenew should be true")
	}
	if a.State != "active" {
		t.Errorf("State = %q, want %q", a.State, "active")
	}
	if a.Notes == nil || *a.Notes != "test notes" {
		t.Error("Notes should be \"test notes\"")
	}
	if a.TerminatedAt != nil {
		t.Error("TerminatedAt should be nil for active agreement")
	}
	if a.CreatedBy != nil {
		t.Error("CreatedBy should be nil when not set")
	}
}

func TestRoamingAgreement_ParsedCostTerms(t *testing.T) {
	a := RoamingAgreement{
		CostTerms: json.RawMessage(`{"cost_per_mb":0.05,"currency":"USD","settlement_period":"monthly"}`),
	}

	ct, err := a.ParsedCostTerms()
	if err != nil {
		t.Fatalf("ParsedCostTerms() error: %v", err)
	}
	if ct.CostPerMB != 0.05 {
		t.Errorf("CostPerMB = %f, want 0.05", ct.CostPerMB)
	}
	if ct.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", ct.Currency)
	}
	if ct.SettlementPeriod != "monthly" {
		t.Errorf("SettlementPeriod = %q, want monthly", ct.SettlementPeriod)
	}
}

func TestRoamingAgreement_ParsedCostTerms_WithVolumeTiers(t *testing.T) {
	a := RoamingAgreement{
		CostTerms: json.RawMessage(`{
			"cost_per_mb":0.10,
			"currency":"EUR",
			"settlement_period":"quarterly",
			"volume_tiers":[
				{"threshold_mb":1000,"cost_per_mb":0.08},
				{"threshold_mb":10000,"cost_per_mb":0.05}
			]
		}`),
	}

	ct, err := a.ParsedCostTerms()
	if err != nil {
		t.Fatalf("ParsedCostTerms() with volume tiers error: %v", err)
	}
	if len(ct.VolumeTiers) != 2 {
		t.Fatalf("VolumeTiers len = %d, want 2", len(ct.VolumeTiers))
	}
	if ct.VolumeTiers[0].ThresholdMB != 1000 {
		t.Errorf("VolumeTiers[0].ThresholdMB = %d, want 1000", ct.VolumeTiers[0].ThresholdMB)
	}
	if ct.VolumeTiers[0].CostPerMB != 0.08 {
		t.Errorf("VolumeTiers[0].CostPerMB = %f, want 0.08", ct.VolumeTiers[0].CostPerMB)
	}
	if ct.VolumeTiers[1].CostPerMB != 0.05 {
		t.Errorf("VolumeTiers[1].CostPerMB = %f, want 0.05", ct.VolumeTiers[1].CostPerMB)
	}
}

func TestRoamingAgreement_ParsedCostTerms_InvalidJSON(t *testing.T) {
	a := RoamingAgreement{
		CostTerms: json.RawMessage(`not-valid-json`),
	}

	_, err := a.ParsedCostTerms()
	if err == nil {
		t.Fatal("expected error for invalid JSON cost_terms, got nil")
	}
}

func TestRoamingAgreement_ParsedCostTerms_EmptyJSON(t *testing.T) {
	a := RoamingAgreement{
		CostTerms: json.RawMessage(`{}`),
	}

	ct, err := a.ParsedCostTerms()
	if err != nil {
		t.Fatalf("ParsedCostTerms() with empty JSON error: %v", err)
	}
	if ct.CostPerMB != 0 {
		t.Errorf("CostPerMB = %f, want 0 for empty JSON", ct.CostPerMB)
	}
	if ct.Currency != "" {
		t.Errorf("Currency = %q, want empty for empty JSON", ct.Currency)
	}
}

func TestSLATermsStruct(t *testing.T) {
	s := SLATerms{
		UptimePct:    99.9,
		LatencyP95Ms: 50,
		MaxIncidents: 3,
	}

	if s.UptimePct != 99.9 {
		t.Errorf("UptimePct = %f, want 99.9", s.UptimePct)
	}
	if s.LatencyP95Ms != 50 {
		t.Errorf("LatencyP95Ms = %d, want 50", s.LatencyP95Ms)
	}
	if s.MaxIncidents != 3 {
		t.Errorf("MaxIncidents = %d, want 3", s.MaxIncidents)
	}
}

func TestCostTermsStruct(t *testing.T) {
	ct := CostTerms{
		CostPerMB:        0.05,
		Currency:         "USD",
		SettlementPeriod: "monthly",
		VolumeTiers: []VolumeTier{
			{ThresholdMB: 5000, CostPerMB: 0.03},
		},
	}

	if ct.CostPerMB != 0.05 {
		t.Errorf("CostPerMB = %f, want 0.05", ct.CostPerMB)
	}
	if ct.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", ct.Currency)
	}
	if ct.SettlementPeriod != "monthly" {
		t.Errorf("SettlementPeriod = %q, want monthly", ct.SettlementPeriod)
	}
	if len(ct.VolumeTiers) != 1 {
		t.Fatalf("VolumeTiers len = %d, want 1", len(ct.VolumeTiers))
	}
	if ct.VolumeTiers[0].ThresholdMB != 5000 {
		t.Errorf("VolumeTiers[0].ThresholdMB = %d, want 5000", ct.VolumeTiers[0].ThresholdMB)
	}
}

func TestVolumeTierStruct(t *testing.T) {
	vt := VolumeTier{
		ThresholdMB: 10000,
		CostPerMB:   0.02,
	}

	if vt.ThresholdMB != 10000 {
		t.Errorf("ThresholdMB = %d, want 10000", vt.ThresholdMB)
	}
	if vt.CostPerMB != 0.02 {
		t.Errorf("CostPerMB = %f, want 0.02", vt.CostPerMB)
	}
}

func TestCreateRoamingAgreementParams(t *testing.T) {
	operatorID := uuid.New()
	start := time.Now().UTC()
	end := start.AddDate(1, 0, 0)
	notes := "test"
	createdBy := uuid.New()

	p := CreateRoamingAgreementParams{
		OperatorID:          operatorID,
		PartnerOperatorName: "Partner A",
		AgreementType:       "national",
		SLATerms:            json.RawMessage(`{}`),
		CostTerms:           json.RawMessage(`{"cost_per_mb":0.01,"currency":"USD","settlement_period":"monthly"}`),
		StartDate:           start,
		EndDate:             end,
		AutoRenew:           true,
		State:               "draft",
		Notes:               &notes,
		CreatedBy:           &createdBy,
	}

	if p.OperatorID != operatorID {
		t.Errorf("OperatorID = %s, want %s", p.OperatorID, operatorID)
	}
	if p.PartnerOperatorName != "Partner A" {
		t.Errorf("PartnerOperatorName = %q, want %q", p.PartnerOperatorName, "Partner A")
	}
	if p.AgreementType != "national" {
		t.Errorf("AgreementType = %q, want national", p.AgreementType)
	}
	if !p.AutoRenew {
		t.Error("AutoRenew should be true")
	}
	if p.State != "draft" {
		t.Errorf("State = %q, want draft", p.State)
	}
	if p.Notes == nil || *p.Notes != "test" {
		t.Error("Notes should be \"test\"")
	}
	if p.CreatedBy == nil || *p.CreatedBy != createdBy {
		t.Error("CreatedBy mismatch")
	}
}

func TestUpdateRoamingAgreementParams_AllOptional(t *testing.T) {
	p := UpdateRoamingAgreementParams{}

	if p.PartnerOperatorName != nil {
		t.Error("PartnerOperatorName should be nil by default")
	}
	if p.AgreementType != nil {
		t.Error("AgreementType should be nil by default")
	}
	if p.SLATerms != nil {
		t.Error("SLATerms should be nil by default")
	}
	if p.CostTerms != nil {
		t.Error("CostTerms should be nil by default")
	}
	if p.StartDate != nil {
		t.Error("StartDate should be nil by default")
	}
	if p.EndDate != nil {
		t.Error("EndDate should be nil by default")
	}
	if p.AutoRenew != nil {
		t.Error("AutoRenew should be nil by default")
	}
	if p.State != nil {
		t.Error("State should be nil by default")
	}
	if p.Notes != nil {
		t.Error("Notes should be nil by default")
	}
}

func TestUpdateRoamingAgreementParams_PartialSet(t *testing.T) {
	name := "Updated Partner"
	state := "active"

	p := UpdateRoamingAgreementParams{
		PartnerOperatorName: &name,
		State:               &state,
	}

	if p.PartnerOperatorName == nil || *p.PartnerOperatorName != "Updated Partner" {
		t.Error("PartnerOperatorName should be \"Updated Partner\"")
	}
	if p.State == nil || *p.State != "active" {
		t.Error("State should be \"active\"")
	}
	if p.AgreementType != nil {
		t.Error("AgreementType should remain nil when not set")
	}
}

func TestListRoamingAgreementsFilter(t *testing.T) {
	opID := uuid.New()
	days := 30

	f := ListRoamingAgreementsFilter{
		OperatorID:         &opID,
		State:              "active",
		ExpiringWithinDays: &days,
		Cursor:             uuid.New().String(),
		Limit:              25,
	}

	if f.OperatorID == nil || *f.OperatorID != opID {
		t.Error("OperatorID mismatch")
	}
	if f.State != "active" {
		t.Errorf("State = %q, want active", f.State)
	}
	if f.ExpiringWithinDays == nil || *f.ExpiringWithinDays != 30 {
		t.Error("ExpiringWithinDays should be 30")
	}
	if f.Limit != 25 {
		t.Errorf("Limit = %d, want 25", f.Limit)
	}
	if f.Cursor == "" {
		t.Error("Cursor should not be empty")
	}
}

func TestListRoamingAgreementsFilter_Defaults(t *testing.T) {
	f := ListRoamingAgreementsFilter{}

	if f.OperatorID != nil {
		t.Error("OperatorID should be nil by default")
	}
	if f.State != "" {
		t.Errorf("State = %q, want empty", f.State)
	}
	if f.ExpiringWithinDays != nil {
		t.Error("ExpiringWithinDays should be nil by default")
	}
	if f.Limit != 0 {
		t.Errorf("Limit = %d, want 0 (store will apply default)", f.Limit)
	}
}

func TestErrRoamingAgreementNotFound(t *testing.T) {
	if ErrRoamingAgreementNotFound.Error() != "store: roaming agreement not found" {
		t.Errorf("ErrRoamingAgreementNotFound = %q, want %q",
			ErrRoamingAgreementNotFound.Error(), "store: roaming agreement not found")
	}
}

func TestErrRoamingAgreementOverlap(t *testing.T) {
	if ErrRoamingAgreementOverlap.Error() != "store: active roaming agreement already exists for this tenant+operator" {
		t.Errorf("ErrRoamingAgreementOverlap = %q, want %q",
			ErrRoamingAgreementOverlap.Error(),
			"store: active roaming agreement already exists for this tenant+operator")
	}
}

func TestRoamingAgreementStates(t *testing.T) {
	validStates := []string{"draft", "active", "suspended", "terminated"}
	for _, state := range validStates {
		a := RoamingAgreement{State: state}
		if a.State != state {
			t.Errorf("State = %q, want %q", a.State, state)
		}
	}
}

func TestRoamingAgreementAgreementTypes(t *testing.T) {
	validTypes := []string{"international", "national", "data_only", "voice_data"}
	for _, atype := range validTypes {
		a := RoamingAgreement{AgreementType: atype}
		if a.AgreementType != atype {
			t.Errorf("AgreementType = %q, want %q", a.AgreementType, atype)
		}
	}
}

func TestRoamingAgreement_TerminatedAt(t *testing.T) {
	now := time.Now().UTC()
	a := RoamingAgreement{
		State:        "terminated",
		TerminatedAt: &now,
	}

	if a.TerminatedAt == nil {
		t.Fatal("TerminatedAt should not be nil for terminated agreement")
	}
	if !a.TerminatedAt.Equal(now) {
		t.Errorf("TerminatedAt = %v, want %v", *a.TerminatedAt, now)
	}
}

func TestRoamingAgreement_CreatedBy(t *testing.T) {
	userID := uuid.New()
	a := RoamingAgreement{
		CreatedBy: &userID,
	}

	if a.CreatedBy == nil {
		t.Fatal("CreatedBy should not be nil when set")
	}
	if *a.CreatedBy != userID {
		t.Errorf("CreatedBy = %s, want %s", *a.CreatedBy, userID)
	}
}

func TestNewRoamingAgreementStore_NotNil(t *testing.T) {
	s := NewRoamingAgreementStore(nil)
	if s == nil {
		t.Fatal("NewRoamingAgreementStore(nil) should not return nil")
	}
}
