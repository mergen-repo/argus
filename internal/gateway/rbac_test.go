package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
)

func TestRoleLevel(t *testing.T) {
	tests := []struct {
		role  string
		level int
	}{
		{"api_user", 1},
		{"analyst", 2},
		{"policy_editor", 3},
		{"sim_manager", 4},
		{"operator_manager", 5},
		{"tenant_admin", 6},
		{"super_admin", 7},
		{"unknown_role", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := RoleLevel(tt.role)
			if got != tt.level {
				t.Errorf("RoleLevel(%q) = %d, want %d", tt.role, got, tt.level)
			}
		})
	}
}

func TestHasRole(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		required string
		want     bool
	}{
		{"super_admin can access super_admin", "super_admin", "super_admin", true},
		{"super_admin can access analyst", "super_admin", "analyst", true},
		{"super_admin can access api_user", "super_admin", "api_user", true},
		{"tenant_admin can access tenant_admin", "tenant_admin", "tenant_admin", true},
		{"tenant_admin can access sim_manager", "tenant_admin", "sim_manager", true},
		{"tenant_admin cannot access super_admin", "tenant_admin", "super_admin", false},
		{"sim_manager can access sim_manager", "sim_manager", "sim_manager", true},
		{"sim_manager can access analyst", "sim_manager", "analyst", true},
		{"sim_manager cannot access operator_manager", "sim_manager", "operator_manager", false},
		{"analyst can access analyst", "analyst", "analyst", true},
		{"analyst cannot access sim_manager", "analyst", "sim_manager", false},
		{"api_user can access api_user", "api_user", "api_user", true},
		{"api_user cannot access analyst", "api_user", "analyst", false},
		{"unknown role cannot access anything", "unknown", "api_user", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasRole(tt.user, tt.required)
			if got != tt.want {
				t.Errorf("HasRole(%q, %q) = %v, want %v", tt.user, tt.required, got, tt.want)
			}
		})
	}
}

func dummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
}

func requestWithRole(role string) *http.Request {
	ctx := context.WithValue(context.Background(), apierr.RoleKey, role)
	return httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)
}

func TestRequireRole_Allowed(t *testing.T) {
	handler := RequireRole("tenant_admin")(dummyHandler())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithRole("super_admin"))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequireRole_ExactMatch(t *testing.T) {
	handler := RequireRole("sim_manager")(dummyHandler())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithRole("sim_manager"))
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequireRole_Denied(t *testing.T) {
	handler := RequireRole("sim_manager")(dummyHandler())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithRole("analyst"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != apierr.CodeInsufficientRole {
		t.Errorf("expected code %s, got %s", apierr.CodeInsufficientRole, resp.Error.Code)
	}
}

func TestRequireRole_MissingRole(t *testing.T) {
	handler := RequireRole("api_user")(dummyHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != apierr.CodeForbidden {
		t.Errorf("expected code %s, got %s", apierr.CodeForbidden, resp.Error.Code)
	}
}

func TestRequireScope_JWTBypass(t *testing.T) {
	handler := RequireScope("sims:read")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.WithValue(context.Background(), apierr.AuthTypeKey, "jwt")
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for JWT auth (bypass scope check), got %d", rec.Code)
	}
}

func TestRequireScope_NoAuthType(t *testing.T) {
	handler := RequireScope("sims:read")(dummyHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for missing auth_type (defaults to JWT behavior), got %d", rec.Code)
	}
}

func TestRequireScope_Allowed(t *testing.T) {
	handler := RequireScope("sims:read")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
	ctx = context.WithValue(ctx, apierr.ScopesKey, []string{"sims:read", "analytics:read"})
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for api_key with matching scope, got %d", rec.Code)
	}
}

func TestRequireScope_Denied(t *testing.T) {
	handler := RequireScope("sims:write")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
	ctx = context.WithValue(ctx, apierr.ScopesKey, []string{"sims:read", "analytics:read"})
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp apierr.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != apierr.CodeScopeDenied {
		t.Errorf("expected code %s, got %s", apierr.CodeScopeDenied, resp.Error.Code)
	}
}

func TestRequireScope_EmptyScopes(t *testing.T) {
	handler := RequireScope("sims:read")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.WithValue(context.Background(), apierr.AuthTypeKey, "api_key")
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for api_key with no scopes, got %d", rec.Code)
	}
}

func TestRequireScope_WildcardAll(t *testing.T) {
	handler := RequireScope("sims:read")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
	ctx = context.WithValue(ctx, apierr.ScopesKey, []string{"*"})
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for wildcard scope *, got %d", rec.Code)
	}
}

func TestRequireScope_ResourceWildcard(t *testing.T) {
	handler := RequireScope("sims:write")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
	ctx = context.WithValue(ctx, apierr.ScopesKey, []string{"sims:*"})
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for resource wildcard sims:*, got %d", rec.Code)
	}
}

func TestRequireScope_ResourceWildcardDifferentResource(t *testing.T) {
	handler := RequireScope("sims:read")(dummyHandler())
	rec := httptest.NewRecorder()

	ctx := context.Background()
	ctx = context.WithValue(ctx, apierr.AuthTypeKey, "api_key")
	ctx = context.WithValue(ctx, apierr.ScopesKey, []string{"cdrs:*"})
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for cdrs:* when sims:read required, got %d", rec.Code)
	}
}

func TestRequireRole_AllRolesHierarchy(t *testing.T) {
	roles := []string{"api_user", "analyst", "policy_editor", "sim_manager", "operator_manager", "tenant_admin", "super_admin"}

	for i, requiredRole := range roles {
		for j, userRole := range roles {
			handler := RequireRole(requiredRole)(dummyHandler())
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, requestWithRole(userRole))

			expectAllowed := j >= i
			if expectAllowed && rec.Code != http.StatusOK {
				t.Errorf("%s should access %s-required endpoint (200), got %d", userRole, requiredRole, rec.Code)
			}
			if !expectAllowed && rec.Code != http.StatusForbidden {
				t.Errorf("%s should NOT access %s-required endpoint (403), got %d", userRole, requiredRole, rec.Code)
			}
		}
	}
}
