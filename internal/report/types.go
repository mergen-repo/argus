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

type DataProvider interface {
	KVKK(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*KVKKData, error)
	GDPR(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*GDPRData, error)
	BTK(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*BTKData, error)
	SLAMonthly(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*SLAData, error)
	UsageSummary(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*UsageData, error)
	CostAnalysis(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*CostData, error)
	AuditExport(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*AuditExportData, error)
	SIMInventory(ctx context.Context, tenantID uuid.UUID, filters map[string]any) (*SIMInventoryData, error)
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
