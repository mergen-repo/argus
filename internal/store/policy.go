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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrPolicyNotFound        = errors.New("store: policy not found")
	ErrPolicyNameExists      = errors.New("store: policy name already exists for this tenant")
	ErrPolicyVersionNotFound = errors.New("store: policy version not found")
	ErrPolicyInUse           = errors.New("store: policy has assigned SIMs")
	ErrVersionNotDraft       = errors.New("store: version is not in draft state")
	ErrRolloutNotFound       = errors.New("store: rollout not found")
	ErrRolloutInProgress     = errors.New("store: a rollout is already in progress for this policy")
	ErrRolloutCompleted      = errors.New("store: rollout already completed")
	ErrRolloutRolledBack     = errors.New("store: rollout already rolled back")
	ErrRolloutAborted        = errors.New("store: rollout already aborted")
	ErrStageInProgress       = errors.New("store: current stage is still processing")
	ErrVersionNotActivatable = errors.New("store: version is not in an activatable state")
)

type Policy struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	Name             string     `json:"name"`
	Description      *string    `json:"description"`
	Scope            string     `json:"scope"`
	ScopeRefID       *uuid.UUID `json:"scope_ref_id"`
	CurrentVersionID *uuid.UUID `json:"current_version_id"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedBy        *uuid.UUID `json:"created_by"`
}

type PolicyVersion struct {
	ID               uuid.UUID       `json:"id"`
	PolicyID         uuid.UUID       `json:"policy_id"`
	Version          int             `json:"version"`
	DSLContent       string          `json:"dsl_content"`
	CompiledRules    json.RawMessage `json:"compiled_rules"`
	State            string          `json:"state"`
	AffectedSIMCount *int            `json:"affected_sim_count"`
	DryRunResult     json.RawMessage `json:"dry_run_result"`
	ActivatedAt      *time.Time      `json:"activated_at"`
	RolledBackAt     *time.Time      `json:"rolled_back_at"`
	CreatedAt        time.Time       `json:"created_at"`
	CreatedBy        *uuid.UUID      `json:"created_by"`
}

type CreatePolicyParams struct {
	Name        string
	Description *string
	Scope       string
	ScopeRefID  *uuid.UUID
	DSLContent  string
	CompiledRules json.RawMessage
	CreatedBy   *uuid.UUID
}

type UpdatePolicyParams struct {
	Name        *string
	Description *string
	State       *string
}

type CreateVersionParams struct {
	PolicyID         uuid.UUID
	DSLContent       string
	CompiledRules    json.RawMessage
	CreatedBy        *uuid.UUID
	AffectedSIMCount *int
}

type PolicyStore struct {
	db *pgxpool.Pool
}

func NewPolicyStore(db *pgxpool.Pool) *PolicyStore {
	return &PolicyStore{db: db}
}

var policyColumns = `id, tenant_id, name, description, scope, scope_ref_id,
	current_version_id, state, created_at, updated_at, created_by`

var policyVersionColumns = `id, policy_id, version, dsl_content, compiled_rules,
	state, affected_sim_count, dry_run_result, activated_at, rolled_back_at,
	created_at, created_by`

func scanPolicy(row pgx.Row) (*Policy, error) {
	var p Policy
	err := row.Scan(
		&p.ID, &p.TenantID, &p.Name, &p.Description,
		&p.Scope, &p.ScopeRefID, &p.CurrentVersionID,
		&p.State, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy,
	)
	return &p, err
}

func scanPolicyVersion(row pgx.Row) (*PolicyVersion, error) {
	var v PolicyVersion
	err := row.Scan(
		&v.ID, &v.PolicyID, &v.Version, &v.DSLContent, &v.CompiledRules,
		&v.State, &v.AffectedSIMCount, &v.DryRunResult,
		&v.ActivatedAt, &v.RolledBackAt, &v.CreatedAt, &v.CreatedBy,
	)
	return &v, err
}

func (s *PolicyStore) Create(ctx context.Context, tenantID uuid.UUID, p CreatePolicyParams) (*Policy, *PolicyVersion, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO policies (tenant_id, name, description, scope, scope_ref_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+policyColumns,
		tenantID, p.Name, p.Description, p.Scope, p.ScopeRefID, p.CreatedBy,
	)

	policy, err := scanPolicy(row)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, nil, ErrPolicyNameExists
		}
		return nil, nil, fmt.Errorf("store: create policy: %w", err)
	}

	vRow := tx.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, created_by)
		VALUES ($1, 1, $2, $3, $4)
		RETURNING `+policyVersionColumns,
		policy.ID, p.DSLContent, p.CompiledRules, p.CreatedBy,
	)

	version, err := scanPolicyVersion(vRow)
	if err != nil {
		return nil, nil, fmt.Errorf("store: create initial version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("store: commit tx: %w", err)
	}

	return policy, version, nil
}

