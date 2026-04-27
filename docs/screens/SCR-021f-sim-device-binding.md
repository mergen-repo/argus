# SCR-021f: SIM Detail — Device Binding Tab

**Type:** Tab content (sibling of SCR-021/021b/021c/021d/021e)
**Auth:** JWT (sim_manager+)
**Route:** `/sims/:id#device-binding`

## Mockup

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ [Overview] [Sessions] [Usage] [History] [Diagnostics] [◉ Device Binding]     │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│ ┌─ Current Binding ─────────────────────────────────────────────────────┐    │
│ │  Bound IMEI:        359211089765432    (Quectel BG95)                 │    │
│ │  Binding Mode:      [ grace-period ▼ ]   ⓘ                             │    │
│ │  Binding Status:    ⏳ pending  (grace expires in 06:14:22)           │    │
│ │  Last Seen At:      2026-04-26 14:02:33 UTC · via Diameter S6a         │    │
│ │  Verified At:       —                                                  │    │
│ │                                                                        │    │
│ │  [🔓 Re-pair to new IMEI]   [🔒 Force Re-verify]                       │    │
│ └────────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│ ┌─ Allowed IMEIs (allowlist mode only) ─────────────────────────────────┐    │
│ │  IMEI                  Added         Added By        Action          │    │
│ │  359211089765432       2026-03-04    Bora T.         [Remove]        │    │
│ │  864120601234567       2026-03-12    Selen K.        [Remove]        │    │
│ │  [+ Add IMEI]                                                         │    │
│ └────────────────────────────────────────────────────────────────────────┘    │
│ (section hidden when binding_mode ≠ 'allowlist')                             │
│                                                                              │
│ ┌─ IMEI History (latest 20 observations)         [View All]   [Export CSV] ┐│
│ │ Timestamp        │ IMEI            │ Proto    │ NAS       │ Δ │ Alarm   ││
│ ├──────────────────┼─────────────────┼──────────┼───────────┼───┼─────────┤│
│ │ 04-26 14:02:33   │ 359211089765432 │ S6a      │ NAS-A2    │ — │ —       ││
│ │ 04-25 09:11:08   │ 359211089765432 │ RADIUS   │ NAS-A2    │ — │ —       ││
│ │ 04-22 18:44:51   │ 864120605431122 │ S6a      │ NAS-B1    │ ⚠ │ ALARM   ││
│ │ 04-22 18:43:02   │ 359211089765432 │ S6a      │ NAS-B1    │ — │ —       ││
│ │ … (+16 more)                                                          ││
│ └──────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
```

### Re-pair confirmation dialog (compact)

```
┌──────────────────────────────────────────────────────────────┐
│  Re-pair Device — 8990111122223333                  [×]      │
├──────────────────────────────────────────────────────────────┤
│  This will clear the bound IMEI and set status to `pending`. │
│  The next successful authentication re-binds the SIM.        │
│                                                              │
│  Reason for re-pair:                                          │
│  ( ) Device replacement (truck-roll)                          │
│  (●) Customer reported swap                                   │
│  ( ) Theft / loss recovery                                    │
│  ( ) Other:  [____________________________________________]  │
│                                                              │
│  ⚠ Audited as `sim.imei_repaired`.                           │
│                                                              │
│                              [Cancel] [Re-pair Now]           │
└──────────────────────────────────────────────────────────────┘
```

## Binding Mode dropdown values
- `(NULL)` Off — no enforcement, IMEI captured & logged only
- `strict` — Reject any IMEI mismatch
- `allowlist` — Accept any IMEI in the per-SIM allowed list (sub-table above)
- `first-use` — First successful auth pins IMEI; subsequent mismatches reject
- `tac-lock` — Accept any IMEI sharing the bound IMEI's TAC (vendor/model lock)
- `grace-period` — Accept mismatch with countdown alert until expiry, then reject
- `soft` — Alert-only; never reject

## Binding Status badge values
- `verified` ✓ (green)
- `pending` ⏳ (amber) — initial or after re-pair
- `mismatch` ⚠ (red) — observed IMEI ≠ bound IMEI
- `unbound` ○ (grey) — no IMEI bound yet
- `disabled` — (text-tertiary) — binding_mode is NULL

## Features
- **Mode change**: dropdown writes via API-328; if change would orphan an active session, shows 409 inline error "Cannot change mode while session is active"
- **Allowlist sub-table**: shown only when mode=`allowlist`; add/remove rows persist via API-328 PATCH (allowlist payload)
- **Re-pair button**: opens compact confirm dialog (Option C); on confirm calls API-329; refreshes status to `pending`
- **Force Re-verify**: clears `binding_verified_at`, sets status to `pending`, no IMEI change — next auth re-verifies
- **History timeline**: latest 20 rows from `imei_history`; "View All" → full SCR-021e-style history scoped to this SIM; export CSV via job
- **Grace countdown**: live-ticking timer when mode=`grace-period` and status=`pending`; turns red when <1h remains

## Permissions
- View: `sim_manager+`
- Mode change / Re-pair / Allowlist edits: `sim_manager+` per API-328/329 (audited)

## Components used
- **Atoms**: Badge (mode, status), Button (Re-pair, Force Re-verify, Add IMEI, Remove), Select (binding_mode dropdown), Countdown
- **Molecules**: KVRow (Current Binding card rows), AllowlistRow with remove action, HistoryRow (alarm flag + timestamp)
- **Organisms**: CurrentBindingCard, AllowlistTable, IMEIHistoryPanel, RepairConfirmDialog (compact — Option C)

## API endpoints used
- API-327 GET `/api/v1/sims/{id}/device-binding` — load tab data
- API-328 PATCH `/api/v1/sims/{id}/device-binding` — mode change, allowlist edits
- API-329 POST `/api/v1/sims/{id}/device-binding/re-pair` — admin re-pair
- API-330 GET `/api/v1/sims/{id}/imei-history` — history panel + "View All"

## Tables used
- TBL-10 `sims` — `binding_mode`, `bound_imei`, `binding_status`, `binding_verified_at`, `last_imei_seen_at`, `binding_grace_expires_at` (Phase 11 column extensions)
- TBL-59 `imei_history` — observation rows
- TBL-60 `sim_imei_allowlist` — allowlist sub-table

## Stories
- STORY-094 (primary — binding model + per-SIM control)
- STORY-097 (Re-pair workflow + grace countdown)
