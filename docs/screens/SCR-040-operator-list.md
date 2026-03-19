# SCR-040: Operator List

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (operator_manager+)
**Route:** `/operators`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Operators                  [+ Add Operator] (super_admin) │
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │ ┌──────────────────────────────────────────────────────┐   │
│                    │ │                                                      │   │
│                    │ │  ┌────────────────────┐  ┌────────────────────┐      │   │
│                    │ │  │ 🟢 TURKCELL        │  │ 🟢 VODAFONE       │      │   │
│                    │ │  │ MCC:286 MNC:01     │  │ MCC:286 MNC:02    │      │   │
│                    │ │  │                    │  │                    │      │   │
│                    │ │  │ SIMs: 4.5M         │  │ SIMs: 3.2M        │      │   │
│                    │ │  │ Sessions: 2.1M     │  │ Sessions: 1.5M    │      │   │
│                    │ │  │ Auth/s: 4,200      │  │ Auth/s: 3,100     │      │   │
│                    │ │  │ Uptime: 99.95%     │  │ Uptime: 99.87%    │      │   │
│                    │ │  │ Latency: 8ms p95   │  │ Latency: 12ms p95 │      │   │
│                    │ │  │ ▁▂▃▄▅▆▇▆▅▄▃▂▁▂▃▄▅│  │ ▁▂▃▅▇▆▅▃▂▁▂▃▅▇▆▅│      │   │
│                    │ │  │                    │  │                    │      │   │
│                    │ │  │ Failover: fallback │  │ Failover: reject  │      │   │
│                    │ │  │ Circuit: CLOSED    │  │ Circuit: CLOSED   │      │   │
│                    │ │  └────────────────────┘  └────────────────────┘      │   │
│                    │ │                                                      │   │
│                    │ │  ┌────────────────────┐  ┌────────────────────┐      │   │
│                    │ │  │ 🟡 TT MOBILE       │  │ ⚪ MOCK SIMULATOR  │      │   │
│                    │ │  │ MCC:286 MNC:03     │  │ MCC:001 MNC:01    │      │   │
│                    │ │  │                    │  │                    │      │   │
│                    │ │  │ SIMs: 2.1M         │  │ SIMs: 10K (dev)   │      │   │
│                    │ │  │ Sessions: 980K     │  │ Sessions: 500     │      │   │
│                    │ │  │ Uptime: 98.20% ⚠   │  │ Uptime: 100%      │      │   │
│                    │ │  │ Latency: 25ms p95⚠ │  │ Latency: 1ms      │      │   │
│                    │ │  │ ▁▂▃▁▁▂▃▄▅▆▇▆▅▃▂▁  │  │ ▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅▅│      │   │
│                    │ │  │                    │  │                    │      │   │
│                    │ │  │ Failover: queue 5s │  │ Failover: —       │      │   │
│                    │ │  │ Circuit: HALF_OPEN │  │ Circuit: CLOSED   │      │   │
│                    │ │  └────────────────────┘  └────────────────────┘      │   │
│                    │ └──────────────────────────────────────────────────────┘   │
│                    │                                                            │
│                    │ SLA Summary (30d)                                          │
│                    │ ┌──────────────────────────────────────────────────────┐   │
│                    │ │ Operator  │ Target │ Actual │ Violations │ Downtime │   │
│                    │ ├───────────┼────────┼────────┼────────────┼──────────┤   │
│                    │ │ Turkcell  │ 99.9%  │ 99.95% │ 0          │ 0h 0m    │   │
│                    │ │ Vodafone  │ 99.9%  │ 99.87% │ 2          │ 0h 38m   │   │
│                    │ │ TT Mobile │ 99.9%  │ 98.20% │ 7          │ 5h 12m   │   │
│                    │ └──────────────────────────────────────────────────────┘   │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Drill-Down Map

| Data Element | Interaction | Target | Pattern |
|-------------|-------------|--------|---------|
| Operator card | Click | SCR-041 Operator Detail | Navigation |
| SIMs count | Click | SCR-020 SIM List (operator=X) | Navigation |
| Uptime % | Hover | 24h health timeline mini-chart | Tooltip |
| SLA violation count | Click | SCR-041 Operator Detail (SLA tab) | Navigation |

## API References
- API-020: GET /api/v1/operators
- API-023: GET /api/v1/operators/:id/health
