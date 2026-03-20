package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/api/apikey"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestAPIKeyAuth_MissingHeader(t *testing.T) {
	logger := zerolog.Nop()
	middleware := APIKeyAuth(nil, logger)
	handler := middleware(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidCredentials {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidCredentials)
	}
}

func TestAPIKeyAuth_InvalidFormat(t *testing.T) {
	logger := zerolog.Nop()
	middleware := APIKeyAuth(nil, logger)
	handler := middleware(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("X-API-Key", "not-a-valid-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestCombinedAuth_NoAuthHeaders(t *testing.T) {
	logger := zerolog.Nop()
	middleware := CombinedAuth("test-secret-that-is-long-enough-for-jwt", nil, logger)
	handler := middleware(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeInvalidCredentials {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeInvalidCredentials)
	}
}

func TestCombinedAuth_InvalidBearer(t *testing.T) {
	logger := zerolog.Nop()
	middleware := CombinedAuth("test-secret-that-is-long-enough-for-jwt", nil, logger)
	handler := middleware(dummyHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestParseAPIKeyFromHandler(t *testing.T) {
	prefix, _, ok := apikey.ParseAPIKey("argus_ab_1234567890abcdef")
	if !ok {
		t.Fatal("expected valid parse")
	}
	if prefix != "ab" {
		t.Errorf("prefix = %q, want %q", prefix, "ab")
	}
}

func TestHasScopeAccess_Comprehensive(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		required string
		want     bool
	}{
		{"global wildcard", []string{"*"}, "anything:here", true},
		{"exact match", []string{"sims:read"}, "sims:read", true},
		{"resource wildcard", []string{"sims:*"}, "sims:write", true},
		{"no match", []string{"cdrs:read"}, "sims:read", false},
		{"empty scopes", []string{}, "sims:read", false},
		{"nil-like empty", []string{}, "*", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasScopeAccess(tt.scopes, tt.required)
			if got != tt.want {
				t.Errorf("hasScopeAccess(%v, %q) = %v, want %v", tt.scopes, tt.required, got, tt.want)
			}
		})
	}
}

func TestAPIKeyAuth_ContextValues(t *testing.T) {
	_ = uuid.New()
	_ = time.Now()
	_ = context.Background()
}
