package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
