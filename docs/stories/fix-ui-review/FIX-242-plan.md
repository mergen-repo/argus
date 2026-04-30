# FIX-242 — Implementation Plan

**Story:** Session Detail Extended DTO Populate — SoR/Policy/Quota fields
**Tier:** P0 · **Effort:** M · **Wave:** 8 (Phase 2 P0 Critical)
**Mode:** AUTOPILOT
**Story spec:** `docs/stories/fix-ui-review/FIX-242-session-detail-dto-populate.md`
**Plan author:** Planner Agent (Amil)
**Plan date:** 2026-04-26

---

## 1. Problem Restated

`internal/api/session/handler.go::Get` (line 268) builds the response as:

```go
detail := sessionDetailDTO{sessionDTO: dto}
apierr.WriteSuccess(w, http.StatusOK, detail)
```

`sessionDetailDTO` (handler.go:116) embeds `sessionDTO` and adds three pointer extensions
`SorDecision *sorDecisionDTO`, `PolicyApplied *policyAppliedDTO`, `QuotaUsage *quotaUsageDTO`,
all marked `omitempty`. None are populated. FE Session Detail page (`web/src/pages/sessions/detail.tsx`)
already renders all three tabs against these pointer paths — but every session falls back to the
"No data" empty state. F-159 / F-161 / F-162 / F-299 share this single root cause.

## 2. Code Reality vs Spec — Critical Discoveries

These were verified by direct grep before drafting the plan; they materially reshape the spec:

### 2.1 SoR engine is dead code (story-spec mismatch)

- **Engine exists:** `internal/operator/sor/engine.go` defines `Engine.Evaluate(ctx, SoRRequest) -> *SoRDecision`.
- **Zero call sites:** `grep -rn 'sor.NewEngine' .` returned nothing. The package is not constructed in
  `cmd/argus/main.go` and is not wired into `internal/aaa/session/session.go::Manager.Create` (which
  hard-codes `SoRDecision: <not set>` in `CreateRadiusSessionParams` — see session.go:253).
- **Schema for persistence already exists:** Migration `20260321000001_sor_fields.up.sql` already added
  `sessions.sor_decision JSONB`. The store (`internal/store/session_radius.go:45`) carries `SoRDecision json.RawMessage`
  through INSERT/SELECT. No new `sor_decisions` table is required — **the spec's AC-2 is wrong about
  needing a new child table.**
- **DTO ↔ engine struct mismatch:** Handler DTO needs per-candidate `Scoring []sorScoreEntry{operator_id, score, reason}`.
  Engine `sor.SoRDecision` only emits `PrimaryOperatorID + FallbackOperatorIDs + single Reason` — no per-candidate
  scoring array. Even if wired today, scoring would be empty.
- **Implication:** Wiring the engine into the live auth path AND extending its decision struct to carry
  per-candidate scoring is itself L/XL work (involves grant + roaming-agreement provider plumbing,
  circuit-breaker wiring, and call-site integration in `session_radius.go`/`bind.go`-equivalent paths).
  **It will not fit inside an M envelope.** Decision DEV-404 (below) defers engine wiring to a new follow-up
  story (FIX-24x candidate); FIX-242 builds the *persistence pipeline* and *DTO populator* so that the
  day the engine ships, data flows automatically.

### 2.2 SoR `Scoring` array — DTO contract decision

DEV-405 (below) constrains FIX-242 to populate SoR DTO **only when** `sessions.sor_decision` JSONB
contains a payload that includes a `scoring` array. Existing seed/live data has no SoR rows. AC-11
becomes the user-facing contract for "feature pending wiring" until the follow-up ships.

### 2.3 FE rendering is already complete

`web/src/pages/sessions/detail.tsx` lines 282–415 already render the SoR, Policy, Quota tabs against
`session.sor_decision`, `session.policy_applied`, `session.quota_usage`. **No JSX work is needed for
the tab bodies.** Only:
- Type augmentation (`web/src/types/session.ts` adds `sor_decision?`, `policy_applied?`, `quota_usage?`,
  `coa_history?`)
- Layout fix per AC-12 (top cards grid-cols-2 + new bottom cards)
- Empty-state copy update to the AC-11 wording

### 2.4 Policy + CoA data is fully available in `policy_assignments`

