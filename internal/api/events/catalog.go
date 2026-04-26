// Package events exposes the canonical event catalog and the HTTP handler
// that serves it. The catalog is the authoritative reference for every
// in-scope NATS subject emitted by the FIX-212 envelope migration.
package events

import "github.com/btopcu/argus/internal/severity"

// CatalogEntry describes a single canonical event type consumers may receive.
// MetaSchema maps meta-key names to their TypeScript-ish type labels
// ("uuid", "string", "int", "bool") so the FE event stream and notification
// preferences UI can render schema-aware filters.
// Tier classifies notification eligibility: "internal", "digest", or "operational".
type CatalogEntry struct {
	Type            string            `json:"type"`
	Source          string            `json:"source"`
	DefaultSeverity string            `json:"default_severity"`
	EntityType      string            `json:"entity_type"`
	Description     string            `json:"description"`
	MetaSchema      map[string]string `json:"meta_schema"`
	Tier            string            `json:"tier"`
}

// Catalog enumerates every in-scope event type (FIX-212 D6 scope).
// Ordered by subject cluster for stable JSON diffs.
var Catalog = []CatalogEntry{
	// ---- session subjects ----
	{
		Type:            "session.started",
		Source:          "aaa",
		DefaultSeverity: severity.Info,
		EntityType:      "sim",
		Description:     "A new AAA session was established for a SIM.",
		MetaSchema: map[string]string{
			"operator_id": "uuid",
			"apn_id":      "uuid",
			"framed_ip":   "string",
			"rat_type":    "string",
			"nas_ip":      "string",
			"session_id":  "uuid",
		},
		Tier: "internal",
	},
	{
		Type:            "session.updated",
		Source:          "aaa",
		DefaultSeverity: severity.Info,
		EntityType:      "sim",
		Description:     "An existing AAA session was updated (interim accounting, CCR-Update, re-auth).",
		MetaSchema: map[string]string{
			"session_id": "uuid",
			"bytes_in":   "int",
			"bytes_out":  "int",
		},
		Tier: "internal",
	},
	{
		Type:            "session.ended",
		Source:          "aaa",
		DefaultSeverity: severity.Info,
		EntityType:      "sim",
		Description:     "An AAA session ended (STOP, CCR-Term, operator disconnect).",
		MetaSchema: map[string]string{
			"session_id":        "uuid",
			"termination_cause": "string",
			"bytes_in":          "int",
			"bytes_out":         "int",
		},
		Tier: "internal",
	},

	// ---- sim lifecycle ----
	{
		Type:            "sim.state_changed",
		Source:          "sim",
		DefaultSeverity: severity.Info,
		EntityType:      "sim",
		Description:     "A SIM moved between lifecycle states (activate/suspend/resume/terminate/lost).",
		MetaSchema: map[string]string{
			"old_state":         "string",
			"new_state":         "string",
			"operator_id":       "uuid",
			"apn_id":            "uuid",
			"policy_version_id": "uuid",
		},
		Tier: "internal",
	},

	// ---- alert subjects ----
	{
		Type:            "operator_down",
		Source:          "operator",
		DefaultSeverity: severity.Critical,
		EntityType:      "operator",
		Description:     "An operator went DOWN (circuit breaker opened).",
		MetaSchema: map[string]string{
			"previous_status": "string",
			"current_status":  "string",
			"circuit_state":   "string",
			"latency_ms":      "int",
		},
		Tier: "operational",
	},
	{
		Type:            "operator_recovered",
		Source:          "operator",
		DefaultSeverity: severity.Info,
		EntityType:      "operator",
		Description:     "A previously-DOWN operator recovered.",
		MetaSchema: map[string]string{
			"previous_status": "string",
			"current_status":  "string",
			"circuit_state":   "string",
		},
		Tier: "operational",
	},
	{
		Type:            "sla_violation",
		Source:          "operator",
		DefaultSeverity: severity.High,
		EntityType:      "operator",
		Description:     "An operator SLA threshold was violated.",
		MetaSchema: map[string]string{
			"sla_type":       "string",
			"threshold":      "string",
			"observed_value": "string",
			"report_id":      "uuid",
		},
		Tier: "operational",
	},
	{
		Type:            "policy_violation",
		Source:          "policy",
		DefaultSeverity: severity.High,
		EntityType:      "sim",
		Description:     "A policy enforcement rule triggered against a SIM.",
		MetaSchema: map[string]string{
			"policy_id":           "uuid",
			"policy_violation_id": "uuid",
			"rule":                "string",
		},
		Tier: "operational",
	},
	{
		Type:            "anomaly_sim_cloning",
		Source:          "sim",
		DefaultSeverity: severity.High,
		EntityType:      "sim",
		Description:     "Potential SIM cloning detected by the anomaly engine.",
		MetaSchema: map[string]string{
			"anomaly_id": "uuid",
			"score":      "string",
			"details":    "string",
		},
		Tier: "operational",
	},
	{
		Type:            "anomaly_data_spike",
		Source:          "sim",
		DefaultSeverity: severity.Medium,
		EntityType:      "sim",
		Description:     "Unusual data volume spike for a SIM.",
		MetaSchema: map[string]string{
			"anomaly_id": "uuid",
			"score":      "string",
		},
		Tier: "operational",
	},
	{
		Type:            "anomaly_auth_flood",
		Source:          "sim",
		DefaultSeverity: severity.High,
		EntityType:      "sim",
		Description:     "High-frequency auth-attempt burst for a SIM.",
		MetaSchema: map[string]string{
			"anomaly_id": "uuid",
			"score":      "string",
		},
		Tier: "operational",
	},
	{
		Type:            "nats_consumer_lag",
		Source:          "infra",
		DefaultSeverity: severity.High,
		EntityType:      "consumer",
		Description:     "A NATS consumer has persistent pending-message lag (infra).",
		MetaSchema: map[string]string{
			"consumer": "string",
			"pending":  "int",
		},
		Tier: "operational",
	},
	{
		Type:            "anomaly_batch_crash",
		Source:          "infra",
		DefaultSeverity: severity.High,
		EntityType:      "job",
		Description:     "The anomaly batch supervisor observed a batch crash.",
		MetaSchema: map[string]string{
			"job_id": "uuid",
			"error":  "string",
		},
		Tier: "operational",
	},
	{
		Type:            "roaming.agreement.renewal_due",
		Source:          "operator",
		DefaultSeverity: severity.Medium,
		EntityType:      "agreement",
		Description:     "A roaming agreement is within its renewal window.",
		MetaSchema: map[string]string{
			"partner_operator_id":   "uuid",
			"partner_operator_name": "string",
			"expires_at":            "string",
		},
		Tier: "operational",
	},
	{
		Type:            "storage.threshold_exceeded",
		Source:          "infra",
		DefaultSeverity: severity.High,
		EntityType:      "system",
		Description:     "A storage threshold (pgdata, cdr partition) was exceeded.",
		MetaSchema: map[string]string{
			"used_bytes":  "int",
			"total_bytes": "int",
			"component":   "string",
		},
		Tier: "operational",
	},

	// ---- operator health (non-alert) ----
	{
		Type:            "operator.health_changed",
		Source:          "operator",
		DefaultSeverity: severity.Info,
		EntityType:      "operator",
		Description:     "Operator health status transitioned (up<->down<->degraded).",
		MetaSchema: map[string]string{
			"previous_status": "string",
			"current_status":  "string",
			"circuit_state":   "string",
			"latency_ms":      "int",
		},
		Tier: "operational",
	},

	// ---- anomaly ----
	{
		Type:            "anomaly.detected",
		Source:          "analytics",
		DefaultSeverity: severity.Medium,
		EntityType:      "sim",
		Description:     "A generic anomaly was recorded by the analytics engine.",
		MetaSchema: map[string]string{
			"anomaly_type": "string",
			"score":        "string",
			"details":      "string",
		},
		Tier: "operational",
	},

	// ---- policy ----
	{
		Type:            "policy.updated",
		Source:          "policy",
		DefaultSeverity: severity.Info,
		EntityType:      "policy",
		Description:     "A policy resource was created, updated, archived, or deleted.",
		MetaSchema: map[string]string{
			"change_type": "string",
			"version":     "int",
		},
		Tier: "operational",
	},
	{
		Type:            "policy.rollout_progress",
		Source:          "policy",
		DefaultSeverity: severity.Info,
		EntityType:      "policy",
		Description:     "A policy rollout made progress (batch processed).",
		MetaSchema: map[string]string{
			"rollout_id":      "uuid",
			"completed_count": "int",
			"total_count":     "int",
		},
		Tier: "operational",
	},

	// ---- ip ----
	{
		Type:            "ip.reclaimed",
		Source:          "job",
		DefaultSeverity: severity.Info,
		EntityType:      "ip",
		Description:     "An IP address was reclaimed from a pool by the reclaim job.",
		MetaSchema: map[string]string{
			"operator_id":    "uuid",
			"pool_id":        "uuid",
			"reclaim_reason": "string",
		},
		Tier: "internal",
	},
	{
		Type:            "ip.released",
		Source:          "job",
		DefaultSeverity: severity.Info,
		EntityType:      "sim",
		Description:     "An IP lease was released after the grace window.",
		MetaSchema: map[string]string{
			"ip":     "string",
			"reason": "string",
		},
		Tier: "internal",
	},

	// ---- sla report ----
	{
		Type:            "sla.report.generated",
		Source:          "job",
		DefaultSeverity: severity.Info,
		EntityType:      "operator",
		Description:     "An SLA report was generated for an operator and period.",
		MetaSchema: map[string]string{
			"report_id":    "uuid",
			"period_start": "string",
			"period_end":   "string",
		},
		Tier: "operational",
	},

	// ---- notification dispatch ----
	{
		Type:            "notification.dispatch",
		Source:          "notification",
		DefaultSeverity: severity.Info,
		EntityType:      "tenant",
		Description:     "A user-facing notification was dispatched to one or more channels.",
		MetaSchema: map[string]string{
			"event_type":      "string",
			"channels_sent":   "string",
			"notification_id": "uuid",
		},
		Tier: "internal",
	},

	// ---- auth ----
	{
		Type:            "auth.attempt",
		Source:          "aaa",
		DefaultSeverity: severity.Info,
		EntityType:      "sim",
		Description:     "An authentication attempt was recorded.",
		MetaSchema: map[string]string{
			"outcome": "string",
			"reason":  "string",
		},
		Tier: "internal",
	},

	// ---- Tier 2 — Aggregate/Digest (FIX-237 NEW) ----
	{
		Type: "fleet.mass_offline", Source: "argus.events.fleet.mass_offline",
		DefaultSeverity: severity.Medium, EntityType: "fleet",
		Description: "Aggregate alert when % of active SIMs offline crosses threshold in last 15-min window.",
		MetaSchema: map[string]string{
			"offline_count": "int", "active_count": "int", "offline_pct": "float", "window_minutes": "int",
		},
		Tier: "digest",
	},
	{
		Type: "fleet.traffic_spike", Source: "argus.events.fleet.traffic_spike",
		DefaultSeverity: severity.Medium, EntityType: "fleet",
		Description: "Aggregate alert when total fleet bytes/15-min crosses ratio vs rolling baseline.",
		MetaSchema: map[string]string{
			"bytes_total": "int", "baseline_bytes": "int", "ratio": "float", "window_minutes": "int",
		},
		Tier: "digest",
	},
	{
		Type: "fleet.quota_breach_count", Source: "argus.events.fleet.quota_breach_count",
		DefaultSeverity: severity.Medium, EntityType: "fleet",
		Description: "Aggregate alert when count of SIMs crossing quota in 15-min window exceeds threshold.",
		MetaSchema: map[string]string{
			"breach_count": "int", "window_minutes": "int",
		},
		Tier: "digest",
	},
	{
		Type: "fleet.violation_surge", Source: "argus.events.fleet.violation_surge",
		DefaultSeverity: severity.Medium, EntityType: "fleet",
		Description: "Aggregate alert when policy_violation events/15-min exceeds ratio vs baseline.",
		MetaSchema: map[string]string{
			"violation_count": "int", "baseline_count": "int", "ratio": "float", "window_minutes": "int",
		},
		Tier: "digest",
	},

	// ---- Tier 3 additions (FIX-237 NEW catalog entries) ----
	{
		Type: "bulk_job.completed", Source: "argus.events.bulk_job.completed",
		DefaultSeverity: severity.Info, EntityType: "bulk_job",
		Description: "A bulk job (state change / policy assign / eSIM switch) reached terminal success state.",
		MetaSchema: map[string]string{
			"bulk_job_id": "uuid", "total_count": "int", "success_count": "int", "fail_count": "int", "job_type": "string",
		},
		Tier: "operational",
	},
	{
		Type: "bulk_job.failed", Source: "argus.events.bulk_job.failed",
		DefaultSeverity: severity.High, EntityType: "bulk_job",
		Description: "A bulk job reached terminal failure state.",
		MetaSchema: map[string]string{
			"bulk_job_id": "uuid", "total_count": "int", "success_count": "int", "fail_count": "int", "job_type": "string", "error": "string",
		},
		Tier: "operational",
	},
	{
		Type: "webhook.dead_letter", Source: "argus.events.webhook.dead_letter",
		DefaultSeverity: severity.High, EntityType: "webhook",
		Description: "A webhook delivery exhausted retries and was moved to dead-letter queue.",
		MetaSchema: map[string]string{
			"webhook_id": "uuid", "url": "string", "last_error": "string", "retry_count": "int",
		},
		Tier: "operational",
	},
	{
		Type: "backup.verify_failed", Source: "argus.events.backup.verify_failed",
		DefaultSeverity: severity.Critical, EntityType: "backup",
		Description: "A backup integrity verification check failed (mapped to alert.triggered path).",
		MetaSchema: map[string]string{
			"backup_id": "uuid", "verification_method": "string", "error": "string",
		},
		Tier: "operational",
	},
	{
		Type: "report.ready", Source: "argus.events.report.ready",
		DefaultSeverity: severity.Info, EntityType: "report",
		Description: "An async report has finished generation and is available for download (system direct-insert per AC-6).",
		MetaSchema: map[string]string{
			"report_id": "uuid", "report_type": "string", "url": "string",
		},
		Tier: "operational",
	},
}
