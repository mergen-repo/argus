# FIX-249 — Global React #185 crash: `useFilteredEventsSelector` re-render loop

**Tier:** P0 | **Effort:** S | **Wave:** UI Review Remediation — hotfix block
**Dependencies:** none (FIX-249 is a hotfix; affects every protected route)
**Surfaced by:** FIX-233 Gate UI Scout (verified pre-existing on HEAD and HEAD~1 before FIX-233 changes were applied)

## Problem Statement

Every protected route in the SPA crashes with React `Minified React error #185` ("Maximum update depth exceeded — a component repeatedly calls setState inside componentWillUpdate / componentDidUpdate, or an unstable selector returned a new value on every render").

Root cause is in the global event-stream drawer mounted by `web/src/components/event-stream/event-stream-drawer.tsx`:

```ts
// Line ~40
const filteredEvents = useEventStore(useFilteredEventsSelector)
```

`useFilteredEventsSelector` is a Zustand selector that returns `state.events.filter(...)` — a fresh `Array` reference on every store read. Under React 19 + `useSyncExternalStore` (which Zustand v4 uses internally), an unstable selector return value triggers an immediate re-render, which re-runs the selector, which returns a new array, which triggers another re-render — the React #185 loop.

The drawer is mounted inside the global `AppShell`, so the crash bricks every authenticated page. UI Scout reproduced it on `HEAD~1` (FIX-232 commit) by stashing FIX-233 work-tree changes, confirming this is **NOT** a FIX-233 regression — it is a pre-existing defect that has been silently shipping since the drawer was wired in.

## Acceptance Criteria

- [ ] **AC-1:** `useFilteredEventsSelector` returns a stable reference when underlying data has not changed (memoized) — verified by mounting drawer and asserting render count stays bounded after store-unrelated parent renders.
- [ ] **AC-2:** No React #185 in browser console on any protected route (`/sims`, `/policies`, `/dashboard`, `/sessions`, etc.).
- [ ] **AC-3:** Existing event-stream UX preserved: incoming events still render, filter toggles still apply.
- [ ] **AC-4:** TS strict — no `any`, no shape regression on `EventStoreState`.

## Files to Touch

- `web/src/components/event-stream/event-stream-drawer.tsx` — selector consumption site
- `web/src/store/event-store.ts` (or wherever `useFilteredEventsSelector` lives) — selector definition

## Recommended Fix (one of two paths)

**Path A — `useShallow` from `zustand/shallow`** (idiomatic, minimal change):
```ts
import { useShallow } from 'zustand/shallow'
const filteredEvents = useEventStore(useShallow(useFilteredEventsSelector))
```

**Path B — split + `useMemo`** (more explicit, no extra import):
```ts
const events  = useEventStore(s => s.events)
const filters = useEventStore(s => s.filters)
const filteredEvents = useMemo(() => applyFilters(events, filters), [events, filters])
```

Path A is shorter; Path B makes the dependency contract explicit. Either acceptable.

## Risks & Regression

- The drawer is mounted globally — a buggy fix re-bricks the whole SPA. Manual smoke after fix on `/sims`, `/policies`, `/dashboard`, `/sessions` is mandatory.
- If event-store reducer mutates in place anywhere, `useShallow` won't help — verify all event-store actions return new arrays. Grep `set(state => { state.events.push` — should be zero matches.

## Test Plan

- [ ] FE unit: render drawer with mocked store; trigger 5 unrelated parent re-renders; assert selector invocation count ≤ 6 (mount + 5 re-renders, no extra cascade).
- [ ] Browser smoke: open dev server, navigate `/sims`, `/policies`, `/dashboard`, `/sessions`, `/policies/:id`, `/sims/:id`. Console must be clean of React #185.
- [ ] Live event smoke: dispatch test events via WS; drawer list updates without console warnings.

## Plan Reference

Surfaced in: `docs/stories/fix-ui-review/FIX-233-gate.md` § F-U1