func (s *PolicyStore) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*Policy, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+policyColumns+` FROM policies WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	p, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get policy: %w", err)
	}
	return p, nil
}

func (s *PolicyStore) List(ctx context.Context, tenantID uuid.UUID, cursor string, limit int, stateFilter, search string) ([]Policy, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if stateFilter != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, stateFilter)
		argIdx++
	}

	if search != "" {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", argIdx))
		args = append(args, "%"+search+"%")
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

	query := fmt.Sprintf(`SELECT %s FROM policies %s ORDER BY created_at DESC, id DESC LIMIT %s`,
		policyColumns, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list policies: %w", err)
	}
	defer rows.Close()

	var results []Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.Name, &p.Description,
			&p.Scope, &p.ScopeRefID, &p.CurrentVersionID,
			&p.State, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan policy: %w", err)
		}
		results = append(results, p)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *PolicyStore) Update(ctx context.Context, tenantID, id uuid.UUID, p UpdatePolicyParams) (*Policy, error) {
	sets := []string{}
	args := []interface{}{id, tenantID}
	argIdx := 3

	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *p.Description)
		argIdx++
	}
	if p.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *p.State)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, tenantID, id)
	}

	query := fmt.Sprintf(`UPDATE policies SET %s WHERE id = $1 AND tenant_id = $2 RETURNING %s`,
		strings.Join(sets, ", "), policyColumns)

	row := s.db.QueryRow(ctx, query, args...)
	policy, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrPolicyNameExists
		}
		return nil, fmt.Errorf("store: update policy: %w", err)
	}
	return policy, nil
}

func (s *PolicyStore) SoftDelete(ctx context.Context, tenantID, id uuid.UUID) error {
	inUse, err := s.HasAssignedSIMs(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return ErrPolicyInUse
	}

	tag, err := s.db.Exec(ctx,
		`UPDATE policies SET state = 'archived' WHERE id = $1 AND tenant_id = $2 AND state != 'archived'`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("store: soft delete policy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPolicyNotFound
	}
	return nil
}

func (s *PolicyStore) RestoreState(ctx context.Context, tenantID, id uuid.UUID, state string) (*Policy, error) {
	if state == "" {
		state = "draft"
	}

	row := s.db.QueryRow(ctx, `
		UPDATE policies
		SET state = $3, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+policyColumns,
		id, tenantID, state,
	)

	policy, err := scanPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: restore policy state: %w", err)
	}
	return policy, nil
}

func (s *PolicyStore) CreateVersion(ctx context.Context, p CreateVersionParams) (*PolicyVersion, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, created_by, affected_sim_count)
		VALUES ($1, (SELECT COALESCE(MAX(version), 0) + 1 FROM policy_versions WHERE policy_id = $1), $2, $3, $4, $5)
		RETURNING `+policyVersionColumns,
		p.PolicyID, p.DSLContent, p.CompiledRules, p.CreatedBy, p.AffectedSIMCount,
	)

	v, err := scanPolicyVersion(row)
	if err != nil {
		return nil, fmt.Errorf("store: create version: %w", err)
	}
	return v, nil
}

func (s *PolicyStore) GetVersionByID(ctx context.Context, id uuid.UUID) (*PolicyVersion, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+policyVersionColumns+` FROM policy_versions WHERE id = $1`, id,
	)
	v, err := scanPolicyVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get version: %w", err)
	}
	return v, nil
}

func (s *PolicyStore) GetVersionsByPolicyID(ctx context.Context, policyID uuid.UUID) ([]PolicyVersion, error) {
	rows, err := s.db.Query(ctx,
		`SELECT `+policyVersionColumns+` FROM policy_versions WHERE policy_id = $1 ORDER BY version DESC`,
		policyID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list versions: %w", err)
	}
	defer rows.Close()

	var results []PolicyVersion
	for rows.Next() {
		var v PolicyVersion
		if err := rows.Scan(
			&v.ID, &v.PolicyID, &v.Version, &v.DSLContent, &v.CompiledRules,
			&v.State, &v.AffectedSIMCount, &v.DryRunResult,
			&v.ActivatedAt, &v.RolledBackAt, &v.CreatedAt, &v.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("store: scan version: %w", err)
		}
		results = append(results, v)
	}
	return results, nil
}

func (s *PolicyStore) UpdateVersion(ctx context.Context, id uuid.UUID, dslContent string, compiledRules json.RawMessage) (*PolicyVersion, error) {
	row := s.db.QueryRow(ctx, `
		UPDATE policy_versions SET dsl_content = $2, compiled_rules = $3
		WHERE id = $1 AND state = 'draft'
		RETURNING `+policyVersionColumns,
		id, dslContent, compiledRules,
	)

	v, err := scanPolicyVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := s.GetVersionByID(ctx, id)
		if getErr != nil {
			return nil, ErrPolicyVersionNotFound
		}
		if existing.State != "draft" {
			return nil, ErrVersionNotDraft
		}
		return nil, ErrPolicyVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update version: %w", err)
	}
	return v, nil
}

func (s *PolicyStore) ActivateVersion(ctx context.Context, versionID uuid.UUID) (*PolicyVersion, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var v PolicyVersion
	err = tx.QueryRow(ctx,
		`SELECT `+policyVersionColumns+` FROM policy_versions WHERE id = $1 FOR UPDATE`,
		versionID,
	).Scan(
		&v.ID, &v.PolicyID, &v.Version, &v.DSLContent, &v.CompiledRules,
		&v.State, &v.AffectedSIMCount, &v.DryRunResult,
		&v.ActivatedAt, &v.RolledBackAt, &v.CreatedAt, &v.CreatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get version for activation: %w", err)
	}

	if v.State != "draft" {
		return nil, ErrVersionNotDraft
	}

	_, err = tx.Exec(ctx, `
		UPDATE policy_versions SET state = 'superseded'
		WHERE policy_id = $1 AND state = 'active'`,
		v.PolicyID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: supersede previous active: %w", err)
	}

	row := tx.QueryRow(ctx, `
		UPDATE policy_versions SET state = 'active', activated_at = NOW()
		WHERE id = $1
		RETURNING `+policyVersionColumns,
		versionID,
	)
	activated, err := scanPolicyVersion(row)
	if err != nil {
		return nil, fmt.Errorf("store: activate version: %w", err)
	}

	_, err = tx.Exec(ctx,
		`UPDATE policies SET current_version_id = $1 WHERE id = $2`,
		versionID, v.PolicyID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: update current_version_id: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit activation: %w", err)
	}

	return activated, nil
}

func (s *PolicyStore) CountAssignedSIMs(ctx context.Context, policyID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM policy_assignments
		WHERE policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $1)`,
		policyID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count assigned sims: %w", err)
	}
	return count, nil
}

