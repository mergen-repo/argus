package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAuditNotFound     = errors.New("store: audit entry not found")
	ErrDateRangeRequired = errors.New("store: from and to date range are required")
	ErrDateRangeTooLarge = errors.New("store: date range must not exceed 90 days")
)

type ListAuditParams struct {
	Cursor     string
	Limit      int
	From       *time.Time
	To         *time.Time
	UserID     *uuid.UUID
	Action     string
	Actions    []string
	EntityType string
	EntityID   string
}

type AuditStore struct {
	db *pgxpool.Pool
}

func NewAuditStore(db *pgxpool.Pool) *AuditStore {
	return &AuditStore{db: db}
}

func (s *AuditStore) CreateWithChain(ctx context.Context, entry *audit.Entry) (*audit.Entry, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin audit chain tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7166482937211513)`) // fixed sentinel: audit chain single-writer lock
	if err != nil {
		return nil, fmt.Errorf("store: acquire audit chain lock: %w", err)
	}

	var tailHash string
	err = tx.QueryRow(ctx, `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&tailHash)
	if errors.Is(err, pgx.ErrNoRows) {
		tailHash = audit.GenesisHash
	} else if err != nil {
		return nil, fmt.Errorf("store: read chain tail: %w", err)
	}

	entry.PrevHash = tailHash
	entry.CreatedAt = entry.CreatedAt.Truncate(time.Microsecond)
	entry.Hash = audit.ComputeHash(*entry, tailHash)

	err = tx.QueryRow(ctx, `
		INSERT INTO audit_logs (tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address, user_agent, correlation_id, hash, prev_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::inet, $11, $12, $13, $14, $15)
		RETURNING id, created_at
	`, entry.TenantID, entry.UserID, entry.APIKeyID, entry.Action, entry.EntityType, entry.EntityID,
		entry.BeforeData, entry.AfterData, entry.Diff, entry.IPAddress, entry.UserAgent,
		entry.CorrelationID, entry.Hash, entry.PrevHash, entry.CreatedAt).
		Scan(&entry.ID, &entry.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: insert audit entry: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit audit chain tx: %w", err)
	}

	return entry, nil
}

func (s *AuditStore) GetAll(ctx context.Context) ([]audit.Entry, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address::text, user_agent, correlation_id,
			hash, prev_hash, created_at
		FROM audit_logs
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: get all audit logs: %w", err)
	}
	defer rows.Close()

	var results []audit.Entry
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.APIKeyID,
			&e.Action, &e.EntityType, &e.EntityID,
			&e.BeforeData, &e.AfterData, &e.Diff,
			&e.IPAddress, &e.UserAgent, &e.CorrelationID,
			&e.Hash, &e.PrevHash, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan audit entry: %w", err)
		}
		results = append(results, e)
	}

	return results, nil
}

func (s *AuditStore) GetBatch(ctx context.Context, afterID int64, limit int) ([]audit.Entry, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address::text, user_agent, correlation_id,
			hash, prev_hash, created_at
		FROM audit_logs
		WHERE id > $1
		ORDER BY id ASC
		LIMIT $2
	`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: get audit batch: %w", err)
	}
	defer rows.Close()

	var results []audit.Entry
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.APIKeyID,
			&e.Action, &e.EntityType, &e.EntityID,
			&e.BeforeData, &e.AfterData, &e.Diff,
			&e.IPAddress, &e.UserAgent, &e.CorrelationID,
			&e.Hash, &e.PrevHash, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan audit batch entry: %w", err)
		}
		results = append(results, e)
	}

	return results, nil
}

const repairBatchSize = 1000

func (s *AuditStore) RepairChain(ctx context.Context) error {
	prevHash := audit.GenesisHash
	var afterID int64

	for {
		batch, err := s.GetBatch(ctx, afterID, repairBatchSize)
		if err != nil {
			return fmt.Errorf("store: repair chain read batch after id=%d: %w", afterID, err)
		}
		if len(batch) == 0 {
			break
		}

		for i := range batch {
			batch[i].PrevHash = prevHash
			batch[i].Hash = audit.ComputeHash(batch[i], prevHash)
			prevHash = batch[i].Hash
		}

		tx, err := s.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("store: repair chain begin tx: %w", err)
		}

		for _, e := range batch {
			_, err := tx.Exec(ctx, `
				UPDATE audit_logs SET hash = $1, prev_hash = $2
				WHERE id = $3 AND created_at = $4
			`, e.Hash, e.PrevHash, e.ID, e.CreatedAt)
			if err != nil {
				tx.Rollback(ctx)
				return fmt.Errorf("store: repair chain update id=%d: %w", e.ID, err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("store: repair chain commit tx: %w", err)
		}

		afterID = batch[len(batch)-1].ID
	}

	return nil
}

func (s *AuditStore) List(ctx context.Context, tenantID uuid.UUID, params ListAuditParams) ([]audit.Entry, string, error) {
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if params.From != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *params.From)
		argIdx++
	}
	if params.To != nil {
		conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *params.To)
		argIdx++
	}
	if params.UserID != nil {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", argIdx))
		args = append(args, *params.UserID)
		argIdx++
	}
	if params.Action != "" {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argIdx))
		args = append(args, params.Action)
		argIdx++
	}
	if len(params.Actions) > 0 {
		conditions = append(conditions, fmt.Sprintf("action = ANY($%d)", argIdx))
		args = append(args, params.Actions)
		argIdx++
	}
	if params.EntityType != "" {
		conditions = append(conditions, fmt.Sprintf("entity_type = $%d", argIdx))
		args = append(args, params.EntityType)
		argIdx++
	}
	if params.EntityID != "" {
		conditions = append(conditions, fmt.Sprintf("entity_id = $%d", argIdx))
		args = append(args, params.EntityID)
		argIdx++
	}
	if params.Cursor != "" {
		conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
		args = append(args, params.Cursor)
		argIdx++
	}

	args = append(args, params.Limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address::text, user_agent, correlation_id,
			hash, prev_hash, created_at
		FROM audit_logs
		WHERE %s
		ORDER BY id DESC
		LIMIT %s
	`, strings.Join(conditions, " AND "), limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list audit logs: %w", err)
	}
	defer rows.Close()

	var results []audit.Entry
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.APIKeyID,
			&e.Action, &e.EntityType, &e.EntityID,
			&e.BeforeData, &e.AfterData, &e.Diff,
			&e.IPAddress, &e.UserAgent, &e.CorrelationID,
			&e.Hash, &e.PrevHash, &e.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("store: scan audit entry: %w", err)
		}
		results = append(results, e)
	}

	nextCursor := ""
	if len(results) > params.Limit {
		nextCursor = fmt.Sprintf("%d", results[params.Limit-1].ID)
		results = results[:params.Limit]
	}

	return results, nextCursor, nil
}

