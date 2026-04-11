package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type passChecker struct{}

func (p *passChecker) HealthCheck(_ context.Context) error { return nil }

type failChecker struct{ msg string }

func (f *failChecker) HealthCheck(_ context.Context) error {
	return errors.New(f.msg)
}

type slowChecker struct{}

func (s *slowChecker) HealthCheck(ctx context.Context) error {
	select {
	case <-time.After(3 * time.Second):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newTestHealthHandler(db, redis, nats HealthChecker) *HealthHandler {
	return NewHealthHandler(db, redis, nats)
}

func doHealthCheck(t *testing.T, h *HealthHandler) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	h.Check(rr, req)

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return rr.Code, body
}

func TestHealthHandler_AllPass_Returns200(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})
	code, body := doHealthCheck(t, h)

	if code != http.StatusOK {
		t.Errorf("status = %d, want 200", code)
	}
	if body["status"] != "success" {
		t.Errorf("status = %q, want success", body["status"])
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing or wrong type")
	}

	for _, probe := range []string{"db", "redis", "nats"} {
		p, ok := data[probe].(map[string]interface{})
		if !ok {
			t.Fatalf("probe %q missing or wrong type", probe)
		}
		if p["status"] != "ok" {
			t.Errorf("probe %q status = %q, want ok", probe, p["status"])
		}
		latency, ok := p["latency_ms"].(float64)
		if !ok {
			t.Errorf("probe %q latency_ms missing or wrong type", probe)
		}
		if latency < 0 {
			t.Errorf("probe %q latency_ms = %v, want >= 0", probe, latency)
		}
	}
}

func TestHealthHandler_OneFailure_Returns503(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &failChecker{msg: "redis down"}, &passChecker{})
	code, body := doHealthCheck(t, h)

	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", code)
	}
	if body["status"] != "error" {
		t.Errorf("status = %q, want error", body["status"])
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing or wrong type")
	}

	dbProbe, ok := data["db"].(map[string]interface{})
	if !ok {
		t.Fatal("db probe missing")
	}
	if dbProbe["status"] != "ok" {
		t.Errorf("db probe status = %q, want ok", dbProbe["status"])
	}

	redisProbe, ok := data["redis"].(map[string]interface{})
	if !ok {
		t.Fatal("redis probe missing")
	}
	if !strings.HasPrefix(redisProbe["status"].(string), "error:") {
		t.Errorf("redis probe status = %q, want error: prefix", redisProbe["status"])
	}
	if redisProbe["error"] != "redis down" {
		t.Errorf("redis probe error = %q, want redis down", redisProbe["error"])
	}
}

func TestHealthHandler_SlowProbe_Returns503WithTimeout(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &slowChecker{}, &passChecker{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	start := time.Now()
	h.Check(rr, req)
	elapsed := time.Since(start)

	if elapsed > 4*time.Second {
		t.Errorf("handler took too long: %v (expected around 2s timeout)", elapsed)
	}

	code := rr.Code
	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "error" {
		t.Errorf("status = %q, want error", body["status"])
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing")
	}

	redisProbe, ok := data["redis"].(map[string]interface{})
	if !ok {
		t.Fatal("redis probe missing")
	}
	errMsg, _ := redisProbe["error"].(string)
	if errMsg == "" {
		t.Error("expected timeout error in redis probe, got empty string")
	}
	if !strings.Contains(errMsg, "context") && !strings.Contains(errMsg, "deadline") && !strings.Contains(errMsg, "cancel") {
		t.Errorf("redis probe error = %q, expected context/deadline/cancel error", errMsg)
	}
}

func TestHealthHandler_AllFail_Returns503(t *testing.T) {
	h := newTestHealthHandler(
		&failChecker{msg: "db error"},
		&failChecker{msg: "redis error"},
		&failChecker{msg: "nats error"},
	)
	code, body := doHealthCheck(t, h)

	if code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", code)
	}

	errObj, ok := body["error"].(map[string]interface{})
	if !ok {
		t.Fatal("error field missing or wrong type")
	}
	if errObj["code"] != "SERVICE_UNAVAILABLE" {
		t.Errorf("error code = %q, want SERVICE_UNAVAILABLE", errObj["code"])
	}
}

func TestHealthHandler_UptimeField_Present(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})
	_, body := doHealthCheck(t, h)

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing")
	}
	uptime, ok := data["uptime"].(string)
	if !ok || uptime == "" {
		t.Error("uptime field missing or empty")
	}
}