func (s *PolicyStore) HasAssignedSIMs(ctx context.Context, policyID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM policy_assignments
			WHERE policy_version_id IN (SELECT id FROM policy_versions WHERE policy_id = $1)
		)`,
		policyID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: check assigned sims: %w", err)
	}
	return exists, nil
}

func (s *PolicyStore) UpdateDryRunResult(ctx context.Context, versionID uuid.UUID, result json.RawMessage, affectedCount int) error {
	_, err := s.db.Exec(ctx, `
		UPDATE policy_versions SET dry_run_result = $2, affected_sim_count = $3
		WHERE id = $1`,
		versionID, result, affectedCount,
	)
	if err != nil {
		return fmt.Errorf("store: update dry run result: %w", err)
	}
	return nil
}

func (s *PolicyStore) GetVersionWithTenant(ctx context.Context, versionID, tenantID uuid.UUID) (*PolicyVersion, error) {
	row := s.db.QueryRow(ctx, `
		SELECT pv.id, pv.policy_id, pv.version, pv.dsl_content, pv.compiled_rules,
			pv.state, pv.affected_sim_count, pv.dry_run_result, pv.activated_at, pv.rolled_back_at,
			pv.created_at, pv.created_by
		FROM policy_versions pv
		JOIN policies p ON pv.policy_id = p.id
		WHERE pv.id = $1 AND p.tenant_id = $2`,
		versionID, tenantID,
	)
	v, err := scanPolicyVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get version with tenant: %w", err)
	}
	return v, nil
}

func (s *PolicyStore) GetActiveVersionSummary(ctx context.Context, policyID uuid.UUID) (*PolicyVersion, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+policyVersionColumns+` FROM policy_versions WHERE policy_id = $1 AND state = 'active' LIMIT 1`,
		policyID,
	)
	v, err := scanPolicyVersion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get active version: %w", err)
	}
	return v, nil
}

type RolloutStage struct {
	Pct      int    `json:"pct"`
	Status   string `json:"status"`
	SimCount *int   `json:"sim_count,omitempty"`
	Migrated *int   `json:"migrated,omitempty"`
}

type PolicyRollout struct {
	ID                uuid.UUID       `json:"id"`
	PolicyVersionID   uuid.UUID       `json:"policy_version_id"`
	PreviousVersionID *uuid.UUID      `json:"previous_version_id"`
	Strategy          string          `json:"strategy"`
	Stages            json.RawMessage `json:"stages"`
	CurrentStage      int             `json:"current_stage"`
	TotalSIMs         int             `json:"total_sims"`
	MigratedSIMs      int             `json:"migrated_sims"`
	State             string          `json:"state"`
	StartedAt         *time.Time      `json:"started_at"`
	CompletedAt       *time.Time      `json:"completed_at"`
	RolledBackAt      *time.Time      `json:"rolled_back_at"`
	AbortedAt         *time.Time      `json:"aborted_at,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	CreatedBy         *uuid.UUID      `json:"created_by"`
	// PolicyID denormalises policy_versions.policy_id onto the rollout row
	// (FIX-231 DEV-345 migration 20260427000001 step 1). Surfaces the
	// denormalisation at the Go layer so reaper/bus paths can avoid an
	// extra JOIN through policy_versions (F-A4 Gate).
	PolicyID uuid.UUID `json:"policy_id"`
}

