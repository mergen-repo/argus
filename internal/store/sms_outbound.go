package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSMSOutboundNotFound = errors.New("store: sms_outbound not found")
)

type SMSOutbound struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	SimID             uuid.UUID
	MSISDN            string
	TextHash          string
	TextPreview       string
	Status            string
	ProviderMessageID *string
	ErrorCode         *string
	QueuedAt          time.Time
	SentAt            *time.Time
	DeliveredAt       *time.Time
}

type SMSListFilters struct {
	SimID  *uuid.UUID
	Status *string
	From   *time.Time
	To     *time.Time
}

var smsOutboundColumns = `id, tenant_id, sim_id, msisdn, text_hash, text_preview,
	status, provider_message_id, error_code, queued_at, sent_at, delivered_at`

func scanSMSOutbound(row pgx.Row) (*SMSOutbound, error) {
	var s SMSOutbound
	err := row.Scan(
		&s.ID, &s.TenantID, &s.SimID, &s.MSISDN, &s.TextHash, &s.TextPreview,
		&s.Status, &s.ProviderMessageID, &s.ErrorCode, &s.QueuedAt, &s.SentAt, &s.DeliveredAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func scanSMSOutboundRows(rows pgx.Rows) (*SMSOutbound, error) {
	var s SMSOutbound
	err := rows.Scan(
		&s.ID, &s.TenantID, &s.SimID, &s.MSISDN, &s.TextHash, &s.TextPreview,
		&s.Status, &s.ProviderMessageID, &s.ErrorCode, &s.QueuedAt, &s.SentAt, &s.DeliveredAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

type SMSOutboundStore struct {
	db *pgxpool.Pool
}

func NewSMSOutboundStore(db *pgxpool.Pool) *SMSOutboundStore {
	return &SMSOutboundStore{db: db}
}

func (s *SMSOutboundStore) Insert(ctx context.Context, m *SMSOutbound) (*SMSOutbound, error) {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	if m.QueuedAt.IsZero() {
		m.QueuedAt = time.Now().UTC()
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO sms_outbound (id, tenant_id, sim_id, msisdn, text_hash, text_preview,
			status, provider_message_id, error_code, queued_at, sent_at, delivered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+smsOutboundColumns,
		m.ID, m.TenantID, m.SimID, m.MSISDN, m.TextHash, m.TextPreview,
		m.Status, m.ProviderMessageID, m.ErrorCode, m.QueuedAt, m.SentAt, m.DeliveredAt,
	)

	result, err := scanSMSOutbound(row)
	if err != nil {
		return nil, fmt.Errorf("store: insert sms_outbound: %w", err)
	}
	return result, nil
}

func (s *SMSOutboundStore) UpdateStatus(ctx context.Context, id uuid.UUID, status, providerMsgID, errorCode string, sentAt *time.Time) error {
	var providerID *string
	if providerMsgID != "" {
		providerID = &providerMsgID
	}
	var errCode *string
	if errorCode != "" {
		errCode = &errorCode
	}

	_, err := s.db.Exec(ctx, `
		UPDATE sms_outbound
		SET status = $2, provider_message_id = COALESCE($3, provider_message_id),
			error_code = $4, sent_at = COALESCE($5, sent_at)
		WHERE id = $1
	`, id, status, providerID, errCode, sentAt)
	if err != nil {
		return fmt.Errorf("store: update sms_outbound status: %w", err)
	}
	return nil
}

func (s *SMSOutboundStore) MarkDelivered(ctx context.Context, providerMsgID string, deliveredAt time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE sms_outbound
		SET status = 'delivered', delivered_at = $2
		WHERE provider_message_id = $1
	`, providerMsgID, deliveredAt)
	if err != nil {
		return fmt.Errorf("store: mark sms_outbound delivered: %w", err)
	}
	return nil
}

func (s *SMSOutboundStore) GetByID(ctx context.Context, id uuid.UUID) (*SMSOutbound, error) {
	row := s.db.QueryRow(ctx, `
		SELECT `+smsOutboundColumns+`
		FROM sms_outbound
		WHERE id = $1
	`, id)

	result, err := scanSMSOutbound(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSMSOutboundNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get sms_outbound by id: %w", err)
	}
	return result, nil
}

func (s *SMSOutboundStore) List(ctx context.Context, tenantID uuid.UUID, filters SMSListFilters, cursor string, limit int) ([]*SMSOutbound, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if filters.SimID != nil {
		conditions = append(conditions, fmt.Sprintf("sim_id = $%d", argIdx))
		args = append(args, *filters.SimID)
		argIdx++
	}
	if filters.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *filters.Status)
		argIdx++
	}
	if filters.From != nil {
		conditions = append(conditions, fmt.Sprintf("queued_at >= $%d", argIdx))
		args = append(args, *filters.From)
		argIdx++
	}
	if filters.To != nil {
		conditions = append(conditions, fmt.Sprintf("queued_at <= $%d", argIdx))
		args = append(args, *filters.To)
		argIdx++
	}

	if cursor != "" {
		parts := strings.SplitN(cursor, "_", 2)
		if len(parts) == 2 {
			unixSec, err1 := strconv.ParseInt(parts[0], 10, 64)
			cursorID, err2 := uuid.Parse(parts[1])
			if err1 == nil && err2 == nil {
				cursorTime := time.Unix(unixSec, 0).UTC()
				conditions = append(conditions, fmt.Sprintf(
					"(queued_at < $%d OR (queued_at = $%d AND id < $%d))",
					argIdx, argIdx, argIdx+1,
				))
				args = append(args, cursorTime, cursorID)
				argIdx += 2
			}
		}
	}

	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT `+smsOutboundColumns+`
		FROM sms_outbound
		WHERE %s
		ORDER BY queued_at DESC, id DESC
		LIMIT %s
	`, strings.Join(conditions, " AND "), limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list sms_outbound: %w", err)
	}
	defer rows.Close()

	var results []*SMSOutbound
	for rows.Next() {
		m, err := scanSMSOutboundRows(rows)
		if err != nil {
			return nil, "", fmt.Errorf("store: scan sms_outbound: %w", err)
		}
		results = append(results, m)
	}

	nextCursor := ""
	if len(results) > limit {
		last := results[limit-1]
		nextCursor = fmt.Sprintf("%d_%s", last.QueuedAt.Unix(), last.ID.String())
		results = results[:limit]
	}

	return results, nextCursor, nil
}
