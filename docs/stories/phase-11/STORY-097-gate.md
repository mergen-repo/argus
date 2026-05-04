# Gate Report: STORY-097 — IMEI Change Detection & Re-pair Workflow

## Summary
- Requirements Tracing: Fields 9/9, Endpoints 3/3 (API-327, API-329, API-330), Workflows 4/4 (history append / re-pair / grace scanner / severity scaling), Components 4/4 (BoundIMEIPanel / IMEIHistoryPanel / DeviceBindingTab / RePairDialog)
- Gap Analysis: 13/13 ACs covered (10 PASS, 3 deferred to documented tech debt with no AC-9 violation)
- Compliance: COMPLIANT (PAT-022 string discipline + PAT-026 8-layer sweep verified)
- Tests: 4129/4129 Go tests PASS, 111 packages clean; `binding/job/events/notification/api-sim/aaa-radius` race-clean (802 PASS)
- Test Coverage: severity-mapping table now exercises 10 (mode, mismatch) cases; AC-3 idempotency + AC-5 severity scaling + AC-6 dedup + AC-1 NULL-mode history all covered
- Performance: no new hot-path queries introduced; iccid lookup on re-pair is best-effort + non-blocking
- Build: PASS (`go build ./...`, `vite build` 2.95s, 0 type errors)
- Screen Mockup Compliance: AC-9 elements 4/4 implemented; 2 mockup-only elements (allowlist sub-table, force-re-verify) routed to D-194; reason-radio routed to VAL-067
- UI Quality: tokenization tightened (17 arbitrary `text-[Npx]` → semantic `text-xs` / `text-sm`), `role="alert"` added to two error CardContent containers, custom Dialog given explicit `aria-labelledby` + `aria-describedby` (custom impl, not Radix — verified)
- Token Enforcement: 17 arbitrary px violations across 4 files → 0 (`w-[3px]`, `tracking-[0.5px]`, `w-[150px]` retained as positional exceptions per FRONTEND.md guidance)
- Overall: **PASS**

## Team Composition
- Analysis Scout: 6 findings (F-A1..F-A6)
- Test/Build Scout: 0 findings (all green)
- UI Scout: 7 findings (F-U1..F-U7)
- De-duplicated: 13 → 13 (no overlap between scouts)

## Findings Disposition

