# SCR-021e: SIM Detail — State History Tab

**Type:** Tab content
**Auth:** JWT (sim_manager+)

## Mockup

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ [Overview] [Sessions] [Usage] [◉ History] [Diagnostics]                      │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│ State Timeline                                                               │
│                                                                              │
│  ORDERED ──●── ACTIVE ──●── SUSPENDED ──●── ACTIVE ──●── (current)          │
│  Jan 10     Jan 10       Feb 15          Feb 16                              │
│                                                                              │
│ Transition Log                                           [Export CSV]        │
│ ┌──────────────────────────────────────────────────────────────────────────┐│
│ │ Date            │ From     │ To       │ Trigger  │ By         │ Reason  ││
│ ├─────────────────┼──────────┼──────────┼──────────┼────────────┼─────────┤│
│ │ Feb 16 09:00    │ SUSPENDED│ ACTIVE   │ User     │ Bora T.    │ Manual  ││
│ │ Feb 15 14:30    │ ACTIVE   │ SUSPENDED│ Policy   │ System     │ FUP 1GB ││
│ │ Jan 10 11:00    │ ORDERED  │ ACTIVE   │ Bulk Job │ Job #12    │ Import  ││
│ │ Jan 10 10:55    │ —        │ ORDERED  │ Bulk Job │ Job #12    │ CSV row ││
│ └──────────────────────────────────────────────────────────────────────────┘│
│                                                                              │
│ Data Usage History (monthly summary)                                        │
│ ┌──────────────────────────────────────────────────────────────────────────┐│
│ │ Month    │ Download │ Upload  │ Sessions │ Cost   │ Operator │ RAT     ││
│ ├──────────┼──────────┼─────────┼──────────┼────────┼──────────┼─────────┤│
│ │ Mar 2026 │ 45.2 GB  │ 8.1 GB  │ 89       │ ₺45.20│ Turkcell │ LTE-M  ││
│ │ Feb 2026 │ 38.7 GB  │ 6.9 GB  │ 76       │ ₺38.70│ Turkcell │ LTE-M  ││
│ │ Jan 2026 │ 42.1 GB  │ 7.5 GB  │ 82       │ ₺42.10│ Turkcell │ LTE-M  ││
│ └──────────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
```

## API References
- API-050: GET /api/v1/sims/:id/history
- API-052: GET /api/v1/sims/:id/usage
