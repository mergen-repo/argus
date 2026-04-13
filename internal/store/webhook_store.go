package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrWebhookConfigNotFound   = errors.New("store: webhook config not found")
	ErrWebhookDeliveryNotFound = errors.New("store: webhook delivery not found")
)

type WebhookConfig struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	URL           string
	Secret        string
	EventTypes    []string
	Enabled       bool
	LastSuccessAt *time.Time
	LastFailureAt *time.Time
	FailureCount  int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type WebhookDelivery struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ConfigID       uuid.UUID
	EventType      string
	PayloadHash    string
	PayloadPreview string
	Signature      string
	ResponseStatus *int
	ResponseBody   *string
	AttemptCount   int
	NextRetryAt    *time.Time
	FinalState     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type WebhookConfigPatch struct {
	URL        *string
	Secret     *string
	EventTypes *[]string
	Enabled    *bool
}

type WebhookConfigStore struct {
	db     *pgxpool.Pool
	encKey string
}

func NewWebhookConfigStore(db *pgxpool.Pool, encKey string) *WebhookConfigStore {
	return &WebhookConfigStore{db: db, encKey: encKey}
}

func (s *WebhookConfigStore) encryptSecret(plain string) ([]byte, error) {
	key, err := hex.DecodeString(s.encKey)
	if err != nil {
		return nil, fmt.Errorf("store: invalid encryption key: %w", err)
	}
	encrypted, err := crypto.Encrypt([]byte(plain), key)
	if err != nil {
		return nil, fmt.Errorf("store: encrypt secret: %w", err)
	}
	return encrypted, nil
}

func (s *WebhookConfigStore) decryptSecret(enc []byte) (string, error) {
	key, err := hex.DecodeString(s.encKey)
	if err != nil {
		return "", fmt.Errorf("store: invalid encryption key: %w", err)
	}
	plain, err := crypto.Decrypt(enc, key)
	if err != nil {
		return "", fmt.Errorf("store: decrypt secret: %w", err)
	}
	return string(plain), nil
}

func (s *WebhookConfigStore) Create(ctx context.Context, cfg *WebhookConfig) (*WebhookConfig, error) {
	encrypted, err := s.encryptSecret(cfg.Secret)
	if err != nil {
		return nil, err
	}

	eventTypes := cfg.EventTypes
	if eventTypes == nil {
		eventTypes = []string{}
	}

	var out WebhookConfig
	var secretEnc []byte
	err = s.db.QueryRow(ctx, `
		INSERT INTO webhook_configs (tenant_id, url, secret_encrypted, event_types, enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, tenant_id, url, secret_encrypted, event_types, enabled,
			last_success_at, last_failure_at, failure_count, created_at, updated_at
	`, cfg.TenantID, cfg.URL, encrypted, eventTypes, cfg.Enabled).Scan(
		&out.ID, &out.TenantID, &out.URL, &secretEnc, &out.EventTypes, &out.Enabled,
		&out.LastSuccessAt, &out.LastFailureAt, &out.FailureCount, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create webhook config: %w", err)
	}

	out.Secret, err = s.decryptSecret(secretEnc)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *WebhookConfigStore) Get(ctx context.Context, id uuid.UUID) (*WebhookConfig, error) {
	var out WebhookConfig
	var secretEnc []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, url, secret_encrypted, event_types, enabled,
			last_success_at, last_failure_at, failure_count, created_at, updated_at
		FROM webhook_configs
		WHERE id = $1
	`, id).Scan(
		&out.ID, &out.TenantID, &out.URL, &secretEnc, &out.EventTypes, &out.Enabled,
		&out.LastSuccessAt, &out.LastFailureAt, &out.FailureCount, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookConfigNotFound
		}
		return nil, fmt.Errorf("store: get webhook config: %w", err)
	}

	out.Secret, err = s.decryptSecret(secretEnc)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *WebhookConfigStore) GetForAPI(ctx context.Context, id uuid.UUID) (*WebhookConfig, error) {
	var out WebhookConfig
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, url, event_types, enabled,
			last_success_at, last_failure_at, failure_count, created_at, updated_at
		FROM webhook_configs
		WHERE id = $1
	`, id).Scan(
		&out.ID, &out.TenantID, &out.URL, &out.EventTypes, &out.Enabled,
		&out.LastSuccessAt, &out.LastFailureAt, &out.FailureCount, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookConfigNotFound
		}
		return nil, fmt.Errorf("store: get webhook config for api: %w", err)
	}
	out.Secret = ""
	return &out, nil
}

