package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials     = errors.New("auth: invalid credentials")
	ErrAccountLocked          = errors.New("auth: account locked")
	ErrAccountDisabled        = errors.New("auth: account disabled")
	ErrInvalid2FACode         = errors.New("auth: invalid 2fa code")
	ErrInvalidRefreshTkn      = errors.New("auth: invalid refresh token")
	ErrPasswordReused         = errors.New("auth: password was recently used")
	ErrInvalidBackupCode      = errors.New("auth: invalid backup code")
	ErrPasswordChangeRequired = errors.New("auth: password change required")
	ErrTOTPNotEnabled         = errors.New("auth: totp not enabled")
	ErrSessionNotFound        = errors.New("auth: session not found")
)

const (
	ReasonPasswordChangeRequired = "password_change_required"
	ReasonPasswordExpired        = "password_expired"
)

type UserRepository interface {
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	UpdateLoginSuccess(ctx context.Context, id uuid.UUID) error
	IncrementFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error
	SetTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error
	EnableTOTP(ctx context.Context, id uuid.UUID) error
	SetPasswordHash(ctx context.Context, id uuid.UUID, hash string) error
	SetPasswordChangeRequired(ctx context.Context, id uuid.UUID, required bool) error
	ClearLockout(ctx context.Context, id uuid.UUID) error
}

type PasswordHistoryRepository interface {
	Insert(ctx context.Context, userID uuid.UUID, hash string) error
	GetLastN(ctx context.Context, userID uuid.UUID, n int) ([]string, error)
	Trim(ctx context.Context, userID uuid.UUID, keep int) error
}

type BackupCodeRepository interface {
	GenerateAndStore(ctx context.Context, userID uuid.UUID, count int, bcryptCost int) ([]string, error)
	ConsumeIfMatch(ctx context.Context, userID uuid.UUID, rawCode string) (bool, int, error)
	CountUnused(ctx context.Context, userID uuid.UUID) (int, error)
	InvalidateAll(ctx context.Context, userID uuid.UUID) error
}

type SessionRepository interface {
	Create(ctx context.Context, params CreateSessionParams) (*UserSession, error)
	RevokeSession(ctx context.Context, sessionID uuid.UUID) error
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*UserSession, error)
	GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]UserSession, error)
	ListActiveByUserID(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]UserSession, string, error)
}

type AuditLogger interface {
	Log(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, action, entityType, entityID string)
}

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
}

type UserSession struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	IPAddress        *string
	UserAgent        *string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

type CreateSessionParams struct {
	UserID           uuid.UUID
	RefreshTokenHash string
	IPAddress        *string
	UserAgent        *string
	ExpiresAt        time.Time
}

type LoginResult struct {
	User         UserInfo
	Token        string
	RefreshToken string
	SessionID    uuid.UUID
	Requires2FA  bool
	Reason       string
}

type UserInfo struct {
	ID                  uuid.UUID `json:"id"`
	Email               string    `json:"email"`
	Name                string    `json:"name"`
	Role                string    `json:"role"`
	OnboardingCompleted bool      `json:"onboarding_completed"`
}

type RefreshResult struct {
	Token        string
	RefreshToken string
	SessionID    uuid.UUID
}

type Setup2FAResult struct {
	Secret string
	QRURI  string
}

type Verify2FAResult struct {
	Token                string
	RefreshToken         string
	SessionID            uuid.UUID
	BackupCodesRemaining int
	UsedBackupCode       bool
}

type Verify2FAInput struct {
	Code       string
	BackupCode string
	IPAddress  string
	UserAgent  string
}

type LockInfo struct {
	RetryAfterSeconds int
	FailedAttempts    int
}

type Config struct {
	JWTSecret            string
	JWTExpiry            time.Duration
	JWTRefreshExpiry     time.Duration
	JWTRememberMeExpiry  time.Duration
	JWTIssuer            string
	BcryptCost           int
	MaxLoginAttempts     int
	LockoutDuration      time.Duration
	EncryptionKey        string
	Policy               PasswordPolicy
	PasswordHistoryCount int
	PasswordMaxAgeDays   int
	BackupCodeCount      int
}

