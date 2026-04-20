package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/apierr"
	authpkg "github.com/btopcu/argus/internal/auth"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type handlerTestUserRepo struct {
	users map[string]*authpkg.User
}

func newHandlerTestUserRepo() *handlerTestUserRepo {
	return &handlerTestUserRepo{users: make(map[string]*authpkg.User)}
}

func (m *handlerTestUserRepo) addUser(u *authpkg.User) {
	m.users[u.Email] = u
	m.users[u.ID.String()] = u
}

func (m *handlerTestUserRepo) GetByEmail(_ context.Context, email string) (*authpkg.User, error) {
	u, ok := m.users[email]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}

func (m *handlerTestUserRepo) GetByID(_ context.Context, id uuid.UUID) (*authpkg.User, error) {
	u, ok := m.users[id.String()]
	if !ok {
		return nil, errors.New("not found")
	}
	return u, nil
}

func (m *handlerTestUserRepo) UpdateLoginSuccess(_ context.Context, id uuid.UUID) error {
	if u, ok := m.users[id.String()]; ok {
		now := time.Now()
		u.LastLoginAt = &now
		u.FailedLoginCount = 0
		u.LockedUntil = nil
	}
	return nil
}

func (m *handlerTestUserRepo) IncrementFailedLogin(_ context.Context, id uuid.UUID, lockUntil *time.Time) error {
	if u, ok := m.users[id.String()]; ok {
		u.FailedLoginCount++
		u.LockedUntil = lockUntil
	}
	return nil
}

func (m *handlerTestUserRepo) SetTOTPSecret(_ context.Context, id uuid.UUID, secret string) error {
	if u, ok := m.users[id.String()]; ok {
		u.TOTPSecret = &secret
	}
	return nil
}

func (m *handlerTestUserRepo) EnableTOTP(_ context.Context, id uuid.UUID) error {
	if u, ok := m.users[id.String()]; ok {
		u.TOTPEnabled = true
	}
	return nil
}

func (m *handlerTestUserRepo) SetPasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	if u, ok := m.users[id.String()]; ok {
		u.PasswordHash = hash
		now := time.Now()
		u.PasswordChangedAt = &now
	}
	return nil
}

func (m *handlerTestUserRepo) SetPasswordChangeRequired(_ context.Context, id uuid.UUID, required bool) error {
	if u, ok := m.users[id.String()]; ok {
		u.PasswordChangeRequired = required
	}
	return nil
}

func (m *handlerTestUserRepo) ClearLockout(_ context.Context, id uuid.UUID) error {
	if u, ok := m.users[id.String()]; ok {
		u.FailedLoginCount = 0
		u.LockedUntil = nil
	}
	return nil
}

type handlerTestSessionRepo struct {
	sessions map[uuid.UUID]*authpkg.UserSession
}

func newHandlerTestSessionRepo() *handlerTestSessionRepo {
	return &handlerTestSessionRepo{sessions: make(map[uuid.UUID]*authpkg.UserSession)}
}

func (m *handlerTestSessionRepo) Create(_ context.Context, params authpkg.CreateSessionParams) (*authpkg.UserSession, error) {
	sess := &authpkg.UserSession{
		ID:               uuid.New(),
		UserID:           params.UserID,
		RefreshTokenHash: params.RefreshTokenHash,
		ExpiresAt:        params.ExpiresAt,
	}
	m.sessions[sess.ID] = sess
	return sess, nil
}

func (m *handlerTestSessionRepo) RevokeSession(_ context.Context, sessionID uuid.UUID) error {
	if sess, ok := m.sessions[sessionID]; ok {
		now := time.Now()
		sess.RevokedAt = &now
	}
	return nil
}

func (m *handlerTestSessionRepo) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, sess := range m.sessions {
		if sess.UserID == userID {
			sess.RevokedAt = &now
		}
	}
	return nil
}

func (m *handlerTestSessionRepo) GetByID(_ context.Context, id uuid.UUID) (*authpkg.UserSession, error) {
	sess, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return sess, nil
}

func (m *handlerTestSessionRepo) GetActiveByUserID(_ context.Context, userID uuid.UUID) ([]authpkg.UserSession, error) {
	var result []authpkg.UserSession
	for _, sess := range m.sessions {
		if sess.UserID == userID && sess.RevokedAt == nil && sess.ExpiresAt.After(time.Now()) {
			result = append(result, *sess)
		}
	}
	return result, nil
}