`policy_assignments` (TBL-15, defined in `migrations/20260320000002_core_schema.up.sql:303`) carries
`sim_id, policy_version_id, rollout_id, assigned_at, coa_sent_at, coa_status` (with the FIX-234 enum CHECK).
Existing store helper `PolicyStore.GetAssignmentBySIMForResend(ctx, simID)` (policy.go:1501) shows the access
pattern — extend it for richer enrichment.

### 2.5 Quota source — no session-scoped counter exists

`grep -rn 'bandwidth_quota|data_cap|quota.Service|UsedBytes' internal/` shows no session-scoped Redis counter
or maintained DB column. Quota usage must be **derived**:
- **Limit:** parse the applied `policy_versions.dsl` for a `bandwidth_quota` / `data_cap` rule.
- **Used:** session's own `bytes_in + bytes_out` (already in `RadiusSession`) is the usage proxy *for that session*.
- The Aggregates facade method `CDRStatsInWindow` (analytics/aggregates/service.go:32) accepts
  `CDRFilter.SessionID` — use this for accurate CDR-derived usage when available.

DEV-401 captures the source choice.

### 2.6 Audit infrastructure for session events

`audit.Auditor` interface is already injected into `aaa/session/coa.go` and `aaa/session/dm.go`
(both call `auditor.CreateEntry` for `entity_type=session`). `internal/aaa/session/session.go::Manager`
itself does **not** call CreateEntry on `Create` / `Finalize`. The session-entity publisher is just
two missing call sites — not a new package.

## 3. Architecture References Embedded

- `docs/ARCHITECTURE.md` — modular monolith, Chi router, store layer per tenant
- `docs/architecture/api/_index.md` — session endpoints API-051..API-058 (envelope `{status, data, meta?, error?}`)
- `docs/architecture/db/_index.md` — TBL-15 (`policy_assignments`), TBL-17 (`sessions`)
- `docs/SCREENS.md` — SCR-Session-Detail (the page being fixed)
- `docs/FRONTEND.md` — design tokens (`bg-bg-surface`, `border-border`, `text-text-primary`,
  `text-success/warning/danger`); progress bar tokens already used in detail.tsx Quota tab
- `docs/architecture/MIDDLEWARE.md` — `apierr.TenantIDKey` extraction pattern (already used in `Get`)
- `docs/architecture/WEBSOCKET_EVENTS.md` (FIX-212) — event envelope; not in scope here but the audit
  publisher consumes the existing `argus.events.audit.create` subject

## 4. Acceptance Criteria — Per-AC Implementation Strategy

