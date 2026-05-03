package main

// binding_adapters.go — STORY-096 Task 7 adapter implementations.
//
// The internal/policy/binding package defines six narrow interfaces that
// the Enforcer + Orchestrator + BufferedHistoryWriter consume:
//
//   - AllowlistChecker      (Enforcer)
//   - BlacklistChecker      (Enforcer)
//   - Auditor               (Orchestrator)
//   - NotificationPublisher (Orchestrator)
//   - SIMUpdater            (Orchestrator)
//   - DropCounter           (Orchestrator + BufferedHistoryWriter)
//
// This file wires those interfaces to the concrete project services
// (SIMIMEIAllowlistStore, IMEIPoolStore, audit.FullService, bus.EventBus,
// SIMStore, observability/metrics.Registry). Adapters are package-private
// thin wrappers — no logic beyond field projection / shape conversion.
//
// Constructed once in main.go after the corresponding service is built.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/store"
)

// ── AllowlistChecker ──────────────────────────────────────────────────────────

// simAllowlistAdapter wraps SIMIMEIAllowlistStore.IsAllowed for the binding
// enforcer (D-187 production wire — the dormant store finally gains a real
// caller in the AAA hot path).
type simAllowlistAdapter struct {
	store *store.SIMIMEIAllowlistStore
}

func (a *simAllowlistAdapter) IsAllowed(ctx context.Context, tenantID, simID uuid.UUID, imei string) (bool, error) {
	if a == nil || a.store == nil {
		return false, nil
	}
	return a.store.IsAllowed(ctx, tenantID, simID, imei)
}

// ── BlacklistChecker ──────────────────────────────────────────────────────────

// imeiPoolBlacklistAdapter wraps IMEIPoolStore.LookupKind(PoolBlacklist) for
// the AC-9 hard-deny crosscut.
type imeiPoolBlacklistAdapter struct {
	store *store.IMEIPoolStore
}

func (a *imeiPoolBlacklistAdapter) IsInBlacklist(ctx context.Context, tenantID uuid.UUID, imei string) (bool, error) {
	if a == nil || a.store == nil {
		return false, nil
	}
	return a.store.LookupKind(ctx, tenantID, store.PoolBlacklist, imei)
}

// ── Auditor ───────────────────────────────────────────────────────────────────

// bindingAuditAdapter translates the binding orchestrator's typed
// AuditPayload into the generic audit.CreateEntryParams shape. The audit
// row's entity_type is "sim" + entity_id is the SIMID (PAT-022 — every
// binding action targets a single SIM row).
type bindingAuditAdapter struct {
	auditor audit.Auditor
}

func (a *bindingAuditAdapter) Log(ctx context.Context, action string, payload binding.AuditPayload) error {
	if a == nil || a.auditor == nil {
		return nil
	}
	type binding_audit_payload struct {
		ObservedIMEI string `json:"observed_imei,omitempty"`
		BoundIMEI    string `json:"bound_imei,omitempty"`
		BindingMode  string `json:"binding_mode,omitempty"`
		ReasonCode   string `json:"reason_code,omitempty"`
		Protocol     string `json:"protocol"`
	}
	afterData, _ := json.Marshal(binding_audit_payload{
		ObservedIMEI: payload.ObservedIMEI,
		BoundIMEI:    payload.BoundIMEI,
		BindingMode:  payload.BindingMode,
		ReasonCode:   payload.ReasonCode,
		Protocol:     payload.Protocol,
	})
	_, err := a.auditor.CreateEntry(ctx, audit.CreateEntryParams{
		TenantID:   payload.TenantID,
		Action:     action,
		EntityType: "sim",
		EntityID:   payload.SIMID.String(),
		AfterData:  afterData,
	})
	return err
}

// ── NotificationPublisher ─────────────────────────────────────────────────────

// bindingNotificationAdapter publishes the orchestrator's typed
// NotificationPayload onto the bus.SubjectNotification subject. The
// notification dispatcher (internal/notification) consumes this subject
// and renders the user-facing email/Telegram/webhook.
type bindingNotificationAdapter struct {
	publisher *bus.EventBus
}

