package binding

import (
	"context"
	"time"
)

// defaultGraceWindow is used when the configured grace window is
// non-positive. 72h matches ADR-004 §Migration recommendation and the
// ARGUS_BINDING_GRACE_WINDOW envconfig default in internal/config.
const defaultGraceWindow = 72 * time.Hour

// Enforcer is the pure decision engine for the IMEI/SIM binding
// pre-check. It runs before policy DSL evaluation in the AAA hot path
// and produces a Verdict consumed by Task 2's side-effect orchestrator.
//
// Construct with New(opts...). All dependencies are interface-typed so
// tests substitute mocks; nil-safety is preserved on every dependency
// (a nil dependency degrades gracefully — see WithAllowlistChecker /
// WithBlacklistChecker for the fail-open semantics).
type Enforcer struct {
	allowlist   AllowlistChecker
	blacklist   BlacklistChecker
	graceWindow time.Duration
	now         func() time.Time
}

// Option configures a new Enforcer. Functional options keep the
// constructor signature stable as new dependencies are threaded by
// Task 2/Task 7 wiring.
type Option func(*Enforcer)

// WithAllowlistChecker wires the SIM-IMEI allowlist lookup used by
// binding_mode='allowlist'. nil checker → allowlist mode rejects every
// observation (defensive: the configuration says the SIM expects an
// allowlist but no source is available).
func WithAllowlistChecker(c AllowlistChecker) Option {
	return func(e *Enforcer) { e.allowlist = c }
}

// WithBlacklistChecker wires the IMEI blacklist lookup used for the
// AC-9 hard-deny crosscut. nil checker → no blacklist enforcement
// (fail-open; STORY-095 already guarantees the lookup never returns
// inconsistent results).
func WithBlacklistChecker(c BlacklistChecker) Option {
	return func(e *Enforcer) { e.blacklist = c }
}

// WithGraceWindow sets the duration applied to grace-period mode's
// binding_grace_expires_at on accepted lock/change. Non-positive values
// are replaced with defaultGraceWindow.
func WithGraceWindow(d time.Duration) Option {
	return func(e *Enforcer) {
		if d > 0 {
			e.graceWindow = d
		}
	}
}

// WithClock injects a deterministic clock for tests. Production code
// should leave this unset (time.Now is the default).
func WithClock(now func() time.Time) Option {
	return func(e *Enforcer) {
		if now != nil {
			e.now = now
		}
	}
}

// New constructs an Enforcer with the supplied options. Defaults:
//   - graceWindow = 72h (ADR-004 §Migration default)
//   - now = time.Now
//   - allowlist / blacklist = nil (each branch handles nil safely)
func New(opts ...Option) *Enforcer {
	e := &Enforcer{
		graceWindow: defaultGraceWindow,
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Evaluate runs the binding pre-check for a single auth attempt. It is
// pure: no DB writes, no audit emit, no notification publish, no
// history append — those are Task 2's responsibility, signalled via
// the Verdict fields.
//
// Order of operations (see plan §Hard-Deny Crosscut):
//  1. Blacklist hard-deny override (AC-9). Runs FIRST so it overrides
//     ANY mode verdict, including NULL-mode Allow. Skipped silently
//     when the observed IMEI is empty (no IMEI to look up).
//  2. NULL-mode short-circuit (AC-2). Performance-critical: 99%+ of
//     production SIMs have BindingMode==nil and must skip every other
//     branch. Returns Allow with BindingStatus="disabled".
//  3. Mode dispatch — one of the six ADR-004 modes. Unknown values
//     (defensive) fall through to the NULL behaviour.
//
// Plan V6 (NULL short-circuit perf claim): the 0-alloc / sub-µs guarantee
// holds when EITHER the blacklist checker is nil-wired OR the observed
// IMEI is empty. The benchmark fixtures in Task 8 set blacklist=nil for
// the NULL baseline; production runs with blacklist wired pay one
// SELECT EXISTS even on NULL-mode SIMs (acceptable per AC-9).
func (e *Enforcer) Evaluate(ctx context.Context, session SessionContext, sim SIMView) (Verdict, error) {
	// 1. Blacklist hard-deny crosscut (AC-9). Blacklist overrides every
	//    mode, including NULL. We check it FIRST; if it hits we never
	//    look at binding_mode at all. Safe on empty IMEI: only a
	//    non-empty IMEI can ever appear in a pool entry.
	if session.IMEI != "" && e.blacklist != nil {
		hit, err := e.blacklist.IsInBlacklist(ctx, session.TenantID, session.IMEI)
		// Fail-open on lookup error (consistent with STORY-095 DSL
		// fail-open for device.imei_in_pool — see evaluator_imei_pool_test.go
		// nil-lookup semantics). The error is propagated for the caller
		// to log; the verdict is whatever the mode decides.
		if err == nil && hit {
			return Verdict{
				Kind:               VerdictReject,
				Reason:             RejectReasonBlacklist,
				Severity:           SeverityHigh,
				BindingStatus:      BindingStatusMismatch,
				EmitAudit:          true,
				AuditAction:        AuditActionBlacklistHit,
				EmitNotification:   true,
				NotifSubject:       NotifSubjectBindingBlacklistHit,
				HistoryWasMismatch: true,
				HistoryAlarmRaised: true,
			}, nil
		}
		if err != nil {
			// Surface the error to the caller (Task 2 logs it). Continue
			// with mode evaluation — fail-open.
			return e.evaluateMode(ctx, session, sim), err
		}
	}

	return e.evaluateMode(ctx, session, sim), nil
}

// evaluateMode runs the mode-specific decision logic. It is split out
// so the blacklist crosscut can call it directly on the fail-open path
// without re-checking the blacklist.
func (e *Enforcer) evaluateMode(ctx context.Context, session SessionContext, sim SIMView) Verdict {
	// NULL-mode short-circuit (AC-2). MUST stay above any DB call so
	// the AAA hot path pays no enforcer cost on the 99% of SIMs without
	// binding configured (AC-13 / DEV-410).
	if sim.BindingMode == nil || *sim.BindingMode == "" {
		return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusDisabled}
	}

	switch *sim.BindingMode {
	case "strict":
		return e.evalStrict(session, sim)
	case "allowlist":
		return e.evalAllowlist(ctx, session, sim)
	case "first-use":
		return e.evalFirstUse(session, sim)
	case "tac-lock":
		return e.evalTACLock(session, sim)
	case "grace-period":
		return e.evalGracePeriod(session, sim)
	case "soft":
		return e.evalSoft(session, sim)
	default:
		// Defensive: unknown mode → behave as NULL. Wire layer logs the
		// surprise; the AAA path does not block.
		return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusDisabled}
	}
}

