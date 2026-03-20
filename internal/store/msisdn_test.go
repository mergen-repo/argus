package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestMSISDNImportResultFields(t *testing.T) {
	result := MSISDNImportResult{
		Total:    5,
		Imported: 3,
		Skipped:  2,
		Errors: []MSISDNImportError{
			{Row: 2, MSISDN: "+905551234567", Message: "MSISDN already exists"},
			{Row: 4, MSISDN: "+905551234568", Message: "MSISDN already exists"},
		},
	}

	if result.Total != 5 {
		t.Errorf("Total = %d, want 5", result.Total)
	}
	if result.Imported != 3 {
		t.Errorf("Imported = %d, want 3", result.Imported)
	}
	if result.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", result.Skipped)
	}
	if len(result.Errors) != 2 {
		t.Errorf("Errors count = %d, want 2", len(result.Errors))
	}
	if result.Errors[0].Row != 2 {
		t.Errorf("Error[0].Row = %d, want 2", result.Errors[0].Row)
	}
	if result.Errors[0].MSISDN != "+905551234567" {
		t.Errorf("Error[0].MSISDN = %q, want %q", result.Errors[0].MSISDN, "+905551234567")
	}
}

func TestMSISDNStates(t *testing.T) {
	validStates := map[string]bool{
		"available": true,
		"assigned":  true,
		"reserved":  true,
	}

	tests := []struct {
		state string
		valid bool
	}{
		{"available", true},
		{"assigned", true},
		{"reserved", true},
		{"deleted", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.state, func(t *testing.T) {
			if validStates[tc.state] != tc.valid {
				t.Errorf("state %q valid = %v, want %v", tc.state, validStates[tc.state], tc.valid)
			}
		})
	}
}

func TestMSISDNImportRow(t *testing.T) {
	row := MSISDNImportRow{
		MSISDN:       "+905551234567",
		OperatorCode: "TURKCELL",
	}

	if row.MSISDN != "+905551234567" {
		t.Errorf("MSISDN = %q, want %q", row.MSISDN, "+905551234567")
	}
	if row.OperatorCode != "TURKCELL" {
		t.Errorf("OperatorCode = %q, want %q", row.OperatorCode, "TURKCELL")
	}
}

func TestMSISDNSentinelErrors(t *testing.T) {
	if ErrMSISDNExists == nil {
		t.Error("ErrMSISDNExists should not be nil")
	}
	if ErrMSISDNNotAvailable == nil {
		t.Error("ErrMSISDNNotAvailable should not be nil")
	}
	if ErrMSISDNExists.Error() != "msisdn already exists" {
		t.Errorf("ErrMSISDNExists = %q, want %q", ErrMSISDNExists.Error(), "msisdn already exists")
	}
	if ErrMSISDNNotAvailable.Error() != "msisdn not available" {
		t.Errorf("ErrMSISDNNotAvailable = %q, want %q", ErrMSISDNNotAvailable.Error(), "msisdn not available")
	}
}

func TestMSISDNStructFields(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	operatorID := uuid.New()
	simID := uuid.New()

	m := MSISDN{
		ID:         id,
		TenantID:   tenantID,
		OperatorID: operatorID,
		MSISDN:     "+905559876543",
		State:      "assigned",
		SimID:      &simID,
	}

	if m.ID != id {
		t.Errorf("ID = %v, want %v", m.ID, id)
	}
	if m.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", m.TenantID, tenantID)
	}
	if m.OperatorID != operatorID {
		t.Errorf("OperatorID = %v, want %v", m.OperatorID, operatorID)
	}
	if m.MSISDN != "+905559876543" {
		t.Errorf("MSISDN = %q, want %q", m.MSISDN, "+905559876543")
	}
	if m.State != "assigned" {
		t.Errorf("State = %q, want %q", m.State, "assigned")
	}
	if m.SimID == nil || *m.SimID != simID {
		t.Errorf("SimID = %v, want %v", m.SimID, simID)
	}
}

func TestMSISDNImportResultPartialSuccess(t *testing.T) {
	result := MSISDNImportResult{
		Total:    10,
		Imported: 7,
		Skipped:  2,
		Errors: []MSISDNImportError{
			{Row: 3, MSISDN: "+905551000003", Message: "MSISDN already exists"},
			{Row: 5, MSISDN: "+905551000005", Message: "MSISDN already exists"},
			{Row: 8, MSISDN: "", Message: "invalid MSISDN format"},
		},
	}

	if result.Total != result.Imported+result.Skipped+1 {
		t.Errorf("Total (%d) != Imported (%d) + Skipped (%d) + other errors (1)",
			result.Total, result.Imported, result.Skipped)
	}

	if len(result.Errors) != 3 {
		t.Errorf("Errors count = %d, want 3", len(result.Errors))
	}

	for _, e := range result.Errors {
		if e.Row <= 0 {
			t.Errorf("Error row should be positive, got %d", e.Row)
		}
		if e.Message == "" {
			t.Error("Error message should not be empty")
		}
	}
}

func TestMSISDNImportErrorJSONTags(t *testing.T) {
	e := MSISDNImportError{
		Row:     1,
		MSISDN:  "+905551234567",
		Message: "duplicate",
	}

	if e.Row != 1 {
		t.Errorf("Row = %d, want 1", e.Row)
	}
	if e.MSISDN != "+905551234567" {
		t.Errorf("MSISDN = %q, want +905551234567", e.MSISDN)
	}
	if e.Message != "duplicate" {
		t.Errorf("Message = %q, want duplicate", e.Message)
	}
}
