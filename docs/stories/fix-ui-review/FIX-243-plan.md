# FIX-243 — Policy DSL Realtime Validate Endpoint + Frontend Linter Integration — PLAN

- **Spec:** `docs/stories/fix-ui-review/FIX-243-dsl-realtime-validate.md`
- **Tier:** P1 · **Effort:** M · **Wave:** 9 (UI Review Remediation Phase 2 P1)
- **Track:** UI Review Remediation (full track AUTOPILOT)
- **Findings:** F-135 (seed DSL invalid), F-136/137/138 (editor UX gaps), F-309 (no realtime validate)

---

## Goal

1. Add stateless `POST /api/v1/policies/validate` endpoint that returns structured `{line, column, severity, message}` diagnostics so the FE can lint the DSL editor in real time (debounced 500 ms).
2. Wire CodeMirror 6 `@codemirror/lint` + `autocompletion` extensions into `dsl-editor.tsx` so authors get squiggly underlines, hover tooltips, error summary panel, Ctrl+Enter validate-now, and context-aware autocomplete.
3. Ship a guard CLI sub-command `argusctl validate-seed-dsl` that scans `migrations/seed/*.sql` for `INSERT INTO policy_versions ... dsl_content ...` and rejects invalid DSL — preventing future seed drift (F-135 root cause).
4. Repair the 17 broken seed entries (F-135) so the new guard passes today.
5. Improve parser error messages with Levenshtein "did you mean" suggestions for unknown match-fields / rule-keys (AC-10).

---

## Architecture Context

### Backend (already in place — minimal new code)

- `internal/policy/dsl/parser.go::DSLError` ALREADY structured: `{Line, Column, Severity, Code, Message, Snippet}` — JSON-tagged. **No refactor needed.**
- `internal/policy/dsl/dsl.go::Validate(source) []DSLError` — public stateless API. Re-use directly.
- `internal/policy/dsl/dsl.go::CompileSource(source) (*CompiledPolicy, []DSLError, error)` — for the optional `compiled_rules` payload in 200 response.
- `internal/api/policy/handler.go` — `Handler` struct exists with `policyStore`, `dsl` deps; add new `Validate(w, r)` method alongside `CreateVersion` (line 515) and `ActivateVersion` (line 645).
- `internal/gateway/router.go:569-583` — policy route block; insert `r.Post("/api/v1/policies/validate", deps.PolicyHandler.Validate)` BEFORE the `{id}` routes (chi precedence).
- `internal/apierr/apierr.go::WriteJSON` for the standard envelope.
- **Rate limit:** reuse `RateLimiter(deps.RedisClient, perMin, perHour, deps.Logger)` already wired at router.go:201. The validate route inherits the global per-IP/user limiter (default ~600/min); explicit per-route 10/sec cap added via a tighter `httprate` instance scoped to this single route.

### CLI (cobra sub-command)

