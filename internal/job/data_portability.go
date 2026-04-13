package job

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-pdf/fpdf"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// PortabilityStorage is the S3 interface needed by this processor.
type PortabilityStorage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

// portabilityExportPayload matches the payload written by the handler.
type portabilityExportPayload struct {
	UserID          string `json:"user_id"`
	RequesterUserID string `json:"requester_user_id"`
	TenantID        string `json:"tenant_id"`
}

// portabilitySIM is a lightweight SIM struct scoped to owner_user_id queries.
// We avoid modifying store.SIM (and its scanSIM/simColumns) because owner_user_id
// is an optional column added via migration and many existing queries don't select it.
type portabilitySIM struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	ICCID    string
	IMSI     string
	State    string
	SimType  string
}

// portabilityExportData is the canonical JSON schema for data.json.
type portabilityExportData struct {
	ExportedAt  string                  `json:"exported_at"`
	UserID      string                  `json:"user_id"`
	TenantName  string                  `json:"tenant_name"`
	User        portabilityUser         `json:"user"`
	Tenant      portabilityTenant       `json:"tenant"`
	SIMs        []portabilitySIM        `json:"sims"`
	CDRs        []store.CDR             `json:"cdrs"`
	AuditLogs   []audit.Entry           `json:"audit_logs"`
}

type portabilityUser struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

type portabilityTenant struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CreatedAt     string `json:"created_at"`
	RetentionDays int    `json:"retention_days"`
}

type DataPortabilityProcessor struct {
	jobs      *store.JobStore
	userStore *store.UserStore
	tenantStore *store.TenantStore
	cdrStore  *store.CDRStore
	auditStore *store.AuditStore
	s3        PortabilityStorage
	eventBus  *bus.EventBus
	db        *pgxpool.Pool
	auditSvc  audit.Auditor
	logger    zerolog.Logger
}

func NewDataPortabilityProcessor(
	jobs *store.JobStore,
	userStore *store.UserStore,
	tenantStore *store.TenantStore,
	cdrStore *store.CDRStore,
	auditStore *store.AuditStore,
	s3 PortabilityStorage,
	eventBus *bus.EventBus,
	db *pgxpool.Pool,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *DataPortabilityProcessor {
	return &DataPortabilityProcessor{
		jobs:        jobs,
		userStore:   userStore,
		tenantStore: tenantStore,
		cdrStore:    cdrStore,
		auditStore:  auditStore,
		s3:          s3,
		eventBus:    eventBus,
		db:          db,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("processor", JobTypeDataPortabilityExport).Logger(),
	}
}

func (p *DataPortabilityProcessor) Type() string {
	return JobTypeDataPortabilityExport
}

func (p *DataPortabilityProcessor) Process(ctx context.Context, job *store.Job) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var payload portabilityExportPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal portability payload: %w", err)
	}

	userID, err := uuid.Parse(payload.UserID)
	if err != nil {
		return fmt.Errorf("parse user_id: %w", err)
	}
	tenantID := job.TenantID

	user, err := p.userStore.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	tenant, err := p.tenantStore.GetByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("get tenant: %w", err)
	}

	sims, err := p.loadSIMsByOwner(ctx, userID, tenantID)
	if err != nil {
		return fmt.Errorf("load sims: %w", err)
	}

	retentionDays := tenant.PurgeRetentionDays
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	if cutoff.Before(time.Now().UTC().AddDate(0, 0, -90)) {
		cutoff = time.Now().UTC().AddDate(0, 0, -90)
	}

	cdrs, err := p.loadCDRsByOwner(ctx, userID, tenantID, cutoff)
	if err != nil {
		return fmt.Errorf("load cdrs: %w", err)
	}

	auditLogs, err := p.loadAuditLogs(ctx, tenantID, userID)
	if err != nil {
		return fmt.Errorf("load audit logs: %w", err)
	}

	exportData := portabilityExportData{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		UserID:     userID.String(),
		TenantName: tenant.Name,
		User: portabilityUser{
			ID:        user.ID.String(),
			Email:     user.Email,
			Name:      user.Name,
			Role:      user.Role,
			State:     user.State,
			CreatedAt: user.CreatedAt.Format(time.RFC3339),
		},
		Tenant: portabilityTenant{
			ID:            tenant.ID.String(),
			Name:          tenant.Name,
			CreatedAt:     tenant.CreatedAt.Format(time.RFC3339),
			RetentionDays: tenant.PurgeRetentionDays,
		},
		SIMs:      sims,
		CDRs:      cdrs,
		AuditLogs: auditLogs,
	}

	dataJSON, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data.json: %w", err)
	}

	summaryPDF, err := buildPortabilitySummaryPDF(exportData)
	if err != nil {
		return fmt.Errorf("build summary.pdf: %w", err)
	}

	readmeTxt := buildPortabilityREADME(tenant.Name, user.Name)

	archiveBytes, err := buildZipArchive(dataJSON, summaryPDF, readmeTxt)
	if err != nil {
		return fmt.Errorf("build zip: %w", err)
	}

	s3Key := fmt.Sprintf("tenants/%s/portability/%s/%s.zip",
		tenantID.String(), userID.String(), job.ID.String())

	if err := p.s3.Upload(ctx, "", s3Key, archiveBytes); err != nil {
		return fmt.Errorf("s3 upload: %w", err)
	}

	signedURL, err := p.s3.PresignGet(ctx, "", s3Key, 7*24*time.Hour)
	if err != nil {
		p.logger.Warn().Err(err).Str("s3_key", s3Key).Msg("presign failed, proceeding without signed url")
		signedURL = ""
	}

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectNotification, map[string]interface{}{
			"tenant_id":    tenantID.String(),
			"user_id":      userID.String(),
			"type":         "data_portability_ready",
			"category":     "compliance",
			"title":        "Data Export Ready",
			"message":      "Your personal data export is ready for download.",
			"severity":     "info",
			"resource_type": "user",
			"resource_id":  userID.String(),
			"download_url": signedURL,
			"expires_at":   expiresAt,
		})
	}

	if p.auditSvc != nil {
		afterData, _ := json.Marshal(map[string]interface{}{
			"s3_key":     s3Key,
			"bytes_size": len(archiveBytes),
			"signed_url": signedURL,
		})
		_, _ = p.auditSvc.CreateEntry(ctx, audit.CreateEntryParams{
			TenantID:   tenantID,
			UserID:     &userID,
			Action:     "data_portability.exported",
			EntityType: "user",
			EntityID:   userID.String(),
			AfterData:  afterData,
		})
	}

	result, _ := json.Marshal(map[string]interface{}{
		"s3_key":     s3Key,
		"signed_url": signedURL,
		"bytes_size": len(archiveBytes),
		"expires_at": expiresAt,
	})

	return p.jobs.Complete(ctx, job.ID, nil, result)
}

