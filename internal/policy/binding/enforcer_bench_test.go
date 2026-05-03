package binding

// enforcer_bench_test.go — STORY-096 Task 8 Part C (AC-13).
//
// Microbenchmarks for the binding Enforcer hot path. The 1M-SIM live
// rig is out-of-CI per D-184/D-192; these benchmarks substitute a
// per-call ns/op measurement against a fixed mock surface so the
// AC-13 ≤5% latency budget claim has empirical grounding.
//
// Benchmark layout (per dispatch §Part C):
//   1. NullMode         — binding_mode=NULL short-circuit, blacklist nil.
//                         Hot-path baseline. Target < 100ns/op, 0 allocs.
//   2. StrictMatch      — strict + bound==observed (Allow path).
//                         One mock blacklist call. Target < 1µs/op.
//   3. StrictMismatch   — strict + bound!=observed (Reject path).
//                         Same shape as StrictMatch; verifies the reject
//                         branch isn't accidentally heavier.
//   4. Allowlist        — allowlist hit (one allowlist mock call).
//   5. OrchestratorApplyReject — full sink fan-out (sync audit + queued
//                                history; notifier stubbed). Captures
//                                per-call cost of the side-effect chain.
//
// Sink variables prevent the compiler from optimizing away the calls
// (PAT: same shape as imei_bench_test.go in radius pkg).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ----- Bench sinks -----------------------------------------------------------

var (
	benchVerdictSink Verdict
	benchErrSink     error
)

// ----- Bench: NULL-mode short-circuit ---------------------------------------

// benchSilentBlacklist is a minimal BlacklistChecker that always returns
// (false, nil) without recording calls. Used for the StrictMatch /
// StrictMismatch benches where the AC-9 crosscut consults the blacklist
// once per Evaluate but the test does NOT assert call counts.
type benchSilentBlacklist struct{}

func (benchSilentBlacklist) IsInBlacklist(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return false, nil
}

// benchSilentAllowlist mirrors benchSilentBlacklist for allowlist mode.
// `allowed` configures the verdict-relevant arm.
type benchSilentAllowlist struct {
	allowed bool
}

func (a benchSilentAllowlist) IsAllowed(_ context.Context, _, _ uuid.UUID, _ string) (bool, error) {
	return a.allowed, nil
}

// BenchmarkEnforcer_Evaluate_NullMode measures the AC-2 hot-path
// short-circuit. The blacklist is intentionally NIL-wired (per
// enforcer.go:101-104 + V6 / NULL_nil_blacklist_zero_calls test) so
// the 0-alloc / sub-µs baseline holds. Production runs with blacklist
// wired pay one SELECT EXISTS even on NULL-mode SIMs (acceptable per
// AC-9), but THIS benchmark captures the pure short-circuit cost.
func BenchmarkEnforcer_Evaluate_NullMode(b *testing.B) {
	e := New() // no checkers, no clock — pure short-circuit
	sim := makeSIM("", nil, nil)
	session := makeSession(imeiA)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchVerdictSink, benchErrSink = e.Evaluate(ctx, session, sim)
	}
}

// BenchmarkEnforcer_Evaluate_StrictMatch measures the strict-mode
// Allow branch. One mock blacklist call (returns false), no allocs
// expected beyond the Verdict struct.
func BenchmarkEnforcer_Evaluate_StrictMatch(b *testing.B) {
	e := New(WithBlacklistChecker(benchSilentBlacklist{}))
	bound := imeiA
	sim := SIMView{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		BindingMode: ptrStr("strict"),
		BoundIMEI:   &bound,
	}
	session := SessionContext{
		TenantID: sim.TenantID,
		SIMID:    sim.ID,
		IMEI:     imeiA,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchVerdictSink, benchErrSink = e.Evaluate(ctx, session, sim)
	}
}

// BenchmarkEnforcer_Evaluate_StrictMismatch measures the strict-mode
// Reject branch — same shape as match; verifies the heavier Verdict
// struct (Reject carries audit + notif fields) does not regress.
func BenchmarkEnforcer_Evaluate_StrictMismatch(b *testing.B) {
	e := New(WithBlacklistChecker(benchSilentBlacklist{}))
	bound := imeiA
	sim := SIMView{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		BindingMode: ptrStr("strict"),
		BoundIMEI:   &bound,
	}
	session := SessionContext{
		TenantID: sim.TenantID,
		SIMID:    sim.ID,
		IMEI:     imeiB, // mismatch
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchVerdictSink, benchErrSink = e.Evaluate(ctx, session, sim)
	}
}

// BenchmarkEnforcer_Evaluate_AllowlistHit measures the allowlist Allow
// branch (one allowlist call + one blacklist call).
func BenchmarkEnforcer_Evaluate_AllowlistHit(b *testing.B) {
	e := New(
		WithAllowlistChecker(benchSilentAllowlist{allowed: true}),
		WithBlacklistChecker(benchSilentBlacklist{}),
	)
	sim := SIMView{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		BindingMode: ptrStr("allowlist"),
	}
	session := SessionContext{
		TenantID: sim.TenantID,
		SIMID:    sim.ID,
		IMEI:     imeiA,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchVerdictSink, benchErrSink = e.Evaluate(ctx, session, sim)
	}
}

// ----- Bench: Orchestrator full sink chain ----------------------------------

// benchSilentNotifier discards published notifications without
// allocating (the orchestrator dispatches in a goroutine; the
// benchmark measures the synchronous cost of Apply, not the goroutine
// itself).
type benchSilentNotifier struct{}

func (benchSilentNotifier) Publish(_ context.Context, _ string, _ NotificationPayload) error {
	return nil
}

// benchDiscardHistoryWriter is a no-op HistoryWriter that does not
// queue / allocate. Captures the per-call cost of building the
// HistoryEntry struct without measuring the buffered-channel send.
type benchDiscardHistoryWriter struct{}

func (benchDiscardHistoryWriter) Append(_ context.Context, _ HistoryEntry) {}

// benchSilentAuditor records the call but does not allocate.
type benchSilentAuditor struct{}

func (benchSilentAuditor) Log(_ context.Context, _ string, _ AuditPayload) error { return nil }

// BenchmarkOrchestrator_Apply_Reject measures the per-call cost of
// the full side-effect chain (audit sync + notification goroutine +
// history append) for a Reject verdict — the heaviest of the three
// kinds.
func BenchmarkOrchestrator_Apply_Reject(b *testing.B) {
	o := NewOrchestrator(
		benchSilentAuditor{},
		benchSilentNotifier{},
		benchDiscardHistoryWriter{},
		nil, // no SIM update on plain Reject
		testGraceWindow,
		WithLogger(zerolog.Nop()),
		WithOrchestratorClock(testFixedClock),
	)
	v := Verdict{
		Kind:               VerdictReject,
		Reason:             RejectReasonMismatchStrict,
		Severity:           SeverityHigh,
		BindingStatus:      BindingStatusMismatch,
		EmitAudit:          true,
		AuditAction:        AuditActionMismatch,
		EmitNotification:   true,
		NotifSubject:       NotifSubjectBindingFailed,
		HistoryWasMismatch: true,
		HistoryAlarmRaised: true,
	}
	session := makeOrchestratorSession(imeiB)
	bound := imeiA
	sim := SIMView{
		ID:          testSIMID,
		TenantID:    testTenantID,
		BindingMode: ptrStr("strict"),
		BoundIMEI:   &bound,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchErrSink = o.Apply(ctx, v, session, sim, "radius")
	}
}
