package binding

// decision_table_e2e_test.go — STORY-096 Task 8 Part B.
//
// End-to-end chain of Enforcer.Evaluate → Orchestrator.Apply for every
// row of the 25-row decision table. T1's enforcer_test.go tested the
// Verdict shape; T2's orchestrator_test.go tested the sinks with
// hand-built Verdicts. This test asserts the chain — for each row,
// the Enforcer's actual Verdict feeds the Orchestrator's Apply and
// every sink fires exactly as the Verdict signals.
//
// Sink-count expectations are derived mechanically from the Verdict
// fields (per advisor guidance) — no hand-authored 25×4 table:
//   - audit count   = 1 if v.EmitAudit else 0
//   - notif count   = 1 if v.EmitNotification else 0 (after waitForNotif)
//   - history count = 1 if session.IMEI != "" else 0
//   - sims count    = 1 if v.LockBoundIMEI || v.RefreshGraceWindow else 0
//
// The test thus catches drift between Verdict-shape and sink-fan-out
// even when both halves pass their own unit suites.

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// e2eDecisionRow is a single row of the decision table. The shape
// mirrors enforcer_test.go's anonymous struct with one addition: the
// expected Verdict is computed by running the Enforcer (so we don't
// duplicate T1's expected-value column).
type e2eDecisionRow struct {
	name         string
	mode         string
	bound        *string
	observed     string
	allowlistHit bool
	graceOpen    bool // whether binding_grace_expires_at is in the future
}

// e2eDecisionTable enumerates the same 25 rows as
// enforcer_test.go:TestEvaluate_DecisionTable but without the expected
// Verdict — the Enforcer computes it for us.
func e2eDecisionTable() []e2eDecisionRow {
	return []e2eDecisionRow{
		{name: "row01_null_no_binding", mode: "", bound: nil, observed: imeiA},
		{name: "row02_strict_match", mode: "strict", bound: ptrStr(imeiA), observed: imeiA},
		{name: "row03_strict_differ", mode: "strict", bound: ptrStr(imeiA), observed: imeiB},
		{name: "row04_strict_empty", mode: "strict", bound: ptrStr(imeiA), observed: ""},
		{name: "row05_strict_absent", mode: "strict", bound: nil, observed: imeiA},
		{name: "row06_allowlist_match", mode: "allowlist", bound: nil, observed: imeiA, allowlistHit: true},
		{name: "row07_allowlist_miss", mode: "allowlist", bound: nil, observed: imeiA, allowlistHit: false},
		{name: "row08_allowlist_empty", mode: "allowlist", bound: nil, observed: ""},
		{name: "row09_firstuse_capture", mode: "first-use", bound: nil, observed: imeiA},
		{name: "row10_firstuse_match", mode: "first-use", bound: ptrStr(imeiA), observed: imeiA},
		{name: "row11_firstuse_differ", mode: "first-use", bound: ptrStr(imeiA), observed: imeiB},
		{name: "row12_firstuse_empty", mode: "first-use", bound: nil, observed: ""},
		{name: "row13_taclock_match", mode: "tac-lock", bound: ptrStr(imeiA), observed: imeiACousin},
		{name: "row14_taclock_differ", mode: "tac-lock", bound: ptrStr(imeiA), observed: imeiB},
		{name: "row15_taclock_empty", mode: "tac-lock", bound: ptrStr(imeiA), observed: ""},
		{name: "row16_taclock_absent", mode: "tac-lock", bound: nil, observed: imeiA},
		{name: "row17_grace_capture", mode: "grace-period", bound: nil, observed: imeiA},
		{name: "row18_grace_match", mode: "grace-period", bound: ptrStr(imeiA), observed: imeiA, graceOpen: true},
		{name: "row19_grace_differ_open", mode: "grace-period", bound: ptrStr(imeiA), observed: imeiB, graceOpen: true},
		{name: "row20_grace_differ_expired", mode: "grace-period", bound: ptrStr(imeiA), observed: imeiB, graceOpen: false},
		{name: "row21_grace_empty_in_window", mode: "grace-period", bound: ptrStr(imeiA), observed: "", graceOpen: true},
		{name: "row22_soft_match", mode: "soft", bound: ptrStr(imeiA), observed: imeiA},
		{name: "row23_soft_differ", mode: "soft", bound: ptrStr(imeiA), observed: imeiB},
		{name: "row24_soft_empty", mode: "soft", bound: ptrStr(imeiA), observed: ""},
		{name: "row25_soft_absent_observed", mode: "soft", bound: nil, observed: imeiA},
	}
}