// OnboardingSessionLookup is the FIX-303 dependency contract used to populate
// UserInfo.OnboardingCompleted at login time. Returns true if the tenant has a
// completed onboarding session. Implementations must be fail-safe: any error
// or nil store should propagate `completed=false` so the FE redirects the user
// to the wizard rather than silently skipping onboarding.
type OnboardingSessionLookup interface {
	IsCompleted(ctx context.Context, tenantID uuid.UUID) (bool, error)
}

type Service struct {
	users              UserRepository
	sessions           SessionRepository
	audit              AuditLogger
	passwordHistory    PasswordHistoryRepository
	backupCodes        BackupCodeRepository
	onboardingSessions OnboardingSessionLookup
	cfg                Config
}

func NewService(users UserRepository, sessions SessionRepository, audit AuditLogger, cfg Config) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
		audit:    audit,
		cfg:      cfg,
	}
}

// WithPasswordHistory wires the password history repository used by
// ChangePassword to enforce the configured reuse policy.
func (s *Service) WithPasswordHistory(repo PasswordHistoryRepository) *Service {
	s.passwordHistory = repo
	return s
}

// WithBackupCodes wires the backup code repository used by
// GenerateBackupCodes and VerifyBackupCode.
func (s *Service) WithBackupCodes(repo BackupCodeRepository) *Service {
	s.backupCodes = repo
	return s
}

// WithOnboardingSessions wires the onboarding-session lookup used to populate
// UserInfo.OnboardingCompleted at login. Without it, the field always serializes
// false (fail-safe to redirect to wizard rather than silently skip).
func (s *Service) WithOnboardingSessions(lookup OnboardingSessionLookup) *Service {
	s.onboardingSessions = lookup
	return s
}

// isOnboardingCompleted returns true if the tenant has a completed onboarding
// session OR the user is a super_admin / system role that bypasses tenant
// onboarding. Fail-safe: returns false on nil lookup or any error for non-
// privileged roles so the FE redirects them to the wizard.
//
// FIX-303 patch (super_admin bypass): super_admin and system roles operate
// across all tenants and don't have their own per-tenant onboarding flow;
// treat the flag as completed for them so they land on the dashboard at
// login instead of getting stuck in the wizard for tenant 00000000-...01.
func (s *Service) isOnboardingCompleted(ctx context.Context, role string, tenantID uuid.UUID) bool {
	if role == "super_admin" || role == "system" {
		return true
	}
	if s.onboardingSessions == nil {
		return false
	}
	done, err := s.onboardingSessions.IsCompleted(ctx, tenantID)
	if err != nil {
		return false
	}
	return done
}