type PolicyAssignment struct {
	ID              uuid.UUID  `json:"id"`
	SimID           uuid.UUID  `json:"sim_id"`
	PolicyVersionID uuid.UUID  `json:"policy_version_id"`
	RolloutID       *uuid.UUID `json:"rollout_id"`
	AssignedAt      time.Time  `json:"assigned_at"`
	CoASentAt       *time.Time `json:"coa_sent_at"`
	CoAStatus       string     `json:"coa_status"`
}

type CreateRolloutParams struct {
	PolicyID          uuid.UUID
	PolicyVersionID   uuid.UUID
	PreviousVersionID *uuid.UUID
	Strategy          string
	Stages            json.RawMessage
	TotalSIMs         int
	CreatedBy         *uuid.UUID
}

var rolloutColumns = `id, policy_version_id, previous_version_id, strategy, stages,
	current_stage, total_sims, migrated_sims, state, started_at, completed_at,
	rolled_back_at, aborted_at, created_at, created_by, policy_id`

func scanRollout(row pgx.Row) (*PolicyRollout, error) {
	var r PolicyRollout
	err := row.Scan(
		&r.ID, &r.PolicyVersionID, &r.PreviousVersionID, &r.Strategy,
		&r.Stages, &r.CurrentStage, &r.TotalSIMs, &r.MigratedSIMs,
		&r.State, &r.StartedAt, &r.CompletedAt, &r.RolledBackAt,
		&r.AbortedAt, &r.CreatedAt, &r.CreatedBy, &r.PolicyID,
	)
	return &r, err
}

func (s *PolicyStore) CreateRollout(ctx context.Context, tenantID uuid.UUID, p CreateRolloutParams) (*PolicyRollout, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var v PolicyVersion
	err = tx.QueryRow(ctx,
		`SELECT `+policyVersionColumns+` FROM policy_versions WHERE id = $1 FOR UPDATE`,
		p.PolicyVersionID,
	).Scan(
		&v.ID, &v.PolicyID, &v.Version, &v.DSLContent, &v.CompiledRules,
		&v.State, &v.AffectedSIMCount, &v.DryRunResult,
		&v.ActivatedAt, &v.RolledBackAt, &v.CreatedAt, &v.CreatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPolicyVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get version for rollout: %w", err)
	}

	if v.State != "draft" {
		return nil, ErrVersionNotDraft
	}

	// FIX-231 F-A5 (Gate): query the denormalised policy_id directly instead
	// of the prior `policy_version_id IN (subquery)` form. Indexed by
	// idx_policy_rollouts_policy and aligns with GetActiveRolloutForPolicy.
	// True race-safety still lives in the 23505 mapping below (the partial
	// unique index `policy_active_rollout` is the source of truth) — this
	// precheck just delivers a clean error before INSERT in the common path.
	var existingCount int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM policy_rollouts
		 WHERE policy_id = $1
		   AND state IN ('pending', 'in_progress')`,
		v.PolicyID,
	).Scan(&existingCount)
	if err != nil {
		return nil, fmt.Errorf("store: check active rollout: %w", err)
	}
	if existingCount > 0 {
		return nil, ErrRolloutInProgress
	}

	_, err = tx.Exec(ctx, `
		UPDATE policy_versions SET state = 'rolling_out'
		WHERE id = $1`,
		p.PolicyVersionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: set version rolling_out: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO policy_rollouts (policy_id, policy_version_id, previous_version_id, strategy, stages,
			total_sims, state, started_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'in_progress', NOW(), $7)
		RETURNING `+rolloutColumns,
		p.PolicyID, p.PolicyVersionID, p.PreviousVersionID, p.Strategy, p.Stages,
		p.TotalSIMs, p.CreatedBy,
	)

	rollout, err := scanRollout(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "policy_active_rollout" {
			return nil, ErrRolloutInProgress
		}
		return nil, fmt.Errorf("store: create rollout: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit rollout: %w", err)
	}

	return rollout, nil
}

func (s *PolicyStore) GetRolloutByID(ctx context.Context, rolloutID uuid.UUID) (*PolicyRollout, error) {
	row := s.db.QueryRow(ctx,
		`SELECT `+rolloutColumns+` FROM policy_rollouts WHERE id = $1`,
		rolloutID,
	)
	r, err := scanRollout(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRolloutNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get rollout: %w", err)
	}
	return r, nil
}

func (s *PolicyStore) GetRolloutByIDWithTenant(ctx context.Context, rolloutID, tenantID uuid.UUID) (*PolicyRollout, error) {
	// FIX-232 DEV-357: column list now matches scanRollout's 16 destinations
	// after migration 20260428000001 added aborted_at. The prior SELECT was
	// missing pr.aborted_at, causing pr.created_at to flow into &r.AbortedAt
	// at scan time. Add aborted_at between rolled_back_at and created_at to
	// align with the rolloutColumns ordering used by scanRollout.
	row := s.db.QueryRow(ctx, `
		SELECT pr.id, pr.policy_version_id, pr.previous_version_id, pr.strategy, pr.stages,
			pr.current_stage, pr.total_sims, pr.migrated_sims, pr.state, pr.started_at, pr.completed_at,
			pr.rolled_back_at, pr.aborted_at, pr.created_at, pr.created_by, pr.policy_id
		FROM policy_rollouts pr
		JOIN policies p ON pr.policy_id = p.id
		WHERE pr.id = $1 AND p.tenant_id = $2`,
		rolloutID, tenantID,
	)
	r, err := scanRollout(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRolloutNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get rollout with tenant: %w", err)
	}
	return r, nil
}

