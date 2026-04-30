# FIX-216: Modal Pattern Standardization — Dialog vs SlidePanel Semantic Split

## Problem Statement
Inconsistent modal patterns — same-class flows use Dialog in one place, SlidePanel in another. F-109 surfaced 5 cases. User approved Option C (semantic split).

## User Story
As a developer, I want a documented rule for when to use Dialog vs SlidePanel so new UI is consistent without per-decision debate.

## Architecture Reference
- Components: `web/src/components/ui/dialog.tsx`, `slide-panel.tsx`
- Doc: `docs/FRONTEND.md` — new section

## Findings Addressed
F-109, F-171 (violations row expand → SlidePanel per pattern), F-207 was concerned but user retained inline for Audit (F-207 dropped)

## Acceptance Criteria
- [ ] **AC-1:** `docs/FRONTEND.md` new section "Modal Pattern" documents rule:
  - **Dialog** (centered, compact): quick confirm (Evet/Hayır + optional reason), destructive action warnings, simple 1-2 field forms.
  - **SlidePanel** (right-side sheet): rich form (3+ fields or multi-step), detail inspection (read-heavy), list pickers with search.
- [ ] **AC-2:** Apply checklist (from DEV-252):
  - SIMs Suspend/Resume/Terminate: SlidePanel → **Dialog**
  - SIMs Assign Policy: Dialog → **SlidePanel** (search + multi-line form)
  - APNs Connected SIMs: KEEP SlidePanel
  - IP Pool Reserve IP: Dialog → **SlidePanel** (search + multi-row list)
  - Alerts preview (future): **SlidePanel**
- [ ] **AC-3:** Violations row expand — inline expand → **SlidePanel** (F-171).
- [ ] **AC-4:** Lint rule / ESLint plugin (optional): flag Dialog usage with >3 form fields.
- [ ] **AC-5:** Visual consistency audit — all Dialog buttons use `variant="default"` primary + `variant="outline"` cancel; all SlidePanel headers use `SlidePanelHeader` component.
- [ ] **AC-6:** Dark mode parity — both patterns respect theme tokens.

## Files to Touch
- `docs/FRONTEND.md`
- `web/src/pages/sims/index.tsx` (suspend/terminate + policy-assign swap)
- `web/src/pages/violations/index.tsx` (row expand)
- `web/src/pages/settings/ip-pool-detail.tsx` (reserve)
- `web/src/components/ui/` — ensure patterns consistent

## Risks & Regression
- **Risk 1 — Muscle memory:** Existing user flows change modal type. Mitigation: release notes + tooltip hint on first encounter.
- **Risk 2 — Mobile responsive:** SlidePanel must collapse fullscreen on <768px. Verify.

## Test Plan
- Browser: each flow opens correct modal type
- Accessibility: ESC closes, focus trap active

## Plan Reference
Priority: P2 · Effort: M · Wave: 5
