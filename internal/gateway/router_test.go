package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metricsapi "github.com/btopcu/argus/internal/api/metrics"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/rs/zerolog"
)

func TestRouterBulkImportRouteRegistered(t *testing.T) {
	health := &HealthHandler{}
	router := NewRouterWithDeps(RouterDeps{
		Health:    health,
		JWTSecret: "test-secret",
		Logger:    zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sims/bulk/import", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when BulkHandler is nil, got %d", w.Code)
	}
}

func TestRouterJobRoutesRegistered(t *testing.T) {
	health := &HealthHandler{}
	router := NewRouterWithDeps(RouterDeps{
		Health:    health,
		JWTSecret: "test-secret",
		Logger:    zerolog.Nop(),
	})

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/jobs"},
		{http.MethodGet, "/api/v1/jobs/00000000-0000-0000-0000-000000000001"},
		{http.MethodPost, "/api/v1/jobs/00000000-0000-0000-0000-000000000001/retry"},
		{http.MethodPost, "/api/v1/jobs/00000000-0000-0000-0000-000000000001/cancel"},
		{http.MethodGet, "/api/v1/jobs/00000000-0000-0000-0000-000000000001/errors"},
	}

	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404 when JobHandler is nil, got %d", rt.method, rt.path, w.Code)
		}
	}
}

func TestRouterSessionRoutesNotRegisteredWhenNil(t *testing.T) {
	health := &HealthHandler{}
	router := NewRouterWithDeps(RouterDeps{
		Health:    health,
		JWTSecret: "test-secret",
		Logger:    zerolog.Nop(),
	})

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/sessions"},
		{http.MethodGet, "/api/v1/sessions/stats"},
		{http.MethodPost, "/api/v1/sessions/00000000-0000-0000-0000-000000000001/disconnect"},
		{http.MethodPost, "/api/v1/sessions/bulk/disconnect"},
	}

	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404 when SessionHandler is nil, got %d", rt.method, rt.path, w.Code)
		}
	}
}

func TestRouterMSISDNPoolRoutesRegistered(t *testing.T) {
	health := &HealthHandler{}
	router := NewRouterWithDeps(RouterDeps{
		Health:    health,
		JWTSecret: "test-secret",
		Logger:    zerolog.Nop(),
	})

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/msisdn-pool"},
		{http.MethodPost, "/api/v1/msisdn-pool/import"},
		{http.MethodPost, "/api/v1/msisdn-pool/00000000-0000-0000-0000-000000000001/assign"},
	}

	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, rt.path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404 when MSISDNHandler is nil, got %d", rt.method, rt.path, w.Code)
		}
	}
}

func TestRouter_MetricsEndpointReturnsPromFormat(t *testing.T) {
	reg := metrics.NewRegistry()
	reg.HTTPRequestsTotal.WithLabelValues("GET", "/probe", "200", "t0").Add(0)

	router := NewRouterWithDeps(RouterDeps{
		Health:     &HealthHandler{},
		JWTSecret:  "test-secret",
		Logger:     zerolog.Nop(),
		MetricsReg: reg,
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics: expected 200, got %d", w.Code)
	}

	body, err := io.ReadAll(w.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)

	for _, want := range []string{
		"# HELP argus_http_requests_total",
		"go_goroutines",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("GET /metrics: body missing %q\nfull body:\n%s", want, text)
		}
	}
}

func TestRouter_SystemMetricsEndpointStillJSON(t *testing.T) {
	router := NewRouterWithDeps(RouterDeps{
		Health:    &HealthHandler{},
		JWTSecret: "test-secret",
		Logger:    zerolog.Nop(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/v1/system/metrics with nil MetricsHandler: expected 404, got %d", w.Code)
	}
}

func TestRouter_SystemMetricsEndpointRouteRegistered(t *testing.T) {
	h := metricsapi.NewHandler(nil, zerolog.Nop())
	router := NewRouterWithDeps(RouterDeps{
		Health:         &HealthHandler{},
		JWTSecret:      "test-secret",
		Logger:         zerolog.Nop(),
		MetricsHandler: h,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Errorf("GET /api/v1/system/metrics with MetricsHandler set: expected route to be registered (not 404), got %d", w.Code)
	}
}
