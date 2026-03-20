package eap

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/rs/zerolog"
)

func adapterTestLogger() zerolog.Logger {
	return zerolog.New(io.Discard)
}

func TestAdapterVectorProvider_GetSIMTriplets(t *testing.T) {
	mockAdapter, err := adapter.NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("NewMockAdapter error: %v", err)
	}

	provider := NewAdapterVectorProvider(mockAdapter, adapterTestLogger())
	ctx := context.Background()

	triplets, err := provider.GetSIMTriplets(ctx, "286010123456789")
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

	emptySRES := [4]byte{}
	for i := 0; i < 3; i++ {
		if triplets.SRES[i] == emptySRES {
			t.Errorf("SRES[%d] is all zeros", i)
		}
	}

	emptyKc := [8]byte{}
	for i := 0; i < 3; i++ {
		if triplets.Kc[i] == emptyKc {
			t.Errorf("Kc[%d] is all zeros", i)
		}
	}
}

func TestAdapterVectorProvider_GetAKAQuintets(t *testing.T) {
	mockAdapter, err := adapter.NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("NewMockAdapter error: %v", err)
	}

	provider := NewAdapterVectorProvider(mockAdapter, adapterTestLogger())
	ctx := context.Background()

	quintets, err := provider.GetAKAQuintets(ctx, "286010123456789")
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

	emptyAUTN := [16]byte{}
	if quintets.AUTN == emptyAUTN {
		t.Error("AUTN is all zeros")
	}

	if len(quintets.XRES) == 0 {
		t.Error("XRES is empty")
	}

	emptyCK := [16]byte{}
	if quintets.CK == emptyCK {
		t.Error("CK is all zeros")
	}

	emptyIK := [16]byte{}
	if quintets.IK == emptyIK {
		t.Error("IK is all zeros")
	}
}

func TestAdapterVectorProvider_TripletConversionDeterministic(t *testing.T) {
	mockAdapter, err := adapter.NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("NewMockAdapter error: %v", err)
	}

	provider := NewAdapterVectorProvider(mockAdapter, adapterTestLogger())
	ctx := context.Background()

	t1, err := provider.GetSIMTriplets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("first GetSIMTriplets error: %v", err)
	}

	t2, err := provider.GetSIMTriplets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("second GetSIMTriplets error: %v", err)
	}

	if t1.RAND != t2.RAND {
		t.Error("triplets should be deterministic for same IMSI")
	}
}

func TestAdapterVectorProvider_QuintetConversionDeterministic(t *testing.T) {
	mockAdapter, err := adapter.NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("NewMockAdapter error: %v", err)
	}

	provider := NewAdapterVectorProvider(mockAdapter, adapterTestLogger())
	ctx := context.Background()

	q1, err := provider.GetAKAQuintets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("first GetAKAQuintets error: %v", err)
	}

	q2, err := provider.GetAKAQuintets(ctx, "286010123456789")
	if err != nil {
		t.Fatalf("second GetAKAQuintets error: %v", err)
	}

	if q1.RAND != q2.RAND {
		t.Error("quintets should be deterministic for same IMSI")
	}
}
