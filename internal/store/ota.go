package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrOTACommandNotFound = errors.New("store: ota command not found")
)

type OTACommand struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	SimID        uuid.UUID       `json:"sim_id"`
	CommandType  string          `json:"command_type"`
	Channel      string          `json:"channel"`
	Status       string          `json:"status"`
	APDUData     []byte          `json:"apdu_data"`
	SecurityMode string          `json:"security_mode"`
	Payload      json.RawMessage `json:"payload"`
	ResponseData json.RawMessage `json:"response_data,omitempty"`
	ErrorMessage *string         `json:"error_message,omitempty"`
	JobID        *uuid.UUID      `json:"job_id,omitempty"`
	RetryCount   int             `json:"retry_count"`
	MaxRetries   int             `json:"max_retries"`
	CreatedBy    *uuid.UUID      `json:"created_by,omitempty"`
	SentAt       *time.Time      `json:"sent_at,omitempty"`
	DeliveredAt  *time.Time      `json:"delivered_at,omitempty"`
	ExecutedAt   *time.Time      `json:"executed_at,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

type CreateOTACommandParams struct {
	SimID        uuid.UUID
	CommandType  string
	Channel      string
	SecurityMode string
	APDUData     []byte
	Payload      json.RawMessage
	MaxRetries   int
	CreatedBy    *uuid.UUID
	JobID        *uuid.UUID
}

type OTACommandFilter struct {
	SimID       *uuid.UUID
	CommandType string
	Status      string
	Channel     string
}

type OTAStore struct {
	db *pgxpool.Pool
}

func NewOTAStore(db *pgxpool.Pool) *OTAStore {
	return &OTAStore{db: db}
}

func (s *OTAStore) Create(ctx context.Context, tenantID uuid.UUID, p CreateOTACommandParams) (*OTACommand, error) {
	payload := p.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}

	var cmd OTACommand
	err := s.db.QueryRow(ctx, `
		INSERT INTO ota_commands (
			tenant_id, sim_id, command_type, channel, status,
			apdu_data, security_mode, payload, max_retries,
			created_by, job_id
		) VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8, $9, $10)
		RETURNING id, tenant_id, sim_id, command_type, channel, status,
			apdu_data, security_mode, payload, response_data, error_message,
			job_id, retry_count, max_retries, created_by,
			sent_at, delivered_at, executed_at, completed_at, created_at
	`, tenantID, p.SimID, p.CommandType, p.Channel,
		p.APDUData, p.SecurityMode, payload, p.MaxRetries,
		p.CreatedBy, p.JobID,
	).Scan(
		&cmd.ID, &cmd.TenantID, &cmd.SimID, &cmd.CommandType, &cmd.Channel, &cmd.Status,
		&cmd.APDUData, &cmd.SecurityMode, &cmd.Payload, &cmd.ResponseData, &cmd.ErrorMessage,
		&cmd.JobID, &cmd.RetryCount, &cmd.MaxRetries, &cmd.CreatedBy,
		&cmd.SentAt, &cmd.DeliveredAt, &cmd.ExecutedAt, &cmd.CompletedAt, &cmd.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create ota command: %w", err)
	}
	return &cmd, nil
}

func (s *OTAStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*OTACommand, error) {
	var cmd OTACommand
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, sim_id, command_type, channel, status,
			apdu_data, security_mode, payload, response_data, error_message,
			job_id, retry_count, max_retries, created_by,
			sent_at, delivered_at, executed_at, completed_at, created_at
		FROM ota_commands
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&cmd.ID, &cmd.TenantID, &cmd.SimID, &cmd.CommandType, &cmd.Channel, &cmd.Status,
		&cmd.APDUData, &cmd.SecurityMode, &cmd.Payload, &cmd.ResponseData, &cmd.ErrorMessage,
		&cmd.JobID, &cmd.RetryCount, &cmd.MaxRetries, &cmd.CreatedBy,
		&cmd.SentAt, &cmd.DeliveredAt, &cmd.ExecutedAt, &cmd.CompletedAt, &cmd.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOTACommandNotFound
		}
		return nil, fmt.Errorf("get ota command: %w", err)
	}
	return &cmd, nil
}

func (s *OTAStore) GetByIDInternal(ctx context.Context, id uuid.UUID) (*OTACommand, error) {
	var cmd OTACommand
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, sim_id, command_type, channel, status,
			apdu_data, security_mode, payload, response_data, error_message,
			job_id, retry_count, max_retries, created_by,
			sent_at, delivered_at, executed_at, completed_at, created_at
		FROM ota_commands
		WHERE id = $1
	`, id).Scan(
		&cmd.ID, &cmd.TenantID, &cmd.SimID, &cmd.CommandType, &cmd.Channel, &cmd.Status,
		&cmd.APDUData, &cmd.SecurityMode, &cmd.Payload, &cmd.ResponseData, &cmd.ErrorMessage,
		&cmd.JobID, &cmd.RetryCount, &cmd.MaxRetries, &cmd.CreatedBy,
		&cmd.SentAt, &cmd.DeliveredAt, &cmd.ExecutedAt, &cmd.CompletedAt, &cmd.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOTACommandNotFound
		}
		return nil, fmt.Errorf("get ota command internal: %w", err)
	}
	return &cmd, nil
}

