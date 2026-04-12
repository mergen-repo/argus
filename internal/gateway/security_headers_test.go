package gateway

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersDefault(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	cfg.HSTSOnlyWhenTLS = false
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Cache-Control", "no-store"},
		{"Pragma", "no-cache"},
	}

	for _, tt := range tests {
		got := rr.Header().Get(tt.header)
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
		}
	}

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Error("Content-Security-Policy header should be set")
	}
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP should contain default-src 'self', got %q", csp)
	}
	if !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP should contain script-src 'self', got %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP should contain frame-ancestors 'none', got %q", csp)
	}

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("Strict-Transport-Security header should be set")
	}
	if !strings.Contains(hsts, "max-age=31536000") {
		t.Errorf("HSTS should contain max-age=31536000, got %q", hsts)
	}
	if !strings.Contains(hsts, "includeSubDomains") {
		t.Errorf("HSTS should contain includeSubDomains, got %q", hsts)
	}
}

func TestSecurityHeadersCustomCSP(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	cfg.CSPDirectives = "default-src 'none'"

	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("CSP = %q, want %q", got, "default-src 'none'")
	}
}

func TestSecurityHeadersDisabledHSTS(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	cfg.HSTSMaxAge = 0

	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS should be empty when max-age is 0, got %q", got)
	}
}

func TestSecurityHeadersHSTSPreload(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	cfg.HSTSPreload = true
	cfg.HSTSOnlyWhenTLS = false

	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if !strings.Contains(hsts, "preload") {
		t.Errorf("HSTS should contain preload, got %q", hsts)
	}
}

func TestPermissionsPolicy(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	pp := rr.Header().Get("Permissions-Policy")
	if pp == "" {
		t.Error("Permissions-Policy header should be set")
	}
	if !strings.Contains(pp, "geolocation=()") {
		t.Errorf("Permissions-Policy should deny geolocation, got %q", pp)
	}
}

func TestHSTSNotEmittedOverPlainHTTP(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be emitted over plain HTTP, got %q", got)
	}
}

func TestHSTSEmittedOverDirectTLS(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.TLS = &tls.ConnectionState{}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("HSTS must be emitted over direct TLS")
	}
	if !strings.Contains(hsts, "max-age=31536000") {
		t.Errorf("HSTS should contain max-age=31536000, got %q", hsts)
	}
}

func TestHSTSEmittedViaForwardedProtoWhenTrusted(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	cfg.TrustForwardedProto = true
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	hsts := rr.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("HSTS must be emitted when X-Forwarded-Proto=https and TrustForwardedProto=true")
	}
}

func TestHSTSNotEmittedViaForwardedProtoWhenNotTrusted(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	cfg.TrustForwardedProto = false
	handler := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS must not be emitted via X-Forwarded-Proto when TrustForwardedProto=false, got %q", got)
	}
}
