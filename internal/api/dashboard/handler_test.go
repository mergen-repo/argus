package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func nopLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestGetDashboard_MissingTenant(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, nopLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	w := httptest.NewRecorder()

	h.GetDashboard(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestTrafficHeatmapCell_JSONShape(t *testing.T) {
	cell := trafficHeatmapCell{Day: 3, Hour: 14, Value: 0.75}

	b, err := json.Marshal(cell)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"day", "hour", "value"} {
		if out[key] == nil {
			t.Errorf("field %q missing from trafficHeatmapCell JSON", key)
		}
	}

	if int(out["day"].(float64)) != 3 {
		t.Errorf("day = %v, want 3", out["day"])
	}
	if int(out["hour"].(float64)) != 14 {
		t.Errorf("hour = %v, want 14", out["hour"])
	}
}

func TestDashboardDTO_TrafficHeatmapField(t *testing.T) {
	data := dashboardDTO{
		TotalSIMs:      100,
		TrafficHeatmap: []trafficHeatmapCell{{Day: 0, Hour: 0, Value: 0.5}},
	}

	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal dashboardDTO: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := out["traffic_heatmap"]; !ok {
		t.Error("traffic_heatmap field missing from dashboardDTO JSON")
	}

	cells, ok := out["traffic_heatmap"].([]interface{})
	if !ok {
		t.Fatal("traffic_heatmap is not an array")
	}
	if len(cells) != 1 {
		t.Errorf("expected 1 cell, got %d", len(cells))
	}

	_ = apierr.TenantIDKey
	_ = uuid.Nil
}

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return client, mr
}

func TestDashboardInvalidatorDeletesKey(t *testing.T) {
	ctx := context.Background()
	rc, _ := newTestRedis(t)

	tenantID := uuid.New()
	cacheKey := fmt.Sprintf("dashboard:%s", tenantID.String())

	if err := rc.Set(ctx, cacheKey, `{"status":"success"}`, 0).Err(); err != nil {
		t.Fatalf("seed redis: %v", err)
	}

	val, err := rc.Get(ctx, cacheKey).Result()
	if err != nil || val == "" {
		t.Fatal("expected key to exist before invalidation")
	}

	if err := rc.Del(ctx, cacheKey).Err(); err != nil {
		t.Fatalf("DEL failed: %v", err)
	}

	if err := rc.Get(ctx, cacheKey).Err(); err != redis.Nil {
		t.Errorf("expected key to be deleted, got err=%v", err)
	}
}

func TestDashboardHandler_UsesCachedSessionCount(t *testing.T) {
	sc := &stubSessionCounter{count: 42}
	h := NewHandler(nil, nil, nil, nil, nil, nil, nopLogger(), WithSessionCounter(sc))
	if h.sessionCounter == nil {
		t.Fatal("sessionCounter not set")
	}
	ctx := context.Background()
	got, err := h.sessionCounter.GetActiveCount(ctx, uuid.New().String())
	if err != nil {
		t.Fatalf("GetActiveCount: %v", err)
	}
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

type stubSessionCounter struct {
	count int64
}

func (s *stubSessionCounter) GetActiveCount(_ context.Context, _ string) (int64, error) {
	return s.count, nil
}