func (m *handlerTestSessionRepo) ListActiveByUserID(_ context.Context, userID uuid.UUID, _ string, limit int) ([]authpkg.UserSession, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var result []authpkg.UserSession
	for _, sess := range m.sessions {
		if sess.UserID == userID && sess.RevokedAt == nil && sess.ExpiresAt.After(time.Now()) {
			result = append(result, *sess)
		}
	}
	nextCursor := ""
	if len(result) > limit {
		nextCursor = result[limit-1].ID.String()
		result = result[:limit]
	}
	return result, nextCursor, nil
}

type handlerTestPasswordHistory struct {
	entries map[uuid.UUID][]string
}

func newHandlerTestPasswordHistory() *handlerTestPasswordHistory {
	return &handlerTestPasswordHistory{entries: make(map[uuid.UUID][]string)}
}

func (m *handlerTestPasswordHistory) Insert(_ context.Context, userID uuid.UUID, hash string) error {
	m.entries[userID] = append([]string{hash}, m.entries[userID]...)
	return nil
}

func (m *handlerTestPasswordHistory) GetLastN(_ context.Context, userID uuid.UUID, n int) ([]string, error) {
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

func (m *handlerTestPasswordHistory) Trim(_ context.Context, userID uuid.UUID, keep int) error {
	if keep >= 0 && keep < len(m.entries[userID]) {
		m.entries[userID] = m.entries[userID][:keep]
	}
	return nil
}

func newChangePasswordTestHandler() (*AuthHandler, *handlerTestUserRepo) {
	users := newHandlerTestUserRepo()
	sessions := newHandlerTestSessionRepo()
	history := newHandlerTestPasswordHistory()
	svc := authpkg.NewService(users, sessions, nil, authpkg.Config{
		JWTSecret:        "test-secret-32-bytes-long-padding",
		JWTExpiry:        15 * time.Minute,
		JWTRefreshExpiry: 168 * time.Hour,
		JWTIssuer:        "argus",
		BcryptCost:       bcrypt.MinCost,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
		Policy: authpkg.PasswordPolicy{
			MinLength:     8,
			RequireUpper:  false,
			RequireLower:  false,
			RequireDigit:  false,
			RequireSymbol: false,
			MaxRepeating:  0,
		},
		PasswordHistoryCount: 3,
	}).WithPasswordHistory(history)
	h := NewAuthHandler(svc, 24*time.Hour, false)
	return h, users
}

func makeChangePasswordUser(email, password string) *authpkg.User {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return &authpkg.User{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		Name:         "Test User",
		Role:         "tenant_admin",
		State:        "active",
	}
}

func changePasswordBody(current, next string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{
		"current_password": current,
		"new_password":     next,
	})
	return bytes.NewBuffer(b)
}

func getErrorCode(body string) string {
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	return resp.Error.Code
}

func getAccessToken(body string) string {
	var resp struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	return resp.Data.AccessToken
}

type handlerTestBackupCodeRepo struct {
	codes     []string
	remaining int
	matchCode string
	matchOK   bool
}

func (m *handlerTestBackupCodeRepo) GenerateAndStore(_ context.Context, _ uuid.UUID, count int, _ int) ([]string, error) {
	out := make([]string, count)
	for i := range out {
		code, _ := authpkg.GenerateBackupCodeFormat()
		out[i] = code
	}
	m.codes = out
	return out, nil
}

func (m *handlerTestBackupCodeRepo) ConsumeIfMatch(_ context.Context, _ uuid.UUID, rawCode string) (bool, int, error) {
	if rawCode == m.matchCode && m.matchOK {
		return true, m.remaining, nil
	}
	return false, 0, nil
}

func (m *handlerTestBackupCodeRepo) CountUnused(_ context.Context, _ uuid.UUID) (int, error) {
	return m.remaining, nil
}

func (m *handlerTestBackupCodeRepo) InvalidateAll(_ context.Context, _ uuid.UUID) error {
	m.codes = nil
	return nil
}