func (s *WebhookConfigStore) List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int) ([]*WebhookConfig, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID, limit + 1}
	conditions := []string{"tenant_id = $1"}
	argIdx := 3

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := strings.Join(conditions, " AND ")
	query := fmt.Sprintf(`
		SELECT id, tenant_id, url, event_types, enabled,
			last_success_at, last_failure_at, failure_count, created_at, updated_at
		FROM webhook_configs
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, where)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list webhook configs: %w", err)
	}
	defer rows.Close()

	var results []*WebhookConfig
	for rows.Next() {
		var c WebhookConfig
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.URL, &c.EventTypes, &c.Enabled,
			&c.LastSuccessAt, &c.LastFailureAt, &c.FailureCount, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan webhook config: %w", err)
		}
		c.Secret = ""
		results = append(results, &c)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *WebhookConfigStore) ListEnabledByEventType(ctx context.Context, tenantID uuid.UUID, eventType string) ([]*WebhookConfig, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, url, secret_encrypted, event_types, enabled,
			last_success_at, last_failure_at, failure_count, created_at, updated_at
		FROM webhook_configs
		WHERE tenant_id = $1
			AND enabled = true
			AND ($2 = ANY(event_types) OR event_types = '{}')
		ORDER BY created_at ASC, id ASC
	`, tenantID, eventType)
	if err != nil {
		return nil, fmt.Errorf("store: list enabled webhook configs by event type: %w", err)
	}
	defer rows.Close()

	var results []*WebhookConfig
	for rows.Next() {
		var c WebhookConfig
		var secretEnc []byte
		if err := rows.Scan(
			&c.ID, &c.TenantID, &c.URL, &secretEnc, &c.EventTypes, &c.Enabled,
			&c.LastSuccessAt, &c.LastFailureAt, &c.FailureCount, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan webhook config: %w", err)
		}
		c.Secret, err = s.decryptSecret(secretEnc)
		if err != nil {
			return nil, err
		}
		results = append(results, &c)
	}

	return results, nil
}

func (s *WebhookConfigStore) Update(ctx context.Context, id uuid.UUID, patch WebhookConfigPatch) error {
	sets := []string{}
	args := []interface{}{}
	argIdx := 1

	if patch.URL != nil {
		sets = append(sets, fmt.Sprintf("url = $%d", argIdx))
		args = append(args, *patch.URL)
		argIdx++
	}
	if patch.Secret != nil {
		encrypted, err := s.encryptSecret(*patch.Secret)
		if err != nil {
			return err
		}
		sets = append(sets, fmt.Sprintf("secret_encrypted = $%d", argIdx))
		args = append(args, encrypted)
		argIdx++
	}
	if patch.EventTypes != nil {
		sets = append(sets, fmt.Sprintf("event_types = $%d", argIdx))
		args = append(args, *patch.EventTypes)
		argIdx++
	}
	if patch.Enabled != nil {
		sets = append(sets, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *patch.Enabled)
		argIdx++
	}

	if len(sets) == 0 {
		return nil
	}

	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE webhook_configs SET %s WHERE id = $%d
	`, strings.Join(sets, ", "), argIdx)

	tag, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("store: update webhook config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookConfigNotFound
	}
	return nil
}

func (s *WebhookConfigStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM webhook_configs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete webhook config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookConfigNotFound
	}
	return nil
}

func (s *WebhookConfigStore) BumpSuccess(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE webhook_configs
		SET last_success_at = $2, failure_count = 0, updated_at = NOW()
		WHERE id = $1
	`, id, at)
	if err != nil {
		return fmt.Errorf("store: bump webhook success: %w", err)
	}
	return nil
}

func (s *WebhookConfigStore) BumpFailure(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := s.db.Exec(ctx, `
		UPDATE webhook_configs
		SET last_failure_at = $2, failure_count = failure_count + 1, updated_at = NOW()
		WHERE id = $1
	`, id, at)
	if err != nil {
		return fmt.Errorf("store: bump webhook failure: %w", err)
	}
	return nil
}

type WebhookDeliveryStore struct {
	db *pgxpool.Pool
}

func NewWebhookDeliveryStore(db *pgxpool.Pool) *WebhookDeliveryStore {
	return &WebhookDeliveryStore{db: db}
}

