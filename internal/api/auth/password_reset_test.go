package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/audit"
	authpkg "github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// ─── DB pool helper ──────────────────────────────────────────────────────────

func testPRDBPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// ─── fake email sender ────────────────────────────────────────────────────────

type fakeEmailSender struct {
	to      string
	subject string
	text    string
	html    string
	callErr error
}

func (f *fakeEmailSender) SendTo(_ context.Context, to, subject, textBody, htmlBody string) error {
	f.to = to
	f.subject = subject
	f.text = textBody
	f.html = htmlBody
	return f.callErr
}

// ─── stub audit service ───────────────────────────────────────────────────────

type auditCounterSvc struct {
	count int
}

func (a *auditCounterSvc) CreateEntry(_ context.Context, _ audit.CreateEntryParams) (*audit.Entry, error) {
	a.count++
	return &audit.Entry{}, nil
}

// ─── seed helpers ─────────────────────────────────────────────────────────────

func prSeedTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tenants (id, name, contact_email) VALUES ($1, $2, $3)`,
		id, "pr-test-"+id.String()[:8], "pr-test-"+id.String()[:8]+"@test.invalid",
	)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM users WHERE tenant_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func prSeedUser(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, email, password string) uuid.UUID {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, tenant_id, email, password_hash, name, role, state)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, tenantID, email, string(hash), "PR Test User", "tenant_admin", "active",
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM password_reset_tokens WHERE user_id = $1`, id)
		pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func prCleanupTokensByEmail(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	t.Cleanup(func() {
		pool.Exec(context.Background(),
			`DELETE FROM password_reset_tokens WHERE email_rate_key = $1`, strings.ToLower(email))
	})
}

// ─── handler builder helper ───────────────────────────────────────────────────

func newPRHandler(t *testing.T, pool *pgxpool.Pool, rateLimit int, emailSender *fakeEmailSender) *AuthHandler {
	t.Helper()
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
		Policy: authpkg.PasswordPolicy{
			MinLength:    8,
			MaxRepeating: 0,
		},
	})
	h := NewAuthHandler(svc, 24*time.Hour, false)

	prStore := store.NewPasswordResetStore(pool)
	h = h.WithPasswordReset(prStore, emailSender, rateLimit, 15*time.Minute, "http://localhost")
	h.userStore = store.NewUserStore(pool)
	return h
}

func prReqBody(email string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"email": email})
	return bytes.NewBuffer(b)
}

func prConfirmBody(token, password string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"token": token, "password": password})
	return bytes.NewBuffer(b)
}

// extractTokenFromEmail parses "token=" query param from the reset URL in the email body.
func extractTokenFromEmail(f *fakeEmailSender) string {
	for _, body := range []string{f.text, f.html} {
		idx := strings.Index(body, "token=")
		if idx < 0 {
			continue
		}
		rest := body[idx+6:]
		end := strings.IndexAny(rest, "\" \t\n&>")
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	return ""
}

// ─── 1. Existing email → 200 generic ─────────────────────────────────────────

func TestPasswordResetRequest_ExistingEmail_Returns200Generic(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "pr-exist-" + uuid.New().String()[:8] + "@test.invalid"
	prSeedUser(t, pool, tenantID, email, "TestPass99")
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 5, sender)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	w := httptest.NewRecorder()
	h.RequestPasswordReset(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "success" {
		t.Errorf("status = %v, want success", resp["status"])
	}
	data, _ := resp["data"].(map[string]interface{})
	if data == nil {
		t.Fatal("expected data field in response")
	}
	msg, _ := data["message"].(string)
	if msg != "If that email exists, a reset link has been sent." {
		t.Errorf("message = %q, want generic success message", msg)
	}
}

// ─── 2. Nonexistent email → 200 generic + dummyBcrypt called once ──────────

func TestPasswordResetRequest_NonexistentEmail_Returns200Generic_AndInvokesDummyBcrypt(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	email := "ghost-" + uuid.New().String()[:8] + "@test.invalid"
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 5, sender)

	hookCalled := 0
	h.dummyBcryptHook = func() { hookCalled++ }

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	w := httptest.NewRecorder()
	h.RequestPasswordReset(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "success" {
		t.Errorf("status = %v, want success", resp["status"])
	}

	if hookCalled != 1 {
		t.Errorf("dummyBcryptHook called %d times, want exactly 1 (DEV-324 deterministic verification)", hookCalled)
	}
}

// ─── 3. Body shape identical across real + ghost ──────────────────────────────

func TestPasswordResetRequest_BodyShape_IdenticalAcrossCases(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)

	realEmails := make([]string, 5)
	for i := range realEmails {
		email := "pr-real-" + uuid.New().String()[:8] + "@test.invalid"
		prSeedUser(t, pool, tenantID, email, "TestPass99")
		prCleanupTokensByEmail(t, pool, email)
		realEmails[i] = email
	}

	ghostEmails := make([]string, 5)
	for i := range ghostEmails {
		ghostEmails[i] = "ghost-" + uuid.New().String()[:8] + "@test.invalid"
	}

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 100, sender)

	allEmails := append(realEmails, ghostEmails...)
	bodies := make([][]byte, 0, len(allEmails))
	for _, email := range allEmails {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
		w := httptest.NewRecorder()
		h.RequestPasswordReset(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("email %q: status = %d, want 200", email, w.Code)
		}
		bodies = append(bodies, w.Body.Bytes())
	}

	ref := bodies[0]
	for i, b := range bodies[1:] {
		if !bytes.Equal(ref, b) {
			t.Errorf("body[%d] differs from body[0]: got %q, want %q", i+1, string(b), string(ref))
		}
	}
}

