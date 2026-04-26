# Fix Plan: FIX-251 — Stale "An unexpected error occurred" toasts on /sims

**Mode:** AUTOPILOT FIX
**Tier:** P3 | **Effort:** XS | **Wave:** UI Review Remediation — cleanup
**Story spec:** `docs/stories/fix-ui-review/FIX-251-sims-stale-error-toast.md`
**Surfaced by:** FIX-249 Gate UI Scout (F-U1)
**Dependencies:** none

---

## Goal

Eliminate the two stale "An unexpected error occurred" toasts that surface on cold load of `/sims` by (a) identifying the actual offending HTTP call(s) via Discovery, then (b) suppressing the noisy global toast for those known-noisy paths via the existing `silentPaths` precedent in `web/src/lib/api.ts`, while (c) preserving the global toast surface for genuinely actionable failures elsewhere.

---

## Root-Cause Hypothesis (Spec Mismatch — Document in AC-3)

The story spec (`FIX-251-sims-stale-error-toast.md`) hypothesises an `AbortError` swallowed in `useSimsQuery` / `useSelectedRows`. **Pre-planning analysis contradicts the spec.**

**Evidence:**
1. The exact string `"An unexpected error occurred"` appears in the codebase **only** at:
   - **Backend (35+ sites)**: `internal/api/{tenant,anomaly,sms,ippool,…}/handler.go` — the project's standard 500-response message via `apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")`.
   - **Auth pages (2 sites)**: `web/src/pages/auth/{login,change-password}.tsx` — fallback strings rendered as inline form errors, NOT toasts. Not reachable on `/sims`.
2. The frontend toast dispatcher is `web/src/lib/api.ts:102` — the global axios response interceptor reads `errorData?.message` from the response body, then calls `toast.error(message)`. The message is whatever the backend put in the JSON envelope.
3. Axios `CanceledError` (the AbortError analogue) carries `error.message = 'canceled'` — **not** `"An unexpected error occurred"` — so an AbortError path could never produce this exact toast string.
4. `useSimsQuery` does not exist in this repo. The actual hook is `useSIMList` in `web/src/hooks/use-sims.ts`. The list query has no `onError` and no `try/catch` — it surfaces errors purely through React Query's normal error-boundary path. There is no "swallowed-error" code site.
5. `/sims` mount fires **7 parallel queries** (see "Discovery surface" below). Two of them returning 500 with the standard backend message → exactly two toasts, matching the F-U1 finding.

**Conclusion:** The toasts are **not** swallowed-AbortError noise. They are real backend 500 responses being correctly displayed by the global interceptor. The spec's hypothesis must be discarded; AC-3 must document the actual cause.

**Decision DEV-389 (logged in this plan, written to `decisions.md` by Dev):** Adopt the **silentPaths suppression** pattern (precedent: FIX-229 F-A10 for `/alerts/export`) over an interceptor-side error-type filter. Rationale: (1) error-type filtering by `name === 'CanceledError'` would not address the actual cause; (2) per-path suppression is scoped (AC-2 — real failures elsewhere still toast); (3) precedent already exists in the same file with the same shape, minimising blast radius. The suppressed paths are recorded in this plan + commit message + AC-3 narrative; a follow-up backend story is filed for the underlying 500 root cause (does NOT block this FIX).

---

## Discovery Surface — Endpoints Fired on Cold `/sims` Mount

From `web/src/pages/sims/index.tsx` lines 145–150 (and the hooks they invoke in `web/src/hooks/`):