// TestDecisionTable_E2E_ChainEvaluateAndApply runs every row through
// the full Enforcer→Orchestrator chain and asserts sink fan-out
// matches the Verdict signal. Note: the Enforcer + Orchestrator share
// the same fixedNow (testNow == fixedNow == 2026-05-03T12:00Z) so the
// grace-window expiry assertion is deterministic.
func TestDecisionTable_E2E_ChainEvaluateAndApply(t *testing.T) {
	// Sanity: enforcer_test.go's fixedNow MUST match orchestrator_test.go's
	// testNow so the grace-window arithmetic stays deterministic when the
	// chain mixes both clocks. Catch drift here so the silently-passing
	// graceExpires assertion below does not become flaky.
	if !fixedNow.Equal(testNow) {
		t.Fatalf("clock fixtures drift: fixedNow=%v testNow=%v — must match for E2E grace arithmetic", fixedNow, testNow)
	}
	for _, tt := range e2eDecisionTable() {
		t.Run(tt.name, func(t *testing.T) {
			al := &mockAllowlist{allowed: tt.allowlistHit}
			// No blacklist mock — covered by the dedicated cross-protocol
			// suite + the orchestrator-level blacklist test. Keeps this
			// row-by-row chain clean (every row corresponds 1:1 to its
			// enforcer_test.go counterpart).
			enforcer := New(
				WithAllowlistChecker(al),
				WithGraceWindow(testGraceWindow),
				WithClock(fixedClock),
			)

			audit := &mockAuditor{}
			notif := &mockNotifier{}
			hist := &mockHistoryWriter{}
			sims := &mockSIMUpdater{}
			metrics := &mockDropCounter{}

			orch := NewOrchestrator(
				audit, notif, hist, sims, testGraceWindow,
				WithDropCounter(metrics),
				WithLogger(zerolog.Nop()),
				WithOrchestratorClock(testFixedClock),
			)

			// Build a SIM matching the row's setup. enforcer_test.go's
			// makeSIM mints fresh UUIDs per call — we use it directly so
			// the Orchestrator sees a unique SIM per row (no cross-row
			// state pollution in mocks).
			var graceExp *time.Time
			if tt.graceOpen {
				graceExp = ptrTime(fixedNow.Add(1 * time.Hour))
			} else if tt.mode == "grace-period" && tt.bound != nil {
				graceExp = ptrTime(fixedNow.Add(-1 * time.Hour))
			}
			sim := makeSIM(tt.mode, tt.bound, graceExp)
			session := SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            tt.observed,
				SoftwareVersion: "1.0",
			}

			ctx := context.Background()
			v, err := enforcer.Evaluate(ctx, session, sim)
			if err != nil {
				t.Fatalf("Evaluate: unexpected err: %v", err)
			}

			if err := orch.Apply(ctx, v, session, sim, "radius"); err != nil {
				t.Fatalf("Apply: unexpected err: %v", err)
			}

			// Audit fan-out — count + action + reason propagation.
			wantAudit := 0
			if v.EmitAudit {
				wantAudit = 1
			}
			if got := audit.callCount(); got != wantAudit {
				t.Errorf("audit calls = %d, want %d", got, wantAudit)
			}
			if v.EmitAudit {
				if audit.calls[0].action != v.AuditAction {
					t.Errorf("audit action = %q, want %q (verdict)", audit.calls[0].action, v.AuditAction)
				}
				if audit.calls[0].payload.ReasonCode != v.Reason {
					t.Errorf("audit reason = %q, want %q (verdict)", audit.calls[0].payload.ReasonCode, v.Reason)
				}
			}

			// Notification fan-out — async; wait if expected.
			wantNotif := 0
			if v.EmitNotification {
				wantNotif = 1
				waitForNotif(t, notif, 1, time.Second)
			}
			if got := notif.callCount(); got != wantNotif {
				t.Errorf("notif calls = %d, want %d", got, wantNotif)
			}
			if v.EmitNotification {
				last, _ := notif.lastCall()
				if last.subject != v.NotifSubject {
					t.Errorf("notif subject = %q, want %q (verdict)", last.subject, v.NotifSubject)
				}
				if last.payload.Severity != v.Severity {
					t.Errorf("notif severity = %q, want %q (verdict)", last.payload.Severity, v.Severity)
				}
			}

			// History fan-out — fires only on non-empty observed IMEI.
			wantHist := 0
			if session.IMEI != "" {
				wantHist = 1
			}
			if got := hist.callCount(); got != wantHist {
				t.Errorf("history calls = %d, want %d (observed=%q)", got, wantHist, session.IMEI)
			}
			if wantHist == 1 {
				entry, _ := hist.lastEntry()
				if entry.WasMismatch != v.HistoryWasMismatch {
					t.Errorf("history WasMismatch = %v, want %v (verdict)", entry.WasMismatch, v.HistoryWasMismatch)
				}
				if entry.AlarmRaised != v.HistoryAlarmRaised {
					t.Errorf("history AlarmRaised = %v, want %v (verdict)", entry.AlarmRaised, v.HistoryAlarmRaised)
				}
				if entry.ObservedIMEI != session.IMEI {
					t.Errorf("history ObservedIMEI = %q, want %q", entry.ObservedIMEI, session.IMEI)
				}
			}

			// SIM update fan-out — count derived from Verdict signals.
			wantSimUpdate := 0
			if v.LockBoundIMEI || v.RefreshGraceWindow {
				wantSimUpdate = 1
			}
			if got := sims.callCount(); got != wantSimUpdate {
				t.Errorf("sims calls = %d, want %d", got, wantSimUpdate)
			}
			if wantSimUpdate == 1 {
				last, _ := sims.lastCall()
				// LockBoundIMEI=true → imei == NewBoundIMEI.
				if v.LockBoundIMEI && last.imei != v.NewBoundIMEI {
					t.Errorf("sim update imei = %q, want %q (verdict)", last.imei, v.NewBoundIMEI)
				}
				// RefreshGraceWindow=true → graceExpires == now+window.
				if v.RefreshGraceWindow {
					if last.graceExpires == nil {
						t.Fatal("sim update graceExpires = nil, want refreshed expiry")
					}
					wantExpires := testNow.Add(testGraceWindow).UTC()
					if !last.graceExpires.Equal(wantExpires) {
						t.Errorf("sim update graceExpires = %v, want %v", last.graceExpires, wantExpires)
					}
				} else if last.graceExpires != nil {
					t.Errorf("sim update graceExpires = %v, want nil", last.graceExpires)
				}
			}
		})
	}
}

