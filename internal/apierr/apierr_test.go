package apierr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	WriteJSON(w, http.StatusOK, data)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("key = %q, want %q", result["key"], "value")
	}
}

func TestWriteSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSuccess(w, http.StatusOK, map[string]string{"name": "test"})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("Status = %q, want %q", resp.Status, "success")
	}
	if resp.Data == nil {
		t.Error("Data should not be nil")
	}
}

func TestWriteSuccess_Created(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSuccess(w, http.StatusCreated, map[string]string{"id": "abc"})

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, CodeInvalidFormat, "Invalid ID format")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "error" {
		t.Errorf("Status = %q, want %q", resp.Status, "error")
	}
	if resp.Error.Code != CodeInvalidFormat {
		t.Errorf("Code = %q, want %q", resp.Error.Code, CodeInvalidFormat)
	}
	if resp.Error.Message != "Invalid ID format" {
		t.Errorf("Message = %q, want %q", resp.Error.Message, "Invalid ID format")
	}
	if resp.Error.Details != nil {
		t.Error("Details should be nil when not provided")
	}
}

func TestWriteError_WithDetails(t *testing.T) {
	w := httptest.NewRecorder()
	details := []map[string]string{{"field": "iccid", "message": "required"}}
	WriteError(w, http.StatusUnprocessableEntity, CodeValidationError, "Validation failed", details)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error.Details == nil {
		t.Error("Details should not be nil when provided")
	}
}

func TestWriteList(t *testing.T) {
	w := httptest.NewRecorder()
	data := []string{"a", "b", "c"}
	meta := ListMeta{Total: 100, Cursor: "cursor-abc", HasMore: true, Limit: 50}

	WriteList(w, http.StatusOK, data, meta)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp ListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("Status = %q, want %q", resp.Status, "success")
	}
	if resp.Meta.Total != 100 {
		t.Errorf("Meta.Total = %d, want 100", resp.Meta.Total)
	}
	if resp.Meta.Cursor != "cursor-abc" {
		t.Errorf("Meta.Cursor = %q, want %q", resp.Meta.Cursor, "cursor-abc")
	}
	if !resp.Meta.HasMore {
		t.Error("Meta.HasMore should be true")
	}
}

func TestWriteList_Empty(t *testing.T) {
	w := httptest.NewRecorder()
	WriteList(w, http.StatusOK, []string{}, ListMeta{Total: 0, HasMore: false})

	var resp ListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Meta.Total != 0 {
		t.Errorf("Meta.Total = %d, want 0", resp.Meta.Total)
	}
	if resp.Meta.HasMore {
		t.Error("Meta.HasMore should be false")
	}
}

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
		{"unknown", 0},
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
		{"super_admin has all access", "super_admin", "api_user", true},
		{"super_admin can access super_admin", "super_admin", "super_admin", true},
		{"tenant_admin can access sim_manager", "tenant_admin", "sim_manager", true},
		{"tenant_admin cannot access super_admin", "tenant_admin", "super_admin", false},
		{"sim_manager can access analyst", "sim_manager", "analyst", true},
		{"sim_manager cannot access tenant_admin", "sim_manager", "tenant_admin", false},
		{"api_user can access api_user", "api_user", "api_user", true},
		{"api_user cannot access analyst", "api_user", "analyst", false},
		{"analyst cannot access sim_manager", "analyst", "sim_manager", false},
		{"policy_editor can access analyst", "policy_editor", "analyst", true},
		{"unknown role denied", "unknown", "api_user", false},
		{"empty role denied", "", "api_user", false},
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

func TestContextKeys(t *testing.T) {
	if CorrelationIDKey != "correlation_id" {
		t.Errorf("CorrelationIDKey = %q, want %q", CorrelationIDKey, "correlation_id")
	}
	if TenantIDKey != "tenant_id" {
		t.Errorf("TenantIDKey = %q, want %q", TenantIDKey, "tenant_id")
	}
	if UserIDKey != "user_id" {
		t.Errorf("UserIDKey = %q, want %q", UserIDKey, "user_id")
	}
	if RoleKey != "role" {
		t.Errorf("RoleKey = %q, want %q", RoleKey, "role")
	}
	if AuthTypeKey != "auth_type" {
		t.Errorf("AuthTypeKey = %q, want %q", AuthTypeKey, "auth_type")
	}
	if ScopesKey != "scopes" {
		t.Errorf("ScopesKey = %q, want %q", ScopesKey, "scopes")
	}
	if APIKeyIDKey != "api_key_id" {
		t.Errorf("APIKeyIDKey = %q, want %q", APIKeyIDKey, "api_key_id")
	}
}

func TestErrorCodes_NotEmpty(t *testing.T) {
	codes := []string{
		CodeInternalError, CodeInvalidFormat, CodeValidationError,
		CodeNotFound, CodeConflict, CodeAlreadyExists,
		CodeInvalidCredentials, CodeAccountLocked, CodeAccountDisabled,
		CodeInvalid2FACode, CodeTokenExpired, CodeInvalidRefreshToken,
		CodeForbidden, CodeInsufficientRole, CodeScopeDenied, CodeForbiddenCrossTenant,
		CodeResourceLimitExceeded, CodeTenantSuspended,
		CodeRateLimited, CodeAPNHasActiveSIMs, CodePoolExhausted,
		CodeIPAlreadyAllocated, CodeICCIDExists, CodeIMSIExists,
		CodeInvalidStateTransition,
		CodeProfileAlreadyEnabled, CodeNotESIM, CodeInvalidProfileState,
		CodeSameProfile, CodeDifferentSIM,
	}
	for _, code := range codes {
		if code == "" {
			t.Error("error code should not be empty")
		}
	}
}

func TestWriteError_AllHTTPStatuses(t *testing.T) {
	statuses := []struct {
		code int
		err  string
	}{
		{http.StatusBadRequest, CodeInvalidFormat},
		{http.StatusUnauthorized, CodeInvalidCredentials},
		{http.StatusForbidden, CodeForbidden},
		{http.StatusNotFound, CodeNotFound},
		{http.StatusConflict, CodeConflict},
		{http.StatusUnprocessableEntity, CodeValidationError},
		{http.StatusTooManyRequests, CodeRateLimited},
		{http.StatusInternalServerError, CodeInternalError},
	}
	for _, tt := range statuses {
		t.Run(tt.err, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, tt.code, tt.err, "test message")
			if w.Code != tt.code {
				t.Errorf("status = %d, want %d", w.Code, tt.code)
			}
		})
	}
}
