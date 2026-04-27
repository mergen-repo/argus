# SCR-196: IMEI Pool Management

**Type:** Page (4 tabs)
**Layout:** DashboardLayout
**Auth:** JWT (sim_manager+ for read; tenant_admin+ for write)
**Route:** `/settings/imei-pools` (default tab `whitelist`; sub-routes `#whitelist`, `#greylist`, `#blacklist`, `#bulk-import`)

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Settings > IMEI Pools                                     │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  [White List] [Grey List] [Black List] [Bulk Import]       │
│                    │  ─────────                                                  │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  [TAC ▼] [Device Model ▼] [Type: full_imei|tac_range ▼]   │
│                    │  [⌘K Search]    [🔍 IMEI Lookup]    [+ Add Entry]         │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  ┌────────────────────────────────────────────────────────┐│
│                    │  │ TAC      │ Device Model    │ Type      │ Bound │ By    ││
│                    │  │          │                 │           │ SIMs  │       ││
│                    │  ├──────────┼─────────────────┼───────────┼───────┼───────┤│
│                    │  │ 35291108 │ Quectel BG95    │ tac_range │ 12,403│ Bora T││
│                    │  │ 35291109 │ Quectel BG96    │ tac_range │  4,201│ Bora T││
│                    │  │ 86412 06…│ SIMCom SIM7600E │ full_imei │      1│ Selen ││
│                    │  │ 35982107 │ Telit ME910     │ tac_range │  8,876│ admin ││
│                    │  └──────────┴─────────────────┴───────────┴───────┴───────┘│
│                    │  Showing 1-50 of 218     ◀ Prev  [1] 2 3 ... 5  Next ▶   │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

### Grey List tab (extra column "Quarantine Reason")

```
│ TAC      │ Device Model │ Type      │ Bound │ Quarantine Reason       │ By    │
├──────────┼──────────────┼───────────┼───────┼─────────────────────────┼───────┤
│ 86223115 │ Generic CAT-1│ tac_range │   234 │ Suspicious roaming pat. │ admin │
│ 35840411 │ Unknown OEM  │ tac_range │    18 │ Pending vendor verify   │ Bora T│
```

### Black List tab (extra columns "Block Reason", "Imported From")

```
│ TAC/IMEI │ Device       │ Type      │ Block Reason          │ Source        │
├──────────┼──────────────┼───────────┼───────────────────────┼───────────────┤
│ 35982107…│ Telit ME910  │ full_imei │ Reported stolen 02/16 │ manual        │
│ 86223115 │ —            │ tac_range │ CEIR ban (Q1 2026)    │ gsma_ceir     │
│ 35840411 │ —            │ tac_range │ Operator EIR feed     │ operator_eir  │
```

### Bulk Import tab

```
│  Bulk Import — White List                                                │
│  ┌──────────────────────────────────────────────────────────────────────┐│
│  │  Drop CSV here  or  [Choose File]                                    ││
│  │  Schema: imei_or_tac, kind, device_model, description,               ││
│  │          quarantine_reason, block_reason, imported_from               ││
│  │  Max: 10 MB / 100,000 rows                                           ││
│  └──────────────────────────────────────────────────────────────────────┘│
│                                                                          │
│  Preview (first 5 rows of 12,540):                                       │
│  ┌──────────┬───────────┬─────────────────┬─────────┬────────────────┐  │
│  │ imei_or… │ kind      │ device_model    │ desc.   │ row_status     │  │
│  ├──────────┼───────────┼─────────────────┼─────────┼────────────────┤  │
│  │ 35291108 │ tac_range │ Quectel BG95    │ —       │ ✓ valid        │  │
│  │ 35291109 │ tac_range │ Quectel BG96    │ —       │ ✓ valid        │  │
│  │ 12345    │ tac_range │ Bad row         │ —       │ ⚠ TAC<8 chars  │  │
│  └──────────┴───────────┴─────────────────┴─────────┴────────────────┘  │
│                                                       [Cancel] [Import] │
│                                                                          │
│  ─── Active Job (after submit) ───                                       │
│  ⏳ Job #312  Bulk Import (whitelist)            Started: 14:21         │
│  ████████████████░░░░ 78%   9,783/12,540   Failed: 4   ETA: ~1 min      │
│                                                  [Cancel] [Logs]         │
```

### Add Entry modal (full_imei vs tac_range toggle)

```
┌──────────────────────────────────────────────────────────────┐
│  Add IMEI Entry — White List                          [×]    │
├──────────────────────────────────────────────────────────────┤
│  Type:   ( ) Full IMEI (15 digits)                            │
│          (●) TAC range (8-digit prefix)                       │
│                                                              │
│  IMEI / TAC:        [ 35291108                            ]  │
│  Device Model:      [ Quectel BG95                        ]  │
│  Description:       [ Standard IoT modem fleet            ]  │
│                                                              │
│                                       [Cancel] [Add Entry]   │
└──────────────────────────────────────────────────────────────┘
```

## Features
- **3-tab list view**: White / Grey / Black with consistent columns + per-list specific columns (quarantine_reason for grey; block_reason + imported_from for black)
- **Bound SIMs link**: count column is clickable → navigates to SIM List filtered by `bound_imei` matching this entry (full_imei → exact; tac_range → TAC prefix)
- **IMEI Lookup**: toolbar button opens SCR-197 modal
- **Add Entry modal**: full_imei vs tac_range toggle, validates length (15 vs 8); blacklist requires block_reason + imported_from; greylist requires quarantine_reason
- **Bulk Import**: reuses the SVC-09 job pattern (SCR-080); 3-stage flow input → preview → result with progress bar + error CSV download (same shape as FIX-224 SCR-020 SIM Import)
- **Row actions**: View Details (drawer with Bound SIMs link + observation history), Move (between lists with reason prompt), Delete (audited)

## Empty states
- All three lists empty → centered illustration + copy "No entries yet. Add your first device or import a CSV." + [+ Add Entry] [Bulk Import] CTAs
- Bulk Import idle → drag-drop area with schema explainer

## Permissions
- View list: `sim_manager+`
- Add / Delete / Bulk import: `tenant_admin+`

## Components used
- **Atoms**: Badge (kind, type), Button (Add Entry, Bulk Import, IMEI Lookup), Input (search), Tabs (4 tab strip)
- **Molecules**: FilterChip (TAC, Device Model, Type), DataTable header with sort, Row action menu (⋮), DropZone (CSV)
- **Organisms**: PageHeader with toolbar, DataTable (paginated), AddEntryDialog (compact confirm — Option C), BulkImportSlidePanel (rich form — Option C, mirrors FIX-224 pattern), JobProgressCard (shared with SCR-080)

## API endpoints used
- API-331 GET `/api/v1/imei-pools/{kind}` — list (with `?include_bound_count=1`)
- API-332 POST `/api/v1/imei-pools/{kind}` — add single entry
- API-333 DELETE `/api/v1/imei-pools/{kind}/{id}` — remove entry
- API-334 POST `/api/v1/imei-pools/{kind}/import` — bulk CSV (returns `job_id`)
- API-335 GET `/api/v1/imei-pools/lookup?imei=` — drives SCR-197

## Tables used
- TBL-56 `imei_whitelist`
- TBL-57 `imei_greylist`
- TBL-58 `imei_blacklist`
- TBL-59 `imei_history` (read-only via row "View Details" drawer)

## Stories
- STORY-095 (primary)
- STORY-094, STORY-096 (cross-reference for Bound SIMs link)
