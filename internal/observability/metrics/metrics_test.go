package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestRecent5xxCount(t *testing.T) {
	t.Run("initial count is zero", func(t *testing.T) {
		r := metrics.NewRegistry()
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("Recent5xxCount() = %d, want 0", got)
		}
	})

	t.Run("records 3x500 within 1s, expect 3", func(t *testing.T) {
		r := metrics.NewRegistry()
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(500)
		if got := r.Recent5xxCount(); got != 3 {
			t.Errorf("Recent5xxCount() = %d, want 3", got)
		}
	})

	t.Run("200 response is ignored", func(t *testing.T) {
		r := metrics.NewRegistry()
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(200)
		if got := r.Recent5xxCount(); got != 3 {
			t.Errorf("Recent5xxCount() = %d, want 3 (200 must not count)", got)
		}
	})

	t.Run("4xx responses are ignored", func(t *testing.T) {
		r := metrics.NewRegistry()
		statuses := []int{100, 200, 201, 301, 400, 401, 403, 404, 429, 499}
		for _, s := range statuses {
			r.RecordHTTPStatus(s)
		}
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("Recent5xxCount() = %d, want 0 (non-5xx must not count)", got)
		}
	})

	t.Run("range of 5xx is counted", func(t *testing.T) {
		r := metrics.NewRegistry()
		statuses := []int{500, 501, 502, 503, 504, 505, 507, 599}
		for _, s := range statuses {
			r.RecordHTTPStatus(s)
		}
		if got := r.Recent5xxCount(); got != int64(len(statuses)) {
			t.Errorf("Recent5xxCount() = %d, want %d", got, len(statuses))
		}
	})

	t.Run("600+ is not counted", func(t *testing.T) {
		r := metrics.NewRegistry()
		r.RecordHTTPStatus(600)
		r.RecordHTTPStatus(700)
		r.RecordHTTPStatus(999)
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("Recent5xxCount() = %d, want 0 (600+ must not count)", got)
		}
	})

	t.Run("then 2x503 adds to total", func(t *testing.T) {
		r := metrics.NewRegistry()
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(500)
		r.RecordHTTPStatus(200)
		r.RecordHTTPStatus(503)
		r.RecordHTTPStatus(503)
		if got := r.Recent5xxCount(); got != 5 {
			t.Errorf("Recent5xxCount() = %d, want 5", got)
		}
	})

	t.Run("nil registry is safe", func(t *testing.T) {
		var r *metrics.Registry
		r.RecordHTTPStatus(500)
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("Recent5xxCount() on nil = %d, want 0", got)
		}
	})

	t.Run("window expiry — records older than 300s drop out", func(t *testing.T) {
		r := metrics.NewRegistry()
		base := time.Unix(1_700_000_000, 0)
		clock := base
		metrics.SetRecent5xxNow(r, func() time.Time { return clock })

		for i := 0; i < 4; i++ {
			r.RecordHTTPStatus(500)
		}
		if got := r.Recent5xxCount(); got != 4 {
			t.Fatalf("before advance: Recent5xxCount() = %d, want 4", got)
		}

		clock = base.Add(299 * time.Second)
		if got := r.Recent5xxCount(); got != 4 {
			t.Errorf("at +299s: Recent5xxCount() = %d, want 4", got)
		}

		clock = base.Add(300 * time.Second)
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("at +300s: Recent5xxCount() = %d, want 0 (slot is now stale)", got)
		}

		r.RecordHTTPStatus(503)
		r.RecordHTTPStatus(503)
		if got := r.Recent5xxCount(); got != 2 {
			t.Errorf("new records after expiry: Recent5xxCount() = %d, want 2", got)
		}

		clock = base.Add(900 * time.Second)
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("at +900s: Recent5xxCount() = %d, want 0", got)
		}
	})

	t.Run("records spread across buckets all sum up within window", func(t *testing.T) {
		r := metrics.NewRegistry()
		base := time.Unix(1_700_000_500, 0)
		clock := base
		metrics.SetRecent5xxNow(r, func() time.Time { return clock })

		for i := 0; i < 30; i++ {
			clock = base.Add(time.Duration(i) * time.Second)
			r.RecordHTTPStatus(500)
		}
		if got := r.Recent5xxCount(); got != 30 {
			t.Errorf("Recent5xxCount() = %d, want 30", got)
		}
	})

	t.Run("records spread across 5 minutes all counted", func(t *testing.T) {
		r := metrics.NewRegistry()
		base := time.Unix(1_700_001_000, 0)
		clock := base
		metrics.SetRecent5xxNow(r, func() time.Time { return clock })

		// One 500 every 5 seconds across the full 300s window → 60 hits.
		for i := 0; i < 60; i++ {
			clock = base.Add(time.Duration(i*5) * time.Second)
			r.RecordHTTPStatus(500)
		}
		// Still at t=+295s — window start is t=+295-299=-4s, all stamps included.
		clock = base.Add(295 * time.Second)
		if got := r.Recent5xxCount(); got != 60 {
			t.Errorf("Recent5xxCount() at +295s = %d, want 60", got)
		}
		// At t=+600s, all original stamps are older than 300s → 0.
		clock = base.Add(600 * time.Second)
		if got := r.Recent5xxCount(); got != 0 {
			t.Errorf("Recent5xxCount() at +600s = %d, want 0", got)
		}
	})

	t.Run("concurrent record is race-safe", func(t *testing.T) {
		r := metrics.NewRegistry()
		const goroutines = 100
		const perGoroutine = 10
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < perGoroutine; j++ {
					r.RecordHTTPStatus(500)
				}
			}()
		}
		wg.Wait()
		got := r.Recent5xxCount()
		want := int64(goroutines * perGoroutine)
		if got != want {
			t.Errorf("Recent5xxCount() = %d, want %d", got, want)
		}
	})
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
