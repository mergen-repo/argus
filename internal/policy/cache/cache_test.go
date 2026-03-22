package cache

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestCachePutGet(t *testing.T) {
	c := New(nil, zerolog.Nop())

	vid := uuid.New()
	pid := uuid.New()
	tid := uuid.New()
	cp := &dsl.CompiledPolicy{Name: "test-policy", Version: "1"}

	c.Put(vid, pid, tid, cp)

	got, ok := c.Get(vid)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Name != "test-policy" {
		t.Errorf("Name = %q, want %q", got.Name, "test-policy")
	}

	stats := c.GetStats()
	if stats.Hits != 1 {
		t.Errorf("Hits = %d, want 1", stats.Hits)
	}
}

func TestCacheMiss(t *testing.T) {
	c := New(nil, zerolog.Nop())

	_, ok := c.Get(uuid.New())
	if ok {
		t.Fatal("expected cache miss")
	}

	stats := c.GetStats()
	if stats.Misses != 1 {
		t.Errorf("Misses = %d, want 1", stats.Misses)
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := New(nil, zerolog.Nop())

	vid := uuid.New()
	pid := uuid.New()
	tid := uuid.New()
	cp := &dsl.CompiledPolicy{Name: "evict-me"}

	c.Put(vid, pid, tid, cp)

	if c.Len() != 1 {
		t.Fatalf("Len = %d, want 1", c.Len())
	}

	c.Invalidate(vid)

	if c.Len() != 0 {
		t.Fatalf("Len = %d after invalidate, want 0", c.Len())
	}

	_, ok := c.Get(vid)
	if ok {
		t.Fatal("expected cache miss after invalidation")
	}

	stats := c.GetStats()
	if stats.Evictions != 1 {
		t.Errorf("Evictions = %d, want 1", stats.Evictions)
	}
}

func TestCacheGetForTenant(t *testing.T) {
	c := New(nil, zerolog.Nop())

	tid := uuid.New()
	otherTid := uuid.New()

	for i := 0; i < 3; i++ {
		c.Put(uuid.New(), uuid.New(), tid, &dsl.CompiledPolicy{Name: "tenant-policy"})
	}
	c.Put(uuid.New(), uuid.New(), otherTid, &dsl.CompiledPolicy{Name: "other"})

	policies := c.GetForTenant(tid)
	if len(policies) != 3 {
		t.Errorf("GetForTenant returned %d policies, want 3", len(policies))
	}

	otherPolicies := c.GetForTenant(otherTid)
	if len(otherPolicies) != 1 {
		t.Errorf("GetForTenant(other) returned %d policies, want 1", len(otherPolicies))
	}
}

func TestCacheWarmUp(t *testing.T) {
	cp := &dsl.CompiledPolicy{
		Name:    "warm-policy",
		Version: "1",
		Rules: dsl.CompiledRules{
			Defaults: map[string]interface{}{"bandwidth_down": float64(10000000)},
		},
	}
	compiled, _ := json.Marshal(cp)

	vid := uuid.New()
	pid := uuid.New()
	tid := uuid.New()

	loader := &mockLoader{
		versions: []PolicyVersionRow{
			{VersionID: vid, PolicyID: pid, TenantID: tid, Compiled: compiled},
		},
	}

	c := New(loader, zerolog.Nop())
	if err := c.WarmUp(context.Background()); err != nil {
		t.Fatal(err)
	}

	got, ok := c.Get(vid)
	if !ok {
		t.Fatal("expected cache hit after warm-up")
	}
	if got.Name != "warm-policy" {
		t.Errorf("Name = %q, want %q", got.Name, "warm-policy")
	}
}

func TestHandleInvalidation(t *testing.T) {
	c := New(nil, zerolog.Nop())

	vid := uuid.New()
	c.Put(vid, uuid.New(), uuid.New(), &dsl.CompiledPolicy{Name: "will-be-invalidated"})

	evt := map[string]string{
		"version_id": vid.String(),
		"action":     "invalidate",
	}
	data, _ := json.Marshal(evt)
	c.handleInvalidation(data)

	_, ok := c.Get(vid)
	if ok {
		t.Fatal("expected cache miss after NATS invalidation")
	}
}

type mockLoader struct {
	versions []PolicyVersionRow
}

func (m *mockLoader) ListActiveVersions(_ context.Context) ([]PolicyVersionRow, error) {
	return m.versions, nil
}

func (m *mockLoader) GetCompiledByVersionID(_ context.Context, _ uuid.UUID) (*dsl.CompiledPolicy, error) {
	return nil, nil
}
