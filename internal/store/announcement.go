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

var ErrAnnouncementNotFound = errors.New("store: announcement not found")

type Announcement struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Type        string     `json:"type"`
	Target      string     `json:"target"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      time.Time  `json:"ends_at"`
	Dismissible bool       `json:"dismissible"`
	CreatedBy   *uuid.UUID `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreateAnnouncementParams struct {
	Title       string
	Body        string
	Type        string
	Target      string
	StartsAt    time.Time
	EndsAt      time.Time
	Dismissible bool
	CreatedBy   *uuid.UUID
}

type UpdateAnnouncementParams struct {
	Title       *string
	Body        *string
	Type        *string
	Target      *string
	StartsAt    *time.Time
	EndsAt      *time.Time
	Dismissible *bool
}

type AnnouncementStore struct {
	db *pgxpool.Pool
}

func NewAnnouncementStore(db *pgxpool.Pool) *AnnouncementStore {
	return &AnnouncementStore{db: db}
}

func (s *AnnouncementStore) Create(ctx context.Context, p CreateAnnouncementParams) (*Announcement, error) {
	var a Announcement
	err := s.db.QueryRow(ctx, `
		INSERT INTO announcements (title, body, type, target, starts_at, ends_at, dismissible, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, title, body, type, target, starts_at, ends_at, dismissible, created_by, created_at`,
		p.Title, p.Body, p.Type, p.Target, p.StartsAt, p.EndsAt, p.Dismissible, p.CreatedBy,
	).Scan(&a.ID, &a.Title, &a.Body, &a.Type, &a.Target, &a.StartsAt, &a.EndsAt, &a.Dismissible, &a.CreatedBy, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: create announcement: %w", err)
	}
	return &a, nil
}

func (s *AnnouncementStore) List(ctx context.Context, page, limit int) ([]Announcement, error) {
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.Query(ctx,
		`SELECT id, title, body, type, target, starts_at, ends_at, dismissible, created_by, created_at
		 FROM announcements ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list announcements: %w", err)
	}
	defer rows.Close()

	var list []Announcement
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.Title, &a.Body, &a.Type, &a.Target, &a.StartsAt, &a.EndsAt, &a.Dismissible, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan announcement: %w", err)
		}
		list = append(list, a)
	}
	if list == nil {
		list = []Announcement{}
	}
	return list, nil
}

func (s *AnnouncementStore) GetActive(ctx context.Context, tenantID uuid.UUID, userID uuid.UUID) ([]Announcement, error) {
	now := time.Now().UTC()
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.title, a.body, a.type, a.target, a.starts_at, a.ends_at, a.dismissible, a.created_by, a.created_at
		FROM announcements a
		WHERE a.starts_at <= $1 AND a.ends_at >= $1
		  AND (a.target = 'all' OR a.target = $2::text)
		  AND NOT EXISTS (
		      SELECT 1 FROM announcement_dismissals d
		      WHERE d.announcement_id = a.id AND d.user_id = $3
		  )
		ORDER BY a.starts_at DESC`,
		now, tenantID.String(), userID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get active announcements: %w", err)
	}
	defer rows.Close()

	var list []Announcement
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.Title, &a.Body, &a.Type, &a.Target, &a.StartsAt, &a.EndsAt, &a.Dismissible, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan active announcement: %w", err)
		}
		list = append(list, a)
	}
	if list == nil {
		list = []Announcement{}
	}
	return list, nil
}

func (s *AnnouncementStore) Update(ctx context.Context, id uuid.UUID, p UpdateAnnouncementParams) (*Announcement, error) {
	existing, err := s.getByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if p.Title != nil {
		existing.Title = *p.Title
	}
	if p.Body != nil {
		existing.Body = *p.Body
	}
	if p.Type != nil {
		existing.Type = *p.Type
	}
	if p.Target != nil {
		existing.Target = *p.Target
	}
	if p.StartsAt != nil {
		existing.StartsAt = *p.StartsAt
	}
	if p.EndsAt != nil {
		existing.EndsAt = *p.EndsAt
	}
	if p.Dismissible != nil {
		existing.Dismissible = *p.Dismissible
	}

	var a Announcement
	err = s.db.QueryRow(ctx, `
		UPDATE announcements SET title=$1, body=$2, type=$3, target=$4, starts_at=$5, ends_at=$6, dismissible=$7
		WHERE id=$8
		RETURNING id, title, body, type, target, starts_at, ends_at, dismissible, created_by, created_at`,
		existing.Title, existing.Body, existing.Type, existing.Target, existing.StartsAt, existing.EndsAt, existing.Dismissible, id,
	).Scan(&a.ID, &a.Title, &a.Body, &a.Type, &a.Target, &a.StartsAt, &a.EndsAt, &a.Dismissible, &a.CreatedBy, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("store: update announcement: %w", err)
	}
	return &a, nil
}

func (s *AnnouncementStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM announcements WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete announcement: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAnnouncementNotFound
	}
	return nil
}

func (s *AnnouncementStore) Dismiss(ctx context.Context, announcementID, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO announcement_dismissals (announcement_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`,
		announcementID, userID,
	)
	if err != nil {
		return fmt.Errorf("store: dismiss announcement: %w", err)
	}
	return nil
}

func (s *AnnouncementStore) getByID(ctx context.Context, id uuid.UUID) (*Announcement, error) {
	var a Announcement
	err := s.db.QueryRow(ctx, `
		SELECT id, title, body, type, target, starts_at, ends_at, dismissible, created_by, created_at
		FROM announcements WHERE id = $1`, id,
	).Scan(&a.ID, &a.Title, &a.Body, &a.Type, &a.Target, &a.StartsAt, &a.EndsAt, &a.Dismissible, &a.CreatedBy, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAnnouncementNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get announcement: %w", err)
	}
	return &a, nil
}
