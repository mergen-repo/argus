# SCR-011: Analytics — Usage Dashboard

**Type:** Page (tabbed: Usage | Cost | Anomalies)
**Layout:** DashboardLayout
**Auth:** JWT (analyst+)
**Route:** `/analytics`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Analytics                                    🔍 ⌘K  👤   │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  [◉ Usage] [○ Cost] [○ Anomalies]    Period: [Last 7d ▼]  │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  Filters: [Operator ▼] [APN ▼] [RAT ▼]  Applied: 0       │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Data Usage Over Time                                │  │
│                    │  │  TB                                                  │  │
│                    │  │  4├─────────────────╮          ╭──────              │  │
│                    │  │  3├───────╮         │         ╱                     │  │
│                    │  │  2├──╮    │    ╭────╯    ╭───╯                      │  │
│                    │  │  1├──╯────╯────╯         │                          │  │
│                    │  │  0├───┬───┬───┬───┬───┬──┬──                        │  │
│                    │  │    Mon Tue Wed Thu Fri Sat Sun                       │  │
│                    │  │  ── Turkcell  ── Vodafone  ── TT Mobile             │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  ┌────────────────────────┐ ┌────────────────────────────┐│
│                    │  │ By RAT Type            │ │ By APN                     ││
│                    │  │ NB-IoT    ████ 35%     │ │ iot.fleet   ████████ 45%  ││
│                    │  │ LTE-M     ██████ 40%   │ │ iot.meter   ██████ 30%   ││
│                    │  │ LTE       ████ 20%     │ │ iot.vehicle ███ 15%      ││
│                    │  │ 5G NR     █ 5%         │ │ Other       ██ 10%       ││
│                    │  └────────────────────────┘ └────────────────────────────┘│
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Top Consumers (SIMs by data usage)        [Export ▼]│  │
│                    │  │ ICCID           │ IMSI      │ APN    │ Usage  │ ⋮  │  │
│                    │  │ 89901112...     │ 28601...  │ fleet  │ 45 GB  │ ⋮  │  │
│                    │  │ 89901113...     │ 28602...  │ fleet  │ 38 GB  │ ⋮  │  │
│                    │  │ 89901114...     │ 28601...  │ veh.   │ 32 GB  │ ⋮  │  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Drill-Down Map

| Data Element | Interaction | Target | Pattern |
|-------------|-------------|--------|---------|
| Chart line (operator) | Click | Filter by that operator | Filter update |
| RAT type bar | Click | Filter by RAT type | Filter update |
| APN bar | Click | SCR-032 APN Detail | Navigation |
| Top consumer row | Click | SCR-021 SIM Detail | Navigation |
| Export button | Click | CSV download (API-115) | Download |
| Period dropdown | Change | Re-fetch all charts | Reload |

## API References

- API-111: GET /api/v1/analytics/usage
- API-115: POST /api/v1/cdrs/export
