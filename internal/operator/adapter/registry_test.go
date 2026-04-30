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

func TestRegistryCreateHTTPAdapter(t *testing.T) {
	r := NewRegistry()
	cfg := json.RawMessage(`{"base_url":"http://example.com"}`)
	a, err := r.CreateAdapter("http", cfg)
	if err != nil {
		t.Fatalf("create http adapter: %v", err)
	}
	if a.Type() != "http" {
		t.Errorf("type = %q, want %q", a.Type(), "http")
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

// TestRegistryMultiProtocolPerOperator (Wave 2 Task 3, AC-2): a single
// operatorID must be able to host concurrent adapter instances for
// distinct protocols — distinct Set/Get lookups return distinct
// adapters without collision.
func TestRegistryMultiProtocolPerOperator(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	mockAdapter, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	httpAdapter, _ := NewHTTPAdapter(json.RawMessage(`{"base_url":"http://example.com"}`))
	r.Set(opID, "mock", mockAdapter)
	r.Set(opID, "http", httpAdapter)

	gotMock, ok := r.Get(opID, "mock")
	if !ok {
		t.Fatal("expected mock adapter to be found")
	}
	if gotMock.Type() != "mock" {
		t.Errorf("mock lookup returned %q adapter", gotMock.Type())
	}

	gotHTTP, ok := r.Get(opID, "http")
	if !ok {
		t.Fatal("expected http adapter to be found")
	}
	if gotHTTP.Type() != "http" {
		t.Errorf("http lookup returned %q adapter", gotHTTP.Type())
	}

	// Both adapters must coexist in the all-for-operator enumeration.
	all := r.GetAllForOperator(opID)
	if len(all) != 2 {
		t.Errorf("GetAllForOperator returned %d adapters, want 2", len(all))
	}
	if _, ok := all["mock"]; !ok {
		t.Error("missing mock from GetAllForOperator")
	}
	if _, ok := all["http"]; !ok {
		t.Error("missing http from GetAllForOperator")
	}
}

func TestRegistrySetAndGet(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	mock, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	r.Set(opID, "mock", mock)

	a, ok := r.Get(opID, "mock")
	if !ok {
		t.Fatal("expected adapter to be found")
	}
	if a.Type() != "mock" {
		t.Errorf("type = %q, want %q", a.Type(), "mock")
	}
}

func TestRegistryRemove_AllProtocols(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	mock, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	httpA, _ := NewHTTPAdapter(json.RawMessage(`{"base_url":"http://example.com"}`))
	r.Set(opID, "mock", mock)
	r.Set(opID, "http", httpA)

	r.Remove(opID)

	if _, ok := r.Get(opID, "mock"); ok {
		t.Error("expected mock adapter removed after Remove")
	}
	if _, ok := r.Get(opID, "http"); ok {
		t.Error("expected http adapter removed after Remove")
	}
	if len(r.GetAllForOperator(opID)) != 0 {
		t.Error("GetAllForOperator should be empty after Remove")
	}
}

func TestRegistryRemoveProtocol_Scoped(t *testing.T) {
	r := NewRegistry()
	opID := uuid.New()

	mock, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	httpA, _ := NewHTTPAdapter(json.RawMessage(`{"base_url":"http://example.com"}`))
	r.Set(opID, "mock", mock)
	r.Set(opID, "http", httpA)

	r.RemoveProtocol(opID, "mock")

	if _, ok := r.Get(opID, "mock"); ok {
		t.Error("expected mock adapter removed")
	}
	if _, ok := r.Get(opID, "http"); !ok {
		t.Error("http adapter should survive mock-scoped removal")
	}
}

func TestRegistryRemoveIsolationAcrossOperators(t *testing.T) {
	r := NewRegistry()
	op1 := uuid.New()
	op2 := uuid.New()

	mock1, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":1}`))
	mock2, _ := NewMockAdapter(json.RawMessage(`{"latency_ms":2}`))
	r.Set(op1, "mock", mock1)
	r.Set(op2, "mock", mock2)

	r.Remove(op1)

	if _, ok := r.Get(op1, "mock"); ok {
		t.Error("op1 mock should be removed")
	}
	if _, ok := r.Get(op2, "mock"); !ok {
		t.Error("op2 mock should be isolated from op1 removal")
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
	if !r.HasFactory("http") {
		t.Error("expected http factory to exist")
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
			r.Set(opID, "mock", mock)

			_, _ = r.Get(opID, "mock")
			_, _ = r.GetOrCreate(uuid.New(), "mock", json.RawMessage(`{"latency_ms":1}`))
			r.Remove(opID)
		}(i)
	}
	wg.Wait()
}
