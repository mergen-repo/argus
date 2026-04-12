package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/go-chi/chi/v5"
)

func TestPrometheusHTTPMetrics_IncrementsCounter(t *testing.T) {
	reg := metrics.NewRegistry()

	r := chi.NewRouter()
	r.Use(PrometheusHTTPMetrics(reg))
	r.Get("/test/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	scrapeReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	scrapeW := httptest.NewRecorder()
	reg.Handler().ServeHTTP(scrapeW, scrapeReq)

	if scrapeW.Code != http.StatusOK {
		t.Fatalf("metrics handler returned %d", scrapeW.Code)
	}

	body, err := io.ReadAll(scrapeW.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}
	bodyStr := string(body)

	wantCounter := `argus_http_requests_total{method="GET",route="/test/{id}",status="200"`
	if !strings.Contains(bodyStr, wantCounter) {
		t.Errorf("counter line not found.\nwant substring: %q\nbody:\n%s", wantCounter, bodyStr)
	}

	wantHist := `argus_http_request_duration_seconds_count{method="GET",route="/test/{id}",tenant_id="unknown"} 1`
	if !strings.Contains(bodyStr, wantHist) {
		t.Errorf("histogram count line not found.\nwant substring: %q\nbody:\n%s", wantHist, bodyStr)
	}
}

func TestPrometheusHTTPMetrics_Unmatched(t *testing.T) {
	reg := metrics.NewRegistry()

	r := chi.NewRouter()
	r.Use(PrometheusHTTPMetrics(reg))
	r.Get("/exists", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	scrapeReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	scrapeW := httptest.NewRecorder()
	reg.Handler().ServeHTTP(scrapeW, scrapeReq)

	body, _ := io.ReadAll(scrapeW.Body)
	if !strings.Contains(string(body), `route="unmatched"`) {
		t.Errorf("expected unmatched route label, body:\n%s", string(body))
	}
}
