package eap

import (
	"context"
	"io"
	"testing"

	"github.com/rs/zerolog"
)

func cacheTestLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

func TestCachedVectorProvider_NilRedis_Passthrough(t *testing.T) {
	inner := NewMockVectorProvider()
	cached := NewCachedVectorProvider(inner, nil, cacheTestLogger())

	ctx := context.Background()

	triplets, err := cached.GetSIMTriplets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("GetSIMTriplets error: %v", err)
	}
	if triplets == nil {
		t.Fatal("triplets is nil")
	}

	emptyRAND := [16]byte{}
	for i := 0; i < 3; i++ {
		if triplets.RAND[i] == emptyRAND {
			t.Errorf("RAND[%d] is all zeros", i)
		}
	}
}

func TestCachedVectorProvider_NilRedis_QuintetsPassthrough(t *testing.T) {
	inner := NewMockVectorProvider()
	cached := NewCachedVectorProvider(inner, nil, cacheTestLogger())

	ctx := context.Background()

	quintets, err := cached.GetAKAQuintets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("GetAKAQuintets error: %v", err)
	}
	if quintets == nil {
		t.Fatal("quintets is nil")
	}

	emptyRAND := [16]byte{}
	if quintets.RAND == emptyRAND {
		t.Error("RAND is all zeros")
	}
}

func TestCachedVectorProvider_CustomOptions(t *testing.T) {
	inner := NewMockVectorProvider()
	cached := NewCachedVectorProvider(inner, nil, cacheTestLogger(),
		WithBatchSize(5),
		WithVectorTTL(60000000000),
	)

	if cached.batchSize != 5 {
		t.Errorf("batchSize = %d, want 5", cached.batchSize)
	}
	if cached.ttl != 60000000000 {
		t.Errorf("ttl = %v, want 60s", cached.ttl)
	}
}

func TestCachedVectorProvider_Consistency(t *testing.T) {
	inner := NewMockVectorProvider()
	cached := NewCachedVectorProvider(inner, nil, cacheTestLogger())

	ctx := context.Background()

	directTriplets, err := inner.GetSIMTriplets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("direct GetSIMTriplets error: %v", err)
	}

	cachedTriplets, err := cached.GetSIMTriplets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("cached GetSIMTriplets error: %v", err)
	}

	if directTriplets.RAND != cachedTriplets.RAND {
		t.Error("cached triplets should match direct triplets")
	}

	directQuintets, err := inner.GetAKAQuintets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("direct GetAKAQuintets error: %v", err)
	}

	cachedQuintets, err := cached.GetAKAQuintets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("cached GetAKAQuintets error: %v", err)
	}

	if directQuintets.RAND != cachedQuintets.RAND {
		t.Error("cached quintets should match direct quintets")
	}
}
