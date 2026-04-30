package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHTTPAdapter_HealthCheck_Happy covers the success path: a live
// upstream returns HTTP 200 on the configured health path.
func TestHTTPAdapter_HealthCheck_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(HTTPConfig{BaseURL: srv.URL, HealthPath: "/health"})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Fatalf("health check failed: %s", result.Error)
	}
}

// TestHTTPAdapter_HealthCheck_404 verifies that a 404 is reported as
// Success=false with the classified status in Error.
func TestHTTPAdapter_HealthCheck_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(HTTPConfig{BaseURL: srv.URL, HealthPath: "/health"})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on 404")
	}
	if !strings.Contains(result.Error, "404") {
		t.Errorf("error should mention 404, got %q", result.Error)
	}
}

// TestHTTPAdapter_HealthCheck_Timeout uses a hung handler + short
// timeout to prove the adapter classifies timeouts correctly.
func TestHTTPAdapter_HealthCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(HTTPConfig{BaseURL: srv.URL, HealthPath: "/health", TimeoutMs: 30})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on timeout")
	}
}

// TestHTTPAdapter_HealthCheck_BasicAuth verifies HTTP Basic
// Authorization header is correctly applied.
func TestHTTPAdapter_HealthCheck_BasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(HTTPConfig{
		BaseURL:    srv.URL,
		HealthPath: "/health",
		AuthType:   "basic",
		BasicUser:  "admin",
		BasicPass:  "secret",
	})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Fatalf("basic auth should succeed, got error: %s", result.Error)
	}
}

// TestHTTPAdapter_HealthCheck_BearerAuth verifies bearer token header
// application.
func TestHTTPAdapter_HealthCheck_BearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer secret-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(HTTPConfig{
		BaseURL:     srv.URL,
		HealthPath:  "/health",
		AuthType:    "bearer",
		BearerToken: "secret-token",
	})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Fatalf("bearer auth should succeed, got error: %s", result.Error)
	}
}

func TestHTTPAdapter_Type(t *testing.T) {
	cfg, _ := json.Marshal(HTTPConfig{BaseURL: "http://example.com"})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	if a.Type() != "http" {
		t.Errorf("Type() = %q, want %q", a.Type(), "http")
	}
}

// TestHTTPAdapter_UnsupportedAAAMethods confirms AAA methods return
// ErrUnsupportedProtocol so routers can fall through cleanly when
// an HTTP adapter is accidentally wired into an AAA path.
func TestHTTPAdapter_UnsupportedAAAMethods(t *testing.T) {
	cfg, _ := json.Marshal(HTTPConfig{BaseURL: "http://example.com"})
	a, err := NewHTTPAdapter(cfg)
	if err != nil {
		t.Fatalf("new http adapter: %v", err)
	}
	ctx := context.Background()
	if _, err := a.ForwardAuth(ctx, AuthRequest{}); err == nil {
		t.Error("ForwardAuth should return ErrUnsupportedProtocol")
	}
	if err := a.ForwardAcct(ctx, AcctRequest{}); err == nil {
		t.Error("ForwardAcct should return ErrUnsupportedProtocol")
	}
	if err := a.SendCoA(ctx, CoARequest{}); err == nil {
		t.Error("SendCoA should return ErrUnsupportedProtocol")
	}
	if err := a.SendDM(ctx, DMRequest{}); err == nil {
		t.Error("SendDM should return ErrUnsupportedProtocol")
	}
	if _, err := a.Authenticate(ctx, AuthenticateRequest{}); err == nil {
		t.Error("Authenticate should return ErrUnsupportedProtocol")
	}
	if err := a.AccountingUpdate(ctx, AccountingUpdateRequest{}); err == nil {
		t.Error("AccountingUpdate should return ErrUnsupportedProtocol")
	}
	if _, err := a.FetchAuthVectors(ctx, "imsi", 1); err == nil {
		t.Error("FetchAuthVectors should return ErrUnsupportedProtocol")
	}
}

func TestHTTPAdapter_RejectsBadConfig(t *testing.T) {
	_, err := NewHTTPAdapter(nil)
	if err == nil {
		t.Error("expected error on nil config")
	}
	_, err = NewHTTPAdapter(json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error when base_url missing")
	}
}
