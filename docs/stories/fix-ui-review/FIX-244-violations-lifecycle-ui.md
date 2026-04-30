# FIX-244: Violations Lifecycle UI — Acknowledge + Remediate Actions Wired

## Problem Statement
Backend already implements violation lifecycle (F-310 verified):
- DB column `acknowledged_at, acknowledged_by, acknowledgment_note`
- Endpoints: `POST /policy-violations/{id}/acknowledge`, `POST /policy-violations/{id}/remediate` (with action: `suspend_sim | escalate | dismiss`)
- Index `idx_policy_violations_unack WHERE acknowledged_at IS NULL` (perf-optimized)

But UI has **zero action buttons** — row click does nothing actionable. F-169 was wrongly scoped ("no status column") — fix was actually "UI not wiring existing backend".

Additional UI gaps:
- Filter type dropdown sends wrong field (F-165) — UI dropdown has `action_taken` values but query uses `violation_type`
- Export 404 — UI path wrong `/violations/export.csv`, backend at `/policy-violations/export.csv` (F-166)
- Row details minimal (F-164): no ICCID, no policy name, just "SIM" literal link
- Row expand inline (F-171) — should be SlidePanel per FIX-216
- "Well done!" empty state tonu (F-170)

## User Story
As a policy admin investigating violations, I want Acknowledge + Remediate actions directly in the violations list, with rich row details, filterable by meaningful fields, and proper Export — so I can triage and resolve violation investigations efficiently.

## Architecture Reference
- Backend endpoints ready — only FE wiring + minor fixes needed
- FE: `web/src/pages/violations/index.tsx`

## Findings Addressed
- F-164 (row detail minimal)
- F-165 (filter field mismatch — FE bug)
- F-166 (Export 404 — FE path bug)
- F-168 (no date range filter)
- F-169 (lifecycle UI)
- F-170 (well done copy)
- F-171 (row expand pattern)
- F-310 (backend ready)

## Acceptance Criteria
- [ ] **AC-1:** Row actions per violation:
  - **Acknowledge** — click → Dialog (FIX-216 pattern, compact): text "Acknowledge 'bandwidth_exceeded' on SIM X?" + optional note textarea → POST /acknowledge
  - **Remediate** — click → dropdown with 3 options: "Suspend SIM", "Escalate to ops", "Dismiss (false positive)"
  - Remediate suspend_sim shows confirm dialog with warning (destructive action)
- [ ] **AC-2:** Status column shows lifecycle state:
  - `acknowledged_at IS NULL AND details.escalated IS NULL` → "Open" (chip red)
  - `acknowledged_at IS NOT NULL` → "Acknowledged" (chip yellow)
  - `details.remediation = suspend_sim` → "Remediated" (chip green)
  - `details.remediation = dismiss` → "Dismissed" (chip gray)
- [ ] **AC-3:** Filter dropdown FIX (F-165) — FE dropdown "Type" sends `?violation_type=...` with correct values (`bandwidth_exceeded`, `session_limit`, `quota_exceeded`, `time_restriction`, `geo_blocked`). Separate new "Action" dropdown sends `?action_taken=throttle|disconnect|block|notify|log|suspend|tag`.
- [ ] **AC-4:** Export path FIX (F-166) — FE calls `/api/v1/policy-violations/export.csv` (correct path). Backend already works.
- [ ] **AC-5:** Row expand → SlidePanel (F-171, FIX-216 pattern):
  - Severity + violation_type + action_taken header
  - Full DETAILS JSONB rendered (measured, threshold, unit, rule, session_id if present)
  - SIM link (ICCID clickable — FIX-219 pattern)
  - Policy link (name + version)
  - Session link (if session_id)
  - Operator + APN chips
  - Timeline: occurrence first seen + last seen + state transitions
  - Related violations list (same dedupe/similar)
  - Action buttons (Acknowledge/Remediate)
- [ ] **AC-6:** Filter additions (F-168):
  - **Date range picker** (preset pills + custom)
  - **Status filter** — Open / Acknowledged / Remediated / Dismissed / All
  - **Severity** from FIX-211 unified taxonomy
- [ ] **AC-7:** Empty state copy (F-170) — replace "Well done!" with professional "No policy violations in the selected timeframe." Timeframe reflects active filter.
- [ ] **AC-8:** Row detail polish (F-164):
  - SIM column: ICCID as clickable link (not literal "SIM")
  - Policy column: policy_name + version chip
  - Measured / Threshold shown in row (not hidden behind expand)
- [ ] **AC-9:** Bulk operations:
  - Row checkbox for multi-select
  - Sticky bulk bar: "Bulk Acknowledge" + "Bulk Dismiss"
  - Respects FIX-236 filter-based selection pattern ("all matching filter")
- [ ] **AC-10:** Audit trail — every acknowledge/remediate action writes audit_logs entry `entity_type=policy_violation`, action=`violation.{acknowledged|remediated|dismissed}`.

## Files to Touch
- `web/src/pages/violations/index.tsx`
- `web/src/pages/violations/detail-panel.tsx` (NEW — SlidePanel)
- `web/src/hooks/use-violations.ts` — mutations for ack/remediate
- `web/src/types/violation.ts` — status derived field
- Backend (minor): verify audit_logs wrote for ack/remediate — fix if missing

## Risks & Regression
- **Risk 1 — Remediate suspend_sim side-effect:** Suspends SIM across whole platform. Confirm dialog explicit about consequence.
- **Risk 2 — Bulk dismiss hides real issues:** AC-9 bulk confirm with count + reason required.
- **Risk 3 — Export backward compat:** Old FE path still used in bookmarks — add redirect from `/violations/export.csv` → `/policy-violations/export.csv` on Nginx.

## Test Plan
- Unit: status derivation logic (open/ack/remediated/dismissed)
- Integration: click Acknowledge → row status changes to Acknowledged, audit entry written
- Browser: filter by date range, Status=Open, Severity=critical → correct subset

## Plan Reference
Priority: P1 · Effort: S · Wave: 9 · Depends: FIX-216 (SlidePanel), FIX-211 (severity taxonomy), FIX-219 (entity links)
