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
	ErrPolicyNotFound      = errors.New("store: policy not found")
	ErrPolicyNameExists    = errors.New("store: policy name already exists for this tenant")
	ErrPolicyVersionNotFound = errors.New("store: policy version not found")
	ErrPolicyInUse         = errors.New("store: policy has assigned SIMs")
	ErrVersionNotDraft     = errors.New("store: version is not in draft state")
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
	PolicyID      uuid.UUID
	DSLContent    string
	CompiledRules json.RawMessage
	CreatedBy     *uuid.UUID
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

func (s *PolicyStore) CreateVersion(ctx context.Context, p CreateVersionParams) (*PolicyVersion, error) {
	row := s.db.QueryRow(ctx, `
		INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, created_by)
		VALUES ($1, (SELECT COALESCE(MAX(version), 0) + 1 FROM policy_versions WHERE policy_id = $1), $2, $3, $4)
		RETURNING `+policyVersionColumns,
		p.PolicyID, p.DSLContent, p.CompiledRules, p.CreatedBy,
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
		SELECT `+policyVersionColumns+`
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
