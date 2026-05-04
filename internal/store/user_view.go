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

var (
	ErrUserViewNotFound     = errors.New("store: user view not found")
	ErrUserViewLimitReached = errors.New("store: user view limit reached (max 20 per page)")
)

var validViewPages = map[string]bool{
	"sims": true, "apns": true, "operators": true, "policies": true,
	"sessions": true, "jobs": true, "audit": true, "cdrs": true,
	"notifications": true, "violations": true, "alerts": true,
	"anomalies": true, "users": true, "api_keys": true,
	"esim": true, "segments": true,
}

type UserView struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	UserID      uuid.UUID       `json:"user_id"`
	Page        string          `json:"page"`
	Name        string          `json:"name"`
	FiltersJSON json.RawMessage `json:"filters_json"`
	ColumnsJSON json.RawMessage `json:"columns_json,omitempty"`
	SortJSON    json.RawMessage `json:"sort_json,omitempty"`
	IsDefault   bool            `json:"is_default"`
	Shared      bool            `json:"shared"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type CreateUserViewParams struct {
	TenantID    uuid.UUID
	UserID      uuid.UUID
	Page        string
	Name        string
	FiltersJSON json.RawMessage
	ColumnsJSON json.RawMessage
	SortJSON    json.RawMessage
	IsDefault   bool
	Shared      bool
}

type UpdateUserViewParams struct {
	Name        *string
	FiltersJSON json.RawMessage
	ColumnsJSON json.RawMessage
	SortJSON    json.RawMessage
	IsDefault   *bool
	Shared      *bool
}

type UserViewStore struct {
	db *pgxpool.Pool
}

func NewUserViewStore(db *pgxpool.Pool) *UserViewStore {
	return &UserViewStore{db: db}
}

func IsValidViewPage(page string) bool {
	return validViewPages[page]
}

func (s *UserViewStore) Create(ctx context.Context, p CreateUserViewParams) (*UserView, error) {
	// Count existing views for page
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_views WHERE user_id = $1 AND page = $2`,
		p.UserID, p.Page,
	).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("store: count user views: %w", err)
	}
	if count >= 20 {
		return nil, ErrUserViewLimitReached
	}

	if p.IsDefault {
		if _, err := s.db.Exec(ctx,
			`UPDATE user_views SET is_default = FALSE WHERE user_id = $1 AND page = $2 AND is_default = TRUE`,
			p.UserID, p.Page,
		); err != nil {
			return nil, fmt.Errorf("store: clear default views: %w", err)
		}
	}

	filters := p.FiltersJSON
	if filters == nil {
		filters = json.RawMessage(`{}`)
	}

	var v UserView
	err = s.db.QueryRow(ctx, `
		INSERT INTO user_views (tenant_id, user_id, page, name, filters_json, columns_json, sort_json, is_default, shared)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, user_id, page, name, filters_json, columns_json, sort_json, is_default, shared, created_at, updated_at`,
		p.TenantID, p.UserID, p.Page, p.Name, filters, p.ColumnsJSON, p.SortJSON, p.IsDefault, p.Shared,
	).Scan(&v.ID, &v.TenantID, &v.UserID, &v.Page, &v.Name, &v.FiltersJSON, &v.ColumnsJSON, &v.SortJSON, &v.IsDefault, &v.Shared, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: create user view: %w", err)
	}
	return &v, nil
}

func (s *UserViewStore) List(ctx context.Context, userID uuid.UUID, tenantID uuid.UUID, page string) ([]UserView, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, user_id, page, name, filters_json, columns_json, sort_json, is_default, shared, created_at, updated_at
		FROM user_views
		WHERE (user_id = $1 OR (shared = TRUE AND tenant_id = $2)) AND page = $3
		ORDER BY is_default DESC, created_at ASC`,
		userID, tenantID, page,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list user views: %w", err)
	}
	defer rows.Close()

	var views []UserView
	for rows.Next() {
		var v UserView
		if err := rows.Scan(&v.ID, &v.TenantID, &v.UserID, &v.Page, &v.Name, &v.FiltersJSON, &v.ColumnsJSON, &v.SortJSON, &v.IsDefault, &v.Shared, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan user view: %w", err)
		}
		views = append(views, v)
	}
	if views == nil {
		views = []UserView{}
	}
	return views, nil
}

func (s *UserViewStore) Update(ctx context.Context, id, userID uuid.UUID, p UpdateUserViewParams) (*UserView, error) {
	var v UserView
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, page, name, filters_json, columns_json, sort_json, is_default, shared, created_at, updated_at
		FROM user_views WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&v.ID, &v.TenantID, &v.UserID, &v.Page, &v.Name, &v.FiltersJSON, &v.ColumnsJSON, &v.SortJSON, &v.IsDefault, &v.Shared, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserViewNotFound
		}
		return nil, fmt.Errorf("store: get user view: %w", err)
	}

	if p.Name != nil {
		v.Name = *p.Name
	}
	if p.FiltersJSON != nil {
		v.FiltersJSON = p.FiltersJSON
	}
	if p.ColumnsJSON != nil {
		v.ColumnsJSON = p.ColumnsJSON
	}
	if p.SortJSON != nil {
		v.SortJSON = p.SortJSON
	}
	if p.Shared != nil {
		v.Shared = *p.Shared
	}

	if p.IsDefault != nil && *p.IsDefault && !v.IsDefault {
		if _, err := s.db.Exec(ctx,
			`UPDATE user_views SET is_default = FALSE WHERE user_id = $1 AND page = $2 AND is_default = TRUE`,
			userID, v.Page,
		); err != nil {
			return nil, fmt.Errorf("store: clear default views: %w", err)
		}
		v.IsDefault = true
	}

	err = s.db.QueryRow(ctx, `
		UPDATE user_views
		SET name=$1, filters_json=$2, columns_json=$3, sort_json=$4, is_default=$5, shared=$6, updated_at=NOW()
		WHERE id=$7 AND user_id=$8
		RETURNING id, tenant_id, user_id, page, name, filters_json, columns_json, sort_json, is_default, shared, created_at, updated_at`,
		v.Name, v.FiltersJSON, v.ColumnsJSON, v.SortJSON, v.IsDefault, v.Shared, id, userID,
	).Scan(&v.ID, &v.TenantID, &v.UserID, &v.Page, &v.Name, &v.FiltersJSON, &v.ColumnsJSON, &v.SortJSON, &v.IsDefault, &v.Shared, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: update user view: %w", err)
	}
	return &v, nil
}

func (s *UserViewStore) Delete(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM user_views WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("store: delete user view: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserViewNotFound
	}
	return nil
}

func (s *UserViewStore) SetDefault(ctx context.Context, id, userID uuid.UUID, page string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE user_views SET is_default = FALSE WHERE user_id = $1 AND page = $2`,
		userID, page,
	); err != nil {
		return fmt.Errorf("store: clear defaults: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`UPDATE user_views SET is_default = TRUE WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("store: set default: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserViewNotFound
	}

	return tx.Commit(ctx)
}
