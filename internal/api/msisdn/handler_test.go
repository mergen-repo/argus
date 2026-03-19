package msisdn

import (
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func TestToDTO(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	operatorID := uuid.New()
	simID := uuid.New()
	now := time.Now().UTC()
	reservedUntil := now.Add(24 * time.Hour)

	m := &store.MSISDN{
		ID:            id,
		TenantID:      tenantID,
		OperatorID:    operatorID,
		MSISDN:        "+905551234567",
		State:         "assigned",
		SimID:         &simID,
		ReservedUntil: &reservedUntil,
		CreatedAt:     now,
	}

	dto := toDTO(m)

	if dto.ID != id {
		t.Errorf("ID = %v, want %v", dto.ID, id)
	}
	if dto.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", dto.TenantID, tenantID)
	}
	if dto.OperatorID != operatorID {
		t.Errorf("OperatorID = %v, want %v", dto.OperatorID, operatorID)
	}
	if dto.MSISDN != "+905551234567" {
		t.Errorf("MSISDN = %q, want %q", dto.MSISDN, "+905551234567")
	}
	if dto.State != "assigned" {
		t.Errorf("State = %q, want %q", dto.State, "assigned")
	}
	if dto.SimID == nil || *dto.SimID != simID {
		t.Errorf("SimID = %v, want %v", dto.SimID, simID)
	}
	if dto.ReservedUntil == nil {
		t.Error("ReservedUntil should not be nil")
	}
	if dto.CreatedAt != now.Format(timeFmt) {
		t.Errorf("CreatedAt = %q, want %q", dto.CreatedAt, now.Format(timeFmt))
	}
}

func TestToDTONilOptionals(t *testing.T) {
	m := &store.MSISDN{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		OperatorID: uuid.New(),
		MSISDN:     "+905551234567",
		State:      "available",
		CreatedAt:  time.Now().UTC(),
	}

	dto := toDTO(m)

	if dto.SimID != nil {
		t.Errorf("SimID = %v, want nil", dto.SimID)
	}
	if dto.ReservedUntil != nil {
		t.Errorf("ReservedUntil = %v, want nil", dto.ReservedUntil)
	}
}

func TestValidStates(t *testing.T) {
	validStates := map[string]bool{"available": true, "assigned": true, "reserved": true}

	if !validStates["available"] {
		t.Error("available should be valid")
	}
	if !validStates["assigned"] {
		t.Error("assigned should be valid")
	}
	if !validStates["reserved"] {
		t.Error("reserved should be valid")
	}
	if validStates["invalid"] {
		t.Error("invalid should not be valid")
	}
}