// ─── 4. Rate limit enforced (PAT-017 anchor) ──────────────────────────────────

func TestPasswordResetRequest_RateLimitEnforced(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	email := "ratelimit-" + uuid.New().String()[:8] + "@test.invalid"
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 5, sender)

	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
		w := httptest.NewRecorder()
		h.RequestPasswordReset(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Errorf("request %d: got 429 too early (rate limit = 5)", i)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	w := httptest.NewRecorder()
	h.RequestPasswordReset(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("request 6: got %d, want 429", w.Code)
	}
	code := getErrorCode(w.Body.String())
	if code != "RATE_LIMITED" {
		t.Errorf("error code = %q, want RATE_LIMITED", code)
	}
}

// ─── 5. 429 body is generic, not reset-specific ──────────────────────────────

func TestPasswordResetRequest_RateLimit429_BodyIsGenericNotResetSpecific(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	email := "ratelimit2-" + uuid.New().String()[:8] + "@test.invalid"
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 5, sender)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
		w := httptest.NewRecorder()
		h.RequestPasswordReset(w, req)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	w := httptest.NewRecorder()
	h.RequestPasswordReset(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	body := w.Body.String()
	lbody := strings.ToLower(body)
	for _, forbidden := range []string{"password", "reset", email} {
		if strings.Contains(lbody, strings.ToLower(forbidden)) {
			t.Errorf("429 body leaks sensitive info (%q found in body): %s", forbidden, body)
		}
	}
}

// ─── 6. Email dispatch fails → still 200, audit logged ───────────────────────

func TestPasswordResetRequest_EmailDispatchFails_StillReturns200(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "mailerr-" + uuid.New().String()[:8] + "@test.invalid"
	prSeedUser(t, pool, tenantID, email, "TestPass99")
	prCleanupTokensByEmail(t, pool, email)

	failSender := &fakeEmailSender{callErr: errors.New("smtp unavailable")}
	h := newPRHandler(t, pool, 5, failSender)

	auditSvc := &auditCounterSvc{}
	h.auditSvc = auditSvc

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	w := httptest.NewRecorder()
	h.RequestPasswordReset(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even when email send fails; body: %s", w.Code, w.Body.String())
	}

	if auditSvc.count == 0 {
		t.Error("expected audit entry to be created even when email dispatch fails")
	}
}

// ─── 7. Valid token full round-trip ──────────────────────────────────────────

