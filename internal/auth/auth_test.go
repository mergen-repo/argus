package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type mockUserRepo struct {
	users map[string]*User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*User)}
}

func (m *mockUserRepo) addUser(u *User) {
	m.users[u.Email] = u
	m.users[u.ID.String()] = u
}

func (m *mockUserRepo) GetByEmail(_ context.Context, email string) (*User, error) {
	u, ok := m.users[email]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}

func (m *mockUserRepo) GetByID(_ context.Context, id uuid.UUID) (*User, error) {
	u, ok := m.users[id.String()]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}

func (m *mockUserRepo) UpdateLoginSuccess(_ context.Context, id uuid.UUID) error {
	if u, ok := m.users[id.String()]; ok {
		now := time.Now()
		u.LastLoginAt = &now
		u.FailedLoginCount = 0
		u.LockedUntil = nil
	}
	return nil
}

func (m *mockUserRepo) IncrementFailedLogin(_ context.Context, id uuid.UUID, lockUntil *time.Time) error {
	if u, ok := m.users[id.String()]; ok {
		u.FailedLoginCount++
		u.LockedUntil = lockUntil
	}
	return nil
}

func (m *mockUserRepo) SetTOTPSecret(_ context.Context, id uuid.UUID, secret string) error {
	if u, ok := m.users[id.String()]; ok {
		u.TOTPSecret = &secret
	}
	return nil
}

func (m *mockUserRepo) EnableTOTP(_ context.Context, id uuid.UUID) error {
	if u, ok := m.users[id.String()]; ok {
		u.TOTPEnabled = true
	}
	return nil
}

type mockSessionRepo struct {
	sessions map[uuid.UUID]*UserSession
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[uuid.UUID]*UserSession)}
}

func (m *mockSessionRepo) Create(_ context.Context, params CreateSessionParams) (*UserSession, error) {
	sess := &UserSession{
		ID:               uuid.New(),
		UserID:           params.UserID,
		RefreshTokenHash: params.RefreshTokenHash,
		ExpiresAt:        params.ExpiresAt,
	}
	m.sessions[sess.ID] = sess
	return sess, nil
}

func (m *mockSessionRepo) RevokeSession(_ context.Context, sessionID uuid.UUID) error {
	if sess, ok := m.sessions[sessionID]; ok {
		now := time.Now()
		sess.RevokedAt = &now
	}
	return nil
}

func (m *mockSessionRepo) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, sess := range m.sessions {
		if sess.UserID == userID {
			sess.RevokedAt = &now
		}
	}
	return nil
}

func (m *mockSessionRepo) GetByID(_ context.Context, id uuid.UUID) (*UserSession, error) {
	sess, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return sess, nil
}

func (m *mockSessionRepo) GetActiveByUserID(_ context.Context, userID uuid.UUID) ([]UserSession, error) {
	var result []UserSession
	for _, sess := range m.sessions {
		if sess.UserID == userID && sess.RevokedAt == nil && sess.ExpiresAt.After(time.Now()) {
			result = append(result, *sess)
		}
	}
	return result, nil
}

type mockAuditLogger struct {
	entries []string
}

func (m *mockAuditLogger) Log(_ context.Context, _ uuid.UUID, _ *uuid.UUID, action, _, _ string) {
	m.entries = append(m.entries, action)
}

func newTestService() (*Service, *mockUserRepo, *mockSessionRepo, *mockAuditLogger) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	audit := &mockAuditLogger{}
	svc := NewService(users, sessions, audit, Config{
		JWTSecret:        testSecret,
		JWTExpiry:        15 * time.Minute,
		JWTRefreshExpiry: 168 * time.Hour,
		JWTIssuer:        "argus",
		BcryptCost:       bcrypt.MinCost,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
	})
	return svc, users, sessions, audit
}

func createTestUser(email, password string, totpEnabled bool) *User {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	u := &User{
		ID:               uuid.New(),
		TenantID:         uuid.New(),
		Email:            email,
		PasswordHash:     string(hash),
		Name:             "Test User",
		Role:             "tenant_admin",
		TOTPEnabled:      totpEnabled,
		State:            "active",
		FailedLoginCount: 0,
	}
	if totpEnabled {
		secret := "JBSWY3DPEHPK3PXP"
		u.TOTPSecret = &secret
	}
	return u
}

func TestLogin_ValidCredentials(t *testing.T) {
	svc, users, _, audit := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	result, lockInfo, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if lockInfo != nil {
		t.Fatal("Expected no lock info")
	}
	if result.Token == "" {
		t.Fatal("Expected non-empty token")
	}
	if result.RefreshToken == "" {
		t.Fatal("Expected non-empty refresh token")
	}
	if result.Requires2FA {
		t.Fatal("Expected requires_2fa=false")
	}
	if result.User.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", result.User.Email)
	}

	hasLogin := false
	for _, e := range audit.entries {
		if e == "login" {
			hasLogin = true
		}
	}
	if !hasLogin {
		t.Error("Expected 'login' audit entry")
	}
}