func (s *PolicyStore) GetActiveRolloutForPolicy(ctx context.Context, policyID uuid.UUID) (*PolicyRollout, error) {
	// FIX-231 DEV-345: policy_rollouts.policy_id is now a denormalised column —
	// query it directly instead of joining policy_versions.
	// $1 = policyID (the policy whose in-flight rollout we want).
	row := s.db.QueryRow(ctx,
		`SELECT `+rolloutColumns+`
		   FROM policy_rollouts
		  WHERE policy_id = $1
		    AND state IN ('pending', 'in_progress')
		  LIMIT 1`,
		policyID,
	)
	r, err := scanRollout(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get active rollout for policy: %w", err)
	}
	return r, nil
}

// ListStuckRollouts returns rollout IDs that are still 'in_progress' even though
// migrated_sims >= total_sims, and have not advanced in the last graceMinutes
// minutes. The reaper job (FIX-231 Task 5) consumes this list and forces each
// rollout to its terminal state in a separate transaction with FOR UPDATE
// SKIP LOCKED. Locking is intentionally NOT done here so the reaper can choose
// its own lock semantics.
//
// Returns an empty slice (not an error) when nothing is stuck.
//
// $1 = graceMinutes — minutes without progress before a rollout is considered
//      stuck. COALESCE(completed_at, created_at) handles rollouts that finished
//      assigning but never had their state flipped.
func (s *PolicyStore) ListStuckRollouts(ctx context.Context, graceMinutes int) ([]uuid.UUID, error) {
	// FIX-231 F-A7 (Gate): ORDER BY created_at gives the reaper deterministic
	// page semantics — without it Postgres can return any 100 of N stuck
	// rollouts. NOTE: FOR UPDATE SKIP LOCKED is intentionally NOT added here
	// (deviation from plan): pgx auto-commits Query() before the caller sees
	// rows, so SKIP LOCKED would release the row locks before CompleteRollout
	// even runs. Real concurrency safety lives in CompleteRollout's own
	// `SELECT ... FOR UPDATE` plus the F-A2 idempotency guard — two reapers
	// converge cleanly: second one sees state='completed' and returns nil.
	// See gate.md "Deviations" section for the trade-off note.
	rows, err := s.db.Query(ctx, `
		SELECT id FROM policy_rollouts
		 WHERE state = 'in_progress'
		   AND total_sims > 0
		   AND migrated_sims >= total_sims
		   AND COALESCE(completed_at, created_at) < NOW() - make_interval(mins => $1)
		 ORDER BY created_at
		 LIMIT 100`,
		graceMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list stuck rollouts: %w", err)
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan stuck rollout id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate stuck rollouts: %w", err)
	}
	return ids, nil
}

func (s *PolicyStore) UpdateRolloutProgress(ctx context.Context, rolloutID uuid.UUID, migratedSIMs, currentStage int, stages json.RawMessage) error {
	_, err := s.db.Exec(ctx, `
		UPDATE policy_rollouts SET migrated_sims = $2, current_stage = $3, stages = $4
		WHERE id = $1`,
		rolloutID, migratedSIMs, currentStage, stages,
	)
	if err != nil {
		return fmt.Errorf("store: update rollout progress: %w", err)
	}
	return nil
}

