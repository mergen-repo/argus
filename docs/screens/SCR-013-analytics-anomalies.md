# SCR-013: Analytics — Anomaly Detection

**Type:** Page (tab 3 of Analytics)
**Layout:** DashboardLayout
**Auth:** JWT (analyst+)
**Route:** `/analytics/anomalies`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Analytics > Anomalies                 Period: [Last 24h ▼]│
│                    ├────────────────────────────────────────────────────────────┤
│                    │  [○ Usage] [○ Cost] [◉ Anomalies]                         │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  ┌──────────┐ ┌──────────┐ ┌──────────┐                   │
│                    │  │🔴 Active │ │🟡 Invest.│ │✅ Resolved│                   │
│                    │  │    5     │ │    3     │ │   28     │                   │
│                    │  └──────────┘ └──────────┘ └──────────┘                   │
│                    │                                                            │
│                    │  Filter: [Type ▼] [Severity ▼] [Status ▼]                 │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ ● │ Time  │ Type          │ Entity     │ Sev.  │ ⋮  │  │
│                    │  ├───┼───────┼───────────────┼────────────┼───────┼────┤  │
│                    │  │🔴 │ 14:23 │ SIM cloning   │ IMSI 286.. │ Crit. │ ⋮  │  │
│                    │  │🔴 │ 14:15 │ Data spike    │ APN fleet  │ High  │ ⋮  │  │
│                    │  │🟡 │ 13:45 │ Auth flood    │ Op Turkcell│ Med.  │ ⋮  │  │
│                    │  │🟡 │ 13:20 │ Unusual roam  │ 45 SIMs    │ Med.  │ ⋮  │  │
│                    │  │🔴 │ 12:58 │ Session spike │ APN meter  │ High  │ ⋮  │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  ▼ Expanded: SIM Cloning Detected                          │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ IMSI: 286010123456789    ICCID: 8990111234567890     │  │
│                    │  │                                                      │  │
│                    │  │ Same IMSI authenticated from 2 different NAS IPs     │  │
│                    │  │ within 3 minutes:                                     │  │
│                    │  │   • 14:21 — NAS 10.0.1.1 (Istanbul)                  │  │
│                    │  │   • 14:23 — NAS 10.0.2.5 (Ankara)                    │  │
│                    │  │                                                      │  │
│                    │  │ Recommended: Suspend SIM immediately                  │  │
│                    │  │                                                      │  │
│                    │  │ [View SIM] [Suspend SIM] [Mark Investigated] [Dismiss]│  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Drill-Down Map

| Data Element | Interaction | Target | Pattern |
|-------------|-------------|--------|---------|
| Active/Investigating/Resolved cards | Click | Filter table by status | Filter |
| Anomaly row | Click | Expand inline detail | Expandable Row |
| IMSI/ICCID in detail | Click | SCR-021 SIM Detail | Navigation |
| APN name in entity | Click | SCR-032 APN Detail | Navigation |
| Operator in entity | Click | SCR-041 Operator Detail | Navigation |
| "View SIM" button | Click | SCR-021 SIM Detail | Navigation |
| "Suspend SIM" button | Click | Confirm dialog → API-045 | Modal |
| "Mark Investigated" | Click | Update status → refresh | Inline |

## States

- **Live:** New anomalies push via WebSocket (alert.new), row appears with animation
- **Loading:** Skeleton table
- **Empty:** "No anomalies detected. Your network is healthy! ✅"

## API References

- API-113: GET /api/v1/analytics/anomalies
- API-045: POST /api/v1/sims/:id/suspend
