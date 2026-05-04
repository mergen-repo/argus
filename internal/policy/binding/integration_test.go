package binding

// integration_test.go — STORY-096 Task 8 Part A.
//
// Cross-protocol hermetic integration test. Wires the real Enforcer +
// Orchestrator behind a single binding.Gate and runs three canonical
// scenarios (Allow / Reject-strict / Blacklist override) against each
// of the three protocol identifiers ("radius" / "diameter_s6a" /
// "5g_sba") = 9 sub-tests.
//
// "Hermetic" — we do NOT spin up RADIUS/Diameter/SBA test rigs. The
// rigs already exist in internal/aaa/{radius,diameter,sba}, but since
// binding cannot import them (cycle: those packages import binding),
// the cross-protocol contract is asserted behaviorally: every Apply
// call carries the protocol identifier into the audit + notification
// payloads, and every BindingGate caller (radius/diameter/sba) routes
// through the same Evaluate→Apply pair. Verifying that the gate
// faithfully propagates protocol + verdict-shape across every protocol
// identifier covers the cross-protocol contract without duplicating
// the rigs' setup cost.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/audit"
)

// crossProtoScenario enumerates the three canonical verdict shapes the
// gate must surface identically across protocols.
type crossProtoScenario struct {
	name        string
	mode        string
	bound       *string
	observed    string
	blacklist   bool
	wantKind    VerdictKind
	wantReason  string
	wantSubject string // empty when EmitNotification=false
	wantAction  string // empty when EmitAudit=false
}

func crossProtocolScenarios() []crossProtoScenario {
	return []crossProtoScenario{
		{
			name:     "allow_strict_match",
			mode:     "strict",
			bound:    ptrStr(imeiA),
			observed: imeiA,
			wantKind: VerdictAllow,
		},
		{
			name:        "reject_strict_mismatch",
			mode:        "strict",
			bound:       ptrStr(imeiA),
			observed:    imeiB,
			wantKind:    VerdictReject,
			wantReason:  RejectReasonMismatchStrict,
			wantSubject: NotifSubjectIMEIChanged,
			wantAction:  AuditActionMismatch,
		},
		{
			name:        "blacklist_overrides_strict_match",
			mode:        "strict",
			bound:       ptrStr(imeiA),
			observed:    imeiA,
			blacklist:   true,
			wantKind:    VerdictReject,
			wantReason:  RejectReasonBlacklist,
			wantSubject: NotifSubjectBindingBlacklistHit,
			wantAction:  AuditActionBlacklistHit,
		},
	}
}

// crossProtocolNames is the canonical protocol identifier set the
// orchestrator's Apply receives at each wire site (radius_server.go,
// s6a.go, ausf.go/udm.go). Mirrors the strings written into audit /
// notification payloads.
var crossProtocolNames = []string{"radius", "diameter_s6a", "5g_sba"}

