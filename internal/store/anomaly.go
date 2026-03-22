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
	ErrAnomalyNotFound          = errors.New("store: anomaly not found")
	ErrInvalidAnomalyTransition = errors.New("store: invalid anomaly state transition")
)

type Anomaly struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	SimID          *uuid.UUID      `json:"sim_id,omitempty"`
	Type           string          `json:"type"`
	Severity       string          `json:"severity"`
	State          string          `json:"state"`
	Details        json.RawMessage `json:"details"`
	Source         *string         `json:"source,omitempty"`
	DetectedAt     time.Time       `json:"detected_at"`
	AcknowledgedAt *time.Time      `json:"acknowledged_at,omitempty"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreateAnomalyParams struct {
	TenantID   uuid.UUID
	SimID      *uuid.UUID
	Type       string
	Severity   string
	Details    json.RawMessage
	Source     *string
	DetectedAt time.Time
}

type ListAnomalyParams struct {
	Cursor   string
	Limit    int
	Type     string
	Severity string
	State    string
	SimID    *uuid.UUID
	From     *time.Time
	To       *time.Time
}

type AnomalyStore struct {
	db *pgxpool.Pool
}

func NewAnomalyStore(db *pgxpool.Pool) *AnomalyStore {
	return &AnomalyStore{db: db}
}

var anomalyColumns = `id, tenant_id, sim_id, type, severity, state, details, source,
	detected_at, acknowledged_at, resolved_at, created_at, updated_at`

func scanAnomaly(row pgx.Row) (*Anomaly, error) {
	var a Anomaly
	err := row.Scan(
		&a.ID, &a.TenantID, &a.SimID, &a.Type, &a.Severity, &a.State,
		&a.Details, &a.Source, &a.DetectedAt, &a.AcknowledgedAt, &a.ResolvedAt,
		&a.CreatedAt, &a.UpdatedAt,
	)
	return &a, err
}

func (s *AnomalyStore) Create(ctx context.Context, p CreateAnomalyParams) (*Anomaly, error) {
	detectedAt := p.DetectedAt
	if detectedAt.IsZero() {
		detectedAt = time.Now().UTC()
	}
	details := p.Details
	if details == nil {
		details = json.RawMessage(`{}`)
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO anomalies (tenant_id, sim_id, type, severity, details, source, detected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+anomalyColumns,
		p.TenantID, p.SimID, p.Type, p.Severity, details, p.Source, detectedAt,
	)

	a, err := scanAnomaly(row)
	if err != nil {
		return nil, fmt.Errorf("store: create anomaly: %w", err)
	}
	return a, nil
}

func (s *AnomalyStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*Anomaly, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+anomalyColumns+` FROM anomalies WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	a, err := scanAnomaly(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAnomalyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get anomaly: %w", err)
	}
	return a, nil
}

func (s *AnomalyStore) ListByTenant(ctx context.Context, tenantID uuid.UUID, p ListAnomalyParams) ([]Anomaly, string, error) {
	limit := p.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
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
	if p.From != nil {
		conditions = append(conditions, fmt.Sprintf("detected_at >= $%d", argIdx))
		args = append(args, *p.From)
		argIdx++
	}
	if p.To != nil {
		conditions = append(conditions, fmt.Sprintf("detected_at <= $%d", argIdx))
		args = append(args, *p.To)
		argIdx++
	}
	if p.Cursor != "" {
		conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
		cursorID, err := uuid.Parse(p.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("store: invalid cursor: %w", err)
		}
		args = append(args, cursorID)
		argIdx++
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM anomalies %s ORDER BY detected_at DESC, id DESC LIMIT %s`,
		anomalyColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list anomalies: %w", err)
	}
	defer rows.Close()

	var results []Anomaly
	for rows.Next() {
		var a Anomaly
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.SimID, &a.Type, &a.Severity, &a.State,
			&a.Details, &a.Source, &a.DetectedAt, &a.AcknowledgedAt, &a.ResolvedAt,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan anomaly: %w", err)
		}
		results = append(results, a)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

var validAnomalyTransitions = map[string][]string{
	"open":         {"acknowledged", "resolved", "false_positive"},
	"acknowledged": {"resolved", "false_positive"},
}

func (s *AnomalyStore) UpdateState(ctx context.Context, tenantID, id uuid.UUID, newState string) (*Anomaly, error) {
	current, err := s.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	valid := false
	for _, allowed := range validAnomalyTransitions[current.State] {
		if allowed == newState {
			valid = true
			break
		}
	}
	if !valid {
		return nil, ErrInvalidAnomalyTransition
	}

	now := time.Now().UTC()
	var ackAt, resAt *time.Time

	switch newState {
	case "acknowledged":
		ackAt = &now
	case "resolved", "false_positive":
		resAt = &now
		if current.AcknowledgedAt != nil {
			ackAt = current.AcknowledgedAt
		}
	}

	row := s.db.QueryRow(ctx, `
		UPDATE anomalies SET
			state = $3,
			acknowledged_at = COALESCE($4, acknowledged_at),
			resolved_at = COALESCE($5, resolved_at),
			updated_at = now()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+anomalyColumns,
		id, tenantID, newState, ackAt, resAt,
	)

	a, err := scanAnomaly(row)
	if err != nil {
		return nil, fmt.Errorf("store: update anomaly state: %w", err)
	}
	return a, nil
}

