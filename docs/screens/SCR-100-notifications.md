# SCR-100: Notification Center

**Type:** Drawer (slides from right on bell icon click) + Page for full view
**Layout:** Overlay on any page / DashboardLayout for full view
**Auth:** JWT (any)
**Route:** `/notifications` (full page) or drawer overlay

## Drawer Mockup (bell icon click)

```
                              ┌──────────────────────────────┐
                              │ Notifications          🔔 5  │
                              │ [Mark All Read]              │
                              ├──────────────────────────────┤
                              │                              │
                              │ 🔴 NOW                       │
                              │ ┌────────────────────────┐  │
                              │ │⚠ SLA Violation          │  │
                              │ │TT Mobile < 99.9% target│  │
                              │ │14:23 • operator         │  │
                              │ └────────────────────────┘  │
                              │ ┌────────────────────────┐  │
                              │ │⚠ Anomaly Detected       │  │
                              │ │Data spike on APN fleet  │  │
                              │ │14:15 • apn              │  │
                              │ └────────────────────────┘  │
                              │                              │
                              │ TODAY                        │
                              │ ┌────────────────────────┐  │
                              │ │ℹ Bulk Import Complete   │  │
                              │ │Job #48: 12K SIMs ok     │  │
                              │ │14:02 • system           │  │
                              │ └────────────────────────┘  │
                              │ ┌────────────────────────┐  │
                              │ │ℹ Policy Rollout 10%    │  │
                              │ │vehicle-premium at stage2│  │
                              │ │13:45 • policy           │  │
                              │ └────────────────────────┘  │
                              │ ┌────────────────────────┐  │
                              │ │⚠ IP Pool Warning       │  │
                              │ │fleet-pool at 82%        │  │
                              │ │12:00 • apn              │  │
                              │ └────────────────────────┘  │
                              │                              │
                              │ [View All Notifications →]  │
                              └──────────────────────────────┘
```

## States
- **Unread:** Bold text, blue dot indicator
- **Read:** Normal weight, no dot
- **Critical:** Red severity bar on left
- **Live:** New notifications push via WebSocket with slide-in animation

## API References
- API-130: GET /api/v1/notifications
- API-131: PATCH /api/v1/notifications/:id/read
- API-132: POST /api/v1/notifications/read-all
- WebSocket: notification.new
