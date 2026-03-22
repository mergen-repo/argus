package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrRetentionConfigNotFound = errors.New("store: retention config not found")
)

type TenantRetentionConfig struct {
	ID                   uuid.UUID `json:"id"`
	TenantID             uuid.UUID `json:"tenant_id"`
	CDRRetentionDays     int       `json:"cdr_retention_days"`
	SessionRetentionDays int       `json:"session_retention_days"`
	AuditRetentionDays   int       `json:"audit_retention_days"`
	S3ArchivalEnabled    bool      `json:"s3_archival_enabled"`
	S3ArchivalBucket     *string   `json:"s3_archival_bucket,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type S3ArchivalRecord struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	TableName       string     `json:"table_name"`
	ChunkName       string     `json:"chunk_name"`
	ChunkRangeStart time.Time  `json:"chunk_range_start"`
	ChunkRangeEnd   time.Time  `json:"chunk_range_end"`
	S3Bucket        string     `json:"s3_bucket"`
	S3Key           string     `json:"s3_key"`
	SizeBytes       int64      `json:"size_bytes"`
	RowCount        int64      `json:"row_count"`
	Status          string     `json:"status"`
	ErrorMessage    *string    `json:"error_message,omitempty"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type DataLifecycleStore struct {
	db *pgxpool.Pool
}

func NewDataLifecycleStore(db *pgxpool.Pool) *DataLifecycleStore {
	return &DataLifecycleStore{db: db}
}

func (s *DataLifecycleStore) GetRetentionConfig(ctx context.Context, tenantID uuid.UUID) (*TenantRetentionConfig, error) {
	var cfg TenantRetentionConfig
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, cdr_retention_days, session_retention_days,
			audit_retention_days, s3_archival_enabled, s3_archival_bucket,
			created_at, updated_at
		FROM tenant_retention_config
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.CDRRetentionDays, &cfg.SessionRetentionDays,
		&cfg.AuditRetentionDays, &cfg.S3ArchivalEnabled, &cfg.S3ArchivalBucket,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRetentionConfigNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get retention config: %w", err)
	}
	return &cfg, nil
}

func (s *DataLifecycleStore) UpsertRetentionConfig(ctx context.Context, tenantID uuid.UUID, cdrDays, sessionDays, auditDays int, s3Enabled bool, s3Bucket *string) (*TenantRetentionConfig, error) {
	var cfg TenantRetentionConfig
	err := s.db.QueryRow(ctx, `
		INSERT INTO tenant_retention_config (tenant_id, cdr_retention_days, session_retention_days, audit_retention_days, s3_archival_enabled, s3_archival_bucket)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id) DO UPDATE SET
			cdr_retention_days = EXCLUDED.cdr_retention_days,
			session_retention_days = EXCLUDED.session_retention_days,
			audit_retention_days = EXCLUDED.audit_retention_days,
			s3_archival_enabled = EXCLUDED.s3_archival_enabled,
			s3_archival_bucket = EXCLUDED.s3_archival_bucket,
			updated_at = NOW()
		RETURNING id, tenant_id, cdr_retention_days, session_retention_days,
			audit_retention_days, s3_archival_enabled, s3_archival_bucket,
			created_at, updated_at
	`, tenantID, cdrDays, sessionDays, auditDays, s3Enabled, s3Bucket).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.CDRRetentionDays, &cfg.SessionRetentionDays,
		&cfg.AuditRetentionDays, &cfg.S3ArchivalEnabled, &cfg.S3ArchivalBucket,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: upsert retention config: %w", err)
	}
	return &cfg, nil
}

func (s *DataLifecycleStore) ListRetentionConfigs(ctx context.Context) ([]TenantRetentionConfig, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, cdr_retention_days, session_retention_days,
			audit_retention_days, s3_archival_enabled, s3_archival_bucket,
			created_at, updated_at
		FROM tenant_retention_config
		ORDER BY tenant_id
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list retention configs: %w", err)
	}
	defer rows.Close()

	var results []TenantRetentionConfig
	for rows.Next() {
		var cfg TenantRetentionConfig
		if err := rows.Scan(
			&cfg.ID, &cfg.TenantID, &cfg.CDRRetentionDays, &cfg.SessionRetentionDays,
			&cfg.AuditRetentionDays, &cfg.S3ArchivalEnabled, &cfg.S3ArchivalBucket,
			&cfg.CreatedAt, &cfg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan retention config: %w", err)
		}
		results = append(results, cfg)
	}
	return results, nil
}

