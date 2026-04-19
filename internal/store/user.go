package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func encryptTOTPValue(plain, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("invalid hex key: %w", err)
	}
	encrypted, err := crypto.Encrypt([]byte(plain), key)
	if err != nil {
		return "", err
	}
	return string(encrypted), nil
}

func decryptTOTPProbe(value, hexKey string) (string, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("invalid hex key: %w", err)
	}
	plaintext, err := crypto.Decrypt([]byte(value), key)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

var (
	ErrUserNotFound    = errors.New("store: user not found")
	ErrSessionNotFound = errors.New("store: session not found")
	ErrEmailExists     = errors.New("store: email already exists in tenant")
)

type User struct {
	ID                     uuid.UUID
	TenantID               uuid.UUID
	Email                  string
	PasswordHash           string
	Name                   string
	Role                   string
	TOTPSecret             *string
	TOTPEnabled            bool
	State                  string
	LastLoginAt            *time.Time
	FailedLoginCount       int
	LockedUntil            *time.Time
	PasswordChangeRequired bool
	PasswordChangedAt      *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
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
		        state, last_login_at, failed_login_count, locked_until,
		        password_change_required, password_changed_at, created_at, updated_at
		 FROM users WHERE email = $1 AND state = 'active' LIMIT 1`, email).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt)
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
		        state, last_login_at, failed_login_count, locked_until,
		        password_change_required, password_changed_at, created_at, updated_at
		 FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt)
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

type TOTPSecretRow struct {
	ID     uuid.UUID
	Secret string
}

func (s *UserStore) ListTOTPSecrets(ctx context.Context) ([]TOTPSecretRow, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, totp_secret FROM users WHERE totp_secret IS NOT NULL AND totp_enabled = true`)
	if err != nil {
		return nil, fmt.Errorf("store: list totp secrets: %w", err)
	}
	defer rows.Close()

	var results []TOTPSecretRow
	for rows.Next() {
		var r TOTPSecretRow
		if err := rows.Scan(&r.ID, &r.Secret); err != nil {
			return nil, fmt.Errorf("store: scan totp secret row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate totp secrets: %w", err)
	}
	return results, nil
}

func (s *UserStore) MigrateTOTPSecretsToEncrypted(ctx context.Context, hexKey string) (int, error) {
	if hexKey == "" {
		return 0, nil
	}

	rows, err := s.ListTOTPSecrets(ctx)
	if err != nil {
		return 0, err
	}

	migrated := 0
	for _, row := range rows {
		if _, err := decryptTOTPProbe(row.Secret, hexKey); err == nil {
			continue
		}
		encrypted, err := encryptTOTPValue(row.Secret, hexKey)
		if err != nil {
			return migrated, fmt.Errorf("store: encrypt totp secret for user %s: %w", row.ID, err)
		}
		if err := s.SetTOTPSecret(ctx, row.ID, encrypted); err != nil {
			return migrated, fmt.Errorf("store: persist encrypted totp secret for user %s: %w", row.ID, err)
		}
		migrated++
	}
	return migrated, nil
}

func (s *UserStore) EnableTOTP(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET totp_enabled = true WHERE id = $1`, id)
	return err
}

func (s *UserStore) SetPasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET password_hash = $1, password_changed_at = NOW() WHERE id = $2`, hash, userID)
	return err
}

func (s *UserStore) SetPasswordChangeRequired(ctx context.Context, userID uuid.UUID, required bool) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET password_change_required = $1 WHERE id = $2`, required, userID)
	return err
}

func (s *UserStore) ClearLockout(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE users SET failed_login_count = 0, locked_until = NULL WHERE id = $1`, userID)
	return err
}

func (s *UserStore) GetPasswordChangeRequired(ctx context.Context, userID uuid.UUID) (bool, error) {
	var required bool
	err := s.db.QueryRow(ctx,
		`SELECT password_change_required FROM users WHERE id = $1`, userID).Scan(&required)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUserNotFound
	}
	if err != nil {
		return false, err
	}
	return required, nil
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

// RevokeAllByTenant revokes all active user_sessions for every user belonging to tenantID.
// It returns the number of distinct affected users and total sessions revoked.
func (s *SessionStore) RevokeAllByTenant(ctx context.Context, tenantID uuid.UUID) (affectedUsers int64, sessionsRevoked int64, err error) {
	row := s.db.QueryRow(ctx, `
		WITH revoked AS (
			UPDATE user_sessions SET revoked_at = NOW()
			WHERE user_id IN (SELECT id FROM users WHERE tenant_id = $1)
			  AND revoked_at IS NULL
			RETURNING user_id
		)
		SELECT COUNT(DISTINCT user_id), COUNT(*) FROM revoked
	`, tenantID)
	err = row.Scan(&affectedUsers, &sessionsRevoked)
	return
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

func (s *SessionStore) ListActiveByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]UserSession, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	args := []interface{}{userID, limit + 1}
	conditions := []string{"user_id = $1", "revoked_at IS NULL", "expires_at > NOW()"}
	argIdx := 3

	if cursor != "" {
		cursorID, parseErr := uuid.Parse(cursor)
		if parseErr == nil {
			conditions = append(conditions, fmt.Sprintf("id < $%d", argIdx))
			args = append(args, cursorID)
			argIdx++
		}
	}

	where := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT id, user_id, refresh_token_hash, ip_address::text, user_agent, expires_at, revoked_at, created_at
		FROM user_sessions
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, where)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []UserSession
	for rows.Next() {
		var sess UserSession
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.RefreshTokenHash, &sess.IPAddress,
			&sess.UserAgent, &sess.ExpiresAt, &sess.RevokedAt, &sess.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("store: scan session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: iterate sessions: %w", err)
	}

	nextCursor := ""
	if len(sessions) > limit {
		nextCursor = sessions[limit-1].ID.String()
		sessions = sessions[:limit]
	}

	return sessions, nextCursor, nil
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
	return s.CreateUserWithPassword(ctx, p, "")
}

