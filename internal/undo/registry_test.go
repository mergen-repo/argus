package undo_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/btopcu/argus/internal/undo"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry(t *testing.T) (*undo.Registry, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return undo.NewRegistry(rdb), mr
}

func TestRegisterConsume_HappyPath(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	tenantID := uuid.New()
	userID := uuid.New()
	type payload struct {
		SIMIDs []string `json:"sim_ids"`
	}
	p := payload{SIMIDs: []string{"sim-1", "sim-2"}}

	actionID, err := reg.Register(ctx, tenantID, userID, "bulk_suspend", p)
	require.NoError(t, err)
	assert.NotEmpty(t, actionID)

	entry, err := reg.Consume(ctx, tenantID, actionID)
	require.NoError(t, err)
	assert.Equal(t, "bulk_suspend", entry.Action)
	assert.Equal(t, tenantID, entry.TenantID)
	assert.Equal(t, userID, entry.UserID)

	var got payload
	require.NoError(t, json.Unmarshal(entry.Payload, &got))
	assert.Equal(t, p.SIMIDs, got.SIMIDs)
}

func TestConsume_Idempotent(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	tenantID := uuid.New()
	actionID, err := reg.Register(ctx, tenantID, uuid.New(), "test", map[string]string{})
	require.NoError(t, err)

	_, err = reg.Consume(ctx, tenantID, actionID)
	require.NoError(t, err)

	_, err = reg.Consume(ctx, tenantID, actionID)
	assert.ErrorIs(t, err, undo.ErrExpired)
}

func TestConsume_Expiry(t *testing.T) {
	reg, mr := newTestRegistry(t)
	ctx := context.Background()

	tenantID := uuid.New()
	actionID, err := reg.Register(ctx, tenantID, uuid.New(), "test", nil)
	require.NoError(t, err)

	mr.FastForward(20 * time.Second)

	_, err = reg.Consume(ctx, tenantID, actionID)
	assert.ErrorIs(t, err, undo.ErrExpired)
}

func TestConsume_CrossTenantRejected(t *testing.T) {
	reg, _ := newTestRegistry(t)
	ctx := context.Background()

	tenantID := uuid.New()
	otherTenant := uuid.New()

	actionID, err := reg.Register(ctx, tenantID, uuid.New(), "test", nil)
	require.NoError(t, err)

	_, err = reg.Consume(ctx, otherTenant, actionID)
	assert.ErrorIs(t, err, undo.ErrTenantDenied)
}