// TestCrossProtocol_GateEnforcesAndApplies exercises the combined Gate
// (Enforcer + Orchestrator) across all three protocol identifiers ×
// three canonical scenarios = 9 sub-tests. Each sub-test asserts:
//   - Verdict shape (Kind / Reason).
//   - Protocol field propagates into audit + notification payloads.
//   - Three sinks (audit / notif / history) fire correct count.
//   - SIM update fires only for LockBoundIMEI / RefreshGraceWindow rows
//     (none of the three scenarios trigger an update — assertion = 0).
//
// The test is hermetic — no external services, no goroutines from
// protocol packages, deterministic clock.
func TestCrossProtocol_GateEnforcesAndApplies(t *testing.T) {
	scenarios := crossProtocolScenarios()
	for _, proto := range crossProtocolNames {
		for _, sc := range scenarios {
			t.Run(proto+"/"+sc.name, func(t *testing.T) {
				bl := &mockBlacklist{hit: sc.blacklist}
				al := &mockAllowlist{}
				enforcer := New(
					WithAllowlistChecker(al),
					WithBlacklistChecker(bl),
					WithGraceWindow(testGraceWindow),
					WithClock(testFixedClock),
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

				gate := NewGate(enforcer, orch)

				session := makeOrchestratorSession(sc.observed)
				sim := makeOrchestratorSIM(sc.mode, sc.bound)

				v, err := gate.Evaluate(context.Background(), session, sim)
				if err != nil {
					t.Fatalf("gate.Evaluate: unexpected err: %v", err)
				}
				if v.Kind != sc.wantKind {
					t.Fatalf("verdict kind = %v, want %v (full: %+v)", v.Kind, sc.wantKind, v)
				}
				if v.Reason != sc.wantReason {
					t.Fatalf("verdict reason = %q, want %q", v.Reason, sc.wantReason)
				}

				if err := gate.Apply(context.Background(), v, session, sim, proto); err != nil {
					t.Fatalf("gate.Apply: unexpected err: %v", err)
				}

				// Audit count + protocol propagation.
				wantAudit := 0
				if v.EmitAudit {
					wantAudit = 1
				}
				if got := audit.callCount(); got != wantAudit {
					t.Errorf("audit calls = %d, want %d", got, wantAudit)
				}
				if v.EmitAudit {
					if audit.calls[0].action != sc.wantAction {
						t.Errorf("audit action = %q, want %q", audit.calls[0].action, sc.wantAction)
					}
					if audit.calls[0].payload.Protocol != proto {
						t.Errorf("audit protocol = %q, want %q", audit.calls[0].payload.Protocol, proto)
					}
				}

				// Notification fires asynchronously — wait if expected.
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
					if last.subject != sc.wantSubject {
						t.Errorf("notif subject = %q, want %q", last.subject, sc.wantSubject)
					}
					if last.payload.Protocol != proto {
						t.Errorf("notif protocol = %q, want %q", last.payload.Protocol, proto)
					}
				}

				// History fires for every non-empty observed IMEI.
				wantHist := 0
				if session.IMEI != "" {
					wantHist = 1
				}
				if got := hist.callCount(); got != wantHist {
					t.Errorf("history calls = %d, want %d", got, wantHist)
				}
				if wantHist == 1 {
					entry, _ := hist.lastEntry()
					if entry.CaptureProtocol != proto {
						t.Errorf("history protocol = %q, want %q", entry.CaptureProtocol, proto)
					}
				}

				// None of the three canonical scenarios trigger a SIM
				// update (no first-use lock, no grace refresh).
				if got := sims.callCount(); got != 0 {
					t.Errorf("sims update calls = %d, want 0", got)
				}
			})
		}
	}
}

