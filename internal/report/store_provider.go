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
	alerts     *store.AlertStore
	operators  *store.OperatorStore
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

// WithAlertStore enables AlertsExport (FIX-229 Task 7). Without it, AlertsExport
// returns an error — callers must wire the AlertStore for PDF alert exports.
func (p *StoreProvider) WithAlertStore(s *store.AlertStore) *StoreProvider {
	p.alerts = s
	return p
}

// WithOperatorStore lets AlertsExport hydrate operator names from operator IDs.
// Optional: if nil, operator column shows the UUID prefix.
func (p *StoreProvider) WithOperatorStore(s *store.OperatorStore) *StoreProvider {
	p.operators = s
	return p
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
	var from, to time.Time
	var operatorID *uuid.UUID

	yearVal, hasYear := filters["year"]
	monthVal, hasMonth := filters["month"]

	if hasYear && hasMonth {
		year, okY := toInt(yearVal)
		month, okM := toInt(monthVal)
		if okY && okM {
			from = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
			to = from.AddDate(0, 1, 0)
		} else {
			from, to = reportWindow(filters, 30)
		}
	} else {
		from, to = reportWindow(filters, 30)
	}

	if rawID, ok := filters["operator_id"].(string); ok && rawID != "" {
		if id, parseErr := uuid.Parse(rawID); parseErr == nil {
			operatorID = &id
		}
	}

	rows, _, err := p.sla.ListByTenant(ctx, tenantID, from, to, operatorID, "", 200)
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

// alertsExportRowCap matches the handler-side cap (10 000) — a single page
// from the store is 100 rows, so we issue at most 100 round-trips.
const alertsExportRowCap = 10000

// alertsExportTruncateAt is the per-PDF row cap. The PDF footer shows
// "Showing first 200 of N alerts" when the total exceeds this.
const alertsExportTruncateAt = 200

// AlertsExport hydrates alerts for the PDF exporter. It pages through the
// store (100 rows/page) until alertsExportRowCap or EOF, computes severity
// and state breakdowns over the full set, then truncates to the first 200
// rows for the printed table.
func (p *StoreProvider) AlertsExport(ctx context.Context, tenantID uuid.UUID, filters AlertsExportFilters) (*AlertsExportData, error) {
	if p.alerts == nil {
		return nil, fmt.Errorf("alerts export: alert store not configured")
	}

	base := store.ListAlertsParams{
		Type:       filters.Type,
		Severity:   filters.Severity,
		Source:     filters.Source,
		State:      filters.State,
		SimID:      filters.SimID,
		OperatorID: filters.OperatorID,
		APNID:      filters.APNID,
		From:       filters.From,
		To:         filters.To,
		Q:          filters.Q,
	}

	const pageSize = 100
	all := make([]store.Alert, 0, pageSize)
	params := base
	params.Limit = pageSize
	params.Cursor = nil

	for len(all) < alertsExportRowCap {
		batch, nextCursor, err := p.alerts.ListByTenant(ctx, tenantID, params)
		if err != nil {
			return nil, fmt.Errorf("alerts export: list page: %w", err)
		}
		all = append(all, batch...)
		if nextCursor == nil {
			break
		}
		if len(all) >= alertsExportRowCap {
			break
		}
		params.Cursor = nextCursor
	}
	if len(all) > alertsExportRowCap {
		all = all[:alertsExportRowCap]
	}

	severityBreakdown := make(map[string]int)
	stateBreakdown := make(map[string]int)
	operatorIDs := make(map[uuid.UUID]struct{})
	simIDs := make(map[uuid.UUID]struct{})

	for i := range all {
		a := &all[i]
		severityBreakdown[a.Severity]++
		stateBreakdown[a.State]++
		if a.OperatorID != nil {
			operatorIDs[*a.OperatorID] = struct{}{}
		}
		if a.SimID != nil {
			simIDs[*a.SimID] = struct{}{}
		}
	}

	// FIX-229 Gate F-A5: batch hydrate via WHERE id = ANY($1) — single query
	// per resource instead of N per-id GetByID/GetICCIDByID calls. For a 10K
	// alert export touching 50 operators + 5K SIMs this turns ~5050 round-trips
	// into 2.
	operatorNames := make(map[uuid.UUID]string, len(operatorIDs))
	if p.operators != nil && len(operatorIDs) > 0 {
		ids := make([]uuid.UUID, 0, len(operatorIDs))
		for id := range operatorIDs {
			ids = append(ids, id)
		}
		if names, err := p.operators.ListNamesByIDs(ctx, ids); err == nil {
			operatorNames = names
		}
	}

	simICCIDs := make(map[uuid.UUID]string, len(simIDs))
	if p.sims != nil && len(simIDs) > 0 {
		ids := make([]uuid.UUID, 0, len(simIDs))
		for id := range simIDs {
			ids = append(ids, id)
		}
		if iccids, err := p.sims.ListICCIDsByIDs(ctx, ids); err == nil {
			simICCIDs = iccids
		}
	}

	totalRows := len(all)
	limit := totalRows
	truncated := 0
	if limit > alertsExportTruncateAt {
		limit = alertsExportTruncateAt
		truncated = alertsExportTruncateAt
	}

	rows := make([]AlertExportRow, 0, limit)
	for i := 0; i < limit; i++ {
		a := &all[i]
		row := AlertExportRow{
			ID:       a.ID.String(),
			Severity: a.Severity,
			State:    a.State,
			Source:   a.Source,
			Type:     a.Type,
			Title:    a.Title,
			FiredAt:  a.FiredAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		}
		if a.ResolvedAt != nil {
			row.ResolvedAt = a.ResolvedAt.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		if a.OperatorID != nil {
			if name, ok := operatorNames[*a.OperatorID]; ok && name != "" {
				row.OperatorName = name
			} else {
				row.OperatorName = a.OperatorID.String()[:8]
			}
		}
		if a.SimID != nil {
			if iccid, ok := simICCIDs[*a.SimID]; ok && iccid != "" {
				row.SimICCID = iccid
			} else {
				row.SimICCID = a.SimID.String()[:8]
			}
		}
		rows = append(rows, row)
	}

	displayFilters, description := summarizeAlertFilters(filters)

	return &AlertsExportData{
		GeneratedAt:       time.Now().UTC(),
		TenantID:          tenantID,
		Filters:           displayFilters,
		FilterDescription: description,
		TotalRows:         totalRows,
		SeverityBreakdown: severityBreakdown,
		StateBreakdown:    stateBreakdown,
		Rows:              rows,
		TruncatedToFirst:  truncated,
	}, nil
}

// alertsExportFiltersFromMap unpacks a Request.Filters map (string keys, any
// values) into the typed AlertsExportFilters used by the data provider.
// Unknown / unparseable values are silently dropped — the handler is the
// source of truth for filter validation.
func alertsExportFiltersFromMap(m map[string]any) AlertsExportFilters {
	f := AlertsExportFilters{}
	if m == nil {
		return f
	}
	if v, ok := m["type"].(string); ok {
		f.Type = v
	}
	if v, ok := m["severity"].(string); ok {
		f.Severity = v
	}
	if v, ok := m["source"].(string); ok {
		f.Source = v
	}
	if v, ok := m["state"].(string); ok {
		f.State = v
	}
	if v, ok := m["q"].(string); ok {
		f.Q = v
	}
	if id, ok := m["sim_id"].(uuid.UUID); ok {
		f.SimID = &id
	}
	if id, ok := m["operator_id"].(uuid.UUID); ok {
		f.OperatorID = &id
	}
	if id, ok := m["apn_id"].(uuid.UUID); ok {
		f.APNID = &id
	}
	if t, ok := m["from"].(time.Time); ok {
		f.From = &t
	}
	if t, ok := m["to"].(time.Time); ok {
		f.To = &t
	}
	return f
}

func summarizeAlertFilters(f AlertsExportFilters) (map[string]string, string) {
	m := map[string]string{}
	if f.Type != "" {
		m["type"] = f.Type
	}
	if f.Severity != "" {
		m["severity"] = f.Severity
	}
	if f.Source != "" {
		m["source"] = f.Source
	}
	if f.State != "" {
		m["state"] = f.State
	}
	if f.Q != "" {
		m["q"] = f.Q
	}
	if f.SimID != nil {
		m["sim_id"] = f.SimID.String()
	}
	if f.OperatorID != nil {
		m["operator_id"] = f.OperatorID.String()
	}
	if f.APNID != nil {
		m["apn_id"] = f.APNID.String()
	}
	if f.From != nil {
		m["from"] = f.From.UTC().Format(time.RFC3339)
	}
	if f.To != nil {
		m["to"] = f.To.UTC().Format(time.RFC3339)
	}
	if len(m) == 0 {
		return m, "No filters applied"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, m[k]))
	}
	return m, "Filters: " + joinFilterParts(parts)
}

func joinFilterParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
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

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
