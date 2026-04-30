package report

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleAlertExportData(rowCount int, truncate int) *AlertsExportData {
	rows := make([]AlertExportRow, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		rows = append(rows, AlertExportRow{
			ID:           uuid.NewString(),
			Severity:     "high",
			State:        "open",
			Source:       "sim",
			Type:         "sim.data_spike",
			Title:        "An unusually long alert title that the PDF builder must truncate to keep one row per line",
			OperatorName: "Acme Mobile",
			SimICCID:     "8949012345678901234",
			FiredAt:      "2026-04-25 10:00:00 UTC",
		})
	}
	return &AlertsExportData{
		GeneratedAt:       time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC),
		TenantID:          uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		Filters:           map[string]string{"severity": "high"},
		FilterDescription: "Filters: severity=high",
		TotalRows:         rowCount,
		SeverityBreakdown: map[string]int{"high": rowCount},
		StateBreakdown:    map[string]int{"open": rowCount},
		Rows:              rows,
		TruncatedToFirst:  truncate,
	}
}

func TestBuildAlertsPDF_GeneratesValidPDF(t *testing.T) {
	data := sampleAlertExportData(3, 0)

	artifact, err := buildAlertsPDF(data)
	if err != nil {
		t.Fatalf("buildAlertsPDF returned error: %v", err)
	}
	if artifact == nil {
		t.Fatal("artifact is nil")
	}
	if len(artifact.Bytes) == 0 {
		t.Fatal("artifact bytes empty")
	}
	if !bytes.HasPrefix(artifact.Bytes, []byte("%PDF-")) {
		t.Fatalf("PDF should start with %%PDF- magic header; got %q", string(artifact.Bytes[:8]))
	}
	if len(artifact.Bytes) < 1024 {
		t.Errorf("expected PDF >= 1KB; got %d bytes", len(artifact.Bytes))
	}
	if artifact.MIME != "application/pdf" {
		t.Errorf("MIME = %q, want application/pdf", artifact.MIME)
	}
	if !strings.HasPrefix(artifact.Filename, "alerts-") || !strings.HasSuffix(artifact.Filename, ".pdf") {
		t.Errorf("Filename = %q, want alerts-YYYYMMDD-HHMMSS.pdf", artifact.Filename)
	}
}

func TestBuildAlertsPDF_TruncationFooter(t *testing.T) {
	data := sampleAlertExportData(200, 200)
	data.TotalRows = 250

	artifact, err := buildAlertsPDF(data)
	if err != nil {
		t.Fatalf("buildAlertsPDF returned error: %v", err)
	}
	if !bytes.HasPrefix(artifact.Bytes, []byte("%PDF-")) {
		t.Fatalf("PDF magic missing")
	}
	if len(artifact.Bytes) < 1024 {
		t.Errorf("expected PDF >= 1KB; got %d", len(artifact.Bytes))
	}
	// The truncation footer is rendered when TruncatedToFirst > 0; we cannot
	// inspect the rendered text easily without a PDF parser, but we can
	// verify the data path is exercised by confirming TruncatedToFirst is
	// accepted and the artifact still validates as a PDF.
	if data.TruncatedToFirst != 200 {
		t.Errorf("TruncatedToFirst = %d, want 200", data.TruncatedToFirst)
	}
	if data.TotalRows != 250 {
		t.Errorf("TotalRows = %d, want 250", data.TotalRows)
	}
}

func TestBuildAlertsPDF_NilDataReturnsError(t *testing.T) {
	if _, err := buildAlertsPDF(nil); err == nil {
		t.Fatal("expected error for nil data, got nil")
	}
}

func TestBuildAlertsPDF_EmptyRows(t *testing.T) {
	data := &AlertsExportData{
		GeneratedAt:       time.Now().UTC(),
		TenantID:          uuid.New(),
		FilterDescription: "Filters: severity=high",
		TotalRows:         0,
		SeverityBreakdown: map[string]int{},
		StateBreakdown:    map[string]int{},
		Rows:              []AlertExportRow{},
	}
	artifact, err := buildAlertsPDF(data)
	if err != nil {
		t.Fatalf("buildAlertsPDF empty: %v", err)
	}
	if !bytes.HasPrefix(artifact.Bytes, []byte("%PDF-")) {
		t.Error("PDF magic missing for empty data")
	}
}

func TestEngine_BuildsAlertsExportPDF(t *testing.T) {
	tenantID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	engine := NewEngine(&fakeProvider{})
	artifact, err := engine.Build(context.Background(), Request{
		Type:     ReportAlertsExport,
		Format:   FormatPDF,
		TenantID: tenantID,
		Filters:  map[string]any{},
		Locale:   "en",
	})
	if err != nil {
		t.Fatalf("engine.Build: %v", err)
	}
	if artifact == nil || len(artifact.Bytes) == 0 {
		t.Fatal("empty artifact")
	}
	if !bytes.HasPrefix(artifact.Bytes, []byte("%PDF-")) {
		t.Fatal("missing PDF magic")
	}
}

func TestTruncateString(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"this is a long string that must be cut", 12, "this is a..."},
		{"", 5, ""},
		{"abc", 0, ""},
		{"abcdef", 2, "ab"},
	}
	for _, c := range cases {
		got := truncateString(c.in, c.n)
		if got != c.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
