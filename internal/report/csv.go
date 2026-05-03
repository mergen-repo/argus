package report

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"
)

func (e *Engine) buildCSV(ctx context.Context, req Request) (*Artifact, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	ts := time.Now().UTC()
	w.Write([]string{fmt.Sprintf("# Report: %s | Tenant: %s | Generated: %s | Locale: %s",
		req.Type, req.TenantID.String(), ts.Format("2006-01-02T15:04:05Z"), req.Locale)})

	switch req.Type {
	case ReportKVKK:
		data, err := e.provider.KVKK(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("kvkk data: %w", err)
		}
		writeSectionsCSV(w, data.Sections)

	case ReportGDPR:
		data, err := e.provider.GDPR(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("gdpr data: %w", err)
		}
		writeSectionsCSV(w, data.Sections)

	case ReportBTK:
		data, err := e.provider.BTK(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("btk data: %w", err)
		}
		writeSectionsCSV(w, data.Sections)

	case ReportSLAMonthly:
		data, err := e.provider.SLAMonthly(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("sla data: %w", err)
		}
		writeTabularCSV(w, data.Columns, data.Rows, data.Summary)

	case ReportUsageSummary:
		data, err := e.provider.UsageSummary(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("usage data: %w", err)
		}
		writeTabularCSV(w, data.Columns, data.Rows, data.Summary)

	case ReportCostAnalysis:
		data, err := e.provider.CostAnalysis(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("cost data: %w", err)
		}
		writeTabularCSV(w, data.Columns, data.Rows, data.Summary)

	case ReportAuditExport:
		data, err := e.provider.AuditExport(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("audit data: %w", err)
		}
		writeTabularCSV(w, data.Columns, data.Rows, data.Summary)

	case ReportSIMInventory:
		data, err := e.provider.SIMInventory(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("sim inventory data: %w", err)
		}
		writeTabularCSV(w, data.Columns, data.Rows, data.Summary)

	case ReportUnverifiedDevices:
		data, err := e.provider.UnverifiedDevices(ctx, req.TenantID, req.Filters)
		if err != nil {
			return nil, fmt.Errorf("unverified devices data: %w", err)
		}
		writeTabularCSV(w, data.Columns, data.Rows, data.Summary)

	default:
		return nil, fmt.Errorf("unsupported report type for csv: %q", req.Type)
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flush csv: %w", err)
	}

	filename := fmt.Sprintf("%s-%s.csv", req.Type, ts.Format("20060102-150405"))
	return &Artifact{
		Bytes:    buf.Bytes(),
		MIME:     "text/csv",
		Filename: filename,
	}, nil
}

func writeSectionsCSV(w *csv.Writer, sections []Section) {
	for i, sec := range sections {
		if i > 0 {
			w.Write([]string{""})
		}
		w.Write([]string{fmt.Sprintf("# Section: %s", sec.Title)})
		for _, row := range sec.Rows {
			w.Write(row)
		}
		if len(sec.Summary) > 0 {
			w.Write([]string{""})
			for _, s := range sec.Summary {
				w.Write([]string{fmt.Sprintf("# %s", s)})
			}
		}
	}
}

func writeTabularCSV(w *csv.Writer, columns []string, rows [][]string, summary map[string]string) {
	if len(columns) > 0 {
		w.Write(columns)
	}
	for _, row := range rows {
		w.Write(row)
	}
	if len(summary) > 0 {
		w.Write([]string{""})
		for k, v := range summary {
			w.Write([]string{fmt.Sprintf("# %s: %s", k, v)})
		}
	}
}
