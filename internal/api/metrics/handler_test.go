package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	analyticmetrics "github.com/btopcu/argus/internal/analytics/metrics"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available: %v", err)
	}
	client.FlushDB(ctx)
	t.Cleanup(func() {
		client.FlushDB(ctx)
		client.Close()
	})
	return client
}

func TestGetSystemMetrics_EmptyResponse(t *testing.T) {
	rdb := newTestRedis(t)
	c := analyticmetrics.NewCollector(rdb, zerolog.Nop())
	h := NewHandler(c, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/metrics", nil)
	w := httptest.NewRecorder()

	h.GetSystemMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["status"] != "success" {
		t.Errorf("status = %v, want success", resp["status"])
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data should be an object")
	}

	if data["system_status"] != "healthy" {
		t.Errorf("system_status = %v, want healthy", data["system_status"])
	}
}

func TestGetSystemMetrics_WithData(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()
	c := analyticmetrics.NewCollector(rdb, zerolog.Nop())

	opID := uuid.New()
	c.SetOperatorIDs([]uuid.UUID{opID})

	for i := 0; i < 50; i++ {
		c.RecordAuth(ctx, opID, true, 5)
	}
	for i := 0; i < 10; i++ {
		c.RecordAuth(ctx, opID, false, 50)
	}

	h := NewHandler(c, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/metrics", nil)
	w := httptest.NewRecorder()

	h.GetSystemMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data := resp["data"].(map[string]interface{})

	byOp, ok := data["by_operator"].(map[string]interface{})
	if !ok {
		t.Fatal("by_operator should be a map")
	}

	if _, exists := byOp[opID.String()]; !exists {
		t.Error("operator metrics should be present")
	}
}
