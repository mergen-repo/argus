# SCR-041: Operator Detail

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (operator_manager+)
**Route:** `/operators/:id`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  ← Operators    Turkcell                    [⋮ Actions ▼] │
│                    ├───────────────────────────┬────────────────────────────────┤
│                    │                           │ Live Metrics                   │
│                    │ Status: 🟢 Healthy        │ ┌─────┐ ┌─────┐ ┌─────┐      │
│                    │ MCC/MNC: 286/01           │ │Auth │ │Lat. │ │Circ.│      │
│                    │ Adapter: Diameter         │ │4.2K/s│ │8ms  │ │CLOSE│      │
│                    │ RAT: NB-IoT,LTE-M,LTE,5G │ └─────┘ └─────┘ └─────┘      │
│                    │ Failover: fallback_to_next│ ┌─────┐ ┌─────┐              │
│                    │ SLA Target: 99.9%         │ │SIMs │ │APNs │              │
│                    │                           │ │4.5M │ │  8  │              │
│                    │                           │ └─────┘ └─────┘              │
│                    ├───────────────────────────┴────────────────────────────────┤
│                    │ [◉ Health] [SLA] [SIMs] [APNs] [Config] [Protocols]        │
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │ Health Timeline (24h)                                      │
│                    │ ┌──────────────────────────────────────────────────────┐  │
│                    │ │ 🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟡🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢🟢│  │
│                    │ │ 00  02  04  06  08  10  12  14  16  18  20  22      │  │
│                    │ └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │ Latency Distribution (1h)         Auth Success Rate        │
│                    │ ┌────────────────────────┐ ┌────────────────────────────┐ │
│                    │ │ ms                      │ │ 99.98%                     │ │
│                    │ │ 20├──╮                  │ │                            │ │
│                    │ │ 15├──│──╮               │ │ ████████████████████████░  │ │
│                    │ │ 10├──│──│──╮            │ │                            │ │
│                    │ │  5├──│──│──│──╮         │ │ Failed: 42 / 210,000      │ │
│                    │ │   p50 p75 p90 p99       │ │ Reject reasons:           │ │
│                    │ │                          │ │  APN not found: 28        │ │
│                    │ │                          │ │  Auth failed: 14          │ │
│                    │ └────────────────────────┘ └────────────────────────────┘ │
│                    │                                                            │
│                    │ Circuit Breaker State                                      │
│                    │ ┌──────────────────────────────────────────────────────┐  │
│                    │ │ State: CLOSED (healthy)                               │  │
│                    │ │ Failure count: 0 / 5 threshold                        │  │
│                    │ │ Last failure: 3 days ago                               │  │
│                    │ │ Recovery window: 60s                                   │  │
│                    │ │ [Test Connection]                                      │  │
│                    │ └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## API References
- API-306: GET /api/v1/operators/:id (detail — returns masked adapter_config; added STORY-090 F-A2)
- API-022: PATCH /api/v1/operators/:id
- API-023: GET /api/v1/operators/:id/health
- API-024: POST /api/v1/operators/:id/test (legacy)
- API-307: POST /api/v1/operators/:id/test/:protocol (per-protocol; added STORY-090)
