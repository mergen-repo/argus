package apikey

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

func TestScopePattern(t *testing.T) {
	tests := []struct {
		scope string
		valid bool
	}{
		{"*", true},
		{"sims:read", true},
		{"sims:*", true},
		{"cdrs:write", true},
		{"analytics:read", true},
		{"", false},
		{"sims", false},
		{"SIMS:READ", false},
		{"sims:read:extra", false},
		{"sims:", false},
		{":read", false},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got := scopePattern.MatchString(tt.scope)
			if got != tt.valid {
				t.Errorf("scopePattern.MatchString(%q) = %v, want %v", tt.scope, got, tt.valid)
			}
		})
	}
}

func TestGenerateAPIKey(t *testing.T) {
	prefix, fullKey, keyHash, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generateAPIKey() error = %v", err)
	}

	if len(prefix) != 2 {
		t.Errorf("prefix length = %d, want 2", len(prefix))
	}

	if !strings.HasPrefix(fullKey, "argus_") {
		t.Errorf("fullKey should start with argus_, got %q", fullKey[:10])
	}

	parts := strings.SplitN(fullKey, "_", 3)
	if len(parts) != 3 {
		t.Fatalf("fullKey should have 3 parts separated by _, got %d", len(parts))
	}
	if parts[0] != "argus" {
		t.Errorf("parts[0] = %q, want argus", parts[0])
	}
	if parts[1] != prefix {
		t.Errorf("parts[1] = %q, want %q (prefix)", parts[1], prefix)
	}

	if len(keyHash) != 64 {
		t.Errorf("keyHash length = %d, want 64 (SHA-256 hex)", len(keyHash))
	}

	reHash := HashAPIKey(fullKey)
	if reHash != keyHash {
		t.Errorf("HashAPIKey(fullKey) = %q, want %q", reHash, keyHash)
	}
}

func TestParseAPIKey(t *testing.T) {
	tests := []struct {
		key    string
		prefix string
		ok     bool
	}{
		{"argus_ab_1234567890abcdef", "ab", true},
		{"argus_cd_secret", "cd", true},
		{"not_a_key", "", false},
		{"argus__secret", "", false},
		{"argus_ab_", "", false},
		{"argus_ab", "", false},
		{"", "", false},
		{"invalid", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			prefix, _, ok := ParseAPIKey(tt.key)
			if ok != tt.ok {
				t.Errorf("ParseAPIKey(%q) ok = %v, want %v", tt.key, ok, tt.ok)
			}
			if ok && prefix != tt.prefix {
				t.Errorf("ParseAPIKey(%q) prefix = %q, want %q", tt.key, prefix, tt.prefix)
			}
		})
	}
}

func TestCreateValidation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, 20, logger)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid json",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeInvalidFormat,
		},
		{
			name:       "missing name",
			body:       `{"scopes":["*"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "missing scopes",
			body:       `{"name":"test-key"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "empty scopes",
			body:       `{"name":"test-key","scopes":[]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "invalid scope format",
			body:       `{"name":"test-key","scopes":["INVALID"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "negative rate limit",
			body:       `{"name":"test-key","scopes":["*"],"rate_limit_per_minute":-1}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "invalid expires_at format",
			body:       `{"name":"test-key","scopes":["*"],"expires_at":"not-a-date"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "expires_at in past",
			body:       `{"name":"test-key","scopes":["*"],"expires_at":"2020-01-01T00:00:00Z"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "name too long",
			body:       `{"name":"` + strings.Repeat("a", 101) + `","scopes":["*"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
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

func TestUpdateValidation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, 20, logger)

	tests := []struct {
		name       string
		idStr      string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid UUID",
			idStr:      "not-a-uuid",
			body:       `{"name":"new-name"}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeInvalidFormat,
		},
		{
			name:       "invalid json",
			idStr:      uuid.New().String(),
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeInvalidFormat,
		},
		{
			name:       "empty name",
			idStr:      uuid.New().String(),
			body:       `{"name":""}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "empty scopes array",
			idStr:      uuid.New().String(),
			body:       `{"scopes":[]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "negative rate limit",
			idStr:      uuid.New().String(),
			body:       `{"rate_limit_per_minute":-5}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/api-keys/"+tt.idStr, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
			req = req.WithContext(ctx)

			req = withChiURLParam(req, "id", tt.idStr)

			rr := httptest.NewRecorder()
			h.Update(rr, req)

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

func TestRotateInvalidID(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, 20, logger)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys/bad-id/rotate", nil)
	req = withChiURLParam(req, "id", "bad-id")

	rr := httptest.NewRecorder()
	h.Rotate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestDeleteInvalidID(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, 20, logger)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/api-keys/bad-id", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	req = req.WithContext(ctx)
	req = withChiURLParam(req, "id", "bad-id")

	rr := httptest.NewRecorder()
	h.Delete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestAllowedIPsValidation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, 20, logger)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid cidr entry",
			body:       `{"name":"k","scopes":["*"],"allowed_ips":["not-a-cidr"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeInvalidCIDR,
		},
		{
			name:       "invalid partial cidr",
			body:       `{"name":"k","scopes":["*"],"allowed_ips":["192.168.1.0/33"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeInvalidCIDR,
		},
		{
			name:       "mixed valid invalid",
			body:       `{"name":"k","scopes":["*"],"allowed_ips":["10.0.0.1","bad"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeInvalidCIDR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
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

func TestAllowedIPsUpdateValidation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, nil, 20, logger)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid cidr in update",
			body:       `{"allowed_ips":["not-a-cidr"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeInvalidCIDR,
		},
		{
			name:       "invalid cidr in update second entry",
			body:       `{"allowed_ips":["10.0.0.1","not-valid"]}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeInvalidCIDR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idStr := uuid.New().String()
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/api-keys/"+idStr, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
			req = req.WithContext(ctx)

			req = withChiURLParam(req, "id", idStr)

			rr := httptest.NewRecorder()
			h.Update(rr, req)

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

func TestValidateIPEntry(t *testing.T) {
	tests := []struct {
		entry string
		valid bool
	}{
		{"192.168.1.0/24", true},
		{"10.0.0.5", true},
		{"10.0.0.5/32", true},
		{"::1", true},
		{"2001:db8::/32", true},
		{"not-a-cidr", false},
		{"192.168.1.0/33", false},
		{"999.999.999.999", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.entry, func(t *testing.T) {
			got := validateIPEntry(tt.entry)
			if got != tt.valid {
				t.Errorf("validateIPEntry(%q) = %v, want %v", tt.entry, got, tt.valid)
			}
		})
	}
}

func TestHashAPIKeyConsistency(t *testing.T) {
	key := "argus_ab_1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	hash1 := HashAPIKey(key)
	hash2 := HashAPIKey(key)

	if hash1 != hash2 {
		t.Errorf("HashAPIKey should be deterministic: %q != %q", hash1, hash2)
	}

	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash1))
	}

	differentKey := "argus_cd_differentkeysecret"
	differentHash := HashAPIKey(differentKey)
	if hash1 == differentHash {
		t.Error("Different keys should produce different hashes")
	}
}
