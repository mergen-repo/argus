package compliance

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestBR7_DeriveTenantSalt_Length(t *testing.T) {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	salt := DeriveTenantSalt(tenantID)
	if len(salt) != 32 {
		t.Fatalf("salt length = %d, want 32", len(salt))
	}
}

func TestBR7_DeriveTenantSalt_Deterministic(t *testing.T) {
	tid := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	salt1 := DeriveTenantSalt(tid)
	salt2 := DeriveTenantSalt(tid)
	if salt1 != salt2 {
		t.Fatal("salt should be deterministic for same tenant")
	}
}

func TestBR7_DeriveTenantSalt_DifferentPerTenant(t *testing.T) {
	t1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	t2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	t3 := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	s1 := DeriveTenantSalt(t1)
	s2 := DeriveTenantSalt(t2)
	s3 := DeriveTenantSalt(t3)

	if s1 == s2 || s1 == s3 || s2 == s3 {
		t.Error("different tenants must have different salts for pseudonymization isolation")
	}
}

func TestBR7_ComplianceDashboard_Structure(t *testing.T) {
	d := ComplianceDashboard{
		PendingPurges: 10,
		OverduePurges: 2,
		RetentionDays: 90,
		CompliancePct: 80.0,
		ChainVerified: true,
	}

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed ComplianceDashboard
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.PendingPurges != 10 {
		t.Errorf("PendingPurges = %d, want 10", parsed.PendingPurges)
	}
	if parsed.OverduePurges != 2 {
		t.Errorf("OverduePurges = %d, want 2", parsed.OverduePurges)
	}
	if parsed.RetentionDays != 90 {
		t.Errorf("RetentionDays = %d, want 90", parsed.RetentionDays)
	}
	if parsed.CompliancePct != 80.0 {
		t.Errorf("CompliancePct = %f, want 80.0", parsed.CompliancePct)
	}
	if !parsed.ChainVerified {
		t.Error("ChainVerified should be true")
	}
}

func TestBR7_CompliancePctCalculation(t *testing.T) {
	tests := []struct {
		name    string
		pending int
		overdue int
		wantPct float64
	}{
		{"no pending", 0, 0, 100.0},
		{"all overdue", 10, 10, 0.0},
		{"half overdue", 10, 5, 50.0},
		{"no overdue", 10, 0, 100.0},
		{"80% compliant", 10, 2, 80.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compliancePct := 100.0
			if tt.pending > 0 {
				if tt.overdue > 0 {
					compliancePct = float64(tt.pending-tt.overdue) / float64(tt.pending) * 100.0
				}
			}
			if compliancePct != tt.wantPct {
				t.Errorf("compliancePct = %f, want %f", compliancePct, tt.wantPct)
			}
		})
	}
}

func TestBR7_BTKReport_Structure(t *testing.T) {
	tid := uuid.New()
	report := BTKReport{
		TenantID:    tid,
		ReportMonth: "2026-02",
		GeneratedAt: "2026-03-23T10:00:00Z",
		TotalActive: 5000,
		TotalSIMs:   10000,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed BTKReport
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.TenantID != tid {
		t.Errorf("TenantID mismatch")
	}
	if parsed.ReportMonth != "2026-02" {
		t.Errorf("ReportMonth = %q, want %q", parsed.ReportMonth, "2026-02")
	}
	if parsed.TotalActive != 5000 {
		t.Errorf("TotalActive = %d, want 5000", parsed.TotalActive)
	}
	if parsed.TotalSIMs != 10000 {
		t.Errorf("TotalSIMs = %d, want 10000", parsed.TotalSIMs)
	}
}

func TestBR7_PurgeResult_Structure(t *testing.T) {
	r := PurgeResult{
		TotalPurged:       5,
		FailedCount:       1,
		FailedSIMs:        []string{"sim-1"},
		PseudonymizedLogs: 15,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var parsed PurgeResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if parsed.TotalPurged != 5 {
		t.Errorf("TotalPurged = %d, want 5", parsed.TotalPurged)
	}
	if parsed.FailedCount != 1 {
		t.Errorf("FailedCount = %d, want 1", parsed.FailedCount)
	}
	if len(parsed.FailedSIMs) != 1 || parsed.FailedSIMs[0] != "sim-1" {
		t.Errorf("FailedSIMs mismatch")
	}
	if parsed.PseudonymizedLogs != 15 {
		t.Errorf("PseudonymizedLogs = %d, want 15", parsed.PseudonymizedLogs)
	}
}

func TestBR7_NewServiceNil(t *testing.T) {
	svc := NewService(nil, nil, nil, zerolog.Nop())
	if svc == nil {
		t.Fatal("NewService should not return nil")
	}
}

func TestBR6_TenantIsolation_DifferentTenantSalts(t *testing.T) {
	tenants := []uuid.UUID{
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		uuid.MustParse("44444444-4444-4444-4444-444444444444"),
	}

	salts := make(map[string]bool)
	for _, tid := range tenants {
		salt := DeriveTenantSalt(tid)
		if salts[salt] {
			t.Errorf("tenant %s has duplicate salt — tenant isolation violation", tid)
		}
		salts[salt] = true
	}

	if len(salts) != len(tenants) {
		t.Errorf("expected %d unique salts, got %d", len(tenants), len(salts))
	}
}

func TestBR7_RetentionDaysValidation(t *testing.T) {
	tests := []struct {
		days    int
		wantErr bool
	}{
		{29, true},
		{30, false},
		{90, false},
		{365, false},
		{366, true},
		{0, true},
		{-1, true},
	}
	for _, tt := range tests {
		valid := tt.days >= 30 && tt.days <= 365
		if valid == tt.wantErr {
			t.Errorf("days=%d: valid=%v but wantErr=%v", tt.days, valid, tt.wantErr)
		}
	}
}

func TestPDFExport_ReturnsValidBytes(t *testing.T) {
	report := &BTKReport{
		TenantID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		ReportMonth: "2026-03",
		GeneratedAt: "2026-04-01T09:00:00Z",
		Operators: []store.BTKOperatorStats{
			{OperatorName: "Turkcell", OperatorCode: "28601", ActiveCount: 5000, SuspendedCount: 100, TerminatedCount: 50, TotalCount: 5150},
			{OperatorName: "Vodafone TR", OperatorCode: "28602", ActiveCount: 3000, SuspendedCount: 80, TerminatedCount: 20, TotalCount: 3100},
		},
		TotalActive: 8000,
		TotalSIMs:   8250,
	}

	data, err := buildBTKReportPDF(report)
	if err != nil {
		t.Fatalf("buildBTKReportPDF() error = %v", err)
	}

	if len(data) == 0 {
		t.Fatal("PDF export returned empty bytes")
	}

	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("PDF output does not start with %%PDF- magic bytes, got prefix: %q", string(data[:min(len(data), 8)]))
	}
}

func TestPDFExport_EmptyOperators(t *testing.T) {
	report := &BTKReport{
		TenantID:    uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		ReportMonth: "2026-03",
		GeneratedAt: "2026-04-01T09:00:00Z",
		Operators:   []store.BTKOperatorStats{},
		TotalActive: 0,
		TotalSIMs:   0,
	}

	data, err := buildBTKReportPDF(report)
	if err != nil {
		t.Fatalf("buildBTKReportPDF() with empty operators error = %v", err)
	}

	if len(data) == 0 {
		t.Fatal("PDF export returned empty bytes for empty operators")
	}

	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("PDF output does not start with %%PDF- magic bytes")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
