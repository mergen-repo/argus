package killswitch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuditor records calls to CreateEntry.
type stubAuditor struct {
	calls int
}

func (a *stubAuditor) CreateEntry(_ context.Context, _ interface{}) (interface{}, error) {
	a.calls++
	return nil, nil
}

func TestIsEnabled_UnknownKey_ReturnsFalse(t *testing.T) {
	svc := &Service{
		cache:    map[string]bool{"radius_auth": false},
		cachedAt: time.Now(),
		ttl:      defaultTTL,
	}
	assert.False(t, svc.IsEnabled("nonexistent_key"))
}

func TestIsEnabled_KnownKeyTrue(t *testing.T) {
	svc := &Service{
		cache:    map[string]bool{"bulk_operations": true},
		cachedAt: time.Now(),
		ttl:      defaultTTL,
	}
	assert.True(t, svc.IsEnabled("bulk_operations"))
}

func TestIsEnabled_KnownKeyFalse(t *testing.T) {
	svc := &Service{
		cache:    map[string]bool{"bulk_operations": false},
		cachedAt: time.Now(),
		ttl:      defaultTTL,
	}
	assert.False(t, svc.IsEnabled("bulk_operations"))
}

func TestGetAll_ReturnsSnapshot(t *testing.T) {
	svc := &Service{
		cache:    map[string]bool{"radius_auth": true, "bulk_operations": false},
		cachedAt: time.Now(),
		ttl:      defaultTTL,
	}
	all := svc.GetAll()
	require.Len(t, all, 2)
	assert.True(t, all["radius_auth"])
	assert.False(t, all["bulk_operations"])
}

func TestGetAll_MutatingReturnedMapDoesNotAffectCache(t *testing.T) {
	svc := &Service{
		cache:    map[string]bool{"radius_auth": false},
		cachedAt: time.Now(),
		ttl:      defaultTTL,
	}
	all := svc.GetAll()
	all["radius_auth"] = true

	assert.False(t, svc.IsEnabled("radius_auth"))
}
