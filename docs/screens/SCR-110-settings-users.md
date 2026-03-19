# SCR-110: Settings — Users & Roles

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (tenant_admin+)
**Route:** `/settings/users`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Settings > Users & Roles                  [+ Invite User]│
│                    ├────────────────────────────────────────────────────────────┤
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Name         │ Email           │ Role      │ 2FA│ ⋮ │  │
│                    │  ├──────────────┼─────────────────┼───────────┼────┼───┤  │
│                    │  │ Bora Topcu   │ bora@acme.com   │ T. Admin  │ 🔒 │ ⋮ │  │
│                    │  │ Ahmet Yilmaz │ ahmet@acme.com  │ SIM Mgr   │ 🔒 │ ⋮ │  │
│                    │  │ Elif Kaya    │ elif@acme.com   │ Policy Ed │ ○  │ ⋮ │  │
│                    │  │ Mehmet Demir │ mehmet@acme.com │ Analyst   │ 🔒 │ ⋮ │  │
│                    │  │ api-fleet    │ —               │ API User  │ —  │ ⋮ │  │
│                    │  │ ✉ Pending    │ zeynep@acme.com │ Op Mgr    │ —  │ ⋮ │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Resource Usage                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Users: 6 / 50 max  │ SIMs: 2.3M / 5M max           │  │
│                    │  │ APNs: 8 / 100 max  │                                │  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Row Actions (⋮)
- Edit role, Disable account, Reset password, Force logout, Remove 2FA

## API References
- API-006: GET /api/v1/users
- API-007: POST /api/v1/users
- API-008: PATCH /api/v1/users/:id
