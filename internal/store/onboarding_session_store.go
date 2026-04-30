package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OnboardingSession struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	StartedBy   uuid.UUID
	CurrentStep int
	StepData    [5]json.RawMessage
	State       string
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type OnboardingSessionStore struct {
	db *pgxpool.Pool
}

func NewOnboardingSessionStore(db *pgxpool.Pool) *OnboardingSessionStore {
	return &OnboardingSessionStore{db: db}
}

func (s *OnboardingSessionStore) Create(ctx context.Context, tenantID, startedBy uuid.UUID) (*OnboardingSession, error) {
	var sess OnboardingSession
	err := s.db.QueryRow(ctx, `
		INSERT INTO onboarding_sessions (tenant_id, started_by, current_step, state)
		VALUES ($1, $2, 1, 'in_progress')
		RETURNING id, tenant_id, started_by, current_step,
			step_1_data, step_2_data, step_3_data, step_4_data, step_5_data,
			state, completed_at, created_at, updated_at
	`, tenantID, startedBy).Scan(
		&sess.ID, &sess.TenantID, &sess.StartedBy, &sess.CurrentStep,
		&sess.StepData[0], &sess.StepData[1], &sess.StepData[2], &sess.StepData[3], &sess.StepData[4],
		&sess.State, &sess.CompletedAt, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create onboarding session: %w", err)
	}
	return &sess, nil
}

func (s *OnboardingSessionStore) GetByID(ctx context.Context, id uuid.UUID) (*OnboardingSession, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var sess OnboardingSession
	err = s.db.QueryRow(ctx, `
		SELECT id, tenant_id, started_by, current_step,
			step_1_data, step_2_data, step_3_data, step_4_data, step_5_data,
			state, completed_at, created_at, updated_at
		FROM onboarding_sessions
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&sess.ID, &sess.TenantID, &sess.StartedBy, &sess.CurrentStep,
		&sess.StepData[0], &sess.StepData[1], &sess.StepData[2], &sess.StepData[3], &sess.StepData[4],
		&sess.State, &sess.CompletedAt, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get onboarding session: %w", err)
	}
	return &sess, nil
}

func (s *OnboardingSessionStore) UpdateStep(ctx context.Context, id uuid.UUID, stepN int, stepData []byte, newCurrentStep int) error {
	if stepN < 1 || stepN > 5 {
		return fmt.Errorf("invalid step number: %d, must be 1..5", stepN)
	}

	col := fmt.Sprintf("step_%d_data", stepN)
	query := fmt.Sprintf(`
		UPDATE onboarding_sessions
		SET %s = $2, current_step = $3, updated_at = NOW()
		WHERE id = $1
	`, col)

	_, err := s.db.Exec(ctx, query, id, stepData, newCurrentStep)
	if err != nil {
		return fmt.Errorf("update onboarding session step: %w", err)
	}
	return nil
}

func (s *OnboardingSessionStore) GetLatestByTenant(ctx context.Context, tenantID uuid.UUID) (*OnboardingSession, error) {
	var sess OnboardingSession
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, started_by, current_step,
			step_1_data, step_2_data, step_3_data, step_4_data, step_5_data,
			state, completed_at, created_at, updated_at
		FROM onboarding_sessions
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, tenantID).Scan(
		&sess.ID, &sess.TenantID, &sess.StartedBy, &sess.CurrentStep,
		&sess.StepData[0], &sess.StepData[1], &sess.StepData[2], &sess.StepData[3], &sess.StepData[4],
		&sess.State, &sess.CompletedAt, &sess.CreatedAt, &sess.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest onboarding session: %w", err)
	}
	return &sess, nil
}

func (s *OnboardingSessionStore) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE onboarding_sessions
		SET state = 'completed', completed_at = NOW(), current_step = 6, updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("mark onboarding session completed: %w", err)
	}
	return nil
}

// IsCompleted satisfies auth.OnboardingSessionLookup (FIX-303): returns true
// if the latest onboarding session for the tenant is in `completed` state.
// Returns false (no error) when no session exists — fail-safe so the FE
// redirects new tenants to the wizard.
func (s *OnboardingSessionStore) IsCompleted(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var state string
	err := s.db.QueryRow(ctx, `
		SELECT state FROM onboarding_sessions
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, tenantID).Scan(&state)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("check onboarding completion: %w", err)
	}
	return state == "completed", nil
}