func (s *UserStore) CreateUserWithPassword(ctx context.Context, p CreateUserParams, passwordHash string) (*User, error) {
	tenantID, err := TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.CreateUserInTenant(ctx, tenantID, p, passwordHash)
}

func (s *UserStore) CreateUserInTenant(ctx context.Context, tenantID uuid.UUID, p CreateUserParams, passwordHash string) (*User, error) {
	state := "invited"
	pwChangeRequired := true
	if passwordHash != "" {
		state = "active"
	}

	var u User
	err := s.db.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, password_hash, name, role, state, password_change_required)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
			state, last_login_at, failed_login_count, locked_until,
			password_change_required, password_changed_at, created_at, updated_at
	`, tenantID, p.Email, passwordHash, p.Name, p.Role, state, pwChangeRequired).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt)
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
			state, last_login_at, failed_login_count, locked_until,
			password_change_required, password_changed_at, created_at, updated_at
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
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
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
			state, last_login_at, failed_login_count, locked_until,
			password_change_required, password_changed_at, created_at, updated_at
	`, strings.Join(sets, ", "))

	var u User
	err = s.db.QueryRow(ctx, query, args...).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt)
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

// PurgeResult reports the effect of a GDPR PII erasure on a user.
type PurgeResult struct {
	UserID          uuid.UUID
	SessionsRevoked int64
	PurgedAt        time.Time
}

// DeletePII erases PII for a single user per GDPR Article 17 (right to
// erasure). Email/name/password_hash/totp_secret are nulled out, TOTP is
// disabled, state is set to "purged", and all active user_sessions are
// revoked. The row remains for referential integrity with audit logs and
// historical SIM assignments. This operation is irreversible.
func (s *UserStore) DeletePII(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) (*PurgeResult, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Verify the user exists in this tenant first so we can return a
	// consistent ErrUserNotFound rather than silently rewriting an
	// unrelated user.
	var existingTenant uuid.UUID
	err = tx.QueryRow(ctx, `SELECT tenant_id FROM users WHERE id = $1`, id).Scan(&existingTenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: look up user for purge: %w", err)
	}
	if existingTenant != tenantID {
		return nil, ErrUserNotFound
	}

	purgedEmail := fmt.Sprintf("purged+%s@purged.invalid", id.String())
	tag, err := tx.Exec(ctx, `
		UPDATE users SET
			email = $2,
			password_hash = '',
			name = '',
			totp_secret = NULL,
			totp_enabled = false,
			state = 'purged',
			updated_at = NOW()
		WHERE id = $1 AND tenant_id = $3
	`, id, purgedEmail, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: null user PII: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrUserNotFound
	}

	sessTag, err := tx.Exec(ctx,
		`UPDATE user_sessions SET revoked_at = NOW()
		 WHERE user_id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return nil, fmt.Errorf("store: revoke user sessions: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("store: commit PII purge: %w", err)
	}

	return &PurgeResult{
		UserID:          id,
		SessionsRevoked: sessTag.RowsAffected(),
		PurgedAt:        time.Now().UTC(),
	}, nil
}

func (s *UserStore) GetByIDGlobal(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := s.db.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
			   state, last_login_at, failed_login_count, locked_until,
			   password_change_required, password_changed_at, created_at, updated_at
		FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get user by id global: %w", err)
	}
	return &u, nil
}

func (s *UserStore) UpdateLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET locale = $1 WHERE id = $2`, locale, userID)
	if err != nil {
		return fmt.Errorf("store: update locale: %w", err)
	}
	return nil
}

func (s *UserStore) ListByRole(ctx context.Context, tenantID uuid.UUID, role string) ([]User, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, tenant_id, email, password_hash, name, role, totp_secret, totp_enabled,
			   state, last_login_at, failed_login_count, locked_until,
			   password_change_required, password_changed_at, created_at, updated_at
		FROM users
		WHERE tenant_id = $1 AND role = $2 AND state = 'active'
		ORDER BY created_at ASC`,
		tenantID, role,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list users by role: %w", err)
	}
	defer rows.Close()

	var results []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Name, &u.Role,
			&u.TOTPSecret, &u.TOTPEnabled, &u.State, &u.LastLoginAt,
			&u.FailedLoginCount, &u.LockedUntil,
			&u.PasswordChangeRequired, &u.PasswordChangedAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan user by role: %w", err)
		}
		results = append(results, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate users by role: %w", err)
	}
	return results, nil
}
