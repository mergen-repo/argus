package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sbaConfigFromTestServer extracts the host/port from an httptest
// server URL so NewSBAAdapter can build its own request URL from the
// SBAConfig primitives (Host, Port, TLSEnabled).
func sbaConfigFromTestServer(t *testing.T, srv *httptest.Server, extra SBAConfig) SBAConfig {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host := u.Hostname()
	portInt, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	extra.Host = host
	extra.Port = portInt
	return extra
}

func TestSBAAdapter_HealthCheck_NRF_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nnrf-nfm/v1/nf-instances" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(sbaConfigFromTestServer(t, srv, SBAConfig{TimeoutMs: 2000}))
	a, err := NewSBAAdapter(cfg)
	if err != nil {
		t.Fatalf("new sba adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Fatalf("healthy NRF should succeed, got error: %s", result.Error)
	}
}

func TestSBAAdapter_HealthCheck_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(sbaConfigFromTestServer(t, srv, SBAConfig{TimeoutMs: 2000}))
	a, err := NewSBAAdapter(cfg)
	if err != nil {
		t.Fatalf("new sba adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on 404")
	}
	if !strings.Contains(result.Error, "http status 404") {
		t.Errorf("expected 'http status 404' in error, got %q", result.Error)
	}
}

func TestSBAAdapter_HealthCheck_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(sbaConfigFromTestServer(t, srv, SBAConfig{TimeoutMs: 30}))
	a, err := NewSBAAdapter(cfg)
	if err != nil {
		t.Fatalf("new sba adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on timeout")
	}
}

func TestSBAAdapter_HealthCheck_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg, _ := json.Marshal(sbaConfigFromTestServer(t, srv, SBAConfig{TimeoutMs: 2000}))
	a, err := NewSBAAdapter(cfg)
	if err != nil {
		t.Fatalf("new sba adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on 500")
	}
	if !strings.Contains(result.Error, "http status 500") {
		t.Errorf("expected 'http status 500' in error, got %q", result.Error)
	}
}