// TestDecisionTable_E2E_BlacklistOverridesAllModes asserts that a
// blacklist hit short-circuits every mode — the Enforcer returns the
// blacklist Verdict, and the Orchestrator routes that verdict to the
// blacklist-specific audit action + notification subject. This is the
// E2E equivalent of TestEvaluate_BlacklistOverride_AllModes (which
// stops at the Enforcer boundary).
func TestDecisionTable_E2E_BlacklistOverridesAllModes(t *testing.T) {
	modes := []struct {
		name  string
		mode  string
		bound *string
	}{
		{"null", "", nil},
		{"strict", "strict", ptrStr(imeiA)},
		{"allowlist", "allowlist", nil},
		{"first-use", "first-use", ptrStr(imeiA)},
		{"tac-lock", "tac-lock", ptrStr(imeiA)},
		{"grace-period", "grace-period", ptrStr(imeiA)},
		{"soft", "soft", ptrStr(imeiA)},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			bl := &mockBlacklist{hit: true}
			al := &mockAllowlist{allowed: true}
			enforcer := New(
				WithAllowlistChecker(al),
				WithBlacklistChecker(bl),
				WithGraceWindow(testGraceWindow),
				WithClock(fixedClock),
			)

			audit := &mockAuditor{}
			notif := &mockNotifier{}
			hist := &mockHistoryWriter{}
			sims := &mockSIMUpdater{}
			metrics := &mockDropCounter{}

			orch := NewOrchestrator(
				audit, notif, hist, sims, testGraceWindow,
				WithDropCounter(metrics),
				WithLogger(zerolog.Nop()),
				WithOrchestratorClock(testFixedClock),
			)

			sim := makeSIM(m.mode, m.bound, ptrTime(fixedNow.Add(1*time.Hour)))
			session := SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            imeiA,
				SoftwareVersion: "1.0",
			}

			ctx := context.Background()
			v, err := enforcer.Evaluate(ctx, session, sim)
			if err != nil {
				t.Fatalf("Evaluate: unexpected err: %v", err)
			}
			if v.Reason != RejectReasonBlacklist {
				t.Fatalf("verdict reason = %q, want %q (mode=%s)", v.Reason, RejectReasonBlacklist, m.mode)
			}

			if err := orch.Apply(ctx, v, session, sim, "radius"); err != nil {
				t.Fatalf("Apply: unexpected err: %v", err)
			}

			waitForNotif(t, notif, 1, time.Second)

			if audit.callCount() != 1 {
				t.Errorf("audit calls = %d, want 1", audit.callCount())
			}
			if audit.calls[0].action != AuditActionBlacklistHit {
				t.Errorf("audit action = %q, want %q", audit.calls[0].action, AuditActionBlacklistHit)
			}
			last, _ := notif.lastCall()
			if last.subject != NotifSubjectBindingBlacklistHit {
				t.Errorf("notif subject = %q, want %q", last.subject, NotifSubjectBindingBlacklistHit)
			}
			if last.payload.Severity != SeverityHigh {
				t.Errorf("notif severity = %q, want high", last.payload.Severity)
			}
			// Blacklist override never locks bound_imei nor refreshes
			// the grace window.
			if sims.callCount() != 0 {
				t.Errorf("sims update calls = %d, want 0", sims.callCount())
			}
			// History MUST be recorded for every blacklist hit (with a
			// non-empty observed IMEI it is the canonical forensic
			// trail).
			if hist.callCount() != 1 {
				t.Errorf("history calls = %d, want 1", hist.callCount())
			}
		})
	}
}
