package metrics_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/google/uuid"
)

func TestPromAAARecorder_IncrementsCounter(t *testing.T) {
	reg := metrics.NewRegistry()
	rec := metrics.NewPromAAARecorder(reg, "radius")

	tenantID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	opID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	ctx := context.WithValue(context.Background(), apierr.TenantIDKey, tenantID)

	rec.RecordAuth(ctx, opID, true, 42)
	rec.RecordAuth(ctx, opID, true, 17)
	rec.RecordAuth(ctx, opID, false, 99)

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	wantSuccess := `argus_aaa_auth_requests_total{operator_id="22222222-2222-2222-2222-222222222222",protocol="radius",result="success",tenant_id="11111111-1111-1111-1111-111111111111"} 2`
	if !strings.Contains(text, wantSuccess) {
		t.Errorf("missing expected success counter line %q\noutput:\n%s", wantSuccess, text)
	}

	wantFailure := `argus_aaa_auth_requests_total{operator_id="22222222-2222-2222-2222-222222222222",protocol="radius",result="failure",tenant_id="11111111-1111-1111-1111-111111111111"} 1`
	if !strings.Contains(text, wantFailure) {
		t.Errorf("missing expected failure counter line %q\noutput:\n%s", wantFailure, text)
	}
}

func TestPromAAARecorder_ObservesHistogram(t *testing.T) {
	reg := metrics.NewRegistry()
	rec := metrics.NewPromAAARecorder(reg, "radius")

	opID := uuid.New()
	ctx := context.Background()

	rec.RecordAuth(ctx, opID, true, 5)
	rec.RecordAuth(ctx, opID, true, 15)
	rec.RecordAuth(ctx, opID, true, 250)

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	// Histogram count reflects three observations; bucket boundaries
	// come from the Registry definition (seconds).
	if !strings.Contains(text, `argus_aaa_auth_latency_seconds_count{operator_id="`+opID.String()+`",protocol="radius",tenant_id="unknown"} 3`) {
		t.Errorf("expected histogram _count to show 3 observations with tenant_id=unknown; output:\n%s", text)
	}
}

func TestPromAAARecorder_TenantUnknownWhenMissing(t *testing.T) {
	reg := metrics.NewRegistry()
	rec := metrics.NewPromAAARecorder(reg, "diameter")
	rec.RecordAuth(context.Background(), uuid.New(), true, 1)

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if !strings.Contains(text, `tenant_id="unknown"`) {
		t.Errorf("expected tenant_id=\"unknown\" fallback in output:\n%s", text)
	}
}

func TestPromAAARecorder_NilSafe(t *testing.T) {
	var rec *metrics.PromAAARecorder
	rec.RecordAuth(context.Background(), uuid.New(), true, 10)

	rec2 := metrics.NewPromAAARecorder(nil, "radius")
	rec2.RecordAuth(context.Background(), uuid.New(), false, 5)
}

type stubRecorder struct {
	mu    sync.Mutex
	calls []stubCall
}

type stubCall struct {
	operatorID uuid.UUID
	success    bool
	latencyMs  int
}

func (s *stubRecorder) RecordAuth(_ context.Context, operatorID uuid.UUID, success bool, latencyMs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubCall{operatorID, success, latencyMs})
}

func TestCompositeRecorder_FansOut(t *testing.T) {
	a := &stubRecorder{}
	b := &stubRecorder{}
	composite := metrics.NewCompositeRecorder(a, b)

	opID := uuid.New()
	composite.RecordAuth(context.Background(), opID, true, 33)

	if len(a.calls) != 1 || len(b.calls) != 1 {
		t.Fatalf("expected 1 call on each recorder, got a=%d b=%d", len(a.calls), len(b.calls))
	}
	if a.calls[0].operatorID != opID || !a.calls[0].success || a.calls[0].latencyMs != 33 {
		t.Errorf("recorder a received unexpected call: %+v", a.calls[0])
	}
	if b.calls[0].operatorID != opID || !b.calls[0].success || b.calls[0].latencyMs != 33 {
		t.Errorf("recorder b received unexpected call: %+v", b.calls[0])
	}
}

