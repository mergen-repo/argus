package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockUserStore struct {
	getByIDFn                   func(ctx context.Context, id uuid.UUID) (*store.User, error)
	clearLockoutFn              func(ctx context.Context, userID uuid.UUID) error
	setPasswordHashFn           func(ctx context.Context, userID uuid.UUID, hash string) error
	setPasswordChangeRequiredFn func(ctx context.Context, userID uuid.UUID, required bool) error
}

func (m *mockUserStore) GetByID(ctx context.Context, id uuid.UUID) (*store.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, store.ErrUserNotFound
}

func (m *mockUserStore) ListByTenant(ctx context.Context, cursor string, limit int, roleFilter string, stateFilter string) ([]store.User, string, error) {
	return nil, "", nil
}

func (m *mockUserStore) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockUserStore) CreateUser(ctx context.Context, p store.CreateUserParams) (*store.User, error) {
	return nil, errors.New("not implemented")
}

func (m *mockUserStore) CreateUserWithPassword(ctx context.Context, p store.CreateUserParams, passwordHash string) (*store.User, error) {
	return nil, errors.New("not implemented")
}

func (m *mockUserStore) UpdateUser(ctx context.Context, id uuid.UUID, p store.UpdateUserParams) (*store.User, error) {
	return nil, errors.New("not implemented")
}

func (m *mockUserStore) DeletePII(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) (*store.PurgeResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockUserStore) ClearLockout(ctx context.Context, userID uuid.UUID) error {
	if m.clearLockoutFn != nil {
		return m.clearLockoutFn(ctx, userID)
	}
	return nil
}

func (m *mockUserStore) SetPasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	if m.setPasswordHashFn != nil {
		return m.setPasswordHashFn(ctx, userID, hash)
	}
	return nil
}

func (m *mockUserStore) SetPasswordChangeRequired(ctx context.Context, userID uuid.UUID, required bool) error {
	if m.setPasswordChangeRequiredFn != nil {
		return m.setPasswordChangeRequiredFn(ctx, userID, required)
	}
	return nil
}

func (m *mockUserStore) UpdateLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	return nil
}

type mockSessionStore struct {
	revokeAllFn    func(ctx context.Context, userID uuid.UUID) error
	getActiveFn    func(ctx context.Context, userID uuid.UUID) ([]store.UserSession, error)
	revokedUserIDs []uuid.UUID
}

func (m *mockSessionStore) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	m.revokedUserIDs = append(m.revokedUserIDs, userID)
	if m.revokeAllFn != nil {
		return m.revokeAllFn(ctx, userID)
	}
	return nil
}

func (m *mockSessionStore) GetActiveByUserID(ctx context.Context, userID uuid.UUID) ([]store.UserSession, error) {
	if m.getActiveFn != nil {
		return m.getActiveFn(ctx, userID)
	}
	return []store.UserSession{{ID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}}, nil
}

type mockAPIKeyStore struct {
	revokeAllFn func(ctx context.Context, userID uuid.UUID) (int64, error)
	count       int64
}

func (m *mockAPIKeyStore) RevokeAllByUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	if m.revokeAllFn != nil {
		return m.revokeAllFn(ctx, userID)
	}
	return m.count, nil
}

type mockWSHub struct {
	droppedUserIDs []uuid.UUID
}

func (m *mockWSHub) DropUser(userID uuid.UUID) {
	m.droppedUserIDs = append(m.droppedUserIDs, userID)
}

func newHandlerForTest(t *testing.T, us userStoreI, opts ...HandlerOption) *Handler {
	t.Helper()
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := &Handler{
		userStore:  us,
		auditSvc:   nil,
		logger:     logger.With().Str("component", "user_handler").Logger(),
		bcryptCost: 4,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

func withChiURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"admin@test.com", true},
		{"user@example.co.uk", true},
		{"a@b.c", true},
		{"", false},
		{"@test.com", false},
		{"admin@", false},
		{"admin", false},
		{"admin@test", false},
		{"admin@.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := isValidEmail(tt.email)
			if got != tt.valid {
				t.Errorf("isValidEmail(%q) = %v, want %v", tt.email, got, tt.valid)
			}
		})
	}
}

func TestValidRoles(t *testing.T) {
	expected := []string{"api_user", "analyst", "policy_editor", "sim_manager", "operator_manager", "tenant_admin"}
	for _, role := range expected {
		if !validRoles[role] {
			t.Errorf("role %q should be valid", role)
		}
	}

	if validRoles["super_admin"] {
		t.Error("super_admin should not be in validRoles map")
	}
	if validRoles["invalid_role"] {
		t.Error("invalid_role should not be valid")
	}
}

