package digest

import (
	"testing"
)

func TestLoadThresholds_Defaults(t *testing.T) {
	t.Setenv("ARGUS_DIGEST_MASS_OFFLINE_PCT", "")
	t.Setenv("ARGUS_DIGEST_MASS_OFFLINE_FLOOR", "")
	t.Setenv("ARGUS_DIGEST_TRAFFIC_SPIKE_RATIO", "")
	t.Setenv("ARGUS_DIGEST_QUOTA_BREACH_COUNT", "")
	t.Setenv("ARGUS_DIGEST_QUOTA_BREACH_FLOOR", "")
	t.Setenv("ARGUS_DIGEST_VIOLATION_SURGE_RATIO", "")
	t.Setenv("ARGUS_DIGEST_VIOLATION_SURGE_FLOOR", "")
	th := LoadThresholds()
	if th.MassOfflinePct != 5.0 {
		t.Errorf("MassOfflinePct: got %v, want 5.0", th.MassOfflinePct)
	}
	if th.MassOfflineFloor != 10 {
		t.Errorf("MassOfflineFloor: got %v, want 10", th.MassOfflineFloor)
	}
	if th.TrafficSpikeRatio != 3.0 {
		t.Errorf("TrafficSpikeRatio: got %v, want 3.0", th.TrafficSpikeRatio)
	}
	if th.QuotaBreachCount != 50 {
		t.Errorf("QuotaBreachCount: got %v, want 50", th.QuotaBreachCount)
	}
	if th.QuotaBreachFloor != 10 {
		t.Errorf("QuotaBreachFloor: got %v, want 10", th.QuotaBreachFloor)
	}
	if th.ViolationSurgeRatio != 2.0 {
		t.Errorf("ViolationSurgeRatio: got %v, want 2.0", th.ViolationSurgeRatio)
	}
	if th.ViolationSurgeFloor != 10 {
		t.Errorf("ViolationSurgeFloor: got %v, want 10", th.ViolationSurgeFloor)
	}
}

func TestLoadThresholds_OverrideValid(t *testing.T) {
	t.Setenv("ARGUS_DIGEST_MASS_OFFLINE_PCT", "12.5")
	t.Setenv("ARGUS_DIGEST_QUOTA_BREACH_COUNT", "200")
	th := LoadThresholds()
	if th.MassOfflinePct != 12.5 {
		t.Errorf("override pct: got %v, want 12.5", th.MassOfflinePct)
	}
	if th.QuotaBreachCount != 200 {
		t.Errorf("override count: got %v, want 200", th.QuotaBreachCount)
	}
}

func TestLoadThresholds_OverrideInvalid_FallsBackToDefault(t *testing.T) {
	t.Setenv("ARGUS_DIGEST_MASS_OFFLINE_PCT", "not-a-number")
	t.Setenv("ARGUS_DIGEST_QUOTA_BREACH_COUNT", "-50")
	th := LoadThresholds()
	if th.MassOfflinePct != 5.0 {
		t.Errorf("invalid override should fall back: got %v, want 5.0", th.MassOfflinePct)
	}
	if th.QuotaBreachCount != 50 {
		t.Errorf("negative override should fall back: got %v, want 50", th.QuotaBreachCount)
	}
}
