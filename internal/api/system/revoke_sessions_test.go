package system

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/auth"
	"github.com/btopcu/argus/internal/notification"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func noopLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

const revokeTestSecret = "revoke-sessions-test-secret-must-be-32ch"

// --- mocks ---

type mockTenantSessionRevoker struct {
	affectedUsers   int64
	sessionsRevoked int64
	err             error
	calledWith      uuid.UUID
}

func (m *mockTenantSessionRevoker) RevokeAllByTenant(_ context.Context, tenantID uuid.UUID) (int64, int64, error) {
	m.calledWith = tenantID
	return m.affectedUsers, m.sessionsRevoked, m.err
}

type mockTenantWSDropper struct {
	calledWith uuid.UUID
}

func (m *mockTenantWSDropper) DisconnectTenant(tenantID uuid.UUID) []uuid.UUID {
	m.calledWith = tenantID
	return nil
}

type mockTenantNotifier struct {
	called bool
	req    notification.NotifyRequest
}

func (m *mockTenantNotifier) Notify(_ context.Context, req notification.NotifyRequest) error {
	m.called = true
	m.req = req
	return nil
}

type mockAuditor struct {
	called bool
	params audit.CreateEntryParams
}

func (m *mockAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	m.called = true
	m.params = p
	return &audit.Entry{}, nil
}

// --- test helpers ---

func buildRevokeJWT(t *testing.T, secret string, tenantID uuid.UUID, role string) string {
	t.Helper()
	tok, err := auth.GenerateToken(secret, uuid.New(), tenantID, role, 15*time.Minute, false)
	if err != nil {
		t.Fatalf("buildRevokeJWT: %v", err)
	}
	return "Bearer " + tok
}

func buildRevokeRouter(secret string, h *RevokeSessionsHandler) *httptest.Server {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/system/revoke-all-sessions",
		injectRole(secret, "tenant_admin", http.HandlerFunc(h.RevokeAll)),
	)
	return httptest.NewServer(mux)
}