func (a *bindingNotificationAdapter) Publish(ctx context.Context, subject string, payload binding.NotificationPayload) error {
	if a == nil || a.publisher == nil {
		return nil
	}
	// The dispatcher subscribes on bus.SubjectNotification; the per-binding
	// "subject" is plumbed through the envelope as a meta key so the
	// dispatcher can route to the correct template.
	envelope := map[string]interface{}{
		"subject":       subject,
		"sim_id":        payload.SIMID.String(),
		"tenant_id":     payload.TenantID.String(),
		"observed_imei": payload.ObservedIMEI,
		"bound_imei":    payload.BoundIMEI,
		"binding_mode":  payload.BindingMode,
		"severity":      string(payload.Severity),
		"reason_code":   payload.ReasonCode,
		"protocol":      payload.Protocol,
	}
	return a.publisher.Publish(ctx, bus.SubjectNotification, envelope)
}

// ── SIMUpdater ────────────────────────────────────────────────────────────────

// simBoundIMEIUpdater wraps SIMStore.LockBoundIMEI for the orchestrator's
// post-verdict SIM row update.
type simBoundIMEIUpdater struct {
	store *store.SIMStore
}

func (u *simBoundIMEIUpdater) LockBoundIMEI(ctx context.Context, tenantID uuid.UUID, simID uuid.UUID, imei string, graceExpiresAt *time.Time) error {
	if u == nil || u.store == nil {
		return nil
	}
	return u.store.LockBoundIMEI(ctx, tenantID, simID, imei, graceExpiresAt)
}

// ── DropCounter ───────────────────────────────────────────────────────────────

// bindingDropAdapter increments the two STORY-096 metrics
// (argus_imei_history_dropped_total + argus_binding_notification_failed_total).
// Nil registry is a no-op so unit tests that pass a nil registry don't crash.
type bindingDropAdapter struct {
	reg *obsmetrics.Registry
}

func (m *bindingDropAdapter) IncHistoryDropped() {
	if m == nil || m.reg == nil || m.reg.IMEIHistoryDroppedTotal == nil {
		return
	}
	m.reg.IMEIHistoryDroppedTotal.Inc()
}

func (m *bindingDropAdapter) IncNotificationFailed() {
	if m == nil || m.reg == nil || m.reg.BindingNotificationFailedTotal == nil {
		return
	}
	m.reg.BindingNotificationFailedTotal.Inc()
}

// ── HistoryFlush ──────────────────────────────────────────────────────────────

// bindingHistoryFlush is the per-entry callback for BufferedHistoryWriter.
// It converts the binding.HistoryEntry shape (CapturedAt, "" semantics) into
// the store.AppendIMEIHistoryParams shape (*string nullables, NASIPAddress
// is *string but the in-memory entry uses ""). The store-layer NOT NULL
// constraint on observed_imei is upheld at the orchestrator level — entries
// with empty observed_imei never reach this adapter.
func bindingHistoryFlush(historyStore *store.IMEIHistoryStore) binding.HistoryFlushFunc {
	return func(ctx context.Context, e binding.HistoryEntry) error {
		if historyStore == nil {
			return nil
		}
		var (
			sv  *string
			nas *string
		)
		if e.ObservedSV != "" {
			tmp := e.ObservedSV
			sv = &tmp
		}
		if e.NASIPAddress != "" {
			tmp := e.NASIPAddress
			nas = &tmp
		}
		_, err := historyStore.Append(ctx, e.TenantID, store.AppendIMEIHistoryParams{
			SIMID:                   e.SIMID,
			ObservedIMEI:            e.ObservedIMEI,
			ObservedSoftwareVersion: sv,
			CaptureProtocol:         e.CaptureProtocol,
			NASIPAddress:            nas,
			WasMismatch:             e.WasMismatch,
			AlarmRaised:             e.AlarmRaised,
		})
		return err
	}
}
