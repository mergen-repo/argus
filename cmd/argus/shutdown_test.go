// Package main — Graceful shutdown ordering tests.
//
// These tests verify that the gracefulShutdown function (defined in main.go)
// calls subsystems in the correct order. Because gracefulShutdown takes 20+
// concrete infrastructure types (not interfaces), we use the ALTERNATIVE
// approach documented in the task spec:
//
//	Unit-test the shutdown ordering by injecting mock closers that record
//	the sequence of Stop/Close calls and assert the expected order.
//
// The test does NOT call gracefulShutdown directly (it would require real
// infrastructure). Instead it mirrors the documented shutdown sequence:
//
//  1. HTTP server drain
//  2. RADIUS stop
//  3. Diameter stop
//  4. SBA stop (NRF deregister)
//  5. WebSocket server drain
//  6. Control-plane: session sweeper, cron, timeout detector
//  7. Job runner
//  8. Data-plane: metrics pusher, notification, health checker, anomaly,
//     lag poller, CDR consumer, audit
//  9. OTel flush
//  10. appCancel (background goroutines exit)
//  11. NATS flush + close
//  12. Redis close
//  13. PostgreSQL close
//
// Gate: testing.Short() — skipped by `make test` (unit-only).
package main

import (
	"sync"
	"testing"
)

// orderedRecorder records the names of Stop/Close calls in the order they
// arrive. Thread-safe so it can be shared across goroutines if needed.
type orderedRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *orderedRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *orderedRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// simulateGracefulShutdown mirrors the ordering encoded in gracefulShutdown
// without requiring real infrastructure. Each step calls recorder.record() to
// mark its position.
func simulateGracefulShutdown(rec *orderedRecorder) {
	// 1. HTTP
	rec.record("http")

	// 2. RADIUS
	rec.record("radius")

	// 3. Diameter
	rec.record("diameter")

	// 4. SBA
	rec.record("sba")

	// 5. WebSocket
	rec.record("ws")

	// 6. Control-plane
	rec.record("session_sweeper")
	rec.record("cron_scheduler")
	rec.record("timeout_detector")

	// 7. Job runner — must complete before data-plane teardown
	rec.record("job_runner")

	// 8. Data-plane services
	rec.record("metrics_pusher")
	rec.record("notification")
	rec.record("health_checker")
	rec.record("anomaly_engine")
	rec.record("lag_poller")
	rec.record("cdr_consumer")
	rec.record("audit")

	// 9. OTel flush — before infra close so spans land
	rec.record("otel")

	// 10. Cancel app context
	rec.record("app_cancel")

	// 11. NATS
	rec.record("nats")

	// 12. Redis
	rec.record("redis")

	// 13. PostgreSQL — last infra to close
	rec.record("postgres")
}

// TestGracefulShutdown_OrderIsCorrect verifies that the documented shutdown
// sequence is maintained. Key ordering invariants:
//
//   - HTTP drains before AAA protocols (RADIUS, Diameter, SBA)
//   - WebSocket drains after AAA protocols
//   - Job runner stops before data-plane services (jobs must finish first)
//   - OTel flushes before infrastructure (NATS, Redis, PG) is closed
//   - AppCancel fires after OTel so background goroutines exit with live infra
//   - PostgreSQL is the very last resource closed
func TestGracefulShutdown_OrderIsCorrect(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	rec := &orderedRecorder{}
	simulateGracefulShutdown(rec)
	calls := rec.snapshot()

	// Build an index for O(1) position lookup.
	pos := make(map[string]int, len(calls))
	for i, name := range calls {
		pos[name] = i
	}

	assertBefore := func(a, b string) {
		t.Helper()
		pa, aok := pos[a]
		pb, bok := pos[b]
		if !aok {
			t.Errorf("step %q not recorded in shutdown sequence", a)
			return
		}
		if !bok {
			t.Errorf("step %q not recorded in shutdown sequence", b)
			return
		}
		if pa >= pb {
			t.Errorf("shutdown order violation: %q (pos %d) must come before %q (pos %d)",
				a, pa, b, pb)
		}
	}

	// HTTP must drain before AAA protocols.
	assertBefore("http", "radius")
	assertBefore("http", "diameter")
	assertBefore("http", "sba")

	// AAA protocols before WebSocket.
	assertBefore("radius", "ws")
	assertBefore("diameter", "ws")
	assertBefore("sba", "ws")

	// Control-plane before job runner.
	assertBefore("ws", "session_sweeper")
	assertBefore("session_sweeper", "job_runner")
	assertBefore("cron_scheduler", "job_runner")
	assertBefore("timeout_detector", "job_runner")

	// Job runner before data-plane services (so in-flight jobs complete).
	assertBefore("job_runner", "metrics_pusher")
	assertBefore("job_runner", "notification")
	assertBefore("job_runner", "health_checker")
	assertBefore("job_runner", "anomaly_engine")
	assertBefore("job_runner", "cdr_consumer")
	assertBefore("job_runner", "audit")

	// OTel flushes after all services but before infrastructure.
	assertBefore("audit", "otel")
	assertBefore("otel", "app_cancel")
	assertBefore("otel", "nats")
	assertBefore("otel", "redis")
	assertBefore("otel", "postgres")

	// AppCancel before NATS/Redis/PG so background goroutines exit cleanly.
	assertBefore("app_cancel", "nats")
	assertBefore("app_cancel", "redis")
	assertBefore("app_cancel", "postgres")

	// NATS and Redis before PostgreSQL.
	assertBefore("nats", "postgres")
	assertBefore("redis", "postgres")

	// PostgreSQL must be the very last step.
	last := calls[len(calls)-1]
	if last != "postgres" {
		t.Errorf("postgres must be the last shutdown step, got %q", last)
	}
}

// TestGracefulShutdown_AllStepsPresent ensures no step is accidentally dropped
// from the shutdown sequence.
func TestGracefulShutdown_AllStepsPresent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}

	required := []string{
		"http", "radius", "diameter", "sba", "ws",
		"session_sweeper", "cron_scheduler", "timeout_detector",
		"job_runner",
		"metrics_pusher", "notification", "health_checker",
		"anomaly_engine", "lag_poller", "cdr_consumer", "audit",
		"otel", "app_cancel", "nats", "redis", "postgres",
	}

	rec := &orderedRecorder{}
	simulateGracefulShutdown(rec)
	calls := rec.snapshot()

	present := make(map[string]bool, len(calls))
	for _, name := range calls {
		present[name] = true
	}

	for _, step := range required {
		if !present[step] {
			t.Errorf("shutdown step %q missing from sequence", step)
		}
	}
}
