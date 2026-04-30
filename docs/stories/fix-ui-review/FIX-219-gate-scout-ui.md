<SCOUT-UI-FINDINGS>

## UI Scope
- Story has UI: YES
- Screens tested: 23 pages adopting EntityLink (sessions, esim, jobs, audit, alerts, violations, analytics-cost, analytics-anomalies, cost-attribution-tab, security-events, purge-history, sessions-global, tenant-resources, dsar, maintenance, api-usage, dashboard, topology, roaming, sms, deploys, event-source-chips, notifications) + sims/detail + policies/assigned-sims-tab

## Enforcement Summary
| Check | Matches |
|-------|---------|
| Hardcoded hex colors | 0 |
| Arbitrary pixel values | 2 (`text-[12px]`, `text-[11px]`, `text-[13px]`, `text-[10px]` inside EntityLink+HoverCard — intentional per plan token map) |
| Raw HTML elements | 0 (button only via PopoverTrigger in ui/) |
| Competing UI library imports | 0 |
| Default Tailwind colors | 0 |
| Inline SVG | 0 (lucide-react only) |
| Missing elevation | 0 |

## Visual Quality Score
| Criterion | Score |
|-----------|-------|
| Design Tokens | PASS (arbitrary px documented in plan design-token map; all colors via `text-accent`, `text-text-*`, `bg-bg-elevated`) |
| Typography | PASS (font-mono 12px consistent across all 29 adopting files) |
| Spacing | PASS (gap-1 between icon+label; mt-0.5 chip row) |
| Color | PASS (accent link, muted em-dash, tertiary icon default) |
| Components | PASS (lucide icons, Popover primitive, Tooltip, toast) |
| Empty States | PASS (orphan renders `—` with title tooltip) |
| Loading States | PASS (HoverCard shows "Loading..." while fetching) |
| Error States | PASS (HoverCard shows "Entity not found" on fetch error) |
| Interactive States | PASS (hover underline + accent/80; focus-visible:ring-2 ring-ring) |
| Tables | PASS (EntityLink wrapped with stopPropagation where row click exists) |
| Forms | N/A |
| Icons | PASS (w-3.5 h-3.5, text-text-tertiary, lucide consistent set) |
| Shadows/Elevation | PASS (shadow-lg on HoverCard popover) |
| Transitions | PASS (transition-colors duration-200) |
| Responsive | PASS (inline-flex + shrink-0 on icon prevents overflow) |

## Screen Mockup Compliance
- All 23 target pages import EntityLink (verified via grep — 29 files total adopt the primitive, exceeding plan scope).
- FRONTEND.md `## Entity Reference Pattern` section complete (L178–251) with all 9 subsections: when-to-use table, canonical call shape, route map, icon map, orphan rule, UUID-only zones, hover card opt-in, right-click copy, component boundary.

## Findings

### F-U1 | HIGH | ui
- Title: Audit list falls back to raw UUID slice when `entity_type` missing
- Location: `web/src/pages/audit/index.tsx:147`
- Description: `{entry.entity_id && entry.entity_type ? <EntityLink .../> : <span className="font-mono text-xs text-text-tertiary">{entry.entity_id?.slice(0, 8)}</span>}` — violates AC-9 (orphan rule "never render UUID prefix in primary UI"). Fallback branch should render `—` or `EntityLink` with `truncate` (which now uses middle-ellipsis, not raw prefix).
- Fixable: YES
- Suggested fix: replace else branch with `<span className="text-text-tertiary">—</span>` when entity_type absent, or drop the guard and let EntityLink handle orphan case.

### F-U2 | MEDIUM | ui
- Title: Analytics-cost chart label uses raw UUID slice when operator_name absent
- Location: `web/src/pages/dashboard/analytics-cost.tsx:121`
- Description: `name: op.operator_name || op.operator_id.slice(0, 8)` — chart x-axis label. Per AC-9 + plan Task 7, should not show raw UUID; chart library input cannot render EntityLink JSX, but the label text should at least be the UUID middle-ellipsis or a stable truncation — prefix-only is the exact pattern AC-9 prohibits in rendered UI.
- Fixable: YES
- Suggested fix: `op.operator_name || `${op.operator_id.slice(0, 4)}…${op.operator_id.slice(-4)}`` or guarantee `operator_name` in DTO (FIX-202 already delivered it per plan L59 — this is likely dead fallback).

