package report

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func TestEngineExcel(t *testing.T) {
	tenantID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	engine := NewEngine(&fakeProvider{})
	ctx := context.Background()

	cases := []struct {
		rt              ReportType
		minSheets       int
		expectDataSheet string
		isCompliance    bool
		numSections     int
	}{
		{ReportKVKK, 2, "", true, 1},
		{ReportGDPR, 2, "", true, 1},
		{ReportBTK, 2, "", true, 1},
		{ReportSLAMonthly, 4, "Per-SIM", false, 0},
		{ReportUsageSummary, 2, "Data", false, 0},
		{ReportCostAnalysis, 2, "Data", false, 0},
		{ReportAuditExport, 2, "Data", false, 0},
		{ReportSIMInventory, 2, "Data", false, 0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.rt)+"/xlsx", func(t *testing.T) {
			req := Request{
				Type:     tc.rt,
				Format:   FormatXLSX,
				TenantID: tenantID,
				Filters:  map[string]any{"month": "2026-04"},
				Locale:   "en",
			}
			artifact, err := engine.Build(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(artifact.Bytes) == 0 {
				t.Fatal("empty bytes")
			}
			if artifact.MIME != xlsxMIME {
				t.Fatalf("expected MIME %q, got %q", xlsxMIME, artifact.MIME)
			}
			if artifact.Extension() != ".xlsx" {
				t.Fatalf("expected extension .xlsx, got %q", artifact.Extension())
			}
			if !strings.HasSuffix(artifact.Filename, ".xlsx") {
				t.Fatalf("filename should end with .xlsx, got %q", artifact.Filename)
			}

			wb, err := excelize.OpenReader(bytes.NewReader(artifact.Bytes))
			if err != nil {
				t.Fatalf("excelize.OpenReader: %v", err)
			}
			defer wb.Close()

			sheets := wb.GetSheetList()
			if len(sheets) == 0 {
				t.Fatal("no sheets in workbook")
			}
			if len(sheets) < tc.minSheets {
				t.Fatalf("expected at least %d sheets, got %d: %v", tc.minSheets, len(sheets), sheets)
			}

			metaFound := false
			for _, s := range sheets {
				if s == "Meta" {
					metaFound = true
					break
				}
			}
			if !metaFound {
				t.Fatalf("Meta sheet missing; sheets: %v", sheets)
			}

			a1, _ := wb.GetCellValue("Meta", "A1")
			b1, _ := wb.GetCellValue("Meta", "B1")
			if a1 != "Field" || b1 != "Value" {
				t.Fatalf("Meta sheet header mismatch: A1=%q B1=%q", a1, b1)
			}

			a2, _ := wb.GetCellValue("Meta", "A2")
			if a2 == "" {
				t.Fatal("Meta sheet has no data rows")
			}

			if tc.isCompliance {
				if len(sheets) < 1+tc.numSections {
					t.Fatalf("compliance report should have Meta + %d section sheets, got %d sheets: %v",
						tc.numSections, len(sheets), sheets)
				}
			}

			if tc.expectDataSheet != "" {
				dataFound := false
				for _, s := range sheets {
					if s == tc.expectDataSheet {
						dataFound = true
						break
					}
				}
				if !dataFound {
					t.Fatalf("expected data sheet %q not found; sheets: %v", tc.expectDataSheet, sheets)
				}

				a1data, _ := wb.GetCellValue(tc.expectDataSheet, "A1")
				if a1data == "" {
					t.Fatalf("data sheet %q appears empty (A1 is empty)", tc.expectDataSheet)
				}
			}
		})
	}
}