func TestLogin_InvalidPassword(t *testing.T) {
	svc, users, _, audit := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	_, _, err := svc.Login(context.Background(), "test@example.com", "wrongpassword", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Expected ErrInvalidCredentials, got: %v", err)
	}

	hasFailedLogin := false
	for _, e := range audit.entries {
		if e == "failed_login" {
			hasFailedLogin = true
		}
	}
	if !hasFailedLogin {
		t.Error("Expected 'failed_login' audit entry")
	}
}

func TestLogin_InvalidEmail(t *testing.T) {
	svc, _, _, _ := newTestService()

	_, _, err := svc.Login(context.Background(), "nonexistent@example.com", "password", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestLogin_AccountLockout(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	for i := 0; i < 4; i++ {
		_, _, err := svc.Login(context.Background(), "test@example.com", "wrong", "127.0.0.1", "TestAgent")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Attempt %d: Expected ErrInvalidCredentials, got: %v", i+1, err)
		}
	}

	_, lockInfo, err := svc.Login(context.Background(), "test@example.com", "wrong", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("Expected ErrAccountLocked on 5th attempt, got: %v", err)
	}
	if lockInfo == nil {
		t.Fatal("Expected lock info")
	}
	if lockInfo.FailedAttempts != 5 {
		t.Errorf("Expected 5 failed attempts, got %d", lockInfo.FailedAttempts)
	}

	_, lockInfo2, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("Expected ErrAccountLocked even with correct password, got: %v", err)
	}
	if lockInfo2 == nil {
		t.Fatal("Expected lock info on locked account")
	}
}

func TestLogin_With2FA(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", true)
	users.addUser(user)

	result, _, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if !result.Requires2FA {
		t.Fatal("Expected requires_2fa=true")
	}
	if result.Token == "" {
		t.Fatal("Expected partial token")
	}
	if result.RefreshToken != "" {
		t.Fatal("Expected no refresh token for 2FA pending login")
	}

	claims, err := ValidateToken(result.Token, testSecret)
	if err != nil {
		t.Fatalf("Partial token validation failed: %v", err)
	}
	if !claims.Partial {
		t.Error("Expected partial=true in claims")
	}
}

func TestLogin_DisabledAccount(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	user.State = "disabled"
	users.addUser(user)

	_, _, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("Expected ErrAccountDisabled, got: %v", err)
	}
}

func TestRefresh_Valid(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	loginResult, _, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	refreshResult, err := svc.Refresh(context.Background(), loginResult.RefreshToken, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	if refreshResult.Token == "" {
		t.Fatal("Expected new token")
	}
	if refreshResult.RefreshToken == "" {
		t.Fatal("Expected new refresh token")
	}
	if refreshResult.RefreshToken == loginResult.RefreshToken {
		t.Error("Expected rotated refresh token (different from original)")
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	svc, _, _, _ := newTestService()

	_, err := svc.Refresh(context.Background(), "invalid-refresh-token", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalidRefreshTkn) {
		t.Fatalf("Expected ErrInvalidRefreshTkn, got: %v", err)
	}
}

func TestRefresh_RevokedToken(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	loginResult, _, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	_, err = svc.Refresh(context.Background(), loginResult.RefreshToken, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("First refresh failed: %v", err)
	}

	_, err = svc.Refresh(context.Background(), loginResult.RefreshToken, "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalidRefreshTkn) {
		t.Fatalf("Expected ErrInvalidRefreshTkn for reused token, got: %v", err)
	}
}

func TestLogout(t *testing.T) {
	svc, users, sessions, audit := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	loginResult, _, err := svc.Login(context.Background(), "test@example.com", "password123", "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	err = svc.Logout(context.Background(), user.ID, loginResult.RefreshToken)
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	sess, _ := sessions.GetByID(context.Background(), loginResult.SessionID)
	if sess != nil && sess.RevokedAt == nil {
		t.Error("Expected session to be revoked after logout")
	}

	hasLogout := false
	for _, e := range audit.entries {
		if e == "logout" {
			hasLogout = true
		}
	}
	if !hasLogout {
		t.Error("Expected 'logout' audit entry")
	}
}

func TestSetup2FA(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	result, err := svc.Setup2FA(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Setup2FA failed: %v", err)
	}

	if result.Secret == "" {
		t.Fatal("Expected non-empty TOTP secret")
	}
	if result.QRURI == "" {
		t.Fatal("Expected non-empty QR URI")
	}
}

func TestVerify2FA_InvalidCode(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	_, err := svc.Setup2FA(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Setup2FA failed: %v", err)
	}

	_, err = svc.Verify2FA(context.Background(), user.ID, "000000", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalid2FACode) {
		t.Fatalf("Expected ErrInvalid2FACode, got: %v", err)
	}
}

func TestVerify2FA_NoSecretSet(t *testing.T) {
	svc, users, _, _ := newTestService()
	user := createTestUser("test@example.com", "password123", false)
	user.TOTPSecret = nil
	users.addUser(user)

	_, err := svc.Verify2FA(context.Background(), user.ID, "123456", "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalid2FACode) {
		t.Fatalf("Expected ErrInvalid2FACode, got: %v", err)
	}
}