| AC | Strategy | In Scope |
|----|----------|----------|
| AC-1 | Add `enrichSessionDetailDTO(ctx, dto, sess)` helper called from `Get`. Populates the three pointers + `CoaHistory` (AC-7). All fetchers tolerate nil/missing data and leave the pointer nil so `omitempty` keeps the wire payload clean. | YES |
| AC-2 | **Reuse existing `sessions.sor_decision` JSONB column** (no new table — DEV-398). Add a `SoRDecision` parameter pass-through in `session.Manager.Create` so the field flows from caller to DB. **Engine wiring deferred (DEV-404).** Populator decodes JSONB into `sorDecisionDTO` if present; nil otherwise. | PARTIAL (pipeline only) |
| AC-3 | New `policyStore.GetAssignmentDetailBySIM(ctx, simID)` — single-row JOIN of `policy_assignments → policy_versions → policies` returning `{policy_id, policy_name, version_id, version_number, dsl, coa_sent_at, coa_status, coa_failure_reason}`. Populator builds `policyAppliedDTO`. `matched_rules` is **stub: `[]int{}`** since no live evaluator records this per-session today; flagged in TODO + USERTEST notes. | YES (matched_rules is structural placeholder) |
| AC-4 | New helper `computeQuotaUsage(ctx, sess, policyDSL)`: extract `data_cap`/`bandwidth_quota` rule from policy DSL → `LimitBytes`. `UsedBytes = sess.BytesIn + sess.BytesOut`. `Pct = used/limit*100` (clamped). For sessions older than 1h or where CDR data is preferred, use `aggregates.CDRStatsInWindow(SessionID=sess.ID)` — DEV-401. | YES |
| AC-5 | Add `auditor.CreateEntry(...)` calls in `aaa/session/session.go::Manager.Create` (action=`session.started`) and `Manager.Finalize` (action=`session.ended`). Existing `coa.go` / `dm.go` already cover `session.coa_sent` and `session.disconnected`. F-161 closes. | YES |
| AC-6 | `Get` is single-session DETAIL — fetcher is one extra targeted query per data class (3 queries total, parallelizable with `errgroup` if needed). Aggregates facade is **not** required for detail; document the trade-off in DEV-403. List endpoint stays untouched. | YES |
| AC-7 | New helper `fetchCoaHistory(ctx, sessionID)`: query `audit_logs WHERE entity_type='session' AND entity_id=? AND action LIKE 'session.coa%' ORDER BY created_at DESC LIMIT 50`. DEV-400 picks `audit_logs` over `policy_assignments.coa_*` because the latter is current-state-only (single row), the former preserves history. | YES |
| AC-8 | FE Quota progress bar already exists (detail.tsx:380-405) using `bg-success/warning/danger` at 70/90 thresholds. Spec says 80/95 — adjust to spec values. Add `reset_at` countdown (display only when present). | YES |
| AC-9 | FE Policy display already exists (detail.tsx:333-376). Update to show `policy_name` (linked) + `version_number` instead of raw IDs once DTO carries them. | YES |
| AC-10 | FE SoR display already exists (detail.tsx:282-330) — sorts descending, highlights chosen, shows reason. No JSX change. Verify compatibility with new typed DTO. | YES (verify only) |
| AC-11 | Update SoR empty-state copy: "SoR scoring not yet persisted for this session — engine wiring planned in FIX-24x". Quota empty-state copy: "No quota rule defined in applied policy". Policy empty-state: keep "No policy applied". Distinguishes "no data" from "broken". | YES |
| AC-12 | `web/src/pages/sessions/detail.tsx` overview tab — wrap the two top cards in `grid grid-cols-1 md:grid-cols-2 gap-4` and add two new bottom cards: **Session Timeline** (uses `started_at` + duration + `coa_history` last entry) and **Policy Context** (mirrors policy DTO summary — name, version, applied since `assigned_at`). Tokens per `docs/FRONTEND.md`: `bg-bg-surface border-border shadow-card rounded-[10px]`. | YES |

### 4.1 Bonus — D-145 fold-in (Tech Debt)

D-145 requests `coa_failure_reason: string | null` + `coa_sent_at: string | null` on SIM DTO so the
"failed CoA" tooltip can show the actual reason. This work overlaps `policyAppliedDTO` enrichment in
AC-3 (same JOIN, same row). **Folded into AC-3** by adding `coa_failure_reason`, `coa_sent_at`, `coa_status`
fields to `policyAppliedDTO`. The SIM-detail tooltip update is a thin FE change in `web/src/pages/sims/detail.tsx`
that consumes the same enriched data via the existing SIM DTO; included as part of T6.

## 5. Decisions Log (next free DEV-398)

