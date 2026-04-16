package report

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

type StoreProvider struct {
	compliance *store.ComplianceStore
	cdr        *store.CDRStore
	audit      *store.AuditStore
	sims       *store.SIMStore
	sla        *store.SLAReportStore
}

func NewStoreProvider(
	compliance *store.ComplianceStore,
	cdr *store.CDRStore,
	auditStore *store.AuditStore,
	sims *store.SIMStore,
	sla *store.SLAReportStore,
) *StoreProvider {
	return &StoreProvider{
		compliance: compliance,
		cdr:        cdr,
		audit:      auditStore,
		sims:       sims,
		sla:        sla,
	}
}

func (p *StoreProvider) KVKK(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*KVKKData, error) {
	stateCounts, _ := p.compliance.CountSIMsByState(ctx, tenantID)
	pending, _ := p.compliance.CountPendingPurges(ctx, tenantID)
	overdue, _ := p.compliance.CountOverduePurges(ctx, tenantID)

	rows := make([][]string, 0, len(stateCounts))
	for _, sc := range stateCounts {
		rows = append(rows, []string{sc.State, strconv.Itoa(sc.Count)})
	}

	return &KVKKData{
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Sections: []Section{
			{
				Title: "Retention Health",
				Rows: [][]string{
					{"pending_purges", strconv.Itoa(pending)},
					{"overdue_purges", strconv.Itoa(overdue)},
				},
				Summary: []string{
					fmt.Sprintf("Pending purges: %d", pending),
					fmt.Sprintf("Overdue purges: %d", overdue),
				},
			},
			{
				Title:   "SIM State Distribution",
				Rows:    rows,
				Summary: []string{fmt.Sprintf("States tracked: %d", len(stateCounts))},
			},
		},
	}, nil
}

func (p *StoreProvider) GDPR(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*GDPRData, error) {
	pending, _ := p.compliance.CountPendingPurges(ctx, tenantID)
	overdue, _ := p.compliance.CountOverduePurges(ctx, tenantID)
	audits, _ := p.audit.GetRange(ctx, tenantID, 100)

	return &GDPRData{
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Sections: []Section{
			{
				Title: "Data Subject & Retention Controls",
				Rows: [][]string{
					{"pending_purges", strconv.Itoa(pending)},
					{"overdue_purges", strconv.Itoa(overdue)},
					{"recent_audit_entries", strconv.Itoa(len(audits))},
				},
				Summary: []string{
					fmt.Sprintf("Recent audit sample size: %d", len(audits)),
				},
			},
		},
	}, nil
}

func (p *StoreProvider) BTK(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*BTKData, error) {
	stats, err := p.compliance.BTKMonthlyStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	rows := make([][]string, 0, len(stats))
	for _, st := range stats {
		rows = append(rows, []string{
			st.OperatorName,
			st.OperatorCode,
			strconv.Itoa(st.ActiveCount),
			strconv.Itoa(st.SuspendedCount),
			strconv.Itoa(st.TerminatedCount),
			strconv.Itoa(st.TotalCount),
		})
	}

	return &BTKData{
		TenantID:    tenantID,
		GeneratedAt: time.Now().UTC(),
		Sections: []Section{
			{
				Title: "Operator Monthly Distribution",
				Rows:  rows,
			},
		},
	}, nil
}

func (p *StoreProvider) SLAMonthly(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*SLAData, error) {
	from, to := reportWindow(filters, 30)
	rows, _, err := p.sla.ListByTenant(ctx, tenantID, from, to, nil, "", 200)
	if err != nil {
		return nil, err
	}

	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, []string{
			row.WindowStart.Format(time.RFC3339),
			row.WindowEnd.Format(time.RFC3339),
			fmt.Sprintf("%.2f", row.UptimePct),
			strconv.Itoa(row.LatencyP95Ms),
			strconv.Itoa(row.IncidentCount),
			strconv.FormatInt(row.SessionsTotal, 10),
		})
	}

	return &SLAData{
		Columns:    []string{"window_start", "window_end", "uptime_pct", "latency_p95_ms", "incident_count", "sessions_total"},
		Rows:       out,
		Summary:    map[string]string{"rows": strconv.Itoa(len(out))},
		PeriodFrom: from,
		PeriodTo:   to,
	}, nil
}