// evalStrict implements rows #2-#5 of the decision table. Empty observed
// IMEI is treated as a mismatch (capture failure → reject).
func (e *Enforcer) evalStrict(session SessionContext, sim SIMView) Verdict {
	bound := derefStr(sim.BoundIMEI)
	if bound != "" && session.IMEI != "" && session.IMEI == bound {
		return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
	}
	return rejectMismatch(RejectReasonMismatchStrict, SeverityHigh)
}

// evalAllowlist implements rows #6-#8. Calls the AllowlistChecker; nil
// checker is treated as "no entries" (every observation rejects).
func (e *Enforcer) evalAllowlist(ctx context.Context, session SessionContext, sim SIMView) Verdict {
	if session.IMEI == "" {
		return rejectMismatch(RejectReasonMismatchAllowlist, SeverityHigh)
	}
	if e.allowlist == nil {
		return rejectMismatch(RejectReasonMismatchAllowlist, SeverityHigh)
	}
	allowed, err := e.allowlist.IsAllowed(ctx, session.TenantID, sim.ID, session.IMEI)
	if err != nil || !allowed {
		return rejectMismatch(RejectReasonMismatchAllowlist, SeverityHigh)
	}
	return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
}

// evalFirstUse implements rows #9-#12. When bound_imei is unset and the
// observed IMEI is non-empty, the SIM "locks" to the captured IMEI.
// Once locked, behaves as strict.
func (e *Enforcer) evalFirstUse(session SessionContext, sim SIMView) Verdict {
	bound := derefStr(sim.BoundIMEI)
	if bound != "" {
		// Locked SIM — same path as strict mode (rows #10/#11).
		if session.IMEI != "" && session.IMEI == bound {
			return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
		}
		return rejectMismatch(RejectReasonMismatchStrict, SeverityHigh)
	}
	// Unbound — capture if observed IMEI is non-empty (row #9), else
	// degenerate reject (row #12).
	if session.IMEI == "" {
		return rejectMismatch(RejectReasonMismatchStrict, SeverityHigh)
	}
	return Verdict{
		Kind:             VerdictAllow,
		BindingStatus:    BindingStatusVerified,
		EmitAudit:        true,
		AuditAction:      AuditActionFirstUseLocked,
		EmitNotification: true,
		NotifSubject:     NotifSubjectBindingLocked,
		Severity:         SeverityInfo,
		LockBoundIMEI:    true,
		NewBoundIMEI:     session.IMEI,
	}
}

// evalTACLock implements rows #13-#16. Compares the first 8 digits
// (TAC) of observed vs bound. Empty observed IMEI and absent bound IMEI
// are both rejects per row #15/#16 (operator must seed bound_imei for
// tac-lock — defensive default).
func (e *Enforcer) evalTACLock(session SessionContext, sim SIMView) Verdict {
	bound := derefStr(sim.BoundIMEI)
	if bound == "" || session.IMEI == "" {
		return rejectMismatch(RejectReasonMismatchTAC, SeverityMedium)
	}
	if tac(session.IMEI) == tac(bound) {
		return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
	}
	return rejectMismatch(RejectReasonMismatchTAC, SeverityMedium)
}

