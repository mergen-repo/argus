package events

import "sort"

// Tier classifies how an event_type should be handled by the notification engine.
type Tier string

const (
	TierInternal    Tier = "internal"
	TierDigest      Tier = "digest"
	TierOperational Tier = "operational"
)

// tier1Events — Internal/Metric. NEVER notification-eligible.
var tier1Events = map[string]struct{}{
	"session.started":       {}, "session_started":   {},
	"session.updated":       {},
	"session.ended":         {}, "session_ended":     {},
	"sim.state_changed":     {}, "sim_state_change":  {},
	"auth.attempt":          {}, "auth_attempt":      {},
	"heartbeat.ok":          {}, "heartbeat_ok":      {},
	"policy.enforced":       {},
	"usage.threshold":       {}, "usage_threshold":   {},
	"ip.reclaimed":          {}, "ip_reclaimed":      {},
	"ip.released":           {}, "ip_released":       {},
	"notification.dispatch": {},
}

// tier2Events — Digest. Aggregated fleet-level events.
var tier2Events = map[string]struct{}{
	"fleet.mass_offline":       {},
	"fleet.traffic_spike":      {},
	"fleet.quota_breach_count": {},
	"fleet.violation_surge":    {},
}

// tier3Events — Operational. Notification-eligible individual alerts.
var tier3Events = map[string]struct{}{
	"operator_down":                 {},
	"operator_recovered":            {},
	"sla_violation":                 {},
	"policy_violation":              {},
	"storage.threshold_exceeded":    {},
	"policy.rollout_progress":       {},
	"anomaly_sim_cloning":           {},
	"anomaly_data_spike":            {},
	"anomaly_auth_flood":            {},
	"anomaly.detected":              {},
	"nats_consumer_lag":             {},
	"anomaly_batch_crash":           {},
	"webhook.dead_letter":           {},
	"bulk_job.completed":            {},
	"bulk_job.failed":               {},
	"backup.verify_failed":          {},
	"report.ready":                  {},
	"kvkk_purge_completed":          {},
	"sms_delivery_failed":           {},
	"operator.health_changed":       {},
}

// TierFor returns the classification tier for an event_type string.
// Unknown event types default to TierOperational (notification-eligible).
// Rationale: an unclassified event is more likely a real alert than spam,
// so the safe default is "surface to admins" rather than "drop silently".
func TierFor(eventType string) Tier {
	if _, ok := tier1Events[eventType]; ok {
		return TierInternal
	}
	if _, ok := tier2Events[eventType]; ok {
		return TierDigest
	}
	if _, ok := tier3Events[eventType]; ok {
		return TierOperational
	}
	return TierOperational
}

// Tier1EventTypes returns sorted canonical event type strings for Tier 1.
func Tier1EventTypes() []string { return sortedKeys(tier1Events) }

// Tier2EventTypes returns sorted canonical event type strings for Tier 2.
func Tier2EventTypes() []string { return sortedKeys(tier2Events) }

// Tier3EventTypes returns sorted canonical event type strings for Tier 3.
func Tier3EventTypes() []string { return sortedKeys(tier3Events) }

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