func (s *PolicyStore) CompleteRollout(ctx context.Context, rolloutID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx,
		`SELECT `+rolloutColumns+` FROM policy_rollouts WHERE id = $1 FOR UPDATE`,
		rolloutID,
	)
	r, err := scanRollout(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRolloutNotFound
	}
	if err != nil {
		return fmt.Errorf("store: get rollout for completion: %w", err)
	}

	// FIX-231 F-A2 (Gate): idempotency guard. CompleteRollout is invoked from
	// (a) the rollout service when the final stage finishes, and (b) the
	// stuck-rollout reaper job that scans for in_progress rollouts whose
	// migration counter is full. Without this guard, a manual finish that
	// races a reaper cycle (or two reaper instances on the same id) would
	// re-flip a terminal rollout, re-stamp completed_at, and re-supersede —
	// overwriting the truthful activation timestamp on the target version.
	// We intentionally return nil on already-completed (success — desired
	// state achieved) and a typed sentinel on rolled_back (caller must
	// distinguish "no-op success" from "do not re-activate this version").
	if r.State == "completed" {
		return nil
	}
	if r.State == "rolled_back" {
		return ErrRolloutRolledBack
	}
	// FIX-232 DEV-357: aborted is terminal — block completion after abort.
	// An aborted rollout intentionally halted without finishing migration;
	// re-flipping it to 'completed' would activate a partially-rolled-out
	// version and contradict the abort decision.
	if r.State == "aborted" {
		return ErrRolloutAborted
	}

	// FIX-231 F-A1 (Gate): UPDATE order is supersede-first, then activate.
	// The `policy_active_version` partial unique index (state='active') is
	// NOT DEFERRABLE — flipping the target to 'active' BEFORE superseding
	// the prior active row would briefly satisfy the index for two rows
	// simultaneously and Postgres rejects with 23505. Reversing the order
	// keeps the index satisfied at every statement boundary: at the
	// supersede step the target is still 'rolling_out' so it is untouched
	// by the `state='active' AND id != $1` predicate, and at the activate
	// step the prior is already 'superseded' so the target is the sole row
	// in the index's predicate set.
	_, err = tx.Exec(ctx, `
		UPDATE policy_rollouts SET state = 'completed', completed_at = NOW()
		WHERE id = $1`,
		rolloutID,
	)
	if err != nil {
		return fmt.Errorf("store: complete rollout: %w", err)
	}

	// FIX-231 DEV-348: supersede every other active version belonging to the
	// same policy, not just r.PreviousVersionID. Defence-in-depth alongside the
	// `policy_active_version` partial unique index from migration
	// 20260427000001 (AC-3 / AC-6) — if any drift slipped in (e.g. from a prior
	// release without the index), we still converge to a single active version
	// per policy. The `id != $1` clause keeps the target row (still
	// 'rolling_out' at this statement) untouched; the sub-select scopes the
	// supersede to this policy only.
	// $1 = target policy_version_id (the version transitioning to active).
	_, err = tx.Exec(ctx, `
		UPDATE policy_versions
		   SET state = 'superseded'
		 WHERE policy_id = (SELECT policy_id FROM policy_versions WHERE id = $1)
		   AND state = 'active'
		   AND id != $1`,
		r.PolicyVersionID,
	)
	if err != nil {
		return fmt.Errorf("store: supersede prior active versions: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE policy_versions SET state = 'active', activated_at = NOW()
		WHERE id = $1`,
		r.PolicyVersionID,
	)
	if err != nil {
		return fmt.Errorf("store: activate rolled out version: %w", err)
	}

	// FIX-231 F-A4 (Gate): r.PolicyID is now scanned directly from
	// policy_rollouts.policy_id (denorm column from migration
	// 20260427000001), removing the prior extra SELECT through
	// policy_versions just to derive the policy id.
	_, err = tx.Exec(ctx, `UPDATE policies SET current_version_id = $1 WHERE id = $2`, r.PolicyVersionID, r.PolicyID)
	if err != nil {
		return fmt.Errorf("store: update current_version_id: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit completion: %w", err)
	}
	return nil
}

func (s *PolicyStore) RollbackRollout(ctx context.Context, rolloutID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx,
		`SELECT `+rolloutColumns+` FROM policy_rollouts WHERE id = $1 FOR UPDATE`,
		rolloutID,
	)
	r, err := scanRollout(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRolloutNotFound
	}
	if err != nil {
		return fmt.Errorf("store: get rollout for rollback: %w", err)
	}

	if r.State == "completed" {
		return ErrRolloutCompleted
	}
	if r.State == "rolled_back" {
		return ErrRolloutRolledBack
	}
	// FIX-232 DEV-357: aborted is a terminal state — block rollback after abort.
	// The rollout has been intentionally stopped without reverting assignments;
	// rolling back now would surprise the operator and re-walk migrated SIMs.
	if r.State == "aborted" {
		return ErrRolloutAborted
	}

	_, err = tx.Exec(ctx, `
		UPDATE policy_rollouts SET state = 'rolled_back', rolled_back_at = NOW()
		WHERE id = $1`,
		rolloutID,
	)
	if err != nil {
		return fmt.Errorf("store: rollback rollout: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE policy_versions SET state = 'rolled_back', rolled_back_at = NOW()
		WHERE id = $1`,
		r.PolicyVersionID,
	)
	if err != nil {
		return fmt.Errorf("store: set version rolled_back: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit rollback: %w", err)
	}
	return nil
}

// AbortRollout atomically transitions a rollout to state='aborted' (FIX-232).
// Unlike RollbackRollout it does NOT touch policy_assignments — already-migrated
// SIMs stay on the new policy version. The associated policy_versions row stays
// at its current 'rolling_out' state (intentionally stuck — operator must create
// a new draft version to retry). Aborting is terminal; subsequent
// Advance/Rollback/Complete calls return ErrRolloutAborted.
//
// Returns ErrRolloutNotFound if the row is missing, ErrRolloutCompleted /
// ErrRolloutRolledBack / ErrRolloutAborted on terminal-state collisions.
func (s *PolicyStore) AbortRollout(ctx context.Context, rolloutID uuid.UUID) (*PolicyRollout, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx,
		`SELECT `+rolloutColumns+` FROM policy_rollouts WHERE id = $1 FOR UPDATE`,
		rolloutID,
	)
	current, err := scanRollout(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRolloutNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get rollout for abort: %w", err)
	}

	switch current.State {
	case "completed":
		return nil, ErrRolloutCompleted
	case "rolled_back":
		return nil, ErrRolloutRolledBack
	case "aborted":
		return nil, ErrRolloutAborted
	}

	updated, err := scanRollout(tx.QueryRow(ctx, `
		UPDATE policy_rollouts SET state = 'aborted', aborted_at = NOW()
		WHERE id = $1
		RETURNING `+rolloutColumns,
		rolloutID,
	))
	if err != nil {
		return nil, fmt.Errorf("store: abort rollout update: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit abort: %w", err)
	}
	return updated, nil
}

