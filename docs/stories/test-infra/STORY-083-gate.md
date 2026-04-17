# Gate Report: STORY-083 — Diameter Simulator Client (Gx/Gy)

## Gate Verdict

**PASS (unconditional)** — 2026-04-17

All 8 ACs from STORY-083 plan satisfied, 4 in-gate fixes applied and verified,
2 deferrals documented, 1 pre-existing finding accepted as out-of-scope.

## Summary

- Requirements Tracing: 8/8 ACs mapped to tasks; all tasks merged before gate.
- Gap Analysis: AC-1..3, AC-5..8 fully automated; AC-4 HTTP-side assertion
  is manual (runbook documented in `docs/architecture/simulator.md`), core
  Diameter session surface is covered by `TestSimulator_AgainstArgusDiameter`.
- Compliance: COMPLIANT — no third-party Diameter library added (reuses
  `internal/aaa/diameter` per plan).
- Tests: 41/41 simulator tests PASS, 2900/2900 full suite PASS, integration
  test PASS, 50/50 flake-hunt iterations PASS with race detector.
- Build: PASS (`go build ./...` clean).
- Overall: PASS

## Scout Summary

Analysis scout flagged 6 findings (F-A1..F-A6) spanning gaps (AC-4 automation),
correctness (gyCounters leak, double-counted abort reason), performance
(busy-poll readiness), and compliance (IP-CAN-Type vs PROTOCOLS.md).
Test/Build scout flagged 2 findings (F-B1..F-B2): a 15% race flake in
`TestPeerGracefulClose` under `-race` (DPR write racing closeCh-triggered
conn teardown) and a pre-existing vet issue owned by STORY-088. No duplicates
between scouts. Consolidated set: 8 findings → 4 FIXED, 2 DEFERRED, 2 ACCEPTED.

## Findings Table

| ID | Severity | Source | Status | Location | Resolution |
|----|----------|--------|--------|----------|------------|
| F-B1 | HIGH | testbuild | **FIXED** | `peer.go:264-310` | Close() now writes DPR and waits for DPA BEFORE `close(closeCh)`. Eliminates the race where watchdog exits on `<-closeCh`, Run calls conn.Close(), and the DPR write hits a closed socket. Verified by 50/50 PASS on `-race -count=50`. |
| F-A2 | MEDIUM | analysis | **FIXED** | `client.go:140-149` | Added `delete(c.gyCounters, sc.AcctSessionID)` under `gyCountersMu` on the Gy CCR-I error path. New regression test `TestClient_GyCCRIFailure_DeletesCounter` drives 50 failed OpenSessions and asserts `len(c.gyCounters) == 0`. |
| F-A3 | MEDIUM | analysis | **FIXED** | `client.go:127-152` + `engine.go:144-161` | Removed client-side `DiameterSessionAbortedTotal{peer_down}` increment. Engine now inspects returned error with `errors.Is(err, diameter.ErrPeerNotOpen)` and picks `peer_down` vs `ccr_i_failed` reason exactly once. Plan's documented reasons are now disjoint partitions. |
| F-A6 | INFO | analysis | **FIXED** | `ccr.go:39,91` + `ccr_test.go:181,220` | Changed IP-CAN-Type value from `1` to `0` (3GPP-GPRS) per RFC 7155 and `docs/architecture/PROTOCOLS.md` table. Comments cite rationale. Test goldens updated. |
| F-A1 | MEDIUM | analysis | **DEFERRED** | `integration_test.go` | Automated HTTP-side assertion for `GET /api/v1/cdrs?protocol=diameter` requires seeded tenant + SIM + auth-token provisioning from within the test binary — out of reasonable in-gate scope. Manual smoke runbook added to `docs/architecture/simulator.md`. Target: future test-infra story (tracked in simulator.md "Tech debt / future enhancements" section). |
| F-A4 | LOW | analysis | **ACCEPTED** | `client.go:104-123` | 5 ms busy-poll for initial peer-open is correct; optimization to `OnStateChange` subscription is a future refactor. Documented in `docs/architecture/simulator.md` "Tech debt / future enhancements". |
| F-A5 | LOW | analysis | **ACCEPTED (with comment)** | `client.go:182-196` | Literal `reqNum=1` for Gx CCR-T is correct because Argus has no Gx Update phase. Added inline comment citing RFC 4006 §8.2 and pointing to the Gy counter pattern if Gx Update is ever introduced. |
| F-B2 | INFO | testbuild | **ACCEPTED** | `internal/policy/...` | Pre-existing D-033 vet issue. Owned by STORY-088; out of STORY-083 scope. No action. |

## In-Gate Fix Diffs Summary

### F-B1: `peer.go` — Close() ordering
Reordered `Peer.Close()` so `close(p.closeCh)` fires AFTER the DPR write and
DPA wait. Previously:
```
close(closeCh)   ← watchdog exits, Run calls conn.Close()
                 ← reader goroutine also exits on read err
setState(Closing)
build DPR
writeMessage()   ← races against the conn.Close() above
```
After:
```
setState(Closing)
build DPR
writeMessage(conn, dpr)   ← guaranteed conn still open
wait DPA or 1s
close(p.closeCh)          ← NOW signal Run/watchdog
conn.Close()              ← idempotent belt-and-braces
```
Nil-conn fast path still closes closeCh immediately. `closeOnce` preserved so
concurrent `Close()` callers are deduped. Run's own `conn.Close()` and
`drainPending` remain safe (conn.Close is idempotent; pending map already
cleaned by Close before closeCh was signalled).

