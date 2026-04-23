# SCR-010: Main Dashboard

**Type:** Page
**Layout:** DashboardLayout (sidebar + content)
**Auth:** JWT (any role)
**Route:** `/`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  ◆ ARGUS           │  Dashboard                    🔍 ⌘K    🔔 5   ☾   👤 BT ▼│
│                    ├────────────────────────────────────────────────────────────┤
│ ─ MAIN ──────────  │                                                            │
│ ◉ Dashboard        │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐     │
│ ○ SIMs             │  │● Active  │ │○ Sessions│ │○ Auth/s  │ │⚠ Alerts  │     │
│ ○ APNs             │  │  SIMs    │ │          │ │          │ │          │     │
│ ○ Operators        │  │2,345,678 │ │  42,891  │ │  8,234   │ │    12    │     │
│ ○ Policies         │  │▲+12,345  │ │▲+1,205   │ │─ steady  │ │▼-3 (24h)│     │
│ ○ eSIM Profiles    │  │▁▂▃▄▅▆▇█ │ │▁▂▃▅▇▆▅▃ │ │▅▅▆▆▇▇▆▅ │ │▃▂▁▁▂▃▂▁ │     │
│                    │  └──────────┘ └──────────┘ └──────────┘ └──────────┘     │
│ ─ MONITORING ────  │                                                            │
│ ○ Sessions  🔴42K  │  ┌──────────────────────────┐ ┌──────────────────────────┐│
│ ○ Analytics        │  │ SIM Distribution          │ │ Operator Health          ││
│ ○ Jobs      ⏳3    │  │ by Operator               │ │                          ││
│                    │  │    ┌───┐                   │ │ 🟢 Turkcell  99.9% ████ ││
│ ─ SYSTEM ────────  │  │    │ T │ 45%              │ │ 🟢 Vodafone  99.7% ████ ││
│ ○ Audit Log        │  │ ┌──┤   │                   │ │ 🟡 TT Mobile 98.2% ███░││
│ ○ Notifications 🔔5│  │ │V │   │                   │ │                          ││
│ ○ Settings   ▶     │  │ │  │   ├──┐               │ │ Last 24h uptime          ││
│                    │  │ └──┴───┴──┘               │ │                          ││
│ ─────────────────  │  └──────────────────────────┘ └──────────────────────────┘│
│ ○ System Health    │                                                            │
│                    │  ┌──────────────────────────┐ ┌──────────────────────────┐│
│                    │  │ Top 5 APNs by Traffic     │ │ Alert Feed (Live)    🔴  ││
│ ┌────────────────┐ │  │                          │ │                          ││
│ │ ☾ Dark  ☼ Light│ │  │ iot.fleet    ████████ 2T│ │ ⚠ 14:23 SLA violation   ││
│ └────────────────┘ │  │ iot.meter    ██████ 1.5T│ │   TT Mobile <99.9%      ││
│                    │  │ iot.vehicle  ████ 800G  │ │ ⚠ 14:15 Anomaly: spike  ││
│  👤 Bora T.   ▼   │  │ iot.pos      ███ 600G   │ │   IMSI 28601... +500%   ││
│                    │  │ iot.scada    ██ 300G    │ │ ℹ 14:02 Bulk import done ││
│                    │  └──────────────────────────┘ │   Job #45: 12K SIMs ok  ││
│                    │                               └──────────────────────────┘│
│                    │  Quick Actions: [Import SIMs] [Create APN] [View Alerts] │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Drill-Down Map

| Data Element | Interaction | Target | Pattern |
|-------------|-------------|--------|---------|
| Active SIMs card | Click | SCR-020 SIM List (state=active) | Navigation |
| Sessions card | Click | SCR-050 Session List | Navigation |
| Auth/s card | Click | SCR-070 System Health | Navigation |
| Alerts card | Click | SCR-013 Anomaly Detection | Navigation |
| SIM Distribution chart segment | Click | SCR-020 SIM List (operator=X) | Navigation |
| Operator name in health | Click | SCR-040 Operator Detail | Navigation |
| Operator health % | Hover | Last 24h health timeline | Tooltip |
| APN name in top 5 | Click | SCR-032 APN Detail | Navigation |
| Alert feed row | Click | Anomaly detail or SIM detail | Navigation |
| Quick Action buttons | Click | SCR-020 import modal / SCR-031 create | Navigation/Modal |
| Traffic Heatmap cell | Hover | Tooltip: `<formatBytes(rawBytes)> @ <Day> HH:00` — raw byte total for that 7-day/hour bucket; uses `raw_bytes` from API-110 DTO (FIX-221) | Tooltip |
| Pool Utilization KPI | Render | Title always shows "(avg across all pools)" clarifier; subtitle conditionally shows `Top pool: <name> <pct>%` when `top_ip_pool` is non-null in API-110 response (FIX-221) | Subtitle |

## States

- **Loading:** Skeleton cards (shimmer), skeleton chart areas
- **Real-time:** Session count + auth/s update via WebSocket (1s interval), alert feed live push
- **Error:** Card shows "Failed to load" with retry button
- **Empty (new tenant):** Cards show 0, message "Complete setup to see data" → link to SCR-003

## API References

- API-110: GET /api/v1/dashboard
- API-101: GET /api/v1/sessions/stats
- API-023: GET /api/v1/operators/:id/health
- WebSocket: metrics.realtime, alert.new
