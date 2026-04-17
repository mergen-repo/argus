# Bug Patterns & Prevention Rules

Runtime knowledge base of bugs that have occurred and rules to prevent them.
Read by: Planner (warnings), Gate/Scouts (compliance check), Developer (awareness).

## Patterns

- [2026-04-17] PAT-001 [STORY-084]: Double-writer on abort/session-end metric vectors — Root Cause: Metric increment lives in both the low-level client (on error return) AND the high-level engine (on classified session outcome); a single logical abort double-counts when both layers fire. — Prevention: For every session-level counter (e.g. `*SessionAbortedTotal`, `*SessionFailureTotal`), designate exactly one writer (typically the engine / outermost orchestrator); inner layers return wrapped sentinel errors and do NOT touch counters. Document the invariant in the package's `doc.go`. Tests must assert the TOTAL across reason buckets for one logical abort equals exactly 1. — Affected: metrics layer (any protocol client + engine pair).
