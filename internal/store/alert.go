package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/btopcu/argus/internal/alertstate"
)

var (
	ErrAlertNotFound          = errors.New("alert not found")
	ErrInvalidAlertTransition = alertstate.ErrInvalidAlertTransition
)

type UpsertResult int

const (
	UpsertInserted UpsertResult = iota
	UpsertDeduplicated
	UpsertCoolingDown
)

func (r UpsertResult) String() string {
	switch r {
	case UpsertInserted:
		return "inserted"
	case UpsertDeduplicated:
		return "deduplicated"
	case UpsertCoolingDown:
		return "cooling_down"
	default:
		return "unknown"
	}
}

type Alert struct {
	ID              uuid.UUID       `json:"id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	Type            string          `json:"type"`
	Severity        string          `json:"severity"`
	Source          string          `json:"source"`
	State           string          `json:"state"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Meta            json.RawMessage `json:"meta"`
	SimID           *uuid.UUID      `json:"sim_id,omitempty"`
	OperatorID      *uuid.UUID      `json:"operator_id,omitempty"`
	APNID           *uuid.UUID      `json:"apn_id,omitempty"`
	DedupKey        *string         `json:"dedup_key,omitempty"`
	FiredAt         time.Time       `json:"fired_at"`
	AcknowledgedAt  *time.Time      `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  *uuid.UUID      `json:"acknowledged_by,omitempty"`
	ResolvedAt      *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	OccurrenceCount int             `json:"occurrence_count"`
	FirstSeenAt     time.Time       `json:"first_seen_at"`
	LastSeenAt      time.Time       `json:"last_seen_at"`
	CooldownUntil   *time.Time      `json:"cooldown_until,omitempty"`
}

type CreateAlertParams struct {
	TenantID    uuid.UUID
	Type        string
	Severity    string
	Source      string
	Title       string
	Description string
	Meta        json.RawMessage
	SimID       *uuid.UUID
	OperatorID  *uuid.UUID
	APNID       *uuid.UUID
	DedupKey    *string
	FiredAt     time.Time
}

type ListAlertsParams struct {
	Type       string
	Severity   string
	Source     string
	State      string
	SimID      *uuid.UUID
	OperatorID *uuid.UUID
	APNID      *uuid.UUID
	DedupKey   string // FIX-229 Gate F-A1: filter by dedup_key (similar-alerts deeplink)
	From       *time.Time
	To         *time.Time
	Q          string
	Cursor     *uuid.UUID
	Limit      int
}

type AlertStore struct {
	db               *pgxpool.Pool
	suppressionStore *AlertSuppressionStore
}

func NewAlertStore(db *pgxpool.Pool) *AlertStore {
	return &AlertStore{db: db}
}

func (s *AlertStore) WithSuppressionStore(ss *AlertSuppressionStore) *AlertStore {
	s.suppressionStore = ss
	return s
}

var alertColumns = `id, tenant_id, type, severity, source, state, title, description, meta,
	sim_id, operator_id, apn_id, dedup_key,
	fired_at, acknowledged_at, acknowledged_by, resolved_at, created_at, updated_at,
	occurrence_count, first_seen_at, last_seen_at, cooldown_until`

func scanAlert(row pgx.Row) (*Alert, error) {
	var a Alert
	err := row.Scan(
		&a.ID, &a.TenantID, &a.Type, &a.Severity, &a.Source, &a.State,
		&a.Title, &a.Description, &a.Meta,
		&a.SimID, &a.OperatorID, &a.APNID, &a.DedupKey,
		&a.FiredAt, &a.AcknowledgedAt, &a.AcknowledgedBy, &a.ResolvedAt,
		&a.CreatedAt, &a.UpdatedAt,
		&a.OccurrenceCount, &a.FirstSeenAt, &a.LastSeenAt, &a.CooldownUntil,
	)
	return &a, err
}

func (s *AlertStore) Create(ctx context.Context, p CreateAlertParams) (*Alert, error) {
	meta := p.Meta
	if meta == nil {
		meta = json.RawMessage(`{}`)
	}

	var row pgx.Row
	if p.FiredAt.IsZero() {
		row = s.db.QueryRow(ctx, `
			INSERT INTO alerts (tenant_id, type, severity, source, title, description, meta,
				sim_id, operator_id, apn_id, dedup_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING `+alertColumns,
			p.TenantID, p.Type, p.Severity, p.Source, p.Title, p.Description, meta,
			p.SimID, p.OperatorID, p.APNID, p.DedupKey,
		)
	} else {
		row = s.db.QueryRow(ctx, `
			INSERT INTO alerts (tenant_id, type, severity, source, title, description, meta,
				sim_id, operator_id, apn_id, dedup_key, fired_at, first_seen_at, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12, $12)
			RETURNING `+alertColumns,
			p.TenantID, p.Type, p.Severity, p.Source, p.Title, p.Description, meta,
			p.SimID, p.OperatorID, p.APNID, p.DedupKey, p.FiredAt,
		)
	}

	a, err := scanAlert(row)
	if err != nil {
		return nil, fmt.Errorf("store: create alert: %w", err)
	}
	return a, nil
}

func (s *AlertStore) UpsertWithDedup(ctx context.Context, p CreateAlertParams, severityOrdinal int) (*Alert, UpsertResult, error) {
	if p.DedupKey == nil || *p.DedupKey == "" {
		a, err := s.Create(ctx, p)
		if err != nil {
			return nil, UpsertInserted, err
		}
		return a, UpsertInserted, nil
	}

	var sentinel int
	err := s.db.QueryRow(ctx, `
		SELECT 1 FROM alerts
		 WHERE tenant_id = $1 AND dedup_key = $2 AND state = 'resolved' AND cooldown_until > NOW()
		 LIMIT 1`,
		p.TenantID, *p.DedupKey,
	).Scan(&sentinel)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, UpsertInserted, fmt.Errorf("store: upsert alert cooldown check: %w", err)
	}
	if err == nil {
		return nil, UpsertCoolingDown, nil
	}

	meta := p.Meta
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}

	insertState := "open"
	if s.suppressionStore != nil {
		probe := AlertMatchProbe{
			AlertID:    uuid.Nil,
			Type:       p.Type,
			OperatorID: p.OperatorID,
			DedupKey:   p.DedupKey,
		}
		match, matchErr := s.suppressionStore.MatchActive(ctx, p.TenantID, probe)
		if matchErr != nil {
			log.Printf("store: upsert alert suppression check failed (proceeding as open): %v", matchErr)
		} else if match != nil {
			insertState = "suppressed"
			merged := map[string]interface{}{"suppression_id": match.ID.String()}
			existing := map[string]interface{}{}
			if len(meta) > 0 {
				_ = json.Unmarshal(meta, &existing)
			}
			for k, v := range existing {
				merged[k] = v
			}
			if b, err := json.Marshal(merged); err == nil {
				meta = json.RawMessage(b)
			}
		}
	}

	var firedAt interface{}
	if p.FiredAt.IsZero() {
		firedAt = nil
	} else {
		firedAt = p.FiredAt
	}

	query := `
		INSERT INTO alerts (
			tenant_id, type, severity, source, state, title, description, meta,
			sim_id, operator_id, apn_id, dedup_key, fired_at,
			occurrence_count, first_seen_at, last_seen_at
		) VALUES (
			$1, $2, $3, $4, $14, $5, $6, COALESCE($7::jsonb, '{}'::jsonb),
			$8, $9, $10, $11, COALESCE($12, NOW()),
			1, COALESCE($12, NOW()), COALESCE($12, NOW())
		)
		ON CONFLICT (tenant_id, dedup_key) WHERE dedup_key IS NOT NULL AND state IN ('open','acknowledged','suppressed')
		DO UPDATE SET
			occurrence_count = alerts.occurrence_count + 1,
			last_seen_at     = NOW(),
			severity         = CASE
				WHEN $13::int > CASE alerts.severity
					WHEN 'critical' THEN 5 WHEN 'high' THEN 4
					WHEN 'medium'   THEN 3 WHEN 'low'  THEN 2
					WHEN 'info'     THEN 1 ELSE 0 END
				THEN EXCLUDED.severity
				ELSE alerts.severity
			END,
			meta       = alerts.meta || EXCLUDED.meta,
			updated_at = NOW()
		RETURNING ` + alertColumns + `, (xmax = 0) AS was_inserted`

	row := s.db.QueryRow(ctx, query,
		p.TenantID, p.Type, p.Severity, p.Source, p.Title, p.Description, meta,
		p.SimID, p.OperatorID, p.APNID, p.DedupKey, firedAt, severityOrdinal, insertState,
	)

	var a Alert
	var wasInserted bool
	if err := row.Scan(
		&a.ID, &a.TenantID, &a.Type, &a.Severity, &a.Source, &a.State,
		&a.Title, &a.Description, &a.Meta,
		&a.SimID, &a.OperatorID, &a.APNID, &a.DedupKey,
		&a.FiredAt, &a.AcknowledgedAt, &a.AcknowledgedBy, &a.ResolvedAt,
		&a.CreatedAt, &a.UpdatedAt,
		&a.OccurrenceCount, &a.FirstSeenAt, &a.LastSeenAt, &a.CooldownUntil,
		&wasInserted,
	); err != nil {
		return nil, UpsertInserted, fmt.Errorf("store: upsert alert: %w", err)
	}

	if wasInserted {
		return &a, UpsertInserted, nil
	}
	return &a, UpsertDeduplicated, nil
}

func (s *AlertStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*Alert, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+alertColumns+` FROM alerts WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	a, err := scanAlert(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get alert: %w", err)
	}
	return a, nil
}

func (s *AlertStore) FindActiveByDedupKey(ctx context.Context, tenantID uuid.UUID, dedupKey string) (*Alert, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+alertColumns+` FROM alerts
		  WHERE tenant_id = $1 AND dedup_key = $2
		    AND state IN ('open','acknowledged','suppressed')
		  LIMIT 1`,
		tenantID, dedupKey,
	)
	a, err := scanAlert(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: find active alert by dedup key: %w", err)
	}
	return a, nil
}

