# FIX-243: Policy DSL Realtime Validate Endpoint + Frontend Linter Integration

## Problem Statement
Current DSL validation only runs on **Save** (`CreateVersion`, `Activate`). User types DSL in editor → no feedback until Save → if invalid, 422 returned from API → edit cycle painful. Additionally, seed DSL invalid (F-135) because seed INSERTs bypass handler validation — migration path needed.

CodeMirror editor in `policies/editor.tsx` has no linter integration. DSL grammar is known (internal package `internal/policy/dsl/parser.go`), could provide real-time feedback.

## User Story
As a policy author, I want inline DSL syntax/semantic errors highlighted as I type, so I don't need to save and see a 422 to know I have a typo. Also, I want seed DSL to be validated at migration time so we don't ship broken seed.

## Architecture Reference
- Backend: new `POST /api/v1/policies/validate` endpoint (stateless, no DB writes)
- FE: CodeMirror 6 linter API + debounced fetch

## Findings Addressed
- F-135 (seed DSL invalid — migration validation)
- F-136/F-137/F-138 (DSL editor UX gaps)
- F-309 (realtime validate gap — code verified)

## Acceptance Criteria
- [ ] **AC-1:** New endpoint `POST /api/v1/policies/validate`:
  - Body: `{ dsl_source: "POLICY \"x\" { MATCH { ... } RULES { ... } }" }`
  - Response 200: `{ valid: true, compiled_rules: {...}, warnings: [...] }`
  - Response 422: `{ valid: false, errors: [{line, column, severity, message}] }`
  - Rate limit: 10 req/sec per user (guards against abuse)
- [ ] **AC-2:** Validator is pure — does NOT persist to DB, does NOT mutate state. Can be called safely at high frequency.
- [ ] **AC-3:** FE CodeMirror linter:
  - Uses `@codemirror/lint` plugin
  - Debounced 500ms after last keystroke
  - Call `/policies/validate` → transforms errors to CodeMirror diagnostics (line, col, message)
  - Highlights problem lines with squiggly underline + hover tooltip
- [ ] **AC-4:** Dry-run preview button (existing) continues working — separate concept. Dry-run computes Affected SIMs using compiled DSL; validate just checks grammar + semantics.
- [ ] **AC-5:** **Seed validation migration:** New cmd `cmd/argusctl/validate-seed-dsl` — walks `migrations/seed/*.sql` finding DSL INSERT statements, calls same DSL compiler, fails migration if invalid.
- [ ] **AC-6:** Seed cleanup: fix all invalid seed DSL (F-135 listed the issue). Each policy version seed must pass AC-5 check.
- [ ] **AC-7:** Editor UX improvements:
  - Error summary panel at bottom: "3 errors, 2 warnings" with click-to-line
  - Highlight on "Save Draft" disabled if errors present (button tooltip explains)
  - Keyboard shortcut Ctrl+Enter = validate now (no wait for debounce)
- [ ] **AC-8:** **DSL auto-format** (nice-to-have) — formatter normalizes whitespace, indentation. Accessed via Ctrl+Shift+F.
- [ ] **AC-9:** **Available fields autocomplete:**
  - MATCH context: show match criteria keywords + operator enum values + APN names (from tenant)
  - RULES context: show rule keywords (bandwidth_down/up, rate_limit, time_window...)
  - Powered by CodeMirror `autocompletion` extension + static word list from parser
- [ ] **AC-10:** Error message quality — DSL parser emits actionable messages: "Unknown match criterion 'aparment' — did you mean 'apn'?" (Levenshtein suggestion for typos).

## Files to Touch
- `internal/api/policy/handler.go::Validate` (NEW)
- `internal/gateway/router.go` — route
- `internal/policy/dsl/parser.go` — ensure error reporting structured (line/col)
- `cmd/argusctl/cmd/validate_seed.go` (NEW)
- `web/src/pages/policies/editor.tsx` — linter integration
- `web/src/components/policy/dsl-editor.tsx` — CodeMirror linter plugin

## Risks & Regression
- **Risk 1 — Validate endpoint abuse:** AC-1 rate limit; caching repeat bodies; can tolerate 10/s per user.
- **Risk 2 — Editor lag:** 500ms debounce + async; no UI block.
- **Risk 3 — Seed validation migration fails production seeds:** AC-6 fix ALL seeds before enabling AC-5 check.
- **Risk 4 — Parser error messages improved may break existing tests:** Update test assertions for new message format.

## Test Plan
- Unit: endpoint valid/invalid responses for 10 DSL cases
- Integration: seed validate cmd rejects bogus DSL
- Browser: type DSL with syntax error → squiggly appears within 600ms
- Autocomplete: MATCH context shows criteria list

## Plan Reference
Priority: P1 · Effort: M · Wave: 9 · No dependencies
