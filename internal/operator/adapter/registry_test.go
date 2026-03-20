package adapter

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestRegistryCreateMockAdapter(t *testing.T) {
	r := NewRegistry()
	a, err := r.CreateAdapter("mock", json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("create mock adapter: %v", err)
	}
	if a.Type() != "mock" {
		t.Errorf("type = %q, want %q", a.Type(), "mock")
	}
}

func TestRegistryCreateRADIUSAdapter(t *testing.T) {
	r := NewRegistry()
	cfg := json.RawMessage(`{"host":"127.0.0.1","shared_secret":"testing123"}`)
	a, err := r.CreateAdapter("radius", cfg)
	if err != nil {
		t.Fatalf("create radius adapter: %v", err)
	}
	if a.Type() != "radius" {
		t.Errorf("type = %q, want %q", a.Type(), "radius")
	}
}

func TestRegistryCreateDiameterAdapter(t *testing.T) {
	r := NewRegistry()
	cfg := json.RawMessage(`{"host":"127.0.0.1","port":3868}`)
	a, err := r.CreateAdapter("diameter", cfg)
	if err != nil {
		t.Fatalf("create diameter adapter: %v", err)
	}
	if a.Type() != "diameter" {
		t.Errorf("type = %q, want %q", a.Type(), "diameter")
	}
}

func TestRegistryUnsupportedProtocol(t *testing.T) {
	r := NewRegistry()
	_, err := r.CreateAdapter("sba_v99", nil)
	if err == nil {
		t.Fatal("expected error for unsupported protocol")
	}
	if !errors.Is(err, ErrUnsupportedProtocol) {
		t.Errorf("error = %v, want ErrUnsupportedProtocol", err)
	}
}

func TestRegistryGetOrCreate(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	a1, err := r.GetOrCreate(opID, "mock", json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("get or create: %v", err)
	}
	if a1.Type() != "mock" {
		t.Errorf("type = %q, want %q", a1.Type(), "mock")
	}

	a2, err := r.GetOrCreate(opID, "mock", json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("get or create (second): %v", err)
	}
	if a1 != a2 {
		t.Error("expected same adapter instance on second GetOrCreate call")
	}
}

func TestRegistrySetAndGet(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	mock, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	r.Set(opID, mock)

	a, ok := r.Get(opID)
	if !ok {
		t.Fatal("expected adapter to be found")
	}
	if a.Type() != "mock" {
		t.Errorf("type = %q, want %q", a.Type(), "mock")
	}
}

func TestRegistryRemove(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	mock, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	r.Set(opID, mock)

	r.Remove(opID)

	_, ok := r.Get(opID)
	if ok {
		t.Error("expected adapter to be removed")
	}
}

func TestRegistryHasFactory(t *testing.T) {
	r := NewRegistry()

	if !r.HasFactory("mock") {
		t.Error("expected mock factory to exist")
	}
	if !r.HasFactory("radius") {
		t.Error("expected radius factory to exist")
	}
	if !r.HasFactory("diameter") {
		t.Error("expected diameter factory to exist")
	}
	if r.HasFactory("nonexistent") {
		t.Error("expected nonexistent factory to not exist")
	}
}

func TestRegistryRegisterCustomFactory(t *testing.T) {
	r := NewRegistry()
	r.RegisterFactory("custom", func(cfg json.RawMessage) (Adapter, error) {
		return NewMockAdapter(cfg)
	})

	if !r.HasFactory("custom") {
		t.Error("expected custom factory to exist after registration")
	}

	a, err := r.CreateAdapter("custom", json.RawMessage(`{"latency_ms":1}`))
	if err != nil {
		t.Fatalf("create custom adapter: %v", err)
	}
	if a.Type() != "mock" {
		t.Errorf("type = %q, want %q", a.Type(), "mock")
	}
}

func TestRegistryCreateSBAAdapter(t *testing.T) {
	r := NewRegistry()
	cfg := json.RawMessage(`{"host":"127.0.0.1","port":8443}`)
	a, err := r.CreateAdapter("sba", cfg)
	if err != nil {
		t.Fatalf("create sba adapter: %v", err)
	}
	if a.Type() != "sba" {
		t.Errorf("type = %q, want %q", a.Type(), "sba")
	}
}

func TestRegistryHasFactorySBA(t *testing.T) {
	r := NewRegistry()
	if !r.HasFactory("sba") {
		t.Error("expected sba factory to exist")
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			opID := uuid.New()

			mock, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
			r.Set(opID, mock)

			_, _ = r.Get(opID)
			_, _ = r.GetOrCreate(uuid.New(), "mock", json.RawMessage(`{"latency_ms":1}`))
			r.Remove(opID)
		}(i)
	}
	wg.Wait()
}
