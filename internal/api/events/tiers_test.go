package events

import "testing"

func TestTierFor_Tier1Events(t *testing.T) {
	cases := []string{
		"session.started", "session_started", "sim.state_changed", "sim_state_change",
		"heartbeat.ok", "heartbeat_ok", "auth.attempt", "usage.threshold",
		"ip.reclaimed", "notification.dispatch",
	}
	for _, evt := range cases {
		if got := TierFor(evt); got != TierInternal {
			t.Errorf("TierFor(%q) = %v, want TierInternal", evt, got)
		}
	}
}

func TestTierFor_Tier2Events(t *testing.T) {
	cases := []string{
		"fleet.mass_offline", "fleet.traffic_spike",
		"fleet.quota_breach_count", "fleet.violation_surge",
	}
	for _, evt := range cases {
		if got := TierFor(evt); got != TierDigest {
			t.Errorf("TierFor(%q) = %v, want TierDigest", evt, got)
		}
	}
}

func TestTierFor_Tier3Events(t *testing.T) {
	cases := []string{
		"operator_down", "policy_violation", "webhook.dead_letter",
		"bulk_job.completed", "bulk_job.failed", "report.ready",
		"operator.health_changed", "anomaly.detected",
	}
	for _, evt := range cases {
		if got := TierFor(evt); got != TierOperational {
			t.Errorf("TierFor(%q) = %v, want TierOperational", evt, got)
		}
	}
}

func TestTierFor_UnknownEvent_DefaultsToOperational(t *testing.T) {
	if got := TierFor("custom.tenant.event"); got != TierOperational {
		t.Errorf("TierFor(unknown) = %v, want TierOperational (safe default)", got)
	}
	if got := TierFor(""); got != TierOperational {
		t.Errorf("TierFor(empty) = %v, want TierOperational (safe default)", got)
	}
}

func TestTierEventTypeSlices_NonEmpty(t *testing.T) {
	if len(Tier1EventTypes()) == 0 {
		t.Error("Tier1EventTypes empty")
	}
	if len(Tier2EventTypes()) == 0 {
		t.Error("Tier2EventTypes empty")
	}
	if len(Tier3EventTypes()) == 0 {
		t.Error("Tier3EventTypes empty")
	}
}
