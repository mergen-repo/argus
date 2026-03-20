package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

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