| ID | Severity | Title | Disposition |
|----|----------|-------|-------------|
| F-A1 | CRITICAL | `imei.changed` subject declared but never emitted | **FIXED** — `rejectMismatch` + `softAlarm` re-pointed to `NotifSubjectIMEIChanged`; 8 test files updated to match (5 in `binding/`, 1 in `aaa/radius/`); blacklist + first-use lock + grace-change subjects untouched |
| F-A2 | HIGH | PAT-026 8-layer sweep missing for 3 new subjects | **FIXED** — registered `imei.changed` (high), `device.binding_re_paired` (info), `device.binding_grace_expiring` (medium) in `events/catalog.go::Catalog`, `events/tiers.go::tier3Events`, `notification/service.go::publisherSourceMap` (all → `sim`) |
| F-A3 | HIGH | FE `BindingMode` union missing 4 of 6 ADR-004 modes | **FIXED** — replaced union with canonical `strict\|allowlist\|first-use\|tac-lock\|grace-period\|soft\|disabled`; `BINDING_MODE_LABEL` filled out; smoke test updated |
| F-A4 | MEDIUM | API-329 payload missing iccid + actor_user_id | **FIXED** — handler now calls `simStore.GetICCIDByID` (best-effort), payload reshaped to `{sim_id, iccid, previous_bound_imei, actor_user_id}` per plan §T6; `severity` field removed (encoded by bus.Envelope per FIX-212) |
| F-A5 | MEDIUM | AC-5 wording vs code mismatch on grace-expired severity | **FIXED (DOC)** — STORY-097 spec AC-5 amended to spell out per-mode severities including `grace-period-expired=high` reconciling VAL-066; cross-reference added |
| F-A6 | LOW | Severity table missing grace-mid-window + tac-lock-same-TAC cases | **FIXED** — 2 rows added (`grace-period-within-window` → AllowWithAlarm SeverityMedium; `tac-lock-same-tac` → Allow) |
| F-U1 | MEDIUM | Arbitrary pixel typography (17 occurrences) | **FIXED** — `text-[10px]/[11px]/[12px]` → `text-xs`; `text-[13px]` → `text-sm` across 2 STORY-097 component files; positional `w-[3px]`, `w-[150px]`, `tracking-[0.5px]` retained per FRONTEND.md exception list |
| F-U2 | HIGH | "Allowed IMEIs" allowlist sub-table absent | **DEFER** — D-194 (NEW) routed to STORY-094/095 follow-up. Mockup element NOT listed in any AC-9 sub-bullet; UI renders correctly without it |
| F-U3 | HIGH | "Force Re-verify" button absent | **DEFER** — D-194 (NEW). Same rationale: not in AC-9; additive enhancement |
| F-U4 | HIGH | Re-pair dialog missing reason radio | **VAL** — VAL-067. AC-3 payload spec is `{previous_bound_imei, actor}`; no reason field; defer to future enhancement when customer demand surfaces |
| F-U5 | MEDIUM | Error states missing `role="alert"` | **FIXED** — added `role="alert"` + `aria-live="polite"` to `device-binding-tab.tsx` and `imei-history-panel.tsx` error CardContent containers |
| F-U6 | MEDIUM | Re-pair dialog missing aria-describedby | **FIXED** — Dialog is a custom component (NOT Radix — verified at `web/src/components/ui/dialog.tsx`); auto-wiring does not exist. Added explicit `role="alertdialog"`, `aria-labelledby={titleId}`, `aria-describedby={descriptionId}`, plus matching `id` props on `DialogTitle` + `DialogDescription` in `re-pair-dialog.tsx` |
| F-U7 | LOW | Grace tone naming "safe" vs visual "warning" unclear | **FIXED** — `GraceTone` union renamed `'safe'` → `'caution'`; `graceToneFor` updated; `BoundIMEIPanel` ternary still works (default branch covers both); FE smoke test message updated |

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Bug | `internal/policy/binding/enforcer.go` | `rejectMismatch` + `softAlarm` NotifSubject → `NotifSubjectIMEIChanged` | tests pass |
| 2 | Test sync | `internal/policy/binding/enforcer_test.go` | 12 `NotifSubjectBindingFailed` + 2 `NotifSubjectIMEIMismatch` → `NotifSubjectIMEIChanged` (replace_all) | tests pass |
| 3 | Test sync | `internal/policy/binding/orchestrator_test.go` | 4 occurrences updated + assertion text in TestOrchestrator_Reject | tests pass |
| 4 | Test sync | `internal/policy/binding/integration_test.go` | wantSubject updated | tests pass |
| 5 | Test sync | `internal/policy/binding/enforcer_bench_test.go` | NotifSubject updated | bench compiles |
| 6 | Test sync | `internal/policy/binding/history_writeback_regression_test.go` | NotifSubject updated | tests pass |
| 7 | Test sync | `internal/policy/binding/story097_integration_test.go` | TestSeverityScaling_E2E — 4 of 5 wantSubject flipped to `NotifSubjectIMEIChanged`; grace-mid-window kept as `NotifSubjectBindingGraceChange` | tests pass |
| 8 | Test sync | `internal/aaa/radius/server_binding_test.go` | mock Verdict NotifSubject updated | tests pass |
| 9 | Catalog | `internal/api/events/catalog.go` | +3 CatalogEntry (imei.changed/binding_re_paired/binding_grace_expiring) with full meta_schema | build PASS |
| 10 | Catalog | `internal/api/events/tiers.go` | +3 entries in tier3Events | build PASS |
| 11 | Catalog | `internal/notification/service.go` | +3 entries in publisherSourceMap (all → `sim`) | build PASS |
| 12 | FE types | `web/src/types/device-binding.ts` | BindingMode union → 7 canonical values; BINDING_MODE_LABEL filled out; GraceTone `'safe'` → `'caution'` | tsc PASS |
| 13 | FE | `web/src/components/sims/__tests__/device-binding-tab.test.tsx` | mode list updated to 7 values; tone test updated to 'caution' | tsc PASS |
| 14 | API | `internal/api/sim/device_binding_handler.go` | API-329 notification payload reshaped — added iccid (via GetICCIDByID best-effort), actor_user_id; removed severity | tests pass |
| 15 | UI tokens | `web/src/components/sims/bound-imei-panel.tsx` | text-[10/11px]→text-xs; text-[13px]→text-sm | vite build PASS |
| 16 | UI tokens | `web/src/components/sims/imei-history-panel.tsx` | text-[10/11/12px]→text-xs; text-[13px]→text-sm | vite build PASS |
| 17 | A11y | `web/src/components/sims/device-binding-tab.tsx` | error CardContent + role="alert" + aria-live="polite" | tsc PASS |
| 18 | A11y | `web/src/components/sims/imei-history-panel.tsx` | error CardContent + role="alert" + aria-live="polite" | tsc PASS |
| 19 | A11y | `web/src/components/sims/re-pair-dialog.tsx` | DialogContent role="alertdialog" + aria-labelledby + aria-describedby; matching id on Title/Description | vite build PASS |
| 20 | DocAmend | `docs/stories/phase-11/STORY-097-imei-change-detection.md` | AC-5 wording amended to per-mode + per-mismatch-type severity table reconciling VAL-066 | n/a |
| 21 | TechDebt | `docs/ROUTEMAP.md` | D-194 NEW (allowlist sub-table + Force Re-verify) | n/a |
| 22 | Decisions | `docs/brainstorming/decisions.md` | VAL-067 NEW (re-pair reason radio deferral) | n/a |
| 23 | Test coverage | `internal/policy/binding/severity_mapping_test.go` | +2 rows (grace-mid-window, tac-lock-same-TAC) | tests pass |