func (s *AnomalyStore) CountByTenantAndState(ctx context.Context, tenantID uuid.UUID, state string) (int64, error) {
	var count int64
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM anomalies WHERE tenant_id = $1 AND state = $2`,
		tenantID, state,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count anomalies: %w", err)
	}
	return count, nil
}

func (s *AnomalyStore) GetDailyUsageForSIM(ctx context.Context, simID uuid.UUID, date time.Time) (int64, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	var totalBytes int64
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(bytes_in + bytes_out), 0)
		FROM cdrs
		WHERE sim_id = $1 AND timestamp >= $2 AND timestamp < $3
	`, simID, dayStart, dayEnd).Scan(&totalBytes)
	if err != nil {
		return 0, fmt.Errorf("store: get daily usage for sim: %w", err)
	}
	return totalBytes, nil
}

func (s *AnomalyStore) GetAvgDailyUsageForSIM(ctx context.Context, simID uuid.UUID, days int) (float64, error) {
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := endDate.Add(-time.Duration(days) * 24 * time.Hour)

	var avgBytes float64
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(daily_total), 0)
		FROM (
			SELECT date_trunc('day', timestamp) AS day, SUM(bytes_in + bytes_out) AS daily_total
			FROM cdrs
			WHERE sim_id = $1 AND timestamp >= $2 AND timestamp < $3
			GROUP BY day
		) sub
	`, simID, startDate, endDate).Scan(&avgBytes)
	if err != nil {
		return 0, fmt.Errorf("store: get avg daily usage for sim: %w", err)
	}
	return avgBytes, nil
}

type DataSpikeCandidate struct {
	SimID      uuid.UUID
	TenantID   uuid.UUID
	TodayBytes int64
	AvgBytes   float64
}

func (s *AnomalyStore) FindDataSpikeCandidates(ctx context.Context, multiplier float64) ([]DataSpikeCandidate, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.Add(-24 * time.Hour)
	avgStart := today.Add(-30 * 24 * time.Hour)

	rows, err := s.db.Query(ctx, `
		WITH today_usage AS (
			SELECT sim_id, tenant_id, SUM(bytes_in + bytes_out) AS today_bytes
			FROM cdrs
			WHERE timestamp >= $1 AND timestamp < $2
			GROUP BY sim_id, tenant_id
			HAVING SUM(bytes_in + bytes_out) > 0
		),
		avg_usage AS (
			SELECT sim_id, AVG(daily_total) AS avg_bytes
			FROM (
				SELECT sim_id, date_trunc('day', timestamp) AS day, SUM(bytes_in + bytes_out) AS daily_total
				FROM cdrs
				WHERE timestamp >= $3 AND timestamp < $4
				GROUP BY sim_id, day
			) sub
			GROUP BY sim_id
			HAVING AVG(daily_total) > 0
		)
		SELECT t.sim_id, t.tenant_id, t.today_bytes, a.avg_bytes
		FROM today_usage t
		JOIN avg_usage a ON t.sim_id = a.sim_id
		WHERE t.today_bytes > a.avg_bytes * $5
	`, yesterday, today, avgStart, yesterday, multiplier)
	if err != nil {
		return nil, fmt.Errorf("store: find data spike candidates: %w", err)
	}
	defer rows.Close()

	var candidates []DataSpikeCandidate
	for rows.Next() {
		var c DataSpikeCandidate
		if err := rows.Scan(&c.SimID, &c.TenantID, &c.TodayBytes, &c.AvgBytes); err != nil {
			return nil, fmt.Errorf("store: scan data spike candidate: %w", err)
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

func (s *AnomalyStore) HasRecentAnomaly(ctx context.Context, tenantID uuid.UUID, simID *uuid.UUID, anomalyType string, window time.Duration) (bool, error) {
	cutoff := time.Now().UTC().Add(-window)

	var count int64
	if simID != nil {
		err := s.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM anomalies
			WHERE tenant_id = $1 AND sim_id = $2 AND type = $3 AND detected_at >= $4
				AND state IN ('open', 'acknowledged')
		`, tenantID, *simID, anomalyType, cutoff).Scan(&count)
		if err != nil {
			return false, fmt.Errorf("store: has recent anomaly: %w", err)
		}
	} else {
		err := s.db.QueryRow(ctx, `
			SELECT COUNT(*) FROM anomalies
			WHERE tenant_id = $1 AND sim_id IS NULL AND type = $2 AND detected_at >= $3
				AND state IN ('open', 'acknowledged')
		`, tenantID, anomalyType, cutoff).Scan(&count)
		if err != nil {
			return false, fmt.Errorf("store: has recent anomaly: %w", err)
		}
	}
	return count > 0, nil
}

func (s *AnomalyStore) GetSimICCID(ctx context.Context, simID uuid.UUID) (string, error) {
	var iccid string
	err := s.db.QueryRow(ctx, `SELECT iccid FROM sims WHERE id = $1`, simID).Scan(&iccid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get sim iccid: %w", err)
	}
	return iccid, nil
}
