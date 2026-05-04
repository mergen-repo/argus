package binding

// history_writeback_regression_test.go — STORY-097 Task 6 Part A.
//
// Regression-protection tests for AC-1: every auth with a non-empty
// observed IMEI produces an imei_history row, regardless of the
// binding_mode value (including NULL-mode, which is the case most
// easily narrowed away in a future refactor).
//
// Mocks (mockAuditor, mockNotifier, mockHistoryWriter, mockSIMUpdater,
// mockDropCounter, newTestOrchestrator, makeOrchestratorSession,
// makeOrchestratorSIM, testFixedClock) are package-level symbols defined
// in orchestrator_test.go — no redefinition here.
//
// Enforcer mocks (mockAllowlist, mockBlacklist, fixedClock, makeSIM,
// makeSession, imeiA, imeiB, ptrStr, ptrTime) are package-level symbols
// defined in enforcer_test.go.

import (
	"context"
	"testing"
)

// TestHistoryWriteback_NullMode_NonEmptyIMEI_StillWritesHistory asserts
// that Orchestrator.Apply records a history row for a plain Allow verdict
// produced under NULL binding_mode when the observed IMEI is non-empty.
// This is the path most likely to be accidentally narrowed ("only write
// history on mismatch") in a refactor.
func TestHistoryWriteback_NullMode_NonEmptyIMEI_StillWritesHistory(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusDisabled}
	session := makeOrchestratorSession(imeiA)
	sim := makeOrchestratorSIM("", nil)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1 (NULL-mode + non-empty IMEI must write history)", got)
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
}

// TestHistoryWriteback_NullMode_EmptyIMEI_SkipsHistory asserts that when
// the observed IMEI is empty, even a plain Allow verdict must NOT produce
// an imei_history insert (TBL-59 NOT NULL constraint — AC-11).
func TestHistoryWriteback_NullMode_EmptyIMEI_SkipsHistory(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusDisabled}
	session := makeOrchestratorSession("")
	sim := makeOrchestratorSIM("", nil)

	if err := o.Apply(context.Background(), v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := hist.callCount(); got != 0 {
		t.Errorf("hist.callCount = %d, want 0 (empty IMEI must skip history insert)", got)
	}
}

// TestHistoryWriteback_StrictMode_AllowedMatch_WritesHistory asserts that
// a matching strict-mode auth (Allow, no mismatch) still appends a row.
func TestHistoryWriteback_StrictMode_AllowedMatch_WritesHistory(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{Kind: VerdictAllow, BindingStatus: BindingStatusVerified}
	session := makeOrchestratorSession(imeiA)
	sim := makeOrchestratorSIM("strict", &[]string{imeiA}[0])

	if err := o.Apply(context.Background(), v, session, sim, "diameter_s6a"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1 (strict match must still write history)", got)
	}
	entry, _ := hist.lastEntry()
	if entry.WasMismatch {
		t.Error("history WasMismatch=true on strict-match Allow; want false")
	}
	if entry.AlarmRaised {
		t.Error("history AlarmRaised=true on strict-match Allow; want false")
	}
}

// TestHistoryWriteback_StrictMode_Mismatch_WritesHistoryWithFlags asserts
// that a strict-mode mismatch Reject produces a history row with
// WasMismatch=true and AlarmRaised=true.
func TestHistoryWriteback_StrictMode_Mismatch_WritesHistoryWithFlags(t *testing.T) {
	audit := &mockAuditor{}
	notif := &mockNotifier{publishC: make(chan struct{}, 1)}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	v := Verdict{
		Kind:               VerdictReject,
		Reason:             RejectReasonMismatchStrict,
		Severity:           SeverityHigh,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionMismatch,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectIMEIChanged,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
	bound := imeiA
	session := makeOrchestratorSession(imeiB)
	sim := makeOrchestratorSIM("strict", &bound)

	if err := o.Apply(context.Background(), v, session, sim, "5g_sba"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1", got)
	}
	entry, _ := hist.lastEntry()
	if !entry.WasMismatch {
		t.Error("history WasMismatch=false on strict mismatch; want true")
	}
	if !entry.AlarmRaised {
		t.Error("history AlarmRaised=false on strict mismatch; want true")
	}
}

// TestHistoryWriteback_BlacklistOverride_WritesHistoryWithFlags drives the
// full Enforcer→Orchestrator chain with a NULL-mode SIM whose observed
// IMEI is in the blacklist. The blacklist override must produce a history
// row (WasMismatch=true, AlarmRaised=true) even though the SIM itself has
// no binding_mode configured.
func TestHistoryWriteback_BlacklistOverride_WritesHistoryWithFlags(t *testing.T) {
	bl := &mockBlacklist{hit: true}
	enforcer := New(
		WithBlacklistChecker(bl),
		WithClock(fixedClock),
	)

	audit := &mockAuditor{}
	notif := &mockNotifier{publishC: make(chan struct{}, 1)}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	o := newTestOrchestrator(t, audit, notif, hist, sims, metrics)

	sim := makeSIM("", nil, nil)
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
		t.Fatalf("verdict reason = %q, want %q (blacklist override)", v.Reason, RejectReasonBlacklist)
	}

	if err := o.Apply(ctx, v, session, sim, "radius"); err != nil {
		t.Fatalf("Apply: unexpected err: %v", err)
	}

	if got := hist.callCount(); got != 1 {
		t.Fatalf("hist.callCount = %d, want 1 (blacklist override must write history)", got)
	}
	entry, _ := hist.lastEntry()
	if !entry.WasMismatch {
		t.Error("history WasMismatch=false on blacklist override; want true")
	}
	if !entry.AlarmRaised {
		t.Error("history AlarmRaised=false on blacklist override; want true")
	}
}
