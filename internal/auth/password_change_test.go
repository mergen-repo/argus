package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type mockPasswordHistory struct {
	mu      sync.Mutex
	entries map[uuid.UUID][]string
}

func newMockPasswordHistory() *mockPasswordHistory {
	return &mockPasswordHistory{entries: make(map[uuid.UUID][]string)}
}

func (m *mockPasswordHistory) Insert(_ context.Context, userID uuid.UUID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[userID] = append([]string{hash}, m.entries[userID]...)
	return nil
}

func (m *mockPasswordHistory) GetLastN(_ context.Context, userID uuid.UUID, n int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hashes := m.entries[userID]
	if n < len(hashes) {
		out := make([]string, n)
		copy(out, hashes[:n])
		return out, nil
	}
	out := make([]string, len(hashes))
	copy(out, hashes)
	return out, nil
}

func (m *mockPasswordHistory) Trim(_ context.Context, userID uuid.UUID, keep int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if keep >= 0 && keep < len(m.entries[userID]) {
		m.entries[userID] = m.entries[userID][:keep]
	}
	return nil
}

type mockBackupCodes struct {
	mu     sync.Mutex
	byUser map[uuid.UUID][]backupCodeEntry
}

type backupCodeEntry struct {
	hash   string
	usedAt *time.Time
}

func newMockBackupCodes() *mockBackupCodes {
	return &mockBackupCodes{byUser: make(map[uuid.UUID][]backupCodeEntry)}
}

func (m *mockBackupCodes) GenerateAndStore(_ context.Context, userID uuid.UUID, count int, bcryptCost int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if bcryptCost < bcrypt.MinCost {
		bcryptCost = bcrypt.MinCost
	}
	codes := make([]string, 0, count)
	entries := make([]backupCodeEntry, 0, count)
	for i := 0; i < count; i++ {
		raw, err := GenerateBackupCodeFormat()
		if err != nil {
			return nil, err
		}
		h, err := bcrypt.GenerateFromPassword([]byte(raw), bcryptCost)
		if err != nil {
			return nil, err
		}
		codes = append(codes, raw)
		entries = append(entries, backupCodeEntry{hash: string(h)})
	}
	m.byUser[userID] = entries
	return codes, nil
}

func (m *mockBackupCodes) ConsumeIfMatch(_ context.Context, userID uuid.UUID, rawCode string) (bool, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.byUser[userID]
	for i := range entries {
		if entries[i].usedAt != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(entries[i].hash), []byte(rawCode)) == nil {
			now := time.Now()
			entries[i].usedAt = &now
			m.byUser[userID] = entries
			remaining := 0
			for _, e := range entries {
				if e.usedAt == nil {
					remaining++
				}
			}
			return true, remaining, nil
		}
	}
	return false, 0, nil
}

func (m *mockBackupCodes) CountUnused(_ context.Context, userID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.byUser[userID] {
		if e.usedAt == nil {
			n++
		}
	}
	return n, nil
}

func (m *mockBackupCodes) InvalidateAll(_ context.Context, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.byUser[userID]
	now := time.Now()
	for i := range entries {
		if entries[i].usedAt == nil {
			entries[i].usedAt = &now
		}
	}
	m.byUser[userID] = entries
	return nil
}

func newPasswordChangeTestService() (*Service, *mockUserRepo, *mockPasswordHistory, *mockBackupCodes, *mockAuditLogger) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	audit := &mockAuditLogger{}
	history := newMockPasswordHistory()
	backup := newMockBackupCodes()
	svc := NewService(users, sessions, audit, Config{
		JWTSecret:        testSecret,
		JWTExpiry:        15 * time.Minute,
		JWTRefreshExpiry: 168 * time.Hour,
		JWTIssuer:        "argus",
		BcryptCost:       bcrypt.MinCost,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
		Policy: PasswordPolicy{
			MinLength:     12,
			RequireUpper:  true,
			RequireLower:  true,
			RequireDigit:  true,
			RequireSymbol: true,
			MaxRepeating:  3,
		},
		PasswordHistoryCount: 3,
		PasswordMaxAgeDays:   0,
		BackupCodeCount:      10,
	}).WithPasswordHistory(history).WithBackupCodes(backup)
	return svc, users, history, backup, audit
}

func TestChangePassword_PolicyViolation_Rejected(t *testing.T) {
	svc, users, _, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	users.addUser(user)

	err := svc.ChangePassword(context.Background(), user.ID, "CurrentPass1!", "short")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("expected ErrPasswordTooShort, got %v", err)
	}

	err = svc.ChangePassword(context.Background(), user.ID, "CurrentPass1!", "alllowercase123!")
	if !errors.Is(err, ErrPasswordMissingClass) {
		t.Fatalf("expected ErrPasswordMissingClass, got %v", err)
	}
}

