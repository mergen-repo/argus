package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SimSegment struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Name             string
	FilterDefinition json.RawMessage
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
}

type CreateSegmentParams struct {
	Name             string
	FilterDefinition json.RawMessage
}

type SegmentFilter struct {
	OperatorID *uuid.UUID `json:"operator_id,omitempty"`
	State      string     `json:"state,omitempty"`
	APNID      *uuid.UUID `json:"apn_id,omitempty"`
	RATType    string     `json:"rat_type,omitempty"`
}

var ErrSegmentNameExists = errors.New("segment name already exists")

type SegmentStore struct {
	db *pgxpool.Pool
}

func NewSegmentStore(db *pgxpool.Pool) *SegmentStore {
	return &SegmentStore{db: db}
}

func (s *SegmentStore) Create(ctx context.Context, p CreateSegmentParams) (*SimSegment, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	filterDef := p.FilterDefinition
	if filterDef == nil {
		filterDef = json.RawMessage(`{}`)
	}

	userIDStr, _ := ctx.Value(apierr.UserIDKey).(string)
	var createdBy *uuid.UUID
	if uid, err := uuid.Parse(userIDStr); err == nil {
		createdBy = &uid
	}

	var seg SimSegment
	err = s.db.QueryRow(ctx, `
		INSERT INTO sim_segments (tenant_id, name, filter_definition, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, name, filter_definition, created_by, created_at
	`, tenantID, p.Name, filterDef, createdBy).Scan(
		&seg.ID, &seg.TenantID, &seg.Name, &seg.FilterDefinition,
		&seg.CreatedBy, &seg.CreatedAt,
	)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrSegmentNameExists
		}
		return nil, fmt.Errorf("create segment: %w", err)
	}
	return &seg, nil
}

func (s *SegmentStore) GetByID(ctx context.Context, id uuid.UUID) (*SimSegment, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var seg SimSegment
	err = s.db.QueryRow(ctx, `
		SELECT id, tenant_id, name, filter_definition, created_by, created_at
		FROM sim_segments
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&seg.ID, &seg.TenantID, &seg.Name, &seg.FilterDefinition,
		&seg.CreatedBy, &seg.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get segment: %w", err)
	}
	return &seg, nil
}

func (s *SegmentStore) List(ctx context.Context, cursor string, limit int) ([]SimSegment, string, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, "", err
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id > $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, name, filter_definition, created_by, created_at
		FROM sim_segments
		%s
		ORDER BY id
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list segments: %w", err)
	}
	defer rows.Close()

	var results []SimSegment
	for rows.Next() {
		var seg SimSegment
		if err := rows.Scan(
			&seg.ID, &seg.TenantID, &seg.Name, &seg.FilterDefinition,
			&seg.CreatedBy, &seg.CreatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("scan segment: %w", err)
		}
		results = append(results, seg)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *SegmentStore) Delete(ctx context.Context, id uuid.UUID) error {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return err
	}

	tag, err := s.db.Exec(ctx, `
		DELETE FROM sim_segments WHERE id = $1 AND tenant_id = $2
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete segment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SegmentStore) buildSegmentFilterQuery(ctx context.Context, id uuid.UUID) (*SegmentFilter, uuid.UUID, error) {
	seg, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, uuid.Nil, err
	}

	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, uuid.Nil, err
	}

	var filter SegmentFilter
	if err := json.Unmarshal(seg.FilterDefinition, &filter); err != nil {
		return nil, uuid.Nil, fmt.Errorf("parse filter definition: %w", err)
	}

	return &filter, tenantID, nil
}

func buildFilterConditions(filter *SegmentFilter, tenantID uuid.UUID) ([]string, []interface{}, int) {
	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if filter.OperatorID != nil {
		conditions = append(conditions, fmt.Sprintf("operator_id = $%d", argIdx))
		args = append(args, *filter.OperatorID)
		argIdx++
	}
	if filter.State != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, filter.State)
		argIdx++
	}
	if filter.APNID != nil {
		conditions = append(conditions, fmt.Sprintf("apn_id = $%d", argIdx))
		args = append(args, *filter.APNID)
		argIdx++
	}
	if filter.RATType != "" {
		conditions = append(conditions, fmt.Sprintf("rat_type = $%d", argIdx))
		args = append(args, filter.RATType)
		argIdx++
	}

	return conditions, args, argIdx
}

func (s *SegmentStore) CountMatchingSIMs(ctx context.Context, id uuid.UUID) (int64, error) {
	filter, tenantID, err := s.buildSegmentFilterQuery(ctx, id)
	if err != nil {
		return 0, err
	}

	conditions, args, _ := buildFilterConditions(filter, tenantID)
	where := "WHERE " + strings.Join(conditions, " AND ")
	query := fmt.Sprintf("SELECT COUNT(*) FROM sims %s", where)

	var count int64
	err = s.db.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count matching sims: %w", err)
	}
	return count, nil
}

func (s *SegmentStore) StateSummary(ctx context.Context, id uuid.UUID) (map[string]int64, int64, error) {
	filter, tenantID, err := s.buildSegmentFilterQuery(ctx, id)
	if err != nil {
		return nil, 0, err
	}

	conditions, args, _ := buildFilterConditions(filter, tenantID)
	where := "WHERE " + strings.Join(conditions, " AND ")
	query := fmt.Sprintf("SELECT state, COUNT(*) FROM sims %s GROUP BY state", where)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("state summary: %w", err)
	}
	defer rows.Close()

	byState := make(map[string]int64)
	var total int64
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return nil, 0, fmt.Errorf("scan state summary: %w", err)
		}
		byState[state] = count
		total += count
	}

	return byState, total, nil
}

type SIMBulkInfo struct {
	ID              uuid.UUID  `json:"id"`
	ICCID           string     `json:"iccid"`
	State           string     `json:"state"`
	SimType         string     `json:"sim_type"`
	PolicyVersionID *uuid.UUID `json:"policy_version_id"`
	OperatorID      uuid.UUID  `json:"operator_id"`
	ESimProfileID   *uuid.UUID `json:"esim_profile_id"`
}

func (s *SegmentStore) ListMatchingSIMIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	filter, tenantID, err := s.buildSegmentFilterQuery(ctx, id)
	if err != nil {
		return nil, err
	}

	conditions, args, _ := buildFilterConditions(filter, tenantID)
	where := "WHERE " + strings.Join(conditions, " AND ")
	query := fmt.Sprintf("SELECT id FROM sims %s ORDER BY id", where)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list matching sim ids: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan sim id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *SegmentStore) ListMatchingSIMIDsWithDetails(ctx context.Context, id uuid.UUID) ([]SIMBulkInfo, error) {
	filter, tenantID, err := s.buildSegmentFilterQuery(ctx, id)
	if err != nil {
		return nil, err
	}

	conditions, args, _ := buildFilterConditions(filter, tenantID)
	where := "WHERE " + strings.Join(conditions, " AND ")
	query := fmt.Sprintf(
		"SELECT id, iccid, state, sim_type, policy_version_id, operator_id, esim_profile_id FROM sims %s ORDER BY id",
		where,
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list matching sim details: %w", err)
	}
	defer rows.Close()

	var results []SIMBulkInfo
	for rows.Next() {
		var s SIMBulkInfo
		if err := rows.Scan(&s.ID, &s.ICCID, &s.State, &s.SimType, &s.PolicyVersionID, &s.OperatorID, &s.ESimProfileID); err != nil {
			return nil, fmt.Errorf("scan sim bulk info: %w", err)
		}
		results = append(results, s)
	}
	return results, nil
}