func TestCreateUserValidation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	tests := []struct {
		name       string
		body       string
		role       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid json",
			body:       `{invalid}`,
			role:       "tenant_admin",
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeInvalidFormat,
		},
		{
			name:       "missing email",
			body:       `{"name":"User","role":"analyst"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "invalid email format",
			body:       `{"email":"not-an-email","name":"User","role":"analyst"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "missing name",
			body:       `{"email":"user@test.com","role":"analyst"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "missing role",
			body:       `{"email":"user@test.com","name":"User"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "invalid role value",
			body:       `{"email":"user@test.com","name":"User","role":"invalid_role"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "tenant_admin cannot assign super_admin",
			body:       `{"email":"user@test.com","name":"User","role":"super_admin"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "tenant_admin cannot assign tenant_admin",
			body:       `{"email":"user@test.com","name":"User","role":"tenant_admin"}`,
			role:       "tenant_admin",
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, tt.role)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			h.Create(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rr.Code, tt.wantStatus, rr.Body.String())
			}

			var resp apierr.ErrorResponse
			json.NewDecoder(rr.Body).Decode(&resp)
			if resp.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", resp.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestUpdateUserSelfCanOnlyChangeName(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	userID := uuid.New()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "self changing role forbidden",
			body:       `{"role":"tenant_admin"}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "self changing state forbidden",
			body:       `{"state":"disabled"}`,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+userID.String(), strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, userID)
			ctx = context.WithValue(ctx, apierr.RoleKey, "analyst")
			req = req.WithContext(ctx)

			req = withChiURLParam(req, "id", userID.String())

			rr := httptest.NewRecorder()
			h.Update(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestUpdateUserNonAdminCannotUpdateOthers(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	callerID := uuid.New()
	targetID := uuid.New()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+targetID.String(), strings.NewReader(`{"name":"New Name"}`))
	req.Header.Set("Content-Type", "application/json")

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "analyst")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Update(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestUpdateUserInvalidRoleValue(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	callerID := uuid.New()
	targetID := uuid.New()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+targetID.String(), strings.NewReader(`{"role":"invalid_role"}`))
	req.Header.Set("Content-Type", "application/json")

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Update(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusUnprocessableEntity, rr.Body.String())
	}
}

func TestUpdateUserInvalidState(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	callerID := uuid.New()
	targetID := uuid.New()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+targetID.String(), strings.NewReader(`{"state":"terminated"}`))
	req.Header.Set("Content-Type", "application/json")

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Update(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusUnprocessableEntity, rr.Body.String())
	}
}

func TestValidUserStates(t *testing.T) {
	if !validUserStates["active"] {
		t.Error("active should be a valid user state")
	}
	if !validUserStates["disabled"] {
		t.Error("disabled should be a valid user state")
	}
	if validUserStates["terminated"] {
		t.Error("terminated should not be a valid user state for updates")
	}
	if validUserStates["invited"] {
		t.Error("invited should not be a valid user state for updates (only initial state)")
	}
}

func TestDelete_RequiresGDPRQueryParam(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	targetID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+targetID.String(), nil)

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Delete(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusUnprocessableEntity, rr.Body.String())
	}

	var resp apierr.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestDelete_RefusesSelfPurge(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	callerID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+callerID.String()+"?gdpr=1", nil)

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", callerID.String())

	rr := httptest.NewRecorder()
	h.Delete(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusForbidden, rr.Body.String())
	}

	var resp apierr.ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeForbidden {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeForbidden)
	}
}

func TestDelete_InvalidUUID(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, logger)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/not-a-uuid?gdpr=1", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", "not-a-uuid")

	rr := httptest.NewRecorder()
	h.Delete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestToUserResponse(t *testing.T) {
	u := &userResponse{
		ID:        uuid.New().String(),
		Email:     "user@test.com",
		Name:      "Test User",
		Role:      "analyst",
		State:     "invited",
		CreatedAt: "2026-03-20T00:00:00Z",
	}

	if u.State != "invited" {
		t.Errorf("State = %q, want %q", u.State, "invited")
	}
	if u.LastLoginAt != nil {
		t.Error("LastLoginAt should be nil for new user")
	}
}

func makeUser(tenantID, userID uuid.UUID) *store.User {
	return &store.User{
		ID:       userID,
		TenantID: tenantID,
		Email:    "user@test.com",
		Name:     "Test User",
		Role:     "analyst",
		State:    "active",
	}
}

func TestUnlock_InvalidUUID(t *testing.T) {
	h := newHandlerForTest(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/not-a-uuid/unlock", nil)
	req = withChiURLParam(req, "id", "not-a-uuid")
	rr := httptest.NewRecorder()
	h.Unlock(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUnlock_UserNotFound(t *testing.T) {
	us := &mockUserStore{}
	h := newHandlerForTest(t, us)

	targetID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/unlock", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Unlock(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestUnlock_WrongTenant(t *testing.T) {
	tenantID := uuid.New()
	targetID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return makeUser(uuid.New(), targetID), nil
		},
	}
	h := newHandlerForTest(t, us)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/unlock", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Unlock(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestUnlock_HappyPath(t *testing.T) {
	tenantID := uuid.New()
	targetID := uuid.New()
	clearLockoutCalled := false

	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return makeUser(tenantID, targetID), nil
		},
		clearLockoutFn: func(ctx context.Context, userID uuid.UUID) error {
			clearLockoutCalled = true
			return nil
		},
	}
	h := newHandlerForTest(t, us)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/unlock", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.Unlock(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !clearLockoutCalled {
		t.Error("ClearLockout was not called")
	}
}

func TestRevokeSessions_InvalidUUID(t *testing.T) {
	h := newHandlerForTest(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/not-a-uuid/revoke-sessions", nil)
	req = withChiURLParam(req, "id", "not-a-uuid")
	rr := httptest.NewRecorder()
	h.RevokeSessions(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestRevokeSessions_NonAdminCannotRevokeOthers(t *testing.T) {
	h := newHandlerForTest(t, nil)
	callerID := uuid.New()
	targetID := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/revoke-sessions", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "analyst")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.RevokeSessions(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusForbidden, rr.Body.String())
	}
}

func TestRevokeSessions_SelfAllowed(t *testing.T) {
	tenantID := uuid.New()
	callerID := uuid.New()
	sessionStore := &mockSessionStore{}
	wsHub := &mockWSHub{}

	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return makeUser(tenantID, callerID), nil
		},
	}
	h := newHandlerForTest(t, us,
		WithSessionStore(sessionStore),
		WithWSHub(wsHub),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+callerID.String()+"/revoke-sessions", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "analyst")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", callerID.String())

	rr := httptest.NewRecorder()
	h.RevokeSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	if len(sessionStore.revokedUserIDs) == 0 {
		t.Error("sessions were not revoked")
	}
	if len(wsHub.droppedUserIDs) == 0 {
		t.Error("ws connections were not dropped")
	}
}

func TestRevokeSessions_HappyPathWithAPIKeys(t *testing.T) {
	tenantID := uuid.New()
	targetID := uuid.New()
	sessionStore := &mockSessionStore{}
	apiKeyStore := &mockAPIKeyStore{count: 3}
	wsHub := &mockWSHub{}

	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return makeUser(tenantID, targetID), nil
		},
	}
	h := newHandlerForTest(t, us,
		WithSessionStore(sessionStore),
		WithAPIKeyStore(apiKeyStore),
		WithWSHub(wsHub),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/revoke-sessions?include_api_keys=true", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.RevokeSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp apierr.SuccessResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	data, _ := json.Marshal(resp.Data)
	var payload map[string]interface{}
	json.Unmarshal(data, &payload)
	if payload["apikeys_revoked"] == nil {
		t.Error("apikeys_revoked missing from response")
	}
}

func TestRevokeSessions_UserNotFound(t *testing.T) {
	us := &mockUserStore{}
	h := newHandlerForTest(t, us)

	targetID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/revoke-sessions", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.RevokeSessions(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestResetPassword_InvalidUUID(t *testing.T) {
	h := newHandlerForTest(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/not-a-uuid/reset-password", nil)
	req = withChiURLParam(req, "id", "not-a-uuid")
	rr := httptest.NewRecorder()
	h.ResetPassword(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestResetPassword_UserNotFound(t *testing.T) {
	us := &mockUserStore{}
	h := newHandlerForTest(t, us)

	targetID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/reset-password", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.ResetPassword(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestResetPassword_WrongTenant(t *testing.T) {
	tenantID := uuid.New()
	targetID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return makeUser(uuid.New(), targetID), nil
		},
	}
	h := newHandlerForTest(t, us)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/reset-password", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.ResetPassword(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestResetPassword_HappyPath(t *testing.T) {
	tenantID := uuid.New()
	targetID := uuid.New()
	sessionStore := &mockSessionStore{}
	wsHub := &mockWSHub{}
	setPasswordHashCalled := false
	setPasswordChangedCalled := false

	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return makeUser(tenantID, targetID), nil
		},
		setPasswordHashFn: func(ctx context.Context, userID uuid.UUID, hash string) error {
			setPasswordHashCalled = true
			return nil
		},
		setPasswordChangeRequiredFn: func(ctx context.Context, userID uuid.UUID, required bool) error {
			setPasswordChangedCalled = true
			if !required {
				t.Error("SetPasswordChangeRequired called with false, want true")
			}
			return nil
		},
	}
	h := newHandlerForTest(t, us,
		WithSessionStore(sessionStore),
		WithWSHub(wsHub),
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetID.String()+"/reset-password", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.ResetPassword(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body: %s)", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !setPasswordHashCalled {
		t.Error("SetPasswordHash was not called")
	}
	if !setPasswordChangedCalled {
		t.Error("SetPasswordChangeRequired was not called")
	}
	if len(sessionStore.revokedUserIDs) == 0 {
		t.Error("sessions were not revoked after password reset")
	}

	var resp apierr.SuccessResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	data, _ := json.Marshal(resp.Data)
	var payload map[string]interface{}
	json.Unmarshal(data, &payload)
	if payload["temp_password"] == "" || payload["temp_password"] == nil {
		t.Error("temp_password missing from response")
	}
}

// STORY-075: GetUser — happy path, not found, cross-tenant 404, invalid UUID
func TestGetUser_Happy(t *testing.T) {
	tenantID := uuid.New()
	targetID := uuid.New()
	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return &store.User{
				ID: targetID, TenantID: tenantID,
				Email: "u@example.com", Name: "Bob", Role: "analyst", State: "active",
				TOTPEnabled: true, CreatedAt: time.Now(),
			}, nil
		},
	}
	h := newHandlerForTest(t, us)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetID.String(), nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())

	rr := httptest.NewRecorder()
	h.GetUser(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	var resp apierr.SuccessResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	data, _ := json.Marshal(resp.Data)
	var payload map[string]interface{}
	json.Unmarshal(data, &payload)
	if payload["email"] != "u@example.com" {
		t.Errorf("email = %v, want u@example.com", payload["email"])
	}
	if payload["totp_enabled"] != true {
		t.Errorf("totp_enabled = %v, want true", payload["totp_enabled"])
	}
}

func TestGetUser_InvalidUUID(t *testing.T) {
	h := newHandlerForTest(t, &mockUserStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/not-a-uuid", nil)
	req = withChiURLParam(req, "id", "not-a-uuid")
	rr := httptest.NewRecorder()
	h.GetUser(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	h := newHandlerForTest(t, &mockUserStore{})
	targetID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetID.String(), nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New())
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())
	rr := httptest.NewRecorder()
	h.GetUser(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestGetUser_CrossTenant(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	targetID := uuid.New()
	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return &store.User{ID: targetID, TenantID: tenantA, Email: "x@y.com", Role: "analyst", State: "active", CreatedAt: time.Now()}, nil
		},
	}
	h := newHandlerForTest(t, us)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetID.String(), nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantB)
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())
	rr := httptest.NewRecorder()
	h.GetUser(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant should return 404, got %d", rr.Code)
	}
}

func TestActivity_InvalidUUID(t *testing.T) {
	h := newHandlerForTest(t, &mockUserStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/bad-id/activity", nil)
	req = withChiURLParam(req, "id", "bad-id")
	rr := httptest.NewRecorder()
	h.Activity(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestActivity_UserNotFound(t *testing.T) {
	h := newHandlerForTest(t, &mockUserStore{})
	targetID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetID.String()+"/activity", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, uuid.New())
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())
	rr := httptest.NewRecorder()
	h.Activity(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestActivity_NoAuditStore(t *testing.T) {
	// When audit store is not configured, handler should return empty list (200).
	tenantID := uuid.New()
	targetID := uuid.New()
	us := &mockUserStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*store.User, error) {
			return &store.User{ID: targetID, TenantID: tenantID, Email: "u@x.com", Role: "analyst", State: "active", CreatedAt: time.Now()}, nil
		},
	}
	h := newHandlerForTest(t, us)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+targetID.String()+"/activity", nil)
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", targetID.String())
	rr := httptest.NewRecorder()
	h.Activity(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
}