| ID | Decision | Rationale |
|----|----------|-----------|
| **DEV-398** | **Use existing `sessions.sor_decision` JSONB column.** Do NOT create a new `sor_decisions` child table. | Migration `20260321000001_sor_fields.up.sql` already provisioned the column; store layer carries it through; spec is incorrect that a new table is required. Avoids unnecessary schema bloat (Risk 4 in spec is moot). |
| **DEV-399** | **Quota limit source = applied policy's DSL `data_cap` / `bandwidth_quota` rule (parsed once, cached on policy version).** | No standalone `quota_rules` table; DSL is the canonical policy expression per docs/architecture/DSL_GRAMMAR.md. |
| **DEV-400** | **CoA history source = `audit_logs WHERE entity_type='session' AND action LIKE 'session.coa%'`.** | `policy_assignments.coa_*` is current-state-only (single row per SIM). `audit_logs` preserves the historical sequence and survives policy reassignment. |
| **DEV-401** | **Quota usage source = session row's own `bytes_in + bytes_out` (live counters); fall back to `aggregates.CDRStatsInWindow(SessionID=sess.ID)` for finalized/historical sessions where CDRs are authoritative.** | Avoids new Redis hot-counter; reuses the FIX-208 Aggregates facade for CDR-derived numbers; live sessions get sub-ms response from session row. |
| **DEV-402** | **Session-entity audit publisher placement = inline in `internal/aaa/session/session.go::Manager.Create / Finalize`,** following the established pattern in `coa.go` / `dm.go`. No new package. | Minimal surface; matches current code style; avoids over-engineering an "events package". |
| **DEV-403** | **N+1 avoidance pattern = three targeted queries inside `Get` handler, optionally wrapped in `errgroup` for parallelism.** Aggregates facade not used for the per-session detail path. | This is a *detail* endpoint (single session), not a list endpoint. Aggregates facade is for cross-surface count consistency. The three queries (`policy_assignments` row, `audit_logs` history, `cdrs` quota) are each indexed on `session_id`/`sim_id` and return ≤ 50 rows total. |
| **DEV-404** | **SoR engine wiring deferred to follow-up story FIX-24x (TBD).** FIX-242 ships the persistence + DTO pipeline (`SoRDecision` flows from `Manager.Create` params → DB column → DTO populator) but does not instantiate `sor.NewEngine` in main.go. | Wiring engine = grant provider + roaming agreement provider + circuit breaker checker + cache + call-site integration in RADIUS/Diameter/SBA bind paths. That alone is L/XL. AC-11 covers the user-facing message until the follow-up ships. **Risk:** users see "data not yet available" on every session detail until the follow-up. Acceptable per Risk-1 in spec. |
| **DEV-405** | **`SoRDecision` JSONB schema for the column = `{"chosen_operator_id": "...", "scoring": [{"operator_id":"...","score":1.23,"reason":"..."}], "decided_at":"<iso8601>"}`.** Engine does not currently produce this shape — when engine wiring story lands it must serialize to this shape. | Aligns with handler `sorDecisionDTO` exactly; zero transformation in populator. Documents the contract today so the future engine work has a clear target. |
| **DEV-406** | **`policyAppliedDTO` extended with `coa_failure_reason`, `coa_sent_at`, `coa_status` fields (folds in tech debt D-145).** | Same JOIN, same row, no extra cost. Closes D-145 as part of FIX-242. |
| **DEV-407** | **`matched_rules []int` is shipped as empty slice `[]int{}` (not nil) when no per-session evaluator data exists.** Live policy evaluator does not record matched-rule indices per session today. | Honors AC-3 contract (field present, non-nil per FIX-241 nil-slice safety) but is honest about data unavailability. Future enhancement requires the policy DSL evaluator to log matched rule indices into `sessions.policy_match_meta JSONB` (out of scope; tracked as new tech debt below). |

## 6. Tech Debt Recorded

- **D-145 (existing)** — folded into FIX-242 via DEV-406. Mark as RESOLVED in ROUTEMAP after closure.
- **NEW D-147** (to be added by orchestrator after closure) — Policy evaluator does not record matched rule indices per session. Required for AC-3 `matched_rules` to be meaningful. Owner: future policy DSL hardening story.
- **NEW D-148** (to be added by orchestrator after closure) — SoR engine instantiation + per-candidate scoring extension. Required for AC-2 `Scoring` array to populate. Owner: FIX-24x SoR engine wiring follow-up story.

## 7. Bug Pattern Warnings

- **PAT-006 (FIX-201, struct-field omission):** `enrichSessionDetailDTO` adds three pointer fields to a
  struct constructed in multiple places. Risk: another `Get`-like handler (e.g. `Disconnect` post-action
  response, or a future `GetByAcctSessionID`) that also constructs `sessionDetailDTO{...}` will silently
  ship zero-valued fields. **Mitigation:** grep `sessionDetailDTO{` across `internal/api/session/`
  immediately after T3 lands; assert in handler_test that every construction either uses the enricher
  or explicitly sets the new fields. Add a regression test that returns the response from `Get` and
  asserts the JSON shape matches the documented contract (e.g. SoR pointer present-or-absent, never
  half-populated).
