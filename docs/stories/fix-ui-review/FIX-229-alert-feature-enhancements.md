# FIX-229: Alert Feature Enhancements (Mute All UX, Export Format, Similar Clustering, Retention)

## Problem Statement
Alerts page has UX friction: "Mute All" lacks scoping options, export format CSV only (no PDF), no "similar alerts" clustering, retention not configurable.

## User Story
As an SRE, I want finer alert management: scoped mute, export options, pattern clustering, retention control.

## Findings Addressed
F-38, F-39, F-41, F-42, F-43

## Acceptance Criteria
- [ ] **AC-1:** "Mute" action supports scope: this alert / all alerts of this type / all alerts on this operator / all alerts matching dedupe_key. Duration: 1h / 24h / 7d / Custom.
- [ ] **AC-2:** Export: CSV + PDF + JSON. PDF via reports engine (FIX-248) with formatted table.
- [ ] **AC-3:** "Show similar alerts" — expand row → list other alerts with matching `dedupe_key` or `type+source`. Facilitates pattern recognition.
- [ ] **AC-4:** Retention config: tenant setting `alert_retention_days` (default 180, max 365).
- [ ] **AC-5:** Suppression rules — save "mute all type X on operator Y for next 24h" as reusable rule with expiration.

## Files to Touch
- `internal/api/alert/handler.go` — mute + similar + export
- `internal/store/alert_suppression.go` (NEW)
- `web/src/pages/alerts/*`

## Risks & Regression
- **Risk 1 — Over-aggressive mute hides real issues:** AC-1 duration limits + audit entry for every mute.

## Test Plan
- Browser: mute scope options work, similar alerts expand, PDF export

## Plan Reference
Priority: P3 · Effort: M · Wave: 7 · Depends: FIX-209 (alerts table)
