package report

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/go-pdf/fpdf"
)

func (e *Engine) buildPDF(ctx context.Context, req Request) (*Artifact, error) {
	// FIX-229 Task 7: alerts export takes a dedicated landscape PDF builder
	// because its column layout and breakdown blocks differ from the standard
	// portrait sections/tables.
	if req.Type == ReportAlertsExport {
		filters := alertsExportFiltersFromMap(req.Filters)
		data, err := e.provider.AlertsExport(ctx, req.TenantID, filters)
		if err != nil {
			return nil, fmt.Errorf("alerts export data: %w", err)
		}
		return buildAlertsPDF(data)
	}

	ts := time.Now().UTC()

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AliasNbPages("")

	tenantLabel := fmt.Sprintf("Tenant: %s", req.TenantID.String())
	title := reportTitle(req.Type, req.Locale)

	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetFont("Arial", "I", 8)
		pdf.SetTextColor(128, 128, 128)
		pdf.CellFormat(0, 10, fmt.Sprintf("Page %d of {nb}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})

	pdf.AddPage()
	pageHeader(pdf, title, tenantLabel, ts)

	switch req.Type {
	case ReportKVKK:
		data, err := e.provider.KVKK(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("kvkk data: %w", err)
		}
		renderSectionsPDF(pdf, data.Sections)
		renderComplianceDisclaimer(pdf, req.Locale, "KVKK", ts)

	case ReportGDPR:
		data, err := e.provider.GDPR(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("gdpr data: %w", err)
		}
		renderSectionsPDF(pdf, data.Sections)
		renderComplianceDisclaimer(pdf, req.Locale, "GDPR", ts)

	case ReportBTK:
		data, err := e.provider.BTK(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("btk data: %w", err)
		}
		renderSectionsPDF(pdf, data.Sections)
		renderComplianceDisclaimer(pdf, req.Locale, "BTK", ts)

	case ReportSLAMonthly:
		data, err := e.provider.SLAMonthly(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("sla data: %w", err)
		}
		renderTabularPDF(pdf, data.Columns, data.Rows, data.Summary)

	case ReportUsageSummary:
		data, err := e.provider.UsageSummary(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("usage data: %w", err)
		}
		renderTabularPDF(pdf, data.Columns, data.Rows, data.Summary)

	case ReportCostAnalysis:
		data, err := e.provider.CostAnalysis(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("cost data: %w", err)
		}
		renderTabularPDF(pdf, data.Columns, data.Rows, data.Summary)

	case ReportAuditExport:
		data, err := e.provider.AuditExport(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("audit data: %w", err)
		}
		renderTabularPDF(pdf, data.Columns, data.Rows, data.Summary)

	case ReportSIMInventory:
		data, err := e.provider.SIMInventory(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("sim inventory data: %w", err)
		}
		renderTabularPDF(pdf, data.Columns, data.Rows, data.Summary)

	case ReportUnverifiedDevices:
		data, err := e.provider.UnverifiedDevices(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("unverified devices data: %w", err)
		}
		renderTabularPDF(pdf, data.Columns, data.Rows, data.Summary)

	default:
		return nil, fmt.Errorf("unsupported report type for pdf: %q", req.Type)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.pdf", req.Type, ts.Format("20060102-150405"))
	return &Artifact{
		Bytes:    buf.Bytes(),
		MIME:     "application/pdf",
		Filename: filename,
	}, nil
}

func pageHeader(pdf *fpdf.Fpdf, title, tenantLabel string, generatedAt time.Time) {
	pdf.SetFont("Arial", "B", 16)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 10, title, "", 1, "C", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(0, 7, tenantLabel, "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "I", 9)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(0, 6, fmt.Sprintf("Generated: %s", generatedAt.Format("2006-01-02 15:04:05 UTC")), "", 1, "R", false, 0, "")
	pdf.SetTextColor(0, 0, 0)

	pdf.SetDrawColor(180, 180, 180)
	pdf.Line(pdf.GetX(), pdf.GetY(), pdf.GetX()+170, pdf.GetY())
	pdf.Ln(4)
}

func renderSectionsPDF(pdf *fpdf.Fpdf, sections []Section) {
	for _, sec := range sections {
		pdf.SetFont("Arial", "B", 11)
		pdf.SetFillColor(230, 235, 245)
		pdf.CellFormat(0, 8, sec.Title, "1", 1, "L", true, 0, "")
		pdf.Ln(1)

		pdf.SetFont("Arial", "", 9)
		pdf.SetFillColor(255, 255, 255)
		for i, row := range sec.Rows {
			if i%2 == 0 {
				pdf.SetFillColor(248, 248, 252)
			} else {
				pdf.SetFillColor(255, 255, 255)
			}
			colW := colWidthsForRow(len(row))
			for j, cell := range row {
				pdf.CellFormat(colW[j], 7, cell, "1", 0, "L", true, 0, "")
			}
			pdf.Ln(-1)
		}

		if len(sec.Summary) > 0 {
			pdf.Ln(2)
			pdf.SetFont("Arial", "I", 9)
			pdf.SetTextColor(80, 80, 80)
			for _, s := range sec.Summary {
				pdf.CellFormat(0, 6, s, "", 1, "L", false, 0, "")
			}
			pdf.SetTextColor(0, 0, 0)
		}
		pdf.Ln(4)
	}
}

func renderTabularPDF(pdf *fpdf.Fpdf, columns []string, rows [][]string, summary map[string]string) {
	if len(columns) == 0 {
		return
	}

	colW := colWidthsForRow(len(columns))

	pdf.SetFont("Arial", "B", 9)
	pdf.SetFillColor(40, 40, 60)
	pdf.SetTextColor(255, 255, 255)
	for i, h := range columns {
		pdf.CellFormat(colW[i], 8, h, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(0, 0, 0)
	for i, row := range rows {
		if i%2 == 0 {
			pdf.SetFillColor(248, 248, 252)
		} else {
			pdf.SetFillColor(255, 255, 255)
		}
		for j, cell := range row {
			if j < len(colW) {
				pdf.CellFormat(colW[j], 7, cell, "1", 0, "L", true, 0, "")
			}
		}
		pdf.Ln(-1)
	}

	if len(summary) > 0 {
		pdf.Ln(4)
		pdf.SetFont("Arial", "B", 9)
		for k, v := range summary {
			pdf.CellFormat(50, 7, k+":", "", 0, "L", false, 0, "")
			pdf.CellFormat(0, 7, v, "", 1, "L", false, 0, "")
		}
	}
}

func renderComplianceDisclaimer(pdf *fpdf.Fpdf, locale, regulation string, ts time.Time) {
	pdf.Ln(6)
	pdf.SetDrawColor(200, 200, 200)
	pdf.Line(pdf.GetX(), pdf.GetY(), pdf.GetX()+170, pdf.GetY())
	pdf.Ln(3)
	pdf.SetFont("Arial", "I", 8)
	pdf.SetTextColor(120, 120, 120)

	var text string
	if locale == "tr" {
		text = fmt.Sprintf("Bu rapor Argus tarafindan otomatik olarak olusturulmustur (%s). %s gerekliliklerini karsilamak amaciyla hazirlanmistir.",
			ts.Format("02.01.2006 15:04"), regulation)
	} else {
		text = fmt.Sprintf("This report was automatically generated by Argus on %s to satisfy %s compliance requirements.",
			ts.Format("2006-01-02 15:04 UTC"), regulation)
	}

	pdf.MultiCell(0, 5, text, "", "L", false)
	pdf.SetTextColor(0, 0, 0)
}

func colWidthsForRow(n int) []float64 {
	if n == 0 {
		return nil
	}
	const totalWidth = 170.0
	w := totalWidth / float64(n)
	widths := make([]float64, n)
	for i := range widths {
		widths[i] = w
	}
	return widths
}

func reportTitle(rt ReportType, locale string) string {
	titles := map[ReportType][2]string{
		ReportKVKK:              {"KVKK Uyum Raporu", "KVKK Compliance Report"},
		ReportGDPR:              {"GDPR Uyum Raporu", "GDPR Compliance Report"},
		ReportBTK:               {"BTK Aylik SIM Raporu", "BTK Monthly SIM Report"},
		ReportSLAMonthly:        {"Aylik SLA Raporu", "Monthly SLA Report"},
		ReportUsageSummary:      {"Kullanim Ozeti Raporu", "Usage Summary Report"},
		ReportCostAnalysis:      {"Maliyet Analizi Raporu", "Cost Analysis Report"},
		ReportAuditExport:       {"Denetim Kaydi Ihracati", "Audit Log Export"},
		ReportSIMInventory:      {"SIM Envanter Raporu", "SIM Inventory Report"},
		ReportUnverifiedDevices: {"Dogrulanmamis Cihazlar Raporu", "Unverified Devices Report"},
	}
	if pair, ok := titles[rt]; ok {
		if locale == "tr" {
			return pair[0]
		}
		return pair[1]
	}
	return string(rt)
}
