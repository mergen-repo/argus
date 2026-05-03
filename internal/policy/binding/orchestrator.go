package binding

// Side-effect orchestrator for STORY-096 / Task 2.
//
// The Orchestrator consumes a Verdict produced by Enforcer.Evaluate and
// fans the implied side effects out to three sinks plus an optional SIM
// row update:
//
//  1. SIMUpdater (synchronous) — UPDATE sims.bound_imei /
//     binding_grace_expires_at when the verdict signals LockBoundIMEI
//     and/or RefreshGraceWindow. This MUST happen before audit so the
//     subsequent audit row reflects the post-update state. Failures
//     bubble up; the wire layer translates them into a 5xx-class reject.
//
//  2. Auditor (synchronous) — chain integrity (AC-16) requires the audit
//     row be persisted before the wire response is emitted. Failures
//     bubble up.
//
//  3. NotificationPublisher (asynchronous, fire-and-forget goroutine) —
//     post-decision; severity-high binding events MUST reach ops even if
//     the AAA client times out. We use a fresh context with a bounded
//     timeout so request cancellation does not drop the notification.
//     Failures are logged + counted via DropCounter, never bubbled.
//
//  4. HistoryWriter (asynchronous, buffered queue) — IMEI history is the
//     forensic trail. AC-11 mandates async storage so the AAA hot path is
//     not slowed. The writer drops on overflow + increments DropCounter.
//     Empty observed IMEI is suppressed because imei_history.observed_imei
//     is NOT NULL in TBL-59 (the * footnote on the decision table).
//
// The orchestrator is NOT a job processor (PAT-026 inverse-orphan does
// not apply). It is a stateless fan-out that adapts a Verdict + context
// into per-sink calls. The buffered history writer (history_writer.go)
// is the only goroutine-bearing component and ships with its own
// graceful-shutdown contract.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Auditor is the narrow interface the orchestrator needs from the audit
// service (internal/audit). Defined locally so tests can mock without
// pulling in the full audit package.
type Auditor interface {
	Log(ctx context.Context, action string, payload AuditPayload) error
}

// AuditPayload carries the fields the audit sink writes for any
// binding-related action. The audit framework in internal/audit translates
// these into the standard CreateEntryParams shape (entity_type='sim',
// entity_id=SIMID.String(), action=<action>, after_data=JSON(payload)).
type AuditPayload struct {
	SIMID        uuid.UUID
	TenantID     uuid.UUID
	ObservedIMEI string
	BoundIMEI    string
	BindingMode  string
	ReasonCode   string // empty for AllowWithAlarm / first-use lock
	Protocol     string // "radius" | "diameter_s6a" | "5g_sba"
}

// NotificationPublisher is the narrow interface the orchestrator needs
// from the notification service (internal/notification or the bus
// publish helper). Defined locally per dispatch convention.
type NotificationPublisher interface {
	Publish(ctx context.Context, subject string, payload NotificationPayload) error
}

// NotificationPayload carries the fields required to render a binding
// notification (email/Telegram/webhook). Severity drives template + queue
// priority on the dispatcher side.
type NotificationPayload struct {
	SIMID        uuid.UUID
	TenantID     uuid.UUID
	ObservedIMEI string
	BoundIMEI    string
	BindingMode  string
	Severity     Severity
	ReasonCode   string // empty for AllowWithAlarm
	Protocol     string
}

// HistoryWriter is the non-blocking interface for the IMEI history sink.
// Implementations MUST drop on overflow rather than block (AC-11).
type HistoryWriter interface {
	Append(ctx context.Context, entry HistoryEntry)
}

// HistoryEntry is the per-row payload appended to imei_history (TBL-59).
// CapturedAt is plumbed in by the orchestrator so all four sinks see the
// same logical "now" for a given verdict.
type HistoryEntry struct {
	TenantID        uuid.UUID
	SIMID           uuid.UUID
	ObservedIMEI    string
	ObservedSV      string
	CapturedAt      time.Time
	CaptureProtocol string
	NASIPAddress    string // optional; empty string suppresses the column
	WasMismatch     bool
	AlarmRaised     bool
}

// SIMUpdater is the narrow interface the orchestrator needs from
// SIMStore for bound_imei + binding_grace_expires_at writes. Task 7
// supplies the concrete implementation that wraps SIMStore.SetDeviceBinding
// with the grace-window column update.
type SIMUpdater interface {
	LockBoundIMEI(ctx context.Context, tenantID uuid.UUID, simID uuid.UUID, imei string, graceExpiresAt *time.Time) error
}