func (s *Service) Login(ctx context.Context, email, password, ipAddr, userAgent string, rememberMe bool) (*LoginResult, *LockInfo, error) {
	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	if user.State != "active" {
		return nil, nil, ErrAccountDisabled
	}

	if user.LockedUntil != nil && user.LockedUntil.After(time.Now()) {
		remaining := time.Until(*user.LockedUntil)
		return nil, &LockInfo{
			RetryAfterSeconds: int(remaining.Seconds()),
			FailedAttempts:    user.FailedLoginCount,
		}, ErrAccountLocked
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		newCount := user.FailedLoginCount + 1
		var lockUntil *time.Time
		if newCount >= s.cfg.MaxLoginAttempts {
			t := time.Now().Add(s.cfg.LockoutDuration)
			lockUntil = &t
		}
		_ = s.users.IncrementFailedLogin(ctx, user.ID, lockUntil)

		uid := user.ID
		s.logAudit(ctx, user.TenantID, &uid, "failed_login", "user", user.ID.String())

		if lockUntil != nil {
			return nil, &LockInfo{
				RetryAfterSeconds: int(s.cfg.LockoutDuration.Seconds()),
				FailedAttempts:    newCount,
			}, ErrAccountLocked
		}
		return nil, nil, ErrInvalidCredentials
	}

	_ = s.users.UpdateLoginSuccess(ctx, user.ID)

	if reason := s.passwordChangeReason(user); reason != "" {
		partialToken, err := GeneratePartialToken(s.cfg.JWTSecret, user.ID, user.TenantID, user.Role, 5*time.Minute, true, reason)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: generate partial token: %w", err)
		}
		return &LoginResult{
			User: UserInfo{
				ID:                  user.ID,
				Email:               user.Email,
				Name:                user.Name,
				Role:                user.Role,
				OnboardingCompleted: s.isOnboardingCompleted(ctx, user.Role, user.TenantID),
			},
			Token:  partialToken,
			Reason: reason,
		}, nil, nil
	}

	if user.TOTPEnabled {
		partialToken, err := GenerateToken(s.cfg.JWTSecret, user.ID, user.TenantID, user.Role, 5*time.Minute, true)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: generate partial token: %w", err)
		}
		return &LoginResult{
			User: UserInfo{
				ID:                  user.ID,
				Email:               user.Email,
				Name:                user.Name,
				Role:                user.Role,
				OnboardingCompleted: s.isOnboardingCompleted(ctx, user.Role, user.TenantID),
			},
			Token:       partialToken,
			Requires2FA: true,
		}, nil, nil
	}

	token, refreshToken, sessionID, err := s.createFullSession(ctx, user, &ipAddr, &userAgent, rememberMe)
	if err != nil {
		return nil, nil, err
	}

	uid := user.ID
	s.logAudit(ctx, user.TenantID, &uid, "login", "user", user.ID.String())

	return &LoginResult{
		User: UserInfo{
			ID:                  user.ID,
			Email:               user.Email,
			Name:                user.Name,
			Role:                user.Role,
			OnboardingCompleted: s.isOnboardingCompleted(ctx, user.Role, user.TenantID),
		},
		Token:        token,
		RefreshToken: refreshToken,
		SessionID:    sessionID,
		Requires2FA:  false,
	}, nil, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken, ipAddr, userAgent string) (*RefreshResult, error) {
	sessionID, rawSecret, err := parseRefreshToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidRefreshTkn
	}

	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, ErrInvalidRefreshTkn
	}

	if sess.RevokedAt != nil {
		return nil, ErrInvalidRefreshTkn
	}

	if sess.ExpiresAt.Before(time.Now()) {
		return nil, ErrInvalidRefreshTkn
	}

	if err := bcrypt.CompareHashAndPassword([]byte(sess.RefreshTokenHash), []byte(rawSecret)); err != nil {
		return nil, ErrInvalidRefreshTkn
	}

	_ = s.sessions.RevokeSession(ctx, sess.ID)

	user, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, ErrInvalidRefreshTkn
	}

	token, newRefresh, newSessionID, err := s.createFullSession(ctx, user, &ipAddr, &userAgent, false)
	if err != nil {
		return nil, err
	}

	return &RefreshResult{
		Token:        token,
		RefreshToken: newRefresh,
		SessionID:    newSessionID,
	}, nil
}

func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID, cursor string, limit int) ([]UserSession, string, error) {
	return s.sessions.ListActiveByUserID(ctx, userID, cursor, limit)
}

func (s *Service) RevokeSessionForUser(ctx context.Context, userID uuid.UUID, sessionID uuid.UUID) error {
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return ErrSessionNotFound
	}
	if sess.UserID != userID {
		return ErrSessionNotFound
	}
	return s.sessions.RevokeSession(ctx, sessionID)
}

func (s *Service) Logout(ctx context.Context, userID uuid.UUID, refreshToken string) error {
	sessionID, rawSecret, err := parseRefreshToken(refreshToken)
	if err != nil {
		return nil
	}

	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil
	}

	if bcrypt.CompareHashAndPassword([]byte(sess.RefreshTokenHash), []byte(rawSecret)) != nil {
		return nil
	}

	_ = s.sessions.RevokeSession(ctx, sess.ID)

	user, _ := s.users.GetByID(ctx, userID)
	if user != nil {
		uid := user.ID
		s.logAudit(ctx, user.TenantID, &uid, "logout", "user", user.ID.String())
	}

	return nil
}

func (s *Service) Setup2FA(ctx context.Context, userID uuid.UUID) (*Setup2FAResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	secret, qrURI, err := GenerateTOTPSecret(user.Email)
	if err != nil {
		return nil, fmt.Errorf("auth: generate totp secret: %w", err)
	}

	encrypted, err := EncryptTOTPSecret(secret, s.cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("auth: encrypt totp secret: %w", err)
	}

	if err := s.users.SetTOTPSecret(ctx, userID, encrypted); err != nil {
		return nil, fmt.Errorf("auth: store totp secret: %w", err)
	}

	return &Setup2FAResult{
		Secret: secret,
		QRURI:  qrURI,
	}, nil
}

