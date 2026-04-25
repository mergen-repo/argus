package report

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// alertsPDFTitleMaxChars truncates the alert title column to keep rows on a
// single line. Tuned against the column width chosen below.
const alertsPDFTitleMaxChars = 60

// buildAlertsPDF renders an A4 portrait PDF with header (tenant, generated
// timestamp, filter summary, severity + state breakdowns), a tabular body of
// up to 200 rows, and a truncation footer when the total result set is larger.
//
// FIX-229 Task 7 (DEV-338) — driven via Engine.Build with ReportAlertsExport.
func buildAlertsPDF(data *AlertsExportData) (*Artifact, error) {
	if data == nil {
		return nil, fmt.Errorf("alerts pdf: nil data")
	}

	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.AliasNbPages("")

	tenantLabel := fmt.Sprintf("Tenant: %s", data.TenantID.String())
	title := "Alerts Export"
	ts := data.GeneratedAt
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Arial", "I", 8)
		pdf.SetTextColor(128, 128, 128)
		pdf.CellFormat(0, 6, fmt.Sprintf("Page %d of {nb}", pdf.PageNo()), "", 0, "C", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
	})

	pdf.AddPage()
	alertsHeaderBlock(pdf, title, tenantLabel, ts, data)
	alertsRowsTable(pdf, data.Rows)
	alertsTruncationFooter(pdf, data)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("alerts pdf: write: %w", err)
	}

	filename := fmt.Sprintf("alerts-%s.pdf", ts.Format("20060102-150405"))
	return &Artifact{
		Bytes:    buf.Bytes(),
		MIME:     "application/pdf",
		Filename: filename,
	}, nil
}

func alertsHeaderBlock(pdf *fpdf.Fpdf, title, tenantLabel string, ts time.Time, data *AlertsExportData) {
	pdf.SetFont("Arial", "B", 16)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 9, title, "", 1, "C", false, 0, "")
	pdf.Ln(1)

	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(0, 6, tenantLabel, "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "I", 9)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(0, 5, fmt.Sprintf("Generated: %s", ts.Format("2006-01-02 15:04:05 UTC")), "", 1, "R", false, 0, "")
	pdf.SetTextColor(0, 0, 0)

	pdf.SetDrawColor(180, 180, 180)
	x, y := pdf.GetX(), pdf.GetY()
	pdf.Line(x, y, x+277, y)
	pdf.Ln(3)

	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(60, 6, "Total alerts:", "", 0, "L", false, 0, "")
	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(0, 6, strconv.Itoa(data.TotalRows), "", 1, "L", false, 0, "")

	if data.FilterDescription != "" {
		pdf.SetFont("Arial", "I", 9)
		pdf.SetTextColor(80, 80, 80)
		pdf.MultiCell(0, 5, data.FilterDescription, "", "L", false)
		pdf.SetTextColor(0, 0, 0)
	}
	pdf.Ln(2)

	alertsBreakdownBlock(pdf, "Severity", data.SeverityBreakdown)
	alertsBreakdownBlock(pdf, "State", data.StateBreakdown)
	pdf.Ln(2)
}

func alertsBreakdownBlock(pdf *fpdf.Fpdf, label string, counts map[string]int) {
	if len(counts) == 0 {
		return
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pdf.SetFont("Arial", "B", 9)
	pdf.SetFillColor(40, 40, 60)
	pdf.SetTextColor(255, 255, 255)
	pdf.CellFormat(40, 6, label, "1", 0, "L", true, 0, "")
	for _, k := range keys {
		pdf.CellFormat(28, 6, k, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(0, 0, 0)
	pdf.SetFillColor(248, 248, 252)
	pdf.CellFormat(40, 6, "Count", "1", 0, "L", true, 0, "")
	for _, k := range keys {
		pdf.CellFormat(28, 6, strconv.Itoa(counts[k]), "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
	pdf.Ln(2)
}

func alertsRowsTable(pdf *fpdf.Fpdf, rows []AlertExportRow) {
	if len(rows) == 0 {
		pdf.SetFont("Arial", "I", 10)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(0, 8, "No alert rows.", "", 1, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		return
	}

	headers := []string{"Fired (UTC)", "Severity", "State", "Source", "Type", "Title", "Operator", "SIM"}
	widths := []float64{40, 22, 22, 22, 38, 70, 35, 28}

	pdf.SetFont("Arial", "B", 9)
	pdf.SetFillColor(40, 40, 60)
	pdf.SetTextColor(255, 255, 255)
	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, h, "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(0, 0, 0)
	for i, r := range rows {
		if i%2 == 0 {
			pdf.SetFillColor(248, 248, 252)
		} else {
			pdf.SetFillColor(255, 255, 255)
		}
		cells := []string{
			r.FiredAt,
			r.Severity,
			r.State,
			r.Source,
			truncateString(r.Type, 22),
			truncateString(r.Title, alertsPDFTitleMaxChars),
			truncateString(r.OperatorName, 20),
			truncateString(r.SimICCID, 18),
		}
		for j, cell := range cells {
			pdf.CellFormat(widths[j], 6, cell, "1", 0, "L", true, 0, "")
		}
		pdf.Ln(-1)
	}
}

func alertsTruncationFooter(pdf *fpdf.Fpdf, data *AlertsExportData) {
	if data.TruncatedToFirst <= 0 {
		return
	}
	pdf.Ln(4)
	pdf.SetFont("Arial", "B", 9)
	pdf.SetTextColor(180, 50, 50)
	msg := fmt.Sprintf("Showing first %d of %d alerts. Narrow filters to view more.",
		data.TruncatedToFirst, data.TotalRows)
	pdf.MultiCell(0, 5, msg, "", "L", false)
	pdf.SetTextColor(0, 0, 0)
}

// truncateString trims s to at most n runes, appending an ellipsis when
// truncation occurs. Pure ASCII ellipsis (...) is used to keep fpdf happy
// without needing an extended font.
func truncateString(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len([]rune(s)) <= n {
		return s
	}
	rs := []rune(s)
	if n <= 3 {
		return string(rs[:n])
	}
	return strings.TrimSpace(string(rs[:n-3])) + "..."
}
