package sba

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

func init() {
	// The integration test (integration_test.go:25) also calls MustRegister,
	// but it's behind a build tag — this unit test must register on its own.
	// Panic on duplicate registration is prevented by using a fresh registry.
	defer func() {
		if r := recover(); r != nil {
			// Already registered by a sibling test — swallow.
		}
	}()
	metrics.MustRegister(prometheus.NewRegistry())
}

// TestClient_CreatePDUSession covers the 201-Created happy path, the 503
// INSUFFICIENT_RESOURCES pool-exhausted path, and the 404 USER_NOT_FOUND
// path. The simulator client must:
//   - Return (smContextRef, ueIpv4, nil) on 201.
//   - Return wrapped ErrPDUSessionFailed with the cause text on any non-201.
//   - Increment SBAPDUSessionsTotal{result} with the correct label per branch.
func TestClient_CreatePDUSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Location", "/nsmf-pdusession/v1/sm-contexts/test-ref-001")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"supi":          "imsi-286010000000001",
				"dnn":           "internet",
				"sNssai":        map[string]interface{}{"sst": 1, "sd": "000001"},
				"ueIpv4Address": "10.20.30.40",
			})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
		c.baseURL = srv.URL

		beforeOK := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "ok"))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		ref, ipv4, err := c.CreatePDUSession(ctx, "imsi-286010000000001", "internet", argussba.SNSSAI{SST: 1, SD: "000001"})
		if err != nil {
			t.Fatalf("CreatePDUSession: unexpected error %v", err)
		}
		if ref != "test-ref-001" {
			t.Errorf("smContextRef = %q, want %q", ref, "test-ref-001")
		}
		if ipv4 != "10.20.30.40" {
			t.Errorf("ueIpv4 = %q, want 10.20.30.40", ipv4)
		}

		afterOK := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "ok"))
		if delta := afterOK - beforeOK; delta <= 0 {
			t.Errorf("SBAPDUSessionsTotal{result=ok}: delta=%v, want >0", delta)
		}
	})

	t.Run("pool_exhausted", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(argussba.ProblemDetails{
				Status: http.StatusServiceUnavailable,
				Cause:  "INSUFFICIENT_RESOURCES",
				Detail: "ip pool exhausted",
			})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
		c.baseURL = srv.URL

		beforePE := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "pool_exhausted"))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, _, err := c.CreatePDUSession(ctx, "imsi-286010000000002", "internet", argussba.SNSSAI{SST: 1, SD: "000001"})
		if err == nil {
			t.Fatal("CreatePDUSession: expected error on 503, got nil")
		}
		if !errors.Is(err, ErrPDUSessionFailed) {
			t.Errorf("expected ErrPDUSessionFailed, got %v", err)
		}
		if !strings.Contains(err.Error(), "INSUFFICIENT_RESOURCES") {
			t.Errorf("error should surface cause, got: %v", err)
		}

		afterPE := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "pool_exhausted"))
		if delta := afterPE - beforePE; delta <= 0 {
			t.Errorf("SBAPDUSessionsTotal{result=pool_exhausted}: delta=%v, want >0", delta)
		}
	})

	t.Run("user_not_found", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(argussba.ProblemDetails{
				Status: http.StatusNotFound,
				Cause:  "USER_NOT_FOUND",
			})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
		c.baseURL = srv.URL

		beforeUNF := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "user_not_found"))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, _, err := c.CreatePDUSession(ctx, "imsi-286019999999999", "internet", argussba.SNSSAI{SST: 1, SD: "000001"})
		if err == nil {
			t.Fatal("CreatePDUSession: expected error on 404, got nil")
		}
		if !errors.Is(err, ErrPDUSessionFailed) {
			t.Errorf("expected ErrPDUSessionFailed, got %v", err)
		}

		afterUNF := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "user_not_found"))
		if delta := afterUNF - beforeUNF; delta <= 0 {
			t.Errorf("SBAPDUSessionsTotal{result=user_not_found}: delta=%v, want >0", delta)
		}
	})
}

// TestClient_ReleasePDUSession covers the 204 happy path, the 404 unknown-ref
// branch, and the empty-ref short-circuit.
func TestClient_ReleasePDUSession(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var deleted string
		mux := http.NewServeMux()
		mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			deleted = strings.TrimPrefix(r.URL.Path, "/nsmf-pdusession/v1/sm-contexts/")
			w.WriteHeader(http.StatusNoContent)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
		c.baseURL = srv.URL

		beforeOK := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "ok"))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := c.ReleasePDUSession(ctx, "test-ref-007"); err != nil {
			t.Fatalf("ReleasePDUSession: unexpected error %v", err)
		}
		if deleted != "test-ref-007" {
			t.Errorf("server saw deleted ref %q, want test-ref-007", deleted)
		}

		afterOK := testutil.ToFloat64(metrics.SBAPDUSessionsTotal.WithLabelValues(c.operatorCode, "ok"))
		if delta := afterOK - beforeOK; delta <= 0 {
			t.Errorf("SBAPDUSessionsTotal{result=ok}: delta=%v, want >0", delta)
		}
	})

	t.Run("unknown_ref", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/nsmf-pdusession/v1/sm-contexts/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(argussba.ProblemDetails{
				Status: http.StatusNotFound,
				Cause:  "CONTEXT_NOT_FOUND",
			})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
		c.baseURL = srv.URL

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := c.ReleasePDUSession(ctx, "does-not-exist")
		if err == nil {
			t.Fatal("ReleasePDUSession: expected error on 404, got nil")
		}
		if !errors.Is(err, ErrPDUSessionFailed) {
			t.Errorf("expected ErrPDUSessionFailed, got %v", err)
		}
	})

	t.Run("empty_ref_noop", func(t *testing.T) {
		c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
		// No httptest server needed — empty ref short-circuits before HTTP.
		if err := c.ReleasePDUSession(context.Background(), ""); err != nil {
			t.Errorf("ReleasePDUSession with empty ref: want nil, got %v", err)
		}
	})
}

// TestExtractRefFromLocation covers the path-segment extraction helper.
func TestExtractRefFromLocation(t *testing.T) {
	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"happy_path", "/nsmf-pdusession/v1/sm-contexts/abc-123", "abc-123"},
		{"absolute_url", "http://argus:8443/nsmf-pdusession/v1/sm-contexts/xyz-99", "xyz-99"},
		{"with_query", "/nsmf-pdusession/v1/sm-contexts/abc?foo=bar", "abc"},
		{"empty", "", ""},
		{"malformed", "/different/path", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractRefFromLocation(tc.location); got != tc.want {
				t.Errorf("extractRefFromLocation(%q) = %q, want %q", tc.location, got, tc.want)
			}
		})
	}
}