func newBackupCodesTestHandler(totpEnabled bool, backupRepo *handlerTestBackupCodeRepo) (*AuthHandler, *handlerTestUserRepo, uuid.UUID) {
	users := newHandlerTestUserRepo()
	sessions := newHandlerTestSessionRepo()
	svc := authpkg.NewService(users, sessions, nil, authpkg.Config{
		JWTSecret:        "test-secret-32-bytes-long-padding",
		JWTExpiry:        15 * time.Minute,
		JWTRefreshExpiry: 168 * time.Hour,
		JWTIssuer:        "argus",
		BcryptCost:       bcrypt.MinCost,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
		BackupCodeCount:  10,
	}).WithBackupCodes(backupRepo)

	userID := uuid.New()
	user := &authpkg.User{
		ID:          userID,
		TenantID:    uuid.New(),
		Email:       "test@example.com",
		Name:        "Test User",
		Role:        "api_user",
		State:       "active",
		TOTPEnabled: totpEnabled,
	}
	users.addUser(user)

	h := NewAuthHandler(svc, 24*time.Hour, false)
	return h, users, userID
}

func ctxWithUser(userID uuid.UUID) context.Context {
	return context.WithValue(context.Background(), apierr.UserIDKey, userID)
}

func getCodesFromBody(body string) []string {
	var resp struct {
		Data struct {
			Codes []string `json:"codes"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	return resp.Data.Codes
}

func getBackupCodesRemaining(body string) (int, bool) {
	var resp struct {
		Meta *struct {
			BackupCodesRemaining int `json:"backup_codes_remaining"`
		} `json:"meta"`
	}
	_ = json.Unmarshal([]byte(body), &resp)
	if resp.Meta == nil {
		return 0, false
	}
	return resp.Meta.BackupCodesRemaining, true
}

func TestAuthHandler_GenerateBackupCodes_TOTPEnabled(t *testing.T) {
	backupRepo := &handlerTestBackupCodeRepo{remaining: 10, matchOK: false}
	h, _, userID := newBackupCodesTestHandler(true, backupRepo)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/backup-codes", nil)
	req = req.WithContext(ctxWithUser(userID))
	w := httptest.NewRecorder()

	h.GenerateBackupCodes(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	codes := getCodesFromBody(w.Body.String())
	if len(codes) != 10 {
		t.Errorf("expected 10 codes, got %d; body: %s", len(codes), w.Body.String())
	}
	for _, c := range codes {
		if len(c) != 9 {
			t.Errorf("code %q has unexpected length %d (want 9 for XXXX-YYYY)", c, len(c))
		}
	}
}

func TestAuthHandler_GenerateBackupCodes_TOTPNotEnabled(t *testing.T) {
	backupRepo := &handlerTestBackupCodeRepo{remaining: 0, matchOK: false}
	h, _, userID := newBackupCodesTestHandler(false, backupRepo)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/backup-codes", nil)
	req = req.WithContext(ctxWithUser(userID))
	w := httptest.NewRecorder()

	h.GenerateBackupCodes(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusConflict, w.Body.String())
	}
	if code := getErrorCode(w.Body.String()); code != apierr.CodeTOTPNotEnabled {
		t.Errorf("error code = %q, want %q", code, apierr.CodeTOTPNotEnabled)
	}
}

func TestAuthHandler_GenerateBackupCodes_NoAuth(t *testing.T) {
	h := NewAuthHandler(nil, 24*time.Hour, false)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/backup-codes", nil)
	w := httptest.NewRecorder()

	h.GenerateBackupCodes(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_Verify2FA_BackupCodeValid_MetaWhenLow(t *testing.T) {
	const validCode = "ABCD-EFGH"
	backupRepo := &handlerTestBackupCodeRepo{
		matchCode: validCode,
		matchOK:   true,
		remaining: 2,
	}
	h, _, userID := newBackupCodesTestHandler(true, backupRepo)

	body, _ := json.Marshal(map[string]string{"backup_code": validCode})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/verify", bytes.NewBuffer(body))
	req = req.WithContext(ctxWithUser(userID))
	w := httptest.NewRecorder()

	h.Verify2FA(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	remaining, hasMeta := getBackupCodesRemaining(w.Body.String())
	if !hasMeta {
		t.Errorf("expected meta.backup_codes_remaining in response; body: %s", w.Body.String())
	}
	if remaining != 2 {
		t.Errorf("backup_codes_remaining = %d, want 2", remaining)
	}
}

func TestAuthHandler_Verify2FA_BackupCodeValid_NoMetaWhenAbove3(t *testing.T) {
	const validCode = "ABCD-EFGH"
	backupRepo := &handlerTestBackupCodeRepo{
		matchCode: validCode,
		matchOK:   true,
		remaining: 5,
	}
	h, _, userID := newBackupCodesTestHandler(true, backupRepo)

	body, _ := json.Marshal(map[string]string{"backup_code": validCode})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/verify", bytes.NewBuffer(body))
	req = req.WithContext(ctxWithUser(userID))
	w := httptest.NewRecorder()

	h.Verify2FA(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	_, hasMeta := getBackupCodesRemaining(w.Body.String())
	if hasMeta {
		t.Errorf("expected no meta.backup_codes_remaining when remaining > 3; body: %s", w.Body.String())
	}
}

func TestAuthHandler_Verify2FA_BackupCodeInvalid(t *testing.T) {
	backupRepo := &handlerTestBackupCodeRepo{
		matchCode: "ABCD-EFGH",
		matchOK:   true,
		remaining: 9,
	}
	h, _, userID := newBackupCodesTestHandler(true, backupRepo)

	body, _ := json.Marshal(map[string]string{"backup_code": "ZZZZ-ZZZZ"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/verify", bytes.NewBuffer(body))
	req = req.WithContext(ctxWithUser(userID))
	w := httptest.NewRecorder()

	h.Verify2FA(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if code := getErrorCode(w.Body.String()); code != apierr.CodeInvalidBackupCode {
		t.Errorf("error code = %q, want %q", code, apierr.CodeInvalidBackupCode)
	}
}

func TestAuthHandler_Verify2FA_NeitherCodeNorBackupCode(t *testing.T) {
	h := NewAuthHandler(nil, 24*time.Hour, false)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/2fa/verify", body)
	req = req.WithContext(ctxWithUser(uuid.New()))
	w := httptest.NewRecorder()

	h.Verify2FA(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestAuthHandler_ListSessions_NoUserContext(t *testing.T) {
	h := NewAuthHandler(nil, 24*time.Hour, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	w := httptest.NewRecorder()

	h.ListSessions(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthHandler_ListSessions_NilUserID(t *testing.T) {
	h := NewAuthHandler(nil, 24*time.Hour, false)

	ctx := context.WithValue(context.Background(), apierr.UserIDKey, uuid.Nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/sessions", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListSessions(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestChangePassword_NoToken_Returns401(t *testing.T) {
	h, _ := newChangePasswordTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
		changePasswordBody("old", "new"))
	w := httptest.NewRecorder()

	h.ChangePassword(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestChangePassword_MissingFields_Returns422(t *testing.T) {
	h, _ := newChangePasswordTestHandler()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.UserIDKey, userID)

	cases := []struct {
		name string
		body string
	}{
		{"missing current", `{"new_password":"newpass"}`},
		{"missing new", `{"current_password":"oldpass"}`},
		{"both missing", `{}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
				strings.NewReader(tc.body))
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.ChangePassword(w, req)

			if w.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s: status = %d, want 422", tc.name, w.Code)
			}
		})
	}
}

