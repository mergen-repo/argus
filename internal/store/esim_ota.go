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
	ErrEsimOTACommandNotFound   = errors.New("store: esim ota command not found")
	ErrEsimOTAInvalidTransition = errors.New("store: invalid esim ota command state transition")
)

type EsimOTACommand struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	EID              string     `json:"eid"`
	ProfileID        *uuid.UUID `json:"profile_id,omitempty"`
	CommandType      string     `json:"command_type"`
	TargetOperatorID *uuid.UUID `json:"target_operator_id,omitempty"`
	SourceProfileID  *uuid.UUID `json:"source_profile_id,omitempty"`
	TargetProfileID  *uuid.UUID `json:"target_profile_id,omitempty"`
	Status           string     `json:"status"`
	SMSRCommandID    *string    `json:"smsr_command_id,omitempty"`
	RetryCount       int        `json:"retry_count"`
	LastError        *string    `json:"last_error,omitempty"`
	JobID            *uuid.UUID `json:"job_id,omitempty"`
	CorrelationID    *uuid.UUID `json:"correlation_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	SentAt           *time.Time `json:"sent_at,omitempty"`
	AckedAt          *time.Time `json:"acked_at,omitempty"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
}

type InsertEsimOTACommandParams struct {
	TenantID         uuid.UUID
	EID              string
	ProfileID        *uuid.UUID
	CommandType      string
	TargetOperatorID *uuid.UUID
	SourceProfileID  *uuid.UUID
	TargetProfileID  *uuid.UUID
	CorrelationID    *uuid.UUID
	JobID            *uuid.UUID
}

var esimOTACommandColumns = `id, tenant_id, eid, profile_id, command_type, target_operator_id,
	source_profile_id, target_profile_id, status, smsr_command_id, retry_count, last_error,
	job_id, correlation_id, created_at, sent_at, acked_at, next_retry_at`

func scanEsimOTACommand(row pgx.Row) (*EsimOTACommand, error) {
	var c EsimOTACommand
	err := row.Scan(
		&c.ID, &c.TenantID, &c.EID, &c.ProfileID, &c.CommandType, &c.TargetOperatorID,
		&c.SourceProfileID, &c.TargetProfileID, &c.Status, &c.SMSRCommandID, &c.RetryCount, &c.LastError,
		&c.JobID, &c.CorrelationID, &c.CreatedAt, &c.SentAt, &c.AckedAt, &c.NextRetryAt,
	)
	return &c, err
}

type EsimOTACommandStore struct {
	db *pgxpool.Pool
}

func NewEsimOTACommandStore(db *pgxpool.Pool) *EsimOTACommandStore {
	return &EsimOTACommandStore{db: db}
}

func (s *EsimOTACommandStore) Insert(ctx context.Context, p InsertEsimOTACommandParams) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.QueryRow(ctx,
		`INSERT INTO esim_ota_commands
		(tenant_id, eid, profile_id, command_type, target_operator_id, source_profile_id, target_profile_id, correlation_id, job_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id`,
		p.TenantID, p.EID, p.ProfileID, p.CommandType, p.TargetOperatorID,
		p.SourceProfileID, p.TargetProfileID, p.CorrelationID, p.JobID,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: insert esim ota command: %w", err)
	}
	return id, nil
}

func (s *EsimOTACommandStore) MarkSent(ctx context.Context, id uuid.UUID, smsrCommandID string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE esim_ota_commands
		SET status='sent', smsr_command_id=$2, sent_at=NOW()
		WHERE id=$1 AND status='queued'`,
		id, smsrCommandID,
	)
	if err != nil {
		return fmt.Errorf("store: mark esim ota sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEsimOTAInvalidTransition
	}
	return nil
}

func (s *EsimOTACommandStore) MarkAcked(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE esim_ota_commands
		SET status='acked', acked_at=NOW()
		WHERE id=$1 AND status='sent'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("store: mark esim ota acked: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEsimOTAInvalidTransition
	}
	return nil
}

func (s *EsimOTACommandStore) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE esim_ota_commands
		SET status='failed', last_error=$2
		WHERE id=$1 AND status IN ('queued','sent')`,
		id, errMsg,
	)
	if err != nil {
		return fmt.Errorf("store: mark esim ota failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEsimOTAInvalidTransition
	}
	return nil
}