// DropCounter is the narrow interface the orchestrator and history
// writer use to surface dropped-side-effect events to metrics. Task 7
// wires a Prometheus counter implementation; tests use an in-memory
// counter.
type DropCounter interface {
	IncHistoryDropped()
	IncNotificationFailed()
}

// noopDropCounter is the zero-value default when the caller does not
// inject a metrics sink. Keeps Apply nil-safe.
type noopDropCounter struct{}

func (noopDropCounter) IncHistoryDropped()     {}
func (noopDropCounter) IncNotificationFailed() {}

// notificationDispatchTimeout bounds the fresh background context used
// for the async notification publish goroutine. Five seconds is the
// existing dispatcher SLA for synchronous publishes (internal/notification
// service.go) — we keep the same budget so a slow NATS write does not
// starve the goroutine pool.
const notificationDispatchTimeout = 5 * time.Second

// Orchestrator fans Verdict-driven side effects out to the four sinks.
// Construct with New(...). Apply is goroutine-safe — each call captures
// its inputs into the fresh notification goroutine, so concurrent calls
// from different AAA workers do not interfere.
type Orchestrator struct {
	auditor     Auditor
	notifier    NotificationPublisher
	history     HistoryWriter
	sims        SIMUpdater
	metrics     DropCounter
	logger      zerolog.Logger
	graceWindow time.Duration
	now         func() time.Time
}

// OrchestratorOption configures a new Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithDropCounter wires a metrics sink. Defaults to a no-op so tests
// that don't care about metrics can omit it.
func WithDropCounter(c DropCounter) OrchestratorOption {
	return func(o *Orchestrator) {
		if c != nil {
			o.metrics = c
		}
	}
}

// WithLogger injects a zerolog logger. Defaults to zerolog.Nop().
func WithLogger(l zerolog.Logger) OrchestratorOption {
	return func(o *Orchestrator) { o.logger = l }
}

// WithOrchestratorClock injects a deterministic clock for tests.
func WithOrchestratorClock(now func() time.Time) OrchestratorOption {
	return func(o *Orchestrator) {
		if now != nil {
			o.now = now
		}
	}
}

