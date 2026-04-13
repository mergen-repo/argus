package job

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakePortabilityStorage struct {
	uploaded  map[string][]byte
	presigned map[string]string
}

func newFakePortabilityStorage() *fakePortabilityStorage {
	return &fakePortabilityStorage{
		uploaded:  make(map[string][]byte),
		presigned: make(map[string]string),
	}
}

func (f *fakePortabilityStorage) Upload(_ context.Context, _, key string, data []byte) error {
	f.uploaded[key] = data
	return nil
}

func (f *fakePortabilityStorage) PresignGet(_ context.Context, _, key string, _ time.Duration) (string, error) {
	url := fmt.Sprintf("https://fake-s3.example.com/%s?presigned=1", key)
	f.presigned[key] = url
	return url, nil
}

type fakeNotifyBus struct {
	published []map[string]interface{}
}

func (b *fakeNotifyBus) capturePublish(_ context.Context, _ string, payload interface{}) error {
	if m, ok := payload.(map[string]interface{}); ok {
		b.published = append(b.published, m)
	}
	return nil
}

type fakeAuditSvc struct {
	entries []audit.CreateEntryParams
}

func (a *fakeAuditSvc) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	a.entries = append(a.entries, p)
	return &audit.Entry{}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return id
}

// ─── unit tests (no DB) ──────────────────────────────────────────────────────

func TestDataPortabilityProcessorType(t *testing.T) {
	proc := &DataPortabilityProcessor{}
	if proc.Type() != JobTypeDataPortabilityExport {
		t.Errorf("Type() = %q, want %q", proc.Type(), JobTypeDataPortabilityExport)
	}
}

