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
	ErrRoamingAgreementNotFound = errors.New("store: roaming agreement not found")
	ErrRoamingAgreementOverlap  = errors.New("store: active roaming agreement already exists for this tenant+operator")
)

type SLATerms struct {
	UptimePct      float64 `json:"uptime_pct"`
	LatencyP95Ms   int     `json:"latency_p95_ms"`
	MaxIncidents   int     `json:"max_incidents"`
}

type VolumeTier struct {
	ThresholdMB int     `json:"threshold_mb"`
	CostPerMB   float64 `json:"cost_per_mb"`
}

type CostTerms struct {
	CostPerMB        float64      `json:"cost_per_mb"`
	Currency         string       `json:"currency"`
	VolumeTiers      []VolumeTier `json:"volume_tiers,omitempty"`
	SettlementPeriod string       `json:"settlement_period"`
}

type RoamingAgreement struct {
	ID                   uuid.UUID       `json:"id"`
	TenantID             uuid.UUID       `json:"tenant_id"`
	OperatorID           uuid.UUID       `json:"operator_id"`
	PartnerOperatorName  string          `json:"partner_operator_name"`
	AgreementType        string          `json:"agreement_type"`
	SLATerms             json.RawMessage `json:"sla_terms"`
	CostTerms            json.RawMessage `json:"cost_terms"`
	StartDate            time.Time       `json:"start_date"`
	EndDate              time.Time       `json:"end_date"`
	AutoRenew            bool            `json:"auto_renew"`
	State                string          `json:"state"`
	Notes                *string         `json:"notes"`
	TerminatedAt         *time.Time      `json:"terminated_at"`
	CreatedBy            *uuid.UUID      `json:"created_by"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

func (r *RoamingAgreement) ParsedCostTerms() (*CostTerms, error) {
	var ct CostTerms
	if err := json.Unmarshal(r.CostTerms, &ct); err != nil {
		return nil, fmt.Errorf("store: parse cost_terms: %w", err)
	}
	return &ct, nil
}

type CreateRoamingAgreementParams struct {
	OperatorID          uuid.UUID
	PartnerOperatorName string
	AgreementType       string
	SLATerms            json.RawMessage
	CostTerms           json.RawMessage
	StartDate           time.Time
	EndDate             time.Time
	AutoRenew           bool
	State               string
	Notes               *string
	CreatedBy           *uuid.UUID
}

type UpdateRoamingAgreementParams struct {
	PartnerOperatorName *string
	AgreementType       *string
	SLATerms            json.RawMessage
	CostTerms           json.RawMessage
	StartDate           *time.Time
	EndDate             *time.Time
	AutoRenew           *bool
	State               *string
	Notes               *string
}

type ListRoamingAgreementsFilter struct {
	OperatorID          *uuid.UUID
	State               string
	ExpiringWithinDays  *int
	Cursor              string
	Limit               int
}

type RoamingAgreementStore struct {
	db *pgxpool.Pool
}

func NewRoamingAgreementStore(db *pgxpool.Pool) *RoamingAgreementStore {
	return &RoamingAgreementStore{db: db}
}

var roamingAgreementColumns = `id, tenant_id, operator_id, partner_operator_name, agreement_type,
	sla_terms, cost_terms, start_date, end_date, auto_renew, state, notes,
	terminated_at, created_by, created_at, updated_at`

func scanRoamingAgreement(row pgx.Row) (*RoamingAgreement, error) {
	var a RoamingAgreement
	err := row.Scan(
		&a.ID, &a.TenantID, &a.OperatorID, &a.PartnerOperatorName, &a.AgreementType,
		&a.SLATerms, &a.CostTerms, &a.StartDate, &a.EndDate, &a.AutoRenew,
		&a.State, &a.Notes, &a.TerminatedAt, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *RoamingAgreementStore) checkOverlap(ctx context.Context, tenantID, operatorID uuid.UUID, excludeID *uuid.UUID) error {
	query := `SELECT COUNT(*) FROM roaming_agreements
		WHERE tenant_id = $1 AND operator_id = $2 AND state = 'active'`
	args := []interface{}{tenantID, operatorID}

	if excludeID != nil {
		query += ` AND id != $3`
		args = append(args, *excludeID)
	}

	var count int
	if err := s.db.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return fmt.Errorf("store: check overlap: %w", err)
	}
	if count > 0 {
		return ErrRoamingAgreementOverlap
	}
	return nil
}

func (s *RoamingAgreementStore) Create(ctx context.Context, tenantID uuid.UUID, p CreateRoamingAgreementParams) (*RoamingAgreement, error) {
	slaTerms := json.RawMessage(`{}`)
	if len(p.SLATerms) > 0 {
		slaTerms = p.SLATerms
	}
	costTerms := json.RawMessage(`{}`)
	if len(p.CostTerms) > 0 {
		costTerms = p.CostTerms
	}
	state := p.State
	if state == "" {
		state = "draft"
	}

	if state == "active" {
		if err := s.checkOverlap(ctx, tenantID, p.OperatorID, nil); err != nil {
			return nil, err
		}
	}

	row := s.db.QueryRow(ctx, `
		INSERT INTO roaming_agreements
			(tenant_id, operator_id, partner_operator_name, agreement_type,
			 sla_terms, cost_terms, start_date, end_date, auto_renew, state, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+roamingAgreementColumns,
		tenantID, p.OperatorID, p.PartnerOperatorName, p.AgreementType,
		slaTerms, costTerms, p.StartDate, p.EndDate, p.AutoRenew,
		state, p.Notes, p.CreatedBy,
	)

	a, err := scanRoamingAgreement(row)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrRoamingAgreementOverlap
		}
		return nil, fmt.Errorf("store: create roaming agreement: %w", err)
	}
	return a, nil
}

