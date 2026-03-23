package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSIMStructFields(t *testing.T) {
	now := time.Now()
	apnID := uuid.New()
	ipAddrID := uuid.New()
	policyID := uuid.New()
	esimID := uuid.New()
	msisdn := "+905551234567"
	ratType := "lte"

	s := &SIM{
		ID:                    uuid.New(),
		TenantID:              uuid.New(),
		OperatorID:            uuid.New(),
		APNID:                 &apnID,
		ICCID:                 "8990123456789012345",
		IMSI:                  "286010123456789",
		MSISDN:                &msisdn,
		IPAddressID:           &ipAddrID,
		PolicyVersionID:       &policyID,
		ESimProfileID:         &esimID,
		SimType:               "physical",
		State:                 "ordered",
		RATType:               &ratType,
		MaxConcurrentSessions: 1,
		SessionIdleTimeoutSec: 3600,
		SessionHardTimeoutSec: 86400,
		Metadata:              json.RawMessage(`{"group":"fleet-a"}`),
		ActivatedAt:           &now,
		SuspendedAt:           nil,
		TerminatedAt:          nil,
		PurgeAt:               nil,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	if s.ICCID != "8990123456789012345" {
		t.Errorf("ICCID = %q, want %q", s.ICCID, "8990123456789012345")
	}
	if s.IMSI != "286010123456789" {
		t.Errorf("IMSI = %q, want %q", s.IMSI, "286010123456789")
	}
	if s.MSISDN == nil || *s.MSISDN != "+905551234567" {
		t.Error("MSISDN should be '+905551234567'")
	}
	if s.SimType != "physical" {
		t.Errorf("SimType = %q, want %q", s.SimType, "physical")
	}
	if s.State != "ordered" {
		t.Errorf("State = %q, want %q", s.State, "ordered")
	}
	if s.RATType == nil || *s.RATType != "lte" {
		t.Error("RATType should be 'lte'")
	}
	if s.MaxConcurrentSessions != 1 {
		t.Errorf("MaxConcurrentSessions = %d, want 1", s.MaxConcurrentSessions)
	}
	if s.SessionIdleTimeoutSec != 3600 {
		t.Errorf("SessionIdleTimeoutSec = %d, want 3600", s.SessionIdleTimeoutSec)
	}
	if s.SessionHardTimeoutSec != 86400 {
		t.Errorf("SessionHardTimeoutSec = %d, want 86400", s.SessionHardTimeoutSec)
	}
	if s.APNID == nil || *s.APNID != apnID {
		t.Error("APNID should match")
	}
	if s.IPAddressID == nil || *s.IPAddressID != ipAddrID {
		t.Error("IPAddressID should match")
	}
	if s.PolicyVersionID == nil || *s.PolicyVersionID != policyID {
		t.Error("PolicyVersionID should match")
	}
	if s.ESimProfileID == nil || *s.ESimProfileID != esimID {
		t.Error("ESimProfileID should match")
	}
	if s.ActivatedAt == nil {
		t.Error("ActivatedAt should not be nil")
	}
}

func TestSIMStructNilFields(t *testing.T) {
	s := &SIM{
		ID:       uuid.New(),
		ICCID:    "8990123456789012345",
		IMSI:     "286010123456789",
		SimType:  "esim",
		State:    "ordered",
		Metadata: json.RawMessage(`{}`),
	}

	if s.MSISDN != nil {
		t.Error("MSISDN should be nil when not set")
	}
	if s.APNID != nil {
		t.Error("APNID should be nil when not set")
	}
	if s.IPAddressID != nil {
		t.Error("IPAddressID should be nil when not set")
	}
	if s.PolicyVersionID != nil {
		t.Error("PolicyVersionID should be nil when not set")
	}
	if s.ESimProfileID != nil {
		t.Error("ESimProfileID should be nil when not set")
	}
	if s.RATType != nil {
		t.Error("RATType should be nil when not set")
	}
	if s.ActivatedAt != nil {
		t.Error("ActivatedAt should be nil when not set")
	}
	if s.SuspendedAt != nil {
		t.Error("SuspendedAt should be nil when not set")
	}
	if s.TerminatedAt != nil {
		t.Error("TerminatedAt should be nil when not set")
	}
	if s.PurgeAt != nil {
		t.Error("PurgeAt should be nil when not set")
	}
}

func TestSimStateHistoryStruct(t *testing.T) {
	now := time.Now()
	userID := uuid.New()
	jobID := uuid.New()
	fromState := "ordered"
	reason := "activated by admin"

	h := &SimStateHistory{
		ID:          42,
		SimID:       uuid.New(),
		FromState:   &fromState,
		ToState:     "active",
		Reason:      &reason,
		TriggeredBy: "user",
		UserID:      &userID,
		JobID:       &jobID,
		CreatedAt:   now,
	}

	if h.ID != 42 {
		t.Errorf("ID = %d, want 42", h.ID)
	}
	if h.FromState == nil || *h.FromState != "ordered" {
		t.Error("FromState should be 'ordered'")
	}
	if h.ToState != "active" {
		t.Errorf("ToState = %q, want %q", h.ToState, "active")
	}
	if h.Reason == nil || *h.Reason != "activated by admin" {
		t.Error("Reason should be 'activated by admin'")
	}
	if h.TriggeredBy != "user" {
		t.Errorf("TriggeredBy = %q, want %q", h.TriggeredBy, "user")
	}
	if h.UserID == nil || *h.UserID != userID {
		t.Error("UserID should match")
	}
	if h.JobID == nil || *h.JobID != jobID {
		t.Error("JobID should match")
	}
}

func TestCreateSIMParamsDefaults(t *testing.T) {
	msisdn := "+905551234567"
	ratType := "nb_iot"

	p := CreateSIMParams{
		ICCID:      "8990123456789012345",
		IMSI:       "286010123456789",
		MSISDN:     &msisdn,
		OperatorID: uuid.New(),
		APNID:      uuid.New(),
		SimType:    "physical",
		RATType:    &ratType,
		Metadata:   json.RawMessage(`{"key":"value"}`),
	}

	if p.ICCID != "8990123456789012345" {
		t.Errorf("ICCID = %q, want expected value", p.ICCID)
	}
	if p.MSISDN == nil || *p.MSISDN != "+905551234567" {
		t.Error("MSISDN should be set")
	}
	if p.RATType == nil || *p.RATType != "nb_iot" {
		t.Error("RATType should be 'nb_iot'")
	}
	if p.SimType != "physical" {
		t.Errorf("SimType = %q, want %q", p.SimType, "physical")
	}
}

func TestValidTransitions(t *testing.T) {
	tests := []struct {
		from string
		to   string
	}{
		{"ordered", "active"},
		{"active", "suspended"},
		{"active", "stolen_lost"},
		{"active", "terminated"},
		{"suspended", "active"},
		{"suspended", "terminated"},
		{"terminated", "purged"},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			err := validateTransition(tt.from, tt.to)
			if err != nil {
				t.Errorf("validateTransition(%q, %q) returned error: %v", tt.from, tt.to, err)
			}
		})
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		from string
		to   string
	}{
		{"ordered", "suspended"},
		{"ordered", "terminated"},
		{"ordered", "stolen_lost"},
		{"ordered", "purged"},
		{"active", "ordered"},
		{"active", "purged"},
		{"suspended", "ordered"},
		{"suspended", "stolen_lost"},
		{"suspended", "purged"},
		{"stolen_lost", "active"},
		{"stolen_lost", "ordered"},
		{"stolen_lost", "suspended"},
		{"stolen_lost", "terminated"},
		{"terminated", "active"},
		{"terminated", "ordered"},
		{"terminated", "suspended"},
		{"purged", "active"},
		{"purged", "ordered"},
	}

	for _, tt := range tests {
		t.Run(tt.from+"->"+tt.to, func(t *testing.T) {
			err := validateTransition(tt.from, tt.to)
			if err == nil {
				t.Errorf("validateTransition(%q, %q) should return error for invalid transition", tt.from, tt.to)
			}
			if err != ErrInvalidStateTransition {
				t.Errorf("validateTransition(%q, %q) returned %v, want ErrInvalidStateTransition", tt.from, tt.to, err)
			}
		})
	}
}