func (s *AlertStore) ListByTenant(ctx context.Context, tenantID uuid.UUID, p ListAlertsParams) ([]Alert, *uuid.UUID, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if p.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, p.Type)
		argIdx++
	}
	if p.Severity != "" {
		conditions = append(conditions, fmt.Sprintf("severity = $%d", argIdx))
		args = append(args, p.Severity)
		argIdx++
	}
	if p.Source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", argIdx))
		args = append(args, p.Source)
		argIdx++
	}
	if p.State != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, p.State)
		argIdx++
	}
	if p.SimID != nil {
		conditions = append(conditions, fmt.Sprintf("sim_id = $%d", argIdx))
		args = append(args, *p.SimID)
		argIdx++
	}
	if p.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *p.OperatorID)
		argIdx++
	}
	if p.APNID != nil {
		conditions = append(conditions, fmt.Sprintf("apn_id = $%d", argIdx))
		args = append(args, *p.APNID)
		argIdx++
	}
	if p.DedupKey != "" {
		conditions = append(conditions, fmt.Sprintf("dedup_key = $%d", argIdx))
		args = append(args, p.DedupKey)
		argIdx++
	}
	if p.From != nil {
		conditions = append(conditions, fmt.Sprintf("fired_at >= $%d", argIdx))
		args = append(args, *p.From)
		argIdx++
	}
	if p.To != nil {
		conditions = append(conditions, fmt.Sprintf("fired_at <= $%d", argIdx))
		args = append(args, *p.To)
		argIdx++
	}
	if p.Q != "" {
		conditions = append(conditions, fmt.Sprintf("(title ILIKE '%%'||$%d||'%%' OR description ILIKE '%%'||$%d||'%%')", argIdx, argIdx))
		args = append(args, p.Q)
		argIdx++
	}
	if p.Cursor != nil {
		conditions = append(conditions, fmt.Sprintf(
			"(fired_at, id) < ((SELECT fired_at FROM alerts WHERE id = $%d), $%d)",
			argIdx, argIdx,
		))
		args = append(args, *p.Cursor)
		argIdx++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM alerts %s ORDER BY fired_at DESC, id DESC LIMIT %s`,
		alertColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("store: list alerts: %w", err)
	}
	defer rows.Close()

	var results []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.Type, &a.Severity, &a.Source, &a.State,
			&a.Title, &a.Description, &a.Meta,
			&a.SimID, &a.OperatorID, &a.APNID, &a.DedupKey,
			&a.FiredAt, &a.AcknowledgedAt, &a.AcknowledgedBy, &a.ResolvedAt,
			&a.CreatedAt, &a.UpdatedAt,
			&a.OccurrenceCount, &a.FirstSeenAt, &a.LastSeenAt, &a.CooldownUntil,
		); err != nil {
			return nil, nil, fmt.Errorf("store: scan alert: %w", err)
		}
		results = append(results, a)
	}

	var nextCursor *uuid.UUID
	if len(results) > limit {
		id := results[limit-1].ID
		nextCursor = &id
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *AlertStore) UpdateState(ctx context.Context, tenantID, id uuid.UUID, newState string, userID *uuid.UUID, cooldownMinutes int) (*Alert, error) {
	if !alertstate.IsUpdateAllowed(newState) {
		return nil, ErrInvalidAlertTransition
	}

	current, err := s.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if !alertstate.CanTransition(current.State, newState) {
		return nil, ErrInvalidAlertTransition
	}

	now := time.Now().UTC()
	var ackAt *time.Time
	var ackBy *uuid.UUID
	var resAt *time.Time

	switch newState {
	case alertstate.StateAcknowledged:
		ackAt = &now
		ackBy = userID
	case alertstate.StateResolved:
		resAt = &now
		if current.AcknowledgedAt != nil {
			ackAt = current.AcknowledgedAt
			ackBy = current.AcknowledgedBy
		}
	}

	var cooldownArg interface{}
	if newState == alertstate.StateResolved && cooldownMinutes > 0 {
		cooldownArg = cooldownMinutes
	}

	row := s.db.QueryRow(ctx, `
		UPDATE alerts SET
			state = $3::text,
			acknowledged_at = COALESCE($4, acknowledged_at),
			acknowledged_by = COALESCE($5, acknowledged_by),
			resolved_at = COALESCE($6, resolved_at),
			cooldown_until = CASE
				WHEN $7::int IS NOT NULL AND $3::text = 'resolved'
					THEN NOW() + ($7::int * INTERVAL '1 minute')
				ELSE cooldown_until
			END,
			updated_at = now()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+alertColumns,
		id, tenantID, newState, ackAt, ackBy, resAt, cooldownArg,
	)

	a, err := scanAlert(row)
	if err != nil {
		return nil, fmt.Errorf("store: update alert state: %w", err)
	}
	return a, nil
}