// NewOrchestrator builds an Orchestrator with the supplied dependencies.
// graceWindow is applied to RefreshGraceWindow updates; non-positive
// values fall back to defaultGraceWindow (72h).
//
// Any of auditor / notifier / history / sims may be nil — Apply degrades
// gracefully: a nil sink simply skips that side-effect with a debug log.
// This keeps Task 3-5 wiring straightforward (a null-wired enforcer can
// be threaded into a handler that has no auditor configured yet).
func NewOrchestrator(auditor Auditor, notifier NotificationPublisher, history HistoryWriter, sims SIMUpdater, graceWindow time.Duration, opts ...OrchestratorOption) *Orchestrator {
	if graceWindow <= 0 {
		graceWindow = defaultGraceWindow
	}
	o := &Orchestrator{
		auditor:     auditor,
		notifier:    notifier,
		history:     history,
		sims:        sims,
		metrics:     noopDropCounter{},
		logger:      zerolog.Nop(),
		graceWindow: graceWindow,
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Apply executes all side effects implied by the Verdict.
//
// Order:
//  1. SIM update (sync, bubble) — must precede audit so the audit row
//     reflects post-update state.
//  2. Audit (sync, bubble) — chain integrity per AC-16.
//  3. Notification (async, swallow) — fresh context + bounded TTL.
//  4. History (async, swallow) — buffered writer with drop semantics.
//
// The protocol argument is one of "radius" / "diameter_s6a" / "5g_sba"
// and is plumbed into both the audit + notification + history payloads.
func (o *Orchestrator) Apply(ctx context.Context, v Verdict, session SessionContext, sim SIMView, protocol string) error {
	// 1. SIM update — synchronous; failure bubbles so the wire layer can
	//    return 5xx and the audit + history don't run on stale state.
	if v.LockBoundIMEI || v.RefreshGraceWindow {
		if err := o.applySIMUpdate(ctx, v, session, sim); err != nil {
			return err
		}
	}

	// 2. Audit — synchronous; chain integrity.
	if v.EmitAudit {
		if o.auditor == nil {
			o.logger.Debug().Str("action", v.AuditAction).Msg("binding orchestrator: nil auditor, skipping audit")
		} else {
			payload := o.buildAuditPayload(v, session, sim, protocol)
			if err := o.auditor.Log(ctx, v.AuditAction, payload); err != nil {
				return fmt.Errorf("binding: audit log %q: %w", v.AuditAction, err)
			}
		}
	}

	// 3. Notification — asynchronous; failures are logged + counted but
	//    never bubble. We use a fresh background context so a cancelled
	//    request context does not drop the notification (the verdict has
	//    already been decided; ops MUST hear about it).
	if v.EmitNotification {
		o.dispatchNotification(v, session, sim, protocol)
	}

	// 4. History — asynchronous via buffered writer. Suppress when
	//    observed IMEI is empty (TBL-59 NOT NULL constraint per the
	//    decision-table * footnote and types.go:122).
	if session.IMEI != "" && o.history != nil {
		entry := HistoryEntry{
			TenantID:        session.TenantID,
			SIMID:           sim.ID,
			ObservedIMEI:    session.IMEI,
			ObservedSV:      session.SoftwareVersion,
			CapturedAt:      o.now().UTC(),
			CaptureProtocol: protocol,
			WasMismatch:     v.HistoryWasMismatch,
			AlarmRaised:     v.HistoryAlarmRaised,
		}
		o.history.Append(ctx, entry)
	}

	return nil
}

// applySIMUpdate handles the bound_imei + binding_grace_expires_at writes.
// LockBoundIMEI=true                       → set bound_imei = NewBoundIMEI.
// LockBoundIMEI=true + RefreshGraceWindow  → set bound_imei + grace expiry.
// LockBoundIMEI=false + RefreshGraceWindow → keep existing bound_imei,
//
//	set grace expiry only.
func (o *Orchestrator) applySIMUpdate(ctx context.Context, v Verdict, session SessionContext, sim SIMView) error {
	if o.sims == nil {
		o.logger.Debug().Msg("binding orchestrator: nil SIMUpdater, skipping SIM update")
		return nil
	}

	imei := v.NewBoundIMEI
	if !v.LockBoundIMEI {
		// Refresh-only: keep the existing bound_imei.
		imei = derefStr(sim.BoundIMEI)
	}

	var graceExpires *time.Time
	if v.RefreshGraceWindow {
		t := o.now().Add(o.graceWindow).UTC()
		graceExpires = &t
	}

	if err := o.sims.LockBoundIMEI(ctx, session.TenantID, sim.ID, imei, graceExpires); err != nil {
		return fmt.Errorf("binding: lock bound_imei: %w", err)
	}
	return nil
}

// dispatchNotification fires the publish goroutine. Captured by value to
// avoid races with subsequent Apply calls overwriting the receiver state.
func (o *Orchestrator) dispatchNotification(v Verdict, session SessionContext, sim SIMView, protocol string) {
	if o.notifier == nil {
		o.logger.Debug().Str("subject", v.NotifSubject).Msg("binding orchestrator: nil notifier, skipping publish")
		return
	}
	payload := NotificationPayload{
		SIMID:        sim.ID,
		TenantID:     session.TenantID,
		ObservedIMEI: session.IMEI,
		BoundIMEI:    derefStr(sim.BoundIMEI),
		BindingMode:  derefStr(sim.BindingMode),
		Severity:     v.Severity,
		ReasonCode:   v.Reason,
		Protocol:     protocol,
	}
	subject := v.NotifSubject
	notifier := o.notifier
	metrics := o.metrics
	logger := o.logger

	go func() {
		nctx, cancel := context.WithTimeout(context.Background(), notificationDispatchTimeout)
		defer cancel()
		if err := notifier.Publish(nctx, subject, payload); err != nil {
			metrics.IncNotificationFailed()
			logger.Warn().Err(err).Str("subject", subject).Str("sim_id", sim.ID.String()).Msg("binding orchestrator: notification publish failed")
		}
	}()
}

// buildAuditPayload assembles the AuditPayload from the verdict + session
// + sim view. Centralised so each sink call site stays one-liner.
func (o *Orchestrator) buildAuditPayload(v Verdict, session SessionContext, sim SIMView, protocol string) AuditPayload {
	return AuditPayload{
		SIMID:        sim.ID,
		TenantID:     session.TenantID,
		ObservedIMEI: session.IMEI,
		BoundIMEI:    derefStr(sim.BoundIMEI),
		BindingMode:  derefStr(sim.BindingMode),
		ReasonCode:   v.Reason,
		Protocol:     protocol,
	}
}