func (s *DataLifecycleStore) CreateArchivalRecord(ctx context.Context, rec S3ArchivalRecord) (*S3ArchivalRecord, error) {
	var result S3ArchivalRecord
	err := s.db.QueryRow(ctx, `
		INSERT INTO s3_archival_log (tenant_id, table_name, chunk_name, chunk_range_start, chunk_range_end,
			s3_bucket, s3_key, size_bytes, row_count, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, table_name, chunk_name, chunk_range_start, chunk_range_end,
			s3_bucket, s3_key, size_bytes, row_count, status, error_message, archived_at, created_at
	`, rec.TenantID, rec.TableName, rec.ChunkName, rec.ChunkRangeStart, rec.ChunkRangeEnd,
		rec.S3Bucket, rec.S3Key, rec.SizeBytes, rec.RowCount, rec.Status,
	).Scan(
		&result.ID, &result.TenantID, &result.TableName, &result.ChunkName,
		&result.ChunkRangeStart, &result.ChunkRangeEnd,
		&result.S3Bucket, &result.S3Key, &result.SizeBytes, &result.RowCount,
		&result.Status, &result.ErrorMessage, &result.ArchivedAt, &result.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create archival record: %w", err)
	}
	return &result, nil
}

func (s *DataLifecycleStore) UpdateArchivalStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	query := `UPDATE s3_archival_log SET status = $2, error_message = $3`
	if status == "completed" {
		query += `, archived_at = NOW()`
	}
	query += ` WHERE id = $1`

	_, err := s.db.Exec(ctx, query, id, status, errMsg)
	if err != nil {
		return fmt.Errorf("store: update archival status: %w", err)
	}
	return nil
}

func (s *DataLifecycleStore) ListArchivalRecords(ctx context.Context, tenantID uuid.UUID, limit int) ([]S3ArchivalRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, table_name, chunk_name, chunk_range_start, chunk_range_end,
			s3_bucket, s3_key, size_bytes, row_count, status, error_message, archived_at, created_at
		FROM s3_archival_log
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list archival records: %w", err)
	}
	defer rows.Close()

	var results []S3ArchivalRecord
	for rows.Next() {
		var r S3ArchivalRecord
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.TableName, &r.ChunkName,
			&r.ChunkRangeStart, &r.ChunkRangeEnd,
			&r.S3Bucket, &r.S3Key, &r.SizeBytes, &r.RowCount,
			&r.Status, &r.ErrorMessage, &r.ArchivedAt, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan archival record: %w", err)
		}
		results = append(results, r)
	}
	return results, nil
}

func (s *DataLifecycleStore) StreamCDRsForArchival(ctx context.Context, tenantID uuid.UUID, from, to time.Time, callback func(CDR) error) error {
	rows, err := s.db.Query(ctx, `
		SELECT `+cdrColumns+` FROM cdrs
		WHERE tenant_id = $1 AND timestamp >= $2 AND timestamp < $3
		ORDER BY timestamp ASC
	`, tenantID, from, to)
	if err != nil {
		return fmt.Errorf("store: stream cdrs for archival: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var c CDR
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.SimID, &c.TenantID, &c.OperatorID,
			&c.APNID, &c.RATType, &c.RecordType,
			&c.BytesIn, &c.BytesOut, &c.DurationSec,
			&c.UsageCost, &c.CarrierCost, &c.RatePerMB, &c.RATMultiplier,
			&c.Timestamp,
		); err != nil {
			return fmt.Errorf("store: scan archival cdr: %w", err)
		}
		if err := callback(c); err != nil {
			return fmt.Errorf("store: archival callback: %w", err)
		}
	}
	return nil
}
