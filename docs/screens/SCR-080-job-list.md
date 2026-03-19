# SCR-080: Job List

**Type:** Page (live updates via WebSocket)
**Layout:** DashboardLayout
**Auth:** JWT (sim_manager+)
**Route:** `/jobs`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Background Jobs                                           │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│                    │  │⏳ Running│ │📋 Queued │ │✅ Done   │ │❌ Failed │     │
│                    │  │    3     │ │    1     │ │   145    │ │    2     │     │
│                    │  └──────────┘ └──────────┘ └──────────┘ └──────────┘     │
│                    │                                                            │
│                    │  Filter: [Type ▼] [State ▼]                               │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ ⏳ Bulk Import #48                    Started: 14:02 │  │
│                    │  │    12,345 SIMs from turkcell_march.csv               │  │
│                    │  │    ████████████████░░░░ 78%  9,629/12,345  Failed: 12│  │
│                    │  │    ETA: ~3 min                      [Cancel] [Logs]  │  │
│                    │  ├──────────────────────────────────────────────────────┤  │
│                    │  │ ⏳ Policy Rollout #47                 Started: 13:45 │  │
│                    │  │    vehicle-premium v4→v5  Stage 2/3 (10%)           │  │
│                    │  │    ████████░░░░░░░░░░░░ 34,500/345,000              │  │
│                    │  │    Waiting for advance command      [Advance] [Logs] │  │
│                    │  ├──────────────────────────────────────────────────────┤  │
│                    │  │ ⏳ IP Reclaim Sweep                   Started: 13:00 │  │
│                    │  │    Reclaiming IPs from terminated SIMs (7d grace)   │  │
│                    │  │    ████████████████████████ 100%  Reclaimed: 342    │  │
│                    │  │    Completing...                                     │  │
│                    │  ├──────────────────────────────────────────────────────┤  │
│                    │  │ ❌ Bulk State Change #46             Failed: 12:30   │  │
│                    │  │    Suspend 500 SIMs on APN iot.test                 │  │
│                    │  │    ████████████████████████ 100%  Failed: 23/500    │  │
│                    │  │    [Download Error Report] [Retry Failed] [Dismiss] │  │
│                    │  ├──────────────────────────────────────────────────────┤  │
│                    │  │ ✅ Bulk Import #45                  Done: 11:45      │  │
│                    │  │    10,000 SIMs imported. Success: 9,988 Failed: 12  │  │
│                    │  │    [Download Error Report] [View SIMs]              │  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## States
- **Running:** Progress bar animates, ETA shown, live via WebSocket (job.progress)
- **Completed:** Green check, success/fail counts, download links
- **Failed:** Red x, error count, retry + error report buttons

## API References
- API-120: GET /api/v1/jobs
- API-121: GET /api/v1/jobs/:id
- API-122: POST /api/v1/jobs/:id/cancel
- API-123: POST /api/v1/jobs/:id/retry
- WebSocket: job.progress, job.completed
