# SCR-030: APN List

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (sim_manager+)
**Route:** `/apns`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  APN Management                            [+ Create APN] │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  Filter: [Operator ▼] [State ▼] [Type ▼]                  │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Name▲      │ Operator │ Type     │ SIMs   │ Traffic │  │
│                    │  │            │          │          │        │ (30d)   │  │
│                    │  ├────────────┼──────────┼──────────┼────────┼─────────┤  │
│                    │  │ iot.fleet  │ 🟢 TCell │ Private  │ 1.2M   │ 45 TB   │  │
│                    │  │ iot.meter  │ 🟢 TCell │ Private  │ 800K   │ 12 TB   │  │
│                    │  │ iot.vehicle│ 🟢 Voda  │ Operator │ 345K   │ 28 TB   │  │
│                    │  │ iot.pos    │ 🟡 TT    │ Private  │ 120K   │ 5 TB    │  │
│                    │  │ iot.scada  │ 🟢 TCell │ Customer │ 80K    │ 2 TB    │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  IP Pool Utilization Summary                               │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ iot.fleet  ████████████████████░░░░ 82%  ⚠ Warning  │  │
│                    │  │ iot.meter  ██████████████░░░░░░░░░░ 62%  ● OK       │  │
│                    │  │ iot.vehicle████████████░░░░░░░░░░░░ 55%  ● OK       │  │
│                    │  │ iot.pos    ████████░░░░░░░░░░░░░░░░ 38%  ● OK       │  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Drill-Down Map

| Data Element | Interaction | Target | Pattern |
|-------------|-------------|--------|---------|
| APN name row | Click | SCR-032 APN Detail | Navigation |
| Operator name | Click | SCR-041 Operator Detail | Navigation |
| SIMs count | Click | SCR-020 SIM List (apn=X) | Navigation |
| IP Pool bar | Click | SCR-112 IP Pool Detail | Navigation |

## API References
- API-030: GET /api/v1/apns
- API-080: GET /api/v1/ip-pools