func (s *AuditStore) GetRange(ctx context.Context, tenantID uuid.UUID, count int) ([]audit.Entry, error) {
	if count <= 0 {
		count = 100
	}
	if count > 10000 {
		count = 10000
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address::text, user_agent, correlation_id,
			hash, prev_hash, created_at
		FROM audit_logs
		WHERE tenant_id = $1
		ORDER BY id DESC
		LIMIT $2
	`, tenantID, count)
	if err != nil {
		return nil, fmt.Errorf("store: get audit range: %w", err)
	}
	defer rows.Close()

	var results []audit.Entry
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.APIKeyID,
			&e.Action, &e.EntityType, &e.EntityID,
			&e.BeforeData, &e.AfterData, &e.Diff,
			&e.IPAddress, &e.UserAgent, &e.CorrelationID,
			&e.Hash, &e.PrevHash, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan audit entry: %w", err)
		}
		results = append(results, e)
	}

	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results, nil
}

func (s *AuditStore) GetByDateRange(ctx context.Context, tenantID uuid.UUID, from, to time.Time) ([]audit.Entry, error) {
	if from.IsZero() || to.IsZero() {
		return nil, ErrDateRangeRequired
	}
	if to.Sub(from) > 90*24*time.Hour {
		return nil, ErrDateRangeTooLarge
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address::text, user_agent, correlation_id,
			hash, prev_hash, created_at
		FROM audit_logs
		WHERE tenant_id = $1 AND created_at >= $2 AND created_at <= $3
		ORDER BY id ASC
	`, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: get audit by date range: %w", err)
	}
	defer rows.Close()

	var results []audit.Entry
	for rows.Next() {
		var e audit.Entry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.APIKeyID,
			&e.Action, &e.EntityType, &e.EntityID,
			&e.BeforeData, &e.AfterData, &e.Diff,
			&e.IPAddress, &e.UserAgent, &e.CorrelationID,
			&e.Hash, &e.PrevHash, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan audit entry: %w", err)
		}
		results = append(results, e)
	}

	return results, nil
}

func (s *AuditStore) Pseudonymize(ctx context.Context, tenantID uuid.UUID, entityIDs []string, tenantSalt string) error {
	if len(entityIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(entityIDs))
	args := []interface{}{tenantID}
	for i, eid := range entityIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, eid)
	}

	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT id, created_at, before_data, after_data, diff
		FROM audit_logs
		WHERE tenant_id = $1
		  AND entity_type = 'sim'
		  AND entity_id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return fmt.Errorf("store: pseudonymize select: %w", err)
	}
	defer rows.Close()

	sensitiveFields := []string{"imsi", "msisdn", "iccid"}

	type pseudoRow struct {
		id         int64
		createdAt  time.Time
		beforeData json.RawMessage
		afterData  json.RawMessage
		diff       json.RawMessage
	}

	var pending []pseudoRow
	for rows.Next() {
		var r pseudoRow
		if err := rows.Scan(&r.id, &r.createdAt, &r.beforeData, &r.afterData, &r.diff); err != nil {
			return fmt.Errorf("store: pseudonymize scan: %w", err)
		}
		r.beforeData = anonymizeJSONWithSalt(r.beforeData, sensitiveFields, tenantSalt)
		r.afterData = anonymizeJSONWithSalt(r.afterData, sensitiveFields, tenantSalt)
		r.diff = anonymizeJSONWithSalt(r.diff, sensitiveFields, tenantSalt)
		pending = append(pending, r)
	}
	rows.Close()

	for _, r := range pending {
		_, err := s.db.Exec(ctx, `
			UPDATE audit_logs SET before_data = $1, after_data = $2, diff = $3
			WHERE id = $4 AND created_at = $5
		`, r.beforeData, r.afterData, r.diff, r.id, r.createdAt)
		if err != nil {
			return fmt.Errorf("store: pseudonymize update id=%d: %w", r.id, err)
		}
	}

	return nil
}