- **PAT-009 (nullable FK COALESCE):** `policy_assignments` may have rows where `coa_sent_at IS NULL`
  (assignment never CoA'd). The JOIN must use `LEFT JOIN`/scan `*time.Time`, not require non-null. Same
  for `policy_versions.coa_failure_reason` if added.
- **PAT-017 (config param threaded but not propagated):** When adding `SoRDecision` to
  `CreateRadiusSessionParams` plumbing, every constructor of that param must explicitly pass the field
  even if `nil`. Grep `CreateRadiusSessionParams{` to enumerate sites.

## 8. Task Decomposition (8 tasks, M envelope)

| # | Title | Complexity | Files | Verify |
|---|-------|------------|-------|--------|
| **T1** | **Wire `SoRDecision` JSONB through session create plumbing** (no new migration; column already exists). Add field pass-through in `aaa/session/session.go::CreateSessionParams`-equivalent local struct + `Manager.Create` body so `store.CreateRadiusSessionParams.SoRDecision = sess.SoRDecision`. **Optional `RecordSoRDecision(ctx, sessionID, decision)` PATCH-style helper** for future engine to back-fill if needed. | LOW | `internal/aaa/session/session.go`, optional `internal/store/session_radius.go` (one new method) | `go test ./internal/aaa/session/... ./internal/store/...` |
| **T2** | **Extend `policy.PolicyStore` with `GetAssignmentDetailBySIM(ctx, simID)`** returning `{policy_id, policy_name, version_id, version_number, dsl, coa_sent_at, coa_status, coa_failure_reason}`. Single LEFT JOIN. **Add `coa_failure_reason` column to `policy_assignments`** via migration `20260427000003_policy_assignment_coa_failure_reason.up.sql` (NULLABLE TEXT, DOWN drops it). | MEDIUM | new migration up/down, `internal/store/policy.go`, `internal/store/policy_test.go` | `make db-migrate && make db-seed && go test ./internal/store/...` |
| **T3** | **Implement `enrichSessionDetailDTO(ctx, sess, dto)` in `internal/api/session/handler.go`.** Calls three sub-fetchers: (a) decode `sess.SoRDecision` JSONB → `*sorDecisionDTO`; (b) `h.policyStore.GetAssignmentDetailBySIM(ctx, simID)` → `*policyAppliedDTO` (with empty `matched_rules`); (c) `computeQuotaUsage(ctx, sess, policyDSL)` → `*quotaUsageDTO`; (d) `fetchCoaHistory(ctx, sessionID)` → `[]coaEntry`. Wire `policyStore` + `cdrStore`/`aggregates` deps via `HandlerOption`. Update `Get` to call enricher. Refactor `disconnectResponse` and any other `sessionDetailDTO{}` literal site. | MEDIUM | `internal/api/session/handler.go`, `internal/api/session/handler_test.go`, `cmd/argus/main.go` (constructor opts), small DTO additions: `coaEntry`, extended `policyAppliedDTO` | `go test ./internal/api/session/...` |
| **T4** | **Add session-entity audit publisher** — call `m.auditor.CreateEntry(ctx, audit.CreateEntryParams{EntityType:"session", EntityID:sess.ID, Action:"session.started", AfterData:<sess JSON>})` in `Manager.Create`; `action="session.ended"` (with terminate cause + counters in AfterData) in `Manager.Finalize`. Inject `audit.Auditor` via new `WithAuditor` ManagerOption following `coa.go` pattern. **Verify `coa.go` and `dm.go` continue to publish `session.coa_sent` and `session.disconnected`** (already do — only confirm). | LOW | `internal/aaa/session/session.go`, `cmd/argus/main.go` (pass `auditSvc` to NewManager), `internal/aaa/session/session_test.go` | `go test ./internal/aaa/session/...` |
| **T5** | **FE types alignment** — extend `web/src/types/session.ts` `Session` interface with optional `sor_decision?: { chosen_operator_id?: string; scoring?: { operator_id: string; score: number; reason?: string }[] }`, `policy_applied?: { policy_id?: string; policy_name?: string; version_id?: string; version_number?: number; matched_rules?: number[]; coa_status?: string; coa_sent_at?: string \| null; coa_failure_reason?: string \| null }`, `quota_usage?: { limit_bytes: number; used_bytes: number; pct: number; reset_at?: string }`, `coa_history?: { at: string; reason?: string; policy_version_id?: string; status?: string }[]`. Run `tsc --noEmit`. | LOW | `web/src/types/session.ts` | `cd web && npx tsc --noEmit` |
| **T6** | **FE consumer updates** — (a) detail.tsx Policy tab: render `policy_name` (link via EntityLink) + `version_number`; (b) detail.tsx Quota tab: thresholds 80/95 (was 70/90), optional `reset_at` countdown row; (c) detail.tsx SoR tab: AC-11 empty-state copy update; (d) AC-12 layout fix in overview tab: `grid grid-cols-1 md:grid-cols-2 gap-4` for top cards + new "Session Timeline" + "Policy Context" cards below; (e) **D-145 fold-in:** `web/src/pages/sims/detail.tsx` CoA Status InfoRow tooltip — show `coa_failure_reason` when status='failed' (still source from policy_applied data if SIM detail enriches similarly). All design tokens per FRONTEND.md. | MEDIUM | `web/src/pages/sessions/detail.tsx`, `web/src/pages/sims/detail.tsx`, possibly small new molecule `<CoaHistoryList>` if reused | `cd web && npx tsc --noEmit && npm run lint` |
| **T7** | **Tests + USERTEST coverage** — (a) `internal/store/policy_test.go` — `GetAssignmentDetailBySIM` happy path + nil/missing assignment + null `coa_sent_at`; (b) `internal/api/session/handler_test.go` — `Get` returns extended DTO when policy/quota present, returns base DTO when missing, nil pointers omit cleanly; assert against full JSON shape (PAT-006 mitigation). Include a "construction sites" test that grep-asserts every `sessionDetailDTO{` literal flows through enricher. (c) `internal/aaa/session/session_test.go` — Manager.Create publishes `session.started` audit entry; Manager.Finalize publishes `session.ended`. (d) Write USERTEST script `docs/stories/fix-ui-review/FIX-242-USERTEST.md` covering all 12 ACs with explicit scenarios (with-data + without-data branches). | MEDIUM | test files above + new USERTEST.md | `go test ./... && cd web && npx tsc --noEmit` |
| **T8** | **Decisions doc + ROUTEMAP step-log** — append DEV-398..DEV-407 with their resolution to `docs/dev/DECISIONS.md` (or wherever the project keeps the running log; check `docs/architecture/_index.md` for canonical location). Update `docs/stories/fix-ui-review/FIX-234-step-log.txt`-equivalent FIX-242 step log with steps Plan→Dev→Lint→Gate→Review→Commit. **Do NOT touch ROUTEMAP — Ana Amil owns it.** | LOW | `docs/dev/DECISIONS.md` (or equivalent), `docs/stories/fix-ui-review/FIX-242-step-log.txt` | manual diff review |

**Sequencing:** T1 || T5 (independent) → T2 → T4 → T3 (depends on T1+T2+T4) → T6 (depends on T3+T5) → T7 → T8.

## 9. Files to Touch (consolidated)

**Backend (Go):**
- `migrations/20260427000003_policy_assignment_coa_failure_reason.up.sql` (NEW)
- `migrations/20260427000003_policy_assignment_coa_failure_reason.down.sql` (NEW)
- `internal/aaa/session/session.go` — Manager.Create plumbing for SoRDecision + audit publisher in Create/Finalize + `WithAuditor` option
- `internal/aaa/session/session_test.go` — audit publish coverage
- `internal/store/policy.go` — `GetAssignmentDetailBySIM` (new), update existing helpers if needed for `coa_failure_reason`
- `internal/store/policy_test.go` — coverage
- `internal/store/session_radius.go` — optional `RecordSoRDecision` UPDATE helper for future engine back-fill
- `internal/api/session/handler.go` — DTO types extended, `enrichSessionDetailDTO`, `Get` updated, `HandlerOption` for policyStore + (cdrStore or aggregates), AC-11 empty-state hooks (none — FE-only)
- `internal/api/session/handler_test.go` — new coverage incl. PAT-006 mitigation
- `cmd/argus/main.go` — pass `policyStore`, `cdrStore`/`aggregates`, `auditSvc` into session handler + `auditSvc` into NewManager via WithAuditor

**Frontend:**
- `web/src/types/session.ts` — extended Session interface
- `web/src/pages/sessions/detail.tsx` — AC-8/9/10/11/12 changes (mostly polish + layout)
- `web/src/pages/sims/detail.tsx` — D-145 fold-in tooltip update

**Docs:**
- `docs/dev/DECISIONS.md` (or canonical decisions doc) — DEV-398..DEV-407
- `docs/stories/fix-ui-review/FIX-242-step-log.txt` — step log
- `docs/stories/fix-ui-review/FIX-242-USERTEST.md` — manual test script

## 10. Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| **R1 — SoR engine never gets wired** → "data not available" forever | DEV-404 documents the deferral; AC-11 copy is honest; D-148 enters tech debt; pipeline is built so a small follow-up story unlocks data |
| **R2 — Policy DSL parsing for quota limit is brittle** | T3 uses simple `dsl.Rules` iteration looking for type==`bandwidth_quota`/`data_cap`; if DSL shape changes, fall back to `nil` quota and AC-11 empty state. Add a focused unit test on the DSL extraction. |
| **R3 — `coa_failure_reason` migration FK / data backfill** | Column is nullable + new — no backfill needed. Down migration drops cleanly. Verify `make db-migrate up && make db-migrate down && make db-migrate up` round-trip in T7. |
| **R4 — Audit publisher in Create runs even on rollback** | Wrap `auditor.CreateEntry` call AFTER successful `sessionStore.Create`; on store error the publisher is skipped. Already the pattern in coa.go. |
| **R5 — N+1 if `Get` is called in a loop by FE** | FE calls `/sessions/{id}` only on detail page mount; no loop risk. List endpoint untouched. |
| **R6 — `disconnectResponse` and other handler paths construct `sessionDetailDTO{}` and silently lose new fields** (PAT-006 trigger) | T3 grep audit + T7 regression test; if found, refactor to share enricher. |
| **R7 — `make db-seed` breaks after migration T2** | Verify per project policy "never defer seed failures": run `make db-migrate && make db-seed` after T2 lands; fix any FK violations before continuing. |

## 11. Quality Gate Checklist (self-validation)

- [x] All 12 ACs mapped to a task with explicit verify step (Section 4 + Section 8 cross-reference)
- [x] Decisions logged with rationale (Section 5: DEV-398..DEV-407)
- [x] Tech debt items identified and routed (Section 6: D-145 folded; D-147/D-148 added)
- [x] Bug pattern warnings explicit with mitigation (Section 7: PAT-006/009/017)
- [x] Files-to-touch list is complete and grouped (Section 9)
- [x] Risks identified with concrete mitigations (Section 10)
- [x] Tasks sized to fit M envelope (8 tasks, 4 LOW + 4 MEDIUM, no HIGH)
- [x] Spec-vs-reality gaps surfaced and resolved with documented decisions (Section 2 + DEV-398/DEV-404/DEV-405)
- [x] FE work uses existing design tokens per FRONTEND.md
- [x] Tenant-scoping convention preserved (existing `apierr.TenantIDKey` extraction in Get already enforces; new fetchers receive `tenantID` where they need it)
- [x] Test plan covers happy + missing-data + boundary (PAT-006 regression)
- [x] No file written outside the explicitly-listed set; no ROUTEMAP / CLAUDE.md / step-log mutations by planner
- [x] Plan is self-contained (no "see external doc X" without that doc being cited and substantively summarized)

**Quality Gate result: PASS**

## 12. Open Uncertainties (escalation candidates if Dev hits friction)

1. **Canonical decisions doc location** — Plan assumes `docs/dev/DECISIONS.md`. If the project tracks decisions inside per-story docs only, T8 should append to `FIX-242-step-log.txt` instead. Dev verifies on first read.
2. **Policy DSL parsing for `data_cap`** — If DSL evaluator already exposes a "limits resolver" function, prefer it over hand-rolled rule iteration in T3. Quick `grep -rn 'ResolveLimits\|EffectiveQuota' internal/policy/` will tell.
3. **`cdrStore` vs `aggregates.Aggregates` injection** — Plan defaults to direct `cdrStore` injection for simplicity; if Aggregates is mandated by FIX-208 invariants for any cross-surface number, switch to Aggregates in T3. Dev decides per the FIX-208 store-direct-call audit pattern.
4. **D-145 SIM-detail tooltip** — If `sims/detail.tsx` does not already fetch the SIM with policy enrichment, the D-145 fold-in requires a small SIM DTO extension too. Out of envelope if the SIM DTO is not already enriched; in that case, defer D-145 to its own follow-up and mark FIX-242 D-145 fold-in as DEFERRED.

---

**Plan ready for Developer.** Hand off via amil orchestrator.