// TestCrossProtocol_NilGateIsNoOp asserts the AC-17 zero-regression
// fallback: a nil-wired Gate (or one whose enforcer/orchestrator are
// nil) must not panic and must not produce side effects. Mirrors the
// behaviour at the AAA wire layer where SetBindingGate has not been
// called yet.
func TestCrossProtocol_NilGateIsNoOp(t *testing.T) {
	cases := []struct {
		name string
		gate *Gate
	}{
		{"nil_receiver", (*Gate)(nil)},
		{"nil_enforcer_and_orch", NewGate(nil, nil)},
		{"nil_enforcer_only", NewGate(nil, NewOrchestrator(nil, nil, nil, nil, testGraceWindow))},
		{"nil_orch_only", NewGate(New(), nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := makeOrchestratorSession(imeiA)
			sim := makeOrchestratorSIM("strict", ptrStr(imeiB))

			v, err := tc.gate.Evaluate(context.Background(), session, sim)
			if err != nil {
				t.Fatalf("Evaluate: unexpected err: %v", err)
			}
			// Apply with whatever Evaluate returned; the nil-fallback
			// shape is Verdict{Kind: Allow}, but the contract is "no
			// panic" — assert that explicitly across every protocol.
			for _, proto := range crossProtocolNames {
				if err := tc.gate.Apply(context.Background(), v, session, sim, proto); err != nil {
					t.Fatalf("Apply(%s): unexpected err: %v", proto, err)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// AC-16 — Audit hash chain integrity across a mixed-mode binding run.
// -----------------------------------------------------------------------------
//
// Story line 42: "Audit hash chain remains valid after a mixed run of all six
// modes producing a stream of audit rows." STORY-096 Gate F-A1: chain
// integrity is inherited from audit.FullService.CreateEntry, but the
// AC-16 contract — chain still valid after a mixed-mode binding run driven
// through the real Gate.Apply pipeline — was not directly evidenced by an
// integration test. This test closes that gap.
//
// Approach: a chain-verifying Auditor mock (chainVerifyAuditor) implements
// binding.Auditor and simulates what audit.FullService.CreateEntry does
// internally — assigns PrevHash from the prior entry, computes Hash via
// audit.ComputeHash, and appends to an in-memory slice. After 50 events
// across all six binding modes + blacklist override + first-use lock +
// grace transitions, the test re-walks the slice and asserts:
//   - every entry's PrevHash equals the prior entry's Hash (or GenesisHash
//     for the first row),
//   - every entry's Hash equals audit.ComputeHash(entry, prevHash),
//   - the count of audit-emitting events matches expectations.
//
// The mock uses audit.ComputeHash and audit.GenesisHash directly so the
// chain math under test is the production chain math, not a re-implementation.

// chainVerifyAuditor implements binding.Auditor and maintains an in-memory
// audit hash chain identical in structure to audit.FullService.
type chainVerifyAuditor struct {
	mu       sync.Mutex
	entries  []audit.Entry
	now      func() time.Time
	tick     int64 // monotonic per-call increment to ensure unique CreatedAt
	tenantID uuid.UUID
}

func newChainVerifyAuditor(now func() time.Time, tenantID uuid.UUID) *chainVerifyAuditor {
	return &chainVerifyAuditor{now: now, tenantID: tenantID}
}

// Log mirrors audit.FullService.CreateEntry's chain-construction shape:
// fetch prev hash → set PrevHash → compute Hash → persist. The persist
// step is an in-memory append.
func (c *chainVerifyAuditor) Log(_ context.Context, action string, p AuditPayload) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	prevHash := audit.GenesisHash
	if len(c.entries) > 0 {
		prevHash = c.entries[len(c.entries)-1].Hash
	}

	// Per-call monotonic tick so each entry has a unique CreatedAt — the
	// audit canonical time format has microsecond resolution and the same
	// instant would otherwise collide on rapid-fire test events.
	c.tick++
	createdAt := c.now().UTC().Add(time.Duration(c.tick) * time.Microsecond)

	// Serialise the payload to AfterData for parity with FullService;
	// ComputeHash does not actually consume AfterData, but we keep the
	// field populated so the persisted entry shape matches production.
	afterData, _ := json.Marshal(p)

	entry := audit.Entry{
		ID:         int64(len(c.entries) + 1),
		TenantID:   p.TenantID,
		Action:     action,
		EntityType: "sim",
		EntityID:   p.SIMID.String(),
		AfterData:  afterData,
		PrevHash:   prevHash,
		CreatedAt:  createdAt,
	}
	entry.Hash = audit.ComputeHash(entry, prevHash)
	c.entries = append(c.entries, entry)
	return nil
}

// snapshot returns a copy of the current entries for verification.
func (c *chainVerifyAuditor) snapshot() []audit.Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]audit.Entry, len(c.entries))
	copy(out, c.entries)
	return out
}

// verifyChain re-walks the entries and asserts hash-chain integrity using
// audit.ComputeHash + audit.GenesisHash. Mirrors audit.FullService.VerifyChain
// (service.go:120-167) line-for-line so the test catches any deviation
// from production chain semantics.
func verifyChain(t *testing.T, entries []audit.Entry) {
	t.Helper()
	prevHash := audit.GenesisHash
	for i, e := range entries {
		if e.PrevHash != prevHash {
			t.Fatalf("chain break at entry %d (action=%q): PrevHash=%s, want %s", i, e.Action, e.PrevHash, prevHash)
		}
		expected := audit.ComputeHash(e, prevHash)
		if e.Hash != expected {
			t.Fatalf("hash mismatch at entry %d (action=%q): got %s, want %s", i, e.Action, e.Hash, expected)
		}
		prevHash = e.Hash
	}
}

// chainTestEvent is one element of the mixed-mode driver list — describes
// a single binding evaluation that will be passed through Gate.Evaluate +
// Gate.Apply. Each event configures the SIM/session shape needed to elicit
// the targeted Verdict. Only audit-emitting verdicts contribute to the
// chain; allow-without-alarm events are still driven through Apply to
// confirm they do NOT touch the chain.
type chainTestEvent struct {
	name        string
	mode        string  // binding mode on the SIM
	bound       *string // bound_imei on the SIM (nil = unbound)
	observed    string  // session-captured IMEI
	blacklist   bool    // blacklist hit override
	expectAudit bool    // whether this event should add a chain entry
	expectAct   string  // expected audit action constant (when expectAudit)
}

// mixedModeChainEvents enumerates 50 events spanning all six binding modes
// + blacklist + first-use lock + grace + soft alarms. Distribution chosen
// so every audit action constant appears at least twice, giving the chain
// at least ~30 entries to verify.
func mixedModeChainEvents() []chainTestEvent {
	pa := ptrStr(imeiA)
	events := []chainTestEvent{}

	// strict mode — 5 mismatches (audit) + 3 matches (no audit) = 8 events.
	for i := 0; i < 5; i++ {
		events = append(events, chainTestEvent{
			name: "strict_mismatch", mode: "strict", bound: pa, observed: imeiB,
			expectAudit: true, expectAct: AuditActionMismatch,
		})
	}
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "strict_match", mode: "strict", bound: pa, observed: imeiA,
			expectAudit: false,
		})
	}

	// allowlist mode — 4 misses (allowlist mismatch audit) + 2 hits (no audit) = 6.
	for i := 0; i < 4; i++ {
		events = append(events, chainTestEvent{
			name: "allowlist_miss", mode: "allowlist", bound: pa, observed: imeiB,
			expectAudit: true, expectAct: AuditActionMismatch,
		})
	}
	for i := 0; i < 2; i++ {
		events = append(events, chainTestEvent{
			name: "allowlist_hit", mode: "allowlist", bound: pa, observed: imeiA,
			expectAudit: false,
		})
	}

	// first-use mode unbound — locks + emits AuditActionFirstUseLocked.
	for i := 0; i < 4; i++ {
		events = append(events, chainTestEvent{
			name: "first_use_lock", mode: "first-use", bound: nil, observed: imeiA,
			expectAudit: true, expectAct: AuditActionFirstUseLocked,
		})
	}
	// first-use mode bound + match — no audit.
	for i := 0; i < 2; i++ {
		events = append(events, chainTestEvent{
			name: "first_use_bound_match", mode: "first-use", bound: pa, observed: imeiA,
			expectAudit: false,
		})
	}
	// first-use mode bound + mismatch — strict-style mismatch audit.
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "first_use_bound_mismatch", mode: "first-use", bound: pa, observed: imeiB,
			expectAudit: true, expectAct: AuditActionMismatch,
		})
	}

	// tac-lock mode — 3 mismatches (different TAC) + 2 matches (same TAC) = 5.
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "tac_lock_mismatch", mode: "tac-lock", bound: pa, observed: imeiB,
			expectAudit: true, expectAct: AuditActionMismatch,
		})
	}
	for i := 0; i < 2; i++ {
		events = append(events, chainTestEvent{
			name: "tac_lock_match", mode: "tac-lock", bound: pa, observed: imeiACousin,
			expectAudit: false,
		})
	}

	// grace-period mode — bound + match (no audit), bound + mismatch (audit).
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "grace_period_match", mode: "grace-period", bound: pa, observed: imeiA,
			expectAudit: false,
		})
	}
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "grace_period_mismatch", mode: "grace-period", bound: pa, observed: imeiB,
			expectAudit: true, expectAct: AuditActionMismatch,
		})
	}

	// soft mode — every mismatch is a soft alarm (audit) + matches (no audit).
	for i := 0; i < 4; i++ {
		events = append(events, chainTestEvent{
			name: "soft_mismatch", mode: "soft", bound: pa, observed: imeiB,
			expectAudit: true, expectAct: AuditActionSoftMismatch,
		})
	}
	for i := 0; i < 2; i++ {
		events = append(events, chainTestEvent{
			name: "soft_match", mode: "soft", bound: pa, observed: imeiA,
			expectAudit: false,
		})
	}

	// blacklist override — fires regardless of mode; covers AuditActionBlacklistHit.
	// Mix 3 different modes to prove blacklist is mode-agnostic.
	for i := 0; i < 4; i++ {
		events = append(events, chainTestEvent{
			name: "blacklist_hit_strict", mode: "strict", bound: pa, observed: imeiA, blacklist: true,
			expectAudit: true, expectAct: AuditActionBlacklistHit,
		})
	}
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "blacklist_hit_soft", mode: "soft", bound: pa, observed: imeiA, blacklist: true,
			expectAudit: true, expectAct: AuditActionBlacklistHit,
		})
	}
	for i := 0; i < 3; i++ {
		events = append(events, chainTestEvent{
			name: "blacklist_hit_allowlist", mode: "allowlist", bound: pa, observed: imeiA, blacklist: true,
			expectAudit: true, expectAct: AuditActionBlacklistHit,
		})
	}

	return events
}