func (s *RoamingAgreementStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*RoamingAgreement, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+roamingAgreementColumns+` FROM roaming_agreements WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	a, err := scanRoamingAgreement(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoamingAgreementNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get roaming agreement: %w", err)
	}
	return a, nil
}

func (s *RoamingAgreementStore) List(ctx context.Context, tenantID uuid.UUID, f ListRoamingAgreementsFilter) ([]RoamingAgreement, string, error) {
	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if f.State != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, f.State)
		argIdx++
	}

	if f.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *f.OperatorID)
		argIdx++
	}

	if f.ExpiringWithinDays != nil {
		conditions = append(conditions, fmt.Sprintf("state = 'active' AND end_date <= (CURRENT_DATE + ($%d || ' days')::interval)", argIdx))
		args = append(args, *f.ExpiringWithinDays)
		argIdx++
	}

	if f.Cursor != "" {
		cursorID, parseErr := uuid.Parse(f.Cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`SELECT %s FROM roaming_agreements %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		roamingAgreementColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list roaming agreements: %w", err)
	}
	defer rows.Close()

	var results []RoamingAgreement
	for rows.Next() {
		a, err := scanRoamingAgreement(rows)
		if err != nil {
			return nil, "", fmt.Errorf("store: scan roaming agreement: %w", err)
		}
		results = append(results, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: iterate roaming agreements: %w", err)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *RoamingAgreementStore) ListByOperator(ctx context.Context, tenantID, operatorID uuid.UUID, cursor string, limit int) ([]RoamingAgreement, string, error) {
	opID := operatorID
	return s.List(ctx, tenantID, ListRoamingAgreementsFilter{
		OperatorID: &opID,
		Cursor:     cursor,
		Limit:      limit,
	})
}

func (s *RoamingAgreementStore) Update(ctx context.Context, tenantID, id uuid.UUID, p UpdateRoamingAgreementParams) (*RoamingAgreement, error) {
	sets := []string{}
	args := []interface{}{id, tenantID}
	argIdx := 3

	if p.PartnerOperatorName != nil {
		sets = append(sets, fmt.Sprintf("partner_operator_name = $%d", argIdx))
		args = append(args, *p.PartnerOperatorName)
		argIdx++
	}
	if p.AgreementType != nil {
		sets = append(sets, fmt.Sprintf("agreement_type = $%d", argIdx))
		args = append(args, *p.AgreementType)
		argIdx++
	}
	if len(p.SLATerms) > 0 {
		sets = append(sets, fmt.Sprintf("sla_terms = $%d", argIdx))
		args = append(args, p.SLATerms)
		argIdx++
	}
	if len(p.CostTerms) > 0 {
		sets = append(sets, fmt.Sprintf("cost_terms = $%d", argIdx))
		args = append(args, p.CostTerms)
		argIdx++
	}
	if p.StartDate != nil {
		sets = append(sets, fmt.Sprintf("start_date = $%d", argIdx))
		args = append(args, *p.StartDate)
		argIdx++
	}
	if p.EndDate != nil {
		sets = append(sets, fmt.Sprintf("end_date = $%d", argIdx))
		args = append(args, *p.EndDate)
		argIdx++
	}
	if p.AutoRenew != nil {
		sets = append(sets, fmt.Sprintf("auto_renew = $%d", argIdx))
		args = append(args, *p.AutoRenew)
		argIdx++
	}
	if p.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *p.State)
		argIdx++
	}
	if p.Notes != nil {
		sets = append(sets, fmt.Sprintf("notes = $%d", argIdx))
		args = append(args, *p.Notes)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, tenantID, id)
	}

	sets = append(sets, "updated_at = NOW()")

	if p.State != nil && *p.State == "active" {
		existing, getErr := s.GetByID(ctx, tenantID, id)
		if getErr == nil {
			if err := s.checkOverlap(ctx, tenantID, existing.OperatorID, &id); err != nil {
				return nil, err
			}
		}
	}

	query := fmt.Sprintf(`UPDATE roaming_agreements SET %s WHERE id = $1 AND tenant_id = $2 RETURNING %s`,
		strings.Join(sets, ", "), roamingAgreementColumns)

	row := s.db.QueryRow(ctx, query, args...)
	a, err := scanRoamingAgreement(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoamingAgreementNotFound
	}
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrRoamingAgreementOverlap
		}
		return nil, fmt.Errorf("store: update roaming agreement: %w", err)
	}
	return a, nil
}

