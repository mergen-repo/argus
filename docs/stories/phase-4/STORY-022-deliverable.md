# Deliverable: STORY-022 — Policy DSL Parser & Evaluator

## Summary

Implemented a complete Policy DSL (Domain-Specific Language) parser and evaluator in `internal/policy/dsl/`. The DSL enables human-readable policy rules for authentication, QoS, and charging configuration without code changes.

## What Was Built

### Core Components (7 files)
- **token.go** — 31 token types (keywords, operators, literals) with keyword lookup
- **lexer.go** — Tokenizer with line/column tracking, whitespace/comment handling, string/number/identifier/operator tokenization
- **ast.go** — Complete AST node types: Policy, MatchBlock, RulesBlock, WhenBlock, ActionCall, ChargingBlock, condition types (Simple/Compound/Not/Group), value types (String/Number/NumberWithUnit/Ident/TimeRange/Percent/Bool)
- **parser.go** — Recursive descent parser with operator precedence (NOT > AND > OR), error recovery (reports all errors), validation (duplicate assignments, RAT type values, action parameter signatures), snippet-based error reporting
- **compiler.go** — AST → JSON rule tree compiler with unit normalization (data sizes → bytes, rates → bps, durations → seconds)
- **evaluator.go** — Session context evaluator: MATCH block filtering, WHEN block evaluation (first match wins for assignments, all actions collected), CHARGING with RAT type multiplier, time range support with midnight wrapping
- **dsl.go** — Public API facade: Parse(), CompileSource(), CompileAST(), EvaluateSource(), EvaluateCompiled(), Validate(), DSLVersion()

### Test Coverage (4 test files, 47+ tests)
- **lexer_test.go** — Token stream verification for all token types
- **parser_test.go** — Error recovery, compound conditions, action validation, CHARGING blocks
- **compiler_test.go** — Unit normalization, operator mapping, roundtrip serialization
- **evaluator_test.go** — MATCH filtering, WHEN evaluation, first-match semantics, charging calculation, time range with midnight wrapping, all action types

## Architecture References Fulfilled
- SVC-05 (Policy Engine): DSL parser and evaluator core implemented
- DSL_GRAMMAR.md: Full EBNF grammar implemented (POLICY, MATCH, RULES, WHEN, ACTION, CHARGING)
- ALGORITHMS.md Section 6: Policy evaluation algorithm implemented

## Gate Fixes Applied
1. **time_of_day range evaluation** — Fixed string equality comparison to proper time range containment with midnight wrapping support
2. **Additional test coverage** — 3 new test functions (15 sub-tests) for time range, midnight wrapping, and suspend action

## Files Changed
```
internal/policy/dsl/token.go          (new)
internal/policy/dsl/lexer.go          (new)
internal/policy/dsl/ast.go            (new)
internal/policy/dsl/parser.go         (new)
internal/policy/dsl/compiler.go       (new)
internal/policy/dsl/evaluator.go      (new)
internal/policy/dsl/dsl.go            (new)
internal/policy/dsl/lexer_test.go     (new)
internal/policy/dsl/parser_test.go    (new)
internal/policy/dsl/compiler_test.go  (new)
internal/policy/dsl/evaluator_test.go (new)
docs/stories/phase-4/STORY-022-plan.md (new)
docs/stories/phase-4/STORY-022-gate.md (new)
```

## Dependencies Unblocked
- STORY-023 (Policy CRUD & Versioning) — can now store DSL source and compiled rules
- STORY-024 (Policy Dry-Run Simulation) — can now evaluate policies against test contexts
- STORY-025 (Policy Staged Rollout) — can now apply compiled policies to sessions
