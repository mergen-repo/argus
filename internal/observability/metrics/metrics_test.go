package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/observability/metrics"
)

func TestNewRegistry_NoPanic(t *testing.T) {
	var r *metrics.Registry
	require := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	_ = require
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("NewRegistry panicked: %v", rec)
		}
	}()
	r = metrics.NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
}

func TestNewRegistry_UniqueMetricNames(t *testing.T) {
	r := metrics.NewRegistry()
	mfs, err := r.Reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}
	seen := make(map[string]bool)
	for _, mf := range mfs {
		name := mf.GetName()
		if seen[name] {
			t.Errorf("duplicate metric name: %s", name)
		}
		seen[name] = true
	}
}

func TestHandler_ReturnsMetrics(t *testing.T) {
	r := metrics.NewRegistry()

	r.HTTPRequestsTotal.WithLabelValues("GET", "/probe", "200", "t0").Add(0)

	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{
		"# HELP argus_http_requests_total",
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics output missing %q\nfull output:\n%s", want, text)
		}
	}
}

func TestNewRegistry_BuildInfoRegistered(t *testing.T) {
	r := metrics.NewRegistry()
	if r.BuildInfo == nil {
		t.Fatal("BuildInfo gauge vec is nil")
	}
	r.BuildInfo.WithLabelValues("v1.0.0", "abc1234", "2026-04-12T00:00:00Z").Set(1)

	mfs, err := r.Reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "argus_build_info" {
			return
		}
	}
	t.Error("argus_build_info metric not found in gathered metrics")
}

func TestCounter_Increments(t *testing.T) {
	r := metrics.NewRegistry()
	r.HTTPRequestsTotal.WithLabelValues("GET", "/test", "200", "t1").Inc()

	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	want := `argus_http_requests_total{method="GET",route="/test",status="200",tenant_id="t1"} 1`
	if !strings.Contains(text, want) {
		t.Errorf("metrics output missing line %q\nfull output:\n%s", want, text)
	}
}
