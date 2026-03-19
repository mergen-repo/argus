# SCR-120: System Health

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (super_admin)
**Route:** `/system/health`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  System Health                           ◉ All Systems Go  │
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │  Services                                                  │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ 🟢 API Gateway       :8080  │ Resp: 2ms   │ Up 45d │  │
│                    │  │ 🟢 WebSocket Server  :8081  │ Conns: 128  │ Up 45d │  │
│                    │  │ 🟢 RADIUS Engine     :1812  │ 8.2K/s      │ Up 45d │  │
│                    │  │ 🟢 Diameter Engine   :3868  │ 4.1K/s      │ Up 45d │  │
│                    │  │ 🟢 5G SBA Proxy      :8443  │ 120/s       │ Up 45d │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Infrastructure                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ 🟢 PostgreSQL   │ Conns: 45/100 │ Disk: 180/500 GB │  │
│                    │  │ 🟢 TimescaleDB  │ Chunks: 1,245 │ Compressed: 82% │  │
│                    │  │ 🟢 Redis        │ Mem: 4.8/16 GB│ Keys: 15.2M     │  │
│                    │  │ 🟢 NATS         │ Msgs/s: 12K   │ Streams: 8      │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Performance (Real-time)                                   │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Auth Throughput (last 1h)                            │  │
│                    │  │ req/s                                                 │  │
│                    │  │ 10K├──╮     ╭──╮     ╭──────                         │  │
│                    │  │  8K├──╯─────╯  ╰─────╯                              │  │
│                    │  │  6K├                                                  │  │
│                    │  │    ├───┬───┬───┬───┬───┬───                          │  │
│                    │  │    00  10  20  30  40  50  min                        │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  ┌────────────────────────┐ ┌────────────────────────────┐│
│                    │  │ Auth Latency           │ │ Go Runtime                 ││
│                    │  │ p50:  3ms              │ │ Goroutines: 1,245          ││
│                    │  │ p95: 12ms              │ │ Heap: 2.1 GB               ││
│                    │  │ p99: 28ms  ✅ < 50ms   │ │ GC Pause: 1.2ms            ││
│                    │  └────────────────────────┘ │ CPU: 45%                   ││
│                    │                              └────────────────────────────┘│
│                    │                                                            │
│                    │  Log Level Controls                                        │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ gateway:  [info ▼]  │ aaa:     [info ▼]             │  │
│                    │  │ policy:   [info ▼]  │ operator:[debug▼] ← changed   │  │
│                    │  │ analytics:[info ▼]  │ jobs:    [info ▼]             │  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## API References
- API-180: GET /api/health
- API-181: GET /api/v1/system/metrics
- API-182: GET /api/v1/system/config
- WebSocket: metrics.realtime
