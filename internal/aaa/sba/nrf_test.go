package sba

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNRFRegister_Success(t *testing.T) {
	var reqCount int32
	var capturedMethod, capturedPath, capturedContentType string
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	nrf := NewNRFRegistration(NRFConfig{
		NRFURL:       srv.URL,
		NFInstanceID: "test-nf-001",
		NFType:       "AUSF",
	}, testLogger())

	if err := nrf.RegisterCtx(context.Background()); err != nil {
		t.Fatalf("RegisterCtx: %v", err)
	}

	if atomic.LoadInt32(&reqCount) != 1 {
		t.Errorf("expected 1 request, got %d", reqCount)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if !strings.Contains(capturedPath, "/nnrf-nfm/v1/nf-instances/test-nf-001") {
		t.Errorf("path = %q, want to contain /nnrf-nfm/v1/nf-instances/test-nf-001", capturedPath)
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedContentType)
	}
	if nfType, _ := capturedBody["nfType"].(string); nfType != "AUSF" {
		t.Errorf("body nfType = %q, want AUSF", nfType)
	}
}

func TestNRFRegister_EmptyURL_NoHTTPCall(t *testing.T) {
	var reqCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	nrf := NewNRFRegistration(NRFConfig{
		NRFURL:       "",
		NFInstanceID: "test-nf-002",
	}, testLogger())

	if err := nrf.RegisterCtx(context.Background()); err != nil {
		t.Fatalf("RegisterCtx with empty URL: %v", err)
	}

	if atomic.LoadInt32(&reqCount) != 0 {
		t.Errorf("expected 0 HTTP calls with empty NRFURL, got %d", reqCount)
	}
}

func TestNRFRegister_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	nrf := NewNRFRegistration(NRFConfig{
		NRFURL:       srv.URL,
		NFInstanceID: "test-nf-003",
	}, testLogger())

	if err := nrf.RegisterCtx(context.Background()); err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
}

func TestNRFHeartbeat_PatchWithCorrectContentType(t *testing.T) {
	var capturedMethod, capturedContentType string
	var capturedPatch []map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedContentType = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&capturedPatch)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	nrf := NewNRFRegistration(NRFConfig{
		NRFURL:       srv.URL,
		NFInstanceID: "test-nf-004",
	}, testLogger())

	if err := nrf.HeartbeatCtx(context.Background()); err != nil {
		t.Fatalf("HeartbeatCtx: %v", err)
	}

	if capturedMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", capturedMethod)
	}
	if capturedContentType != "application/json-patch+json" {
		t.Errorf("Content-Type = %q, want application/json-patch+json", capturedContentType)
	}
	if len(capturedPatch) == 0 {
		t.Fatal("expected non-empty patch body")
	}
	found := false
	for _, op := range capturedPatch {
		if op["op"] == "replace" && op["path"] == "/nfStatus" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected patch op replace /nfStatus, got %v", capturedPatch)
	}
}

func TestNRFDeregister_DeleteToCorrectPath(t *testing.T) {
	var capturedMethod, capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	nrf := NewNRFRegistration(NRFConfig{
		NRFURL:       srv.URL,
		NFInstanceID: "test-nf-005",
	}, testLogger())

	if err := nrf.DeregisterCtx(context.Background()); err != nil {
		t.Fatalf("DeregisterCtx: %v", err)
	}

	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if !strings.Contains(capturedPath, "/nnrf-nfm/v1/nf-instances/test-nf-005") {
		t.Errorf("path = %q, want to contain /nnrf-nfm/v1/nf-instances/test-nf-005", capturedPath)
	}
}

func TestNRFRegister_Timeout_ReturnsCtxErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	nrf := NewNRFRegistration(NRFConfig{
		NRFURL:       srv.URL,
		NFInstanceID: "test-nf-006",
	}, testLogger())
	nrf.http = &http.Client{Timeout: 0}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := nrf.RegisterCtx(ctx)
	if err == nil {
		t.Fatal("expected error due to context deadline, got nil")
	}
	if ctx.Err() == nil {
		t.Errorf("expected context error, got: %v", err)
	}
}
