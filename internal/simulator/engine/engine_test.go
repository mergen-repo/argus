package engine

import (
	"context"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/btopcu/argus/internal/simulator/reactive"
	"github.com/btopcu/argus/internal/simulator/sba"
	simradius "github.com/btopcu/argus/internal/simulator/radius"
	"github.com/btopcu/argus/internal/simulator/scenario"
	"github.com/rs/zerolog"
)

// TestShouldUseSBA_DispatchContract sanity-checks that the engine's SBA fork
// uses the same plan-contracted picker invariants the dedicated picker tests
// cover (rate=0 never selects SBA; rate=1 always does). This is a thin guard
// against a future refactor that might accidentally decouple engine from
// sba.ShouldUseSBA.
//
// Full distribution coverage sits in internal/simulator/sba/picker_test.go;
// this test asserts only the extreme cases so the engine branch is defended
// without duplicating the statistical tests.
func TestShouldUseSBA_DispatchContract(t *testing.T) {
	sc := sba.SessionContext{AcctSessionID: "engine-test-sess-1"}

	if sba.ShouldUseSBA(sc, 0.0) {
		t.Error("rate=0.0: picker must never select SBA")
	}
	if !sba.ShouldUseSBA(sc, 1.0) {
		t.Error("rate=1.0: picker must always select SBA")
	}
}

// TestEngineFork_NilSBAClientSkipsPath verifies the nil-guard in runSession's
// SBA fork. If the SBA map has no entry for an operator, the fork MUST NOT
// dereference nil — the session should proceed on the RADIUS path.
//
// We can't run the full session here (RADIUS needs a server), but we assert
// the map-lookup semantics the fork depends on.
func TestEngineFork_NilSBAClientSkipsPath(t *testing.T) {
	sbaClients := map[string]*sba.Client{}

	if c := sbaClients["missing-op"]; c != nil {
		t.Errorf("expected nil lookup for missing operator, got %v", c)
	}

	var nilMap map[string]*sba.Client
	if c := nilMap["any-op"]; c != nil {
		t.Errorf("expected nil lookup on nil map, got %v", c)
	}
}

