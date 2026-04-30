package job

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/rs/zerolog"
)

func TestJobRunner_MetricsRecord_Success(t *testing.T) {
	reg := metrics.NewRegistry()
	r := NewRunner(nil, nil, nil, RunnerConfig{}, zerolog.Nop())
	r.SetMetrics(reg)

	r.recordMetrics("test_job_type", 100*time.Millisecond, nil)

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()

	body := scrapeMetrics(t, srv.URL)

	wantCounter := `argus_job_runs_total{job_type="test_job_type",result="success"} 1`
	if !strings.Contains(body, wantCounter) {
		t.Errorf("missing counter line %q\nfull output:\n%s", wantCounter, body)
	}

	wantHist := `argus_job_duration_seconds_count{job_type="test_job_type"} 1`
	if !strings.Contains(body, wantHist) {
		t.Errorf("missing histogram count line %q\nfull output:\n%s", wantHist, body)
	}
}

func TestJobRunner_MetricsRecord_Failure(t *testing.T) {
	reg := metrics.NewRegistry()
	r := NewRunner(nil, nil, nil, RunnerConfig{}, zerolog.Nop())
	r.SetMetrics(reg)

	r.recordMetrics("test_job_type", 50*time.Millisecond, errors.New("something failed"))

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()

	body := scrapeMetrics(t, srv.URL)

	wantCounter := `argus_job_runs_total{job_type="test_job_type",result="failure"} 1`
	if !strings.Contains(body, wantCounter) {
		t.Errorf("missing counter line %q\nfull output:\n%s", wantCounter, body)
	}

	wantHist := `argus_job_duration_seconds_count{job_type="test_job_type"} 1`
	if !strings.Contains(body, wantHist) {
		t.Errorf("missing histogram count line %q\nfull output:\n%s", wantHist, body)
	}
}

func TestJobRunner_MetricsNil_NoOp(t *testing.T) {
	r := NewRunner(nil, nil, nil, RunnerConfig{}, zerolog.Nop())

	r.recordMetrics("test_job_type", 10*time.Millisecond, nil)
	r.recordMetrics("test_job_type", 10*time.Millisecond, errors.New("err"))
}

func scrapeMetrics(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
