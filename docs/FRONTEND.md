# Frontend Design System — Argus

> Generated from approved HTML mockups. Reference for ALL UI implementation.
> Mockups: `docs/mockups/*.html`
> Skill: frontend-design MUST be used by Developer agent for implementation.

## Design Identity

**Argus Neon Dark** — Linear meets Bloomberg Terminal meets Vercel Dashboard.
NOC-ready, data-dense, premium enterprise aesthetic.
Dark-first with glass-morphism, neon accents, terminal-inspired data views.

## Color Palette

| Token | Value | Usage |
|-------|-------|-------|
| `--bg-primary` | `#06060B` | Page background, code editor bg |
| `--bg-surface` | `#0C0C14` | Cards, panels, sidebar |
| `--bg-elevated` | `#12121C` | Elevated surfaces, dropdowns, modals |
| `--bg-hover` | `#1A1A28` | Hover states, skeleton loaders |
| `--bg-active` | `#1E1E2E` | Active/selected states |
| `--bg-glass` | `rgba(12,12,20,0.75)` | Header, glass-morphism elements (+ backdrop-blur: 12px) |
| `--border` | `#1E1E30` | Primary borders |
| `--border-subtle` | `#16162A` | Table row borders, subtle dividers |
| `--text-primary` | `#E4E4ED` | Primary text |
| `--text-secondary` | `#7A7A95` | Secondary text, labels |
| `--text-tertiary` | `#4A4A65` | Muted text, placeholders |
| `--accent` | `#00D4FF` | Primary accent — CTAs, links, active states, neon glow |
| `--accent-dim` | `rgba(0,212,255,0.15)` | Accent backgrounds (active nav, selected row) |
| `--accent-glow` | `rgba(0,212,255,0.25)` | Card hover glow |
| `--success` | `#00FF88` | Success states, healthy, active SIM |
| `--success-dim` | `rgba(0,255,136,0.12)` | Success backgrounds |
| `--warning` | `#FFB800` | Warnings, degraded, suspended |
| `--warning-dim` | `rgba(255,184,0,0.12)` | Warning backgrounds |
| `--danger` | `#FF4466` | Errors, critical, terminated |
| `--danger-dim` | `rgba(255,68,102,0.12)` | Danger backgrounds |
| `--purple` | `#A855F7` | Secondary accent — eSIM, Vodafone, charts |
| `--info` | `#6C8CFF` | Informational |

### Syntax Highlighting (Policy DSL Editor)

| Token | Value | Usage |
|-------|-------|-------|
| `--syntax-keyword` | `#FF79C6` | POLICY, MATCH, WHEN, ACTION, RULES, CHARGING |
| `--syntax-string` | `#F1FA8C` | String literals |
| `--syntax-number` | `#BD93F9` | Numbers, units (1mbps, 500MB) |
| `--syntax-comment` | `#6272A4` | Comments |
| `--syntax-function` | `#50FA7B` | Functions (notify, throttle) |
| `--syntax-operator` | `#FF6E6E` | Operators (=, >, IN) |
| `--syntax-type` | `#8BE9FD` | Types (nb_iot, postpaid) |

## Typography

| Token | Value | Usage |
|-------|-------|-------|
| `--font-ui` | `'Inter', -apple-system, system-ui, sans-serif` | All UI text |
| `--font-mono` | `'JetBrains Mono', 'Fira Code', monospace` | Data values, ICCID/IMSI, code, metrics |
| Font size base | `14px` (html root) | Body text |
| Metric values | `28px` mono bold | Dashboard metric cards |
| Table data | `13px` | Table cell content |
| Table mono data | `12px` mono | ICCID, IMSI, IP addresses |
| Labels | `12px` 500 weight | Form labels, card labels |
| Section labels | `10-11px` uppercase, `letter-spacing: 0.5-1.5px` | Nav sections, stat labels |
| Headings | `15-16px` 600 weight | Page titles |

## Spacing

Base unit: `4px`

| Token | Value |
|-------|-------|
| `--space-1` | `4px` |
| `--space-2` | `8px` |
| `--space-3` | `12px` |
| `--space-4` | `16px` |
| `--space-5` | `20px` |
| `--space-6` | `24px` |
| `--space-8` | `32px` |
| `--space-12` | `48px` |

