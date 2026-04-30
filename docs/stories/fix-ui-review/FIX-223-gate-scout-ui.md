# FIX-223 — Gate Scout: UI

Scout executed inline by Gate Lead (subagent nesting constraint).

## Surfaces Reviewed
1. `web/src/pages/settings/ip-pool-detail.tsx` — search input, address table, Reserve SlidePanel
2. `web/src/pages/apns/detail.tsx` — IP Pools section header
3. `web/src/lib/glossary-tooltips.ts` — `static_ip` entry
4. `web/src/hooks/use-settings.ts::useIpPoolAddresses` — signature + queryKey
5. `web/src/types/settings.ts::IpAddress` — new `last_seen_at?`
6. `web/src/hooks/use-debounce.ts` — reused primitive

<SCOUT-UI-FINDINGS>
F-U1 | MEDIUM | ui
- Title: "Currently reserved" mini-list hidden when main search is active
- File: web/src/pages/settings/ip-pool-detail.tsx:113-115 (reservedAddresses), :422-439 (panel)
- Detail: `reservedAddresses` was derived from `sortedAddresses`, which is the server-filtered list. Opening the Reserve SlidePanel while a search like `10.0.1` is active made the "Currently reserved (N)" mini-list show ONLY the reserved IPs matching that filter — a minor UX regression introduced by moving search server-side. Intent is that the panel always reflects the pool's full reservation state independent of the main table's filter.
- Fixable: YES — add a second `useIpPoolAddresses(poolId, undefined)` call (React Query deduplicates identical keys; costs one extra query only when a search is active) and derive `reservedAddresses` from that unfiltered source.
- Severity: MEDIUM — now FIXED

F-U2 | PASS | ui
- Title: Debounce — 300ms on search input
- Evidence: `useDebounce(searchFilter, 300)` wired at L71, query key changes on debouncedSearch only. Fast typing fires at most one request after quiesce.

F-U3 | PASS | ui
- Title: Empty q returns full list
- Evidence: Hook sends `?q=…` only when `q` truthy (use-settings.ts:217). Handler accepts empty + skips predicate (ippool.go:535). Clearing the search via the `X` button resets `searchFilter=''` → debounces back to `''` → full list reloads.

F-U4 | PASS | ui
- Title: "Last Seen" column rendered; colSpan updated
- Evidence: L282 `<TableHead>Last Seen</TableHead>`; L289 `colSpan={6}` matches 6 total headers (IP, State, SIM, Assigned At, Last Seen, action). Empty cell uses `—` per design.

F-U5 | PASS | ui
- Title: InfoTooltip on APN "IP Pools" section label
- Evidence: `web/src/pages/apns/detail.tsx:187-189` — `<InfoTooltip term="static_ip">IP Pools</InfoTooltip>`. Glossary entry present at L13 of glossary-tooltips.ts. Console-clean; text matches GLOSSARY.md source-of-truth.

F-U6 | PASS | ui
- Title: Reserve SlidePanel ICCID enrichment works
- Evidence: L430-434 reads `addr.sim_iccid` (from new DTO field), falls back to UUID slice with inline rationale comment. DTO returns `sim_iccid` from LEFT JOIN. "Currently reserved" list now populates via unfiltered data (F-U1 fix).

F-U7 | PASS | ui
- Title: Token enforcement clean
- Evidence: raw-button=0, hex=0, native-dialog=0. All new markup reuses shadcn primitives (`Input`, `Button`, `Table*`, `SlidePanel`, `InfoTooltip`).

F-U8 | PASS | ui
- Title: Dark mode parity preserved
- Evidence: All added elements use existing `text-text-*`, `bg-bg-*`, `border-border*` semantic tokens. No hard-coded light colors.

F-U9 | PASS | ui
- Title: Empty-state copy reflects server-side search
- Evidence: L293 `No addresses matching "{searchFilter}"` when search non-empty; `No addresses loaded yet` otherwise. Footer counter shows `{sortedAddresses.length} addresses` without "of N" (server pre-filtered, so total-of comparison would be misleading).
</SCOUT-UI-FINDINGS>
