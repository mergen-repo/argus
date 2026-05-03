# STORY-096: Binding Enforcement & Mismatch Handling

## User Story
As a platform operator, I want the AAA engine to gate authentication on each SIM's binding mode, so that SIMs in `strict`, `allowlist`, `first-use`, `tac-lock`, `grace-period`, or `soft` mode behave exactly as ADR-004 specifies and a stolen / swapped SIM is either rejected or alarmed depending on policy.

## Description
Implement the **binding pre-check** in the AAA engine: a stage that runs after IMEI capture (STORY-093) and before policy DSL evaluation, gates Access-Accept / Access-Reject based on the SIM's `binding_mode` × the observed IMEI × pool membership, and emits both audit and notification events when a mismatch occurs. Six modes are implemented exactly per ADR-004; the NULL default short-circuits the pre-check entirely. A new "Unverified Devices" report aggregates fleet-wide SIMs in `pending` or `mismatch` state.

This is the "teeth" of the IMEI epic — the story where stolen-device protection actually fires. It MUST NOT degrade auth latency by more than 5% on the existing 1M-SIM benchmark.

## Architecture Reference
- Services: SVC-04 (AAA Engine), SVC-05 (Policy — for pool predicate reads), SVC-08 (Notification), SVC-10 (Audit)
- Packages: `internal/aaa/radius/handler.go`, `internal/aaa/diameter/handler.go`, `internal/aaa/sba/handler.go`, `internal/policy/binding/enforcer.go` (new), `internal/audit/`, `internal/notification/event.go`, `internal/reports/` (new report definition)
- Source: `docs/architecture/PROTOCOLS.md`, `docs/architecture/DSL_GRAMMAR.md` (note at line 308 about pre-check ordering), `docs/adrs/ADR-004-imei-binding-architecture.md`
- Existing reject reason codes: extend the AAA reject-reason catalog with `BINDING_MISMATCH_STRICT`, `BINDING_MISMATCH_ALLOWLIST`, `BINDING_MISMATCH_TAC`, `BINDING_BLACKLIST`, `BINDING_GRACE_EXPIRED`

## Screen Reference
- SCR-021f (Device Binding tab) — surfaces binding_status badge updated by enforcement
- SCR-050 (Live Sessions) — displays `binding_status` chip per session row (consumed via existing column toggle)
- New "Unverified Devices" report tile in the existing reports framework (no new dedicated screen — appears under existing Reports list)