// loadSIMsByOwner fetches SIMs where owner_user_id = userID AND tenant_id = tenantID.
// Only SIMs with an explicit owner are included (portability scope = explicitly owned SIMs).
// Note: for tenant_admin requests, the dispatch payload still targets a specific user_id;
// a broader tenant-level scope is not applied here — the handler may in future pass a
// flag for that distinction if needed.
func (p *DataPortabilityProcessor) loadSIMsByOwner(ctx context.Context, userID, tenantID uuid.UUID) ([]portabilitySIM, error) {
	rows, err := p.db.Query(ctx,
		`SELECT id, tenant_id, iccid, imsi, state, sim_type
		 FROM sims
		 WHERE owner_user_id = $1 AND tenant_id = $2`,
		userID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("query sims by owner: %w", err)
	}
	defer rows.Close()

	var sims []portabilitySIM
	for rows.Next() {
		var s portabilitySIM
		if err := rows.Scan(&s.ID, &s.TenantID, &s.ICCID, &s.IMSI, &s.State, &s.SimType); err != nil {
			return nil, fmt.Errorf("scan portability sim: %w", err)
		}
		sims = append(sims, s)
	}
	return sims, rows.Err()
}

// loadCDRsByOwner loads CDRs for SIMs owned by the user within the retention window.
// We join via the sims table using owner_user_id rather than adding a user_id FK to cdrs.
func (p *DataPortabilityProcessor) loadCDRsByOwner(ctx context.Context, userID, tenantID uuid.UUID, since time.Time) ([]store.CDR, error) {
	rows, err := p.db.Query(ctx,
		`SELECT c.id, c.session_id, c.sim_id, c.tenant_id, c.operator_id, c.apn_id,
		        c.rat_type, c.record_type, c.bytes_in, c.bytes_out, c.duration_sec,
		        c.usage_cost, c.carrier_cost, c.rate_per_mb, c.rat_multiplier, c.timestamp
		 FROM cdrs c
		 JOIN sims s ON s.id = c.sim_id
		 WHERE s.owner_user_id = $1 AND c.tenant_id = $2 AND c.timestamp >= $3
		 ORDER BY c.timestamp DESC
		 LIMIT 10000`,
		userID, tenantID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("query cdrs by owner: %w", err)
	}
	defer rows.Close()

	var cdrs []store.CDR
	for rows.Next() {
		var c store.CDR
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.SimID, &c.TenantID, &c.OperatorID, &c.APNID,
			&c.RATType, &c.RecordType, &c.BytesIn, &c.BytesOut, &c.DurationSec,
			&c.UsageCost, &c.CarrierCost, &c.RatePerMB, &c.RATMultiplier, &c.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan cdr: %w", err)
		}
		cdrs = append(cdrs, c)
	}
	return cdrs, rows.Err()
}