func (s *Service) Verify2FA(ctx context.Context, userID uuid.UUID, code, ipAddr, userAgent string) (*Verify2FAResult, error) {
	return s.Verify2FAWithInput(ctx, userID, Verify2FAInput{
		Code:      code,
		IPAddress: ipAddr,
		UserAgent: userAgent,
	})
}

// Verify2FAWithInput verifies either a TOTP code or, when TOTP is not provided
// and a non-empty BackupCode is supplied, consumes a single-use backup code.
// TOTP is preferred; backup codes are only consumed when Code is empty.
func (s *Service) Verify2FAWithInput(ctx context.Context, userID uuid.UUID, in Verify2FAInput) (*Verify2FAResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(in.Code) == "" && strings.TrimSpace(in.BackupCode) != "" {
		return s.verifyBackupCodeInternal(ctx, user, in.BackupCode, in.IPAddress, in.UserAgent)
	}

	if user.TOTPSecret == nil {
		return nil, ErrInvalid2FACode
	}

	plainSecret, err := DecryptTOTPSecret(*user.TOTPSecret, s.cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("auth: decrypt totp secret: %w", err)
	}

	if !ValidateTOTPCodeWithWindow(plainSecret, in.Code) {
		return nil, ErrInvalid2FACode
	}

	if !user.TOTPEnabled {
		if err := s.users.EnableTOTP(ctx, userID); err != nil {
			return nil, fmt.Errorf("auth: enable totp: %w", err)
		}
	}

	token, refreshToken, sessionID, err := s.createFullSession(ctx, user, &in.IPAddress, &in.UserAgent, false)
	if err != nil {
		return nil, err
	}

	uid := user.ID
	s.logAudit(ctx, user.TenantID, &uid, "login_2fa", "user", user.ID.String())

	return &Verify2FAResult{
		Token:        token,
		RefreshToken: refreshToken,
		SessionID:    sessionID,
	}, nil
}

// VerifyBackupCode consumes a single-use backup code for a user and, on
// success, issues a full session. Returns the remaining unused backup-code
// count for UX warnings. Returns ErrInvalidBackupCode if the code does not
// match any stored hash.
func (s *Service) VerifyBackupCode(ctx context.Context, userID uuid.UUID, code, ipAddr, userAgent string) (*Verify2FAResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.verifyBackupCodeInternal(ctx, user, code, ipAddr, userAgent)
}

func (s *Service) verifyBackupCodeInternal(ctx context.Context, user *User, code, ipAddr, userAgent string) (*Verify2FAResult, error) {
	if s.backupCodes == nil {
		return nil, ErrInvalidBackupCode
	}
	normalized := NormalizeBackupCode(code)
	if normalized == "" {
		return nil, ErrInvalidBackupCode
	}

	ok, remaining, err := s.backupCodes.ConsumeIfMatch(ctx, user.ID, normalized)
	if err != nil {
		return nil, fmt.Errorf("auth: consume backup code: %w", err)
	}
	if !ok {
		return nil, ErrInvalidBackupCode
	}

	token, refreshToken, sessionID, err := s.createFullSession(ctx, user, &ipAddr, &userAgent, false)
	if err != nil {
		return nil, err
	}

	uid := user.ID
	s.logAudit(ctx, user.TenantID, &uid, "user.login_backup_code", "user", user.ID.String())

	return &Verify2FAResult{
		Token:                token,
		RefreshToken:         refreshToken,
		SessionID:            sessionID,
		BackupCodesRemaining: remaining,
		UsedBackupCode:       true,
	}, nil
}

// GenerateBackupCodes invalidates any existing backup codes for the user and
// emits a fresh set according to cfg.BackupCodeCount (defaulting to 10). The
// raw codes are returned only this once; after this call, only their hashes
// remain stored.
func (s *Service) GenerateBackupCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if s.backupCodes == nil {
		return nil, errors.New("auth: backup codes repository not configured")
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if !user.TOTPEnabled {
		return nil, ErrTOTPNotEnabled
	}

	count := s.cfg.BackupCodeCount
	if count <= 0 {
		count = 10
	}

	codes, err := s.backupCodes.GenerateAndStore(ctx, userID, count, s.cfg.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth: generate backup codes: %w", err)
	}

	uid := user.ID
	s.logAudit(ctx, user.TenantID, &uid, "user.backup_codes_generated", "user", user.ID.String())

	return codes, nil
}