func (s *PolicyStore) SelectSIMsForStage(
	ctx context.Context,
	tenantID, rolloutID uuid.UUID,
	previousVersionID *uuid.UUID,
	dslPredicate string,
	dslArgs []interface{},
	targetCount int,
) ([]uuid.UUID, error) {
	args := []interface{}{tenantID, rolloutID}
	conditions := []string{"s.tenant_id = $1", "s.state = 'active'"}
	argIdx := 3

	conditions = append(conditions, `s.id NOT IN (
		SELECT sim_id FROM policy_assignments WHERE rollout_id = $2)`)

	if previousVersionID != nil {
		conditions = append(conditions, fmt.Sprintf("s.policy_version_id = $%d", argIdx))
		args = append(args, *previousVersionID)
		argIdx++
	}

	predicate := dslPredicate
	if predicate == "" {
		predicate = "TRUE"
	}
	conditions = append(conditions, "("+predicate+")")
	for _, a := range dslArgs {
		args = append(args, a)
		argIdx++
	}

	args = append(args, targetCount)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT s.id FROM sims s
		WHERE %s
		ORDER BY random()
		LIMIT %s
		FOR UPDATE SKIP LOCKED`,
		strings.Join(conditions, " AND "), limitPlaceholder,
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: select sims for stage: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan sim id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *PolicyStore) AssignSIMsToVersion(ctx context.Context, simIDs []uuid.UUID, versionID, rolloutID uuid.UUID) (int, error) {
	if len(simIDs) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	batchSize := 500
	assigned := 0
	for i := 0; i < len(simIDs); i += batchSize {
		end := i + batchSize
		if end > len(simIDs) {
			end = len(simIDs)
		}
		batch := simIDs[i:end]

		valueStrings := make([]string, len(batch))
		args := []interface{}{versionID, rolloutID}
		for j, simID := range batch {
			argIdx := j + 3
			valueStrings[j] = fmt.Sprintf("($%d, $1, $2, NOW(), 'pending')", argIdx)
			args = append(args, simID)
		}

		// FIX-231 DEV-346: trg_sims_policy_version_sync (migration 20260427000001)
		// is now the single writer of sims.policy_version_id. The previous
		// explicit `UPDATE sims SET policy_version_id` block was removed — it
		// duplicated work the trigger already performs and risked diverging from
		// policy_assignments on partial failures. The upsert's RowsAffected()
		// counts every inserted-or-updated assignment row, which is what callers
		// expect from "assigned".
		tag, err := tx.Exec(ctx, fmt.Sprintf(`
			INSERT INTO policy_assignments (sim_id, policy_version_id, rollout_id, assigned_at, coa_status)
			VALUES %s
			ON CONFLICT (sim_id) DO UPDATE SET
				policy_version_id = EXCLUDED.policy_version_id,
				rollout_id = EXCLUDED.rollout_id,
				assigned_at = NOW(),
				coa_status = 'pending'`,
			strings.Join(valueStrings, ", ")),
			args...,
		)
		if err != nil {
			return assigned, fmt.Errorf("store: batch assign sims: %w", err)
		}
		assigned += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit assign: %w", err)
	}
	return assigned, nil
}

func (s *PolicyStore) RevertRolloutAssignments(ctx context.Context, rolloutID uuid.UUID, previousVersionID *uuid.UUID) (int, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if previousVersionID != nil {
		_, err = tx.Exec(ctx, `
			UPDATE policy_assignments SET
				policy_version_id = $1,
				coa_status = 'pending'
			WHERE rollout_id = $2`,
			*previousVersionID, rolloutID,
		)
	} else {
		_, err = tx.Exec(ctx, `
			DELETE FROM policy_assignments
			WHERE rollout_id = $1`,
			rolloutID,
		)
	}
	if err != nil {
		return 0, fmt.Errorf("store: revert assignments for rollout: %w", err)
	}

	var reverted int64
	if previousVersionID != nil {
		revertTag, execErr := tx.Exec(ctx, `
			UPDATE sims SET policy_version_id = $1
			WHERE id IN (SELECT sim_id FROM policy_assignments WHERE rollout_id = $2)`,
			*previousVersionID, rolloutID,
		)
		if execErr != nil {
			return 0, fmt.Errorf("store: revert sim versions for rollout: %w", execErr)
		}
		reverted = revertTag.RowsAffected()
	} else {
		revertTag, execErr := tx.Exec(ctx, `
			UPDATE sims SET policy_version_id = NULL
			WHERE id IN (SELECT sim_id FROM policy_assignments WHERE rollout_id = $1)`,
			rolloutID,
		)
		if execErr != nil {
			return 0, fmt.Errorf("store: revert sim versions for rollout: %w", execErr)
		}
		reverted = revertTag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit revert: %w", err)
	}
	return int(reverted), nil
}

func (s *PolicyStore) UpdateAssignmentCoAStatus(ctx context.Context, simID uuid.UUID, status string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE policy_assignments SET coa_status = $2, coa_sent_at = NOW()
		WHERE sim_id = $1`,
		simID, status,
	)
	if err != nil {
		return fmt.Errorf("store: update coa status: %w", err)
	}
	return nil
}

