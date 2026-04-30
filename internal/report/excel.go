package report

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/xuri/excelize/v2"
)

const xlsxMIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func (e *Engine) buildExcel(ctx context.Context, req Request) (*Artifact, error) {
	ts := time.Now().UTC()
	f := excelize.NewFile()
	defer f.Close()

	metaIdx, err := f.NewSheet("Meta")
	if err != nil {
		return nil, fmt.Errorf("create meta sheet: %w", err)
	}

	writeMetaSheet(f, "Meta", req, ts)

	switch req.Type {
	case ReportKVKK:
		data, err := e.provider.KVKK(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("kvkk data: %w", err)
		}
		if err := writeComplianceSections(f, data.Sections); err != nil {
			return nil, err
		}

	case ReportGDPR:
		data, err := e.provider.GDPR(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("gdpr data: %w", err)
		}
		if err := writeComplianceSections(f, data.Sections); err != nil {
			return nil, err
		}

	case ReportBTK:
		data, err := e.provider.BTK(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("btk data: %w", err)
		}
		if err := writeComplianceSections(f, data.Sections); err != nil {
			return nil, err
		}

	case ReportSLAMonthly:
		data, err := e.provider.SLAMonthly(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("sla data: %w", err)
		}
		if err := writeSLASheets(f, data); err != nil {
			return nil, err
		}

	case ReportUsageSummary:
		data, err := e.provider.UsageSummary(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("usage data: %w", err)
		}
		if err := writeTabularSheet(f, "Data", data.Columns, data.Rows, data.Summary); err != nil {
			return nil, err
		}

	case ReportCostAnalysis:
		data, err := e.provider.CostAnalysis(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("cost data: %w", err)
		}
		if err := writeTabularSheet(f, "Data", data.Columns, data.Rows, data.Summary); err != nil {
			return nil, err
		}

	case ReportAuditExport:
		data, err := e.provider.AuditExport(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("audit data: %w", err)
		}
		if err := writeTabularSheet(f, "Data", data.Columns, data.Rows, data.Summary); err != nil {
			return nil, err
		}

	case ReportSIMInventory:
		data, err := e.provider.SIMInventory(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("sim inventory data: %w", err)
		}
		if err := writeTabularSheet(f, "Data", data.Columns, data.Rows, data.Summary); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unsupported report type for xlsx: %q", req.Type)
	}

	if err := f.DeleteSheet("Sheet1"); err != nil {
		_ = err
	}

	f.SetActiveSheet(metaIdx)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.xlsx", req.Type, ts.Format("20060102-150405"))
	return &Artifact{
		Bytes:    buf.Bytes(),
		MIME:     xlsxMIME,
		Filename: filename,
	}, nil
}

func writeMetaSheet(f *excelize.File, sheet string, req Request, ts time.Time) {
	_ = f.SetCellValue(sheet, "A1", "Field")
	_ = f.SetCellValue(sheet, "B1", "Value")

	rows := [][]string{
		{"Report Type", string(req.Type)},
		{"Tenant ID", req.TenantID.String()},
		{"Generated At", ts.Format("2006-01-02 15:04:05 UTC")},
		{"Locale", req.Locale},
	}

	keys := make([]string, 0, len(req.Filters))
	for k := range req.Filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rows = append(rows, []string{k, fmt.Sprintf("%v", req.Filters[k])})
	}

	for i, row := range rows {
		rowNum := i + 2
		_ = f.SetCellValue(sheet, cellRef("A", rowNum), row[0])
		_ = f.SetCellValue(sheet, cellRef("B", rowNum), row[1])
	}
}