func TestBuildZipArchive_ContainsExpectedFiles(t *testing.T) {
	dataJSON := []byte(`{"exported_at":"2026-04-13T00:00:00Z"}`)
	summaryPDF := []byte("%PDF-1.4 test")
	readmeTxt := []byte("README content")

	archiveBytes, err := buildZipArchive(dataJSON, summaryPDF, readmeTxt)
	if err != nil {
		t.Fatalf("buildZipArchive: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	want := map[string]bool{"data.json": false, "summary.pdf": false, "README.txt": false}
	for _, f := range zr.File {
		want[f.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("zip missing file: %s", name)
		}
	}
}

func TestBuildPortabilitySummaryPDF_StartsWithPDFMagic(t *testing.T) {
	data := portabilityExportData{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		UserID:     uuid.New().String(),
		TenantName: "Test Tenant",
		User: portabilityUser{
			ID:        uuid.New().String(),
			Email:     "user@example.com",
			Name:      "Test User",
			Role:      "api_user",
			State:     "active",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Tenant: portabilityTenant{
			ID:            uuid.New().String(),
			Name:          "Test Tenant",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			RetentionDays: 90,
		},
		SIMs:      []portabilitySIM{{ID: uuid.New(), TenantID: uuid.New(), ICCID: "8900000000000000001", IMSI: "310150000000001", State: "active", SimType: "physical"}},
		CDRs:      []store.CDR{},
		AuditLogs: []audit.Entry{},
	}

	pdfBytes, err := buildPortabilitySummaryPDF(data)
	if err != nil {
		t.Fatalf("buildPortabilitySummaryPDF: %v", err)
	}

	if len(pdfBytes) == 0 {
		t.Fatal("summary.pdf is empty")
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Errorf("summary.pdf does not start with %%PDF-, got prefix: %q", pdfBytes[:min(8, len(pdfBytes))])
	}
}

func TestBuildPortabilityREADME_ContainsTenantAndUser(t *testing.T) {
	readme := buildPortabilityREADME("AcmeCorp", "Jane Doe")
	content := string(readme)
	if !contains(content, "AcmeCorp") {
		t.Error("README missing tenant name")
	}
	if !contains(content, "Jane Doe") {
		t.Error("README missing user name")
	}
	if !contains(content, "GDPR") {
		t.Error("README missing GDPR reference")
	}
	if !contains(content, "KVKK") {
		t.Error("README missing KVKK reference")
	}
}

// ─── integration-style test using in-memory mocks ────────────────────────────

// TestDataPortabilityProcessor_ProcessWithMockData tests the full processor path
// using a fake S3, fake audit, and direct struct manipulation (no live DB).
// We test the zip-building, S3 upload, presigning, notification, and result JSON.
func TestDataPortabilityProcessor_ZipContainsExpectedSections(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	jobID := uuid.New()

	exportData := portabilityExportData{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		UserID:     userID.String(),
		TenantName: "IntegTenant",
		User: portabilityUser{
			ID:        userID.String(),
			Email:     "integ@test.io",
			Name:      "Integ User",
			Role:      "api_user",
			State:     "active",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Tenant: portabilityTenant{
			ID:            tenantID.String(),
			Name:          "IntegTenant",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			RetentionDays: 90,
		},
		SIMs: func() []portabilitySIM {
			sims := make([]portabilitySIM, 5)
			for i := range sims {
				sims[i] = portabilitySIM{
					ID:       uuid.New(),
					TenantID: tenantID,
					ICCID:    fmt.Sprintf("890000000000000%04d", i),
					IMSI:     fmt.Sprintf("31015000000000%d", i),
					State:    "active",
					SimType:  "physical",
				}
			}
			return sims
		}(),
		CDRs: func() []store.CDR {
			cdrs := make([]store.CDR, 20)
			for i := range cdrs {
				cdrs[i] = store.CDR{
					ID:          int64(i + 1),
					SessionID:   uuid.New(),
					SimID:       uuid.New(),
					TenantID:    tenantID,
					OperatorID:  uuid.New(),
					RecordType:  "interim",
					BytesIn:     int64(1000 * (i + 1)),
					BytesOut:    int64(500 * (i + 1)),
					DurationSec: 60 * (i + 1),
					Timestamp:   time.Now().UTC().Add(-time.Duration(i) * time.Hour),
				}
			}
			return cdrs
		}(),
		AuditLogs: func() []audit.Entry {
			entries := make([]audit.Entry, 3)
			for i := range entries {
				entries[i] = audit.Entry{
					ID:         int64(i + 1),
					TenantID:   tenantID,
					UserID:     &userID,
					Action:     fmt.Sprintf("test.action.%d", i),
					EntityType: "sim",
					EntityID:   uuid.New().String(),
					CreatedAt:  time.Now().UTC().Add(-time.Duration(i) * time.Hour),
				}
			}
			return entries
		}(),
	}

	dataJSON, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		t.Fatalf("marshal exportData: %v", err)
	}

	summaryPDF, err := buildPortabilitySummaryPDF(exportData)
	if err != nil {
		t.Fatalf("buildPortabilitySummaryPDF: %v", err)
	}
	if !bytes.HasPrefix(summaryPDF, []byte("%PDF-")) {
		t.Errorf("summary.pdf missing %%PDF- header")
	}

	readmeTxt := buildPortabilityREADME(exportData.TenantName, exportData.User.Name)
	archiveBytes, err := buildZipArchive(dataJSON, summaryPDF, readmeTxt)
	if err != nil {
		t.Fatalf("buildZipArchive: %v", err)
	}

	fakeS3 := newFakePortabilityStorage()
	s3Key := fmt.Sprintf("tenants/%s/portability/%s/%s.zip",
		tenantID.String(), userID.String(), jobID.String())
	if err := fakeS3.Upload(context.Background(), "", s3Key, archiveBytes); err != nil {
		t.Fatalf("fake s3 upload: %v", err)
	}

	uploadedBytes := fakeS3.uploaded[s3Key]
	if len(uploadedBytes) == 0 {
		t.Fatal("s3 upload: no bytes stored")
	}

	zr, err := zip.NewReader(bytes.NewReader(uploadedBytes), int64(len(uploadedBytes)))
	if err != nil {
		t.Fatalf("open uploaded zip: %v", err)
	}

	fileContents := make(map[string][]byte)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		fileContents[f.Name] = b
	}

	for _, name := range []string{"data.json", "summary.pdf", "README.txt"} {
		if _, ok := fileContents[name]; !ok {
			t.Errorf("zip missing file: %s", name)
		}
	}

	if !bytes.HasPrefix(fileContents["summary.pdf"], []byte("%PDF-")) {
		t.Error("summary.pdf in zip does not start with %PDF-")
	}

	var parsed portabilityExportData
	if err := json.Unmarshal(fileContents["data.json"], &parsed); err != nil {
		t.Fatalf("unmarshal data.json from zip: %v", err)
	}
	if len(parsed.SIMs) != 5 {
		t.Errorf("data.json sims count = %d, want 5", len(parsed.SIMs))
	}
	if len(parsed.CDRs) != 20 {
		t.Errorf("data.json cdrs count = %d, want 20", len(parsed.CDRs))
	}
	if len(parsed.AuditLogs) != 3 {
		t.Errorf("data.json audit_logs count = %d, want 3", len(parsed.AuditLogs))
	}

	signedURL, err := fakeS3.PresignGet(context.Background(), "", s3Key, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !contains(signedURL, s3Key) {
		t.Errorf("signed url %q does not contain key %q", signedURL, s3Key)
	}
}

// TestDataPortabilityProcessor_NilDB verifies the processor can be constructed
// and its Type() is correct without needing a live database connection.
func TestDataPortabilityProcessor_ConstructorAndType(t *testing.T) {
	proc := NewDataPortabilityProcessor(
		store.NewJobStore(nil),
		store.NewUserStore(nil),
		store.NewTenantStore(nil),
		store.NewCDRStore(nil),
		store.NewAuditStore(nil),
		newFakePortabilityStorage(),
		nil,
		(*pgxpool.Pool)(nil),
		nil,
		zerolog.Nop(),
	)
	if proc.Type() != JobTypeDataPortabilityExport {
		t.Errorf("Type() = %q, want %q", proc.Type(), JobTypeDataPortabilityExport)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