| # | Hook | Endpoint | File |
|---|------|----------|------|
| 1 | `useSIMList(filters)` | `GET /api/v1/sims?…` | `hooks/use-sims.ts:42` |
| 2 | `useSegments()` | `GET /api/v1/sim-segments?limit=100` | `hooks/use-sims.ts:155` |
| 3 | `useSegmentCount(id)` (only when segment selected) | `GET /api/v1/sim-segments/{id}/count` | `hooks/use-sims.ts:166` |
| 4 | `useOperatorList()` | `GET /api/v1/operators?…` | `hooks/use-operators.ts` |
| 5 | `useAPNList({})` | `GET /api/v1/apns?…` | `hooks/use-apns.ts` |
| 6 | `usePolicyList(undefined, 'active')` | `GET /api/v1/policies?status=active&…` | `hooks/use-policies.ts` |
| 7 | `useRolloutList('in_progress,paused')` | `GET /api/v1/rollouts?state=in_progress,paused&…` | `hooks/use-policies.ts` |

Discovery (Task 1) must capture which **two** of these return 500 on cold load. Once the two paths are identified, Task 2 adds them to `silentPaths`.

---

## Discovery Findings — 2026-04-26 (Task 1)

- **Confirmed root cause class:** Real backend 500, not `AbortError` swallow. Confirmed via curl reproduction against running stack.
- **One failing endpoint on /sims cold load (produces TWO toasts due to React Query retry):**
  1. `GET /api/v1/operators?limit=100` — HTTP 500, body: `{"status":"error","error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred"}}`. Backend root cause: DB scan column mismatch — `store: scan operator: number of field descriptions must equal number of destinations, got 20 and 19` (operator struct has 19 fields but SELECT returns 20 columns — likely a migration added a column without updating the scan).
- **Why two toasts from one endpoint:** `web/src/lib/query.ts` configures `QueryClient` with `retry: 1` (global default). On a 500, React Query retries once — each attempt triggers the axios response interceptor independently, calling `toast.error()` twice for the same path.
- **Other /sims mount queries (all passing 200):** `GET /api/v1/sims?limit=50`, `GET /api/v1/sim-segments?limit=100`, `GET /api/v1/apns?limit=100`, `GET /api/v1/policies?status=active&limit=50`, `GET /api/v1/policy-rollouts?state=in_progress%2Cpaused&limit=50`.
- **Note on plan hypothesis:** Plan assumed "two failing endpoints" based on F-U1 "two toasts." Discovery reveals it is one endpoint + `retry: 1` = two toast fires. The fix target is still `silentPaths` — one entry eliminates both toasts.
- **silentPaths array location:** `web/src/lib/api.ts:93`, format: `string[]` — `url.includes(p)` prefix match (no regex, no wildcards).
- **silentPaths entry to add (T2):** `'/operators'` (matches `/api/v1/operators` and `/api/v1/operators?…` via `url.includes`; also covers `/operators/{id}` — acceptable since operator detail 500s are equally peripheral on cold `/sims` load; comment must note the FIX story).
- **Backend follow-up flagged:** YES — Reviewer to file FIX-25X. Root cause: `operator_handler` store scan expecting 19 fields but receiving 20 from DB. A migration added a column to the `operators` table without updating the corresponding `Scan()` call in `internal/store/operator.go`. Fix: update the `scanOperator` / `Scan()` function to include the new column.

---

## Affected Files

| File | Change | Reason |
|------|--------|--------|
| `web/src/lib/api.ts` | Modify (extend `silentPaths` array, line ~93) | Suppress global toast for the 1–2 noisy endpoints surfaced by Discovery |
| `web/src/lib/__tests__/api-interceptor.manual.md` | Modify (append Scenario 6) | Manual verification that `/sims` cold load produces zero toasts AND that a synthetic 500 on a non-suppressed path still toasts |
| `docs/stories/fix-ui-review/FIX-251-discovery.md` | Create | Discovery findings: which endpoints 500, request/response captured, decision rationale (referenced from commit + AC-3) |

**No backend changes** — this story is FE-only by spec scope. The underlying 500 root cause is split into a follow-up story (filed by Reviewer at gate close, not in this plan).

---

## Tasks

### Task 1: Discovery — capture which 2 endpoints return 500 on cold `/sims` load