func TestPasswordResetConfirm_ValidToken_SucceedsAndInvalidatesToken(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "roundtrip-" + uuid.New().String()[:8] + "@test.invalid"
	userID := prSeedUser(t, pool, tenantID, email, "OldPassword99")
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 10, sender)

	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	wReset := httptest.NewRecorder()
	h.RequestPasswordReset(wReset, reqReset)
	if wReset.Code != http.StatusOK {
		t.Fatalf("RequestPasswordReset: status = %d; body: %s", wReset.Code, wReset.Body.String())
	}

	rawToken := extractTokenFromEmail(sender)
	if rawToken == "" {
		t.Fatal("could not extract token from fake email sender — check RenderPasswordResetEmail URL format")
	}

	newPwd := "NewValidPass99"
	reqConfirm := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(rawToken, newPwd))
	wConfirm := httptest.NewRecorder()
	h.ConfirmPasswordReset(wConfirm, reqConfirm)
	if wConfirm.Code != http.StatusOK {
		t.Fatalf("ConfirmPasswordReset: status = %d; body: %s", wConfirm.Code, wConfirm.Body.String())
	}

	var newHash string
	err := pool.QueryRow(context.Background(),
		`SELECT password_hash FROM users WHERE id = $1`, userID,
	).Scan(&newHash)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(newHash), []byte(newPwd)); err != nil {
		t.Error("password was not updated in DB after successful reset")
	}

	reqConfirm2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(rawToken, "AnotherPass99"))
	wConfirm2 := httptest.NewRecorder()
	h.ConfirmPasswordReset(wConfirm2, reqConfirm2)
	if wConfirm2.Code != http.StatusBadRequest {
		t.Errorf("second confirm: status = %d, want 400 (token should be invalidated)", wConfirm2.Code)
	}
	code := getErrorCode(wConfirm2.Body.String())
	if code != "PASSWORD_RESET_INVALID_TOKEN" {
		t.Errorf("second confirm error code = %q, want PASSWORD_RESET_INVALID_TOKEN", code)
	}
}

// ─── 8. Expired token → 400 ───────────────────────────────────────────────────

