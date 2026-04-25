package report

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeProvider struct{}

func (f *fakeProvider) KVKK(_ context.Context, tenantID uuid.UUID, _ map[string]any) (*KVKKData, error) {
	return &KVKKData{
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Sections: []Section{
			{
				Title: "Data Inventory",
				Rows:  [][]string{{"MSISDN", "stored", "encrypted"}, {"IMSI", "stored", "hashed"}},
				Summary: []string{"Total data categories: 2"},
			},
		},
	}, nil
}

func (f *fakeProvider) GDPR(_ context.Context, tenantID uuid.UUID, _ map[string]any) (*GDPRData, error) {
	return &GDPRData{
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Sections: []Section{
			{
				Title: "Lawful Basis",
				Rows:  [][]string{{"Processing", "Consent", "Active"}, {"Storage", "Contract", "Active"}},
				Summary: []string{"All processing activities have lawful basis"},
			},
		},
	}, nil
}

func (f *fakeProvider) BTK(_ context.Context, tenantID uuid.UUID, _ map[string]any) (*BTKData, error) {
	return &BTKData{
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Sections: []Section{
			{
				Title: "Operator Stats",
				Rows:  [][]string{{"Turkcell", "TUR01", "500", "10", "2", "512"}},
				Summary: []string{"Total SIMs: 512"},
			},
		},
	}, nil
}

func (f *fakeProvider) SLAMonthly(_ context.Context, _ uuid.UUID, _ map[string]any) (*SLAData, error) {
	return &SLAData{
		Columns:    []string{"Service", "Uptime%", "Target%", "Status"},
		Rows:       [][]string{{"RADIUS", "99.95", "99.9", "PASS"}, {"API", "99.80", "99.9", "FAIL"}},
		Summary:    map[string]string{"Overall": "1 breach"},
		PeriodFrom: time.Now().AddDate(0, -1, 0),
		PeriodTo:   time.Now(),
	}, nil
}

func (f *fakeProvider) UsageSummary(_ context.Context, _ uuid.UUID, _ map[string]any) (*UsageData, error) {
	return &UsageData{
		Columns:    []string{"MSISDN", "DataMB", "SMSCount", "VoiceMin"},
		Rows:       [][]string{{"905001234567", "1024", "50", "120"}, {"905007654321", "512", "10", "60"}},
		Summary:    map[string]string{"TotalDataMB": "1536"},
		PeriodFrom: time.Now().AddDate(0, -1, 0),
		PeriodTo:   time.Now(),
	}, nil
}

func (f *fakeProvider) CostAnalysis(_ context.Context, _ uuid.UUID, _ map[string]any) (*CostData, error) {
	return &CostData{
		Columns:    []string{"Operator", "DataCost", "SMSCost", "TotalCost"},
		Rows:       [][]string{{"Turkcell", "100.00", "20.00", "120.00"}, {"Vodafone", "80.00", "15.00", "95.00"}},
		Summary:    map[string]string{"GrandTotal": "215.00"},
		PeriodFrom: time.Now().AddDate(0, -1, 0),
		PeriodTo:   time.Now(),
	}, nil
}

func (f *fakeProvider) AuditExport(_ context.Context, _ uuid.UUID, _ map[string]any) (*AuditExportData, error) {
	return &AuditExportData{
		Columns:    []string{"Timestamp", "Actor", "Action", "Entity", "EntityID"},
		Rows:       [][]string{{"2026-04-01T10:00:00Z", "admin", "update", "sim", "uuid-1"}, {"2026-04-01T11:00:00Z", "api", "delete", "sim", "uuid-2"}},
		Summary:    map[string]string{"TotalEvents": "2"},
		PeriodFrom: time.Now().AddDate(0, -1, 0),
		PeriodTo:   time.Now(),
	}, nil
}

