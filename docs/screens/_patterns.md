# UI Pattern Library — Argus

> All screens MUST follow these patterns. No exceptions.
> frontend-design skill MUST be used during implementation for premium aesthetics.

## Design Philosophy

- **Dark-first**: NOC-ready, 7/24 monitoring environment
- **Data-dense**: Maximize information per pixel, compact row heights
- **Group-first**: Navigate by segments/groups, drill-down to individual
- **Real-time**: Live indicators, WebSocket-driven updates, no stale data
- **Premium**: Neon accents, sleek animations, terminal-inspired data views

## Grid System

- **Page layout**: Fixed sidebar (240px collapsed 64px) + scrollable content area
- **Content grid**: 12-column CSS grid, 24px gap
- **Card grid**: Auto-fit, min 320px per card
- **Spacing scale**: 4, 8, 12, 16, 24, 32, 48, 64px
- **Max content width**: 1440px (centered on larger screens)

## Color System (Dark Mode)

| Role | Token | Value |
|------|-------|-------|
| Background | bg-primary | #0A0A0F |
| Surface | bg-surface | #12121A |
| Surface elevated | bg-elevated | #1A1A25 |
| Border | border-default | #2A2A3A |
| Text primary | text-primary | #E8E8ED |
| Text secondary | text-secondary | #8888A0 |
| Accent | accent | #00D4FF (cyan neon) |
| Success | success | #00FF88 |
| Warning | warning | #FFB800 |
| Danger | danger | #FF4466 |
| Info | info | #6C8CFF |

## Data Display Patterns

### Data Table (primary pattern — used for SIMs, APNs, sessions, jobs, audit)

```
┌─────────────────────────────────────────────────────────────────────┐
│ [Segment: All Active ▼]  [+ Add Filter]  [⌘K Search]    [Export ▼]│
├─────────────────────────────────────────────────────────────────────┤
│ Applied: operator=Turkcell × state=active ×    Clear all (2)       │
├─────────────────────────────────────────────────────────────────────┤
│ ☐ │ ICCID▲        │ IMSI          │ Operator │ APN    │ State  │⋮ │
├───┼────────────────┼───────────────┼──────────┼────────┼────────┼──┤
│ ☐ │ 8990111...    │ 28601...      │ 🟢 TCell │ iot.fl │ ● ACT  │⋮ │
│ ☐ │ 8990112...    │ 28602...      │ 🟡 Voda  │ iot.mt │ ● ACT  │⋮ │
│ ☑ │ 8990113...    │ 28603...      │ 🟢 TCell │ iot.fl │ ◐ SUSP │⋮ │
├─────────────────────────────────────────────────────────────────────┤
│ ┌─ Bulk Actions (1 selected): [Activate] [Suspend] [Assign Policy]│
├─────────────────────────────────────────────────────────────────────┤
│ Showing 1-50 of 2,345,678  │  ◀ Prev  [1] 2 3 ... 46,914  Next ▶│
└─────────────────────────────────────────────────────────────────────┘
```

Rules:
- Segment dropdown as primary filter (saved filters)
- Filter chips below header, removable with ×
- Checkbox column for bulk selection
- Sort indicator on column headers (▲▼)
- Row overflow menu (⋮) for single-item actions
- Bulk action bar appears on selection
- Cursor-based pagination (not offset)
- Real-time status indicators (● colored dots)
- Column resizing via drag

### Detail View

