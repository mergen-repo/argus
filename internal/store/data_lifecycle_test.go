package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTenantRetentionConfig_Defaults(t *testing.T) {
	cfg := TenantRetentionConfig{
		ID:                   uuid.New(),
		TenantID:             uuid.New(),
		CDRRetentionDays:     365,
		SessionRetentionDays: 365,
		AuditRetentionDays:   730,
		S3ArchivalEnabled:    false,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	if cfg.CDRRetentionDays != 365 {
		t.Errorf("expected default CDR retention 365 days, got %d", cfg.CDRRetentionDays)
	}
	if cfg.SessionRetentionDays != 365 {
		t.Errorf("expected default session retention 365 days, got %d", cfg.SessionRetentionDays)
	}
	if cfg.AuditRetentionDays != 730 {
		t.Errorf("expected default audit retention 730 days, got %d", cfg.AuditRetentionDays)
	}
	if cfg.S3ArchivalEnabled {
		t.Error("expected S3 archival disabled by default")
	}
}

func TestS3ArchivalRecord_Fields(t *testing.T) {
	now := time.Now()
	bucket := "argus-storage"

	rec := S3ArchivalRecord{
		ID:              uuid.New(),
		TenantID:        uuid.New(),
		TableName:       "cdrs",
		ChunkName:       "_hyper_1_1_chunk",
		ChunkRangeStart: now.Add(-30 * 24 * time.Hour),
		ChunkRangeEnd:   now.Add(-23 * 24 * time.Hour),
		S3Bucket:        bucket,
		S3Key:           "archival/tenant-id/cdrs/2025-12/2025-12-31.csv",
		SizeBytes:       1024 * 1024 * 50,
		RowCount:        100000,
		Status:          "completed",
		ArchivedAt:      &now,
		CreatedAt:       now,
	}

	if rec.TableName != "cdrs" {
		t.Errorf("expected table cdrs, got %s", rec.TableName)
	}
	if rec.Status != "completed" {
		t.Errorf("expected completed status, got %s", rec.Status)
	}
	if rec.RowCount != 100000 {
		t.Errorf("expected 100000 rows, got %d", rec.RowCount)
	}
	if rec.S3Bucket != bucket {
		t.Errorf("expected bucket %s, got %s", bucket, rec.S3Bucket)
	}
}

func TestS3ArchivalRecord_Statuses(t *testing.T) {
	statuses := []string{"pending", "uploading", "completed", "failed"}
	for _, s := range statuses {
		rec := S3ArchivalRecord{Status: s}
		if rec.Status != s {
			t.Errorf("expected status %s, got %s", s, rec.Status)
		}
	}
}

func TestTenantRetentionConfig_WithS3(t *testing.T) {
	bucket := "custom-bucket"
	cfg := TenantRetentionConfig{
		ID:                uuid.New(),
		TenantID:          uuid.New(),
		CDRRetentionDays:  180,
		S3ArchivalEnabled: true,
		S3ArchivalBucket:  &bucket,
	}

	if !cfg.S3ArchivalEnabled {
		t.Error("expected S3 archival enabled")
	}
	if cfg.S3ArchivalBucket == nil || *cfg.S3ArchivalBucket != "custom-bucket" {
		t.Error("expected custom S3 bucket")
	}
	if cfg.CDRRetentionDays != 180 {
		t.Errorf("expected 180 day retention, got %d", cfg.CDRRetentionDays)
	}
}