## Layout

- **Navigation:** Fixed sidebar (240px expanded, 64px collapsed) + sticky header
- **Sidebar width:** `--sidebar-w: 240px` (collapsed: 64px)
- **Header height:** `--header-h: 56px`
- **Content padding:** `24px`
- **Metric grid:** `repeat(4, 1fr)` with `16px` gap
- **Panel grid:** `repeat(2, 1fr)` with `16px` gap
- **Max content width:** None (fluid, sidebar constrains left)
- **Breakpoints:** Desktop (≥1280px full), Tablet (768-1279px collapsed sidebar), Mobile (≤767px hidden sidebar)

## Component Tokens

| Token | Value |
|-------|-------|
| `--radius-sm` | `6px` — Buttons, inputs, badges, nav items |
| `--radius-md` | `10px` — Cards, panels, tables |
| `--radius-lg` | `14px` — Modals, large cards |
| `--radius-xl` | `18px` — Full-page overlays |
| `--shadow-glow` | `0 0 20px rgba(0,212,255,0.08)` — Accent card hover |
| `--shadow-card` | `0 2px 8px rgba(0,0,0,0.3), 0 0 1px rgba(255,255,255,0.05)` — Card elevation |
| `--transition` | `0.2s cubic-bezier(0.4, 0, 0.2, 1)` — All transitions |

## Key Visual Patterns

### Card Hover Effect
- `transform: translateY(-2px)`
- Border color transitions to `--accent`
- `box-shadow: var(--shadow-glow)`
- Bottom 2px accent bar on metric cards

### Status Indicators
- Pulsing dot (`animation: pulse 2s infinite`) for live/active states
- Color-coded dots: green (healthy/active), yellow (warning/degraded), red (critical/down)
- Glow effect: `box-shadow: 0 0 6-8px` matching color at 40% opacity

### Glass-morphism
- Header: `background: var(--bg-glass); backdrop-filter: blur(12px)`
- Used sparingly — header only, not on all cards

### Neon Glow
- Logo: `box-shadow: 0 0 16px rgba(0,212,255,0.3)`
- Accent buttons on hover: `box-shadow: 0 0 20px rgba(0,212,255,0.3)`
- Status dots: subtle glow matching their color

### Ambient Background
- Subtle radial gradients fixed to viewport (cyan, purple, green at very low opacity 2-3%)
- Creates depth without distraction

### Data Table Pattern
- Compact rows, mono font for data values
- Row hover: `var(--bg-hover)`
- Selected row: `var(--accent-dim)` background
- Checkbox column, sort indicators, row actions (⋮)
- Bulk action bar slides up on selection

### Sparklines
- Flex container with thin bars (2px gap, 2px border-radius top)
- Color matches metric accent, opacity varies by value
- 12 bars representing recent trend

## Dark/Light Mode

- **Default:** Dark (Neon Dark theme)
- **Light mode:** Available via toggle (sidebar bottom)
- Light overrides: swap bg/text tokens, reduce glow effects, mute neon accents

## Reference Mockups

| File | Screen | Shows |
|------|--------|-------|
| `docs/mockups/01-dashboard.html` | SCR-010: Dashboard | Full layout, sidebar, header, metric cards, panels, alert feed, sparklines |
| `docs/mockups/02-sim-list.html` | SCR-020: SIM List | Collapsed sidebar, filter bar, segments, data table, bulk actions, pagination |
| `docs/mockups/03-policy-editor.html` | SCR-062: Policy Editor | Split pane, code editor with syntax highlighting, dry-run preview, version tabs, action bar |

## Implementation Notes

- Use `tailwindcss` with custom theme extending these tokens
- Use `shadcn/ui` components as base, restyled to match this system
- All interactive elements must have hover states
- Staggered animations for list items (`animation-delay: calc(var(--i) * 50ms)`)
- `Recharts` for all charts with custom dark theme
- `TanStack Table` for data tables with virtual scrolling
- `Monaco Editor` or `CodeMirror` for Policy DSL editor with custom Argus theme