## Acceptance Criteria
- [ ] AC-1: A new package `internal/policy/binding/enforcer.go` exposes `Evaluate(ctx, session SessionContext, sim SIM) (Verdict, error)` where `Verdict` is one of `Allow`, `Reject{Reason}`, `AllowWithAlarm{Severity}`. The enforcer is called by RADIUS, Diameter S6a, and 5G SBA handlers immediately after IMEI capture and before policy DSL evaluation.
- [ ] AC-2: Mode `NULL` (default) — enforcer short-circuits with `Allow`. `SessionContext.BindingStatus` is set to `'disabled'`. No audit, no alarm. Auth proceeds unchanged.
- [ ] AC-3: Mode `strict` — when `SessionContext.IMEI != sim.bound_imei`, enforcer returns `Reject{BINDING_MISMATCH_STRICT}`. RADIUS emits Access-Reject with Reply-Message containing the reason; Diameter emits the equivalent rejection (Result-Code 5xxx); 5G SBA returns 403/404 per spec. Audit `sim.binding_mismatch` written, notification `device.binding_failed` published with severity `high`. `binding_status='mismatch'`. When `IMEI` is empty (capture failed) but `binding_mode` is set, treat as mismatch (same path).
- [ ] AC-4: Mode `allowlist` — when `SessionContext.IMEI` is NOT in `sim_imei_allowlist` for this SIM (TBL-60), enforcer returns `Reject{BINDING_MISMATCH_ALLOWLIST}`. Audit + notification as in AC-3. When in the allowlist → `Allow`, `binding_status='verified'`, `binding_verified_at=now()`.
- [ ] AC-5: Mode `first-use` — when `sim.bound_imei IS NULL` and `SessionContext.IMEI != ""`, enforcer captures the observed IMEI: `bound_imei = SessionContext.IMEI`, `binding_status='verified'`, `binding_verified_at=now()`, audit `sim.binding_first_use_locked`, notification `device.binding_locked` (severity `info`). When `bound_imei` already set, behave exactly as `strict`.
- [ ] AC-6: Mode `tac-lock` — when `tac(SessionContext.IMEI) != tac(sim.bound_imei)` (first 8 digits compared), `Reject{BINDING_MISMATCH_TAC}`, audit + notification severity `medium`. Same TAC → `Allow`, `binding_status='verified'`.
- [ ] AC-7: Mode `grace-period` — like `first-use` for initial capture. Once bound, IMEI changes within `binding_grace_expires_at` are accepted with `binding_status='pending'` and notification `device.binding_grace_change` (severity `medium`). After expiry, behaves as `strict` and returns `Reject{BINDING_GRACE_EXPIRED}` for any mismatch. The grace window length is read from a tenant-scoped config (default 72h) and `binding_grace_expires_at` is set on each accepted change.
- [ ] AC-8: Mode `soft` — never rejects. On every `IMEI != bound_imei`, enforcer returns `AllowWithAlarm{info}`. Audit `sim.binding_soft_mismatch`, notification `imei.mismatch_detected` severity `info`. `binding_status='mismatch'` is set on the SIM but auth still succeeds.
- [ ] AC-9: Hard-deny path — at any binding_mode (including `NULL`), if `device.imei_in_pool('blacklist')` is true, enforcer returns `Reject{BINDING_BLACKLIST}` and the result OVERRIDES any `Allow` from the mode-specific check. Audit `sim.binding_blacklist_hit`, notification severity `high`. (Whitelist / greylist do not affect the verdict here — they participate in DSL post-pre-check policies per `DSL_GRAMMAR.md` line 308.)
- [ ] AC-10: When the enforcer returns a non-`Allow` verdict, the AAA handler:
  - emits Access-Reject (RADIUS) / appropriate failure response (Diameter / 5G) with the reject reason code present in the wire payload (Reply-Message for RADIUS; Error-Diagnostic AVP for Diameter; problem-details JSON for SBA),
  - writes one tamper-proof audit row with `action`, `sim_id`, `tenant_id`, `observed_imei`, `bound_imei`, `binding_mode`, `reason_code`, `protocol`,
  - publishes one bus envelope (NATS subject `notifications.binding.*`) with severity per AC-3..AC-9,
  - inserts one row into `imei_history` (TBL-59) with `was_mismatch=true`, `alarm_raised=true`.
- [ ] AC-11: For `Allow` and `AllowWithAlarm` verdicts, `imei_history` still records the observation (`was_mismatch=false` for Allow, `was_mismatch=true` for AllowWithAlarm) when `SessionContext.IMEI` is non-empty. Storage is asynchronous (insert hands off to a buffered writer) so the auth path is not slowed by DB latency.
- [ ] AC-12: New "Unverified Devices" report (e.g., `internal/reports/unverified_devices.go`) lists tenant SIMs with `binding_status IN ('pending','mismatch')`. Report appears under the existing reports framework (no new screen), supports cursor pagination, and includes columns `iccid, sim_id, binding_mode, binding_status, last_imei_seen_at, bound_imei`.
- [ ] AC-13: Performance — auth-path p95 latency overhead from the enforcer ≤ 5% over the baseline measured against the 1M-SIM benchmark suite (existing harness from earlier phases). Recorded in story-096 plan addendum.
- [ ] AC-14: DSL post-pre-check policies — DSL programs that match on `device.binding_status` (set by the pre-check) work for all six modes, including `disabled` for NULL mode. `DSL_GRAMMAR.md` ordering guarantee is verified by integration test.
- [ ] AC-15: RBAC — only SIMs scoped to the calling tenant are evaluated; cross-tenant pool entries do not affect a SIM's verdict. RLS enforced in all enforcer queries.
- [ ] AC-16: Audit hash chain remains valid after a mixed run of all six modes producing a stream of audit rows.
- [ ] AC-17: Regression — STORY-015 / STORY-019 / STORY-020 test suites pass unchanged for SIMs where `binding_mode IS NULL` (default, vast majority). 100% of pre-existing AAA-path E2E tests stay green.

## Dependencies
- Blocked by: STORY-094 (column model + DSL parser), STORY-095 (pool tables for blacklist hard-deny + allowlist semantics)
- Blocks: STORY-097 (change-detection semantics depend on the enforcer producing mismatch events and history rows)

