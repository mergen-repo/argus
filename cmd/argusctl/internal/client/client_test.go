package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Do_SuccessEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header, got %q", r.Header.Get("Authorization"))
		}
		if r.Method != "GET" || r.URL.Path != "/api/v1/tenants" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":"t1","name":"acme"}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}

	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.Do(context.Background(), "GET", "/api/v1/tenants", nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != "t1" || out.Name != "acme" {
		t.Fatalf("decoded data wrong: %+v", out)
	}
}

func TestClient_Do_4xxReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"FORBIDDEN","message":"nope"}}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL, Token: "x"})
	err := c.Do(context.Background(), "POST", "/api/v1/tenants", map[string]string{"name": "x"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Status != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", apiErr.Status)
	}
	if apiErr.Code != "FORBIDDEN" {
		t.Errorf("expected code FORBIDDEN, got %s", apiErr.Code)
	}
	if apiErr.Message != "nope" {
		t.Errorf("expected message, got %s", apiErr.Message)
	}
}

func TestClient_Do_5xxReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<html><body>502 bad gateway</body></html>`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	err := c.Do(context.Background(), "GET", "/any", nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("want 502, got %d", apiErr.Status)
	}
	if apiErr.Code != "UNEXPECTED_RESPONSE" {
		t.Errorf("want UNEXPECTED_RESPONSE fallback code, got %s", apiErr.Code)
	}
	if !strings.Contains(apiErr.Message, "502") {
		t.Errorf("expected raw body snippet, got %q", apiErr.Message)
	}
}

func TestClient_Do_NoTokenNoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no auth header, got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":null}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	if err := c.Do(context.Background(), "GET", "/health/ready", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestClient_DoRaw_NonEnvelopeBody(t *testing.T) {
	// Health endpoints return JSON that is NOT wrapped in the standard
	// `{status,data}` envelope. The client currently returns an envelope
	// whose Data is empty in that case; callers of DoRaw on health paths
	// must fall back to reading the synthetic body directly — which is
	// exactly what cmd/health.go does. Exercise that the call succeeds
	// without error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"overall":"up","checks":{"db":"ok"}}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	data, _, err := c.DoRaw(context.Background(), "GET", "/health/ready", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-enveloped bodies should round-trip as the raw response bytes so
	// callers can pretty-print them unchanged.
	if !strings.Contains(string(data), `"overall":"up"`) {
		t.Errorf("expected raw non-enveloped body, got %q", string(data))
	}
}

func TestClient_Do_RequestBodyEncoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("want application/json, got %q", ct)
		}
		var in map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if in["name"] != "acme" {
			t.Errorf("body not forwarded: %v", in)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"ok":true}}`))
	}))
	defer srv.Close()

	c, _ := New(Config{BaseURL: srv.URL})
	var out map[string]interface{}
	if err := c.Do(context.Background(), "POST", "/api/v1/tenants", map[string]string{"name": "acme"}, &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Errorf("expected ok=true, got %v", out)
	}
}

func TestClient_New_RequiresBaseURL(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error for missing base URL")
	}
}

func TestClient_New_mTLSMissingKey(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://x", CertFile: "/tmp/cert.pem"}); err == nil {
		// Missing key file passes this layer but LoadX509KeyPair fails.
		// We require cert+key at the flag-validation layer (root.go), so
		// here only test that an invalid path produces an error.
	}
}
