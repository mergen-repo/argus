package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserNotFound    = errors.New("store: user not found")
	ErrSessionNotFound = errors.New("store: session not found")
	ErrEmailExists     = errors.New("store: email already exists in tenant")
)

type User struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Email            string
	PasswordHash     string
	Name             string
	Role             string
	TOTPSecret       *string
	TOTPEnabled      bool
	State            string
	LastLoginAt      *time.Time
	FailedLoginCount int
	LockedUntil      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type UserSession struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	IPAddress        *string
	UserAgent        *string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
}

type CreateSessionParams struct {
	UserID           uuid.UUID
	RefreshTokenHash string
	IPAddress        *string
	UserAgent        *string
	ExpiresAt        time.Time
}

type UserStore struct {
	db *pgxpool.Pool
}

func NewUserStore(db *pgxpool.Pool) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`SELECT id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
		        state, last_login_at, failed_login_count, locked_until, created_at, updated_at
		 FROM users WHERE email = $1 AND state = 'active' LIMIT 1`, email).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := s.db.QueryRow(ctx,
		`SELECT id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
		        state, last_login_at, failed_login_count, locked_until, created_at, updated_at
		 FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *UserStore) UpdateLoginSuccess(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET last_login_at = NOW(), failed_login_count = 0, locked_until = NULL WHERE id = $1`, id)
	return err
}

func (s *UserStore) IncrementFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET failed_login_count = failed_login_count + 1, locked_until = $2 WHERE id = $1`, id, lockUntil)
	return err
}

func (s *UserStore) SetTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET totp_secret = $2 WHERE id = $1`, id, secret)
	return err
}

func (s *UserStore) EnableTOTP(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET totp_enabled = true WHERE id = $1`, id)
	return err
}

type SessionStore struct {
	db *pgxpool.Pool
}

func NewSessionStore(db *pgxpool.Pool) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) Create(ctx context.Context, params CreateSessionParams) (*UserSession, error) {
	var sess UserSession
	err := s.db.QueryRow(ctx,
		`INSERT INTO user_sessions (user_id, refresh_token_hash, ip_address, user_agent, expires_at)
		 VALUES ($1, $2, $3::inet, $4, $5)
		 RETURNING id, user_id, refresh_token_hash, ip_address::text, user_agent, expires_at, revoked_at, created_at`,
		params.UserID, params.RefreshTokenHash, params.IPAddress, params.UserAgent, params.ExpiresAt).
		Scan(&sess.ID, &sess.UserID, &sess.RefreshTokenHash, &sess.IPAddress,
			&sess.UserAgent, &sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *SessionStore) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE user_sessions SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SessionStore) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE user_sessions SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *SessionStore) GetByID(ctx context.Context, id uuid.UUID) (*UserSession, error) {
	var sess UserSession
	err := s.db.QueryRow(ctx,
		`SELECT id, user_id, refresh_token_hash, ip_address::text, user_agent, expires_at, revoked_at, created_at
		 FROM user_sessions WHERE id = $1`, id).
		Scan(&sess.ID, &sess.UserID, &sess.RefreshTokenHash, &sess.IPAddress,
			&sess.UserAgent, &sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *SessionStore) GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]UserSession, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, user_id, refresh_token_hash, ip_address::text, user_agent, expires_at, revoked_at, created_at
		 FROM user_sessions WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > NOW()
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []UserSession
	for rows.Next() {
		var sess UserSession
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.RefreshTokenHash, &sess.IPAddress,
			&sess.UserAgent, &sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

type CreateUserParams struct {
	Email string
	Name  string
	Role  string
}

type UpdateUserParams struct {
	Name  *string
	Role  *string
	State *string
}

func (s *UserStore) CreateUser(ctx context.Context, p CreateUserParams) (*User, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	var u User
	err = s.db.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, password_hash, name, role, state)
		VALUES ($1, $2, '', $3, $4, 'invited')
		RETURNING id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
			state, last_login_at, failed_login_count, locked_until, created_at, updated_at
	`, tenantID, p.Email, p.Name, p.Role).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrEmailExists
		}
		return nil, fmt.Errorf("store: create user: %w", err)
	}
	return &u, nil
}

func (s *UserStore) ListByTenant(ctx context.Context, cursor string, limit int, roleFilter string, stateFilter string) ([]User, string, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, "", err
	}

	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{tenantID}
	conditions := []string{"tenant_id = $1"}
	argIdx := 2

	if roleFilter != "" {
		conditions = append(conditions, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, roleFilter)
		argIdx++
	}

	if stateFilter != "" {
		conditions = append(conditions, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, stateFilter)
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

	query := fmt.Sprintf(`
		SELECT id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
			state, last_login_at, failed_login_count, locked_until, created_at, updated_at
		FROM users
		%s
		ORDER BY created_at DESC, id DESC
		LIMIT %s
	`, where, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()

	var results []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, "", fmt.Errorf("store: scan user: %w", err)
		}
		results = append(results, u)
	}

	nextCursor := ""
	if len(results) > limit {
		nextCursor = results[limit-1].ID.String()
		results = results[:limit]
	}

	return results, nextCursor, nil
}

func (s *UserStore) UpdateUser(ctx context.Context, id uuid.UUID, p UpdateUserParams) (*User, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	sets := []string{}
	args := []interface{}{id, tenantID}
	argIdx := 3

	if p.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.Role != nil {
		sets = append(sets, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, *p.Role)
		argIdx++
	}
	if p.State != nil {
		sets = append(sets, fmt.Sprintf("state = $%d", argIdx))
		args = append(args, *p.State)
		argIdx++
	}

	if len(sets) == 0 {
		return s.GetByID(ctx, id)
	}

	query := fmt.Sprintf(`
		UPDATE users SET %s
		WHERE id = $1 AND tenant_id = $2
		RETURNING id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
			state, last_login_at, failed_login_count, locked_until, created_at, updated_at
	`, strings.Join(sets, ", "))

	var u User
	err = s.db.QueryRow(ctx, query, args...).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: update user: %w", err)
	}
	return &u, nil
}

func (s *UserStore) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND state != 'terminated'`, tenantID).
		Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count users by tenant: %w", err)
	}
	return count, nil
}