- `cmd/argusctl/main.go` already delegates to `cmd.Execute()` (cobra root).
- `cmd/argusctl/cmd/root.go` registers sub-commands (status/sim/tenant/backup/compliance/...).
- **Decision (Check #1):** add `cmd/argusctl/cmd/validate_seed.go` exposing `argusctl validate-seed-dsl [--seed-dir migrations/seed]`. NOT a new binary. Spec says `cmd/argusctl/validate-seed-dsl` — interpreted as new sub-command file (consistent with existing layout).

### Frontend

- `web/src/components/policy/dsl-editor.tsx` (126 lines) — CodeMirror 6 editor; already imports `lintGutter` but no actual `linter`. Already has `closeBrackets` from `@codemirror/autocomplete` package — `autocompletion()` adds zero npm deps.
- `web/src/lib/codemirror/dsl-language.ts` — language definition (extend with completion source).
- `web/src/pages/policies/editor.tsx` — page mount; consume the new validate hook + render error-summary panel.

---

## Acceptance Criteria Mapping

| AC  | Description | Wave | Task(s) |
|-----|-------------|------|---------|
| AC-1 | `POST /api/v1/policies/validate` 200/422 + rate limit | A | A-1, A-2, A-4 |
| AC-2 | Validator pure (no DB writes) | A | A-1 (test asserts no store calls) |
| AC-3 | CodeMirror linter debounced 500 ms | C | C-1, C-2 |
| AC-4 | Existing dry-run still works | C | C-2 (regression check in browser test) |
| AC-5 | `argusctl validate-seed-dsl` CLI guard | B | B-1, B-2 |
| AC-6 | Fix 17 broken seed DSL entries | B | B-3 |
| AC-7 | Error summary panel + disabled Save + Ctrl+Enter | C, D | C-3, D-1 |
| AC-8 | DSL auto-format Ctrl+Shift+F (nice-to-have) | D | D-2 (included, not deferred) |
| AC-9 | Autocomplete (MATCH/RULES context + APN list) | C | C-4 |
| AC-10 | Levenshtein "did you mean" | A | A-3 |

---

## Files to Touch

### Wave A — Backend validate endpoint + parser polish

| Path | Change |
|------|--------|
| `internal/policy/dsl/suggest.go` | NEW. Pure-Go Levenshtein (≤30 LoC) + `Suggest(token, vocab []string) string` returning best match if edit-distance ≤ 2. |
| `internal/policy/dsl/vocab.go` | NEW. `VocabSnapshot()` returns the static keyword/operator/unit/match-field/rule-key set as a stable JSON-shaped struct (consumed by FE autocomplete via the validate response or a sibling endpoint). |
| `internal/policy/dsl/parser.go` | EDIT. In the existing "unknown match field" / "unknown rule key" error paths, append `Suggest(...)` output to `Message` when applicable. Snippet field already populated for some errors; ensure consistency. |
| `internal/api/policy/handler.go` | EDIT. New method `(h *Handler) Validate(w, r)` — read `{dsl_source}`, call `dsl.CompileSource`, return `{valid, compiled_rules?, errors[], warnings[], vocab?}` via `apierr.WriteJSON`. NO `policyStore` calls. |
| `internal/api/policy/handler_test.go` | EDIT. Add `TestValidate_*` cases: empty body 400, valid DSL 200, syntax error 422 with line/col, store dependency NOT invoked. |
| `internal/gateway/router.go` | EDIT. Add `r.Post("/api/v1/policies/validate", deps.PolicyHandler.Validate)` inside the `policy_editor` role group BEFORE `{id}` routes. Wrap with a per-route `httprate.LimitByIP(10, time.Second)` to enforce AC-1 rate limit (or wire to existing redis limiter). |
| `internal/gateway/router_test.go` (if present, else inline) | Add route registration assertion. |

### Wave B — Seed validate CLI + seed cleanup

| Path | Change |
|------|--------|
| `cmd/argusctl/cmd/validate_seed.go` | NEW. Cobra sub-command. Walks `--seed-dir` (default `migrations/seed`), regex-matches `INSERT INTO policy_versions ... dsl_content` rows, extracts the DSL string literal (handle `$tag$ ... $tag$` and quoted strings), calls `dsl.Validate(source)`, prints offending file:line + DSLError list, exits non-zero on any error. |
| `cmd/argusctl/cmd/validate_seed_test.go` | NEW. Table-driven: rejects bogus DSL fixture, accepts clean DSL fixture. |
| `cmd/argusctl/cmd/root.go` | EDIT. Register the new sub-command. |
| `migrations/seed/003_comprehensive_seed.sql` | EDIT. Replace 4 IF-THEN policies (ABC Data Cap, ABC Roaming Block, XYZ Business Hours, XYZ Data Cap) with grammar-compliant `POLICY "name" { MATCH {...} RULES {...} }`. Strip `;` separators from the 13 `;`-delimited RULES blocks. |
| `migrations/seed/005_multi_operator_seed.sql` | EDIT. Same `;` cleanup pass for any policy_versions inserts. |
| `Makefile` | EDIT. Add `db-seed-validate` target invoking `argusctl validate-seed-dsl`; wire as a prereq of `db-seed` (or document in CLAUDE.md). |

### Wave C — FE linter + autocomplete + error summary

| Path | Change |
|------|--------|
| `web/src/lib/api/policies.ts` (or wherever fetch lives) | EDIT/NEW. Add `validateDSL(source: string): Promise<{valid, errors, warnings, vocab?}>`. |
| `web/src/lib/codemirror/dsl-linter.ts` | NEW. Wraps `@codemirror/lint::linter()`; debounced fetch to `validateDSL`; transforms errors → `Diagnostic[]` with `{from, to, severity, message}` — translate (line, column) to absolute offset using `state.doc.line(line).from + (column-1)`. |
| `web/src/lib/codemirror/dsl-language.ts` | EDIT. Add `autocompletion()` source pulling from `vocab` (static keywords) + lazy-fetched APN list (cached via tanstack-query). Context-aware: detect MATCH vs RULES block via simple cursor scan. |
| `web/src/components/policy/dsl-editor.tsx` | EDIT. Wire the linter extension; expose `errors` upstream via a new `onDiagnostics?: (d: Diagnostic[]) => void` prop. Add Ctrl+Enter keymap binding to force immediate validate (clears debounce). |
| `web/src/components/policy/dsl-error-summary.tsx` | NEW. Compact panel: "X errors, Y warnings" with click-to-jump (calls editor `view.dispatch({selection: ...})`). |
| `web/src/pages/policies/editor.tsx` | EDIT. Render `<DSLErrorSummary />` below editor; disable "Save Draft" button when `errors.length > 0`; tooltip explains why. |
| `web/src/components/policy/__tests__/dsl-editor.test.tsx` | EDIT/NEW. Add tests: linter fires after 500 ms, Ctrl+Enter forces immediate, Save disabled with errors. |

### Wave D — Polish + auto-format + Ctrl+Enter + USERTEST

| Path | Change |
|------|--------|
| `internal/policy/dsl/format.go` | NEW. `Format(source string) (string, error)` — tokenize (reusing `lexer.go`), emit canonical 2-space-indented form. Returns input unchanged on parse error. |
| `internal/policy/dsl/format_test.go` | NEW. Round-trip tests: idempotent on already-formatted, normalises whitespace. |
| `internal/api/policy/handler.go` | EDIT. Add optional `?format=true` query on the validate endpoint OR new `POST /api/v1/policies/format` (decision: query param — fewer routes). Returns `{formatted_source}`. |
| `web/src/lib/codemirror/dsl-format.ts` | NEW. Ctrl+Shift+F keybinding → call format endpoint → `view.dispatch({changes: {...}})`. |
| `web/src/components/policy/dsl-editor.tsx` | EDIT. Register Ctrl+Shift+F keymap. |
| `docs/PRODUCT.md` | EDIT. Add "Realtime DSL validation" + "DSL auto-format" feature notes under Policy Engine section. |
| `docs/stories/fix-ui-review/FIX-243-usertest.md` | NEW. 4-scenario manual UAT script (typo highlight, Ctrl+Enter, autocomplete, format). |

---

## Risks

| Risk | Mitigation |
|------|------------|
| R-1: New endpoint hammered by editor → backend CPU | Reuse existing `RateLimiter` middleware + per-route 10/sec cap (AC-1); validator is pure (no DB) so cost is bounded to lex+parse (~200 µs typical). |
| R-2: Editor lag from network round-trip | 500 ms debounce; abort in-flight on new keystroke via `AbortController`; cache last-validated source hash. |
| R-3: Seed migration guard fails CI on existing broken seeds | AC-6 fix lands in SAME PR as AC-5 guard; staged commits within Wave B (B-3 BEFORE wiring guard into Makefile prereq). |
| R-4: Improved parser messages break existing tests | Update `parser_test.go` assertions in Wave A; tests use substring matches where possible. |
| R-5: Ctrl+Enter conflicts with existing dry-run keybind (`Mod-Enter` already triggers `onDryRun` in `dsl-editor.tsx:54`) | Re-bind: Ctrl+Enter = validate-now; Ctrl+Shift+Enter = dry-run. Document in tooltip. NOTE existing UX expectation; acceptable break per spec AC-7. |
| R-6: Frontend autocomplete vocab drifts from parser vocab | Vocab served by backend (`/validate` response includes `vocab` field on first call OR a cheap GET `/api/v1/policies/vocab`); FE caches per session. Single source of truth = `internal/policy/dsl/vocab.go`. |
| R-7: Seed string-extraction regex misses edge cases (multi-line `$tag$`) | CLI uses Postgres dollar-quoted-string-aware tokeniser; tests cover both `'...'` and `$$...$$`. |

---

## Test Plan

### Backend (Go)

- `TestValidate_EmptyBody` — 400.
- `TestValidate_ValidDSL` — 200, `valid:true`, `compiled_rules` populated, no store calls (use mock store and assert zero invocations).
- `TestValidate_SyntaxError` — 422, errors include line/col matching the canonical `;`-separator failure.
- `TestValidate_UnknownMatchField_Suggests` — error message contains `did you mean 'apn'` for input `aparment`.
- `TestValidate_RateLimit` — 11th request within 1 sec returns 429.
- `TestSuggest_EditDistance` — table test for Levenshtein.
- `TestVocabSnapshot_Stable` — snapshot test ensuring vocab is sorted/deterministic.
- `TestFormat_Idempotent` + `TestFormat_NormalizesWhitespace`.
- `TestValidateSeedCmd_RejectsBogus` + `TestValidateSeedCmd_AcceptsClean` (Wave B).

### Frontend (Vitest + RTL)

- `dsl-editor.test.tsx` — types invalid DSL → after 500 ms, `linter` decoration count > 0.
- `dsl-editor.test.tsx` — Ctrl+Enter triggers validate without waiting for debounce.
- `dsl-editor.test.tsx` — Save Draft disabled when `errors.length > 0`.
- `dsl-error-summary.test.tsx` — click-to-jump dispatches selection change.

### Manual UAT (`FIX-243-usertest.md`)

- Scenario 1: type `MATC {}` → squiggly within 1 s, hover shows error.
- Scenario 2: type `aparment = "x"` → suggestion "did you mean 'apn'".
- Scenario 3: Ctrl+Shift+F formats current buffer.
- Scenario 4: clone a previously-broken seed policy → editor highlights immediately, no Save needed.
- Browser: confirm existing dry-run preview still functions (AC-4).

---

## Out of Scope

- WASM-compiled parser in browser (mentioned in F-309 as "advanced" — defer to FUTURE.md).
- Server-side LSP protocol (gold-plating).
- Multi-tenant DSL grammar variants.
- DSL formatter style options (single canonical style only).

---

## Decisions Log

- **DEV-510** — `argusctl validate-seed-dsl` is a cobra sub-command in `cmd/argusctl/cmd/validate_seed.go`, NOT a new binary. Aligns with existing layout (status/sim/tenant/backup/compliance). Spec text "`cmd/argusctl/validate-seed-dsl`" interpreted as the invocation, not a separate package.
- **DEV-511** — Reuse `internal/policy/dsl::DSLError` as-is — already has `Line/Column/Severity/Code/Message/Snippet` JSON-tagged. No parser refactor; only Wave A AC-10 message-quality tweaks.
- **DEV-512** — Validate endpoint is per-route rate-limited via `httprate.LimitByIP(10, time.Second)` in addition to inheriting the global limiter. Tighter cap honours AC-1 ("10 req/sec per user"); IP-keyed because user-id middleware runs before this in the chain.
- **DEV-513** — Vocab single source of truth is `internal/policy/dsl/vocab.go::VocabSnapshot()`. FE consumes via `/api/v1/policies/vocab` (cheap GET, cached) so future grammar additions auto-propagate without FE redeploy.
- **DEV-514** — Levenshtein implemented in-package (`internal/policy/dsl/suggest.go`, ~30 LoC) — NO external dependency (per planner check #5).
- **DEV-515** — DSL auto-format INCLUDED in Wave D (planner check #8 resolved as IN-SCOPE, not deferred). Format-on-save NOT default; Ctrl+Shift+F manual only.
- **DEV-516** — Existing `Mod-Enter` (dry-run) keybind moves to `Mod-Shift-Enter` to free Ctrl+Enter for AC-7 validate-now. Documented in tooltip + USERTEST.
- **DEV-517** — Format endpoint exposed as `POST /api/v1/policies/validate?format=true` (query param) instead of a sibling route, to keep the route tree minimal and reuse the same rate-limit bucket.
- **DEV-518** — Seed CLI validates BOTH `migrations/seed/003_comprehensive_seed.sql` and `005_multi_operator_seed.sql`. Future seed files auto-discovered via glob.
- **DEV-519** — Wave B order: B-3 (fix seeds) MUST land before B-2 (CLI test) so test fixtures match committed seed state. Documented in Wave Breakdown below.

---

## Tech Debt

- **D-149** — `Format()` does not preserve user-authored comments (parser drops `//` and `/* */` tokens already; lexer support optional). Track for follow-up if comment support added to grammar.
- **D-150** — Vocab autocomplete does not yet learn from tenant-defined custom tags (e.g. SIM custom_attributes). Future enhancement once attributes stabilise.

---

## Wave Breakdown (M-effort: 4 waves)

### Wave A — Backend validate endpoint + parser polish (4 tasks)

- **A-1** — Implement `Handler.Validate(w, r)` + `vocab.go::VocabSnapshot` + `suggest.go::Levenshtein/Suggest`. Unit tests for each.
- **A-2** — Wire route in `internal/gateway/router.go` (BEFORE `{id}` routes) with per-route `httprate` 10/sec.
- **A-3** — Tweak `parser.go` unknown-field/keyword error sites to call `Suggest()` and append "did you mean ...". Update `parser_test.go` assertions.
- **A-4** — Handler tests: empty body / valid / invalid / suggest / rate-limit / no-DB-write assertion. Run `go test ./internal/api/policy/... ./internal/policy/dsl/...`.

**Quality gate:** all new + existing Go tests pass; `go vet ./...` clean; `golangci-lint run` clean.

### Wave B — Seed validate CLI + seed repair (3 tasks)

- **B-1** — Implement `cmd/argusctl/cmd/validate_seed.go` (cobra) + register in `root.go`. Postgres dollar-quoted + single-quoted string extraction. Unit tests with two fixtures.
- **B-3** — Repair `003_comprehensive_seed.sql` (4 IF-THEN policies + 13 `;`-separator policies) and `005_multi_operator_seed.sql`. After each edit, manually run `argusctl validate-seed-dsl` to verify.
- **B-2** — Add `Makefile` target `db-seed-validate`; wire as prereq of `db-seed`. CI guard ensured.

**Quality gate:** `make db-seed-validate` exits 0; `make db-seed` runs end-to-end clean (per CLAUDE.md no-defer-seed rule).

### Wave C — FE linter + autocomplete + error summary (4 tasks)

- **C-1** — `dsl-linter.ts` debounced linter source + `policies.ts::validateDSL` fetcher. Vitest covers debounce + abort-on-keystroke.
- **C-2** — Wire linter extension into `dsl-editor.tsx`; add `onDiagnostics` callback; verify existing dry-run untouched (AC-4).
- **C-3** — `dsl-error-summary.tsx` + integration in `pages/policies/editor.tsx` + Save Draft disabled-when-errors logic.
- **C-4** — Autocomplete: extend `dsl-language.ts` with `autocompletion({override: [...]})`; vocab fetched lazily + cached; APN list fetched on editor focus.

**Quality gate:** `pnpm -C web test`, `pnpm -C web lint`, `pnpm -C web build` all pass; tsc clean.

### Wave D — Auto-format + USERTEST + docs (3 tasks)

- **D-1** — Re-bind `Mod-Enter` → validate-now (AC-7); `Mod-Shift-Enter` → dry-run; update tooltip + tests.
- **D-2** — `internal/policy/dsl/format.go` + `?format=true` query support + FE Ctrl+Shift+F keybind + tests. (AC-8)
- **D-3** — `FIX-243-usertest.md` (4 scenarios) + `docs/PRODUCT.md` Policy Engine section update + ROUTEMAP closure entry.

**Quality gate:** Manual UAT signed off; Embedded Quality Gate per planner-prompt.md; CLAUDE.md "Last closed" updated.

---

## Plan Self-Check

- [x] Plan file written at `docs/stories/fix-ui-review/FIX-243-plan.md`.
- [x] All 10 ACs mapped to a wave + task.
- [x] All 8 mandatory planner checks resolved with file/line evidence.
- [x] Wave count = 4 (M-effort sweet spot per planner-prompt.md).
- [x] Decisions logged DEV-510 .. DEV-519 (10 entries).
- [x] Tech debt logged D-149, D-150.
- [x] No blockers identified.
- [x] Step-log will be appended after this file is written.
