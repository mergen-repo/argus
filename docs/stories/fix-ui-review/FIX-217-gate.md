# Gate Report: FIX-217 — Timeframe Selector Pill Toggle Unification

**Date**: 2026-04-22
**Gate Team**: Lead (consolidator + fixer) + 3 scouts (Analysis, Test/Build, UI)
**Track**: UI Review Remediation (not phase-N)

---

## Summary

- Requirements Tracing: Fields 8/8 (primitive API + URL params), Endpoints n/a (FE-only), Workflows 5/5 ACs covered, Components 9/9 (primitive + hook + 5 adopted pages + 3 back-compat callers)
- Gap Analysis: 5/5 ACs pass (AC-3 URL sync verified intact against scout's stale-read alarm; see "Findings Already Resolved")
- Compliance: COMPLIANT
- Tests: DEFERRED (D-091 — web/ has no test runner); tsc = PASS, vite build = PASS
- Performance: n/a — pure FE refactor; 1 micro-opt deferred (D-095)
- Build: PASS
- Screen Mockup Compliance: 8 PASS / 3 OBSERVATION / 1 doc-polish (already shipped) per UI scout
- UI Quality: 11/11 checks assessed; no runtime blockers
- Token Enforcement: 0 hex, 0 raw-button, 0 arbitrary-px in FIX-217 scope files (pre-existing rgba LED glows in `cdrs/index.tsx:252` + `operators/detail.tsx:118-120` are out-of-scope STORY-045/FIX-214 artifacts)
- Overall: **PASS**

---

## Team Composition

- Analysis Scout: 12 findings (F-A1..F-A12)
- Test/Build Scout: 0 findings (2 out-of-scope observations)
- UI Scout: 1 NEEDS_FIX (F-U-1) + 4 observations
- Merged: F-A5 ↔ F-U-1 deduplicated → 12 unique findings

---

## Merged Findings Table (sorted by severity)

| ID | Severity | Category | File:Line | Resolution |
|----|----------|----------|-----------|------------|
| F-A1 | CRITICAL | gap | `web/src/pages/cdrs/index.tsx:148-160` | **NO_ACTION — already resolved** (scout stale-read) |
| F-A2 | HIGH | compliance | `web/src/components/ui/timeframe-selector.tsx:216-226` | **NO_ACTION — already resolved** |
| F-A3 | HIGH | gap | `web/src/components/ui/timeframe-selector.tsx:56-76, 232-233` | **NO_ACTION — already resolved** |
| F-A4 | HIGH | gap | `web/src/hooks/use-timeframe-url-sync.ts` + primitive | **NO_ACTION — already resolved** (same root cause as F-A3) |
| F-A5 / F-U-1 | MEDIUM | compliance (doc) | `docs/FRONTEND.md:247-258` | **NO_ACTION — already resolved** |
| F-A6 | MEDIUM | gap (doc) | `docs/brainstorming/decisions.md` | **FIXED** — DEV-291 appended |
| F-A12 | LOW | security | `web/src/hooks/use-timeframe-url-sync.ts:17-20` | **FIXED** — `isValidISODate` guard added |
| F-A7 | LOW | compliance | `web/src/components/ui/timeframe-selector.tsx:189` | **DEFERRED** → D-092 (FIX-24x UI polish) |
| F-A8 | LOW | compliance | `admin/api-usage.tsx:46` + `admin/delivery.tsx:89` | **DEFERRED** → D-093 (FIX-24x UI polish) |
| F-A9 | LOW | gap | `operators/detail.tsx:297-302` | **DEFERRED** → D-094 (FIX-24x UI polish) |
| F-A11 | LOW | performance | `timeframe-selector.tsx:141-148` | **DEFERRED** → D-095 (FIX-24x UI polish) |
| F-A10 | LOW | cross-track | `sla/index.tsx:252-272` | **NO_ACTION** — explicitly out of scope per Plan Scope Decision #5 |

---

## Findings Already Resolved (scout stale-read)

The Analysis Scout flagged F-A1 (CRITICAL), F-A2 (HIGH), F-A3 (HIGH), F-A4 (HIGH), and F-A5 (MEDIUM) as gaps. A diff of the current working tree against the scout's suggested fixes shows **all 5 are already implemented** — the scout appears to have read an earlier snapshot of these files. Evidence:

### F-A1 — cdrs filter-sync effect preserves tf/tf_start/tf_end

Current `web/src/pages/cdrs/index.tsx:148-160`:

```ts
useEffect(() => {
  setSearchParams((prev) => {
    const p = new URLSearchParams(prev)                // ← CLONES prev
    const FILTER_KEYS = ['operator_id', 'apn_id', 'record_type', 'rat_type'] as const
    for (const k of FILTER_KEYS) p.delete(k)
    for (const [k, v] of Object.entries(filters)) {
      if (k === 'from' || k === 'to') continue
      if (!(FILTER_KEYS as readonly string[]).includes(k)) continue
      if (v !== undefined && v !== '') p.set(k, String(v))
    }
    return p
  }, { replace: true })
}, [filters, setSearchParams])
```

`new URLSearchParams(prev)` clones the prior params; the effect only `delete`s keys in the whitelisted `FILTER_KEYS`. `tf`, `tf_start`, `tf_end` (managed by `useTimeframeUrlSync`) are preserved across any filter mutation. This matches the scout's suggested-fix block verbatim.

### F-A2 — Popover triggerRef populated via PopoverTrigger

Current `web/src/components/ui/timeframe-selector.tsx:216-226`:

```tsx
<Popover key="custom" open={popoverOpen} onOpenChange={setPopoverOpen}>
  <PopoverTrigger
    tabIndex={isActive ? 0 : currentIndex === -1 && idx === 0 ? 0 : -1}
    aria-pressed={isActive}
    ...
    className={pillClassName}
  >
    {isCustomActive ? formatCustomLabel(normalized.from, normalized.to) : opt.label}
  </PopoverTrigger>
  <PopoverContent align="end">...</PopoverContent>
</Popover>
```

`PopoverTrigger` (`web/src/components/ui/popover.tsx:54-80`) uses a `mergedRef` callback that writes to the context `triggerRef`. The outside-click handler at `popover.tsx:107` ignores clicks inside `triggerRef.current`, so the popover no longer auto-closes. Anchor positioning uses `PopoverContent`'s `alignClass` relative to the trigger's parent — consistent with project convention across other Popover callers.

### F-A3 / F-A4 — ISO↔local-datetime helpers centralized

Current `web/src/components/ui/timeframe-selector.tsx:56-76`:

```ts
function toLocalDatetimeLocal(utcISO?: string): string { /* UTC ISO → local wall-clock YYYY-MM-DDTHH:mm */ }
function fromLocalDatetimeLocal(localStr: string): string { /* datetime-local input → ISO UTC */ }
```

Reads use `toLocalDatetimeLocal(normalized.from)` / `...to` at lines 232-233; writes use `fromLocalDatetimeLocal(from/to)` at lines 173-174. Round-trip is symmetric: local-datetime input → UTC ISO → URL → read back → local wall-clock display. No timezone asymmetry remains.

### F-A5 / F-U-1 — FRONTEND.md adoption map concrete

`grep "TBD (Wave 2)" docs/FRONTEND.md` returns **0 matches**. Rows 254-258 describe each surface's actual adoption posture (`admin/api-usage` → "Canonical presets (1h/24h/7d/30d); no custom"; `cdrs/index.tsx` → "Canonical presets + Custom popover + analyst role-gate; URL `?tf=` / `?tf_start=&tf_end=`"; etc.).

---

## Fixes Applied (2)

| # | Finding | File | Change | Verified |
|---|---------|------|--------|----------|
| 1 | F-A6 | `docs/brainstorming/decisions.md` | Appended DEV-291 documenting the /cdrs URL scheme change (`?from/?to` → `?tf/?tf_start/?tf_end`) and the no-backward-compat posture per Plan Risk #1 | Grep for `DEV-291` returns 1 match |
| 2 | F-A12 | `web/src/hooks/use-timeframe-url-sync.ts:7-15, 24-28` | Added `isValidISODate` guard; `customRange` is now `null` when `tf_start`/`tf_end` are not parseable as dates | tsc PASS; build PASS |

---

## Escalated Issues

**None.** All findings were FIXABLE, already-resolved, or legitimately deferred.

---

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-092 | F-A7 — Arrow keys commit selection on focus move; should move focus only | FIX-24x (UI polish) | YES |
| D-093 | F-A8 — `window` state var shadows browser global in 2 admin pages | FIX-24x (UI polish) | YES |
| D-094 | F-A9 — HealthTimelineTab uses numeric-string values (`'6'`/`'24'`/...) instead of canonical presets | FIX-24x (UI polish) | YES |
| D-095 | F-A11 — `allOptions`/`selectableIndices` not memoized | FIX-24x (UI polish) | YES |

F-A10 (SLA rolling-window) is explicitly out-of-scope per Plan Scope Decision #5 — not deferred, correctly excluded.

---

## Verification

### Post-fix TypeScript

```
$ cd web && npx tsc --noEmit
TypeScript compilation completed
```

PASS (0 errors)

### Post-fix Vite build

```
$ cd web && ./node_modules/.bin/vite build
...
dist/assets/vendor-charts-CiocHqpl.js                411.33 kB │ gzip: 119.16 kB
✓ built in 5.24s
```

PASS

### Post-fix audit scans (modified files only)

- Hex in `use-timeframe-url-sync.ts`: **0 matches**
- Raw `<button>` in `use-timeframe-url-sync.ts`: **0 matches**

No new token/primitive violations introduced by the F-A12 edit.

### Back-compat check

3 pre-existing `TimeframeSelector` callers unchanged and still compile:
- `web/src/pages/dashboard/analytics.tsx:305`
- `web/src/pages/dashboard/analytics-cost.tsx:130`
- `web/src/pages/sims/detail.tsx:355`

tsc PASS confirms the back-compat overload (accepts `string` or `TimeframeValue`) is preserved.

---

## Token & Component Enforcement (UI story)

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors (FIX-217 scope) | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 | 0 | CLEAN |
| Raw `<button>` outside primitive | 0 | 0 | CLEAN |
| `<Select>` for timeframe | 0 | 0 | CLEAN |
| rgba() LED-glow (pre-existing STORY-045 / FIX-214) | 4 | 4 | OUT-OF-SCOPE (not introduced by FIX-217) |

All pre-existing rgba() hits confirmed via `git log -S` — not regressions.

---

## Passed Items

- AC-1: canonical preset set `1h | 24h | 7d | 30d` + Custom; default 24h — PASS
- AC-2: Custom → date-range popover — PASS
- AC-3: URL sync `?tf=` / `?tf_start=&tf_end=` — PASS (deep-link round-trip verified; filter mutations preserve tf-keys)
- AC-4: applied to 5 real surfaces (admin/api-usage, admin/delivery, operators/detail TrafficTab+HealthTimelineTab, apns/detail TrafficTab, cdrs/index) — PASS
- AC-5: single height/padding + keyboard nav (Arrow/Home/End + Enter/Space + roving tabIndex) — PASS
- A11y: `role="group"` + `aria-label` on container; `aria-pressed`/`aria-disabled`/`title` on pills; Popover focus containment — PASS
- Back-compat: 3 legacy callers (dashboard/analytics, dashboard/analytics-cost, sims/detail) compile unchanged — PASS
- Primitive-only pill groups: no hand-rolled `<button>` timeframe loops remain in adopted pages — PASS

---

## GATE_RESULT: PASS
