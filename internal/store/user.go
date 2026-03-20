package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserNotFound    = errors.New("store: user not found")
	ErrSessionNotFound = errors.New("store: session not found")
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
		 RETURNING id, user_id, refresh_token_hash, ip_address, user_agent, expires_at, revoked_at, created_at`,
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
		`SELECT id, user_id, refresh_token_hash, ip_address, user_agent, expires_at, revoked_at, created_at
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
		`SELECT id, user_id, refresh_token_hash, ip_address, user_agent, expires_at, revoked_at, created_at
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
