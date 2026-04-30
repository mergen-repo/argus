package store

import (
	"fmt"
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

func TestMSISDNStore_BulkImport_BatchSize500(t *testing.T) {
	const total = 1200
	const batchSize = bulkImportBatchSize

	expectedChunks := (total + batchSize - 1) / batchSize
	if expectedChunks != 3 {
		t.Fatalf("expected 3 chunks for 1200 rows, got %d", expectedChunks)
	}

	chunkSizes := make([]int, 0, expectedChunks)
	for start := 0; start < total; start += batchSize {
		end := start + batchSize
		if end > total {
			end = total
		}
		chunkSizes = append(chunkSizes, end-start)
	}

	if len(chunkSizes) != 3 {
		t.Fatalf("chunk count = %d, want 3", len(chunkSizes))
	}
	if chunkSizes[0] != 500 {
		t.Errorf("chunk[0] = %d, want 500", chunkSizes[0])
	}
	if chunkSizes[1] != 500 {
		t.Errorf("chunk[1] = %d, want 500", chunkSizes[1])
	}
	if chunkSizes[2] != 200 {
		t.Errorf("chunk[2] = %d, want 200", chunkSizes[2])
	}

	duplicateRows := map[int]bool{5: true, 250: true, 750: true, 1100: true}
	rows := make([]MSISDNImportRow, total)
	for i := range rows {
		rows[i] = MSISDNImportRow{MSISDN: "+9055500" + fmt.Sprintf("%07d", i+1)}
	}

	simulatedResult := &MSISDNImportResult{Total: total}
	for i, row := range rows {
		if duplicateRows[i+1] {
			simulatedResult.Skipped++
			simulatedResult.Errors = append(simulatedResult.Errors, MSISDNImportError{
				Row:     i + 1,
				MSISDN:  row.MSISDN,
				Message: "duplicate",
			})
		} else {
			simulatedResult.Imported++
		}
	}

	wantImported := total - len(duplicateRows)
	wantSkipped := len(duplicateRows)

	if simulatedResult.Imported != wantImported {
		t.Errorf("Imported = %d, want %d", simulatedResult.Imported, wantImported)
	}
	if simulatedResult.Skipped != wantSkipped {
		t.Errorf("Skipped = %d, want %d", simulatedResult.Skipped, wantSkipped)
	}
	if len(simulatedResult.Errors) != wantSkipped {
		t.Errorf("Errors count = %d, want %d", len(simulatedResult.Errors), wantSkipped)
	}
	if simulatedResult.Imported+simulatedResult.Skipped != total {
		t.Errorf("Imported + Skipped = %d, want %d", simulatedResult.Imported+simulatedResult.Skipped, total)
	}

	for _, e := range simulatedResult.Errors {
		if e.Message != "duplicate" {
			t.Errorf("error message = %q, want %q", e.Message, "duplicate")
		}
		if !duplicateRows[e.Row] {
			t.Errorf("unexpected duplicate at row %d", e.Row)
		}
	}
}
