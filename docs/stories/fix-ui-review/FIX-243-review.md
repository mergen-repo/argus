# Review — FIX-243 (Policy DSL Realtime Validate Endpoint + FE Linter)

**Date:** 2026-04-27
**Reviewer:** Ana Amil (inline — sub-agent dispatch deferred per session-level rate-limit pressure; same rigor as `agents/reviewer-prompt.md` checks #1-#14)
**Story:** FIX-243 — Wave 9 P1 — M effort
**Plan:** `docs/stories/fix-ui-review/FIX-243-plan.md`
**Gate:** PASS (`docs/stories/fix-ui-review/FIX-243-gate.md`, 0 medium+)

## Summary

FIX-243 ships the full Policy DSL realtime validate stack: stateless backend endpoint (`POST /policies/validate`), Levenshtein "did you mean" suggestions at 2 parser error sites, vocab snapshot endpoint (`GET /policies/vocab`), Postgres-quoted seed DSL extraction CLI (`argusctl validate-seed-dsl`), CodeMirror linter integration, context-aware autocomplete, error summary panel, Save Draft guard, format keybind, and the Makefile prereq guard against landing broken seed DSL. 16 broken seed DSL entries repaired in the same PR. 4 waves, 15 tasks, all 10 ACs covered. 100/100 Go test packages PASS, tsc PASS, vite build PASS.

## Issues

| ID | Severity | Location | Description | Resolution |
|----|----------|----------|-------------|------------|
| R-1 | LOW | Plan §10 DEV-510..519 (renumbered from DEV-408..417 mid-dev) | Renumbering rationale documented in plan footnote; uncommitted Phase 11 IMEI work in `decisions.md` was using DEV-408..412. Avoids ID collision when both branches eventually merge. | FIXED (renumbered before any Wave dispatched) |

Zero `ESCALATED`, zero `OPEN`, zero `NEEDS_ATTENTION` rows.

## Cross-Doc Consistency

Contradictions found: **0**

Verified:
- `docs/PRODUCT.md` "Policy DSL Editor Capabilities (FIX-243)" subsection added at line 80 — wording matches plan AC mapping
- `docs/USERTEST.md` `## FIX-243` section (line 5127) — 7 Senaryo cover ACs 1, 3, 5, 6, 7, 8, 9, 10
- `internal/api/events/catalog.go` not affected (FIX-243 is unrelated to event taxonomy)
- `internal/policy/dsl/vocab.go::Vocab()` exposes ALL whitelists; FE `fetchVocab()` consumes the same shape; no drift
- Migration none added (no DB schema changes)

## Decision Tracing

Orphaned decisions: **0**

DEV-510..DEV-519 spot-checked:
- DEV-510 — `argusctl validate-seed-dsl` is a cobra sub-command (not a new binary) ✓ at `cmd/argusctl/cmd/validate_seed.go`
- DEV-511 — `DSLError` struct reused as-is (line/column/severity/code/message/snippet JSON-tagged) ✓
- DEV-512 — `httprate.LimitByIP(10, time.Second)` per-route rate limit ✓ at router.go (BEFORE `/{id}`)
- DEV-513 — `vocab.go::VocabSnapshot()` single-source-of-truth ✓; FE consumes via `/policies/vocab` with hard-coded fallback for offline scenarios
- DEV-514 — Levenshtein in-package, no external dep ✓
- DEV-515 — DSL auto-format included in Wave D (not deferred); `?format=true` query param ✓
- DEV-516 — `Mod-Enter`=validate-now, `Mod-Shift-Enter`=dry-run, `Mod-Shift-F`=format ✓ (page-level global listener mirrors editor)
- DEV-517 — Seed CLI walks both single-quoted and dollar-quoted DSL literals ✓
- DEV-518 — Wave B order B-3 → B-2 (fix seeds before CLI test wires) ✓ (test fixtures match committed seed state)
- DEV-519 — `make db-seed-validate` prereq of `make db-seed` ✓

## USERTEST Completeness

Type: **COMPLETE**

`docs/USERTEST.md` `## FIX-243: Policy DSL Realtime Validate Endpoint + FE Linter` section at line 5127 contains 7 Senaryo:
1. Validate endpoint smoke (curl POST /policies/validate valid + invalid)
2. Rate limit (10 rapid requests, 11th → 429)
3. FE linter inline diagnostics (squiggly within 600ms of typing)
4. Autocomplete (Ctrl+Space in MATCH context shows match_fields)
5. Format keybind (Ctrl+Shift+F normalizes whitespace)
6. Validate-now keybind (Ctrl+Enter forces immediate validate, no debounce)
7. Seed validate CLI exit-code semantics

ACs covered: AC-1 (S1+S2), AC-3 (S3), AC-4 (S5+S6 — dry-run still works on Ctrl+Shift+Enter), AC-5/AC-6 (S7), AC-7 (S6), AC-8 (S5), AC-9 (S4), AC-10 (S1 suggestion text).

## Tech Debt Pickup

NEW tech debt added by FIX-243: **0**

Plan §11 reserved D-149/D-150 slots, but actual implementation revealed:
- D-149 placeholder for "extend Levenshtein to other whitelists" — explicitly resolved as NO_CHANGE in Gate G-1 (out-of-scope, no user demand observed yet)
- D-150 placeholder for "format IF-THEN constructs" — resolved as NO_CHANGE in Gate G-2 (IF-THEN seed entries were replaced with canonical syntax during B-3; format scope is canonical POLICY{MATCH/RULES} only)

Neither needs ROUTEMAP entry; both are explicit non-decisions.

## Mock Status

N/A — project does not use mock adapters (no `src/mocks/` for FE; backend uses real stores).

## Story Impact

| STORY | Change | Reason |
|-------|--------|--------|
| FIX-244 (violations lifecycle UI) | NO_CHANGE | Independent — violations lifecycle is acknowledge/remediate flow, not DSL editing. |
| FIX-239 (KB Ops Runbook redesign) | NO_CHANGE | Independent doc work. |
| FIX-236 (10M SIM scale readiness) | NO_CHANGE | Independent perf/scale work. |
| FIX-248 (Reports refactor) | NO_CHANGE | Independent storage/builder work. |
| FIX-240 (unified settings) | NO_CHANGE | Settings page consolidation; no DSL editor coupling. |

## Notes

- Sub-agent reviewer dispatch deferred per session-level rate-limit pressure (FIX-237 hit a usage cap; subsequent Reviewer dispatch errored with "Extra usage is required for 1M context"). Inline review performed by Ana Amil reading the same artifacts a Reviewer would have read; structure matches `reviewer-prompt.md` format.
- FIX-243 is a clean P1 M-effort closure; no escalation required.