func TestPasswordResetConfirm_ExpiredToken_Returns400(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "expired-" + uuid.New().String()[:8] + "@test.invalid"
	userID := prSeedUser(t, pool, tenantID, email, "TestPass99")
	prCleanupTokensByEmail(t, pool, email)

	var rawTokBytes [32]byte
	if _, err := rand.Read(rawTokBytes[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	tokHash := sha256.Sum256(rawTokBytes[:])
	expiredAt := time.Now().Add(-1 * time.Minute)

	_, err := pool.Exec(context.Background(),
		`INSERT INTO password_reset_tokens (user_id, token_hash, email_rate_key, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		userID, tokHash[:], email, expiredAt,
	)
	if err != nil {
		t.Fatalf("insert expired token: %v", err)
	}

	rawToken := base64.RawURLEncoding.EncodeToString(rawTokBytes[:])

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 10, sender)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(rawToken, "NewPass99"))
	w := httptest.NewRecorder()
	h.ConfirmPasswordReset(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for expired token; body: %s", w.Code, w.Body.String())
	}
	code := getErrorCode(w.Body.String())
	if code != "PASSWORD_RESET_INVALID_TOKEN" {
		t.Errorf("error code = %q, want PASSWORD_RESET_INVALID_TOKEN", code)
	}
}

// ─── 9. Reused token → 400 (single-use, DEV-323) ─────────────────────────────

func TestPasswordResetConfirm_ReusedToken_Returns400(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "reuse-" + uuid.New().String()[:8] + "@test.invalid"
	prSeedUser(t, pool, tenantID, email, "TestPass99")
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 10, sender)

	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	wReset := httptest.NewRecorder()
	h.RequestPasswordReset(wReset, reqReset)
	if wReset.Code != http.StatusOK {
		t.Fatalf("RequestPasswordReset: status = %d", wReset.Code)
	}

	rawToken := extractTokenFromEmail(sender)
	if rawToken == "" {
		t.Fatal("could not extract token")
	}

	reqC1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(rawToken, "FirstNew99"))
	wC1 := httptest.NewRecorder()
	h.ConfirmPasswordReset(wC1, reqC1)
	if wC1.Code != http.StatusOK {
		t.Fatalf("first confirm: status = %d; body: %s", wC1.Code, wC1.Body.String())
	}

	reqC2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(rawToken, "SecondNew99"))
	wC2 := httptest.NewRecorder()
	h.ConfirmPasswordReset(wC2, reqC2)

	if wC2.Code != http.StatusBadRequest {
		t.Errorf("second confirm: status = %d, want 400 (token single-use DEV-323)", wC2.Code)
	}
	code := getErrorCode(wC2.Body.String())
	if code != "PASSWORD_RESET_INVALID_TOKEN" {
		t.Errorf("error code = %q, want PASSWORD_RESET_INVALID_TOKEN", code)
	}
}

// ─── 10. Invalid policy → 422 PASSWORD_TOO_SHORT ─────────────────────────────

func TestPasswordResetConfirm_InvalidPolicy_Returns422(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "policy-" + uuid.New().String()[:8] + "@test.invalid"
	prSeedUser(t, pool, tenantID, email, "TestPass99")
	prCleanupTokensByEmail(t, pool, email)

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 10, sender)

	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset", prReqBody(email))
	wReset := httptest.NewRecorder()
	h.RequestPasswordReset(wReset, reqReset)
	if wReset.Code != http.StatusOK {
		t.Fatalf("RequestPasswordReset: status = %d", wReset.Code)
	}

	rawToken := extractTokenFromEmail(sender)
	if rawToken == "" {
		t.Fatal("could not extract token")
	}

	reqConfirm := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(rawToken, "ab"))
	wConfirm := httptest.NewRecorder()
	h.ConfirmPasswordReset(wConfirm, reqConfirm)

	if wConfirm.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422 for short password; body: %s", wConfirm.Code, wConfirm.Body.String())
	}
	code := getErrorCode(wConfirm.Body.String())
	if code != "PASSWORD_TOO_SHORT" {
		t.Errorf("error code = %q, want PASSWORD_TOO_SHORT", code)
	}
}

// ─── 11. Success invalidates ALL user tokens (DEV-331) ───────────────────────

func TestPasswordResetConfirm_SuccessInvalidatesAllUserTokens(t *testing.T) {
	pool := testPRDBPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set — skipping DB-gated password reset test")
	}

	tenantID := prSeedTenant(t, pool)
	email := "allinv-" + uuid.New().String()[:8] + "@test.invalid"
	userID := prSeedUser(t, pool, tenantID, email, "TestPass99")
	prCleanupTokensByEmail(t, pool, email)

	prStore := store.NewPasswordResetStore(pool)

	var tok1Bytes, tok2Bytes [32]byte
	if _, err := rand.Read(tok1Bytes[:]); err != nil {
		t.Fatalf("rand.Read tok1: %v", err)
	}
	if _, err := rand.Read(tok2Bytes[:]); err != nil {
		t.Fatalf("rand.Read tok2: %v", err)
	}

	tok1Hash := sha256.Sum256(tok1Bytes[:])
	tok2Hash := sha256.Sum256(tok2Bytes[:])
	future := time.Now().Add(15 * time.Minute)

	if err := prStore.Create(context.Background(), userID, tok1Hash, email, future); err != nil {
		t.Fatalf("Create tok1: %v", err)
	}
	if err := prStore.Create(context.Background(), userID, tok2Hash, email, future); err != nil {
		t.Fatalf("Create tok2: %v", err)
	}

	sender := &fakeEmailSender{}
	h := newPRHandler(t, pool, 100, sender)

	tok1B64 := base64.RawURLEncoding.EncodeToString(tok1Bytes[:])
	reqConfirm := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password/reset/confirm", prConfirmBody(tok1B64, "NewValidPass99"))
	wConfirm := httptest.NewRecorder()
	h.ConfirmPasswordReset(wConfirm, reqConfirm)
	if wConfirm.Code != http.StatusOK {
		t.Fatalf("ConfirmPasswordReset: status = %d; body: %s", wConfirm.Code, wConfirm.Body.String())
	}

	var remaining int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = $1`, userID,
	).Scan(&remaining)
	if err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if remaining != 0 {
		t.Errorf("expected 0 tokens after confirm, got %d (DEV-331: all tokens must be invalidated)", remaining)
	}
}