func (s *WebhookDeliveryStore) Insert(ctx context.Context, d *WebhookDelivery) (*WebhookDelivery, error) {
	var out WebhookDelivery
	err := s.db.QueryRow(ctx, `
		INSERT INTO webhook_deliveries
			(tenant_id, config_id, event_type, payload_hash, payload_preview, signature,
			 response_status, response_body, attempt_count, next_retry_at, final_state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, tenant_id, config_id, event_type, payload_hash, payload_preview, signature,
			response_status, response_body, attempt_count, next_retry_at, final_state,
			created_at, updated_at
	`, d.TenantID, d.ConfigID, d.EventType, d.PayloadHash, d.PayloadPreview, d.Signature,
		d.ResponseStatus, d.ResponseBody, d.AttemptCount, d.NextRetryAt, d.FinalState,
	).Scan(
		&out.ID, &out.TenantID, &out.ConfigID, &out.EventType, &out.PayloadHash,
		&out.PayloadPreview, &out.Signature, &out.ResponseStatus, &out.ResponseBody,
		&out.AttemptCount, &out.NextRetryAt, &out.FinalState, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: insert webhook delivery: %w", err)
	}
	return &out, nil
}

func (s *WebhookDeliveryStore) UpdateAttempt(ctx context.Context, id uuid.UUID, attemptCount int, nextRetryAt *time.Time, responseStatus *int, responseBody *string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET attempt_count = $2, next_retry_at = $3, response_status = $4, response_body = $5,
			updated_at = NOW()
		WHERE id = $1
	`, id, attemptCount, nextRetryAt, responseStatus, responseBody)
	if err != nil {
		return fmt.Errorf("store: update webhook delivery attempt: %w", err)
	}
	return nil
}

func (s *WebhookDeliveryStore) ListByConfig(ctx context.Context, configID uuid.UUID, cursor string, limit int) ([]*WebhookDelivery, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{configID, limit + 1}
	conditions := []string{"config_id = $1"}
	argIdx := 3

	if cursor != "" {
		cursorID, err := uuid.Parse(cursor)
		if err == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := strings.Join(conditions, " AND ")
	query := fmt.Sprintf(`
		SELECT id, tenant_id, config_id, event_type, payload_hash, payload_preview, signature,
			response_status, response_body, attempt_count, next_retry_at, final_state,
			created_at, updated_at
		FROM webhook_deliveries
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, where)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list webhook deliveries by config: %w", err)
	}
	defer rows.Close()

	var results []*WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.ConfigID, &d.EventType, &d.PayloadHash,
			&d.PayloadPreview, &d.Signature, &d.ResponseStatus, &d.ResponseBody,
			&d.AttemptCount, &d.NextRetryAt, &d.FinalState, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan webhook delivery: %w", err)
		}
		results = append(results, &d)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *WebhookDeliveryStore) ListDueForRetry(ctx context.Context, now time.Time, limit int) ([]*WebhookDelivery, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, config_id, event_type, payload_hash, payload_preview, signature,
			response_status, response_body, attempt_count, next_retry_at, final_state,
			created_at, updated_at
		FROM webhook_deliveries
		WHERE final_state = 'retrying' AND next_retry_at <= $1
		ORDER BY next_retry_at ASC
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list due for retry: %w", err)
	}
	defer rows.Close()

	var results []*WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.ConfigID, &d.EventType, &d.PayloadHash,
			&d.PayloadPreview, &d.Signature, &d.ResponseStatus, &d.ResponseBody,
			&d.AttemptCount, &d.NextRetryAt, &d.FinalState, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan due retry delivery: %w", err)
		}
		results = append(results, &d)
	}

	return results, nil
}

func (s *WebhookDeliveryStore) MarkFinal(ctx context.Context, id uuid.UUID, state string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE webhook_deliveries
		SET final_state = $2, next_retry_at = NULL, updated_at = NOW()
		WHERE id = $1
	`, id, state)
	if err != nil {
		return fmt.Errorf("store: mark webhook delivery final: %w", err)
	}
	return nil
}

func (s *WebhookDeliveryStore) GetByID(ctx context.Context, id uuid.UUID) (*WebhookDelivery, error) {
	var d WebhookDelivery
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, config_id, event_type, payload_hash, payload_preview, signature,
			response_status, response_body, attempt_count, next_retry_at, final_state,
			created_at, updated_at
		FROM webhook_deliveries
		WHERE id = $1
	`, id).Scan(
		&d.ID, &d.TenantID, &d.ConfigID, &d.EventType, &d.PayloadHash,
		&d.PayloadPreview, &d.Signature, &d.ResponseStatus, &d.ResponseBody,
		&d.AttemptCount, &d.NextRetryAt, &d.FinalState, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWebhookDeliveryNotFound
		}
		return nil, fmt.Errorf("store: get webhook delivery: %w", err)
	}
	return &d, nil
}
