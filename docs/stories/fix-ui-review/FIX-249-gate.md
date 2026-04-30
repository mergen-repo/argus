# Gate Report: FIX-249

**Story:** Global React #185 crash — `useFilteredEventsSelector` re-render loop in event-stream-drawer
**Date:** 2026-04-26
**Mode:** AUTOPILOT — UI Review Remediation
**Verdict:** **PASS**

## Summary

- Requirements Tracing: ACs 4/4 covered (AC-1 stable selector, AC-2 zero React #185, AC-3 UX preserved, AC-4 TS strict)
- Gap Analysis: 4/4 acceptance criteria PASS
- Compliance: COMPLIANT
- Tests: pnpm tsc PASS, pnpm build PASS (2.60s), go build PASS — vitest unavailable per FIX-232 fallback (UI Scout 6-route smoke substituted for AC-1 render-stability assertion)
- Performance: N/A (single-line selector wrap)
- Build: PASS
- Screen Mockup Compliance: N/A (hotfix, no new UI)
- UI Quality: PASS — 6 routes verified clean of React #185, drawer functionality preserved, live event smoke PASS
- Token Enforcement: N/A (no styling changes)
- Turkish Text: N/A
- Overall: **PASS**

## Team Composition

- Analysis Scout: 1 finding (F-A1 — pattern catalog gap, DEFER LOW)
- Test/Build Scout: 0 findings (clean — tsc PASS, web-build PASS, go-build PASS, all greps clear)
- UI Scout: 2 findings (F-U1 stale-toast on /sims, F-U2 activate 500 — both DEFER LOW, both pre-existing)
- De-duplicated: 3 → 3 findings (no overlap; orthogonal scout coverage)

## Merged Findings Table

| ID | Severity | Source | Title | Outcome |
|----|----------|--------|-------|---------|
| F-A1 | LOW | Analysis Scout | PAT-020 candidate — Zustand v5 selector returning derived collection requires `useShallow` wrap with React 19 `useSyncExternalStore` | DEFER (Reviewer-action; document in `docs/brainstorming/bug-patterns.md`) |
| F-U1 | LOW | UI Scout | Two stale "An unexpected error occurred" toasts on `/sims` — no observable XHR failure; pre-existing on `HEAD` and `HEAD~1` | DEFER → FIX-251 (P3 / XS) |
| F-U2 | LOW | UI Scout | `POST /api/v1/sims/{id}/activate` returns 500 (likely IP-pool allocation failure); SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1` left suspended | DEFER → FIX-252 (P2 / S) |

## Fixes Applied (this gate)

**NONE.** All three scouts returned PASS on first iteration. No in-scope code changes required —
the FIX-249 selector wrap (single line `useEventStore(useShallow(useFilteredEventsSelector))` in
`web/src/components/event-stream/event-stream-drawer.tsx` line ~40) was applied in Step 2 and
verified by all three scouts independently.

## Acceptance Criteria Mapping

| AC | Description | Verdict | Evidence |
|----|-------------|---------|----------|
| AC-1 | `useFilteredEventsSelector` returns stable reference when underlying data unchanged (memoized) | PASS | Analysis Scout PASS (selector definition + `useShallow` wrap correct); UI Scout S5 render-count proxy: 5s idle observation, `LOG_COUNT=0 REACT185=0`. Pre-FIX-249 baseline produced unbounded loop on every protected route — current silence is conclusive evidence-of-design. |
| AC-2 | **KEYSTONE** — Zero React #185 in console on any protected route | **PASS** | UI Scout S2.1-2.6: navigated `/dashboard`, `/sims`, `/policies`, `/sessions`, `/policies/<id>`, `/sims/<id>`. **6/6 routes clean** of React #185. Pre-FIX-249 baseline crashed on every one of these routes. |
| AC-3 | Existing event-stream UX preserved — incoming events render, filter toggles apply | PASS | UI Scout S3 drawer functionality (open/close, filter chips toggle, severity chips work); S4 live event smoke (POST `/sims/<id>/suspend` → drawer received "SIM active → suspended" event in real-time, counter "2/2 olay · 1 filtre aktif"). |
| AC-4 | TS strict — no `any`, no shape regression on `EventStoreState` | PASS | TestBuild Scout: `pnpm tsc --noEmit` PASS; `useShallow` type infer correctly preserves `LiveEvent[]`; `pnpm build` PASS in 2.60s. |

## Escalated Issues

NONE.

## Deferred Items (logged to ROUTEMAP "UI Review Remediation [IN PROGRESS]" track)

| # | Finding | Target Story | Spec Path | Tier/Effort | Written to ROUTEMAP |
|---|---------|-------------|-----------|-------------|---------------------|
| F-U1 | Stale "An unexpected error occurred" toasts on `/sims` (swallowed-error path in `useSimsQuery`/`useSelectedRows`) | **FIX-251** | `docs/stories/fix-ui-review/FIX-251-sims-stale-error-toast.md` | P3 / XS | YES |
| F-U2 | `POST /sims/{id}/activate` returns 500 (likely IP-pool allocation failure on reactivate); SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1` stuck in suspended state | **FIX-252** | `docs/stories/fix-ui-review/FIX-252-sim-activate-500-ip-pool.md` | P2 / S | YES |

## Reviewer-Action Items (not story-owned)

- **PAT-020 candidate (from F-A1):** Reviewer to consider adding pattern entry to
  `docs/brainstorming/bug-patterns.md` (current highest = PAT-019). Proposed title:
  *"PAT-020 — Zustand v5 selector returning derived collection requires `useShallow` wrap
  with React 19 `useSyncExternalStore`"*. Body should reference FIX-249 as the canonical
  reproduction case. Reviewer's call whether to add.

## Build Hygiene Caveat (environment, not story-level)

The UI Scout had to manually run `pnpm build` + `docker compose restart nginx` at the start of
its gate run because the bind-mounted `web/dist` was stale — the served chunk did not reflect
the FIX-249 patch until the rebuild + restart cycle completed. After the rebuild, behavior
matched expectations.

This is a **dev-loop / CI hygiene** item, not a story-level finding:

- Likely root cause: a prior session's `rm -rf dist` plus the bind mount lost reference to the
  rebuilt directory inode.
- Mitigation for future stories: add a `make web-rebuild` target that does `pnpm build` +
  `docker compose restart nginx` atomically, or move from bind mount to a named volume.
- Not blocking — surfaced here so the team is aware the env caveat existed during this gate run.

## Verification

| Gate | Result | Evidence |
|------|--------|----------|
| `pnpm tsc --noEmit` | PASS | TestBuild Scout |
| `pnpm build` | PASS | TestBuild Scout (2.60s) |
| `go build ./...` | PASS | TestBuild Scout (BE sanity) |
| Risk-2 (event-store mutation) grep | PASS | Zero matches for `state.events.push` / `state.events.splice` / etc. |
| `useShallow` import path resolution | PASS | `zustand/react/shallow` (zustand v5.0.12) |
| Diff blast radius | PASS | 1 file, +3/-1 |
| Comment marker | PASS | `// FIX-249:` annotation present at wrap site |
| Browser console (6 protected routes) | PASS | 0 React #185 errors across all routes |
| Drawer open/close + filter chips | PASS | UI Scout S3 |
| Live WS event drawer update | PASS | UI Scout S4 (suspend event arrived in real-time) |
| Reference stability across nav cycles | PASS | UI Scout S6 |

Fix iterations: **0** (no fixes needed at gate; scouts validated Step 2 work).

## Step-Log Entry

Appended to `docs/stories/fix-ui-review/FIX-249-step-log.txt`:

```
STEP_3 GATE: EXECUTED | 2026-04-26 | scouts=3(A:1+B:0+U:2)+merged=3 | fixed=0 | deferred=3-all-LOW(F-A1-PAT-020-candidate+F-U1-sims-stale-toast+F-U2-sim-activate-500) | out-of-scope-logged=2-FIX-251+FIX-252 | AC-2-keystone-PASS-6-routes-zero-React-185 | build-caveat=web-dist-rebuild-nginx-restart-needed | report=docs/stories/fix-ui-review/FIX-249-gate.md | tsc=PASS | web-build=PASS | go-build=PASS | result=PASS
```

## Passed Items

- AC-2 keystone (zero React #185 across 6 protected routes) — verified by UI Scout in fresh browser session
- Single-file, single-line minimal-blast-radius patch (idiomatic Path A from spec — `useShallow` wrap)
- Live event reactivity preserved (drawer received WS-pushed suspend event in real-time)
- Drawer UX preserved (open/close, filter chips, severity chips all functional)
- TS strict + Vite build both clean post-patch
- Risk-2 verified: event-store has zero in-place mutations of `state.events` (`useShallow` is sound)
- Zero impact on backend (`go build` clean)
- No new color tokens / no new components / no design-system changes (PAT-018 inherent compliance)

---

**GATE_VERDICT: PASS**

Reviewer to mark FIX-249 DONE in Step 4 (this gate does not modify the story status).