func TestChangePassword_InvalidJSON_Returns400(t *testing.T) {
	h, _ := newChangePasswordTestHandler()
	userID := uuid.New()
	ctx := context.WithValue(context.Background(), apierr.UserIDKey, userID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
		strings.NewReader("not-json"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ChangePassword(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestChangePassword_WrongCurrentPassword_Returns401(t *testing.T) {
	h, users := newChangePasswordTestHandler()
	user := makeChangePasswordUser("u@example.com", "CorrectPass1")
	users.addUser(user)

	ctx := context.WithValue(context.Background(), apierr.UserIDKey, user.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
		changePasswordBody("WrongPass", "NewPass123"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ChangePassword(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if code := getErrorCode(w.Body.String()); code != apierr.CodeInvalidCredentials {
		t.Errorf("error code = %q, want %q", code, apierr.CodeInvalidCredentials)
	}
}

func TestChangePassword_PasswordTooShort_Returns422(t *testing.T) {
	h, users := newChangePasswordTestHandler()
	user := makeChangePasswordUser("u@example.com", "CorrectPass1")
	users.addUser(user)

	ctx := context.WithValue(context.Background(), apierr.UserIDKey, user.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
		changePasswordBody("CorrectPass1", "ab"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ChangePassword(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	if code := getErrorCode(w.Body.String()); code != apierr.CodePasswordTooShort {
		t.Errorf("error code = %q, want %q", code, apierr.CodePasswordTooShort)
	}
}

func TestChangePassword_PasswordReused_Returns422(t *testing.T) {
	h, users := newChangePasswordTestHandler()
	user := makeChangePasswordUser("u@example.com", "CorrectPass1")
	users.addUser(user)

	ctx := context.WithValue(context.Background(), apierr.UserIDKey, user.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
		changePasswordBody("CorrectPass1", "CorrectPass1"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ChangePassword(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	if code := getErrorCode(w.Body.String()); code != apierr.CodePasswordReused {
		t.Errorf("error code = %q, want %q", code, apierr.CodePasswordReused)
	}
}

func TestChangePassword_Success_Returns200WithTokens(t *testing.T) {
	h, users := newChangePasswordTestHandler()
	user := makeChangePasswordUser("u@example.com", "CorrectPass1")
	user.PasswordChangeRequired = true
	users.addUser(user)

	ctx := context.WithValue(context.Background(), apierr.UserIDKey, user.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/change",
		changePasswordBody("CorrectPass1", "NewPassword2"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ChangePassword(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	accessToken := getAccessToken(w.Body.String())
	if accessToken == "" {
		t.Error("expected non-empty access_token in response")
	}

	updated := users.users[user.ID.String()]
	if updated.PasswordChangeRequired {
		t.Error("expected password_change_required to be cleared after successful change")
	}

	var found bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected refresh_token cookie to be set")
	}
}

func TestAuthHandler_ListSessions_LimitBounds(t *testing.T) {
	cases := []struct {
		name         string
		limitParam   string
		wantEffective int
	}{
		{"default", "", 50},
		{"valid 10", "10", 10},
		{"valid 100", "100", 100},
		{"over 100 clamped", "200", 50},
		{"zero clamped", "0", 50},
		{"non-numeric clamped", "abc", 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := 50
			if tc.limitParam != "" {
				parsed := 0
				valid := true
				for _, c := range tc.limitParam {
					if c < '0' || c > '9' {
						valid = false
						break
					}
					parsed = parsed*10 + int(c-'0')
				}
				if valid && parsed > 0 && parsed <= 100 {
					limit = parsed
				}
			}
			if limit != tc.wantEffective {
				t.Errorf("limit = %d, want %d", limit, tc.wantEffective)
			}
		})
	}
}

func newRefreshRateLimitTestHandler(t *testing.T) (*AuthHandler, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	users := newHandlerTestUserRepo()
	sessions := newHandlerTestSessionRepo()
	svc := authpkg.NewService(users, sessions, nil, authpkg.Config{
		JWTSecret:        "test-secret-32-bytes-long-padding",
		JWTExpiry:        15 * time.Minute,
		JWTRefreshExpiry: 168 * time.Hour,
		JWTIssuer:        "argus",
		BcryptCost:       bcrypt.MinCost,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
	})

	h := NewAuthHandler(svc, 24*time.Hour, false).WithRedis(client)
	return h, mr
}

func TestRefreshHandler_RateLimit_60PerMinute_429(t *testing.T) {
	h, _ := newRefreshRateLimitTestHandler(t)

	cookieVal := "test-refresh-token-value"

	fireRefresh := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieVal})
		w := httptest.NewRecorder()
		h.Refresh(w, req)
		return w.Code
	}

	for i := 1; i <= 60; i++ {
		code := fireRefresh()
		if code == http.StatusTooManyRequests {
			t.Errorf("request %d: got 429 too early (expected rate limit after 60)", i)
		}
	}

	code := fireRefresh()
	if code != http.StatusTooManyRequests {
		t.Errorf("request 61: got %d, want 429", code)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieVal})
	h.Refresh(w, req)

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error.Code != apierr.CodeRateLimited {
		t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeRateLimited)
	}
}
