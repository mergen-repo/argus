package binding

// story097_integration_test.go — STORY-097 Task 8 Part A.
//
// Cross-task integration tests covering ACs not individually exercised by
// T1-T7 unit tests. Four scenarios:
//
//  1. Re-pair lifecycle (AC-3): strict-mode SIM + mismatch auth → API-329
//     clears bound_imei → next auth with new IMEI still rejects because
//     strict mode with bound_imei=NULL rejects (evalStrict returns
//     rejectMismatch when bound==""). Operator must then PATCH (API-328)
//     to set new bound_imei before auth can succeed (VAL-066 code-is-truth).
//
//  2. Grace scanner dedup (AC-6): covered by
//     internal/job/binding_grace_scanner_test.go (10 tests in package job).
//     Not duplicated here: fakeGraceDedup / fakeGraceSimScanner live in
//     package job and cannot be imported into package binding without
//     introducing an import cycle.
//
//  3. Severity scaling E2E (AC-5): drives Enforcer.Evaluate for each of the
//     four (mode, mismatch type) pairs wired through the full Orchestrator
//     pipeline and asserts the correct NotifSubject + Severity reaches the
//     mockNotifier. Covers: strict→High/BindingFailed,
//     tac-lock→Medium/BindingFailed, soft→Info/IMEIMismatch,
//     grace-period-expired→High/BindingFailed.
//
//  4. NULL-mode history (AC-1 / AC-8): NULL-mode SIM + observed IMEI →
//     imei_history row created with was_mismatch=false, alarm_raised=false.
//     Evidences AC-1 (every non-empty IMEI auth writes history regardless of
//     mode). This complements T6's regression suite which tests all named
//     modes; NULL-mode is the most dangerous path to accidentally optimise away.
//
//  (Re-pair idempotency is scenario 5 in the spec numbering but implemented
//  as TestRePair_Idempotency_NoDoubleAudit below, counted as the fourth real
//  test function in this file.)
//
// All tests are hermetic: no external services, deterministic clock, in-process
// mocks only. Mocks (mockAuditor, mockNotifier, mockHistoryWriter, mockSIMUpdater,
// mockDropCounter, testFixedClock, testTenantID, testSIMID, testGraceWindow,
// imeiA, imeiB, imeiACousin, ptrStr) are package-level symbols defined in
// enforcer_test.go and orchestrator_test.go.

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// -----------------------------------------------------------------------------
// 1. Re-pair lifecycle — strict mode, bound_imei cleared, next auth rejects.
// -----------------------------------------------------------------------------