func (s *RoamingAgreementStore) Terminate(ctx context.Context, tenantID, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE roaming_agreements
		SET state = 'terminated', terminated_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND state NOT IN ('terminated')`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("store: terminate roaming agreement: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrRoamingAgreementNotFound
	}
	return nil
}

func (s *RoamingAgreementStore) ListActiveByTenant(ctx context.Context, tenantID uuid.UUID, now time.Time) ([]RoamingAgreement, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+roamingAgreementColumns+`
		FROM roaming_agreements
		WHERE tenant_id = $1 AND state = 'active'
		  AND start_date <= $2 AND end_date >= $2
		ORDER BY created_at DESC, id DESC`,
		tenantID, now,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list active roaming agreements by tenant: %w", err)
	}
	defer rows.Close()

	var results []RoamingAgreement
	for rows.Next() {
		a, err := scanRoamingAgreement(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan active roaming agreement: %w", err)
		}
		results = append(results, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate active roaming agreements: %w", err)
	}
	return results, nil
}

func (s *RoamingAgreementStore) ListRecentlyExpiredByTenant(ctx context.Context, tenantID uuid.UUID, now time.Time, lookback time.Duration) ([]RoamingAgreement, error) {
	cutoff := now.Add(-lookback)
	rows, err := s.db.Query(ctx, `
		SELECT `+roamingAgreementColumns+`
		FROM roaming_agreements
		WHERE tenant_id = $1 AND state = 'active'
		  AND end_date >= $2 AND end_date < $3
		ORDER BY end_date ASC`,
		tenantID, cutoff, now,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list recently expired roaming agreements: %w", err)
	}
	defer rows.Close()

	var results []RoamingAgreement
	for rows.Next() {
		a, err := scanRoamingAgreement(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan recently expired roaming agreement: %w", err)
		}
		results = append(results, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate recently expired roaming agreements: %w", err)
	}
	return results, nil
}

func (s *RoamingAgreementStore) ListExpiringWithin(ctx context.Context, days int) ([]RoamingAgreement, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+roamingAgreementColumns+`
		FROM roaming_agreements
		WHERE state = 'active'
		  AND end_date BETWEEN CURRENT_DATE AND CURRENT_DATE + ($1 || ' days')::interval
		ORDER BY end_date ASC`,
		days,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list expiring roaming agreements: %w", err)
	}
	defer rows.Close()

	var results []RoamingAgreement
	for rows.Next() {
		a, err := scanRoamingAgreement(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan expiring roaming agreement: %w", err)
		}
		results = append(results, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate expiring roaming agreements: %w", err)
	}
	return results, nil
}
