# SCR-032: APN Detail

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (sim_manager+)
**Route:** `/apns/:id`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  ← APNs    APN: iot.fleet                  [Edit] [Delete] │
│                    ├────────────────────────────────────────────────────────────┤
│                    │ KPI Row (FIX-222)                                          │
│                    │ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      │
│                    │ │ SIMs     │ │ Traffic  │ │ Top      │ │ APN      │      │
│                    │ │  1.2M    │ │  24h     │ │ Operator │ │ State    │      │
│                    │ │          │ │  45 TB   │ │ Turkcell │ │ ACTIVE   │      │
│                    │ └──────────┘ └──────────┘ └──────────┘ └──────────┘      │
│                    │ (Top Operator: based on first 50 SIMs — see D-119)        │
│                    ├────────────────────────────────────────────────────────────┤
│                    │ [◉ Overview] [Config] [IP Pools] [SIMs] [Traffic]         │
│                    │ [Policies] [Audit] [Alerts]                                │
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │ Overview tab (default, FIX-222 read-heavy-first):         │
│                    │   Config summary, APN metadata fields (read-only)         │
│                    │                                                            │
│                    │ Traffic tab — Traffic Over Time (30d)                     │
│                    │ ┌──────────────────────────────────────────────────────┐  │
│                    │ │ TB                                                    │  │
│                    │ │ 2├──╮     ╭──╮     ╭──╮                              │  │
│                    │ │ 1├──╯─────╯  ╰─────╯  ╰──                           │  │
│                    │ │  ├───┬───┬───┬───┬───┬───                            │  │
│                    │ │    W1   W2   W3   W4                                  │  │
│                    │ │  ── Download  ── Upload                               │  │
│                    │ └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │ SIM State Distribution              Data by RAT Type      │
│                    │ ┌────────────────────────┐ ┌────────────────────────────┐ │
│                    │ │ Active    ████████ 1.15M│ │ NB-IoT  ████ 8 TB        │ │
│                    │ │ Suspended ██ 45K        │ │ LTE-M   ████████ 30 TB   │ │
│                    │ │ Terminated█ 5K          │ │ LTE     ████ 7 TB        │ │
│                    │ └────────────────────────┘ └────────────────────────────┘ │
│                    │                                                            │
│                    │ Alerts tab (merged Notifications panel — FIX-222)         │
│                    │ InfoTooltip ⓘ wraps ICCID/IMSI/MSISDN in SIMs headers   │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## API References
- API-032: GET /api/v1/apns/:id
- API-035: GET /api/v1/apns/:id/sims
- API-082: GET /api/v1/ip-pools/:id