func (s *AlertStore) SuppressAlert(ctx context.Context, tenantID, id uuid.UUID, reason string) (*Alert, error) {
	current, err := s.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if !alertstate.CanTransition(current.State, alertstate.StateSuppressed) {
		return nil, ErrInvalidAlertTransition
	}

	row := s.db.QueryRow(ctx, `
		UPDATE alerts SET
			state = 'suppressed',
			meta = meta || jsonb_build_object('suppress_reason', $3::text),
			updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+alertColumns,
		id, tenantID, reason,
	)

	a, err := scanAlert(row)
	if err != nil {
		return nil, fmt.Errorf("store: suppress alert: %w", err)
	}
	return a, nil
}

func (s *AlertStore) UnsuppressAlert(ctx context.Context, tenantID, id uuid.UUID) (*Alert, error) {
	current, err := s.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	if !alertstate.CanTransition(current.State, alertstate.StateOpen) {
		return nil, ErrInvalidAlertTransition
	}

	row := s.db.QueryRow(ctx, `
		UPDATE alerts SET
			state = 'open',
			updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+alertColumns,
		id, tenantID,
	)

	a, err := scanAlert(row)
	if err != nil {
		return nil, fmt.Errorf("store: unsuppress alert: %w", err)
	}
	return a, nil
}

