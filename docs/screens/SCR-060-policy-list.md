# SCR-060: Policy List

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (policy_editor+)
**Route:** `/policies`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Policies                               [+ Create Policy] │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  Filter: [Scope ▼] [State ▼]                              │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Name▲            │Scope  │Ver │SIMs    │State  │ ⋮  │  │
│                    │  ├──────────────────┼───────┼────┼────────┼───────┼────┤  │
│                    │  │ iot-fleet-std    │Global │v3  │1.2M    │●Active│ ⋮  │  │
│                    │  │ iot-meter-lowbw  │APN    │v2  │800K    │●Active│ ⋮  │  │
│                    │  │ vehicle-premium  │APN    │v5  │345K    │◐Roll. │ ⋮  │  │
│                    │  │ pos-minimal      │APN    │v1  │120K    │●Active│ ⋮  │  │
│                    │  │ scada-priority   │Op     │v2  │80K     │●Active│ ⋮  │  │
│                    │  │ test-unlimited   │Global │v1  │10K     │●Active│ ⋮  │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Active Rollouts                                           │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ vehicle-premium v4→v5  Stage 2/3 (10%)              │  │
│                    │  │ ████████░░░░░░░░░░░░ 34,500 / 345,000 SIMs         │  │
│                    │  │ Started: 14:00 today    [Advance] [Pause] [Rollback]│  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## API References
- API-090: GET /api/v1/policies
- API-099: GET /api/v1/policy-rollouts/:id
