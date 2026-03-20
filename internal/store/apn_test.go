package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAPNStructFields(t *testing.T) {
	now := time.Now()
	policyID := uuid.New()
	createdBy := uuid.New()
	displayName := "IoT Fleet APN"

	a := &APN{
		ID:                uuid.New(),
		TenantID:          uuid.New(),
		OperatorID:        uuid.New(),
		Name:              "iot.fleet",
		DisplayName:       &displayName,
		APNType:           "private_managed",
		SupportedRATTypes: []string{"lte", "nr_5g"},
		DefaultPolicyID:   &policyID,
		State:             "active",
		Settings:          json.RawMessage(`{"qos":"premium"}`),
		CreatedAt:         now,
		UpdatedAt:         now,
		CreatedBy:         &createdBy,
	}

	if a.Name != "iot.fleet" {
		t.Errorf("Name = %q, want %q", a.Name, "iot.fleet")
	}
	if a.APNType != "private_managed" {
		t.Errorf("APNType = %q, want %q", a.APNType, "private_managed")
	}
	if a.DisplayName == nil || *a.DisplayName != "IoT Fleet APN" {
		t.Error("DisplayName should be 'IoT Fleet APN'")
	}
	if len(a.SupportedRATTypes) != 2 {
		t.Errorf("SupportedRATTypes len = %d, want 2", len(a.SupportedRATTypes))
	}
	if a.DefaultPolicyID == nil || *a.DefaultPolicyID != policyID {
		t.Error("DefaultPolicyID should match")
	}
	if a.State != "active" {
		t.Errorf("State = %q, want %q", a.State, "active")
	}
	if a.CreatedBy == nil || *a.CreatedBy != createdBy {
		t.Error("CreatedBy should match")
	}
	if a.UpdatedBy != nil {
		t.Error("UpdatedBy should be nil when not set")
	}
}

func TestAPNStructNilFields(t *testing.T) {
	a := &APN{
		ID:       uuid.New(),
		Name:     "test",
		APNType:  "operator_managed",
		Settings: json.RawMessage(`{}`),
	}

	if a.DisplayName != nil {
		t.Error("DisplayName should be nil when not set")
	}
	if a.DefaultPolicyID != nil {
		t.Error("DefaultPolicyID should be nil when not set")
	}
	if a.CreatedBy != nil {
		t.Error("CreatedBy should be nil when not set")
	}
	if a.UpdatedBy != nil {
		t.Error("UpdatedBy should be nil when not set")
	}
}

func TestCreateAPNParamsDefaults(t *testing.T) {
	p := CreateAPNParams{
		Name:       "test.apn",
		OperatorID: uuid.New(),
		APNType:    "private_managed",
	}

	if p.Name != "test.apn" {
		t.Errorf("Name = %q, want %q", p.Name, "test.apn")
	}
	if p.SupportedRATTypes != nil {
		t.Error("SupportedRATTypes should be nil by default")
	}
	if p.DisplayName != nil {
		t.Error("DisplayName should be nil by default")
	}
	if p.DefaultPolicyID != nil {
		t.Error("DefaultPolicyID should be nil by default")
	}
	if p.Settings != nil {
		t.Error("Settings should be nil by default")
	}
}

func TestUpdateAPNParamsNilHandling(t *testing.T) {
	p := UpdateAPNParams{}

	if p.DisplayName != nil {
		t.Error("DisplayName should be nil")
	}
	if p.SupportedRATTypes != nil {
		t.Error("SupportedRATTypes should be nil")
	}
	if p.DefaultPolicyID != nil {
		t.Error("DefaultPolicyID should be nil")
	}
	if p.Settings != nil {
		t.Error("Settings should be nil")
	}
	if p.UpdatedBy != nil {
		t.Error("UpdatedBy should be nil")
	}
}

func TestAPNErrorSentinels(t *testing.T) {
	if ErrAPNNotFound == ErrAPNNameExists {
		t.Error("ErrAPNNotFound and ErrAPNNameExists should be distinct")
	}
	if ErrAPNNotFound == ErrAPNHasActiveSIMs {
		t.Error("ErrAPNNotFound and ErrAPNHasActiveSIMs should be distinct")
	}
	if ErrAPNNameExists == ErrAPNHasActiveSIMs {
		t.Error("ErrAPNNameExists and ErrAPNHasActiveSIMs should be distinct")
	}

	if ErrAPNNotFound.Error() != "store: apn not found" {
		t.Errorf("ErrAPNNotFound message = %q", ErrAPNNotFound.Error())
	}
	if ErrAPNNameExists.Error() != "store: apn name already exists for this tenant+operator" {
		t.Errorf("ErrAPNNameExists message = %q", ErrAPNNameExists.Error())
	}
	if ErrAPNHasActiveSIMs.Error() != "store: apn has active sims" {
		t.Errorf("ErrAPNHasActiveSIMs message = %q", ErrAPNHasActiveSIMs.Error())
	}
}
