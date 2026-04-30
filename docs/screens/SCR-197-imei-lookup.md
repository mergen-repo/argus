# SCR-197: IMEI Lookup (modal/drawer)

**Type:** Modal (compact form) → Drawer (rich result)
**Layout:** Overlay on parent (SCR-196 IMEI Pools, SCR-050 Live Sessions, SCR-020 SIM List)
**Auth:** JWT (sim_manager+)
**Route:** Drawer state — opens from toolbar button "🔍 IMEI Lookup" on parent screens (no dedicated route; URL hash `#imei-lookup` reflects open state)

## Mockup — Input modal (compact)

```
┌──────────────────────────────────────────────────────────────┐
│  IMEI Lookup                                          [×]    │
├──────────────────────────────────────────────────────────────┤
│  Paste a full IMEI (15 digits) or TAC prefix (8 digits):     │
│                                                              │
│  [ 359211089765432                                        ]  │
│                                                              │
│  ⚠ Cross-tenant lookup is restricted to your tenant scope.  │
│                                                              │
│                                       [Cancel] [Lookup]      │
└──────────────────────────────────────────────────────────────┘
```

## Mockup — Result drawer (rich)

```
┌────────────────────────────────────────────────────────────┐
│  IMEI Lookup — 359211089765432                       [×]   │
├────────────────────────────────────────────────────────────┤
│  TAC: 35291108  ·  Device: Quectel BG95  (vendor TAC match)│
│                                                            │
│  ┌─ Pool Membership ──────────────────────────────────────┐│
│  │ ✓ White List  matched_via: tac_range                   ││
│  │   Entry id: a83f… · Added 2026-03-04 by Bora T.        ││
│  │ — Grey List   not present                              ││
│  │ — Black List  not present                              ││
│  └────────────────────────────────────────────────────────┘│
│                                                            │
│  ┌─ Bound SIMs (3) ───────────────────────────────────────┐│
│  │ ICCID            │ Binding Mode  │ Status      │       ││
│  ├──────────────────┼───────────────┼─────────────┼───────┤│
│  │ 8990111122223333 │ strict        │ ✓ verified  │ View →││
│  │ 8990111122224444 │ grace-period  │ ⏳ pending   │ View →││
│  │ 8990111122225555 │ allowlist     │ ⚠ mismatch  │ View →││
│  └──────────────────┴───────────────┴─────────────┴───────┘│
│                                                            │
│  ┌─ Recent Observations (last 30 days, 14 events) ────────┐│
│  │ 2026-04-26 14:02  · ICCID 8990…3333 · S6a · NAS-A2     ││
│  │ 2026-04-26 13:58  · ICCID 8990…3333 · 5G SBA · gNB-12  ││
│  │ 2026-04-25 09:11  · ICCID 8990…4444 · RADIUS · NAS-A2  ││
│  │ … (+11 more)                                            ││
│  │                                          [View All]    ││
│  └────────────────────────────────────────────────────────┘│
│                                                            │
│  Last seen: 2026-04-26 14:02:33 UTC · via Diameter S6a     │
│                                                            │
│                          [Add to Pool ▼] [Close]           │
└────────────────────────────────────────────────────────────┘
```

## Mockup — TAC-prefix lookup (8 digits)

```
│  TAC Lookup — 35291108                                     │
│                                                            │
│  Vendor: Quectel  ·  Model: BG95 (assumed by TBL-56 hit)   │
│                                                            │
│  Pool Membership: ✓ White List (tac_range entry)           │
│                                                            │
│  Bound SIMs (TAC prefix match): 12,403                     │
│    [→ View in SIM List filtered by this TAC]               │
```

## Features
- **Two input modes**: full 15-digit IMEI (exact lookup across pools + bindings) or 8-digit TAC prefix (range lookup)
- **Pool membership**: shows which lists contain the IMEI/TAC and `matched_via` (`exact` vs `tac_range`)
- **Bound SIMs**: clickable list of currently-bound SIMs; each row links to SIM Detail (SCR-021)
- **Observations**: latest events from `imei_history` for any SIM that observed this IMEI; "View All" → SCR-021e or filtered view
- **Add to Pool**: dropdown with sub-actions ("Add to White List", "Add to Grey List", "Add to Black List") opens SCR-196 Add Entry modal pre-populated with this IMEI

## Empty states
- IMEI not found anywhere → "No matches in this tenant. The IMEI has not been observed and is not in any pool." + [Add to Pool ▼]
- IMEI in pool but never observed → result drawer shows pool membership + "Not yet bound to any SIM" line

## Error states
- Invalid input length (≠15 and ≠8) → 422 inline message "Enter a 15-digit IMEI or 8-digit TAC."
- Server error → toast + retry button

## Permissions
- All authenticated `sim_manager+` users (read-only lookup)
- "Add to Pool" actions enforce `tenant_admin+` per API-332

## Components used
- **Atoms**: Input (IMEI), Button (Lookup, Add to Pool, Close), Badge (matched_via, binding status, list pill)
- **Molecules**: ResultSection (3 collapsible sections — Pool / Bound SIMs / Observations), KVRow (TAC, vendor, last seen)
- **Organisms**: IMEILookupDialog (compact input — Option C), IMEILookupDrawer (rich result — SlidePanel width="lg", Option C)

## API endpoints used
- API-335 GET `/api/v1/imei-pools/lookup?imei=` — primary data source
- API-330 GET `/api/v1/sims/{id}/imei-history` — invoked per "View All" link

## Tables used
- TBL-56 `imei_whitelist`, TBL-57 `imei_greylist`, TBL-58 `imei_blacklist` (pool match)
- TBL-59 `imei_history` (observations)
- TBL-10 `sims` (binding metadata for Bound SIMs section)

## Stories
- STORY-095 (primary — drives the lookup tool)
- STORY-094 (Bound SIMs / observations)