func writeComplianceSections(f *excelize.File, sections []Section) error {
	for _, sec := range sections {
		name := truncateSheetName(sec.Title)
		_, err := f.NewSheet(name)
		if err != nil {
			return fmt.Errorf("create sheet %q: %w", name, err)
		}

		rowNum := 1
		for _, row := range sec.Rows {
			for col, cell := range row {
				coord := fmt.Sprintf("%s%d", colLetter(col), rowNum)
				_ = f.SetCellValue(name, coord, cell)
			}
			rowNum++
		}

		if len(sec.Summary) > 0 {
			rowNum++
			_ = f.SetCellValue(name, cellRef("A", rowNum), "Summary")
			rowNum++
			for _, s := range sec.Summary {
				_ = f.SetCellValue(name, cellRef("A", rowNum), s)
				rowNum++
			}
		}
	}
	return nil
}

func writeSLASheets(f *excelize.File, data *SLAData) error {
	if _, err := f.NewSheet("Summary"); err != nil {
		return fmt.Errorf("create Summary sheet: %w", err)
	}
	_ = f.SetCellValue("Summary", "A1", "Key")
	_ = f.SetCellValue("Summary", "B1", "Value")
	keys := make([]string, 0, len(data.Summary))
	for k := range data.Summary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		_ = f.SetCellValue("Summary", cellRef("A", i+2), k)
		_ = f.SetCellValue("Summary", cellRef("B", i+2), data.Summary[k])
	}

	if _, err := f.NewSheet("Per-SIM"); err != nil {
		return fmt.Errorf("create Per-SIM sheet: %w", err)
	}
	writeColumnsAndRows(f, "Per-SIM", data.Columns, data.Rows)

	if _, err := f.NewSheet("Breach Log"); err != nil {
		return fmt.Errorf("create Breach Log sheet: %w", err)
	}
	breachRows := filterBreachRows(data.Columns, data.Rows)
	writeColumnsAndRows(f, "Breach Log", data.Columns, breachRows)

	return nil
}

func filterBreachRows(columns []string, rows [][]string) [][]string {
	statusIdx := -1
	for i, c := range columns {
		if c == "Status" {
			statusIdx = i
			break
		}
	}

	if statusIdx < 0 {
		return rows
	}

	var out [][]string
	for _, row := range rows {
		if statusIdx < len(row) && row[statusIdx] != "PASS" {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return rows
	}
	return out
}

func writeTabularSheet(f *excelize.File, sheet string, columns []string, rows [][]string, summary map[string]string) error {
	_, err := f.NewSheet(sheet)
	if err != nil {
		return fmt.Errorf("create %q sheet: %w", sheet, err)
	}
	rowNum := writeColumnsAndRows(f, sheet, columns, rows)

	if len(summary) > 0 {
		rowNum++
		_ = f.SetCellValue(sheet, cellRef("A", rowNum), "Summary")
		rowNum++
		keys := make([]string, 0, len(summary))
		for k := range summary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_ = f.SetCellValue(sheet, cellRef("A", rowNum), k)
			_ = f.SetCellValue(sheet, cellRef("B", rowNum), summary[k])
			rowNum++
		}
	}
	return nil
}

func writeColumnsAndRows(f *excelize.File, sheet string, columns []string, rows [][]string) int {
	rowNum := 1
	for col, header := range columns {
		coord := fmt.Sprintf("%s%d", colLetter(col), rowNum)
		_ = f.SetCellValue(sheet, coord, header)
	}
	rowNum++
	for _, row := range rows {
		for col, cell := range row {
			coord := fmt.Sprintf("%s%d", colLetter(col), rowNum)
			_ = f.SetCellValue(sheet, coord, cell)
		}
		rowNum++
	}
	return rowNum - 1
}

func truncateSheetName(s string) string {
	runes := []rune(s)
	if len(runes) > 31 {
		return string(runes[:31])
	}
	return s
}

func cellRef(col string, row int) string {
	return fmt.Sprintf("%s%d", col, row)
}

func colLetter(idx int) string {
	if idx < 26 {
		return string(rune('A' + idx))
	}
	return string(rune('A'+idx/26-1)) + string(rune('A'+idx%26))
}
