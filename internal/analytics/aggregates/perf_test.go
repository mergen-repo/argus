package aggregates

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func TestAggregates_CacheHit_P95Under50ms(t *testing.T) {
	if testing.Short() {
		t.Skip("perf test skipped with -short")
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	inner := newFakeAggregates()
	inner.simCountByOperator = map[uuid.UUID]int{uuid.New(): 42}

	cached := &cachedAggregates{
		inner:  inner,
		rdb:    rdb,
		ttl:    60 * time.Second,
		reg:    nil,
		logger: zerolog.Nop(),
	}

	ctx := context.Background()
	tid := uuid.New()

	if _, err := cached.SIMCountByOperator(ctx, tid); err != nil {
		t.Fatalf("prime: %v", err)
	}

	const N = 1000
	samples := make([]time.Duration, 0, N)
	for i := 0; i < N; i++ {
		start := time.Now()
		if _, err := cached.SIMCountByOperator(ctx, tid); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		samples = append(samples, time.Since(start))
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[int(0.95*float64(N))]
	t.Logf("p95 latency: %v (samples: min=%v, median=%v, max=%v)",
		p95, samples[0], samples[N/2], samples[N-1])

	if p95 > 50*time.Millisecond {
		t.Errorf("AC-6 FAIL: p95 cache-hit latency %v exceeds 50ms target", p95)
	}
}

func BenchmarkCachedAggregates_SIMCountByOperator(b *testing.B) {
	mr := miniredis.RunT(b)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	inner := newFakeAggregates()
	inner.simCountByOperator = map[uuid.UUID]int{uuid.New(): 42}

	cached := &cachedAggregates{
		inner:  inner,
		rdb:    rdb,
		ttl:    60 * time.Second,
		reg:    nil,
		logger: zerolog.Nop(),
	}

	ctx := context.Background()
	tid := uuid.New()

	_, _ = cached.SIMCountByOperator(ctx, tid)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cached.SIMCountByOperator(ctx, tid)
	}
}
