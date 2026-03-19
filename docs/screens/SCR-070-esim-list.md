# SCR-070: eSIM Profiles

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (sim_manager+)
**Route:** `/esim`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  eSIM Profiles                                             │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  ┌──────────┐ ┌──────────┐ ┌──────────┐                   │
│                    │  │ Total    │ │ Enabled  │ │ Disabled │                   │
│                    │  │ 245,000  │ │ 238,000  │ │  7,000   │                   │
│                    │  └──────────┘ └──────────┘ └──────────┘                   │
│                    │                                                            │
│                    │  Filter: [Operator ▼] [Profile State ▼] [EID search...]   │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ EID          │ ICCID        │ Op  │ Profile │ Last  │⋮│ │
│                    │  ├──────────────┼──────────────┼─────┼─────────┼───────┼──┤ │
│                    │  │ abc123de...  │ 89901112...  │🟢TC │●Enabled │Mar 15 │⋮│ │
│                    │  │ def456gh...  │ 89901113...  │🟢VF │●Enabled │Mar 12 │⋮│ │
│                    │  │ ghi789jk...  │ 89901114...  │🟡TT │○Disabled│Mar 10 │⋮│ │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  eSIM by Operator                                          │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Turkcell  ██████████████████ 145K (59%)              │  │
│                    │  │ Vodafone  ████████████ 72K (29%)                     │  │
│                    │  │ TT Mobile █████ 28K (12%)                            │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Bulk Actions: [Bulk Enable] [Bulk Disable] [Bulk Switch] │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Row Actions (⋮)
- View SIM → SCR-021
- Enable Profile → API-072
- Disable Profile → API-073
- Switch Operator → API-074 (confirm dialog)

## API References
- API-070: GET /api/v1/esim-profiles
- API-072-074: Profile management