// loadAuditLogs returns audit entries where user_id = userID, up to 1000 most recent.
func (p *DataPortabilityProcessor) loadAuditLogs(ctx context.Context, tenantID, userID uuid.UUID) ([]audit.Entry, error) {
	uid := userID
	entries, _, err := p.auditStore.List(ctx, tenantID, store.ListAuditParams{
		UserID: &uid,
		Limit:  1000,
	})
	return entries, err
}

func buildPortabilitySummaryPDF(data portabilityExportData) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetMargins(15, 15, 15)

	pdf.SetFont("Arial", "B", 16)
	pdf.CellFormat(0, 10, "Personal Data Export Summary", "", 1, "C", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Arial", "", 10)
	pdf.CellFormat(50, 7, "Tenant:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, data.TenantName, "", 1, "L", false, 0, "")
	pdf.CellFormat(50, 7, "User:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, data.User.Name+" ("+data.User.Email+")", "", 1, "L", false, 0, "")
	pdf.CellFormat(50, 7, "Export Date:", "", 0, "L", false, 0, "")
	pdf.CellFormat(0, 7, data.ExportedAt, "", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetFont("Arial", "B", 12)
	pdf.CellFormat(0, 8, "Section Summary", "", 1, "L", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Arial", "B", 10)
	pdf.SetFillColor(40, 40, 60)
	pdf.SetTextColor(255, 255, 255)
	pdf.CellFormat(100, 8, "Section", "1", 0, "L", true, 0, "")
	pdf.CellFormat(40, 8, "Record Count", "1", 1, "R", true, 0, "")

	pdf.SetFont("Arial", "", 10)
	pdf.SetTextColor(0, 0, 0)
	sections := []struct {
		name  string
		count int
	}{
		{"User Profile", 1},
		{"Tenant Metadata", 1},
		{"SIM Cards (owned)", len(data.SIMs)},
		{"Charging Data Records (CDRs)", len(data.CDRs)},
		{"Audit Log Entries", len(data.AuditLogs)},
	}
	for i, s := range sections {
		if i%2 == 0 {
			pdf.SetFillColor(248, 248, 252)
		} else {
			pdf.SetFillColor(255, 255, 255)
		}
		pdf.CellFormat(100, 7, s.name, "1", 0, "L", true, 0, "")
		pdf.CellFormat(40, 7, fmt.Sprintf("%d", s.count), "1", 1, "R", true, 0, "")
	}

	pdf.Ln(4)
	pdf.SetFont("Arial", "", 9)
	pdf.MultiCell(0, 5,
		"This export was generated in compliance with GDPR Article 20 (Right to Data Portability) "+
			"and KVKK Article 11. The archive contains your personal data held by "+data.TenantName+
			" in machine-readable JSON format.",
		"", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render summary pdf: %w", err)
	}
	return buf.Bytes(), nil
}

const portabilityREADME = `DATA PORTABILITY EXPORT
========================

EN: This archive contains your personal data exported under GDPR Article 20
    (Right to Data Portability) and KVKK Article 11.

TR: Bu arşiv, GDPR Madde 20 (Veri Taşınabilirliği Hakkı) ve KVKK 11. Madde
    kapsamında dışa aktarılan kişisel verilerinizi içermektedir.

Contents / İçindekiler:
  data.json     — Machine-readable export of all personal data sections
  summary.pdf   — Human-readable summary with section row counts
  README.txt    — This file

The signed download link expires 7 days after generation.
İndirme bağlantısı oluşturulduktan 7 gün sonra geçerliliğini yitirir.
`

func buildPortabilityREADME(tenantName, userName string) []byte {
	return []byte(fmt.Sprintf("%s\nTenant: %s\nUser: %s\n", portabilityREADME, tenantName, userName))
}

func buildZipArchive(dataJSON, summaryPDF, readmeTxt []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	files := []struct {
		name string
		data []byte
	}{
		{"data.json", dataJSON},
		{"summary.pdf", summaryPDF},
		{"README.txt", readmeTxt},
	}

	for _, f := range files {
		fw, err := w.Create(f.name)
		if err != nil {
			return nil, fmt.Errorf("create zip entry %s: %w", f.name, err)
		}
		if _, err := fw.Write(f.data); err != nil {
			return nil, fmt.Errorf("write zip entry %s: %w", f.name, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return buf.Bytes(), nil
}
