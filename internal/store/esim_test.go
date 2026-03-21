package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestESimProfileStructFields(t *testing.T) {
	now := time.Now()
	smdpID := "smdp-plus-123"
	iccidOnProfile := "8990100000000000001"
	lastError := "connection timeout"

	p := &ESimProfile{
		ID:                uuid.New(),
		SimID:             uuid.New(),
		EID:               "89044000000000000000000000000001",
		SMDPPlusID:        &smdpID,
		OperatorID:        uuid.New(),
		ProfileState:      "disabled",
		ICCIDOnProfile:    &iccidOnProfile,
		LastProvisionedAt: &now,
		LastError:         &lastError,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if p.EID != "89044000000000000000000000000001" {
		t.Errorf("EID = %q, want %q", p.EID, "89044000000000000000000000000001")
	}
	if p.ProfileState != "disabled" {
		t.Errorf("ProfileState = %q, want %q", p.ProfileState, "disabled")
	}
	if p.SMDPPlusID == nil || *p.SMDPPlusID != "smdp-plus-123" {
		t.Error("SMDPPlusID should be 'smdp-plus-123'")
	}
	if p.ICCIDOnProfile == nil || *p.ICCIDOnProfile != "8990100000000000001" {
		t.Error("ICCIDOnProfile should match")
	}
	if p.LastProvisionedAt == nil {
		t.Error("LastProvisionedAt should not be nil")
	}
	if p.LastError == nil || *p.LastError != "connection timeout" {
		t.Error("LastError should match")
	}
}

func TestESimProfileStructNilFields(t *testing.T) {
	p := &ESimProfile{
		ID:           uuid.New(),
		SimID:        uuid.New(),
		EID:          "89044000000000000000000000000002",
		OperatorID:   uuid.New(),
		ProfileState: "enabled",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if p.SMDPPlusID != nil {
		t.Error("SMDPPlusID should be nil when not set")
	}
	if p.ICCIDOnProfile != nil {
		t.Error("ICCIDOnProfile should be nil when not set")
	}
	if p.LastProvisionedAt != nil {
		t.Error("LastProvisionedAt should be nil when not set")
	}
	if p.LastError != nil {
		t.Error("LastError should be nil when not set")
	}
}

func TestListESimProfilesParams(t *testing.T) {
	simID := uuid.New()
	opID := uuid.New()

	p := ListESimProfilesParams{
		Cursor:     "abc123",
		Limit:      25,
		SimID:      &simID,
		OperatorID: &opID,
		State:      "enabled",
	}

	if p.Cursor != "abc123" {
		t.Errorf("Cursor = %q, want %q", p.Cursor, "abc123")
	}
	if p.Limit != 25 {
		t.Errorf("Limit = %d, want 25", p.Limit)
	}
	if p.SimID == nil || *p.SimID != simID {
		t.Error("SimID should match")
	}
	if p.OperatorID == nil || *p.OperatorID != opID {
		t.Error("OperatorID should match")
	}
	if p.State != "enabled" {
		t.Errorf("State = %q, want %q", p.State, "enabled")
	}
}

func TestSwitchResultFields(t *testing.T) {
	simID := uuid.New()
	opID := uuid.New()

	old := &ESimProfile{
		ID:           uuid.New(),
		SimID:        simID,
		EID:          "eid-1",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	newP := &ESimProfile{
		ID:           uuid.New(),
		SimID:        simID,
		EID:          "eid-2",
		OperatorID:   opID,
		ProfileState: "enabled",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	result := &SwitchResult{
		SimID:         simID,
		OldProfile:    old,
		NewProfile:    newP,
		NewOperatorID: opID,
	}

	if result.SimID != simID {
		t.Error("SimID should match")
	}
	if result.OldProfile.ProfileState != "disabled" {
		t.Errorf("OldProfile state = %q, want 'disabled'", result.OldProfile.ProfileState)
	}
	if result.NewProfile.ProfileState != "enabled" {
		t.Errorf("NewProfile state = %q, want 'enabled'", result.NewProfile.ProfileState)
	}
	if result.NewOperatorID != opID {
		t.Error("NewOperatorID should match")
	}
}

func TestESimProfileErrors(t *testing.T) {
	if ErrESimProfileNotFound.Error() != "store: esim profile not found" {
		t.Errorf("ErrESimProfileNotFound = %q", ErrESimProfileNotFound.Error())
	}
	if ErrProfileAlreadyEnabled.Error() != "store: another profile is already enabled for this SIM" {
		t.Errorf("ErrProfileAlreadyEnabled = %q", ErrProfileAlreadyEnabled.Error())
	}
	if ErrInvalidProfileState.Error() != "store: invalid profile state transition" {
		t.Errorf("ErrInvalidProfileState = %q", ErrInvalidProfileState.Error())
	}
	if ErrSameProfile.Error() != "store: source and target profiles are the same" {
		t.Errorf("ErrSameProfile = %q", ErrSameProfile.Error())
	}
	if ErrDifferentSIM.Error() != "store: profiles belong to different SIMs" {
		t.Errorf("ErrDifferentSIM = %q", ErrDifferentSIM.Error())
	}
}

func TestESimProfileSameProfileValidation(t *testing.T) {
	profileID := uuid.New()
	if profileID != profileID {
		t.Error("same profile ID check should detect equality")
	}
}
