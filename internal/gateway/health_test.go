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

func doReady(t *testing.T, h *HealthHandler) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rr := httptest.NewRecorder()
	h.Ready(rr, req)

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return rr.Code, body
}

func doLive(t *testing.T, h *HealthHandler) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	rr := httptest.NewRecorder()
	h.Live(rr, req)

	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return rr.Code, body
}

func doStartup(t *testing.T, h *HealthHandler) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/health/startup", nil)
	rr := httptest.NewRecorder()
	h.Startup(rr, req)

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

func TestLive_Always200_NoDependencies(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})
	code, body := doLive(t, h)

	if code != http.StatusOK {
		t.Errorf("live status = %d, want 200", code)
	}
	if body["status"] != "success" {
		t.Errorf("live body status = %q, want success", body["status"])
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing or wrong type")
	}
	if data["status"] != "alive" {
		t.Errorf("data.status = %q, want alive", data["status"])
	}
	if _, ok := data["uptime"].(string); !ok {
		t.Error("data.uptime missing or wrong type")
	}
	if _, ok := data["goroutines"].(float64); !ok {
		t.Error("data.goroutines missing or wrong type")
	}
	if _, ok := data["go_version"].(string); !ok {
		t.Error("data.go_version missing or wrong type")
	}
}

func TestLive_Returns200_WhenAllDepsFail(t *testing.T) {
	h := newTestHealthHandler(
		&failChecker{msg: "db down"},
		&failChecker{msg: "redis down"},
		&failChecker{msg: "nats down"},
	)
	code, _ := doLive(t, h)
	if code != http.StatusOK {
		t.Errorf("live status = %d, want 200 even when deps fail", code)
	}
}

func TestReady_503_WhenDBProbeErrors(t *testing.T) {
	h := newTestHealthHandler(
		&failChecker{msg: "db connection refused"},
		&passChecker{},
		&passChecker{},
	)
	code, body := doReady(t, h)

	if code != http.StatusServiceUnavailable {
		t.Errorf("ready status = %d, want 503", code)
	}
	if body["status"] != "error" {
		t.Errorf("body status = %q, want error", body["status"])
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing")
	}
	if data["state"] != "unhealthy" {
		t.Errorf("state = %q, want unhealthy", data["state"])
	}
}

func TestReady_200_Degraded_WhenDiskAtDegradedThreshold(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})
	h.SetDiskConfig([]string{"/tmp"}, 0, 95)

	code, body := doReady(t, h)

	if code != http.StatusOK {
		t.Errorf("ready status = %d, want 200 for degraded", code)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing")
	}
	state, _ := data["state"].(string)
	if state != "degraded" && state != "healthy" {
		t.Errorf("state = %q, want degraded (or healthy if disk < threshold)", state)
	}
}

func TestReady_200_Degraded_AAA_Partial(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})
	h.SetAAAChecker(&mockAAAHealthy{})
	h.SetDiameterChecker(&mockDiameterUnhealthy{})

	code, body := doReady(t, h)

	if code != http.StatusOK {
		t.Errorf("ready status = %d, want 200 for partial AAA", code)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing")
	}
	if data["state"] != "degraded" {
		t.Errorf("state = %q, want degraded for partial AAA", data["state"])
	}
	reasons, ok := data["degraded_reasons"].([]interface{})
	if !ok || len(reasons) == 0 {
		t.Error("expected degraded_reasons to be set")
	}
}

type mockAAAHealthy struct{}

func (m *mockAAAHealthy) Healthy() bool                                    { return true }
func (m *mockAAAHealthy) ActiveSessionCount(_ context.Context) (int64, error) { return 0, nil }

type mockDiameterUnhealthy struct{}

func (m *mockDiameterUnhealthy) Healthy() bool                                    { return false }
func (m *mockDiameterUnhealthy) ActiveSessionCount(_ context.Context) (int64, error) { return 0, nil }

func TestStartup_LatchesPermanentlyAfterFirstSuccess(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})

	code, body := doStartup(t, h)
	if code != http.StatusOK {
		t.Errorf("startup code = %d, want 200", code)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data missing")
	}
	if data["state"] != "started" {
		t.Errorf("state = %q, want started", data["state"])
	}

	code2, body2 := doStartup(t, h)
	if code2 != http.StatusOK {
		t.Errorf("second startup code = %d, want 200 (latched)", code2)
	}
	data2, _ := body2["data"].(map[string]interface{})
	if data2["state"] != "started" {
		t.Errorf("second startup state = %q, want started", data2["state"])
	}
}

func TestStartup_Returns503_After3TransientFailures_WithinFirstMinute(t *testing.T) {
	h := newTestHealthHandler(
		&failChecker{msg: "db down"},
		&passChecker{},
		&passChecker{},
	)

	for i := 1; i <= 3; i++ {
		code, _ := doStartup(t, h)
		if code != http.StatusServiceUnavailable {
			t.Errorf("startup attempt %d: code = %d, want 503", i, code)
		}
	}

	code4, body4 := doStartup(t, h)
	if code4 != http.StatusOK {
		t.Errorf("4th startup attempt code = %d, want 200 (latched after 3 failures)", code4)
	}
	data, _ := body4["data"].(map[string]interface{})
	if data["state"] != "started" {
		t.Errorf("state after latch = %q, want started", data["state"])
	}
}

func TestReady_StateField_Present(t *testing.T) {
	h := newTestHealthHandler(&passChecker{}, &passChecker{}, &passChecker{})
	_, body := doReady(t, h)

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data field missing")
	}
	if data["state"] != "healthy" {
		t.Errorf("state = %q, want healthy", data["state"])
	}
}