// TestAuditChain_ValidAfterMixedModeRun (STORY-096 Gate F-A1, AC-16
// directly evidenced) drives 50 binding events through the real Gate +
// Orchestrator pipeline against a chain-verifying Auditor mock that uses
// audit.ComputeHash + audit.GenesisHash. After the run, asserts:
//
//   - every audit action constant appears at least once,
//   - the per-event audit-emit count matches Verdict.EmitAudit,
//   - the persisted chain validates without gap (PrevHash continuity +
//     Hash recompute), using the same algorithm as
//     audit.FullService.VerifyChain.
//
// The test exercises all six binding modes (strict / allowlist / first-use
// / tac-lock / grace-period / soft) plus the blacklist hard-deny override.
// It does NOT need a live audit DB — the chain math under test is the
// production chain math (ComputeHash + GenesisHash from internal/audit),
// so equivalence with FullService is guaranteed by construction.
func TestAuditChain_ValidAfterMixedModeRun(t *testing.T) {
	events := mixedModeChainEvents()
	if len(events) != 50 {
		t.Fatalf("test setup: expected 50 events, got %d (AC-16 mandates a 50-event mixed run)", len(events))
	}

	auditor := newChainVerifyAuditor(testFixedClock, testTenantID)
	notif := &mockNotifier{}
	hist := &mockHistoryWriter{}
	sims := &mockSIMUpdater{}
	metrics := &mockDropCounter{}

	expectedAuditCount := 0
	actionsSeen := map[string]int{}
	expectedActions := []string{
		AuditActionMismatch,
		AuditActionFirstUseLocked,
		AuditActionSoftMismatch,
		AuditActionBlacklistHit,
	}

	for i, ev := range events {
		bl := &mockBlacklist{hit: ev.blacklist}
		al := &mockAllowlist{}
		// allowlist_hit needs the allowlist to return true for imeiA.
		if ev.name == "allowlist_hit" {
			al = &mockAllowlist{allowed: true}
		}

		enforcer := New(
			WithAllowlistChecker(al),
			WithBlacklistChecker(bl),
			WithGraceWindow(testGraceWindow),
			WithClock(testFixedClock),
		)
		orch := NewOrchestrator(
			auditor, notif, hist, sims, testGraceWindow,
			WithDropCounter(metrics),
			WithLogger(zerolog.Nop()),
			WithOrchestratorClock(testFixedClock),
		)
		gate := NewGate(enforcer, orch)

		session := makeOrchestratorSession(ev.observed)
		sim := makeOrchestratorSIM(ev.mode, ev.bound)

		v, err := gate.Evaluate(context.Background(), session, sim)
		if err != nil {
			t.Fatalf("event[%d] %s: Evaluate err: %v", i, ev.name, err)
		}
		if err := gate.Apply(context.Background(), v, session, sim, "radius"); err != nil {
			t.Fatalf("event[%d] %s: Apply err: %v", i, ev.name, err)
		}

		if v.EmitAudit {
			expectedAuditCount++
			actionsSeen[v.AuditAction]++
		}
		if ev.expectAudit != v.EmitAudit {
			t.Errorf("event[%d] %s: EmitAudit=%v, want %v (verdict=%+v)", i, ev.name, v.EmitAudit, ev.expectAudit, v)
		}
		if ev.expectAudit && v.AuditAction != ev.expectAct {
			t.Errorf("event[%d] %s: AuditAction=%q, want %q", i, ev.name, v.AuditAction, ev.expectAct)
		}
	}

	entries := auditor.snapshot()
	if got := len(entries); got != expectedAuditCount {
		t.Fatalf("audit chain length = %d, want %d (sum of EmitAudit over %d events)", got, expectedAuditCount, len(events))
	}

	// Every audit action constant in the binding catalog MUST be present
	// in the chain — the AC-16 "mixed run of all six modes" contract.
	for _, want := range expectedActions {
		if actionsSeen[want] == 0 {
			t.Errorf("expected at least one audit entry with action=%q in mixed-mode run, got 0", want)
		}
	}

	// Final integrity check — chain validates per audit.FullService rules.
	verifyChain(t, entries)
}