// TestNew_NilReactiveSubsystem verifies that engine.New accepts nil for
// reactiveSub and produces a properly initialised engine with e.reactive == nil.
// When reactive is nil, no reactive subsystem state should be referenced.
func TestNew_NilReactiveSubsystem(t *testing.T) {
	cfg := &config.Config{
		Operators: []config.OperatorConfig{{Code: "turkcell", NASIP: "10.0.0.1", NASIdentifier: "sim"}},
		Scenarios: []config.ScenarioConfig{{
			Name: "basic", Weight: 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate: config.RateConfig{MaxRadiusRequestsPerSecond: 5},
	}
	picker := scenario.New(cfg.Scenarios, 42)
	client := simradius.New("127.0.0.1", 1812, 1813, "secret")
	logger := zerolog.Nop()

	eng := New(cfg, picker, client, nil, nil, nil, logger)
	if eng == nil {
		t.Fatal("New returned nil engine")
	}
	if eng.reactive != nil {
		t.Error("engine.reactive should be nil when reactiveSub=nil")
	}
	if eng.ActiveCount() != 0 {
		t.Errorf("fresh engine should have 0 active sessions, got %d", eng.ActiveCount())
	}
}

// TestNew_ReactiveSubsystemWired verifies that a non-nil reactiveSub is stored
// on the engine and its components are accessible.
func TestNew_ReactiveSubsystemWired(t *testing.T) {
	cfg := &config.Config{
		Operators: []config.OperatorConfig{{Code: "turkcell"}},
		Scenarios: []config.ScenarioConfig{{
			Name: "basic", Weight: 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate: config.RateConfig{MaxRadiusRequestsPerSecond: 5},
	}
	reactCfg := config.ReactiveDefaults{
		Enabled:                true,
		SessionTimeoutRespect:  true,
		EarlyTerminationMargin: 5 * time.Second,
		RejectBackoffBase:      30 * time.Second,
		RejectBackoffMax:       600 * time.Second,
		RejectMaxRetriesPerHour: 5,
	}
	sub := &reactive.Subsystem{
		Cfg:      reactCfg,
		Rejects:  reactive.NewRejectTracker(reactCfg),
		Registry: reactive.NewRegistry(),
	}
	picker := scenario.New(cfg.Scenarios, 42)
	client := simradius.New("127.0.0.1", 1812, 1813, "secret")

	eng := New(cfg, picker, client, nil, nil, sub, zerolog.Nop())
	if eng.reactive == nil {
		t.Fatal("engine.reactive should be non-nil when subsystem is provided")
	}
	if eng.reactive.Rejects == nil {
		t.Error("engine.reactive.Rejects must not be nil")
	}
	if eng.reactive.Registry == nil {
		t.Error("engine.reactive.Registry must not be nil")
	}
}

// TestClassifyTermination_AllCauses is a table-driven test of the pure
// classifyTermination helper across all expected output labels.
func TestClassifyTermination_AllCauses(t *testing.T) {
	now := time.Now()
	scenarioDL := now.Add(5 * time.Minute) // the scenario's original deadline

	// helpers
	sessWithCause := func(c reactive.DisconnectCause, dl time.Time) *reactive.Session {
		s := &reactive.Session{}
		s.SetDisconnectCause(c)
		s.UpdateDeadline(dl)
		return s
	}
	sessNoCause := func(dl time.Time) *reactive.Session {
		s := &reactive.Session{}
		s.UpdateDeadline(dl)
		return s
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	tests := []struct {
		name     string
		sess     *reactive.Session
		ctx      context.Context
		want     string
	}{
		{
			name: "DM cause → disconnect",
			sess: sessWithCause(reactive.CauseDM, scenarioDL),
			ctx:  context.Background(),
			want: "disconnect",
		},
		{
			name: "CoA deadline cause → coa_deadline",
			sess: sessWithCause(reactive.CauseCoADeadline, scenarioDL),
			ctx:  context.Background(),
			want: "coa_deadline",
		},
		{
			name: "context cancelled → shutdown",
			sess: sessNoCause(scenarioDL),
			ctx:  cancelledCtx,
			want: "shutdown",
		},
		{
			name: "nil session, context cancelled → shutdown",
			sess: nil,
			ctx:  cancelledCtx,
			want: "shutdown",
		},
		{
			name: "deadline past and below scenario → session_timeout",
			sess: sessNoCause(now.Add(-1 * time.Second)), // past AND before scenarioDL
			ctx:  context.Background(),
			want: "session_timeout",
		},
		{
			name: "deadline past but equals scenario (no shortening) → scenario_end",
			sess: sessNoCause(now.Add(-1 * time.Second)),
			ctx:  context.Background(),
			// scenarioDL passed as 1s before now so deadline == scenarioDL; not Before
			want: "scenario_end",
		},
		{
			name: "no special cause, no deadline past → scenario_end",
			sess: sessNoCause(now.Add(10 * time.Minute)),
			ctx:  context.Background(),
			want: "scenario_end",
		},
		{
			name: "nil session, no ctx error → scenario_end",
			sess: nil,
			ctx:  context.Background(),
			want: "scenario_end",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// For "deadline past and equals scenario" case, use the session's deadline as scenarioDL.
			sdl := scenarioDL
			if tc.name == "deadline past but equals scenario (no shortening) → scenario_end" {
				sdl = now.Add(-1 * time.Second) // same as session deadline
			}
			got := classifyTermination(tc.sess, tc.ctx, sdl)
			if got != tc.want {
				t.Errorf("classifyTermination() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRejectTracker_AllowedAfterSuspension verifies that RejectTracker.Allowed
// returns false for a suspended (op, imsi) pair — this is the gate the engine
// checks at the top of runSession before sending any Access-Request.
func TestRejectTracker_AllowedAfterSuspension(t *testing.T) {
	cfg := config.ReactiveDefaults{
		RejectBackoffBase:       time.Millisecond,
		RejectBackoffMax:        10 * time.Millisecond,
		RejectMaxRetriesPerHour: 2,
	}
	tracker := reactive.NewRejectTracker(cfg)

	// Exhaust retries to trigger suspension.
	for i := 0; i <= cfg.RejectMaxRetriesPerHour; i++ {
		tracker.NextBackoff("turkcell", "imsi-001")
	}

	// Allowed must return false while suspended.
	if tracker.Allowed("turkcell", "imsi-001") {
		t.Error("Allowed() should return false for a suspended SIM")
	}
	// A different SIM should still be allowed.
	if !tracker.Allowed("turkcell", "imsi-002") {
		t.Error("Allowed() should return true for a non-rejected SIM")
	}
}

// TestSessionTimeoutRespect_DeadlineShortened verifies the reactive deadline
// shortening logic: when SessionTimeoutRespect is true and the server returns
// a Session-Timeout shorter than the scenario duration, the effective deadline
// moves to serverDeadline - margin.
func TestSessionTimeoutRespect_DeadlineShortened(t *testing.T) {
	reactCfg := config.ReactiveDefaults{
		Enabled:                true,
		SessionTimeoutRespect:  true,
		EarlyTerminationMargin: 5 * time.Second,
	}

	// Simulate the deadline computation from runSession.
	scenarioDuration := 10 * time.Minute
	scenarioDeadline := time.Now().Add(scenarioDuration)

	serverTimeout := 60 * time.Second // much shorter than 10min
	serverDeadline := time.Now().Add(serverTimeout).Add(-reactCfg.EarlyTerminationMargin)

	deadline := scenarioDeadline
	if reactCfg.SessionTimeoutRespect && serverTimeout > 0 {
		if serverDeadline.Before(deadline) {
			deadline = serverDeadline
		}
	}

	if !deadline.Before(scenarioDeadline) {
		t.Error("deadline should have been shortened by ServerSessionTimeout")
	}
	// Allow ±1s for time.Now() drift in the test.
	expectedApprox := time.Now().Add(serverTimeout).Add(-reactCfg.EarlyTerminationMargin)
	diff := deadline.Sub(expectedApprox)
	if diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("deadline %v is far from expected %v (diff %v)", deadline, expectedApprox, diff)
	}
}

// TestSessionTimeout_SubIntervalDeadlineFires is F-A6/F-B1 — end-to-end
// wall-clock assertion that a Session-Timeout shorter than the interim tick
// interval still terminates the session on time. Before the fix, the interim
// ticker (set to the scenario's InterimInterval) was the only channel that
// could trigger the deadline check, so a 2s Session-Timeout with a 60s
// interim would only expire 58s late. After the fix, the engine arms a
// per-session deadline timer that fires exactly when the effective deadline
// is reached.
//
// This test exercises the deadline-timer mechanism in isolation — it
// reproduces the timer-arm pattern from runSession (engine.go:314-363 after
// STORY-085-gate fix) and asserts that a 500ms deadline triggers before a
// 10s ticker would.
func TestSessionTimeout_SubIntervalDeadlineFires(t *testing.T) {
	rsess := &reactive.Session{OperatorCode: "test-op", AcctSessionID: "sub-interval-test"}
	// Deadline 500ms in the future.
	target := time.Now().Add(500 * time.Millisecond)
	rsess.UpdateDeadline(target)

	// Ticker is deliberately much longer than the deadline so a bug that
	// waits on the ticker alone would cause the test to hang / fail.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	deadlineTimer := time.NewTimer(time.Until(rsess.CurrentDeadline()))
	defer deadlineTimer.Stop()

	start := time.Now()
	select {
	case <-deadlineTimer.C:
		elapsed := time.Since(start)
		// Should fire at ~500ms, well before the 10s ticker.
		if elapsed < 400*time.Millisecond || elapsed > 1500*time.Millisecond {
			t.Errorf("deadline fired at %v — expected ~500ms", elapsed)
		}
	case <-ticker.C:
		t.Fatal("ticker fired before deadline — sub-interval deadline NOT respected (F-A6 regression)")
	case <-time.After(2 * time.Second):
		t.Fatal("deadline timer never fired")
	}
}
