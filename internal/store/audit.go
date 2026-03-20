package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

var ErrAuditNotFound = errors.New("store: audit entry not found")

type ListAuditParams struct {
	Cursor     string
	Limit      int
	From       *time.Time
	To         *time.Time
	UserID     *uuid.UUID
	Action     string
	EntityType string
	EntityID   string
}

type AuditStore struct {
	db *pgxpool.Pool
}

func NewAuditStore(db *pgxpool.Pool) *AuditStore {
	return &AuditStore{db: db}
}

func (s *AuditStore) Create(ctx context.Context, entry *audit.Entry) (*audit.Entry, error) {
	err := s.db.QueryRow(ctx, `
		INSERT INTO audit_logs (tenant_id, user_id, api_key_id, action, entity_type, entity_id,
			before_data, after_data, diff, ip_address, user_agent, correlation_id, hash, prev_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::inet, $11, $12, $13, $14, $15)
		RETURNING id, created_at
	`, entry.TenantID, entry.UserID, entry.APIKeyID, entry.Action, entry.EntityType, entry.EntityID,
		entry.BeforeData, entry.AfterData, entry.Diff, entry.IPAddress, entry.UserAgent,
		entry.CorrelationID, entry.Hash, entry.PrevHash, entry.CreatedAt).
		Scan(&entry.ID, &entry.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: create audit entry: %w", err)
	}
	return entry, nil
}

func (s *AuditStore) GetLastHash(ctx context.Context, tenantID uuid.UUID) (string, error) {
	var hash string
	err := s.db.QueryRow(ctx, `
		SELECT hash FROM audit_logs
		WHERE tenant_id = $1
		ORDER BY id DESC
		LIMIT 1
	`, tenantID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return audit.GenesisHash, nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get last audit hash: %w", err)
	}
	return hash, nil
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

func (s *AuditStore) Pseudonymize(ctx context.Context, tenantID uuid.UUID, entityIDs []string) error {
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

	for rows.Next() {
		var id int64
		var createdAt time.Time
		var beforeData, afterData, diff json.RawMessage

		if err := rows.Scan(&id, &createdAt, &beforeData, &afterData, &diff); err != nil {
			return fmt.Errorf("store: pseudonymize scan: %w", err)
		}

		beforeData = anonymizeJSON(beforeData, sensitiveFields)
		afterData = anonymizeJSON(afterData, sensitiveFields)
		diff = anonymizeJSON(diff, sensitiveFields)

		_, err := s.db.Exec(ctx, `
			UPDATE audit_logs SET before_data = $1, after_data = $2, diff = $3
			WHERE id = $4 AND created_at = $5
		`, beforeData, afterData, diff, id, createdAt)
		if err != nil {
			return fmt.Errorf("store: pseudonymize update id=%d: %w", id, err)
		}
	}

	return nil
}

func anonymizeJSON(data json.RawMessage, fields []string) json.RawMessage {
	if len(data) == 0 {
		return data
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}

	changed := false
	for _, field := range fields {
		if val, ok := m[field]; ok {
			if strVal, ok := val.(string); ok && strVal != "" {
				h := sha256.Sum256([]byte(strVal))
				m[field] = hex.EncodeToString(h[:])
				changed = true
			}
		}
	}

	if !changed {
		return data
	}

	result, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return result
}
