# SCR-121: Tenant Management

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (super_admin only)
**Route:** `/system/tenants`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Tenant Management                      [+ Create Tenant] │
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │  ┌──────────┐ ┌──────────┐ ┌──────────┐                   │
│                    │  │ Total    │ │ Active   │ │ Total    │                   │
│                    │  │ Tenants  │ │ SIMs     │ │ Users    │                   │
│                    │  │    12    │ │  9.8M    │ │   156    │                   │
│                    │  └──────────┘ └──────────┘ └──────────┘                   │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Tenant        │ SIMs   │ Users│ Operators│ State│ ⋮ │  │
│                    │  ├───────────────┼────────┼──────┼──────────┼──────┼───┤  │
│                    │  │ Acme Energy   │ 4.2M   │ 24   │ TC,VF,TT │●Active│⋮│  │
│                    │  │ Fleet Corp    │ 2.1M   │ 12   │ TC,VF    │●Active│⋮│  │
│                    │  │ Smart Grid Co │ 1.8M   │ 18   │ TC,TT    │●Active│⋮│  │
│                    │  │ IoT Solutions │ 980K   │ 8    │ VF       │●Active│⋮│  │
│                    │  │ Test Tenant   │ 10K    │ 3    │ Mock     │●Active│⋮│  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Resource Usage by Tenant                                  │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Tenant      │ SIM Quota    │ APN Quota  │ Data (30d)│  │
│                    │  ├─────────────┼──────────────┼────────────┼───────────┤  │
│                    │  │ Acme Energy │ 4.2M / 5M   │ 8 / 100    │ 125 TB    │  │
│                    │  │             │ ████████████░│ █░░░░░░░░░ │           │  │
│                    │  │ Fleet Corp  │ 2.1M / 3M   │ 5 / 50     │ 68 TB     │  │
│                    │  │             │ ██████████░░░│ █░░░░░░░░░ │           │  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Row Actions (⋮)
- Edit tenant (name, limits, contact)
- Manage operator grants
- Suspend tenant
- View as tenant (impersonate)
- Delete tenant (with type-to-confirm)

## API References
- API-010 to API-014