func (s *PolicyStore) GetAssignmentsByRollout(ctx context.Context, rolloutID uuid.UUID) ([]PolicyAssignment, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, sim_id, policy_version_id, rollout_id, assigned_at, coa_sent_at, coa_status
		FROM policy_assignments
		WHERE rollout_id = $1`,
		rolloutID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get assignments by rollout: %w", err)
	}
	defer rows.Close()

	var results []PolicyAssignment
	for rows.Next() {
		var a PolicyAssignment
		if err := rows.Scan(&a.ID, &a.SimID, &a.PolicyVersionID, &a.RolloutID,
			&a.AssignedAt, &a.CoASentAt, &a.CoAStatus); err != nil {
			return nil, fmt.Errorf("store: scan assignment: %w", err)
		}
		results = append(results, a)
	}
	return results, nil
}

func (s *PolicyStore) GetRolloutSimIDs(ctx context.Context, rolloutID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `
		SELECT sim_id FROM policy_assignments WHERE rollout_id = $1`,
		rolloutID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get rollout sim ids: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan rollout sim id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// FIX-231 F-A6 (Gate): both helpers below now use the denormalised
// policy_rollouts.policy_id column directly, dropping the prior JOIN
// through policy_versions. Same correctness, one fewer hop.
func (s *PolicyStore) GetTenantIDForRollout(ctx context.Context, rolloutID uuid.UUID) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT p.tenant_id
		  FROM policy_rollouts pr
		  JOIN policies p ON p.id = pr.policy_id
		 WHERE pr.id = $1`,
		rolloutID,
	).Scan(&tenantID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: get tenant_id for rollout: %w", err)
	}
	return tenantID, nil
}

func (s *PolicyStore) GetPolicyIDForRollout(ctx context.Context, rolloutID uuid.UUID) (uuid.UUID, error) {
	var policyID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT policy_id FROM policy_rollouts WHERE id = $1`,
		rolloutID,
	).Scan(&policyID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: get policy_id for rollout: %w", err)
	}
	return policyID, nil
}

func (s *PolicyStore) SetVersionState(ctx context.Context, versionID uuid.UUID, state string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE policy_versions SET state = $2
		WHERE id = $1`,
		versionID, state,
	)
	if err != nil {
		return fmt.Errorf("store: set version state: %w", err)
	}
	return nil
}

// ListReferencingAPN returns policies whose active compiled_rules JSON text
// contains apnName as a substring (D-007). Uses the GIN trigram index for
// fast ILIKE search. apnName must be ≥ 3 characters to avoid degenerate scans.
// Limitation: false positives may occur when apnName is a strict substring of
// another identifier (e.g. "iot" matches "iot-extra"). Callers should perform
// a secondary filter if exact matching is required.
func (s *PolicyStore) ListReferencingAPN(ctx context.Context, tenantID uuid.UUID, apnName string, limit int, cursor string) ([]Policy, string, error) {
	if len(apnName) < 3 {
		return nil, "", fmt.Errorf("store: apnName must be at least 3 characters for trigram search")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	pattern := "%" + apnName + "%"

	var cursorCond string
	var args []interface{}
	args = append(args, tenantID, pattern)

	if cursor != "" {
		cursorCond = fmt.Sprintf(" AND p.id > $%d", len(args)+1)
		args = append(args, cursor)
	}

	args = append(args, limit+1)
	limitArg := len(args)

	qualifiedCols := `p.id, p.tenant_id, p.name, p.description, p.scope, p.scope_ref_id,
		p.current_version_id, p.state, p.created_at, p.updated_at, p.created_by`

	query := fmt.Sprintf(`
		SELECT DISTINCT %s
		FROM policies p
		JOIN policy_versions pv ON pv.id = p.current_version_id
		WHERE p.tenant_id = $1
		  AND pv.compiled_rules::text ILIKE $2
		%s
		ORDER BY p.id
		LIMIT $%d`,
		qualifiedCols, cursorCond, limitArg,
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list referencing apn: %w", err)
	}
	defer rows.Close()

	var policies []Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.Name, &p.Description,
			&p.Scope, &p.ScopeRefID, &p.CurrentVersionID,
			&p.State, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan referencing policy: %w", err)
		}
		policies = append(policies, p)
	}

	var nextCursor string
	if len(policies) > limit {
		nextCursor = policies[limit].ID.String()
		policies = policies[:limit]
	}

	if policies == nil {
		policies = []Policy{}
	}

	return policies, nextCursor, nil
}