// evalGracePeriod implements rows #17-#21.
//
//   - Unbound + non-empty observed → first-use-style capture (row #17).
//   - Bound + match → row #18.
//   - Bound + differ + within window → accept change to status=pending,
//     audit binding_grace_change, notify medium (row #19). Refresh
//     binding_grace_expires_at on accepted change.
//   - Bound + differ + window expired → strict reject with the
//     GRACE_EXPIRED code (row #20).
//   - Bound + empty observed → reject MISMATCH_STRICT (row #21).
func (e *Enforcer) evalGracePeriod(session SessionContext, sim SIMView) Verdict {
	bound := derefStr(sim.BoundIMEI)
	if bound == "" {
		// Row #17 — first capture under grace mode.
		if session.IMEI == "" {
			return rejectMismatch(RejectReasonMismatchStrict, SeverityHigh)
		}
		return Verdict{
			Kind:               VerdictAllow,
			BindingStatus:      BindingStatusVerified,
			EmitAudit:          true,
			AuditAction:        AuditActionFirstUseLocked,
			EmitNotification:   true,
			NotifSubject:       NotifSubjectBindingLocked,
			Severity:           SeverityInfo,
			LockBoundIMEI:      true,
			NewBoundIMEI:       session.IMEI,
			RefreshGraceWindow: true,
		}
	}
	if session.IMEI == "" {
		// Row #21 — empty observed under grace.
		return rejectMismatch(RejectReasonMismatchStrict, SeverityHigh)
	}
	if session.IMEI == bound {
		// Row #18.
		return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
	}
	// Differ — check window.
	if e.graceWindowOpen(sim) {
		// Row #19 — accepted grace-period change.
		return Verdict{
			Kind:               VerdictAllowWithAlarm,
			Severity:           SeverityMedium,
			BindingStatus:      BindingStatusPending,
			EmitAudit:          true,
			AuditAction:        AuditActionGraceChange,
			EmitNotification:   true,
			NotifSubject:       NotifSubjectBindingGraceChange,
			HistoryWasMismatch: true,
			HistoryAlarmRaised: true,
			LockBoundIMEI:      true,
			NewBoundIMEI:       session.IMEI,
			RefreshGraceWindow: true,
		}
	}
	// Row #20 — grace expired → reject with GRACE_EXPIRED code.
	return rejectMismatch(RejectReasonGraceExpired, SeverityHigh)
}

// evalSoft implements rows #22-#25. Never rejects: any mismatch
// produces an AllowWithAlarm at info severity.
func (e *Enforcer) evalSoft(session SessionContext, sim SIMView) Verdict {
	bound := derefStr(sim.BoundIMEI)
	if session.IMEI == "" {
		// Row #24 — no observation, no row to write. Status untouched.
		return Verdict{Kind: VerdictAllow}
	}
	if bound == "" {
		// Row #25 — observed but nothing to compare; flag for review.
		return softAlarm()
	}
	if session.IMEI == bound {
		// Row #22.
		return Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
	}
	// Row #23.
	return softAlarm()
}

// rejectMismatch is a tiny helper for the common reject shape — keeps
// the per-mode functions short.
//
// HistoryWasMismatch is unconditionally true: the verdict carries the
// intent. Task 2's sink suppresses the actual imei_history insert when
// observed_imei is empty (TBL-59 NOT NULL constraint — the * footnote
// on the decision table). This is a deliberate separation — the
// enforcer signals "this was a mismatch", the sink applies the storage
// constraint.
//
// Notification subject is NotifSubjectIMEIChanged ("imei.changed") for
// all non-blacklist mismatch verdicts per STORY-097 AC-5. Blacklist
// verdicts use NotifSubjectBindingBlacklistHit and grace-period accepted
// changes use NotifSubjectBindingGraceChange — neither flow through this
// helper.
func rejectMismatch(reason string, sev Severity) Verdict {
	return Verdict{
		Kind:               VerdictReject,
		Reason:             reason,
		Severity:           sev,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionMismatch,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectIMEIChanged,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
}

func softAlarm() Verdict {
	return Verdict{
		Kind:               VerdictAllowWithAlarm,
		Severity:           SeverityInfo,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionSoftMismatch,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectIMEIChanged,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
}

// graceWindowOpen reports whether the SIM's binding_grace_expires_at is
// in the future. A nil expires-at is conservatively treated as already
// expired (the grace window must be explicitly opened by a prior
// first-use lock — row #17).
func (e *Enforcer) graceWindowOpen(sim SIMView) bool {
	if sim.BindingGraceExpiresAt == nil {
		return false
	}
	return e.now().Before(*sim.BindingGraceExpiresAt)
}

// tac returns the first 8 digits of an IMEI (the TAC — Type Allocation
// Code). Caller guarantees a non-empty IMEI; we still bound-check for
// safety (a 14-digit IMEI without check digit, or a malformed input,
// returns the input verbatim — the comparison will then succeed only
// if both bound and observed share the same prefix).
func tac(imei string) string {
	if len(imei) < 8 {
		return imei
	}
	return imei[:8]
}

// derefStr dereferences a *string, returning "" for nil. Centralised
// so the per-mode functions stay readable (PAT-009 nil-safety).
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
