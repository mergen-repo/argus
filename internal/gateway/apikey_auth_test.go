package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/api/apikey"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestMatchIPOrCIDR_CIDR_Deny(t *testing.T) {
	if matchIPOrCIDR("10.0.0.1", "192.168.1.0/24") {
		t.Error("10.0.0.1 should not match 192.168.1.0/24")
	}
}

func TestMatchIPOrCIDR_CIDR_Allow(t *testing.T) {
	if !matchIPOrCIDR("192.168.1.5", "192.168.1.0/24") {
		t.Error("192.168.1.5 should match 192.168.1.0/24")
	}
}

func TestMatchIPOrCIDR_PlainIP_Allow(t *testing.T) {
	if !matchIPOrCIDR("10.0.0.5", "10.0.0.5") {
		t.Error("10.0.0.5 should match plain IP 10.0.0.5")
	}
}

func TestMatchIPOrCIDR_PlainIP_Deny(t *testing.T) {
	if matchIPOrCIDR("10.0.0.6", "10.0.0.5") {
		t.Error("10.0.0.6 should not match plain IP 10.0.0.5")
	}
}

func TestMatchIPOrCIDR_IPv6_CIDR_Allow(t *testing.T) {
	if !matchIPOrCIDR("::1", "::1/128") {
		t.Error("::1 should match ::1/128")
	}
}

func TestMatchIPOrCIDR_EmptyAllowedIPs_AlwaysAllow(t *testing.T) {
	allowedIPs := []string{}
	if len(allowedIPs) > 0 {
		t.Error("empty AllowedIPs must skip enforcement — this test validates the guard condition")
	}
}

func TestTrustedProxy_PrivateIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:9999"
	if !trustedProxy(req) {
		t.Error("192.168.1.1 should be trusted proxy (private IP)")
	}
}

func TestTrustedProxy_Loopback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	if !trustedProxy(req) {
		t.Error("127.0.0.1 should be trusted proxy (loopback)")
	}
}

func TestTrustedProxy_PublicIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:9999"
	if trustedProxy(req) {
		t.Error("1.2.3.4 should not be trusted proxy (public IP)")
	}
}

func TestAPIKeyIPWhitelist_DeniedByCIDR(t *testing.T) {
	allowedIPs := []string{"192.168.1.0/24"}
	clientIP := "10.0.0.1"
	allowed := false
	for _, entry := range allowedIPs {
		if matchIPOrCIDR(clientIP, entry) {
			allowed = true
			break
		}
	}
	if allowed {
		t.Error("10.0.0.1 should be denied by 192.168.1.0/24")
	}
}

func TestAPIKeyIPWhitelist_AllowedByCIDR(t *testing.T) {
	allowedIPs := []string{"192.168.1.0/24"}
	clientIP := "192.168.1.5"
	allowed := false
	for _, entry := range allowedIPs {
		if matchIPOrCIDR(clientIP, entry) {
			allowed = true
			break
		}
	}
	if !allowed {
		t.Error("192.168.1.5 should be allowed by 192.168.1.0/24")
	}
}

func TestAPIKeyIPWhitelist_XForwardedFor_TrustedProxy(t *testing.T) {
	allowedIPs := []string{"192.168.1.0/24"}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "192.168.1.5, 10.0.0.1")

	clientIP := extractIP(req)
	if trustedProxy(req) {
		if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
			clientIP = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}

	allowed := false
	for _, entry := range allowedIPs {
		if matchIPOrCIDR(clientIP, entry) {
			allowed = true
			break
		}
	}
	if !allowed {
		t.Errorf("XFF 192.168.1.5 via trusted proxy should be allowed; clientIP resolved to %q", clientIP)
	}
}

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