func (s *EsimOTACommandStore) MarkTimeout(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE esim_ota_commands
		SET status='timeout'
		WHERE id=$1 AND status='sent'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("store: mark esim ota timeout: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEsimOTAInvalidTransition
	}
	return nil
}

func (s *EsimOTACommandStore) IncrementRetry(ctx context.Context, id uuid.UUID, nextRetryAt time.Time) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE esim_ota_commands
		SET status='queued', retry_count=retry_count+1, next_retry_at=$2
		WHERE id=$1`,
		id, nextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("store: increment esim ota retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrEsimOTACommandNotFound
	}
	return nil
}

func (s *EsimOTACommandStore) ListQueued(ctx context.Context, limit int, now time.Time) ([]EsimOTACommand, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+esimOTACommandColumns+`
		FROM esim_ota_commands
		WHERE status='queued' AND (next_retry_at IS NULL OR next_retry_at <= $1)
		ORDER BY created_at ASC
		LIMIT $2`,
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list queued esim ota commands: %w", err)
	}
	defer rows.Close()
	return scanEsimOTACommands(rows)
}

func (s *EsimOTACommandStore) ListSentBefore(ctx context.Context, cutoff time.Time) ([]EsimOTACommand, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+esimOTACommandColumns+`
		FROM esim_ota_commands
		WHERE status='sent' AND sent_at < $1`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list sent before cutoff esim ota commands: %w", err)
	}
	defer rows.Close()
	return scanEsimOTACommands(rows)
}

func (s *EsimOTACommandStore) ListByEID(ctx context.Context, tenantID uuid.UUID, eid, cursor string, limit int) ([]EsimOTACommand, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID, eid}
	where := `WHERE tenant_id=$1 AND eid=$2`
	argIdx := 3

	if cursor != "" {
		if cursorID, err := uuid.Parse(cursor); err == nil {
			where += fmt.Sprintf(` AND id < $%d`, argIdx)
			args = append(args, cursorID)
			argIdx++
		}
	}

	args = append(args, limit+1)
	rows, err := s.db.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM esim_ota_commands %s ORDER BY created_at DESC, id DESC LIMIT $%d`,
			esimOTACommandColumns, where, argIdx),
		args...,
	)
	if err != nil {
		return nil, "", fmt.Errorf("store: list esim ota commands by eid: %w", err)
	}
	defer rows.Close()

	results, err := scanEsimOTACommands(rows)
	if err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}
	return results, nextCursor, nil
}

func (s *EsimOTACommandStore) GetByID(ctx context.Context, id uuid.UUID) (*EsimOTACommand, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+esimOTACommandColumns+` FROM esim_ota_commands WHERE id=$1`,
		id,
	)
	c, err := scanEsimOTACommand(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEsimOTACommandNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get esim ota command by id: %w", err)
	}
	return c, nil
}

func (s *EsimOTACommandStore) BatchInsert(ctx context.Context, params []InsertEsimOTACommandParams) (int, error) {
	if len(params) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin batch insert esim ota: %w", err)
	}
	defer tx.Rollback(ctx)

	count := 0
	for _, p := range params {
		var id uuid.UUID
		err := tx.QueryRow(ctx,
			`INSERT INTO esim_ota_commands
			(tenant_id, eid, profile_id, command_type, target_operator_id, source_profile_id, target_profile_id, correlation_id, job_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			RETURNING id`,
			p.TenantID, p.EID, p.ProfileID, p.CommandType, p.TargetOperatorID,
			p.SourceProfileID, p.TargetProfileID, p.CorrelationID, p.JobID,
		).Scan(&id)
		if err != nil {
			return count, fmt.Errorf("store: batch insert esim ota command: %w", err)
		}
		count++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit batch insert esim ota: %w", err)
	}
	return count, nil
}

func scanEsimOTACommands(rows pgx.Rows) ([]EsimOTACommand, error) {
	var results []EsimOTACommand
	for rows.Next() {
		c, err := scanEsimOTACommand(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan esim ota command: %w", err)
		}
		results = append(results, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iter esim ota commands: %w", err)
	}
	return results, nil
}
