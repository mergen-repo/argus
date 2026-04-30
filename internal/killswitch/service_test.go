package killswitch

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func newTestService(envMap map[string]string) *Service {
	calls := 0
	svc := NewService(zerolog.Nop())
	svc.getenv = func(key string) string {
		_ = calls
		return envMap[key]
	}
	return svc
}

// TestIsEnabled_AllKnownKeys_EnvOn verifies every known key returns true when
// env is set to "on".
func TestIsEnabled_AllKnownKeys_EnvOn(t *testing.T) {
	keys := []string{"radius_auth", "session_create", "bulk_operations", "external_notifications", "read_only_mode"}
	for _, key := range keys {
		envKey := "KILLSWITCH_" + strings.ToUpper(key)
		svc := newTestService(map[string]string{envKey: "on"})
		assert.True(t, svc.IsEnabled(key), "key=%s env=on should return true", key)
	}
}

// TestIsEnabled_AllKnownKeys_EnvOff verifies every known key returns false when
// env is set to "off".
func TestIsEnabled_AllKnownKeys_EnvOff(t *testing.T) {
	keys := []string{"radius_auth", "session_create", "bulk_operations", "external_notifications", "read_only_mode"}
	for _, key := range keys {
		envKey := "KILLSWITCH_" + strings.ToUpper(key)
		svc := newTestService(map[string]string{envKey: "off"})
		assert.False(t, svc.IsEnabled(key), "key=%s env=off should return false", key)
	}
}

// TestIsEnabled_EnvVariants tests all accepted truthy/falsy env values.
func TestIsEnabled_EnvVariants(t *testing.T) {
	truthy := []string{"on", "true", "1"}
	falsy := []string{"off", "false", "0"}

	for _, v := range truthy {
		svc := newTestService(map[string]string{"KILLSWITCH_RADIUS_AUTH": v})
		assert.True(t, svc.IsEnabled("radius_auth"), "env=%q should return true", v)
	}
	for _, v := range falsy {
		svc := newTestService(map[string]string{"KILLSWITCH_RADIUS_AUTH": v})
		assert.False(t, svc.IsEnabled("radius_auth"), "env=%q should return false", v)
	}
}

// TestIsEnabled_Unset_UsesDefault verifies unset env falls back to
// enabledByDefault (all five keys default to false — switches off by default).
func TestIsEnabled_Unset_UsesDefault(t *testing.T) {
	keys := []string{"radius_auth", "session_create", "bulk_operations", "external_notifications", "read_only_mode"}
	for _, key := range keys {
		svc := newTestService(map[string]string{})
		assert.False(t, svc.IsEnabled(key), "key=%s unset should default to false (switch off)", key)
	}
}

// TestIsEnabled_CacheTTL_HitDoesNotCallGetenv verifies second read within TTL
// uses cached value without calling getenv.
func TestIsEnabled_CacheTTL_HitDoesNotCallGetenv(t *testing.T) {
	callCount := 0
	svc := NewService(zerolog.Nop())
	svc.getenv = func(key string) string {
		callCount++
		return "on"
	}
	now := time.Now()
	svc.clock = func() time.Time { return now }

	svc.IsEnabled("radius_auth")
	assert.Equal(t, 1, callCount, "first call should invoke getenv")

	svc.IsEnabled("radius_auth")
	assert.Equal(t, 1, callCount, "second call within TTL must NOT invoke getenv again")
}

// TestIsEnabled_CacheTTL_ExpiredTriggersReread verifies re-read after TTL expiry.
func TestIsEnabled_CacheTTL_ExpiredTriggersReread(t *testing.T) {
	callCount := 0
	svc := NewService(zerolog.Nop())
	svc.getenv = func(key string) string {
		callCount++
		return "on"
	}
	now := time.Now()
	svc.clock = func() time.Time { return now }

	svc.IsEnabled("radius_auth")
	assert.Equal(t, 1, callCount)

	// Advance clock past TTL.
	now = now.Add(defaultTTL + time.Second)
	svc.IsEnabled("radius_auth")
	assert.Equal(t, 2, callCount, "call after TTL expiry should invoke getenv again")
}

// TestIsEnabled_Polarity_RadiusAuth_Off means switch is off = feature permitted.
func TestIsEnabled_Polarity_RadiusAuth_Off(t *testing.T) {
	svc := newTestService(map[string]string{"KILLSWITCH_RADIUS_AUTH": "off"})
	assert.False(t, svc.IsEnabled("radius_auth"), "off → switch inactive → permit auth traffic")
}

// TestIsEnabled_Polarity_RadiusAuth_On means switch is on = block auth traffic.
func TestIsEnabled_Polarity_RadiusAuth_On(t *testing.T) {
	svc := newTestService(map[string]string{"KILLSWITCH_RADIUS_AUTH": "on"})
	assert.True(t, svc.IsEnabled("radius_auth"), "on → switch active → block auth traffic")
}

// TestIsEnabled_UnknownKey_ReturnsFalse_AndLogsWarnOnce verifies unknown keys
// return false (never block) and warning is emitted only once.
func TestIsEnabled_UnknownKey_ReturnsFalse_AndLogsWarnOnce(t *testing.T) {
	svc := newTestService(map[string]string{})
	assert.False(t, svc.IsEnabled("nonexistent_key"), "unknown key must return false (permit)")
	assert.False(t, svc.IsEnabled("nonexistent_key"), "second call also returns false")
	// Warn should have been recorded only once.
	svc.warnMu.Lock()
	assert.True(t, svc.warnedUnknown["nonexistent_key"], "unknown key should be marked as warned")
	svc.warnMu.Unlock()
}

// TestIsEnabled_InvalidEnvValue_FallsBackToDefault verifies unrecognised env
// values fall back to the key's default.
func TestIsEnabled_InvalidEnvValue_FallsBackToDefault(t *testing.T) {
	svc := newTestService(map[string]string{"KILLSWITCH_RADIUS_AUTH": "maybe"})
	// radius_auth default is false.
	assert.False(t, svc.IsEnabled("radius_auth"))
}
