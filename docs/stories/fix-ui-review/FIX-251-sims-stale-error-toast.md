# FIX-251 — Stale "An unexpected error occurred" toasts on /sims

**Tier:** P3 | **Effort:** XS | **Wave:** UI Review Remediation — cleanup
**Dependencies:** none
**Surfaced by:** FIX-249 Gate UI Scout (F-U1)

## Problem Statement

When navigating to `/sims`, two stale toast notifications "An unexpected error occurred" surface
without any observable XHR failure in the network panel. The list itself loads correctly and the
errors disappear after the toast TTL elapses. UI Scout confirmed the defect is **pre-existing**
on `HEAD` and `HEAD~1` (independent of FIX-249 selector patch).

The most likely culprit is a swallowed-error path in either `useSimsQuery` or
`useSelectedRows` — a try/catch (or `onError` callback) that fires `toast.error('An unexpected
error occurred')` even when the underlying error is benign (e.g. `AbortController` cancellation
during route mount/unmount, or React-Query refetch race during fast nav).

This is cosmetic — it does not block the user — but two error toasts greeting the user on the
SIM list page is poor signal-to-noise for the operator.

## Acceptance Criteria

- [ ] **AC-1:** Loading `/sims` from a logged-in cold start produces zero "An unexpected error
      occurred" toasts when no underlying XHR failed.
- [ ] **AC-2:** When an actual SIM-list query DOES fail (e.g. backend 500), the user still sees
      a meaningful error UI (toast or inline banner) — error suppression must be scoped, not
      blanket.
- [ ] **AC-3:** Root cause documented in spec body / commit message — name the offending hook
      and the swallowed-error condition.
- [ ] **AC-4:** TS strict; no behavior regression on valid error paths.

## Files to Touch (best-effort)

- `web/src/features/sims/hooks/useSimsQuery.ts` (or equivalent React-Query hook)
- `web/src/features/sims/hooks/useSelectedRows.ts` (or equivalent selection hook)
- Possibly `web/src/lib/toast.ts` if the global toast helper is the dispatcher
- Test file: `web/src/features/sims/__tests__/useSimsQuery.test.ts` if missing

## Investigation Steps

1. Add a temporary `console.error(err)` in every `onError` and `catch` block in the SIM-list
   path to capture which call is firing the toast.
2. Reproduce: cold-load `/sims`, observe console log + toast.
3. Identify the swallowed error type — likely `AbortError` from a cancelled request or a
   React-Query stale-fetch racing with route unmount.
4. Filter the offending error type before dispatching the user-visible toast (e.g.
   `if (err.name === 'AbortError') return;`).
5. Confirm zero toasts on cold load; confirm toasts still fire on synthetic 500.

## Risks & Regression

- Over-broadening the error filter could swallow real errors. Filter MUST be by error type
  (AbortError / cancellation), NOT by error message string.
- React-Query has built-in cancellation handling — verify the hook isn't double-handling.

## Test Plan

- [ ] Manual: cold-load `/sims` 3x; zero spurious toasts each time.
- [ ] Manual: kill backend, load `/sims`; meaningful error toast appears.
- [ ] Manual: rapid nav `/dashboard → /sims → /policies`; no spurious toasts.
- [ ] Optional unit: mock `useSimsQuery` to throw `AbortError`; assert toast NOT dispatched.

## Plan Reference

Surfaced in: `docs/stories/fix-ui-review/FIX-249-gate.md` § F-U1
