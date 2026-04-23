# SCR-041: Operator Detail

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (operator_manager+)
**Route:** `/operators/:id`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  ← Operators    Turkcell (MCC ⓘ 286 / MNC ⓘ 01) [Edit][Del]│
│                    ├────────────────────────────────────────────────────────────┤
│                    │ KPI Row (FIX-222)                                          │
│                    │ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│                    │ │ SIMs     │ │ Active   │ │ Auth/s   │ │ Uptime % │      │
│                    │ │  4.5M    │ │ Sessions │ │  4.2K    │ │  99.97%  │      │
│                    │ │          │ │   589K   │ │  (1h avg)│ │  (24h)   │      │
│                    │ └──────────┘ └──────────┘ └──────────┘ └──────────┘      │
│                    ├────────────────────────────────────────────────────────────┤
│                    │ [◉ Overview] [Protocols] [Health] [Traffic] [Sessions]     │
│                    │ [SIMs] [eSIM] [Alerts] [Audit] [Agreements*]               │
│                    │ (* removed after FIX-238 when roaming decommissioned)      │
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │ Health tab (Overview → shows Health Timeline + Circuit Breaker merged) │
│                    │ Health Timeline (24h)                                      │
│                    │ ┌──────────────────────────────────────────────────────┐  │
│                    │ │ 🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟡🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢│  │
│                    │ │ 00  02  04  06  08  10  12  14  16  18  20  22      │  │
│                    │ └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │ Circuit Breaker (merged into Health tab — FIX-222)        │
│                    │ ┌──────────────────────────────────────────────────────┐  │
│                    │ │ State: CLOSED (healthy)                               │  │
│                    │ │ Failure count: 0 / 5 threshold                        │  │
│                    │ │ Last failure: 3 days ago                               │  │
│                    │ │ [Test Connection]                                      │  │
│                    │ └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │ Alerts tab (merged Notifications panel — FIX-222)         │
│                    │ eSIM tab (reverse-link to eSIM profiles for operator)     │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## API References
- API-306: GET /api/v1/operators/:id (detail — returns masked adapter_config; added STORY-090 F-A2)
- API-022: PATCH /api/v1/operators/:id
- API-023: GET /api/v1/operators/:id/health
- API-024: POST /api/v1/operators/:id/test (legacy)
- API-307: POST /api/v1/operators/:id/test/:protocol (per-protocol; added STORY-090)
