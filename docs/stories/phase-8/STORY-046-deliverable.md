# Deliverable: STORY-046 — Frontend Policy List & DSL Editor

## Summary

Full policy management frontend: list with version status + split-pane DSL editor with CodeMirror 6 syntax highlighting, dry-run preview, version management with diff viewer, and staged rollout controls with WebSocket progress.

## Files Changed
| File | Purpose |
|------|---------|
| `web/src/types/policy.ts` | Policy, version, dry-run, rollout types |
| `web/src/hooks/use-policies.ts` | 14 TanStack Query hooks |
| `web/src/lib/codemirror/dsl-language.ts` | Custom DSL language mode |
| `web/src/lib/codemirror/dsl-theme.ts` | Argus dark theme for CodeMirror |
| `web/src/components/policy/dsl-editor.tsx` | CodeMirror 6 editor component |
| `web/src/components/policy/preview-tab.tsx` | Dry-run preview with breakdowns |
| `web/src/components/policy/versions-tab.tsx` | Version list + inline diff viewer |
| `web/src/components/policy/rollout-tab.tsx` | Staged rollout controls + WS progress |
| `web/src/pages/policies/index.tsx` | Policy list (SCR-060) |
| `web/src/pages/policies/editor.tsx` | Policy editor (SCR-062) |

## Key Features
- CodeMirror 6 with custom DSL syntax (POLICY/MATCH/RULES/WHEN/ACTION/CHARGING keywords)
- Split-pane resizable layout (25-75% range)
- Dry-run preview: affected SIMs, operator/APN/RAT breakdown, sample before/after
- Version management: draft/active/archived badges, create new version, inline diff
- Rollout: start (1%→10%→100%), advance, rollback with WebSocket live progress
- Keyboard shortcuts: Ctrl+S save, Ctrl+Enter dry-run

## Test Coverage
- TypeScript strict, npm run build clean
- 17/17 ACs verified