```
┌─────────────────────────────────────────────────────────────────────┐
│ ← Back to SIM List    SIM: 8990111234567890          [⋮ Actions ▼]│
├───────────────────────────────┬─────────────────────────────────────┤
│                               │                                     │
│ State: ● ACTIVE               │ Quick Stats                        │
│ Operator: 🟢 Turkcell        │ ┌─────────┐ ┌─────────┐           │
│ APN: iot.fleet                │ │ Sessions│ │ Data    │           │
│ IMSI: 286010123456789         │ │    12   │ │ 2.3 GB  │           │
│ MSISDN: +905321234567         │ └─────────┘ └─────────┘           │
│ Policy: iot-fleet-std v3      │ ┌─────────┐ ┌─────────┐           │
│ IP: 10.0.1.42 (static)       │ │ Cost/mo │ │ Uptime  │           │
│ RAT: LTE-M                   │ │ ₺45.20  │ │ 99.8%   │           │
│ eSIM: EID abc123...           │ └─────────┘ └─────────┘           │
│                               │                                     │
├───────────────────────────────┴─────────────────────────────────────┤
│ [Overview] [Sessions] [Usage] [State History] [Diagnostics]        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│ (Tab content area — each tab is a separate mockup)                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

Rules:
- Back navigation + entity identifier in header
- Actions dropdown (top-right): Suspend, Terminate, Diagnose, Switch Operator
- Left panel: key attributes (always visible)
- Right panel: quick stats cards
- Tabbed content below
- Each tab has its own full mockup

### Dashboard Cards

```
┌─────────────────────┐
│ ○ Total Active SIMs │
│                     │
│    2,345,678        │
│    ▲ +12,345 (24h)  │
│    ▁▂▃▄▅▆▇█▇▆▅▃▂▁  │  ← sparkline
└─────────────────────┘
```

Rules:
- Status indicator dot (○ = normal, ● = alert)
- Metric name (secondary text)
- Large value (primary text)
- Trend indicator (▲ green up, ▼ red down, ─ neutral)
- Mini sparkline (7-day trend)
- Clickable → navigates to detail

## Form Patterns

### Create/Edit Form

```
┌─────────────────────────────────────────────────┐
│ Create New APN                         [×Close] │
├─────────────────────────────────────────────────┤
│                                                  │
│ APN Name *                                       │
│ ┌──────────────────────────────────────────────┐│
│ │ iot.fleet                                     ││
│ └──────────────────────────────────────────────┘│
│                                                  │
│ Operator *                                       │
│ ┌──────────────────────────────────────────────┐│
│ │ Turkcell                              ▼      ││
│ └──────────────────────────────────────────────┘│
│                                                  │
│ APN Type *                                       │
│ ┌──────────────────────────────────────────────┐│
│ │ ○ Private Managed  ● Operator Managed        ││
│ │ ○ Customer Managed                           ││
│ └──────────────────────────────────────────────┘│
│                                                  │
│ Supported RAT Types                              │
│ [☑ NB-IoT] [☑ LTE-M] [☐ LTE] [☐ 5G NR]       │
│                                                  │
│              [Cancel]  [Create APN]              │
└─────────────────────────────────────────────────┘
```

Rules:
- Labels above inputs
- Required fields marked with *
- Validation errors below field (red text)
- Cancel (secondary) left, Submit (primary) right
- Drawer/modal for create, inline for edit
- Form auto-saves draft every 30s (for complex forms like policy DSL)

### Filter Bar

```
┌──────────────────────────────────────────────────────────────┐
│ [Segment ▼] [+ Operator ▼] [+ State ▼] [+ APN ▼] [+ RAT ▼]│
│                                                              │
│ Applied: operator:Turkcell × state:active ×  Clear all (2)  │
└──────────────────────────────────────────────────────────────┘
```

Rules:
- Saved segment as primary (dropdown)
- Additional filters as additive chips
- Each filter opens a dropdown with options
- Applied filters shown as removable chips
- "Clear all" with count

## Navigation Patterns

### Sidebar

```
┌────────────────────┐
│  ◆ ARGUS           │
│                    │
│ ─ MAIN ──────────  │
│ ◉ Dashboard        │
│ ○ SIMs             │
│ ○ APNs             │
│ ○ Operators        │
│ ○ Policies         │
│ ○ eSIM Profiles    │
│                    │
│ ─ MONITORING ────  │
│ ○ Sessions  🔴 42K │
│ ○ Analytics        │
│ ○ Jobs      ⏳ 3   │
│                    │
│ ─ SYSTEM ────────  │
│ ○ Audit Log        │
│ ○ Notifications 🔔5│
│ ○ Settings   ▶     │
│                    │
│ ─────────────────  │
│ ○ System Health    │
│                    │
│ ┌────────────────┐ │
│ │ ☾ Dark  ☼ Light│ │
│ └────────────────┘ │
│                    │
│  👤 Bora T.   ▼   │
└────────────────────┘
```

Rules:
- Logo top, user bottom
- Grouped sections with labels
- Active item: filled circle (◉) + accent color
- Badge counts for live items (sessions, notifications, running jobs)
- Collapsible to icon-only (64px)
- Theme toggle at bottom
- Settings has sub-menu (▶ indicator)

### Command Palette (Ctrl+K)

```
┌─────────────────────────────────────────┐
│ 🔍 Search SIMs, APNs, commands...      │
├─────────────────────────────────────────┤
│ RECENT                                  │
│   ↗ SIM 8990111234567890               │
│   ↗ APN iot.fleet                      │
│                                        │
│ COMMANDS                               │
│   ⚡ Create new SIM                     │
│   ⚡ Create new APN                     │
│   ⚡ Run bulk import                    │
│                                        │
│ PAGES                                  │
│   📄 Dashboard                          │
│   📄 Analytics                          │
│   📄 Settings                           │
└─────────────────────────────────────────┘
```

## Action Patterns

| Action | Placement | Style | Confirmation |
|--------|-----------|-------|-------------|
| Primary (Create, Import) | Top-right of page | Accent button | No |
| Secondary (Export, Filter) | Top-right, next to primary | Ghost button | No |
| Row action (Edit) | Row overflow ⋮ menu | Menu item | No |
| Row action (Suspend) | Row overflow ⋮ menu | Menu item (warning) | Dialog |
| Row action (Terminate) | Row overflow ⋮ menu | Menu item (danger) | Dialog + type-to-confirm |
| Bulk action | Appears on selection | Toolbar buttons | Dialog |
| Undo | Toast with action | "Undo" link in toast | No (auto-expires 10s) |

## Feedback Patterns

### Toast

```
┌─────────────────────────────────────┐
│ ✓ 2,345 SIMs activated successfully │
│   View details    Undo         [×]  │
└─────────────────────────────────────┘
Position: bottom-right, stacked
Duration: 5s (info), 10s (warning), persistent (error)
```

### Empty State

```
┌─────────────────────────────────────┐
│                                     │
│          📡                          │
│                                     │
│    No SIMs found                    │
│    Import your first batch of SIMs  │
│    to get started.                  │
│                                     │
│       [Import SIMs]                 │
│                                     │
└─────────────────────────────────────┘
```

### Loading Skeleton

```
┌─────────────────────────────────────┐
│ ░░░░░░░░░░░░░ │ ░░░░░░░ │ ░░░░░░  │
│ ░░░░░░░░░░░░░ │ ░░░░░░░ │ ░░░░░░  │
│ ░░░░░░░░░░░░░ │ ░░░░░░░ │ ░░░░░░  │
│ ░░░░░░░░░░░░░ │ ░░░░░░░ │ ░░░░░░  │
└─────────────────────────────────────┘
Animated shimmer effect on skeleton blocks
```

## Icon Conventions

| Action | Icon | Consistent everywhere |
|--------|------|----------------------|
| Edit | ✏️ pencil | Yes |
| Delete/Terminate | 🗑️ trash | Yes |
| View detail | 👁️ eye | Yes |
| Add/Create | ➕ plus | Yes |
| Filter | 🔽 funnel | Yes |
| Search | 🔍 magnifier | Yes |
| Export | ⬇️ download | Yes |
| Import | ⬆️ upload | Yes |
| Refresh | 🔄 refresh | Yes |
| Settings | ⚙️ gear | Yes |
| Notification | 🔔 bell | Yes |
| Health/Status | ● colored dot | Yes |
| Live/Real-time | ◉ pulsing dot | Yes |

## Responsive Behavior

- **Desktop-first** (≥1280px): Full layout, sidebar expanded
- **Tablet** (768-1279px): Sidebar collapsed to icons, content fluid
- **Mobile** (≤767px): Sidebar hidden (hamburger menu), stacked cards, simplified tables
- Tables on mobile: card view instead of rows
