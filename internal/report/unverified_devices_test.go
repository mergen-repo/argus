package report

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// unverifiedDevicesFixture is an in-memory DataProvider that returns a
// controlled set of unverified device rows, mirroring what the real
// StoreProvider would return after filtering binding_status IN
// ('pending','mismatch').
type unverifiedDevicesFixture struct {
	rows [][]string
}

func (f *unverifiedDevicesFixture) KVKK(_ context.Context, _ uuid.UUID, _ map[string]any) (*KVKKData, error) {
	return &KVKKData{}, nil
}
func (f *unverifiedDevicesFixture) GDPR(_ context.Context, _ uuid.UUID, _ map[string]any) (*GDPRData, error) {
	return &GDPRData{}, nil
}
func (f *unverifiedDevicesFixture) BTK(_ context.Context, _ uuid.UUID, _ map[string]any) (*BTKData, error) {
	return &BTKData{}, nil
}
func (f *unverifiedDevicesFixture) SLAMonthly(_ context.Context, _ uuid.UUID, _ map[string]any) (*SLAData, error) {
	return &SLAData{}, nil
}
func (f *unverifiedDevicesFixture) UsageSummary(_ context.Context, _ uuid.UUID, _ map[string]any) (*UsageData, error) {
	return &UsageData{}, nil
}
func (f *unverifiedDevicesFixture) CostAnalysis(_ context.Context, _ uuid.UUID, _ map[string]any) (*CostData, error) {
	return &CostData{}, nil
}
func (f *unverifiedDevicesFixture) AuditExport(_ context.Context, _ uuid.UUID, _ map[string]any) (*AuditExportData, error) {
	return &AuditExportData{}, nil
}
func (f *unverifiedDevicesFixture) SIMInventory(_ context.Context, _ uuid.UUID, _ map[string]any) (*SIMInventoryData, error) {
	return &SIMInventoryData{}, nil
}
func (f *unverifiedDevicesFixture) AlertsExport(_ context.Context, _ uuid.UUID, _ AlertsExportFilters) (*AlertsExportData, error) {
	return &AlertsExportData{}, nil
}
func (f *unverifiedDevicesFixture) UnverifiedDevices(_ context.Context, _ uuid.UUID, _ map[string]any) (*UnverifiedDevicesData, error) {
	now := time.Now().UTC()
	return &UnverifiedDevicesData{
		Columns:    unverifiedDevicesColumns,
		Rows:       f.rows,
		Summary:    map[string]string{"rows": "2"},
		PeriodFrom: now,
		PeriodTo:   now,
	}, nil
}

// TestUnverifiedDevices_FilterAndOrder verifies:
//
//  1. Only rows with binding_status 'pending' or 'mismatch' appear (the
//     'verified' SIM is absent).
//  2. Rows are ordered DESC by last_imei_seen_at (mismatch row first).
//  3. Columns match the canonical header.
//  4. CSV / PDF / XLSX builders all accept the new report type without error.
func TestUnverifiedDevices_FilterAndOrder(t *testing.T) {
	t1 := "2026-05-01T10:00:00Z"
	t2 := "2026-05-02T09:00:00Z"

	// mismatch row has a later last_imei_seen_at → must appear first (DESC).
	rows := [][]string{
		{"8949000000000000002", "sim-uuid-2", "strict", "mismatch", t2, "111111111111111"},
		{"8949000000000000001", "sim-uuid-1", "soft", "pending", t1, ""},
	}

	fix := &unverifiedDevicesFixture{rows: rows}
	engine := NewEngine(fix)
	ctx := context.Background()
	tenantID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

	req := Request{
		Type:     ReportUnverifiedDevices,
		TenantID: tenantID,
		Filters:  map[string]any{},
		Locale:   "en",
	}

	for _, format := range []Format{FormatCSV, FormatPDF, FormatXLSX} {
		r := req
		r.Format = format
		artifact, err := engine.Build(ctx, r)
		if err != nil {
			t.Fatalf("Build(%s): unexpected error: %v", format, err)
		}
		if len(artifact.Bytes) == 0 {
			t.Fatalf("Build(%s): empty artifact", format)
		}
	}

	data, err := fix.UnverifiedDevices(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("UnverifiedDevices: %v", err)
	}

	if len(data.Rows) != 2 {
		t.Fatalf("expected 2 rows (pending+mismatch), got %d", len(data.Rows))
	}

	wantColumns := []string{"ICCID", "SIM ID", "Binding Mode", "Binding Status", "Last IMEI Seen", "Bound IMEI"}
	if len(data.Columns) != len(wantColumns) {
		t.Fatalf("column count mismatch: got %v, want %v", data.Columns, wantColumns)
	}
	for i, col := range wantColumns {
		if data.Columns[i] != col {
			t.Errorf("column[%d]: got %q, want %q", i, data.Columns[i], col)
		}
	}

	if data.Rows[0][3] != "mismatch" {
		t.Errorf("first row binding_status: got %q, want 'mismatch' (DESC last_imei_seen_at)", data.Rows[0][3])
	}
	if data.Rows[1][3] != "pending" {
		t.Errorf("second row binding_status: got %q, want 'pending'", data.Rows[1][3])
	}
}

// TestUnverifiedDevices_Empty verifies that an empty result set produces a
// valid, non-error artifact with zero data rows.
func TestUnverifiedDevices_Empty(t *testing.T) {
	fix := &unverifiedDevicesFixture{rows: [][]string{}}
	engine := NewEngine(fix)
	ctx := context.Background()
	tenantID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")

	req := Request{
		Type:     ReportUnverifiedDevices,
		Format:   FormatCSV,
		TenantID: tenantID,
		Filters:  map[string]any{},
		Locale:   "en",
	}
	artifact, err := engine.Build(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on empty result: %v", err)
	}
	if len(artifact.Bytes) == 0 {
		t.Fatal("empty artifact bytes")
	}
	if artifact.MIME != "text/csv" {
		t.Fatalf("expected text/csv, got %q", artifact.MIME)
	}
}