## Escalated Issues
None. All CRITICAL + HIGH findings resolved or scope-deferred with explicit AC justification.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-194 | F-U2 + F-U3 — SCR-021f mockup elements (allowlist sub-table + Force Re-verify) out of AC-9 scope | STORY-094/095 follow-up (SCR-021f polish) | YES |

## Validation Decisions Recorded
- **VAL-067** — Re-pair reason radio group deferred (AC-3 doesn't require it; await customer demand)

## PAT-026 8-Layer Sweep Trace (FIX-238 RECURRENCE protocol)
| Layer | Where | Status |
|-------|-------|--------|
| L1 — binding constants | `internal/policy/binding/types.go::NotifSubject*` | Already declared — verified |
| L2 — enforcer/handler emitters | `enforcer.go::rejectMismatch + softAlarm` | FIXED (this gate) |
| L3 — events catalog | `internal/api/events/catalog.go::Catalog` | FIXED (+3 entries) |
| L4 — events tiers | `internal/api/events/tiers.go::tier3Events` | FIXED (+3 entries) |
| L5 — notification source map | `internal/notification/service.go::publisherSourceMap` | FIXED (+3 entries) |
| L6 — main.go wiring | `cmd/argus/main.go` | No-op (no subject registration in main; binding gate already wired) |
| L7 — test fixtures | `internal/policy/binding/*_test.go` (5 files) + `internal/aaa/radius/server_binding_test.go` | FIXED (subject assertions updated) |
| L8 — DSL fixture | n/a (non-DSL events) | N/A |

## Verification
- `go build ./...` → PASS
- `go vet ./...` → clean
- `gofmt -l` on touched files → empty
- `go test -count=1 ./...` → 4129 passed (111 packages)
- `go test -count=1 -race ./internal/policy/binding/... ./internal/job/... ./internal/api/events/... ./internal/notification/... ./internal/api/sim/... ./internal/aaa/radius/...` → 802 passed, race-clean
- `cd web && npx tsc --noEmit` → PASS
- `cd web && npm run build` → PASS (`✓ built in 2.95s`)
- Fix iterations: 1 (no rework needed)

## Passed Items
- AC-1 (history append on every non-empty IMEI auth) — covered by `TestNullMode_HistoryRow_WasMismatchFalse` + orchestrator buffered-writer tests
- AC-2 (append-only) — `internal/store/imei_history.go` audited; only INSERT + SELECT
- AC-3 (re-pair lifecycle + idempotency) — `TestRePair_LifecycleIntegration` + `TestRePair_Idempotency_NoDoubleAudit` + handler 7-test suite
- AC-4 (RBAC) — handler mounted under `sim_manager+` role gate (D-RBAC pattern)
- AC-5 (severity scaling + `imei.changed` subject) — `TestSeverityScaling_E2E` 5 cases + severity_mapping_test.go 10 cases (post-gate)
- AC-6 (grace scanner dedup) — `binding_grace_scanner_test.go` 10 tests including Redis SETNX dedup
- AC-7 (post-expiry → strict) — `evalGracePeriod` row #20
- AC-8 (API-330 cursor pagination) — pre-existing from STORY-094, retained
- AC-9 (SCR-021f) — Bound IMEI panel + Re-pair button + IMEI History panel + grace countdown badge all rendered; allowlist sub-table + Force Re-verify deferred to D-194 (not AC-required)
- AC-10 (audit hash chain) — `sim.imei_repaired` participates in chain via `auditSvc.CreateEntry`
- AC-11 (dedup window) — FIX-210 framework applies via subject `imei.changed`
- AC-12 (perf) — buffered writer non-blocking; iccid lookup on re-pair is best-effort
- AC-13 (regression) — full `make test` green
