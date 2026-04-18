package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

func TestRouting_OperatorEndpoints(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	operators := []string{"turkcell", "vodafone_tr", "vodafone", "turk_telekom", "mock"}

	for _, op := range operators {
		t.Run(op+"_health", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+op+"/health", nil)
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("operator=%s health: status = %d, want 200", op, w.Code)
			}
			var body struct {
				Operator string `json:"operator"`
				Status   string `json:"status"`
			}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Operator != op {
				t.Errorf("operator = %q, want %q", body.Operator, op)
			}
			if body.Status != "ok" {
				t.Errorf("status = %q, want ok", body.Status)
			}
		})

		t.Run(op+"_subscriber", func(t *testing.T) {
			imsi := "234150123456789"
			req := httptest.NewRequest(http.MethodGet, "/"+op+"/subscribers/"+imsi, nil)
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("operator=%s subscriber: status = %d, want 200", op, w.Code)
			}
			var body struct {
				IMSI     string `json:"imsi"`
				Operator string `json:"operator"`
				Plan     string `json:"plan"`
				Status   string `json:"status"`
			}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.IMSI != imsi {
				t.Errorf("imsi = %q, want %q", body.IMSI, imsi)
			}
			if body.Operator != op {
				t.Errorf("operator = %q, want %q", body.Operator, op)
			}
			if body.Status != "active" {
				t.Errorf("status = %q, want active", body.Status)
			}
			if body.Plan != "default" {
				t.Errorf("plan = %q, want default", body.Plan)
			}
		})

		t.Run(op+"_cdr", func(t *testing.T) {
			body := `{"session_id":"abc123","bytes_in":1024,"bytes_out":2048}`
			req := httptest.NewRequest(http.MethodPost, "/"+op+"/cdr", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.router.ServeHTTP(w, req)
			if w.Code != http.StatusAccepted {
				t.Errorf("operator=%s cdr: status = %d, want 202", op, w.Code)
			}
			var resp struct {
				Received   bool      `json:"received"`
				IngestedAt time.Time `json:"ingested_at"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !resp.Received {
				t.Error("received should be true")
			}
			if time.Since(resp.IngestedAt) > 2*time.Second {
				t.Errorf("ingested_at %v is not recent", resp.IngestedAt)
			}
		})
	}
}

func TestRouting_UnknownOperator(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/unknown_operator/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}

	var body struct {
		Error    string `json:"error"`
		Operator string `json:"operator"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "unknown operator" {
		t.Errorf("error = %q, want 'unknown operator'", body.Error)
	}
	if body.Operator != "unknown_operator" {
		t.Errorf("operator = %q, want 'unknown_operator'", body.Operator)
	}
}

func TestMetricsServer_Health(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())
	metricsMux := srv.buildMetricsMux()

	req := httptest.NewRequest(http.MethodGet, "/-/health", nil)
	w := httptest.NewRecorder()
	metricsMux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/-/health status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("/-/health body = %q, want to contain 'ok'", w.Body.String())
	}
}

func TestMetricsServer_MetricsRoute(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())
	metricsMux := srv.buildMetricsMux()

	req := httptest.NewRequest(http.MethodGet, "/-/metrics", nil)
	w := httptest.NewRecorder()
	metricsMux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("/-/metrics status = %d, want 200", w.Code)
	}
}

func TestRun_CancelledContext(t *testing.T) {
	cfg := testConfig()
	cfg.Server.Listen = ":0"
	cfg.Server.MetricsListen = ":0"

	srv := New(cfg, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after context cancel")
	}
}

func TestMetricsRegistration_AfterRequest(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/turkcell/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("pre-request failed: %d", w.Code)
	}

	count, err := testutil.GatherAndCount(srv.metrics.reg)
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one metric family to be registered")
	}
}

func TestMetricsCounter_IncrementOnRequest(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/turkcell/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("request failed: %d", w.Code)
	}

	_, err := testutil.GatherAndCount(srv.metrics.reg)
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	count := testutil.ToFloat64(srv.metrics.requestsTotal.WithLabelValues(
		"turkcell", "/{operator}/health", http.MethodGet, "200",
	))
	if count < 1 {
		t.Errorf("counter = %v, want >= 1", count)
	}
}

func TestStatusRecorder_DefaultStatus200(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/turkcell/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestStatusRecorder_ExplicitStatus(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	sr.WriteHeader(http.StatusCreated)
	if sr.statusCode() != http.StatusCreated {
		t.Errorf("statusCode = %d, want 201", sr.statusCode())
	}
	sr.WriteHeader(http.StatusOK)
	if sr.statusCode() != http.StatusCreated {
		t.Errorf("statusCode should not change after first WriteHeader call")
	}
}

func TestStatusRecorder_DefaultWhenNotWritten(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if sr.statusCode() != http.StatusOK {
		t.Errorf("default statusCode = %d, want 200", sr.statusCode())
	}
}

func TestCDREcho_Disabled(t *testing.T) {
	cfg := testConfig()
	cfg.Stubs.CDREcho = false
	srv := New(cfg, zerolog.Nop())

	body := `{"session_id":"test123"}`
	req := httptest.NewRequest(http.MethodPost, "/turkcell/cdr", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", w.Code)
	}
}

func TestSubscriberHandler_IMSIAndOperatorMatch(t *testing.T) {
	srv := New(testConfig(), zerolog.Nop())

	imsi := "310260123456789"
	req := httptest.NewRequest(http.MethodGet, "/vodafone_tr/subscribers/"+imsi, nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		IMSI     string `json:"imsi"`
		Operator string `json:"operator"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IMSI != imsi {
		t.Errorf("imsi = %q, want %q", resp.IMSI, imsi)
	}
	if resp.Operator != "vodafone_tr" {
		t.Errorf("operator = %q, want vodafone_tr", resp.Operator)
	}
}