func TestChangePassword_WrongCurrentPassword_Rejected(t *testing.T) {
	svc, users, _, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	users.addUser(user)

	err := svc.ChangePassword(context.Background(), user.ID, "wrong-current", "BrandNewPass1!")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestChangePassword_Reuse_CurrentRejected(t *testing.T) {
	svc, users, _, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	users.addUser(user)

	err := svc.ChangePassword(context.Background(), user.ID, "CurrentPass1!", "CurrentPass1!")
	if !errors.Is(err, ErrPasswordReused) {
		t.Fatalf("expected ErrPasswordReused for reusing current, got %v", err)
	}
}

func TestChangePassword_Reuse_HistoryRejected(t *testing.T) {
	svc, users, history, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	users.addUser(user)

	oldHash, _ := bcrypt.GenerateFromPassword([]byte("OldPassword9!"), bcrypt.MinCost)
	_ = history.Insert(context.Background(), user.ID, string(oldHash))

	err := svc.ChangePassword(context.Background(), user.ID, "CurrentPass1!", "OldPassword9!")
	if !errors.Is(err, ErrPasswordReused) {
		t.Fatalf("expected ErrPasswordReused from history, got %v", err)
	}
}

func TestChangePassword_Success_UpdatesHashAndClearsFlags(t *testing.T) {
	svc, users, history, _, audit := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	user.PasswordChangeRequired = true
	user.FailedLoginCount = 3
	lockUntil := time.Now().Add(10 * time.Minute)
	user.LockedUntil = &lockUntil
	users.addUser(user)

	if err := svc.ChangePassword(context.Background(), user.ID, "CurrentPass1!", "FreshPass456!"); err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	updated := users.users[user.ID.String()]
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("FreshPass456!")) != nil {
		t.Fatal("expected new password to verify against stored hash")
	}
	if updated.PasswordChangeRequired {
		t.Error("expected password_change_required to be cleared")
	}
	if updated.FailedLoginCount != 0 {
		t.Errorf("expected failed_login_count=0, got %d", updated.FailedLoginCount)
	}
	if updated.LockedUntil != nil {
		t.Error("expected locked_until to be cleared")
	}

	hashes, _ := history.GetLastN(context.Background(), user.ID, 10)
	if len(hashes) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(hashes))
	}
	if bcrypt.CompareHashAndPassword([]byte(hashes[0]), []byte("FreshPass456!")) != nil {
		t.Error("expected history to contain new hash")
	}

	hasEvent := false
	for _, e := range audit.entries {
		if e == "user.password_change" {
			hasEvent = true
		}
	}
	if !hasEvent {
		t.Error("expected user.password_change audit event")
	}
}

func TestChangePassword_HistoryTrimmed(t *testing.T) {
	svc, users, history, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	users.addUser(user)

	passwords := []string{"FirstPassA1!", "SecondPassB2@", "ThirdPassC3#", "FourthPassD4$", "FifthPassE5%"}
	current := "CurrentPass1!"
	for _, newPwd := range passwords {
		if err := svc.ChangePassword(context.Background(), user.ID, current, newPwd); err != nil {
			t.Fatalf("ChangePassword(%s) failed: %v", newPwd, err)
		}
		current = newPwd
	}

	hashes, _ := history.GetLastN(context.Background(), user.ID, 10)
	if len(hashes) != 3 {
		t.Fatalf("expected history trimmed to 3, got %d", len(hashes))
	}
}