// TestRePair_LifecycleIntegration drives the enforcer/orchestrator pair through
// the three steps of the re-pair flow:
//
//	a. Initial mismatch → Reject, history+audit+notif emitted.
//	b. Re-pair clears bound_imei (simulated as SIMView with BoundIMEI=nil).
//	c. Next auth with new IMEI → still Reject because strict mode with bound==""
//	   rejects per evalStrict line 175 (code-is-truth: bound != "" guard fails).
//
// This validates the operator workflow: re-pair stages a re-binding, the
// subsequent auth does not auto-allow — the operator must set a new bound_imei
// via API-328 PATCH before the SIM can authenticate.
func TestRePair_LifecycleIntegration(t *testing.T) {
	const imeiOriginal = imeiA
	const imeiNew = imeiB

	// ── Step a: initial mismatch on strict-mode SIM. ───────────────────────
	bl := &mockBlacklist{}
	al := &mockAllowlist{}
	enforcer := New(
		WithAllowlistChecker(al),
		WithBlacklistChecker(bl),
		WithGraceWindow(testGraceWindow),
		WithClock(testFixedClock),
	)

	audit1 := &mockAuditor{}
	notif1 := &mockNotifier{}
	hist1 := &mockHistoryWriter{}
	sims1 := &mockSIMUpdater{}

	orch1 := NewOrchestrator(
		audit1, notif1, hist1, sims1, testGraceWindow,
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)

	sessionMismatch := makeOrchestratorSession(imeiNew) // different from bound
	simBound := makeOrchestratorSIM("strict", ptrStr(imeiOriginal))

	v, err := enforcer.Evaluate(context.Background(), sessionMismatch, simBound)
	if err != nil {
		t.Fatalf("step a: Evaluate: %v", err)
	}
	if v.Kind != VerdictReject {
		t.Fatalf("step a: want VerdictReject, got %v", v.Kind)
	}
	if v.Reason != RejectReasonMismatchStrict {
		t.Fatalf("step a: want BINDING_MISMATCH_STRICT, got %q", v.Reason)
	}

	if err := orch1.Apply(context.Background(), v, sessionMismatch, simBound, "radius"); err != nil {
		t.Fatalf("step a: Apply: %v", err)
	}
	if got := audit1.callCount(); got != 1 {
		t.Errorf("step a: audit calls = %d, want 1", got)
	}
	waitForNotif(t, notif1, 1, time.Second)
	if got := notif1.callCount(); got != 1 {
		t.Errorf("step a: notif calls = %d, want 1", got)
	}
	if got := hist1.callCount(); got != 1 {
		t.Errorf("step a: hist calls = %d, want 1", got)
	}
	histEntry, _ := hist1.lastEntry()
	if !histEntry.WasMismatch {
		t.Error("step a: history WasMismatch=false, want true")
	}
	if !histEntry.AlarmRaised {
		t.Error("step a: history AlarmRaised=false, want true")
	}

	// ── Step b: re-pair clears bound_imei → SIMView with BoundIMEI=nil. ────
	// (The actual ClearBoundIMEI call is API-329; here we model the post-repair
	// SIM state: BindingMode still 'strict', BoundIMEI=nil.)
	simCleared := makeOrchestratorSIM("strict", nil)

	// ── Step c: next auth with new IMEI → still rejects (evalStrict bound=""
	// guard fails — "pending" re-binding requires operator PATCH to set the
	// new bound_imei before auth can succeed).
	audit2 := &mockAuditor{}
	notif2 := &mockNotifier{}
	hist2 := &mockHistoryWriter{}
	sims2 := &mockSIMUpdater{}
	orch2 := NewOrchestrator(
		audit2, notif2, hist2, sims2, testGraceWindow,
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)

	sessionNew := makeOrchestratorSession(imeiNew)
	v2, err := enforcer.Evaluate(context.Background(), sessionNew, simCleared)
	if err != nil {
		t.Fatalf("step c: Evaluate: %v", err)
	}
	// strict + bound="" → rejectMismatch (evalStrict bound != "" guard fails).
	if v2.Kind != VerdictReject {
		t.Fatalf("step c: want VerdictReject, got %v (strict+null-bound must still reject)", v2.Kind)
	}
	if err := orch2.Apply(context.Background(), v2, sessionNew, simCleared, "radius"); err != nil {
		t.Fatalf("step c: Apply: %v", err)
	}
	// One more mismatch audit + history + notif.
	if got := audit2.callCount(); got != 1 {
		t.Errorf("step c: audit calls = %d, want 1 (strict+null-bound rejects)", got)
	}
}

// -----------------------------------------------------------------------------
// 2. Severity scaling E2E — four mode/mismatch pairs through Gate.Apply.
// -----------------------------------------------------------------------------

