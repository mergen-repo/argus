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
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrAccountLocked      = errors.New("auth: account locked")
	ErrAccountDisabled    = errors.New("auth: account disabled")
	ErrInvalid2FACode     = errors.New("auth: invalid 2fa code")
	ErrInvalidRefreshTkn  = errors.New("auth: invalid refresh token")
)

type UserRepository interface {
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	UpdateLoginSuccess(ctx context.Context, id uuid.UUID) error
	IncrementFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error
	SetTOTPSecret(ctx context.Context, id uuid.UUID, secret string) error
	EnableTOTP(ctx context.Context, id uuid.UUID) error
}

type SessionRepository interface {
	Create(ctx context.Context, params CreateSessionParams) (*UserSession, error)
	RevokeSession(ctx context.Context, sessionID uuid.UUID) error
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*UserSession, error)
	GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]UserSession, error)
}

type AuditLogger interface {
	Log(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, action, entityType, entityID string)
}

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
}

type UserSession struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
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
}

type UserInfo struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  string    `json:"role"`
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
	Token        string
	RefreshToken string
	SessionID    uuid.UUID
}

type LockInfo struct {
	RetryAfterSeconds int
	FailedAttempts    int
}

type Config struct {
	JWTSecret           string
	JWTExpiry           time.Duration
	JWTRefreshExpiry    time.Duration
	JWTRememberMeExpiry time.Duration
	JWTIssuer           string
	BcryptCost          int
	MaxLoginAttempts    int
	LockoutDuration     time.Duration
}

type Service struct {
	users    UserRepository
	sessions SessionRepository
	audit    AuditLogger
	cfg      Config
}

func NewService(users UserRepository, sessions SessionRepository, audit AuditLogger, cfg Config) *Service {
	return &Service{
		users:    users,
		sessions: sessions,
		audit:    audit,
		cfg:      cfg,
	}
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

	if user.TOTPEnabled {
		partialToken, err := GenerateToken(s.cfg.JWTSecret, user.ID, user.TenantID, user.Role, 5*time.Minute, true)
		if err != nil {
			return nil, nil, fmt.Errorf("auth: generate partial token: %w", err)
		}
		return &LoginResult{
			User: UserInfo{
				ID:    user.ID,
				Email: user.Email,
				Name:  user.Name,
				Role:  user.Role,
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
			ID:    user.ID,
			Email: user.Email,
			Name:  user.Name,
			Role:  user.Role,
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

	if err := s.users.SetTOTPSecret(ctx, userID, secret); err != nil {
		return nil, fmt.Errorf("auth: store totp secret: %w", err)
	}

	return &Setup2FAResult{
		Secret: secret,
		QRURI:  qrURI,
	}, nil
}

func (s *Service) Verify2FA(ctx context.Context, userID uuid.UUID, code, ipAddr, userAgent string) (*Verify2FAResult, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	if user.TOTPSecret == nil {
		return nil, ErrInvalid2FACode
	}

	if !ValidateTOTPCodeWithWindow(*user.TOTPSecret, code) {
		return nil, ErrInvalid2FACode
	}

	if !user.TOTPEnabled {
		if err := s.users.EnableTOTP(ctx, userID); err != nil {
			return nil, fmt.Errorf("auth: enable totp: %w", err)
		}
	}

	token, refreshToken, sessionID, err := s.createFullSession(ctx, user, &ipAddr, &userAgent, false)
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
