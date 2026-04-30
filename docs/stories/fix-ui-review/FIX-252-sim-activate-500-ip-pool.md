# FIX-252 — `POST /api/v1/sims/{id}/activate` returns 500 (likely IP-pool allocation)

**Tier:** P2 | **Effort:** S | **Wave:** UI Review Remediation — backend hardening
**Dependencies:** none
**Surfaced by:** FIX-249 Gate UI Scout (F-U2)

## Problem Statement

`POST /api/v1/sims/{id}/activate` returns HTTP 500 when reactivating a previously-suspended SIM.
Reproduces on SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1` which was left in `suspended` state
during the FIX-249 UI Scout's live-event smoke test (the suspend half worked correctly; the
re-activate half returned 500).

The most likely root cause is **IP-pool allocation failure** during the activate handler —
either:

1. A previously-allocated IP for this SIM was not released on suspend, and the activate
   re-allocation collides with a unique constraint, OR
2. The IP-pool exhausted available addresses for this APN/operator and the handler returns
   bare 500 instead of a structured pool-exhausted error, OR
3. A nil-pointer / nil-slice path inside the activate state-machine that the suspend path
   does not trigger.

## Acceptance Criteria

- [ ] **AC-1:** `POST /api/v1/sims/{id}/activate` succeeds for a SIM in `suspended` state
      whose previous activation completed cleanly (round-trip suspend → activate works).
- [ ] **AC-2:** When the IP pool genuinely cannot allocate, handler returns a structured
      4xx (e.g. `409 IP_POOL_EXHAUSTED`) — never a bare 500.
- [ ] **AC-3:** Root cause logged with stack trace + correlation ID; an audit log entry is
      written for both success and failure.
- [ ] **AC-4:** Unit test: activate-after-suspend round-trip on a seeded SIM passes.
- [ ] **AC-5:** Manual: SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1` can be reactivated (or
      restored via `make db-seed`).

## Files to Touch (best-effort)

- `internal/api/sim_handler.go` (activate endpoint)
- `internal/store/sim_store.go` (state transition + IP allocation call)
- `internal/aaa/ip_pool.go` (allocation logic — verify release on suspend)
- Migration if a constraint needs adjusting — unlikely
- Test file: `internal/api/sim_handler_test.go` (round-trip activate-after-suspend)

## Investigation Steps

1. Reproduce locally with `curl -X POST http://localhost:8084/api/v1/sims/fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1/activate -H 'Authorization: Bearer ...'`.
2. Check Argus container logs for the 500 — capture full stack trace.
3. Identify the panic site: likely `ip_pool.Allocate(...)` returns an error that the handler
   bubbles as 500 without classification.
4. Inspect SIM row state: `tenant_id`, `apn_id`, previous `ip_address`, `status_history`.
5. Audit suspend flow: confirm IP IS released on suspend (`ip_address` set to NULL, pool row
   freed). If not, that's the root cause — the second allocate collides.
6. Implement structured error handling in the activate handler; map pool errors to 409 with
   `ERR_IP_POOL_EXHAUSTED` code.

## Manual Restore

After fix or as a workaround, the stuck SIM can be restored by:

```bash
make db-seed   # full reseed (loses other test state)
# OR targeted SQL:
psql ... -c "UPDATE sims SET status='active' WHERE id='fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1';"
```

## Risks & Regression

- Activate handler is on the hot path for every operator-driven activation. Any change must
  preserve the success path. Add a regression test before refactoring.
- IP-pool semantics are shared with eSIM provisioning — verify no cross-coupling regression.

## Test Plan

- [ ] Unit: seed SIM in `suspended` state; call activate; assert 200 + new IP allocated.
- [ ] Unit: exhaust IP pool; call activate; assert 409 with `ERR_IP_POOL_EXHAUSTED` (NOT 500).
- [ ] Manual: restore stuck SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1`; confirm activate succeeds.
- [ ] Audit log: confirm both success and failure activate attempts produce audit rows.

## Plan Reference

Surfaced in: `docs/stories/fix-ui-review/FIX-249-gate.md` § F-U2