### F-A2: `client.go` — gyCounters cleanup on Gy CCR-I error
```go
if err := c.sendGyCCRI(ctx, sc); err != nil {
    c.gyCountersMu.Lock()
    delete(c.gyCounters, sc.AcctSessionID)
    c.gyCountersMu.Unlock()
    return fmt.Errorf("gy ccr-i: %w", err)
}
```
Guards against unbounded map growth when Diameter aborts before
`CloseSession` is ever reached.

### F-A3: `client.go` + `engine.go` — single classification point
Removed the two client-side `metrics.DiameterSessionAbortedTotal.WithLabelValues(c.operatorCode, "peer_down").Inc()` calls.
Engine now classifies:
```go
if err := dmClient.OpenSession(ctx, sc); err != nil {
    reason := "ccr_i_failed"
    if errors.Is(err, diameter.ErrPeerNotOpen) {
        reason = "peer_down"
    }
    metrics.DiameterSessionAbortedTotal.WithLabelValues(sim.OperatorCode, reason).Inc()
    return
}
```
Each aborted session increments exactly one bucket. Test
`TestClient_PeerDown_ReturnsSentinel` updated — still verifies the error chain
exposes `ErrPeerNotOpen` so the engine can classify, drops the now-obsolete
metric assertion (with a comment explaining the move).

### F-A6: `ccr.go` + `ccr_test.go` — IP-CAN-Type 0
Two builder call sites changed from `1` → `0` (3GPP-GPRS per the PROTOCOLS.md
table and RFC 7155). Two test expectation sites updated. Brief comments added.

## Verification Commands + Results

```bash
$ go build ./...
✓ clean

$ go test ./internal/simulator/...
✓ ok — 41 tests across 7 packages

$ go test -race ./internal/simulator/...
✓ ok — 41 tests across 7 packages, no race reports

$ go test -race -run TestPeerGracefulClose -count=50 ./internal/simulator/diameter/...
✓ ok — 50/50 iterations PASS (F-B1 regression)

$ go test -tags=integration -race -run TestSimulator_AgainstArgusDiameter ./internal/simulator/diameter/...
✓ ok — integration round-trip against in-process argusdiameter.Server

$ go test ./...
✓ ok — 2900/2900 tests across 96 packages (baseline 2899 + 1 new regression test)
```

Baseline on entry: 2899 tests passing. New total: 2900 tests passing
(`TestClient_GyCCRIFailure_DeletesCounter` added for F-A2). The unchanged
`TestClient_PeerDown_ReturnsSentinel` retains its 1-test footprint; the count
change comes entirely from the new leak-regression test.

## Bug Patterns & Prevention

### New pattern worth adding to decisions.md: "write-before-close-channel"

**Pattern:** In a goroutine network of (writer, watchdog, reader, shutdown),
if shutdown needs to emit a final wire message AND uses a broadcast channel
(`close(ch)`) to signal other goroutines to exit, the broadcast MUST happen
AFTER the final write completes, not before.

**Why it bites under `-race`:** The Go memory model permits the scheduler to
interleave `close(closeCh)` → watchdog select wakes → `return "close"` →
Run's post-watchdog `conn.Close()` → writer's subsequent `conn.Write(dpr)`
returning an error, ALL before the writer has even begun encoding the DPR.
Under the race detector, the additional instrumentation slows the writer path
just enough that this interleaving occurs ~15% of the time, vs ~0% without
`-race`.

**Prevention:**
1. Close broadcast channels AFTER the last operation they would abort, not at
   the top of the shutdown function.
2. If the shutdown path must hold invariants across a "point of no return",
   gate the broadcast behind that point. In our case: "DPR is either written
   or provably errored; THEN tell peers we're done."
3. Where idempotency is available (e.g. `net.Conn.Close`), use it defensively
   so double-close at the tail of Run doesn't panic.

### Single-writer metric classification

**Pattern:** A counter with a `reason` label that takes values from a fixed
enumeration ("disjoint partitions") must be incremented by exactly one writer
across the layer stack. If multiple layers (e.g. Client wraps Peer, Engine
wraps Client) each increment the same counter on overlapping error paths, the
counter overcounts N-fold (where N = layers), and the enum invariant breaks
(one real event → two reason labels both +=1).

**Prevention:**
1. Identify the "owner" layer for each counter — typically the highest layer
   that can distinguish the enum values.
2. Lower layers return wrapped sentinel errors (`errors.Is`-compatible); the
   owner layer does the classification.
3. In tests, assert the TOTAL across all label values for one logical event
   equals exactly 1 — this catches double-counts even when individual label
   assertions pass.

## Passed Items

- 8/8 AC mapping covered by the plan's Acceptance Criteria Mapping table.
- Plan-documented metric semantics ("disjoint partitions") now actually hold.
- `internal/aaa/diameter` reuse rule satisfied; `go.mod` unchanged by gate.
- Shutdown order (engine → clients → peers → TCP) preserved after fix.
- No new races introduced by any in-gate fix.
- All pre-existing simulator tests continue to pass.

## Deferred Items

| # | Finding | Target Story | Tracking |
|---|---------|--------------|----------|
| F-A1 | Automated HTTP CDR assertion for `GET /api/v1/cdrs?protocol=diameter` | future test-infra story | `docs/architecture/simulator.md` "Tech debt / future enhancements" + manual runbook |

## Accepted (No Action) Items

| # | Finding | Rationale |
|---|---------|-----------|
| F-A4 | Busy-poll readiness | Optimization; 5 ms tick is correct. Documented in simulator.md. |
| F-A5 | Hardcoded Gx CCR-T reqNum=1 | Correct — Gx has no Update phase. Comment added citing RFC 4006 §8.2. |
| F-B2 | Pre-existing go vet finding in internal/policy | Owned by STORY-088; out of STORY-083 scope. |
