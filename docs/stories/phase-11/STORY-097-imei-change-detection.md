# STORY-097: IMEI Change Detection & Re-pair Workflow

## User Story
As a sim_manager, I want every observed IMEI logged in history with mismatch / alarm flags, severity-scaled alarms when a SIM's IMEI changes, and an admin "Re-pair to new IMEI" action with audit, so that I can investigate device swaps forensically and clear legitimate replacements without DB surgery.

## Description
Wire the `imei_history` insert pipeline (TBL-59) to every captured auth (success and mismatch alike), surface a per-SIM "Device Binding" tab action to re-pair the SIM to a new IMEI, publish severity-scaled `imei.changed` alarms on the notification bus, and expose a paginated history endpoint (API-330 — already added in STORY-094, this story enriches its data). Re-pair clears `bound_imei`, sets `binding_status='pending'`, retains `binding_mode`, audits, and notifies. RBAC restricts re-pair to admin / sim_manager.

The grace-period countdown alert is the second feature: a scheduled job notifies operators when `binding_grace_expires_at` is within 24 h, and switches behavior to strict on expiry (per STORY-096 AC-7).

## Architecture Reference
- Services: SVC-03 (Core API), SVC-04 (AAA Engine — history-write hook), SVC-08 (Notification), SVC-09 (Job — grace-period scanner), SVC-10 (Audit)
- Packages: `migrations/` (TBL-59 already added in STORY-094 — verify), `internal/store/imei_history.go` (new for write path; STORY-094 added the read-side helper), `internal/api/sim/handler.go` (re-pair endpoint API-329), `internal/notification/event.go` (`imei.changed`, `device.binding_grace_expiring`), `internal/job/binding_grace_scanner.go` (new), `web/src/pages/sims/detail.tsx` (Device Binding tab actions per SCR-021f)
- Source: `docs/architecture/api/_index.md` (API-329, API-330), `docs/screens/SCR-021f-sim-device-binding.md`, `docs/adrs/ADR-004-imei-binding-architecture.md`

## Screen Reference
- SCR-021f — SIM Detail → Device Binding tab. Adds the "Re-pair to new IMEI" button (with confirmation modal), the IMEI History panel, and the grace-period countdown badge.

## Acceptance Criteria
- [ ] AC-1: Every auth that produces a non-empty `SessionContext.IMEI` (success or mismatch, all three protocols) writes one row to `imei_history` (TBL-59). Columns populated: `tenant_id, sim_id, observed_imei, observed_software_version (nullable), observed_at, capture_protocol, nas_ip_address (nullable), was_mismatch, alarm_raised`. Insert is asynchronous (bounded buffered writer) so auth path is not slowed.
- [ ] AC-2: `imei_history` is append-only from application code — no UPDATE / DELETE statements anywhere in `internal/store/imei_history.go`. A code-search test (or static analysis) verifies absence of these statements; only INSERT and SELECT are present.
- [ ] AC-3: API-329 POST `/api/v1/sims/{id}/device-binding/re-pair` (idempotent) clears `sims.bound_imei = NULL`, sets `sims.binding_status = 'pending'`, retains `sims.binding_mode`. Writes audit `sim.imei_repaired` (hash-chained) carrying `previous_bound_imei` and the user/actor. Publishes notification `device.binding_re_paired` (severity `info`). Returns 200 with the updated binding DTO. Idempotent — second call on already-cleared SIM returns 200 with no audit / notification re-emission.
- [ ] AC-4: RBAC — API-329 requires `sim_manager+`. `tenant_admin+` retained as superset. `viewer` and `policy_author` roles return 403 `INSUFFICIENT_PERMISSIONS`. Self-service re-pair is NOT exposed to non-admin / non-sim_manager roles via any code path.
- [ ] AC-5: Notification `imei.changed` is published whenever the enforcer (STORY-096) detects mismatch. Severity is scaled by binding_mode: `strict` and `BLACKLIST` → `high`; `tac-lock` and `grace-period` → `medium`; `soft` and `first-use` → `info`. Subject and payload conform to the `bus.Envelope` v2 contract (FIX-212). The corresponding `imei_history` row has `alarm_raised=true`.
- [ ] AC-6: Notification `device.binding_grace_expiring` is published 24 h before `binding_grace_expires_at` for any SIM with `binding_mode='grace-period'`. A scheduled job (`internal/job/binding_grace_scanner.go`) runs hourly via SVC-09. Notification severity `medium`; payload includes `sim_id`, `iccid`, `binding_grace_expires_at`. Idempotent — same SIM does not get re-notified within the same 24 h window.
- [ ] AC-7: On `binding_grace_expires_at` reached, the SIM's behavior in the enforcer (STORY-096) becomes `strict`. This is a runtime check (no migration / mode-write); STORY-096 AC-7 expectation holds.
- [ ] AC-8: API-330 GET `/api/v1/sims/{id}/imei-history` (already exposed in STORY-094) returns rows ordered by `observed_at DESC`, cursor-paginated (default 50, max 200), with filters `since` (RFC3339), `protocol` (`radius|diameter_s6a|5g_sba`). Each row includes `was_mismatch` and `alarm_raised` flags so the UI can color-code.
- [ ] AC-9: SCR-021f Device Binding tab UI:
  - "Bound IMEI" panel shows current `bound_imei`, `binding_mode`, `binding_status`, `binding_verified_at`, and (when present) `binding_grace_expires_at` with countdown ("expires in 14 h").
  - "Re-pair to new IMEI" button → confirmation modal listing previous IMEI + warning text → on confirm, calls API-329, refreshes panel, shows toast "Re-pair successful — pending next auth".
  - "IMEI History" panel renders API-330 results with mismatch (red) / alarm (yellow) badges, paginated.
  - Empty state ("No IMEI observations yet") rendered when history is empty.