func TestCompositeRecorder_DropsNilRecorders(t *testing.T) {
	a := &stubRecorder{}
	composite := metrics.NewCompositeRecorder(nil, a, nil)

	if composite.Len() != 1 {
		t.Errorf("expected Len=1 after dropping nils, got %d", composite.Len())
	}

	composite.RecordAuth(context.Background(), uuid.New(), false, 7)
	if len(a.calls) != 1 {
		t.Errorf("expected 1 call after fanout, got %d", len(a.calls))
	}
}

func TestCompositeRecorder_EmptyIsSafe(t *testing.T) {
	composite := metrics.NewCompositeRecorder()
	composite.RecordAuth(context.Background(), uuid.New(), true, 1)
}

func TestCompositeRecorder_CombinesPromAndStub(t *testing.T) {
	reg := metrics.NewRegistry()
	prom := metrics.NewPromAAARecorder(reg, "radius")
	stub := &stubRecorder{}

	composite := metrics.NewCompositeRecorder(prom, stub)
	opID := uuid.New()
	composite.RecordAuth(context.Background(), opID, true, 11)
	composite.RecordAuth(context.Background(), opID, false, 22)

	if len(stub.calls) != 2 {
		t.Errorf("stub recorder expected 2 calls, got %d", len(stub.calls))
	}

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)
	if !strings.Contains(text, `argus_aaa_auth_requests_total{operator_id="`+opID.String()+`",protocol="radius",result="success",tenant_id="unknown"} 1`) {
		t.Errorf("missing prom success counter in:\n%s", text)
	}
}

func TestRegistry_SetOperatorHealth(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.SetOperatorHealth("op-1", "healthy")
	reg.SetOperatorHealth("op-2", "degraded")
	reg.SetOperatorHealth("op-3", "down")
	reg.SetOperatorHealth("op-4", "garbage")

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	for _, want := range []string{
		`argus_operator_health{operator_id="op-1"} 2`,
		`argus_operator_health{operator_id="op-2"} 1`,
		`argus_operator_health{operator_id="op-3"} 0`,
		`argus_operator_health{operator_id="op-4"} 0`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing line %q\noutput:\n%s", want, text)
		}
	}
}

func TestRegistry_SetCircuitBreakerState(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.SetCircuitBreakerState("op-1", "closed")

	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	scrape := func() string {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("GET /metrics: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	text := scrape()
	if !strings.Contains(text, `argus_circuit_breaker_state{operator_id="op-1",state="closed"} 1`) {
		t.Errorf("expected closed=1 after initial set, got:\n%s", text)
	}
	if !strings.Contains(text, `argus_circuit_breaker_state{operator_id="op-1",state="open"} 0`) {
		t.Errorf("expected open=0 after initial set, got:\n%s", text)
	}
	if !strings.Contains(text, `argus_circuit_breaker_state{operator_id="op-1",state="half_open"} 0`) {
		t.Errorf("expected half_open=0 after initial set, got:\n%s", text)
	}

	reg.SetCircuitBreakerState("op-1", "open")
	text = scrape()
	if !strings.Contains(text, `argus_circuit_breaker_state{operator_id="op-1",state="open"} 1`) {
		t.Errorf("expected open=1 after transition, got:\n%s", text)
	}
	if !strings.Contains(text, `argus_circuit_breaker_state{operator_id="op-1",state="closed"} 0`) {
		t.Errorf("expected closed=0 after transition to open, got:\n%s", text)
	}

	reg.SetCircuitBreakerState("op-1", "half_open")
	text = scrape()
	if !strings.Contains(text, `argus_circuit_breaker_state{operator_id="op-1",state="half_open"} 1`) {
		t.Errorf("expected half_open=1, got:\n%s", text)
	}
}

// Compile-time: confirm PromAAARecorder satisfies the aaa/radius-shaped
// interface used by the server to swap implementations.
var _ metrics.AAARecorder = (*metrics.PromAAARecorder)(nil)
var _ metrics.AAARecorder = (*metrics.CompositeMetricsRecorder)(nil)