func (s *Service) BackupCodesRemaining(ctx context.Context, userID uuid.UUID) (int, bool, error) {
	if s.backupCodes == nil {
		return 0, false, nil
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	remaining, err := s.backupCodes.CountUnused(ctx, userID)
	if err != nil {
		return 0, false, err
	}
	return remaining, user.TOTPEnabled, nil
}

// ChangePassword verifies the current password, validates the new password
// against the configured policy and reuse history, rotates the password hash,
// clears the force-change flag and lockout state, and records the change in
// password history (trimmed to cfg.PasswordHistoryCount).
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentPwd, newPwd string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPwd)); err != nil {
		return ErrInvalidCredentials
	}

	if err := ValidatePasswordPolicy(newPwd, s.cfg.Policy); err != nil {
		return err
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(newPwd)) == nil {
		return ErrPasswordReused
	}

	if s.passwordHistory != nil && s.cfg.PasswordHistoryCount > 0 {
		recent, err := s.passwordHistory.GetLastN(ctx, userID, s.cfg.PasswordHistoryCount)
		if err != nil {
			return fmt.Errorf("auth: read password history: %w", err)
		}
		for _, h := range recent {
			if bcrypt.CompareHashAndPassword([]byte(h), []byte(newPwd)) == nil {
				return ErrPasswordReused
			}
		}
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPwd), s.cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}

	if err := s.users.SetPasswordHash(ctx, userID, string(newHash)); err != nil {
		return fmt.Errorf("auth: persist password hash: %w", err)
	}

	if s.passwordHistory != nil {
		if err := s.passwordHistory.Insert(ctx, userID, string(newHash)); err != nil {
			return fmt.Errorf("auth: insert password history: %w", err)
		}
		if s.cfg.PasswordHistoryCount > 0 {
			if err := s.passwordHistory.Trim(ctx, userID, s.cfg.PasswordHistoryCount); err != nil {
				return fmt.Errorf("auth: trim password history: %w", err)
			}
		}
	}

	if err := s.users.SetPasswordChangeRequired(ctx, userID, false); err != nil {
		return fmt.Errorf("auth: clear password change flag: %w", err)
	}
	if err := s.users.ClearLockout(ctx, userID); err != nil {
		return fmt.Errorf("auth: clear lockout: %w", err)
	}

	uid := user.ID
	s.logAudit(ctx, user.TenantID, &uid, "user.password_change", "user", user.ID.String())

	return nil
}

// ResetPasswordForUser sets a new password for the given user without requiring
// the current password. It validates the new password against the configured
// policy, updates the password hash, clears lockout, and clears the
// password-change-required flag. It is intended for use by the password-reset
// flow after the caller has already verified the reset token.
func (s *Service) ResetPasswordForUser(ctx context.Context, userID uuid.UUID, newPwd string) error {
	if err := ValidatePasswordPolicy(newPwd, s.cfg.Policy); err != nil {
		return err
	}

	if s.passwordHistory != nil && s.cfg.PasswordHistoryCount > 0 {
		recent, err := s.passwordHistory.GetLastN(ctx, userID, s.cfg.PasswordHistoryCount)
		if err != nil {
			return fmt.Errorf("auth: read password history: %w", err)
		}
		for _, h := range recent {
			if bcrypt.CompareHashAndPassword([]byte(h), []byte(newPwd)) == nil {
				return ErrPasswordReused
			}
		}
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPwd), s.cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}

	if err := s.users.SetPasswordHash(ctx, userID, string(newHash)); err != nil {
		return fmt.Errorf("auth: persist password hash: %w", err)
	}

	if s.passwordHistory != nil {
		if err := s.passwordHistory.Insert(ctx, userID, string(newHash)); err != nil {
			return fmt.Errorf("auth: insert password history: %w", err)
		}
		if s.cfg.PasswordHistoryCount > 0 {
			if err := s.passwordHistory.Trim(ctx, userID, s.cfg.PasswordHistoryCount); err != nil {
				return fmt.Errorf("auth: trim password history: %w", err)
			}
		}
	}

	if err := s.users.SetPasswordChangeRequired(ctx, userID, false); err != nil {
		return fmt.Errorf("auth: clear password change flag: %w", err)
	}
	if err := s.users.ClearLockout(ctx, userID); err != nil {
		return fmt.Errorf("auth: clear lockout: %w", err)
	}

	return nil
}