## Test Scenarios
- [ ] Integration: SIM with `binding_mode=NULL` → auth proceeds, `binding_status='disabled'`, no audit, no notification, no history row.
- [ ] Integration: SIM with `binding_mode='strict'`, observed IMEI matches `bound_imei` → Allow, `binding_status='verified'`, history row with `was_mismatch=false`.
- [ ] Integration: SIM with `binding_mode='strict'`, observed IMEI differs → Access-Reject with `BINDING_MISMATCH_STRICT`, audit + notification + history row written.
- [ ] Integration: SIM with `binding_mode='strict'`, observed IMEI empty (capture failed) → treated as mismatch, Reject + audit + notification.
- [ ] Integration: SIM with `binding_mode='allowlist'`, IMEI in `sim_imei_allowlist` → Allow, `binding_status='verified'`.
- [ ] Integration: SIM with `binding_mode='allowlist'`, IMEI not in `sim_imei_allowlist` → Reject `BINDING_MISMATCH_ALLOWLIST`.
- [ ] Integration: SIM with `binding_mode='first-use'` and `bound_imei IS NULL` → first observed IMEI captured, `bound_imei` populated, `binding_status='verified'`, audit `sim.binding_first_use_locked`.
- [ ] Integration: same SIM second auth with different IMEI → Reject (acts as strict).
- [ ] Integration: SIM with `binding_mode='tac-lock'`, observed IMEI shares TAC with bound → Allow.
- [ ] Integration: same SIM, observed IMEI different TAC → Reject `BINDING_MISMATCH_TAC`, severity `medium`.
- [ ] Integration: SIM with `binding_mode='grace-period'` within window, IMEI change → Allow with `device.binding_grace_change` notification, `binding_status='pending'`.
- [ ] Integration: same SIM after `binding_grace_expires_at` past → Reject `BINDING_GRACE_EXPIRED`.
- [ ] Integration: SIM with `binding_mode='soft'`, observed IMEI mismatch → Allow with alarm, `binding_status='mismatch'`, severity `info`.
- [ ] Integration: SIM (any mode including NULL) with observed IMEI in blacklist → Reject `BINDING_BLACKLIST` overrides mode verdict.
- [ ] Integration: Across all three protocols (RADIUS, Diameter, 5G) — same SIM produces consistent verdicts; reason code surfaces in protocol-appropriate field.
- [ ] Integration: Unverified Devices report returns the right SIMs after a mixed test run.
- [ ] Integration: Audit hash chain valid after 50 mixed-mode auth events.
- [ ] Performance: 1M-SIM benchmark — auth p95 latency overhead ≤ 5% vs. baseline (with NULL mode majority).
- [ ] Unit: enforcer logic table — 6 modes × (bound IMEI present / absent) × (observed IMEI match / differ / empty) × (in pool / not).
- [ ] Regression: full `make test` + existing AAA E2E suites green.

## STORY-094 Handoff Notes (added by Reviewer 2026-05-01)

- **D-184 — 1M-SIM benchmark re-targeted here:** STORY-094 has no enforcement on the AAA hot path, so the 1M-SIM bench is not meaningful there. D-184 target updated to STORY-096. Run the literal 1M-SIM bench when the binding pre-check lands on the auth hot path; record p95 latency overhead and update plan §Perf note.
- **`BindingStatus` / `BindingMode` writes:** STORY-094 STORY-094 ships the column model (`sims.binding_status`, `sims.binding_mode`) and the `SetDeviceBinding` store method. STORY-096 is the FIRST story to write `binding_status` back to the DB during auth (e.g., transition `pending → verified`, `verified → mismatch`). Do not add binding_status writes in STORY-095.
- **`imei_history.Append` stub:** STORY-094 shipped `IMEIHistoryStore` with `List` + a stub `Append`. STORY-096 is the consumer of `Append` on every auth that captures an IMEI. Implement the full `Append` method (per-protocol, `was_mismatch`, `alarm_raised` fields) during this story.
- **PAT-006 guard:** The enforcer that calls `SetDeviceBinding` must pre-fetch existing state (as F-A2 mandated for the bulk worker) before any UPDATE that patches only some binding fields.
- **PAT-031 guard:** Any new PATCH handler in STORY-096 that accepts nullable fields must use non-pointer `json.RawMessage` + `decodeOptionalStringField`.

## Effort Estimate
- Size: L
- Complexity: High (six modes × three protocols, perf budget, audit + notification + history side effects all in the auth-hot path)
- Notes: This is where customer-facing behavior is verified. The 5% latency budget is a hard gate — instrument, measure, and tune buffered history writes if needed.