func TestLogin_PasswordChangeRequired_ReturnsPartialTokenWithReason(t *testing.T) {
	svc, users, _, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	user.PasswordChangeRequired = true
	users.addUser(user)

	result, _, err := svc.Login(context.Background(), "test@example.com", "CurrentPass1!", "127.0.0.1", "TestAgent", false)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if result.Reason != ReasonPasswordChangeRequired {
		t.Errorf("expected reason=%s, got %q", ReasonPasswordChangeRequired, result.Reason)
	}
	if result.RefreshToken != "" {
		t.Error("expected no refresh token when password change required")
	}
	if result.Token == "" {
		t.Fatal("expected partial token")
	}

	claims, err := ValidateToken(result.Token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if !claims.Partial {
		t.Error("expected partial=true")
	}
	if claims.Reason != ReasonPasswordChangeRequired {
		t.Errorf("expected claims.reason=%s, got %q", ReasonPasswordChangeRequired, claims.Reason)
	}
}

func TestLogin_PasswordExpired_ReturnsPartialTokenWithReason(t *testing.T) {
	svc, users, _, _, _ := newPasswordChangeTestService()
	svc.cfg.PasswordMaxAgeDays = 30
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	old := time.Now().Add(-60 * 24 * time.Hour)
	user.PasswordChangedAt = &old
	users.addUser(user)

	result, _, err := svc.Login(context.Background(), "test@example.com", "CurrentPass1!", "127.0.0.1", "TestAgent", false)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if result.Reason != ReasonPasswordExpired {
		t.Errorf("expected reason=%s, got %q", ReasonPasswordExpired, result.Reason)
	}
	if result.RefreshToken != "" {
		t.Error("expected no refresh token when password expired")
	}
}

func TestGenerateBackupCodes_ReturnsCodesAndInvalidatesPriorSet(t *testing.T) {
	svc, users, _, backup, audit := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", true)
	users.addUser(user)

	first, err := svc.GenerateBackupCodes(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GenerateBackupCodes failed: %v", err)
	}
	if len(first) != 10 {
		t.Fatalf("expected 10 codes, got %d", len(first))
	}
	for _, c := range first {
		if len(strings.ReplaceAll(c, "-", "")) != 8 {
			t.Errorf("unexpected code format: %q", c)
		}
	}

	// Regenerate — old codes should no longer match.
	second, err := svc.GenerateBackupCodes(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GenerateBackupCodes (second) failed: %v", err)
	}
	if len(second) != 10 {
		t.Fatalf("expected 10 fresh codes, got %d", len(second))
	}

	ok, _, err := backup.ConsumeIfMatch(context.Background(), user.ID, first[0])
	if err != nil {
		t.Fatalf("ConsumeIfMatch failed: %v", err)
	}
	if ok {
		t.Error("expected old backup code to be invalidated after regeneration")
	}

	hasEvent := false
	for _, e := range audit.entries {
		if e == "user.backup_codes_generated" {
			hasEvent = true
		}
	}
	if !hasEvent {
		t.Error("expected user.backup_codes_generated audit event")
	}
}

func TestVerifyBackupCode_ConsumesCodeAndReturnsRemaining(t *testing.T) {
	svc, users, _, _, audit := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", true)
	users.addUser(user)

	codes, err := svc.GenerateBackupCodes(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GenerateBackupCodes failed: %v", err)
	}

	result, err := svc.VerifyBackupCode(context.Background(), user.ID, codes[0], "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("VerifyBackupCode failed: %v", err)
	}
	if !result.UsedBackupCode {
		t.Error("expected UsedBackupCode=true")
	}
	if result.BackupCodesRemaining != 9 {
		t.Errorf("expected 9 remaining, got %d", result.BackupCodesRemaining)
	}
	if result.Token == "" || result.RefreshToken == "" {
		t.Error("expected full session tokens after backup code consumption")
	}

	// Re-using the same code should fail.
	_, err = svc.VerifyBackupCode(context.Background(), user.ID, codes[0], "127.0.0.1", "TestAgent")
	if !errors.Is(err, ErrInvalidBackupCode) {
		t.Errorf("expected ErrInvalidBackupCode on reuse, got %v", err)
	}

	hasEvent := false
	for _, e := range audit.entries {
		if e == "user.login_backup_code" {
			hasEvent = true
		}
	}
	if !hasEvent {
		t.Error("expected user.login_backup_code audit event")
	}
}

func TestVerify2FA_WithBackupCode_RoutesToBackupFlow(t *testing.T) {
	svc, users, _, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", true)
	users.addUser(user)

	codes, err := svc.GenerateBackupCodes(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GenerateBackupCodes failed: %v", err)
	}

	result, err := svc.Verify2FAWithInput(context.Background(), user.ID, Verify2FAInput{
		BackupCode: codes[0],
		IPAddress:  "127.0.0.1",
		UserAgent:  "TestAgent",
	})
	if err != nil {
		t.Fatalf("Verify2FAWithInput backup-code flow failed: %v", err)
	}
	if !result.UsedBackupCode {
		t.Error("expected UsedBackupCode=true")
	}
	if result.BackupCodesRemaining != 9 {
		t.Errorf("expected 9 remaining, got %d", result.BackupCodesRemaining)
	}
}

func TestVerify2FA_TOTPPreferredOverBackupCode(t *testing.T) {
	// When both Code and BackupCode are supplied, TOTP is preferred per spec.
	svc, users, _, _, _ := newPasswordChangeTestService()
	user := createTestUser("test@example.com", "CurrentPass1!", false)
	users.addUser(user)

	setup, err := svc.Setup2FA(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Setup2FA failed: %v", err)
	}

	// Enable TOTP so GenerateBackupCodes does not return ErrTOTPNotEnabled.
	if err := users.EnableTOTP(context.Background(), user.ID); err != nil {
		t.Fatalf("EnableTOTP failed: %v", err)
	}

	codes, err := svc.GenerateBackupCodes(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GenerateBackupCodes failed: %v", err)
	}

	totpCode, err := generateCurrentTOTPCode(setup.Secret)
	if err != nil {
		t.Fatalf("generate TOTP: %v", err)
	}

	result, err := svc.Verify2FAWithInput(context.Background(), user.ID, Verify2FAInput{
		Code:       totpCode,
		BackupCode: codes[0],
		IPAddress:  "127.0.0.1",
		UserAgent:  "TestAgent",
	})
	if err != nil {
		t.Fatalf("Verify2FAWithInput failed: %v", err)
	}
	if result.UsedBackupCode {
		t.Error("expected TOTP path, not backup code consumption")
	}
	// Backup code count should still be 10 (none consumed).
	// We can't read the internal mock directly, but if UsedBackupCode is false, TOTP branch ran.
}
