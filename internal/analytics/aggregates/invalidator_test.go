package aggregates

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func nopLogger() zerolog.Logger {
	return zerolog.Nop()
}

func newTestRedisForInvalidator(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

func tenantPayload(tenantID uuid.UUID) []byte {
	b, _ := json.Marshal(map[string]string{"tenant_id": tenantID.String()})
	return b
}

func seedKey(t *testing.T, rdb *redis.Client, key string) {
	t.Helper()
	if err := rdb.Set(context.Background(), key, "v", 0).Err(); err != nil {
		t.Fatalf("seed key %s: %v", key, err)
	}
}

func assertKeyGone(t *testing.T, mr *miniredis.Miniredis, key string) {
	t.Helper()
	if mr.Exists(key) {
		t.Errorf("expected key to be deleted, still present: %s", key)
	}
}

func assertKeyExists(t *testing.T, mr *miniredis.Miniredis, key string) {
	t.Helper()
	if !mr.Exists(key) {
		t.Errorf("expected key to still exist, but it was deleted: %s", key)
	}
}

// TestInvalidator_SIMUpdated_DeletesAllSIMKeys verifies that onSIMUpdated removes
// sim_count_by_tenant, sim_count_by_operator, sim_count_by_apn, sim_count_by_state,
// and any sim_count_by_policy:{policyID} keys for the tenant.
func TestInvalidator_SIMUpdated_DeletesAllSIMKeys(t *testing.T) {
	mr, rdb := newTestRedisForInvalidator(t)
	tid := uuid.New()
	policyID1 := uuid.New()
	policyID2 := uuid.New()

	staticKeys := []string{
		fmt.Sprintf("%s:%s:sim_count_by_tenant", keyPrefix, tid),
		fmt.Sprintf("%s:%s:sim_count_by_operator", keyPrefix, tid),
		fmt.Sprintf("%s:%s:sim_count_by_apn", keyPrefix, tid),
		fmt.Sprintf("%s:%s:sim_count_by_state", keyPrefix, tid),
	}
	policyKey1 := fmt.Sprintf("%s:%s:sim_count_by_policy:%s", keyPrefix, tid, policyID1)
	policyKey2 := fmt.Sprintf("%s:%s:sim_count_by_policy:%s", keyPrefix, tid, policyID2)

	for _, k := range staticKeys {
		seedKey(t, rdb, k)
	}
	seedKey(t, rdb, policyKey1)
	seedKey(t, rdb, policyKey2)

	inv := &invalidator{rdb: rdb, logger: nopLogger()}
	inv.onSIMUpdated(context.Background(), "argus.events.sim.updated", tenantPayload(tid))

	for _, k := range staticKeys {
		assertKeyGone(t, mr, k)
	}
	assertKeyGone(t, mr, policyKey1)
	assertKeyGone(t, mr, policyKey2)
}

// TestInvalidator_PolicyChanged_UnlinksPolicyKeys verifies that onPolicyChanged
// removes only policy-scoped keys for the tenant.
func TestInvalidator_PolicyChanged_UnlinksPolicyKeys(t *testing.T) {
	mr, rdb := newTestRedisForInvalidator(t)
	tid := uuid.New()
	policyID1 := uuid.New()
	policyID2 := uuid.New()

	policyKey1 := fmt.Sprintf("%s:%s:sim_count_by_policy:%s", keyPrefix, tid, policyID1)
	policyKey2 := fmt.Sprintf("%s:%s:sim_count_by_policy:%s", keyPrefix, tid, policyID2)
	otherKey := fmt.Sprintf("%s:%s:sim_count_by_tenant", keyPrefix, tid)

	seedKey(t, rdb, policyKey1)
	seedKey(t, rdb, policyKey2)
	seedKey(t, rdb, otherKey)

	inv := &invalidator{rdb: rdb, logger: nopLogger()}
	inv.onPolicyChanged(context.Background(), "argus.events.policy.changed", tenantPayload(tid))

	assertKeyGone(t, mr, policyKey1)
	assertKeyGone(t, mr, policyKey2)
	assertKeyExists(t, mr, otherKey)
}

// TestInvalidator_SessionActivity_DeletesSessionKeys verifies that onSessionActivity
// removes active_session_stats and traffic_by_operator but leaves SIM keys untouched.
func TestInvalidator_SessionActivity_DeletesSessionKeys(t *testing.T) {
	mr, rdb := newTestRedisForInvalidator(t)
	tid := uuid.New()

	sessionKey := fmt.Sprintf("%s:%s:active_session_stats", keyPrefix, tid)
	trafficKey := fmt.Sprintf("%s:%s:traffic_by_operator", keyPrefix, tid)
	simKey := fmt.Sprintf("%s:%s:sim_count_by_tenant", keyPrefix, tid)

	seedKey(t, rdb, sessionKey)
	seedKey(t, rdb, trafficKey)
	seedKey(t, rdb, simKey)

	inv := &invalidator{rdb: rdb, logger: nopLogger()}
	inv.onSessionActivity(context.Background(), "argus.events.session.started", tenantPayload(tid))

	assertKeyGone(t, mr, sessionKey)
	assertKeyGone(t, mr, trafficKey)
	assertKeyExists(t, mr, simKey)
}

// TestInvalidator_MissingTenantID_NoOp verifies that an event with no tenant_id
// does not delete any keys.
func TestInvalidator_MissingTenantID_NoOp(t *testing.T) {
	mr, rdb := newTestRedisForInvalidator(t)
	tid := uuid.New()

	key := fmt.Sprintf("%s:%s:sim_count_by_tenant", keyPrefix, tid)
	seedKey(t, rdb, key)

	inv := &invalidator{rdb: rdb, logger: nopLogger()}

	inv.onSIMUpdated(context.Background(), "argus.events.sim.updated", []byte(`{}`))
	inv.onSIMUpdated(context.Background(), "argus.events.sim.updated", []byte(`not-json`))
	inv.onPolicyChanged(context.Background(), "argus.events.policy.changed", []byte(`{}`))
	inv.onSessionActivity(context.Background(), "argus.events.session.started", []byte(`{}`))

	assertKeyExists(t, mr, key)
}

// TestInvalidator_TenantIsolation verifies that invalidation for tenant X
// does not affect tenant Y's cache keys.
func TestInvalidator_TenantIsolation(t *testing.T) {
	mr, rdb := newTestRedisForInvalidator(t)
	tidX := uuid.New()
	tidY := uuid.New()
	policyID := uuid.New()

	keyX := fmt.Sprintf("%s:%s:sim_count_by_tenant", keyPrefix, tidX)
	keyY := fmt.Sprintf("%s:%s:sim_count_by_tenant", keyPrefix, tidY)
	policyKeyX := fmt.Sprintf("%s:%s:sim_count_by_policy:%s", keyPrefix, tidX, policyID)
	policyKeyY := fmt.Sprintf("%s:%s:sim_count_by_policy:%s", keyPrefix, tidY, policyID)

	seedKey(t, rdb, keyX)
	seedKey(t, rdb, keyY)
	seedKey(t, rdb, policyKeyX)
	seedKey(t, rdb, policyKeyY)

	inv := &invalidator{rdb: rdb, logger: nopLogger()}
	inv.onSIMUpdated(context.Background(), "argus.events.sim.updated", tenantPayload(tidX))

	assertKeyGone(t, mr, keyX)
	assertKeyGone(t, mr, policyKeyX)
	assertKeyExists(t, mr, keyY)
	assertKeyExists(t, mr, policyKeyY)
}
