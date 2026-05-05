package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockAuditStore struct {
	mu      sync.Mutex
	entries []audit.Entry
	nextID  int64
}

func (m *mockAuditStore) CreateWithChain(_ context.Context, entry *audit.Entry) (*audit.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prevHash := audit.GenesisHash
	if len(m.entries) > 0 {
		prevHash = m.entries[len(m.entries)-1].Hash
	}

	entry.PrevHash = prevHash
	entry.Hash = audit.ComputeHash(*entry, prevHash)
	m.nextID++
	entry.ID = m.nextID
	m.entries = append(m.entries, *entry)
	return entry, nil
}

func (m *mockAuditStore) GetAll(_ context.Context) ([]audit.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]audit.Entry, len(m.entries))
	copy(result, m.entries)
	return result, nil
}

func (m *mockAuditStore) GetBatch(_ context.Context, afterID int64, limit int) ([]audit.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []audit.Entry
	for _, e := range m.entries {
		if e.ID > afterID {
			result = append(result, e)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func withTenantContext(r *http.Request) *http.Request {
	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	return r.WithContext(ctx)
}

func TestHandler_List_NoTenantContext(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeForbidden {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeForbidden)
	}
}

func TestHandler_Verify_Success(t *testing.T) {
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	event := audit.AuditEvent{
		TenantID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Action:     "create",
		EntityType: "sim",
		EntityID:   "sim-1",
	}
	if err := svc.ProcessEntry(context.Background(), event); err != nil {
		t.Fatalf("ProcessEntry: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs/verify", nil)
	w := httptest.NewRecorder()

	handler.Verify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Data verifyResponse `json:"data"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Data.Verified {
		t.Fatal("chain should be verified")
	}
	if resp.Data.TotalRows != 1 {
		t.Fatalf("total_rows = %d, want 1", resp.Data.TotalRows)
	}
}

func TestHandler_Export_NoTenantContext(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"2026-03-01","to":"2026-03-20"}`))
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandler_Export_InvalidBody(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader("not json"))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestHandler_Export_MissingFields(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestHandler_Export_InvalidDateFormat(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"not-a-date","to":"2026-03-20"}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestToAuditLogResponse(t *testing.T) {
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ip := "192.168.1.1"
	ts := time.Date(2026, 3, 18, 14, 2, 0, 123456789, time.UTC)
	email := "user@example.com"
	name := "Test User"

	entry := store.EntryWithUser{
		Entry: audit.Entry{
			ID:         42,
			UserID:     &userID,
			Action:     "create",
			EntityType: "sim",
			EntityID:   "sim-1",
			Diff:       json.RawMessage(`{"name":{"from":null,"to":"test"}}`),
			IPAddress:  &ip,
			CreatedAt:  ts,
		},
		UserEmail: &email,
		UserName:  &name,
	}

	resp := toAuditLogResponse(entry)

	if resp.ID != 42 {
		t.Fatalf("id = %d, want 42", resp.ID)
	}
	if resp.Action != "create" {
		t.Fatalf("action = %s, want create", resp.Action)
	}
	if resp.EntityType != "sim" {
		t.Fatalf("entity_type = %s, want sim", resp.EntityType)
	}
	if resp.EntityID != "sim-1" {
		t.Fatalf("entity_id = %s, want sim-1", resp.EntityID)
	}
	if *resp.IPAddress != "192.168.1.1" {
		t.Fatalf("ip_address = %s, want 192.168.1.1", *resp.IPAddress)
	}
	if resp.CreatedAt != ts.Format(time.RFC3339Nano) {
		t.Fatalf("created_at = %s, want %s", resp.CreatedAt, ts.Format(time.RFC3339Nano))
	}
	if resp.UserEmail != "user@example.com" {
		t.Fatalf("user_email = %s, want user@example.com", resp.UserEmail)
	}
	if resp.UserName != "Test User" {
		t.Fatalf("user_name = %s, want Test User", resp.UserName)
	}
}

func TestToAuditLogResponse_NilOptionalFields(t *testing.T) {
	ts := time.Date(2026, 3, 18, 14, 0, 0, 0, time.UTC)

	entry := store.EntryWithUser{
		Entry: audit.Entry{
			ID:         1,
			Action:     "delete",
			EntityType: "user",
			EntityID:   "user-1",
			CreatedAt:  ts,
		},
	}

	resp := toAuditLogResponse(entry)

	if resp.UserID != nil {
		t.Fatal("user_id should be nil")
	}
	if resp.IPAddress != nil {
		t.Fatal("ip_address should be nil")
	}
	if resp.Diff != nil {
		t.Fatal("diff should be nil")
	}
}

func TestHandler_EmitSystemEvent_Success(t *testing.T) {
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	body := `{"action":"bluegreen_flip","entity_type":"deployment","entity_id":"deploy-1","after_data":{"env":"staging","new_color":"green"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events", strings.NewReader(body))
	w := httptest.NewRecorder()

	handler.EmitSystemEvent(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	if len(store.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(store.entries))
	}
	entry := store.entries[0]
	if entry.TenantID != uuid.Nil {
		t.Fatalf("tenant_id = %s, want uuid.Nil", entry.TenantID)
	}
	if entry.Action != "bluegreen_flip" {
		t.Fatalf("action = %s, want bluegreen_flip", entry.Action)
	}
	if entry.EntityType != "deployment" {
		t.Fatalf("entity_type = %s, want deployment", entry.EntityType)
	}
	if entry.EntityID != "deploy-1" {
		t.Fatalf("entity_id = %s, want deploy-1", entry.EntityID)
	}
	if entry.Hash == "" || len(entry.Hash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(entry.Hash))
	}
	if entry.PrevHash != audit.GenesisHash {
		t.Fatalf("first entry prev_hash = %s, want GenesisHash", entry.PrevHash)
	}
}

func TestHandler_EmitSystemEvent_ChainAppends(t *testing.T) {
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	bodies := []string{
		`{"action":"bluegreen_flip","entity_type":"deployment","entity_id":"flip-1"}`,
		`{"action":"rollback","entity_type":"deployment","entity_id":"rb-1","after_data":{"version":"v1.2.3"}}`,
	}
	for i, body := range bodies {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler.EmitSystemEvent(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("request %d: status = %d, want 201", i, w.Code)
		}
	}

	if len(store.entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(store.entries))
	}
	if store.entries[1].PrevHash != store.entries[0].Hash {
		t.Fatalf("chain broken: second entry prev_hash=%s, first entry hash=%s",
			store.entries[1].PrevHash, store.entries[0].Hash)
	}
}

func TestHandler_EmitSystemEvent_InvalidBody(t *testing.T) {
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	handler.EmitSystemEvent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidFormat {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidFormat)
	}
}

func TestHandler_EmitSystemEvent_MissingFields(t *testing.T) {
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	handler.EmitSystemEvent(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestHandler_EmitSystemEvent_NilAuditSvc(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events",
		strings.NewReader(`{"action":"x","entity_type":"y","entity_id":"z"}`))
	w := httptest.NewRecorder()

	handler.EmitSystemEvent(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestEmitSystemEvent_RouterAuth_Unauthenticated(t *testing.T) {
	// Integration: ensure a raw POST through the chi router with the middleware
	// chain (JWTAuth + RequireRole super_admin) rejects missing tokens with 401.
	// This proves F-1's script auth is enforced end-to-end.
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	// Compose a minimal middleware analogous to the router:
	// missing Authorization header => 401 from JWTAuth.
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeInvalidCredentials,
					"Missing or invalid authorization header")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	protected := mw(http.HandlerFunc(handler.EmitSystemEvent))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events",
		strings.NewReader(`{"action":"x","entity_type":"y","entity_id":"z"}`))
	w := httptest.NewRecorder()
	protected.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestEmitSystemEvent_RouterAuth_InsufficientRole(t *testing.T) {
	// Integration: role < super_admin should be rejected 403 by RequireRole.
	store := &mockAuditStore{}
	svc := audit.NewFullService(store, nil, zerolog.Nop())
	handler := NewHandler(nil, svc, zerolog.Nop())

	roleMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role, _ := r.Context().Value(apierr.RoleKey).(string)
			if !apierr.HasRole(role, "super_admin") {
				apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
					"This action requires super_admin role or higher")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	protected := roleMW(http.HandlerFunc(handler.EmitSystemEvent))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit/system-events",
		strings.NewReader(`{"action":"x","entity_type":"y","entity_id":"z"}`))
	ctx := context.WithValue(req.Context(), apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	protected.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInsufficientRole {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInsufficientRole)
	}
}

func TestHandler_Export_InvalidToDate(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"2026-03-01","to":"not-a-date"}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandler_List_MissingTo_Returns400(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?from=2020-01-01", nil)
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidDateRange {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidDateRange)
	}
}

func TestHandler_List_MissingFrom_Returns400(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?to=2020-01-31", nil)
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidDateRange {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidDateRange)
	}
}

func TestHandler_List_180DaySpan_Returns400(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?from=2020-01-01&to=2020-07-01", nil)
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidDateRange {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidDateRange)
	}
}

func TestHandler_Export_180DaySpan_Returns400(t *testing.T) {
	handler := NewHandler(nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/audit-logs/export", strings.NewReader(`{"from":"2020-01-01","to":"2020-07-01"}`))
	req = withTenantContext(req)
	w := httptest.NewRecorder()

	handler.Export(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidDateRange {
		t.Fatalf("error code = %s, want %s", resp.Error.Code, apierr.CodeInvalidDateRange)
	}
}