// CreateSessionForUser creates a full JWT + refresh session for the given user
// after a successful password change. It accepts optional ipAddr and userAgent
// strings (may be empty).
func (s *Service) CreateSessionForUser(ctx context.Context, userID uuid.UUID, ipAddr, userAgent string) (*LoginResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	var ip, ua *string
	if ipAddr != "" {
		ip = &ipAddr
	}
	if userAgent != "" {
		ua = &userAgent
	}

	token, refreshToken, sessionID, err := s.createFullSession(ctx, user, ip, ua, false)
	if err != nil {
		return nil, err
	}

	return &LoginResult{
		User: UserInfo{
			ID:                  user.ID,
			Email:               user.Email,
			Name:                user.Name,
			Role:                user.Role,
			OnboardingCompleted: s.isOnboardingCompleted(ctx, user.Role, user.TenantID),
		},
		Token:        token,
		RefreshToken: refreshToken,
		SessionID:    sessionID,
	}, nil
}

// passwordChangeReason returns a non-empty reason string when the user must
// change their password before a full session may be issued.
func (s *Service) passwordChangeReason(user *User) string {
	if user.PasswordChangeRequired {
		return ReasonPasswordChangeRequired
	}
	if s.cfg.PasswordMaxAgeDays > 0 && user.PasswordChangedAt != nil {
		maxAge := time.Duration(s.cfg.PasswordMaxAgeDays) * 24 * time.Hour
		if time.Since(*user.PasswordChangedAt) > maxAge {
			return ReasonPasswordExpired
		}
	}
	return ""
}

func (s *Service) createFullSession(ctx context.Context, user *User, ipAddr, userAgent *string, rememberMe bool) (string, string, uuid.UUID, error) {
	jwtExpiry := s.cfg.JWTExpiry
	refreshExpiry := s.cfg.JWTRefreshExpiry
	if rememberMe {
		jwtExpiry = s.cfg.JWTRememberMeExpiry
		refreshExpiry = s.cfg.JWTRememberMeExpiry
	}

	token, err := GenerateToken(s.cfg.JWTSecret, user.ID, user.TenantID, user.Role, jwtExpiry, false)
	if err != nil {
		return "", "", uuid.Nil, fmt.Errorf("auth: generate token: %w", err)
	}

	rawSecret, err := generateRandomSecret()
	if err != nil {
		return "", "", uuid.Nil, fmt.Errorf("auth: generate refresh secret: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(rawSecret), s.cfg.BcryptCost)
	if err != nil {
		return "", "", uuid.Nil, fmt.Errorf("auth: hash refresh token: %w", err)
	}

	sess, err := s.sessions.Create(ctx, CreateSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: string(hash),
		IPAddress:        ipAddr,
		UserAgent:        userAgent,
		ExpiresAt:        time.Now().Add(refreshExpiry),
	})
	if err != nil {
		return "", "", uuid.Nil, fmt.Errorf("auth: create session: %w", err)
	}

	compositeToken := buildRefreshToken(sess.ID, rawSecret)
	return token, compositeToken, sess.ID, nil
}

func buildRefreshToken(sessionID uuid.UUID, rawSecret string) string {
	return sessionID.String() + "." + rawSecret
}

func parseRefreshToken(token string) (uuid.UUID, string, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return uuid.Nil, "", ErrInvalidRefreshTkn
	}
	sessionID, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", ErrInvalidRefreshTkn
	}
	return sessionID, parts[1], nil
}

func (s *Service) logAudit(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, action, entityType, entityID string) {
	if s.audit != nil {
		s.audit.Log(ctx, tenantID, userID, action, entityType, entityID)
	}
}

func generateRandomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func HashPassword(password string, cost int) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}