// BackfillSuppression flips currently-open alerts matching the given scope to
// state='suppressed' and stamps meta with {"suppression_id":..,"suppress_reason":..}.
// Acknowledged/resolved alerts are NOT touched (FIX-229 R7). Returns the row count.
//
// scopeType ∈ {'this','type','operator','dedup_key'}. The dispatch is hard-coded
// per scope (one query each) so casts ($::uuid) stay type-safe and the WHERE
// shape is never assembled from caller input.
func (s *AlertStore) BackfillSuppression(
	ctx context.Context,
	tenantID uuid.UUID,
	scopeType, scopeValue string,
	suppressionID uuid.UUID,
	reason string,
) (int64, error) {
	metaPatch := func() string {
		return `meta = meta || jsonb_build_object('suppression_id', $3::text, 'suppress_reason', $4::text)`
	}
	var (
		tag pgconn.CommandTag
		err error
	)
	switch scopeType {
	case "this":
		tag, err = s.db.Exec(ctx, `
			UPDATE alerts SET
				state = 'suppressed',
				`+metaPatch()+`,
				updated_at = NOW()
			WHERE tenant_id = $1 AND state = 'open' AND id = $2::uuid`,
			tenantID, scopeValue, suppressionID.String(), reason,
		)
	case "type":
		tag, err = s.db.Exec(ctx, `
			UPDATE alerts SET
				state = 'suppressed',
				`+metaPatch()+`,
				updated_at = NOW()
			WHERE tenant_id = $1 AND state = 'open' AND type = $2`,
			tenantID, scopeValue, suppressionID.String(), reason,
		)
	case "operator":
		tag, err = s.db.Exec(ctx, `
			UPDATE alerts SET
				state = 'suppressed',
				`+metaPatch()+`,
				updated_at = NOW()
			WHERE tenant_id = $1 AND state = 'open' AND operator_id = $2::uuid`,
			tenantID, scopeValue, suppressionID.String(), reason,
		)
	case "dedup_key":
		tag, err = s.db.Exec(ctx, `
			UPDATE alerts SET
				state = 'suppressed',
				`+metaPatch()+`,
				updated_at = NOW()
			WHERE tenant_id = $1 AND state = 'open' AND dedup_key = $2`,
			tenantID, scopeValue, suppressionID.String(), reason,
		)
	default:
		return 0, fmt.Errorf("store: backfill suppression: unknown scope_type %q", scopeType)
	}
	if err != nil {
		return 0, fmt.Errorf("store: backfill suppression: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RestoreSuppressedByMetaID is the best-effort inverse of BackfillSuppression:
// any alert currently in state='suppressed' whose meta.suppression_id matches
// the deleted rule's UUID is flipped back to 'open' and the suppression_id key
// is stripped from meta. Returns the row count. Does NOT remove suppress_reason.
func (s *AlertStore) RestoreSuppressedByMetaID(
	ctx context.Context,
	tenantID uuid.UUID,
	suppressionID uuid.UUID,
) (int64, error) {
	tag, err := s.db.Exec(ctx, `
		UPDATE alerts SET
			state = 'open',
			meta = meta - 'suppression_id',
			updated_at = NOW()
		WHERE tenant_id = $1 AND state = 'suppressed' AND meta->>'suppression_id' = $2::text`,
		tenantID, suppressionID.String(),
	)
	if err != nil {
		return 0, fmt.Errorf("store: restore suppressed alerts: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *AlertStore) CountByTenantAndState(ctx context.Context, tenantID uuid.UUID, state string) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM alerts WHERE tenant_id = $1 AND state = $2`,
		tenantID, state,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count alerts: %w", err)
	}
	return count, nil
}

func (s *AlertStore) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	rows, err := s.db.Query(ctx,
		`DELETE FROM alerts WHERE fired_at < $1 RETURNING id`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("store: delete old alerts: %w", err)
	}
	defer rows.Close()

	var count int64
	for rows.Next() {
		count++
	}
	if rerr := rows.Err(); rerr != nil {
		return count, fmt.Errorf("store: delete old alerts: iterate: %w", rerr)
	}
	return count, nil
}

func (s *AlertStore) DeleteOlderThanForTenant(ctx context.Context, tenantID uuid.UUID, cutoff time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx,
		`DELETE FROM alerts WHERE tenant_id = $1 AND fired_at < $2`,
		tenantID, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("store: delete old alerts for tenant: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *AlertStore) ListSimilar(ctx context.Context, tenantID uuid.UUID, anchor *Alert, limit int) ([]Alert, string, error) {
	if limit < 1 {
		limit = 1
	} else if limit > 50 {
		limit = 50
	}

	var (
		query         string
		args          []interface{}
		matchStrategy string
	)

	if anchor.DedupKey != nil && *anchor.DedupKey != "" {
		matchStrategy = "dedup_key"
		query = `SELECT ` + alertColumns + ` FROM alerts
			WHERE tenant_id=$1 AND id <> $2 AND dedup_key = $3
			ORDER BY fired_at DESC LIMIT $4`
		args = []interface{}{tenantID, anchor.ID, *anchor.DedupKey, limit}
	} else {
		matchStrategy = "type_source"
		query = `SELECT ` + alertColumns + ` FROM alerts
			WHERE tenant_id=$1 AND id <> $2 AND type=$3 AND source=$4
			ORDER BY fired_at DESC LIMIT $5`
		args = []interface{}{tenantID, anchor.ID, anchor.Type, anchor.Source, limit}
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list similar alerts: %w", err)
	}
	defer rows.Close()

	var results []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.Type, &a.Severity, &a.Source, &a.State,
			&a.Title, &a.Description, &a.Meta,
			&a.SimID, &a.OperatorID, &a.APNID, &a.DedupKey,
			&a.FiredAt, &a.AcknowledgedAt, &a.AcknowledgedBy, &a.ResolvedAt,
			&a.CreatedAt, &a.UpdatedAt,
			&a.OccurrenceCount, &a.FirstSeenAt, &a.LastSeenAt, &a.CooldownUntil,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan similar alert: %w", err)
		}
		results = append(results, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: list similar alerts: iterate: %w", err)
	}

	return results, matchStrategy, nil
}
