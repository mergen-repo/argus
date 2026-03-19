# SCR-090: Audit Log

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (tenant_admin+)
**Route:** `/audit`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Audit Log                    [Verify Integrity] [Export]  │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  Date: [Mar 18 ▼] to [Mar 18 ▼]  [User ▼] [Action ▼]     │
│                    │  [Entity Type ▼] [Entity ID: ______]   [Search]           │
│                    │                                                            │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Time     │ User    │ Action       │ Entity    │ Diff │  │
│                    │  ├──────────┼─────────┼──────────────┼───────────┼──────┤  │
│                    │  │ 14:35:02 │ Bora T. │ state_change │ SIM 899.. │ [▶]  │  │
│                    │  │ 14:30:15 │ System  │ policy_roll  │ Policy v5 │ [▶]  │  │
│                    │  │ 14:23:00 │ System  │ coa_sent     │ Session.. │ [▶]  │  │
│                    │  │ 14:15:22 │ Bora T. │ update       │ APN fleet │ [▶]  │  │
│                    │  │ 14:02:00 │ System  │ bulk_op      │ Job #48   │ [▶]  │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  ▼ Expanded: state_change on SIM 8990111234567890          │
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ Correlation ID: a1b2c3d4-e5f6-7890                   │  │
│                    │  │ IP: 192.168.1.100  │  UA: Chrome/120                 │  │
│                    │  │ Hash: 5a3f...  │  Prev Hash: 8b2e...                 │  │
│                    │  │                                                      │  │
│                    │  │ Before:                    After:                     │  │
│                    │  │ ┌───────────────────┐     ┌───────────────────┐      │  │
│                    │  │ │ state: "active"   │     │ state: "suspended"│      │  │
│                    │  │ │ suspended_at: null│     │ suspended_at:     │      │  │
│                    │  │ │                   │     │  "2026-03-18T14:35│      │  │
│                    │  │ └───────────────────┘     └───────────────────┘      │  │
│                    │  │                                                      │  │
│                    │  │ [View Entity]  [Copy Hash]  [View Full JSON]         │  │
│                    │  └──────────────────────────────────────────────────────┘  │
│                    │                                                            │
│                    │  Hash Chain Status: ✅ Verified (last check: 14:00)        │
│                    │  ◀ Prev  1 2 3 ... 234  Next ▶                            │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## API References
- API-140: GET /api/v1/audit-logs
- API-141: GET /api/v1/audit-logs/verify
- API-142: POST /api/v1/audit-logs/export