func doRevokeRequest(t *testing.T, srv *httptest.Server, token, queryString string) *http.Response {
	t.Helper()
	url := srv.URL + "/api/v1/system/revoke-all-sessions"
	if queryString != "" {
		url += "?" + queryString
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(""))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// --- tests ---

func TestRevokeSessionsHandler_SuperAdmin_Success(t *testing.T) {
	targetTenantID := uuid.New()

	revoker := &mockTenantSessionRevoker{affectedUsers: 5, sessionsRevoked: 12}
	dropper := &mockTenantWSDropper{}
	notifier := &mockTenantNotifier{}
	auditor := &mockAuditor{}

	h := NewRevokeSessionsHandler(revoker, dropper, notifier, auditor, noopLogger())
	srv := buildRevokeRouter(revokeTestSecret, h)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, uuid.New(), "super_admin")
	resp := doRevokeRequest(t, srv, tok, "tenant="+targetTenantID.String())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			AffectedUsers   int64 `json:"affected_users"`
			SessionsRevoked int64 `json:"sessions_revoked"`
			Notified        bool  `json:"notified"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Status != "success" {
		t.Errorf("status = %q, want success", body.Status)
	}
	if body.Data.AffectedUsers != 5 {
		t.Errorf("affected_users = %d, want 5", body.Data.AffectedUsers)
	}
	if body.Data.SessionsRevoked != 12 {
		t.Errorf("sessions_revoked = %d, want 12", body.Data.SessionsRevoked)
	}
	if body.Data.Notified {
		t.Error("notified = true, want false (notify param not set)")
	}
	if revoker.calledWith != targetTenantID {
		t.Errorf("revoker called with %v, want %v", revoker.calledWith, targetTenantID)
	}
	if dropper.calledWith != targetTenantID {
		t.Errorf("ws dropper called with %v, want %v", dropper.calledWith, targetTenantID)
	}
	if !auditor.called {
		t.Error("audit entry not created")
	}
	if auditor.params.Action != "system.tenant_sessions_revoked" {
		t.Errorf("audit action = %q, want system.tenant_sessions_revoked", auditor.params.Action)
	}
}

func TestRevokeSessionsHandler_TenantAdmin_OwnTenant_Success(t *testing.T) {
	tenantID := uuid.New()

	revoker := &mockTenantSessionRevoker{affectedUsers: 3, sessionsRevoked: 7}
	dropper := &mockTenantWSDropper{}
	notifier := &mockTenantNotifier{}
	auditor := &mockAuditor{}

	h := NewRevokeSessionsHandler(revoker, dropper, notifier, auditor, noopLogger())
	srv := buildRevokeRouter(revokeTestSecret, h)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, tenantID, "tenant_admin")
	resp := doRevokeRequest(t, srv, tok, "tenant="+tenantID.String())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "success" {
		t.Errorf("status = %q, want success", body.Status)
	}
}

func TestRevokeSessionsHandler_TenantAdmin_DifferentTenant_Forbidden(t *testing.T) {
	callerTenantID := uuid.New()
	otherTenantID := uuid.New()

	revoker := &mockTenantSessionRevoker{}
	h := NewRevokeSessionsHandler(revoker, nil, nil, nil, noopLogger())
	srv := buildRevokeRouter(revokeTestSecret, h)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, callerTenantID, "tenant_admin")
	resp := doRevokeRequest(t, srv, tok, "tenant="+otherTenantID.String())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	if revoker.calledWith != uuid.Nil {
		t.Error("revoker should not have been called")
	}
}

func TestRevokeSessionsHandler_NonAdmin_Forbidden(t *testing.T) {
	targetTenantID := uuid.New()

	revoker := &mockTenantSessionRevoker{}
	h := NewRevokeSessionsHandler(revoker, nil, nil, nil, noopLogger())

	mux := http.NewServeMux()
	mux.Handle("/api/v1/system/revoke-all-sessions",
		injectRole(revokeTestSecret, "tenant_admin", http.HandlerFunc(h.RevokeAll)),
	)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, uuid.New(), "analyst")
	resp := doRevokeRequest(t, srv, tok, "tenant="+targetTenantID.String())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestRevokeSessionsHandler_MissingTenantParam_BadRequest(t *testing.T) {
	revoker := &mockTenantSessionRevoker{}
	h := NewRevokeSessionsHandler(revoker, nil, nil, nil, noopLogger())
	srv := buildRevokeRouter(revokeTestSecret, h)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, uuid.New(), "super_admin")
	resp := doRevokeRequest(t, srv, tok, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "error" {
		t.Errorf("status = %q, want error", body.Status)
	}
	if body.Error.Code != apierr.CodeValidationError {
		t.Errorf("error code = %q, want %q", body.Error.Code, apierr.CodeValidationError)
	}
}

func TestRevokeSessionsHandler_WithNotify_SetsNotified(t *testing.T) {
	targetTenantID := uuid.New()

	revoker := &mockTenantSessionRevoker{affectedUsers: 2, sessionsRevoked: 4}
	dropper := &mockTenantWSDropper{}
	notifier := &mockTenantNotifier{}
	auditor := &mockAuditor{}

	h := NewRevokeSessionsHandler(revoker, dropper, notifier, auditor, noopLogger())
	srv := buildRevokeRouter(revokeTestSecret, h)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, uuid.New(), "super_admin")
	resp := doRevokeRequest(t, srv, tok, "tenant="+targetTenantID.String()+"&notify=true")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data struct {
			Notified bool `json:"notified"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Data.Notified {
		t.Error("notified = false, want true when ?notify=true")
	}
}

func TestRevokeSessionsHandler_StoreError_InternalServerError(t *testing.T) {
	targetTenantID := uuid.New()

	revoker := &mockTenantSessionRevoker{err: errors.New("db down")}
	h := NewRevokeSessionsHandler(revoker, nil, nil, nil, noopLogger())
	srv := buildRevokeRouter(revokeTestSecret, h)
	defer srv.Close()

	tok := buildRevokeJWT(t, revokeTestSecret, uuid.New(), "super_admin")
	resp := doRevokeRequest(t, srv, tok, "tenant="+targetTenantID.String())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
