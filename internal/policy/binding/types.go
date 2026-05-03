// Package binding implements the AAA-side IMEI/SIM binding pre-check
// (STORY-096). It is invoked between IMEI capture and policy DSL
// evaluation across all three protocols (RADIUS, Diameter S6a, 5G SBA)
// and decides one of three verdicts (Allow / Reject / AllowWithAlarm)
// per ADR-004's six binding modes plus a blacklist hard-deny crosscut.
//
// Task 1 ships the pure decision engine: no DB writes, no audit, no
// notifications, no history append. Side effects are signalled through
// fields on the returned Verdict and consumed by the Task 2 sinks
// orchestrator.
package binding

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// VerdictKind enumerates the three high-level enforcement decisions.
type VerdictKind int

const (
	// VerdictAllow — proceed to policy DSL evaluation. No alarm.
	VerdictAllow VerdictKind = iota
	// VerdictReject — terminate the auth attempt at the wire layer.
	VerdictReject
	// VerdictAllowWithAlarm — proceed but emit audit/notification/history
	// for operator review (soft-mode mismatches).
	VerdictAllowWithAlarm
)

// Severity is the alarm severity carried on Reject and AllowWithAlarm
// verdicts. Empty for plain Allow.
type Severity string

const (
	SeverityInfo   Severity = "info"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// Reject reason codes. These propagate to the wire (RADIUS Reply-Message,
// Diameter Error-Message AVP-281, 5G SBA problem-details `cause`) and
// must match the entries in internal/apierr/apierr.go + ERROR_CODES.md
// (PAT-022 discipline).
const (
	RejectReasonMismatchStrict    = "BINDING_MISMATCH_STRICT"
	RejectReasonMismatchAllowlist = "BINDING_MISMATCH_ALLOWLIST"
	RejectReasonMismatchTAC       = "BINDING_MISMATCH_TAC"
	RejectReasonBlacklist         = "BINDING_BLACKLIST"
	RejectReasonGraceExpired      = "BINDING_GRACE_EXPIRED"
)

// Audit action keys (consumed by Task 2 sinks).
const (
	AuditActionMismatch       = "sim.binding_mismatch"
	AuditActionFirstUseLocked = "sim.binding_first_use_locked"
	AuditActionSoftMismatch   = "sim.binding_soft_mismatch"
	AuditActionBlacklistHit   = "sim.binding_blacklist_hit"
	AuditActionGraceChange    = "sim.binding_grace_change"
)

// Notification subject keys (consumed by Task 2 sinks).
const (
	NotifSubjectBindingFailed       = "device.binding_failed"
	NotifSubjectBindingLocked       = "device.binding_locked"
	NotifSubjectBindingGraceChange  = "device.binding_grace_change"
	NotifSubjectIMEIMismatch        = "imei.mismatch_detected"
	NotifSubjectBindingBlacklistHit = "device.binding_blacklist_hit"
)

// Binding status values written to sims.binding_status. These are the
// existing TBL-10 binding_status enum values from STORY-094 — no new
// strings introduced (PAT-022).
const (
	BindingStatusVerified = "verified"
	BindingStatusPending  = "pending"
	BindingStatusMismatch = "mismatch"
	BindingStatusDisabled = "disabled"
	BindingStatusUnbound  = "unbound"
)

// Verdict is the decision returned by Enforcer.Evaluate. The decision
// itself (Kind/Reason/Severity) is consumed by the wire layer; the
// side-effect signal fields (EmitAudit, EmitNotification, History*,
// LockBoundIMEI, RefreshGraceWindow) are consumed by the Task 2 sinks
// orchestrator. Keeping decision and side-effects in a single value
// keeps the enforcer testable as pure logic.
type Verdict struct {
	// Kind is the high-level decision: Allow / Reject / AllowWithAlarm.
	Kind VerdictKind

	// Reason is a non-empty wire-level code for Reject (one of the
	// RejectReason* constants). Empty for Allow / AllowWithAlarm.
	Reason string

	// Severity is the alarm severity for Reject and AllowWithAlarm. Empty
	// for plain Allow.
	Severity Severity

	// BindingStatus is the value to be written to sims.binding_status
	// ("verified" / "pending" / "mismatch" / "disabled" / "unbound").
	// Empty when the status should NOT be touched (rare — currently only
	// soft-mode + empty IMEI per row #24 of the decision table).
	BindingStatus string

	// EmitAudit signals Task 2 to call Auditor.CreateEntry synchronously
	// before returning the wire response (audit hash chain integrity —
	// AC-16).
	EmitAudit   bool
	AuditAction string // one of the AuditAction* constants

	// EmitNotification signals Task 2 to publish on bus.SubjectNotification
	// asynchronously (fire-and-forget goroutine).
	EmitNotification bool
	NotifSubject     string // one of the NotifSubject* constants

	// History* signal Task 2 to append a row to imei_history. The row is
	// only written when the observed IMEI is non-empty (the column is
	// NOT NULL in TBL-59).
	HistoryWasMismatch bool
	HistoryAlarmRaised bool

	// LockBoundIMEI signals Task 2 to UPDATE sims.bound_imei = NewBoundIMEI
	// (first-use mode and accepted grace-window changes). Task 2 also
	// stamps binding_verified_at = NOW() when LockBoundIMEI is true.
	LockBoundIMEI bool
	NewBoundIMEI  string

	// RefreshGraceWindow signals Task 2 to UPDATE
	// sims.binding_grace_expires_at = NOW() + graceWindow on accepted
	// grace-period changes (row #17, #19).
	RefreshGraceWindow bool
}

// SessionContext carries the per-request fields the enforcer reads. It
// is intentionally decoupled from internal/policy/dsl.SessionContext so
// the binding package keeps a narrow, test-friendly surface — Task 2/3
// adapt from the DSL session value when wiring at the AAA call sites.
type SessionContext struct {
	TenantID        uuid.UUID
	SIMID           uuid.UUID
	IMEI            string
	SoftwareVersion string
}

// AllowlistChecker is the narrow interface the enforcer needs from
// SIMIMEIAllowlistStore (D-187 wire). Defined here so tests can mock
// without pulling in store.
type AllowlistChecker interface {
	IsAllowed(ctx context.Context, tenantID, simID uuid.UUID, imei string) (bool, error)
}

// BlacklistChecker is the narrow interface the enforcer needs from
// IMEIPoolStore for the AC-9 hard-deny crosscut. The implementation
// in internal/store wraps LookupKind(ctx, tenantID, imei, "blacklist").
type BlacklistChecker interface {
	IsInBlacklist(ctx context.Context, tenantID uuid.UUID, imei string) (bool, error)
}

// SIMView is the minimal projection of *store.SIM the enforcer needs —
// all binding-related fields, plus the SIM's identity so the enforcer
// can be exercised in tests without constructing a full *store.SIM.
//
// Production callers populate it from store.SIM in Task 2/3 via
// FromStoreSIM (defined alongside the side-effect orchestrator). The
// fields mirror the post-STORY-094 shape on store.SIM.
type SIMView struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
	BindingMode           *string
	BoundIMEI             *string
	BindingGraceExpiresAt *time.Time
}
