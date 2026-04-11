package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestBruteForceProgressiveDelay(t *testing.T) {
	tests := []struct {
		attempts int
		base     int
		max      int
		want     int
	}{
		{3, 1, 30, 1},
		{5, 1, 30, 1},
		{6, 1, 30, 3},
		{10, 1, 30, 11},
		{20, 1, 30, 30},
		{100, 1, 30, 30},
	}

	for _, tt := range tests {
		got := progressiveDelay(tt.attempts, tt.base, tt.max)
		if got != tt.want {
			t.Errorf("progressiveDelay(%d, %d, %d) = %d, want %d",
				tt.attempts, tt.base, tt.max, got, tt.want)
		}
	}
}

func TestIsAuthEndpoint(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/v1/auth/login", true},
		{"/api/v1/auth/2fa/verify", true},
		{"/api/v1/sims", false},
		{"/api/v1/auth/logout", false},
		{"/api/v1/auth/refresh", true},
		{"/api/health", false},
	}

	for _, tt := range tests {
		got := isAuthEndpoint(tt.path)
		if got != tt.want {
			t.Errorf("isAuthEndpoint(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{"ipv4 with port", "192.168.1.1:12345", "192.168.1.1"},
		{"ipv4 loopback", "127.0.0.1:443", "127.0.0.1"},
		{"ipv4 private", "10.0.0.1:8080", "10.0.0.1"},
		{"ipv6 bracketed with port", "[2001:db8::1]:4500", "2001:db8::1"},
		{"ipv6 loopback bracketed", "[::1]:53142", "::1"},
		{"ipv6 bare", "2001:db8::1", "2001:db8::1"},
		{"ipv4 bare no port", "192.168.1.1", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
			req.RemoteAddr = tt.remoteAddr
			got := extractIP(req)
			if got != tt.want {
				t.Errorf("extractIP(remoteAddr=%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

func TestBruteForceNilRedisPassthrough(t *testing.T) {
	cfg := DefaultBruteForceConfig()
	logger := zerolog.Nop()

	called := false
	handler := BruteForceProtection(nil, cfg, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when Redis is nil (fail-open)")
	}
}

func TestBruteForceSkipsNonAuth(t *testing.T) {
	cfg := DefaultBruteForceConfig()
	logger := zerolog.Nop()

	called := false
	handler := BruteForceProtection(nil, cfg, logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called for non-auth endpoints")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
}

func TestDefaultBruteForceConfig(t *testing.T) {
	cfg := DefaultBruteForceConfig()
	if cfg.MaxAttempts != 10 {
		t.Errorf("MaxAttempts = %d, want 10", cfg.MaxAttempts)
	}
	if cfg.WindowSeconds != 900 {
		t.Errorf("WindowSeconds = %d, want 900", cfg.WindowSeconds)
	}
	if cfg.BaseDelaySec != 1 {
		t.Errorf("BaseDelaySec = %d, want 1", cfg.BaseDelaySec)
	}
	if cfg.MaxDelaySec != 30 {
		t.Errorf("MaxDelaySec = %d, want 30", cfg.MaxDelaySec)
	}
}
