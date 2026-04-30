package cache_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/cache"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/redis/go-redis/v9"
)

func newTestClient(t *testing.T, addr string) *redis.Client {
	t.Helper()
	return redis.NewClient(&redis.Options{Addr: addr})
}

func scrapeMetrics(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("scrape metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestMetricsHook_IncrementsOnProcess(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	reg := metrics.NewRegistry()
	client := newTestClient(t, mr.Addr())
	defer client.Close()

	cache.RegisterRedisMetrics(client, reg)

	ctx := context.Background()

	if err := client.Set(ctx, "key1", "val1", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if err := client.Get(ctx, "key1").Err(); err != nil {
		t.Fatalf("GET: %v", err)
	}

	text := scrapeMetrics(t, reg)

	for _, want := range []string{
		`argus_redis_ops_total{op="set",result="success"}`,
		`argus_redis_ops_total{op="get",result="success"}`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics output missing %q\nfull output:\n%s", want, text)
		}
	}
}

func TestMetricsHook_IncrementsOnMiss(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	reg := metrics.NewRegistry()
	client := newTestClient(t, mr.Addr())
	defer client.Close()

	cache.RegisterRedisMetrics(client, reg)

	ctx := context.Background()

	err = client.Get(ctx, "nonexistent").Err()
	if err != redis.Nil {
		t.Fatalf("expected redis.Nil, got: %v", err)
	}

	text := scrapeMetrics(t, reg)

	want := `argus_redis_ops_total{op="get",result="success"}`
	if !strings.Contains(text, want) {
		t.Errorf("redis.Nil should be counted as success; missing %q\nfull output:\n%s", want, text)
	}
}

func TestRegisterRedisMetrics_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterRedisMetrics panicked: %v", r)
		}
	}()

	reg := metrics.NewRegistry()

	cache.RegisterRedisMetrics(nil, reg)
	cache.RegisterRedisMetrics(nil, nil)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	client := newTestClient(t, mr.Addr())
	defer client.Close()

	cache.RegisterRedisMetrics(client, nil)
}
