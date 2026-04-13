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

var ErrMaintenanceWindowNotFound = errors.New("store: maintenance window not found")

type MaintenanceWindow struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         *uuid.UUID `json:"tenant_id"`
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           time.Time  `json:"ends_at"`
	AffectedServices []string   `json:"affected_services"`
	CronExpression   *string    `json:"cron_expression"`
	NotifyPlan       []byte     `json:"notify_plan"`
	State            string     `json:"state"`
	CreatedBy        *uuid.UUID `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateMaintenanceWindowParams struct {
	TenantID         *uuid.UUID
	Title            string
	Description      string
	StartsAt         time.Time
	EndsAt           time.Time
	AffectedServices []string
	CronExpression   *string
	NotifyPlan       []byte
	CreatedBy        *uuid.UUID
}

type MaintenanceWindowStore struct {
	db *pgxpool.Pool
}

func NewMaintenanceWindowStore(db *pgxpool.Pool) *MaintenanceWindowStore {
	return &MaintenanceWindowStore{db: db}
}

func (s *MaintenanceWindowStore) Create(ctx context.Context, p CreateMaintenanceWindowParams) (*MaintenanceWindow, error) {
	if p.AffectedServices == nil {
		p.AffectedServices = []string{}
	}
	notifyPlan := p.NotifyPlan
	if len(notifyPlan) == 0 {
		notifyPlan = []byte("{}")
	}

	var mw MaintenanceWindow
	err := s.db.QueryRow(ctx, `
		INSERT INTO maintenance_windows (tenant_id, title, description, starts_at, ends_at,
			affected_services, cron_expression, notify_plan, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, title, description, starts_at, ends_at,
			affected_services, cron_expression, notify_plan, state, created_by, created_at, updated_at
	`, p.TenantID, p.Title, p.Description, p.StartsAt, p.EndsAt,
		p.AffectedServices, p.CronExpression, notifyPlan, p.CreatedBy).
		Scan(&mw.ID, &mw.TenantID, &mw.Title, &mw.Description, &mw.StartsAt, &mw.EndsAt,
			&mw.AffectedServices, &mw.CronExpression, &mw.NotifyPlan, &mw.State,
			&mw.CreatedBy, &mw.CreatedAt, &mw.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: create maintenance window: %w", err)
	}
	return &mw, nil
}

func (s *MaintenanceWindowStore) Get(ctx context.Context, id uuid.UUID) (*MaintenanceWindow, error) {
	var mw MaintenanceWindow
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, title, description, starts_at, ends_at,
			affected_services, cron_expression, notify_plan, state, created_by, created_at, updated_at
		FROM maintenance_windows
		WHERE id = $1
	`, id).Scan(&mw.ID, &mw.TenantID, &mw.Title, &mw.Description, &mw.StartsAt, &mw.EndsAt,
		&mw.AffectedServices, &mw.CronExpression, &mw.NotifyPlan, &mw.State,
		&mw.CreatedBy, &mw.CreatedAt, &mw.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMaintenanceWindowNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get maintenance window: %w", err)
	}
	return &mw, nil
}

func (s *MaintenanceWindowStore) List(ctx context.Context, tenantID *uuid.UUID, activeOnly bool) ([]MaintenanceWindow, error) {
	args := []interface{}{}
	conditions := []string{}
	argIdx := 1

	if activeOnly {
		conditions = append(conditions, fmt.Sprintf("state IN ('scheduled','active')"))
	}
	if tenantID != nil {
		conditions = append(conditions, fmt.Sprintf("(tenant_id = $%d OR tenant_id IS NULL)", argIdx))
		args = append(args, *tenantID)
		argIdx++
	}

	query := `
		SELECT id, tenant_id, title, description, starts_at, ends_at,
			affected_services, cron_expression, notify_plan, state, created_by, created_at, updated_at
		FROM maintenance_windows`
	if len(conditions) > 0 {
		query += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}
	query += " ORDER BY starts_at DESC LIMIT 100"

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list maintenance windows: %w", err)
	}
	defer rows.Close()

	var results []MaintenanceWindow
	for rows.Next() {
		var mw MaintenanceWindow
		if err := rows.Scan(&mw.ID, &mw.TenantID, &mw.Title, &mw.Description, &mw.StartsAt, &mw.EndsAt,
			&mw.AffectedServices, &mw.CronExpression, &mw.NotifyPlan, &mw.State,
			&mw.CreatedBy, &mw.CreatedAt, &mw.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan maintenance window: %w", err)
		}
		results = append(results, mw)
	}
	if results == nil {
		results = []MaintenanceWindow{}
	}
	return results, nil
}

func (s *MaintenanceWindowStore) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := s.db.Exec(ctx, `
		UPDATE maintenance_windows SET state = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND state NOT IN ('completed','cancelled')
	`, id)
	if err != nil {
		return fmt.Errorf("store: cancel maintenance window: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrMaintenanceWindowNotFound
	}
	return nil
}

// IsActiveFor returns the nearest active/scheduled maintenance window matching
// the given tenantID (or global, tenant_id IS NULL) and service name.
func (s *MaintenanceWindowStore) IsActiveFor(ctx context.Context, tenantID *uuid.UUID, service string) (*MaintenanceWindow, error) {
	args := []interface{}{time.Now().UTC()}
	tenantCond := "tenant_id IS NULL"
	if tenantID != nil {
		tenantCond = fmt.Sprintf("(tenant_id IS NULL OR tenant_id = $2)")
		args = append(args, *tenantID)
	}

	query := fmt.Sprintf(`
		SELECT id, tenant_id, title, description, starts_at, ends_at,
			affected_services, cron_expression, notify_plan, state, created_by, created_at, updated_at
		FROM maintenance_windows
		WHERE state IN ('scheduled','active')
		  AND starts_at <= $1 AND ends_at >= $1
		  AND %s
		  AND ($1 = $1 OR $%d = ANY(affected_services) OR cardinality(affected_services) = 0)
		ORDER BY starts_at ASC
		LIMIT 1
	`, tenantCond, len(args)+1)

	// Service filter
	args = append(args, service)
	servicePlaceholder := len(args)

	// Rebuild query with proper placeholder for service
	query = fmt.Sprintf(`
		SELECT id, tenant_id, title, description, starts_at, ends_at,
			affected_services, cron_expression, notify_plan, state, created_by, created_at, updated_at
		FROM maintenance_windows
		WHERE state IN ('scheduled','active')
		  AND starts_at <= $1 AND ends_at >= $1
		  AND %s
		  AND ($%d = ANY(affected_services) OR cardinality(affected_services) = 0)
		ORDER BY starts_at ASC
		LIMIT 1
	`, tenantCond, servicePlaceholder)

	var mw MaintenanceWindow
	err := s.db.QueryRow(ctx, query, args...).
		Scan(&mw.ID, &mw.TenantID, &mw.Title, &mw.Description, &mw.StartsAt, &mw.EndsAt,
			&mw.AffectedServices, &mw.CronExpression, &mw.NotifyPlan, &mw.State,
			&mw.CreatedBy, &mw.CreatedAt, &mw.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: is active for maintenance window: %w", err)
	}
	return &mw, nil
}