func (s *OTAStore) ListBySimID(ctx context.Context, tenantID, simID uuid.UUID, cursor string, limit int, filter OTACommandFilter) ([]OTACommand, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []interface{}{tenantID, simID}
	conditions := []string{"tenant_id = $1", "sim_id = $2"}
	argIdx := 3

	if filter.CommandType != "" {
		conditions = append(conditions, fmt.Sprintf("command_type = $%d", argIdx))
		args = append(args, filter.CommandType)
		argIdx++
	}
	if filter.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Channel != "" {
		conditions = append(conditions, fmt.Sprintf("channel = $%d", argIdx))
		args = append(args, filter.Channel)
		argIdx++
	}

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, sim_id, command_type, channel, status,
			apdu_data, security_mode, payload, response_data, error_message,
			job_id, retry_count, max_retries, created_by,
			sent_at, delivered_at, executed_at, completed_at, created_at
		FROM ota_commands
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list ota commands: %w", err)
	}
	defer rows.Close()

	var results []OTACommand
	for rows.Next() {
		var cmd OTACommand
		if err := rows.Scan(
			&cmd.ID, &cmd.TenantID, &cmd.SimID, &cmd.CommandType, &cmd.Channel, &cmd.Status,
			&cmd.APDUData, &cmd.SecurityMode, &cmd.Payload, &cmd.ResponseData, &cmd.ErrorMessage,
			&cmd.JobID, &cmd.RetryCount, &cmd.MaxRetries, &cmd.CreatedBy,
			&cmd.SentAt, &cmd.DeliveredAt, &cmd.ExecutedAt, &cmd.CompletedAt, &cmd.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("scan ota command: %w", err)
		}
		results = append(results, cmd)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *OTAStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, responseData json.RawMessage, errorMessage *string) error {
	var timestampCol string
	switch status {
	case "sent":
		timestampCol = "sent_at"
	case "delivered":
		timestampCol = "delivered_at"
	case "executed":
		timestampCol = "executed_at"
	case "confirmed", "failed":
		timestampCol = "completed_at"
	}

	query := `UPDATE ota_commands SET status = $2, response_data = COALESCE($3, response_data), error_message = COALESCE($4, error_message)`
	if timestampCol != "" {
		query += fmt.Sprintf(", %s = NOW()", timestampCol)
	}
	query += ` WHERE id = $1`

	tag, err := s.db.Exec(ctx, query, id, status, responseData, errorMessage)
	if err != nil {
		return fmt.Errorf("update ota status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOTACommandNotFound
	}
	return nil
}

func (s *OTAStore) IncrementRetry(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE ota_commands SET retry_count = retry_count + 1, status = 'queued'
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("increment retry: %w", err)
	}
	return nil
}

func (s *OTAStore) CountBySimInWindow(ctx context.Context, simID uuid.UUID, window time.Duration) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM ota_commands
		WHERE sim_id = $1 AND created_at > NOW() - $2::interval
	`, simID, fmt.Sprintf("%d seconds", int(window.Seconds()))).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count ota commands: %w", err)
	}
	return count, nil
}

func (s *OTAStore) ListByJobID(ctx context.Context, jobID uuid.UUID) ([]OTACommand, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, sim_id, command_type, channel, status,
			apdu_data, security_mode, payload, response_data, error_message,
			job_id, retry_count, max_retries, created_by,
			sent_at, delivered_at, executed_at, completed_at, created_at
		FROM ota_commands
		WHERE job_id = $1
		ORDER BY created_at ASC
	`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list ota by job: %w", err)
	}
	defer rows.Close()

	var results []OTACommand
	for rows.Next() {
		var cmd OTACommand
		if err := rows.Scan(
			&cmd.ID, &cmd.TenantID, &cmd.SimID, &cmd.CommandType, &cmd.Channel, &cmd.Status,
			&cmd.APDUData, &cmd.SecurityMode, &cmd.Payload, &cmd.ResponseData, &cmd.ErrorMessage,
			&cmd.JobID, &cmd.RetryCount, &cmd.MaxRetries, &cmd.CreatedBy,
			&cmd.SentAt, &cmd.DeliveredAt, &cmd.ExecutedAt, &cmd.CompletedAt, &cmd.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ota command: %w", err)
		}
		results = append(results, cmd)
	}

	return results, nil
}
