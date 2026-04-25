package report

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ReportType string

const (
	ReportKVKK         ReportType = "compliance_kvkk"
	ReportGDPR         ReportType = "compliance_gdpr"
	ReportBTK          ReportType = "compliance_btk"
	ReportSLAMonthly   ReportType = "sla_monthly"
	ReportUsageSummary ReportType = "usage_summary"
	ReportCostAnalysis ReportType = "cost_analysis"
	ReportAuditExport  ReportType = "audit_log_export"
	ReportSIMInventory ReportType = "sim_inventory"
	// ReportAlertsExport (FIX-229 Task 7 / DEV-338) — paginated alert export
	// driven through the unified Engine.Build path. Format: PDF (this task);
	// CSV/JSON exports are streamed directly by the alert handler instead.
	ReportAlertsExport ReportType = "alerts_export"
)

type Format string

const (
	FormatPDF  Format = "pdf"
	FormatCSV  Format = "csv"
	FormatXLSX Format = "xlsx"
)

type Request struct {
	Type     ReportType
	Format   Format
	TenantID uuid.UUID
	Filters  map[string]any
	Locale   string
}

type Artifact struct {
	Bytes    []byte
	MIME     string
	Filename string
}

func (a *Artifact) Extension() string {
	switch {
	case a.MIME == "text/csv":
		return ".csv"
	case a.MIME == "application/pdf":
		return ".pdf"
	case a.MIME == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	default:
		return ""
	}
}

type Section struct {
	Title   string
	Rows    [][]string
	Summary []string
}

type KVKKData struct {
	Sections    []Section
	GeneratedAt time.Time
	TenantID    uuid.UUID
}

type GDPRData struct {
	Sections    []Section
	GeneratedAt time.Time
	TenantID    uuid.UUID
}

type BTKData struct {
	Sections    []Section
	GeneratedAt time.Time
	TenantID    uuid.UUID
}

type SLAData struct {
	Columns    []string
	Rows       [][]string
	Summary    map[string]string
	PeriodFrom time.Time
	PeriodTo   time.Time
}

type UsageData struct {
	Columns    []string
	Rows       [][]string
	Summary    map[string]string
	PeriodFrom time.Time
	PeriodTo   time.Time
}

type CostData struct {
	Columns    []string
	Rows       [][]string
	Summary    map[string]string
	PeriodFrom time.Time
	PeriodTo   time.Time
}

type AuditExportData struct {
	Columns    []string
	Rows       [][]string
	Summary    map[string]string
	PeriodFrom time.Time
	PeriodTo   time.Time
}

type SIMInventoryData struct {
	Columns    []string
	Rows       [][]string
	Summary    map[string]string
	PeriodFrom time.Time
	PeriodTo   time.Time
}

// AlertsExportFilters mirrors store.ListAlertsParams minus pagination
// cursor — alert exports are single-shot and pagination is internal to the
// data provider (see store_provider.go AlertsExport).
type AlertsExportFilters struct {
	Type       string
	Severity   string
	Source     string
	State      string
	SimID      *uuid.UUID
	OperatorID *uuid.UUID
	APNID      *uuid.UUID
	From       *time.Time
	To         *time.Time
	Q          string
}

// AlertExportRow is a flattened, display-ready alert row used by the PDF
// builder. All fields are pre-formatted strings — the PDF builder does not
// re-format dates or resolve names.
type AlertExportRow struct {
	ID           string
	Severity     string
	State        string
	Source       string
	Type         string
	Title        string
	OperatorName string
	SimICCID     string
	FiredAt      string
	ResolvedAt   string
}

// AlertsExportData is the payload fed to buildAlertsPDF. TotalRows reflects
// the pre-truncate count; Rows is capped to the first 200 entries when
// TruncatedToFirst > 0.
type AlertsExportData struct {
	GeneratedAt       time.Time
	TenantID          uuid.UUID
	Filters           map[string]string
	FilterDescription string
	TotalRows         int
	SeverityBreakdown map[string]int
	StateBreakdown    map[string]int
	Rows              []AlertExportRow
	TruncatedToFirst  int
}

type DataProvider interface {
	KVKK(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*KVKKData, error)
	GDPR(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*GDPRData, error)
	BTK(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*BTKData, error)
	SLAMonthly(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*SLAData, error)
	UsageSummary(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*UsageData, error)
	CostAnalysis(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*CostData, error)
	AuditExport(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*AuditExportData, error)
	SIMInventory(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*SIMInventoryData, error)
	AlertsExport(ctx context.Context, tenantID uuid.UUID, filters AlertsExportFilters) (*AlertsExportData, error)
}

type Engine struct {
	provider DataProvider
}

func NewEngine(p DataProvider) *Engine { return &Engine{provider: p} }

func (e *Engine) Build(ctx context.Context, req Request) (*Artifact, error) {
	switch req.Format {
	case FormatCSV:
		return e.buildCSV(ctx, req)
	case FormatPDF:
		return e.buildPDF(ctx, req)
	case FormatXLSX:
		return e.buildExcel(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported format: %q", req.Format)
	}
}
