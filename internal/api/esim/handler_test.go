package esim

import (
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestToProfileResponse(t *testing.T) {
	now := time.Now()
	smdpID := "smdp-plus-123"
	iccidOnProfile := "8990100000000000001"

	p := &store.ESimProfile{
		ID:                uuid.New(),
		SimID:             uuid.New(),
		EID:               "89044000000000000000000000000001",
		SMDPPlusID:        &smdpID,
		OperatorID:        uuid.New(),
		ProfileState:      "enabled",
		ICCIDOnProfile:    &iccidOnProfile,
		LastProvisionedAt: &now,
		LastError:         nil,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	resp := toProfileResponse(p)

	if resp.ID != p.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, p.ID.String())
	}
	if resp.SimID != p.SimID.String() {
		t.Errorf("SimID = %q, want %q", resp.SimID, p.SimID.String())
	}
	if resp.EID != "89044000000000000000000000000001" {
		t.Errorf("EID = %q, want %q", resp.EID, "89044000000000000000000000000001")
	}
	if resp.OperatorID != p.OperatorID.String() {
		t.Errorf("OperatorID = %q, want %q", resp.OperatorID, p.OperatorID.String())
	}
	if resp.ProfileState != "enabled" {
		t.Errorf("ProfileState = %q, want %q", resp.ProfileState, "enabled")
	}
	if resp.SMDPPlusID == nil || *resp.SMDPPlusID != "smdp-plus-123" {
		t.Error("SMDPPlusID should be 'smdp-plus-123'")
	}
	if resp.ICCIDOnProfile == nil || *resp.ICCIDOnProfile != "8990100000000000001" {
		t.Error("ICCIDOnProfile should match")
	}
	if resp.LastProvisionedAt == nil {
		t.Error("LastProvisionedAt should not be nil")
	}
	if resp.LastError != nil {
		t.Error("LastError should be nil")
	}
	if resp.CreatedAt == "" {
		t.Error("CreatedAt should not be empty")
	}
	if resp.UpdatedAt == "" {
		t.Error("UpdatedAt should not be empty")
	}
}

func TestToProfileResponseNilFields(t *testing.T) {
	now := time.Now()

	p := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        uuid.New(),
		EID:          "89044000000000000000000000000002",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := toProfileResponse(p)

	if resp.SMDPPlusID != nil {
		t.Error("SMDPPlusID should be nil when not set")
	}
	if resp.ICCIDOnProfile != nil {
		t.Error("ICCIDOnProfile should be nil when not set")
	}
	if resp.LastProvisionedAt != nil {
		t.Error("LastProvisionedAt should be nil when not set")
	}
	if resp.LastError != nil {
		t.Error("LastError should be nil when not set")
	}
}

func TestSwitchResponseFormat(t *testing.T) {
	simID := uuid.New()
	opID := uuid.New()
	now := time.Now()

	old := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        simID,
		EID:          "eid-1",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	newP := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        simID,
		EID:          "eid-2",
		OperatorID:   opID,
		ProfileState: "enabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := switchResponse{
		SimID:         simID.String(),
		OldProfile:    toProfileResponse(old),
		NewProfile:    toProfileResponse(newP),
		NewOperatorID: opID.String(),
	}

	if resp.SimID != simID.String() {
		t.Errorf("SimID = %q, want %q", resp.SimID, simID.String())
	}
	if resp.OldProfile.ProfileState != "disabled" {
		t.Errorf("OldProfile state = %q, want 'disabled'", resp.OldProfile.ProfileState)
	}
	if resp.NewProfile.ProfileState != "enabled" {
		t.Errorf("NewProfile state = %q, want 'enabled'", resp.NewProfile.ProfileState)
	}
	if resp.NewOperatorID != opID.String() {
		t.Errorf("NewOperatorID = %q, want %q", resp.NewOperatorID, opID.String())
	}
}

func TestProfileResponseJSONTags(t *testing.T) {
	now := time.Now()

	p := &store.ESimProfile{
		ID:           uuid.New(),
		SimID:        uuid.New(),
		EID:          "eid-test",
		OperatorID:   uuid.New(),
		ProfileState: "disabled",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := toProfileResponse(p)

	if resp.ProfileState != "disabled" {
		t.Errorf("ProfileState = %q, want %q", resp.ProfileState, "disabled")
	}
	if resp.EID != "eid-test" {
		t.Errorf("EID = %q, want %q", resp.EID, "eid-test")
	}
}

func TestSwitchRequestStruct(t *testing.T) {
	req := switchRequest{
		TargetProfileID: uuid.New().String(),
	}

	if req.TargetProfileID == "" {
		t.Error("TargetProfileID should not be empty")
	}

	_, err := uuid.Parse(req.TargetProfileID)
	if err != nil {
		t.Errorf("TargetProfileID should be a valid UUID: %v", err)
	}
}
