package digest

import (
	"os"
	"strconv"
)

// Thresholds carries the operator-tunable thresholds the digest worker
// uses to decide whether a Tier 2 fleet event should be emitted in a
// given 15-min tick. All values are env-overridable; sane M2M defaults
// are documented in docs/architecture/EVENTS.md.
type Thresholds struct {
	// MassOfflinePct: % of active SIMs offline in the window that triggers
	// fleet.mass_offline. Default 5.0 (5% of fleet).
	MassOfflinePct float64
	// MassOfflineFloor: absolute minimum offline count to ever fire.
	// Prevents low-fleet noise (e.g. dev clusters with 50 active SIMs).
	MassOfflineFloor int

	// TrafficSpikeRatio: bytes/15-min divided by rolling baseline that
	// triggers fleet.traffic_spike. Default 3.0 (3× baseline).
	TrafficSpikeRatio float64

	// QuotaBreachCount: number of SIMs crossing quota in window.
	// Default 50.
	QuotaBreachCount int
	// QuotaBreachFloor: absolute floor (always 10 minimum).
	QuotaBreachFloor int

	// ViolationSurgeRatio: policy_violation events/15-min vs baseline.
	// Default 2.0.
	ViolationSurgeRatio float64
	// ViolationSurgeFloor: absolute floor (always 10 minimum).
	ViolationSurgeFloor int
}

// LoadThresholds reads env vars and returns a populated Thresholds with
// safe defaults applied where env is unset or invalid.
func LoadThresholds() Thresholds {
	return Thresholds{
		MassOfflinePct:      envFloat("ARGUS_DIGEST_MASS_OFFLINE_PCT", 5.0),
		MassOfflineFloor:    envInt("ARGUS_DIGEST_MASS_OFFLINE_FLOOR", 10),
		TrafficSpikeRatio:   envFloat("ARGUS_DIGEST_TRAFFIC_SPIKE_RATIO", 3.0),
		QuotaBreachCount:    envInt("ARGUS_DIGEST_QUOTA_BREACH_COUNT", 50),
		QuotaBreachFloor:    envInt("ARGUS_DIGEST_QUOTA_BREACH_FLOOR", 10),
		ViolationSurgeRatio: envFloat("ARGUS_DIGEST_VIOLATION_SURGE_RATIO", 2.0),
		ViolationSurgeFloor: envInt("ARGUS_DIGEST_VIOLATION_SURGE_FLOOR", 10),
	}
}

func envFloat(key string, def float64) float64 {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func envInt(key string, def int) int {
	s := os.Getenv(key)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
