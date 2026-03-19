# SCR-062: Policy Editor (DSL)

**Type:** Page
**Layout:** DashboardLayout (full-width content, no cards)
**Auth:** JWT (policy_editor+)
**Route:** `/policies/:id`

## Mockup

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  ← Policies   iot-fleet-standard          [⋮ Actions ▼]   │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  Version: [v2 ●Active] [v3 Draft ◉] [+ New Version]       │
│                    ├────────────────────────────┬───────────────────────────────┤
│                    │  Policy DSL Editor         │ Preview / Dry-Run            │
│                    │                            │                               │
│                    │  ┌────────────────────────┐│ Affected SIMs: 1,234,567     │
│                    │  │ 1│POLICY "iot-fleet" { ││                               │
│                    │  │ 2│  MATCH {            ││ By Operator:                  │
│                    │  │ 3│    apn IN ("iot.fl" ││ ┌────────────────────────┐   │
│                    │  │ 4│    rat_type IN (nb_i││ │ Turkcell   ████ 800K  │   │
│                    │  │ 5│  }                  ││ │ Vodafone   ██ 300K    │   │
│                    │  │ 6│                     ││ │ TT Mobile  █ 134K     │   │
│                    │  │ 7│  RULES {            ││ └────────────────────────┘   │
│                    │  │ 8│    bandwidth_down =  ││                               │
│                    │  │ 9│    bandwidth_up = 25 ││ By RAT Type:                  │
│                    │  │10│                     ││ ┌────────────────────────┐   │
│                    │  │11│    WHEN usage > 500M││ │ NB-IoT     ████ 600K  │   │
│                    │  │12│      bandwidth_down ││ │ LTE-M      ██████ 500K│   │
│                    │  │13│      ACTION notify( ││ │ LTE        ██ 134K    │   │
│                    │  │14│    }                ││ └────────────────────────┘   │
│                    │  │15│                     ││                               │
│                    │  │16│    WHEN usage > 1GB ││ Estimated Impact:              │
│                    │  │17│      ACTION throttle││ ┌────────────────────────┐   │
│                    │  │18│    }                ││ │ SIMs throttled: ~12%   │   │
│                    │  │19│  }                  ││ │ Quota alerts: ~45%     │   │
│                    │  │20│                     ││ │ Cost change: -₺85K/mo  │   │
│                    │  │21│  CHARGING {         ││ └────────────────────────┘   │
│                    │  │22│    model = postpaid ││                               │
│                    │  │23│    rate = 0.01/MB   ││                               │
│                    │  │24│  }                  ││                               │
│                    │  │25│}                    ││                               │
│                    │  └────────────────────────┘│                               │
│                    │                            │                               │
│                    │  ⚠ Line 8: syntax valid   │                               │
│                    │  ✅ DSL compiles OK        │                               │
│                    ├────────────────────────────┴───────────────────────────────┤
│                    │ [Save Draft] [Run Dry-Run] [Activate (Immediate)] [Staged Rollout ▼] │
└────────────────────┴────────────────────────────────────────────────────────────┘
```

## Features
- **Code editor**: Syntax highlighting for Policy DSL, line numbers, error markers
- **Auto-save**: Draft saved every 30s
- **Live validation**: DSL parsed on every change, errors shown inline
- **Split pane**: Editor left, preview/dry-run right
- **Dry-run**: Shows affected SIM count, distribution, estimated cost impact
- **Diff view**: Compare v2 vs v3 changes

## API References
- API-092: GET /api/v1/policies/:id
- API-093: POST /api/v1/policies/:id/versions
- API-094: POST /api/v1/policy-versions/:id/dry-run
- API-095: POST /api/v1/policy-versions/:id/activate
- API-096: POST /api/v1/policy-versions/:id/rollout