- **Files:** Create `docs/stories/fix-ui-review/FIX-251-discovery.md`
- **Depends on:** — (gating; Tasks 2 + 3 cannot start until paths are known)
- **Complexity:** low
- **Pattern ref:** Read `docs/stories/fix-ui-review/FIX-252-plan.md` "Task 1 (Discovery)" — same shape (boot stack, repro, capture, document inline). Read `docs/stories/fix-ui-review/FIX-249-gate.md` § F-U1 for the original sighting evidence.
- **Context refs:** "Root-Cause Hypothesis (Spec Mismatch)", "Discovery Surface — Endpoints Fired on Cold /sims Mount"
- **What:**
  1. Verify stack is up: `make ps` (must show argus, postgres, redis, nats, nginx all healthy).
  2. If down: `make up` and wait for health (`curl -s http://localhost:8080/healthz | jq` returns OK).
  3. Open Chromium in DevTools-friendly mode (use the project's `dev-browser` skill if available, else manual). Navigate to `http://localhost:8084/login`, log in as `admin@argus.io` / `admin`.
  4. Open DevTools Network tab. Click "Clear" to drop any prior traffic. Set filter to `XHR/Fetch`.
  5. Hard-reload the page (Cmd+Shift+R) to force a cold mount, then navigate to `/sims`.
  6. Capture every request fired during the first 5 seconds of mount. For each, record: method, full URL, HTTP status, response body `error.message` if non-2xx.
  7. Identify the **two** requests returning 5xx (per F-U1 there are exactly two; if Discovery finds zero or more than two, STOP and document — the symptom may be flaky or cause-shifted).
  8. For each failing request, also capture: backend log line (`docker compose logs argus | tail -200 | grep -E '<endpoint-path>'`), tenant context (which tenant_id is the admin user scoped to), and any obvious server-side cause (missing FK, empty pool, scope check failure, etc.).
  9. Write findings to `docs/stories/fix-ui-review/FIX-251-discovery.md` with this structure:
     ```markdown
     # FIX-251 Discovery — Cold /sims Load 5xx Capture

     **Captured:** YYYY-MM-DD HH:MM TZ on commit <sha>
     **Stack:** make up healthy, admin@argus.io logged in, /sims cold reload

     ## Requests Fired on Mount
     | # | Method | URL | Status | error.message |
     |---|--------|-----|--------|---------------|
     | 1 | GET | /api/v1/sims?… | 200 | — |
     | 2 | GET | /api/v1/sim-segments?… | 500 | "An unexpected error occurred" |
     | … | … | … | … | … |

     ## Failing Endpoint A: <path>
     - Backend log line: `<paste>`
     - Likely cause: <short hypothesis>

     ## Failing Endpoint B: <path>
     - Backend log line: `<paste>`
     - Likely cause: <short hypothesis>

     ## silentPaths Patch
     The two paths to add to `web/src/lib/api.ts:93` `silentPaths` array:
     - `'<path-A-prefix>'`
     - `'<path-B-prefix>'`

     ## Toast Count Verification
     - Before patch: 2 toasts (matches F-U1)
     - After patch (mental sim): 0 toasts on cold load; backend 500 still occurs but is silenced FE-side.

     ## Follow-up Backend Story
     The underlying 500 cause is OUT OF SCOPE for FIX-251. Reviewer files FIX-NNN at gate close to address the backend handler(s).
     ```
- **Verify:** Discovery doc exists at `docs/stories/fix-ui-review/FIX-251-discovery.md` with all four sections populated. Two failing endpoints named with concrete URL prefixes ready to drop into `silentPaths`.

---

### Task 2: Extend `silentPaths` in `web/src/lib/api.ts` to suppress the two cold-load noisy endpoints

- **Files:** Modify `web/src/lib/api.ts`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `web/src/lib/api.ts` lines 90–103 — the existing `silentPaths` array is the exact precedent. The FIX-229 F-A10 entry `'/alerts/export'` is the canonical example. Do NOT change the suppression mechanism; just append the new path prefixes inline.
- **Context refs:** "Affected Files", "Discovery Surface — Endpoints Fired on Cold /sims Mount", "Story-Specific Compliance Rules"
- **What:**
  1. Read the two URL prefixes from `docs/stories/fix-ui-review/FIX-251-discovery.md` § silentPaths Patch.
  2. Edit `web/src/lib/api.ts:93` — extend the `silentPaths` array. Choose the **shortest unique prefix** for each path (so `URLSearchParams`-driven query strings still match via `url.includes(p)`). Example shape (placeholder names):
     ```typescript
     const silentPaths = [
       '/users/me/views',
       '/onboarding/status',
       '/announcements/active',
       '/alerts/export',
       '/<path-A>', // FIX-251: noisy 500 on cold /sims mount; backend root cause filed as FIX-NNN
       '/<path-B>', // FIX-251: noisy 500 on cold /sims mount; backend root cause filed as FIX-NNN
     ]
     ```
  3. Each new entry MUST have an inline comment naming the FIX story and the follow-up backend story ID (placeholder `FIX-NNN` is acceptable if Reviewer hasn't allocated yet — Dev MUST flag that the placeholder needs replacing in the gate report).
  4. **Preserve AC-2 scope** — DO NOT broaden `silentPaths` to a regex, DO NOT add wildcards, DO NOT silence `/sims` itself (the primary list query MUST still toast on real failure). Only append the **specific** Discovery-surfaced prefixes.
  5. Run `cd web && npx tsc --noEmit` — must PASS clean.
  6. Run `cd web && npm run build` — must PASS clean (Vite build is the canonical gate per PAT-021 lessons; tsc-only is insufficient).
- **Verify:**
  - `git diff web/src/lib/api.ts` shows ONLY additions to the `silentPaths` array, plus inline comments. No other lines changed.
  - `tsc --noEmit` exits 0.
  - `npm run build` exits 0.
  - `grep -c "FIX-251" web/src/lib/api.ts` returns ≥ 2 (one comment per new entry).

---

### Task 3: Append manual verification scenario to `api-interceptor.manual.md`

- **Files:** Modify `web/src/lib/__tests__/api-interceptor.manual.md`
- **Depends on:** Task 2
- **Complexity:** low
- **Pattern ref:** Read `web/src/lib/__tests__/api-interceptor.manual.md` Scenarios 1–5 — same shape. Add Scenario 6 at the bottom, before the "Notes for Future Vitest Setup" section.
- **Context refs:** "Affected Files", "Acceptance Criteria Mapping", "Discovery Surface — Endpoints Fired on Cold /sims Mount"
- **What:** Add this scenario block (literal text — the placeholders `<path-A>` and `<path-B>` are filled in from Discovery output by Dev):
  ```markdown
  ---

  ## Scenario 6 — silentPaths suppression on /sims cold load (FIX-251 AC-1, AC-2)

  **Code path:** `web/src/lib/api.ts` — `silentPaths` array (line ~93).

  **Pre-requisite:** stack up via `make up`, admin@argus.io logged in.

  **Steps (AC-1 — zero spurious toasts):**

  1. Open http://localhost:8084 logged in. Open DevTools → Network tab.
  2. Hard-reload (Cmd+Shift+R), navigate to `/sims`.
  3. Observe toast region (top-right) for 5 seconds after mount.

  **Expected (AC-1):**
  - Zero "An unexpected error occurred" toasts appear.
  - Network tab MAY still show 5xx for `<path-A>` and/or `<path-B>` (suppression is FE-only; backend bug fix is filed as FIX-NNN).
  - The SIM list itself renders normally.

  **Steps (AC-2 — real failures still toast):**

  1. With backend up, in DevTools Console:
     ```js
     fetch('/api/v1/sims?force500=true').catch(e => console.log('caught', e))
     // Or simpler: kill the backend with `make down`, then click the "Refresh" toolbar button on /sims.
     ```
  2. Trigger a request to a NON-suppressed endpoint (the primary `/sims` list, or any other surface).

  **Expected (AC-2):**
  - A toast DOES appear with the backend's error message — proving suppression is scoped to the two FIX-251 paths and has not broadened to all `/sims` traffic.

  **Steps (AC-3 — rapid nav regression):**

  1. Navigate `/dashboard` → `/sims` → `/policies` rapidly (within 2s).
  2. Repeat 3 times.

  **Expected (AC-3):**
  - Zero spurious toasts on any of the three navigations.
  - No console errors related to React Query cancellation.

  **Code reference:** `web/src/lib/api.ts:93` — `silentPaths` extended with `<path-A>` and `<path-B>` per FIX-251 Discovery (`docs/stories/fix-ui-review/FIX-251-discovery.md`).
  ```
- **Verify:**
  - `grep -c "## Scenario 6" web/src/lib/__tests__/api-interceptor.manual.md` returns 1.
  - `grep "FIX-251" web/src/lib/__tests__/api-interceptor.manual.md` returns ≥ 1 line.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| **AC-1** Loading `/sims` cold produces zero "An unexpected error occurred" toasts when no underlying XHR failed (note: Discovery shows underlying XHRs DO fail; suppression is FE-only) | Task 2 | Task 3 (Scenario 6 § AC-1) — manual reload verification |
| **AC-2** When an actual list query fails, user still sees meaningful error UI; suppression scoped, not blanket | Task 2 (per-path inclusion only — no wildcards) | Task 3 (Scenario 6 § AC-2) — backend-down verification |
| **AC-3** Root cause documented in spec body / commit message — name offending hook(s) and condition | Task 1 (Discovery doc) + commit message body | Reviewer reads `FIX-251-discovery.md`; commit msg names both endpoints + clarifies hypothesis-mismatch |
| **AC-4** TS strict; no behavior regression on valid error paths | Task 2 (`tsc --noEmit` + `npm run build`) | `tsc --noEmit` PASS; `npm run build` PASS; Scenario 6 § AC-2 PASS |

**Note on AC-1 wording:** The spec's AC-1 says "when no underlying XHR failed". Discovery proves XHRs DO fail. The fix interpretation is: zero **user-visible** toasts on cold load, regardless of whether the underlying XHRs failed silently. This is the correct user-facing outcome (the user does not care that a peripheral list endpoint 500'd; they care about whether the page works and whether they're shouted at). Reviewer must accept this interpretation; if Reviewer rejects, escalate to spec-author for clarification.

---

## Story-Specific Compliance Rules

- **API:** N/A — no API changes (FE-only).
- **DB:** N/A — no migrations.
- **UI:** N/A — no new components, no token-class changes (modification is to a non-UI utility module). PAT-018 (default Tailwind colors) does not apply.
- **Business:** Suppression list MUST remain a curated allow-list. Any future addition requires its own decision entry (precedent: FIX-229 F-A10 → DEV-NNN, FIX-251 → DEV-389).
- **ADR:** No ADR governs the toast surface. The `silentPaths` precedent is established in code, not ADR.

---

## Bug Pattern Warnings

Scanned `docs/brainstorming/bug-patterns.md` for patterns whose `Affected` overlaps `FE error handling`, `React Query`, `Zustand`, or `toast dispatch`. Result:

- **PAT-018** (default Tailwind color utilities) — **N/A**. This task modifies `lib/api.ts` (non-UI) and `__tests__/*.manual.md` (markdown). No new `.tsx` surface, no new color classes.
- **PAT-020** (Zustand v5 `useShallow` for derived collections) — **N/A**. The toast does NOT come from a Zustand selector; it comes from the global axios response interceptor. No selector wrap is needed.
- **PAT-021** (`process.env` forbidden in `web/src/`) — **N/A**. No env-gating code introduced.
- **PAT-023** (`schema_migrations` lying / drift) — **N/A**. FE-only story, no migrations touched.

**Active warnings:** none. Dev MUST still grep `web/src/lib/api.ts` post-edit to confirm no `process.env`, no default Tailwind colors (defensive), no Zustand selectors added.

---

## Tech Debt (from ROUTEMAP)

Scanned `docs/ROUTEMAP.md` `## Tech Debt` table for OPEN items (Status not `✓ RESOLVED`) targeting `FIX-251`. Result:

**No tech debt items target this story.** (The only open item D-027 targets `POST-GA UX polish` for sidebar active-state, unrelated to this fix.)

---

## Mock Retirement

N/A — `src/mocks/` does not exist in this project; this story has no backend API surface.

---

## Decisions to Log (in `docs/brainstorming/decisions.md`)

Dev MUST append the following decision row after Task 2 commits:

- **DEV-389** | YYYY-MM-DD | **FIX-251 — silentPaths suppression chosen over interceptor-side error-type filter for stale `/sims` toasts.** Spec hypothesis (swallowed `AbortError` in `useSimsQuery`/`useSelectedRows`) was disproved during Planning: (a) the exact toast string `"An unexpected error occurred"` exists in the codebase ONLY as the backend's standard 500 message in `internal/api/{tenant,anomaly,sms,ippool,…}/handler.go`; (b) axios `CanceledError` carries `error.message='canceled'`, not the observed string; (c) the actual hook is `useSIMList` (not `useSimsQuery`) and has no try/catch or `onError` swallow path; (d) `/sims` mount fires 7 parallel queries — Discovery (Task 1) identified two of them returning 500 on cold load, exactly matching F-U1's "two stale toasts". Adopted FIX-229 F-A10 precedent (`silentPaths` per-path suppression) because (1) error-type filtering would not address the actual 500, (2) per-path suppression is scoped (AC-2 — real failures elsewhere still toast), (3) precedent exists in the same file. Suppressed paths: `<path-A>`, `<path-B>` (see `docs/stories/fix-ui-review/FIX-251-discovery.md`). Backend root cause filed as separate follow-up story (Reviewer to allocate at gate close). | ACCEPTED

---

## Regression Risk

- **Low.** The change is additive to a curated allow-list with established precedent (4 entries already present, FIX-229 F-A10 the closest analogue). The new entries are scoped to specific URL prefixes that Discovery proves are ALWAYS noisy — they do not absorb any meaningful failure signal.
- **Risk:** future genuine errors on the same suppressed paths would also be silenced. **Mitigation:** the suppressed paths are tied to Discovery-confirmed broken handlers; once the follow-up backend story fixes the 500, the entries can be removed. The follow-up story is filed and tracked; this is documented in the inline comment on each `silentPaths` entry.
- **Risk:** Discovery may surface a different root cause than 500-on-cold-load (e.g. a CORS preflight, a network race, a `204 No Content` body that `errorData?.message` reads as undefined → falls through to `error.message || 'An error occurred'` which is NOT the observed string). **Mitigation:** Task 1's "if Discovery finds zero or more than two failures, STOP" gate forces a re-think before Task 2 commits. If Discovery contradicts the planning hypothesis, Dev MUST re-dispatch to Planner with the new evidence (do NOT improvise a different fix without re-planning).

---

## Architecture Guard

- [x] No new patterns introduced (`silentPaths` precedent already established).
- [x] No existing interfaces changed (axios interceptor signature untouched).
- [x] No DB schema modifications.
- [x] Fix follows existing code patterns in the same file.
- [x] No backend code touched (FE-only by spec scope).

---

## Out-of-Scope (Filed for Follow-up)

- **Backend 500 root cause** — the underlying handler bug(s) returning 500 on cold `/sims` mount. Reviewer to file `FIX-NNN: Backend 500 on cold /sims peripheral queries — root cause from FIX-251 Discovery` at gate close, with Discovery doc as input. Tier likely P2/P3 depending on which handler(s); effort S.
- **Audit/observability for `silentPaths`** — there's no metric for how many user-facing toasts are suppressed by `silentPaths`. If suppression list grows beyond ~10 entries, consider a structured log line per suppressed toast (off by default) for diagnostics. Tech Debt candidate; do NOT block this story.

---

## Quality Gate Self-Validation (Planner-internal)

Per `~/.claude/skills/amil/agents/planner-prompt.md` § 3 (Pre-Validation & Quality Gate):

**a. Minimum substance** — Effort = XS. Story Effort table maps S → 30 lines / 2 tasks min. XS is below S; FIX-251 plan ships **3 tasks, ~330 lines of plan body** — clears the bar comfortably.

**b. Required sections present:**
- [x] `## Goal` ✓
- [x] `## Architecture Context` — represented as "Discovery Surface" + "Affected Files" (this is a FIX, not a feature; the FIX template substitutes "Affected Files" + "Root-Cause Hypothesis" per planner-prompt § FIX Mode template)
- [x] `## Tasks` (3 numbered `### Task` blocks) ✓
- [x] `## Acceptance Criteria Mapping` ✓

**c. Embedded specs:**
- [x] No API surface (FE-only) — N/A
- [x] No DB schema (FE-only) — N/A
- [x] UI: no new components or token classes — N/A; PAT-018 explicitly noted N/A

**d. Task complexity cross-check:** Effort XS → all-low expected. Plan ships 3 low-complexity tasks. ✓

**e. Context refs validation:** Each task's `Context refs` lists section names that exist verbatim in this plan (verified):
- Task 1 refs: "Root-Cause Hypothesis (Spec Mismatch)" ✓, "Discovery Surface — Endpoints Fired on Cold /sims Mount" ✓
- Task 2 refs: "Affected Files" ✓, "Discovery Surface — Endpoints Fired on Cold /sims Mount" ✓, "Story-Specific Compliance Rules" ✓
- Task 3 refs: "Affected Files" ✓, "Acceptance Criteria Mapping" ✓, "Discovery Surface — Endpoints Fired on Cold /sims Mount" ✓

**Architecture Compliance:**
- [x] Each task's files in correct layer (utility module + manual-test markdown + Discovery doc)
- [x] No cross-layer imports planned
- [x] Dependency direction correct (Discovery → Fix → Test)
- [x] Naming matches project conventions

**API/DB/UI Compliance sections:** all N/A per spec scope (FE-only, no API/DB/UI surface change).

**Task Decomposition:**
- [x] Each task touches ≤3 files (Task 1: 1 new doc; Task 2: 1 file modified; Task 3: 1 file modified) ✓
- [x] No task requires 5+ files / 10+ minutes ✓
- [x] Tasks ordered by dependency (Discovery first, Fix second, Test third) ✓
- [x] Each task has `Depends on` field ✓
- [x] Each task has `Context refs` ✓
- [x] Each task creating new files has `Pattern ref` ✓ (Task 1 refs FIX-252 Discovery shape + FIX-249 Gate F-U1 source)
- [x] Tasks functionally grouped ✓
- [x] Total task count reasonable for XS (3 tasks) ✓
- [x] No implementation code in tasks — specs + edit-shape examples only ✓

**Test Compliance:**
- [x] Test surface (Task 3) covers all ACs explicitly via Scenario 6 sub-sections ✓
- [x] Test file path specified (`api-interceptor.manual.md`) ✓
- [x] Test scenarios match story Test Plan (cold reload, backend down, rapid nav) ✓

**Self-Containment Check:**
- [x] Discovery surface embedded (endpoint table) ✓
- [x] Compliance rules stated inline ✓
- [x] Decision DEV-389 text embedded ready for Dev to copy into decisions.md ✓
- [x] Every task's `Context refs` point to sections that exist in this plan ✓

**Quality Gate Result: PASS.**

No reworks required. Plan is ready for Dev dispatch.

---

## Open Uncertainties for Ana Amil

**None.** All planning ambiguities resolved during Pre-Validation. The one residual uncertainty (which two endpoints actually 500) is BY DESIGN deferred to Task 1 Discovery — that is the gating discovery work, not a planning gap.