func (p *StoreProvider) UsageSummary(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*UsageData, error) {
	from, to := reportWindow(filters, 30)
	cdrs, _, err := p.cdr.ListByTenant(ctx, tenantID, store.ListCDRParams{Limit: 500, From: &from, To: &to})
	if err != nil {
		return nil, err
	}

	type dayAgg struct {
		bytes int64
		count int
	}
	daily := map[string]dayAgg{}
	var totalBytes int64
	for _, cdr := range cdrs {
		day := cdr.Timestamp.UTC().Format("2006-01-02")
		agg := daily[day]
		agg.bytes += cdr.BytesIn + cdr.BytesOut
		agg.count++
		daily[day] = agg
		totalBytes += cdr.BytesIn + cdr.BytesOut
	}

	keys := make([]string, 0, len(daily))
	for k := range daily {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		agg := daily[k]
		rows = append(rows, []string{k, strconv.FormatInt(agg.bytes, 10), strconv.Itoa(agg.count)})
	}

	return &UsageData{
		Columns:    []string{"day", "total_bytes", "cdr_count"},
		Rows:       rows,
		Summary:    map[string]string{"total_bytes": strconv.FormatInt(totalBytes, 10), "sampled_cdrs": strconv.Itoa(len(cdrs))},
		PeriodFrom: from,
		PeriodTo:   to,
	}, nil
}

func (p *StoreProvider) CostAnalysis(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*CostData, error) {
	from, to := reportWindow(filters, 30)
	rows, err := p.cdr.GetCostAggregation(ctx, tenantID, from, to, nil)
	if err != nil {
		return nil, err
	}

	out := make([][]string, 0, len(rows))
	var totalCost float64
	for _, row := range rows {
		out = append(out, []string{
			row.OperatorID.String(),
			row.Bucket.Format(time.RFC3339),
			fmt.Sprintf("%.4f", row.TotalUsageCost),
			fmt.Sprintf("%.4f", row.TotalCarrierCost),
			strconv.FormatInt(row.TotalBytes, 10),
			strconv.FormatInt(row.ActiveSims, 10),
		})
		totalCost += row.TotalUsageCost
	}

	return &CostData{
		Columns:    []string{"operator_id", "bucket", "usage_cost", "carrier_cost", "total_bytes", "active_sims"},
		Rows:       out,
		Summary:    map[string]string{"total_usage_cost": fmt.Sprintf("%.4f", totalCost)},
		PeriodFrom: from,
		PeriodTo:   to,
	}, nil
}

func (p *StoreProvider) AuditExport(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*AuditExportData, error) {
	from, to := reportWindow(filters, 30)
	entries, err := p.audit.GetByDateRange(ctx, tenantID, from, to)
	if err != nil {
		return nil, err
	}

	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, auditRow(entry))
	}

	return &AuditExportData{
		Columns:    []string{"created_at", "action", "entity_type", "entity_id", "user_id"},
		Rows:       rows,
		Summary:    map[string]string{"rows": strconv.Itoa(len(rows))},
		PeriodFrom: from,
		PeriodTo:   to,
	}, nil
}

func (p *StoreProvider) SIMInventory(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*SIMInventoryData, error) {
	sims, _, err := p.sims.List(ctx, tenantID, store.ListSIMsParams{Limit: 500})
	if err != nil {
		return nil, err
	}

	rows := make([][]string, 0, len(sims))
	for _, sim := range sims {
		rows = append(rows, []string{
			sim.ID.String(),
			sim.ICCID,
			sim.IMSI,
			stringPtr(sim.MSISDN),
			sim.State,
			stringPtrUUID(sim.APNID),
			stringPtrUUID(sim.PolicyVersionID),
			sim.CreatedAt.Format(time.RFC3339),
		})
	}

	return &SIMInventoryData{
		Columns:    []string{"id", "iccid", "imsi", "msisdn", "state", "apn_id", "policy_version_id", "created_at"},
		Rows:       rows,
		Summary:    map[string]string{"rows": strconv.Itoa(len(rows))},
		PeriodFrom: time.Now().UTC(),
		PeriodTo:   time.Now().UTC(),
	}, nil
}

func reportWindow(filters map[string]any, fallbackDays int) (time.Time, time.Time) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -fallbackDays)

	if raw, ok := filters["from"].(string); ok && raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			from = parsed.UTC()
		}
	}
	if raw, ok := filters["to"].(string); ok && raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			to = parsed.UTC()
		}
	}
	return from, to
}

func auditRow(entry audit.Entry) []string {
	userID := ""
	if entry.UserID != nil {
		userID = entry.UserID.String()
	}
	return []string{
		entry.CreatedAt.Format(time.RFC3339),
		entry.Action,
		entry.EntityType,
		entry.EntityID,
		userID,
	}
}

func stringPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func stringPtrUUID(v *uuid.UUID) string {
	if v == nil {
		return ""
	}
	return v.String()
}
