package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
)

func TestCORSAllowedOrigin(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.argus.io", "https://admin.argus.io"}

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://app.argus.io")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.argus.io" {
		t.Errorf("ACAO = %q, want %q", got, "https://app.argus.io")
	}
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("ACAC = %q, want %q", got, "true")
	}
}

func TestCORSBlockedOrigin(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.argus.io"}

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO should be empty for blocked origin, got %q", got)
	}
}

func TestCORSPreflightAllowed(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.argus.io"}

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://app.argus.io")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rr.Code)
	}

	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("ACAM should be set for preflight")
	}

	if got := rr.Header().Get("Access-Control-Max-Age"); got == "" {
		t.Error("ACMA should be set for preflight")
	}
}

func TestCORSPreflightBlocked(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.argus.io"}

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for blocked preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("blocked preflight status = %d, want 403", rr.Code)
	}
}

func TestCORSAllowAll(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowAllOrigins = true

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://anything.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.com" {
		t.Errorf("ACAO = %q, want %q", got, "https://anything.com")
	}
}

func TestCORSNoOriginHeader(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.argus.io"}

	called := false
	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("handler should be called when no Origin header")
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("ACAO should be empty when no Origin, got %q", got)
	}
}

func TestCORSExposedHeaders(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.argus.io"}

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://app.argus.io")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	exposed := rr.Header().Get("Access-Control-Expose-Headers")
	if exposed == "" {
		t.Error("exposed headers should be set")
	}
}

func TestCORSCaseInsensitiveOrigin(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://APP.ARGUS.IO"}

	handler := CORS(cfg, zerolog.Nop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sims", nil)
	req.Header.Set("Origin", "https://app.argus.io")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://app.argus.io" {
		t.Errorf("case-insensitive ACAO = %q, want %q", got, "https://app.argus.io")
	}
}