- [ ] AC-10: Audit `sim.imei_repaired` participates in the tamper-proof hash chain. Verifier passes after a sequence of (mismatch alarm, re-pair, fresh capture, mismatch again) over time.
- [ ] AC-11: Notification dedup (per FIX-210) applies — the same SIM emitting `imei.changed` multiple times within the dedup window collapses to a single user-visible alert (existing dedup framework, not re-implemented here). Verified with a multi-event integration test.
- [ ] AC-12: Performance — `imei_history` write latency does not push auth p95 above STORY-096 AC-13 (≤ 5% over baseline). The buffered writer drops rows ONLY under explicit overflow (logged at WARN with counter); a partial outage of the writer must not block auth.
- [ ] AC-13: Regression — full `make test` green; SCR-021f loads with empty / populated states correctly; STORY-096 and STORY-094 test suites pass unchanged.

## Dependencies
- Blocked by: STORY-094 (TBL-59 / API-330 schema), STORY-096 (enforcement is what creates mismatch states worth recording)
- Blocks: none in Phase 11

## STORY-095 Handoff Notes (added by Reviewer 2026-05-03)

- **D-188 — API-335 Lookup `bound_sims` + `history` empty arrays:** STORY-095 ships the Lookup endpoint with `bound_sims=[]` and `history=[]` (deferred). STORY-097 must implement both fields: (a) `bound_sims` — populate via `SIMStore.ListByBoundIMEI(ctx, tenantID, imei)` returning `[{sim_id, iccid, binding_mode, binding_status}]`; (b) `history` — populate via `IMEIHistoryStore.ListByObservedIMEI(ctx, tenantID, imei, limit=50)` ordered DESC. The SCR-197 drawer FE already renders both sections with empty-state placeholders. See ROUTEMAP D-188.
- **`IMEIPoolStore.LookupKind` is functional:** STORY-095 ships the full pool lookup including TAC-range matching. STORY-097 change-detection can rely on IMEI pool membership checks being accurate for all three pool kinds.
- **SCR-197 drawer sections pre-wired:** The FE drawer for IMEI Lookup already has 3 sections: List Membership (populated by STORY-095), Bound SIMs, and History (both empty per D-188). STORY-097 connects the backend data — no FE drawer restructuring needed.
- **SIM cross-link navigation:** SCR-197 drawers show a "View in SIM Detail" link for each bound SIM. Wire the link to the device binding tab (`/sims/{id}#device-binding`) per AC-11 in STORY-095 story spec.

## STORY-094 Handoff Notes (added by Reviewer 2026-05-01)