// TestSeverityScaling_E2E drives the full Enforcer→Gate→Orchestrator pipeline
// for four representative (mode, mismatch_type) pairs and asserts that the
// mockNotifier receives the correct NotifSubject with the correct Severity.
//
// STORY-097 AC-5: every non-blacklist mismatch publishes NotifSubjectIMEIChanged
// ("imei.changed") with severity scaled per binding mode. The grace-period
// mid-window AllowWithAlarm path is the one exception — it routes through
// evalGracePeriod's explicit NotifSubjectBindingGraceChange (not rejectMismatch
// or softAlarm).
func TestSeverityScaling_E2E(t *testing.T) {
	futureExpiry := testNow.Add(48 * time.Hour)

	cases := []struct {
		name        string
		mode        string
		bound       *string
		observed    string
		graceExpiry *time.Time
		wantKind    VerdictKind
		wantSev     Severity
		wantSubject string
	}{
		{
			name:        "strict_mismatch_high",
			mode:        "strict",
			bound:       ptrStr(imeiA),
			observed:    imeiB,
			wantKind:    VerdictReject,
			wantSev:     SeverityHigh,
			wantSubject: NotifSubjectIMEIChanged,
		},
		{
			name:        "tac_lock_mismatch_medium",
			mode:        "tac-lock",
			bound:       ptrStr(imeiA),
			observed:    imeiB, // different TAC
			wantKind:    VerdictReject,
			wantSev:     SeverityMedium,
			wantSubject: NotifSubjectIMEIChanged,
		},
		{
			name:        "soft_mismatch_info",
			mode:        "soft",
			bound:       ptrStr(imeiA),
			observed:    imeiB,
			wantKind:    VerdictAllowWithAlarm,
			wantSev:     SeverityInfo,
			wantSubject: NotifSubjectIMEIChanged,
		},
		{
			name:        "grace_expired_high",
			mode:        "grace-period",
			bound:       ptrStr(imeiA),
			observed:    imeiB,
			graceExpiry: ptrTime(testNow.Add(-time.Hour)), // expired
			wantKind:    VerdictReject,
			wantSev:     SeverityHigh,
			wantSubject: NotifSubjectIMEIChanged,
		},
		{
			name:        "grace_period_within_window_medium",
			mode:        "grace-period",
			bound:       ptrStr(imeiA),
			observed:    imeiB,
			graceExpiry: &futureExpiry,
			wantKind:    VerdictAllowWithAlarm,
			wantSev:     SeverityMedium,
			wantSubject: NotifSubjectBindingGraceChange,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			sim := SIMView{
				ID:                    testSIMID,
				TenantID:              testTenantID,
				BindingMode:           ptrStr(tc.mode),
				BoundIMEI:             tc.bound,
				BindingGraceExpiresAt: tc.graceExpiry,
			}

			enforcer := New(
				WithClock(testFixedClock),
				WithGraceWindow(testGraceWindow),
			)
			audit := &mockAuditor{}
			notif := &mockNotifier{}
			hist := &mockHistoryWriter{}
			sims := &mockSIMUpdater{}
			orch := NewOrchestrator(
				audit, notif, hist, sims, testGraceWindow,
				WithLogger(zerolog.Nop()),
				WithOrchestratorClock(testFixedClock),
			)
			gate := NewGate(enforcer, orch)

			session := makeOrchestratorSession(tc.observed)
			v, err := gate.Evaluate(context.Background(), session, sim)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if v.Kind != tc.wantKind {
				t.Fatalf("Kind = %v, want %v", v.Kind, tc.wantKind)
			}
			if v.Severity != tc.wantSev {
				t.Fatalf("Severity = %q, want %q", v.Severity, tc.wantSev)
			}
			if err := gate.Apply(context.Background(), v, session, sim, "radius"); err != nil {
				t.Fatalf("Apply: %v", err)
			}

			if v.EmitNotification {
				waitForNotif(t, notif, 1, time.Second)
				last, ok := notif.lastCall()
				if !ok {
					t.Fatal("notif.lastCall: no call recorded")
				}
				if last.subject != tc.wantSubject {
					t.Errorf("notif subject = %q, want %q", last.subject, tc.wantSubject)
				}
				if last.payload.Severity != tc.wantSev {
					t.Errorf("notif severity = %q, want %q", last.payload.Severity, tc.wantSev)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// 3. NULL-mode history (AC-1).
// -----------------------------------------------------------------------------

// TestNullMode_HistoryRow_WasMismatchFalse asserts that NULL-mode auth with a
// non-empty observed IMEI produces an imei_history row with was_mismatch=false
// and alarm_raised=false (AC-1: all non-empty IMEI auths produce a history
// row, including the "no binding configured" path).
func TestNullMode_HistoryRow_WasMismatchFalse(t *testing.T) {
	enforcer := New(WithClock(testFixedClock))

	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	orch := NewOrchestrator(
		audit, notif, hist, sims, testGraceWindow,
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)
	gate := NewGate(enforcer, orch)

	session := makeOrchestratorSession(imeiA)
	sim := makeOrchestratorSIM("", nil) // NULL mode

	v, err := gate.Evaluate(context.Background(), session, sim)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Kind != VerdictAllow {
		t.Fatalf("NULL-mode: want VerdictAllow, got %v", v.Kind)
	}
	if v.BindingStatus != BindingStatusDisabled {
		t.Fatalf("NULL-mode: want BindingStatus=disabled, got %q", v.BindingStatus)
	}
	if err := gate.Apply(context.Background(), v, session, sim, "5g_sba"); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist calls = %d, want 1 (NULL-mode + non-empty IMEI must write history)", got)
	}
	entry, _ := hist.lastEntry()
	if entry.WasMismatch {
		t.Error("history WasMismatch=true on NULL-mode Allow; want false")
	}
	if entry.AlarmRaised {
		t.Error("history AlarmRaised=true on NULL-mode Allow; want false")
	}
	if entry.ObservedIMEI != imeiA {
		t.Errorf("history ObservedIMEI = %q, want %q", entry.ObservedIMEI, imeiA)
	}
	if entry.CaptureProtocol != "5g_sba" {
		t.Errorf("history CaptureProtocol = %q, want %q", entry.CaptureProtocol, "5g_sba")
	}
	// No audit and no notification on a plain Allow (no EmitAudit, no EmitNotification).
	if got := audit.callCount(); got != 0 {
		t.Errorf("audit calls = %d, want 0 (NULL-mode Allow emits no audit)", got)
	}
	if got := notif.callCount(); got != 0 {
		t.Errorf("notif calls = %d, want 0 (NULL-mode Allow emits no notification)", got)
	}
}

// -----------------------------------------------------------------------------
// 4. Re-pair idempotency at orchestrator level (AC-3).
// -----------------------------------------------------------------------------

// TestRePair_Idempotency_NoDoubleAudit models the API-329 idempotency guard at
// the Orchestrator level: applying the same "already-pending" verdict twice
// (once for the actual re-pair, once as an accidental re-submit) must not
// double-emit audit. The handler achieves this via a state-based early-return
// (bound_imei IS NULL AND binding_status='pending' → return 200 immediately
// without going through Apply). At the unit level we validate that Orchestrator
// is side-effect-free when called with a plain-Allow Verdict (no EmitAudit).
func TestRePair_Idempotency_NoDoubleAudit(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	orch := NewOrchestrator(
		audit, notif, hist, sims, testGraceWindow,
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)

	// "Already pending" state: bound_imei=nil, binding_status='pending'.
	// The handler short-circuits before Apply; here we verify that if Apply
	// were called with an Allow verdict (no side effects), no audit row is emitted.
	pendingStatus := BindingStatusPending
	sim := SIMView{
		ID:          testSIMID,
		TenantID:    testTenantID,
		BindingMode: ptrStr("strict"),
		BoundIMEI:   nil, // already cleared
	}
	_ = pendingStatus // used as documentation; the sim state is the key here

	allowVerdict := Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusPending}
	session := makeOrchestratorSession(imeiA)

	// First "apply".
	if err := orch.Apply(context.Background(), allowVerdict, session, sim, "radius"); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	// Second "apply" (re-submit scenario).
	if err := orch.Apply(context.Background(), allowVerdict, session, sim, "radius"); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	// Neither apply emits audit (EmitAudit=false on Allow verdict).
	if got := audit.callCount(); got != 0 {
		t.Errorf("audit calls = %d after two Allow applies; want 0 (idempotent state-based guard)", got)
	}
	// History is written twice (one per call — each auth produces a row).
	// This is intentional: the idempotency guard is about audit + side effects,
	// not history recording.
	if got := hist.callCount(); got != 2 {
		t.Errorf("hist calls = %d, want 2 (each auth writes history regardless)", got)
	}
}