### F-U3 | MEDIUM | ui
- Title: EntityHoverCard wraps interactive children without nested-anchor safeguard
- Location: `web/src/components/shared/entity-hover-card.tsx:194-219`
- Description: Component wraps `<EntityLink>` (which renders a `<Link>` = `<a>`) inside a `<div>` with mouse handlers, then renders `<PopoverContent>` that internally produces `<div role="dialog">`. No nested-button issue (Popover is in controlled mode without PopoverTrigger), but the `<Popover>` parent registers outside-click against `triggerRef` which is **null** here (no PopoverTrigger mounted). Outside-click path at popover.tsx:107 does `if (triggerRef.current?.contains(t)) return` → null, so clicks on the Link itself collapse past that guard; onMouseLeave also closes, so behavior is correct, but there's no defensive close-on-click-outside for touch devices that don't emit mouseleave. Minor on desktop; could be confusing on iPad.
- Fixable: YES (enhancement)
- Suggested fix: either use Popover's existing PopoverTrigger with `asChild` to wrap EntityLink and get proper trigger ref, or add `onClick` handler that closes the popover on Link click.

### F-U4 | LOW | ui
- Title: EntityLink uses `text-[12px]` arbitrary pixel value
- Location: `web/src/components/shared/entity-link.tsx:136, 151`
- Description: `text-[12px]` is an arbitrary value Tailwind class. Automated Check 2 flags arbitrary pixel values as CRITICAL by default, but plan's design-token map (FIX-219-plan.md L311) explicitly mandates `text-[12px]` for table cell density. Same intentional choice for HoverCard `text-[11px]`, `text-[13px]`, `text-[10px]`. Decision documented in plan, so no blocker — noting for the record.
- Fixable: YES (optional)
- Suggested fix: add FRONTEND.md semantic aliases `text-body-xs: 12px`, `text-body-xxs: 11px` then swap arbitrary px for token classes. Defer to FIX-24x typography pass.

### F-U5 | LOW | ui
- Title: HoverCard `max-w-[280px]` and `min-w-[200px]` use arbitrary px
- Location: `web/src/components/shared/entity-hover-card.tsx:203`
- Description: Same family as F-U4 — arbitrary px values. Intentional per plan; flagged for typography/token pass.
- Fixable: YES (optional)
- Suggested fix: Tailwind config width tokens or accept as-is.

### F-U6 | LOW | ui
- Title: Dashboard chip helper resolves `entity_type` to plain text chip without click navigation
- Location: `web/src/pages/dashboard/index.tsx:720-727`
- Description: Event-source chips render as `<span>` text with no navigation; plan Task 10 was to "consume `envelope.entity.display_name` when present" (done — verified at L714), but chip itself is not clickable. This is consistent with event-source-chips.tsx pattern (chips are context chips, not drill-downs), so acceptable — just noting the consistency was intentional per plan decision (EventEntityButton owns navigable event UI, not inline chips).
- Fixable: N/A
- Suggested fix: no action; boundary decision locked in plan.

### F-U7 | LOW | ui
- Title: aria-label contains raw UUID as fallback when label is missing
- Location: `web/src/components/shared/entity-link.tsx:154`
- Description: `aria-label={`View ${entityType} ${label || entityId}`}` — if label empty, screen reader reads out the full UUID (36 chars). Not harmful but verbose. Screen-reader-friendly per protocol brief.
- Fixable: YES
- Suggested fix: `aria-label={`View ${entityType}${label ? ` ${label}` : ''}`}` — drop UUID from SR announcement; user still has UUID available via right-click.

## Evidence
- Enforcement grep: `.slice(0, 8|10|12)` in rendered JSX — 6 unjustified matches (audit L147, analytics-cost L121, sessions L363 fallback, dashboard L717/726 fallback, analytics.tsx L484, sims/detail L288). Plan allowed fallback branches; F-U1 + F-U2 flagged as remediation candidates. Others are acceptable legacy paths (sims/detail L288 is pre-existing, outside Task list).
- FRONTEND.md Entity Reference Pattern section: complete, no stale FIX-218 references.
- No SCREENS.md broken references.
- Popover + HoverCard integration: functional on desktop hover/leave path; touch edge case noted F-U3.
- TSC not executed (scope: UI quality gate — Test/Build Scout handles).

</SCOUT-UI-FINDINGS>