- **D-183 (5G non-3GPP PEI raw retention) — re-targeted here:** D-183 target updated from "STORY-094 / STORY-097" to STORY-097 exclusively. STORY-094 kept SessionContext flat per AC-12 (no PEIRaw). This story must add `SessionContext.PEIRaw string`, extend `ParsePEI` for `mac-`/`eui64-` prefix forms, and propagate through `session.Session`. See ROUTEMAP D-183.
- **Diameter S6a listener now wired (D-182 CLOSED):** STORY-094 Task 7 wired `ExtractTerminalInformation` at the Diameter S6a Notify-Request / ULR path (capture-only). STORY-097 change-detection can rely on `SessionContext.IMEI` being populated for all three protocols (RADIUS + Diameter S6a + 5G SBA). D-182 is RESOLVED.
- **`imei_history.Append` implementation:** STORY-094 shipped `IMEIHistoryStore` with a stub `Append` (STORY-096 fully implements it). STORY-097 consumes append-produced rows via `IMEIHistoryStore.List` (API-330, already exposed). Confirm `Append` is fully implemented before change-detection writes.
- **`SetDeviceBinding` for re-pair:** API-329 re-pair calls `SIMStore.ClearBoundIMEI` shipped in STORY-094. The method signature is `ClearBoundIMEI(ctx, tenantID, simID) error` — use directly.

## STORY-093 Handoff Notes (added by Reviewer 2026-05-01)
- **D-183 (5G non-3GPP PEI raw retention):** PROTOCOLS.md §PEI documents forensic retention for `mac-` / `eui64-` prefix forms. Currently `internal/aaa/sba/imei.go:73-74` returns `("", "", true)` and silently discards non-3GPP PEI values. `SessionContext` has no `PEIRaw` field. If STORY-094 does not implement this, STORY-097 is the latest target: add `SessionContext.PEIRaw string`, extend `ParsePEI` to populate it for non-3GPP prefixes, and propagate to `session.Session`. See ROUTEMAP D-183 for full context.
- **`SessionContext.IMEI` dependency:** Change-detection in this story (AC-1) reads `SessionContext.IMEI` produced by STORY-093 parsers. Confirm at task start that the IMEI value is present in the session blob for all three protocol paths (RADIUS, Diameter S6a, 5G SBA) — STORY-093 confirms RADIUS + 5G SBA wired; Diameter S6a listener wiring targets STORY-094 (D-182).

## Test Scenarios
- [ ] Integration: 5 successful auths → 5 rows in `imei_history` with `was_mismatch=false`, `alarm_raised=false`.
- [ ] Integration: 1 mismatch under `strict` → 1 row with `was_mismatch=true`, `alarm_raised=true`, severity `high` notification, audit row written.
- [ ] Integration: API-329 re-pair on a SIM with `bound_imei='359...'` → DB row shows `bound_imei IS NULL`, `binding_status='pending'`, audit `sim.imei_repaired` carries `previous_bound_imei`, notification `device.binding_re_paired` published.
- [ ] Integration: API-329 second call → 200, no second audit row, no second notification.
- [ ] Integration: API-329 with `viewer` role → 403.
- [ ] Integration: SIM with `binding_mode='grace-period'`, `binding_grace_expires_at = now + 23h` → grace scanner publishes `device.binding_grace_expiring` once; second scanner run within 24h does not re-publish.
- [ ] Integration: SIM crosses `binding_grace_expires_at` → next mismatch returns `BINDING_GRACE_EXPIRED` (STORY-096 path).
- [ ] Integration: API-330 history endpoint — paginate through 75 rows, filter `since`, filter `protocol=radius`, all return correct subsets.
- [ ] Integration: append-only static check — `grep -nE 'UPDATE.+imei_history|DELETE.+imei_history' internal/store/` returns zero matches.
- [ ] Unit: severity mapping table — strict→high, tac-lock→medium, grace-period→medium, soft→info, first-use→info, blacklist→high.
- [ ] Unit: dedup window collapses 5 mismatches in 60 s into 1 user-visible alert.
- [ ] E2E (Playwright): SIM Detail → Device Binding tab → "Re-pair" button → confirm modal → success toast → panel refreshes with `pending` badge.
- [ ] E2E: same tab → IMEI History panel renders with mismatch + alarm color coding.
- [ ] Regression: full `make test` + Vitest + existing E2E suites green.

## Effort Estimate
- Size: M
- Complexity: Medium (write-side history pipeline + 1 endpoint + scheduled scanner + UI patch on SCR-021f; reuses STORY-096 enforcement events and FIX-210 dedup)
- Notes: Re-pair UX is the main customer-support flow — the confirmation modal must spell out exactly what changes (previous IMEI cleared, status pending, mode preserved).