func TestListSIMsParamsStruct(t *testing.T) {
	operatorID := uuid.New()
	apnID := uuid.New()

	p := ListSIMsParams{
		Cursor:     "cursor-abc",
		Limit:      25,
		ICCID:      "899012345",
		IMSI:       "286010",
		MSISDN:     "+90555",
		OperatorID: &operatorID,
		APNID:      &apnID,
		State:      "active",
		RATType:    "lte",
		Q:          "search term",
	}

	if p.Cursor != "cursor-abc" {
		t.Errorf("Cursor = %q, want %q", p.Cursor, "cursor-abc")
	}
	if p.Limit != 25 {
		t.Errorf("Limit = %d, want 25", p.Limit)
	}
	if p.ICCID != "899012345" {
		t.Errorf("ICCID = %q, want %q", p.ICCID, "899012345")
	}
	if p.State != "active" {
		t.Errorf("State = %q, want %q", p.State, "active")
	}
	if p.Q != "search term" {
		t.Errorf("Q = %q, want %q", p.Q, "search term")
	}
	if p.OperatorID == nil || *p.OperatorID != operatorID {
		t.Error("OperatorID should match")
	}
	if p.APNID == nil || *p.APNID != apnID {
		t.Error("APNID should match")
	}
}

func TestValidTransitionsMapCompleteness(t *testing.T) {
	expectedStates := []string{"ordered", "active", "suspended", "stolen_lost", "terminated", "purged"}

	for _, state := range expectedStates {
		if _, ok := validTransitions[state]; !ok {
			t.Errorf("validTransitions missing entry for state %q", state)
		}
	}
}

func TestValidateTransition_UnknownCurrentState(t *testing.T) {
	err := validateTransition("nonexistent", "active")
	if err != ErrInvalidStateTransition {
		t.Errorf("expected ErrInvalidStateTransition for unknown current state, got %v", err)
	}
}

func TestValidateTransition_SelfTransition(t *testing.T) {
	states := []string{"ordered", "active", "suspended", "stolen_lost", "terminated", "purged"}
	for _, state := range states {
		t.Run(state+"->"+state, func(t *testing.T) {
			err := validateTransition(state, state)
			if err == nil {
				t.Errorf("self-transition %s->%s should be invalid", state, state)
			}
		})
	}
}

func TestValidateTransition_TerminalStatesHaveNoOutbound(t *testing.T) {
	terminalStates := []string{"stolen_lost", "purged"}
	for _, state := range terminalStates {
		allowed := validTransitions[state]
		if len(allowed) != 0 {
			t.Errorf("terminal state %q should have no allowed transitions, got %v", state, allowed)
		}
	}
}

func TestValidateTransition_StolenLostIsAbsorbing(t *testing.T) {
	targets := []string{"ordered", "active", "suspended", "terminated", "purged"}
	for _, target := range targets {
		err := validateTransition("stolen_lost", target)
		if err == nil {
			t.Errorf("stolen_lost->%s should be invalid (absorbing state)", target)
		}
	}
}
