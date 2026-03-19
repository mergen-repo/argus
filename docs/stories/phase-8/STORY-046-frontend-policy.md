# STORY-046: Frontend Policy List & DSL Editor

## User Story
As a policy editor, I want a policy list with version status and a full-featured DSL editor with syntax highlighting, dry-run preview, version management, and rollout controls, so that I can create and deploy policies confidently.

## Description
Policy list (SCR-060) showing policies with active version, SIM count, and status. Policy editor (SCR-062) with split-pane layout: left pane is Monaco/CodeMirror editor with DSL syntax highlighting, right pane shows dry-run preview, version tabs, and rollout controls. Version diff viewer compares two versions side by side.

## Architecture Reference
- Services: SVC-05 (Policy Engine), SVC-03 (Core API)
- API Endpoints: API-090 to API-099
- Source: docs/architecture/api/_index.md (Policies section)

## Screen Reference
- SCR-060: Policy List — policies table with version, SIM count, status, actions
- SCR-062: Policy Editor — split pane DSL editor, dry-run preview, version tabs, rollout controls

## Acceptance Criteria
- [ ] Policy list: table with name, active version, SIM count, status, last modified, actions
- [ ] Policy list: create new policy button → dialog with name, description, scope
- [ ] Policy editor: split-pane layout (resizable divider)
- [ ] Left pane: code editor (Monaco or CodeMirror) with DSL syntax highlighting
- [ ] DSL syntax highlighting: keywords (POLICY, MATCH, RULES, WHEN, ACTION, CHARGING), strings, numbers, operators
- [ ] Editor features: auto-indent, bracket matching, line numbers, error markers (red underline)
- [ ] Right pane tabs: Preview, Versions, Rollout
- [ ] Preview tab: dry-run results (affected SIMs, breakdown charts, sample SIMs with before/after)
- [ ] Preview auto-updates on DSL change (debounced 1s)
- [ ] Versions tab: list of versions with state badges (draft/active/archived), create new version button
- [ ] Version diff: select two versions → side-by-side DSL diff (added/removed highlighting)
- [ ] Rollout tab: start rollout (1%→10%→100%), advance, rollback buttons
- [ ] Rollout progress: visual progress bar, migrated count, current stage
- [ ] Rollout events via WebSocket policy.rollout_progress → progress bar updates live
- [ ] Save draft: save current DSL as draft version
- [ ] Activate: activate draft version (confirmation dialog with affected SIM count)
- [ ] Keyboard shortcuts: Ctrl+S save, Ctrl+Enter run dry-run

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-023 (policy API), STORY-024 (dry-run API), STORY-025 (rollout API)
- Blocks: None

## Test Scenarios
- [ ] Policy list loads with correct version and SIM count
- [ ] Create new policy → dialog → policy created, navigate to editor
- [ ] DSL editor: type POLICY keyword → syntax highlighted
- [ ] DSL error: missing closing brace → red underline on error line
- [ ] Dry-run preview: shows affected SIM count and breakdown
- [ ] Preview updates when DSL changes (debounced)
- [ ] Version tab: shows all versions with state badges
- [ ] Create new version → v2 draft created, editor switches to it
- [ ] Version diff: two versions selected → side-by-side diff shown
- [ ] Start rollout → progress bar appears, stage shown
- [ ] WebSocket rollout event → progress bar advances
- [ ] Rollback → confirmation → rollback executed, progress reset

## Effort Estimate
- Size: XL
- Complexity: Very High