func (f *fakeProvider) SIMInventory(_ context.Context, _ uuid.UUID, _ map[string]any) (*SIMInventoryData, error) {
	return &SIMInventoryData{
		Columns:    []string{"ICCID", "MSISDN", "Status", "Operator"},
		Rows:       [][]string{{"8949012345678901234", "905001234567", "active", "Turkcell"}, {"8949012345678901235", "905007654321", "suspended", "Vodafone"}},
		Summary:    map[string]string{"Total": "2", "Active": "1"},
		PeriodFrom: time.Now().AddDate(0, -1, 0),
		PeriodTo:   time.Now(),
	}, nil
}

func (f *fakeProvider) AlertsExport(_ context.Context, tenantID uuid.UUID, _ AlertsExportFilters) (*AlertsExportData, error) {
	return &AlertsExportData{
		GeneratedAt:       time.Now().UTC(),
		TenantID:          tenantID,
		Filters:           map[string]string{},
		FilterDescription: "No filters applied",
		TotalRows:         2,
		SeverityBreakdown: map[string]int{"high": 1, "critical": 1},
		StateBreakdown:    map[string]int{"open": 2},
		Rows: []AlertExportRow{
			{ID: "a1", Severity: "high", State: "open", Source: "sim", Type: "sim.data_spike", Title: "Row 1", FiredAt: "2026-04-25 10:00:00 UTC"},
			{ID: "a2", Severity: "critical", State: "open", Source: "operator", Type: "operator.down", Title: "Row 2", FiredAt: "2026-04-25 11:00:00 UTC"},
		},
	}, nil
}

func TestEngine(t *testing.T) {
	tenantID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	engine := NewEngine(&fakeProvider{})
	ctx := context.Background()

	reportTypes := []ReportType{
		ReportKVKK,
		ReportGDPR,
		ReportBTK,
		ReportSLAMonthly,
		ReportUsageSummary,
		ReportCostAnalysis,
		ReportAuditExport,
		ReportSIMInventory,
	}

	for _, rt := range reportTypes {
		t.Run(string(rt)+"/csv", func(t *testing.T) {
			req := Request{
				Type:     rt,
				Format:   FormatCSV,
				TenantID: tenantID,
				Filters:  map[string]any{},
				Locale:   "en",
			}
			artifact, err := engine.Build(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(artifact.Bytes) == 0 {
				t.Fatal("empty bytes")
			}
			if !bytes.HasPrefix(artifact.Bytes, []byte("#")) {
				t.Fatalf("CSV should start with '#', got: %q", string(artifact.Bytes[:min(20, len(artifact.Bytes))]))
			}
			if !strings.Contains(string(artifact.Bytes), "Report:") {
				t.Fatal("CSV missing 'Report:' metadata")
			}
			if artifact.MIME != "text/csv" {
				t.Fatalf("expected MIME text/csv, got %q", artifact.MIME)
			}
			if artifact.Extension() != ".csv" {
				t.Fatalf("expected extension .csv, got %q", artifact.Extension())
			}
			if !strings.HasSuffix(artifact.Filename, ".csv") {
				t.Fatalf("filename should end with .csv, got %q", artifact.Filename)
			}
		})

		t.Run(string(rt)+"/pdf", func(t *testing.T) {
			req := Request{
				Type:     rt,
				Format:   FormatPDF,
				TenantID: tenantID,
				Filters:  map[string]any{},
				Locale:   "tr",
			}
			artifact, err := engine.Build(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(artifact.Bytes) == 0 {
				t.Fatal("empty bytes")
			}
			if !bytes.HasPrefix(artifact.Bytes, []byte("%PDF-")) {
				t.Fatalf("PDF should start with '%%PDF-', got: %q", string(artifact.Bytes[:min(10, len(artifact.Bytes))]))
			}
			if artifact.MIME != "application/pdf" {
				t.Fatalf("expected MIME application/pdf, got %q", artifact.MIME)
			}
			if artifact.Extension() != ".pdf" {
				t.Fatalf("expected extension .pdf, got %q", artifact.Extension())
			}
			if !strings.HasSuffix(artifact.Filename, ".pdf") {
				t.Fatalf("filename should end with .pdf, got %q", artifact.Filename)
			}
		})
	}

}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
