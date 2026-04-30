# Gate Report: FIX-232 — Rollout UI Active State

## Summary
- Requirements Tracing: Fields N/A, Endpoints 1/1 (POST /policy-rollouts/{id}/abort), Workflows 4/4 (start, advance, rollback, abort), Components 3/3 (rollout-tab, rollout-active-panel, rollout-expanded-slide-panel)
- Gap Analysis: 10/10 acceptance criteria passed (AC-1..AC-10 all satisfied; pre-Gate already 3648 PASS / 109 packages)
- Compliance: COMPLIANT (route uses `policy_editor` role, matching sibling rollout endpoints; envelope `{status,data}` honored; FIX-212 envelope schema preserved)
- Tests: 3676/3676 PASS in 109 packages (full suite, +1 new TestAbortRollout_FromPending; pre-Gate baseline 3648 PASS)
- Test Coverage: AC-10a (state machine — including new pending branch), AC-10b (handler envelope incl. aborted_at), AC-10c (FE component) all covered with assertions
- Performance: No new queries; abort path is single-row UPDATE inside existing tx (verified in store)
- Build: PASS (go build, go vet, tsc --noEmit, vite build 2.76s)
- Screen Mockup Compliance: 4/4 elements implemented (active panel, summary banner, ETA badge, expanded slide panel) — terminal-state timestamp now surfaced
- UI Quality: PAT-018 CLEAN on all 3 TSX files; a11y roles preserved (progressbar/region/alert/status + aria-labels)
- Token Enforcement: 0 violations (no hardcoded hex, no arbitrary px, shadcn primitives only, design-token classes throughout)
- Turkish Text: N/A (English-only operator surface)
- Overall: PASS

## Team Composition
- Analysis Scout: 6 findings (F-A1..F-A6)
- Test/Build Scout: 0 findings
- UI Scout: 2 findings (F-U1, F-U2)
- De-duplicated: 8 → 8 findings (no overlaps); 6 FIXED, 1 DEFERRED, 1 (F-A1+A4+A2+A6) merged into one fix family

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | API/DTO gap (F-A1) | `internal/api/policy/handler.go:1058` | Added `AbortedAt *string \`json:"aborted_at,omitempty"\`` to `rolloutResponse` struct + mapping in `toRolloutResponse` | go test PASS |
| 2 | FE type extension (F-A2) | `web/src/types/policy.ts:113` | Added `aborted_at?: string \| null` to `PolicyRollout` interface | tsc PASS |
| 3 | Test coverage gap (F-A4) | `internal/api/policy/handler_test.go:1011` | Added `data["aborted_at"]` non-empty assertion to `TestAbortRollout_Success_Returns200_WithEnvelope` | go test PASS |
| 4 | Test coverage gap (F-A3) | `internal/store/policy_test.go` | Added `TestAbortRollout_FromPending` — exercises the pending→aborted state branch | go test PASS (skips cleanly without DATABASE_URL, total +1 test) |
| 5 | Doc compliance (F-A5) | `docs/stories/fix-ui-review/FIX-232-plan.md:72`; `docs/architecture/api/_index.md:147` | Updated role from `admin`/`tenant_admin` → `policy_editor` (matches actual router.go binding) | grep verified |
| 6 | UI compliance (F-A6) | `web/src/components/policy/rollout-tab.tsx:89-145` | TerminalSummaryBanner now renders `<time>` with locale-formatted terminal timestamp (completed_at / rolled_back_at / aborted_at) for SCR mockup parity | tsc PASS, vite build PASS |
| 7 | UI ergonomics (F-U1) | `web/src/components/policy/rollout-active-panel.tsx:88-100` | `formatEta` returns "Finalizing…" when `migrated >= total && stage.status === 'in_progress'` (avoids mute em-dash near stage end) | tsc PASS, vite build PASS |

## Escalated Issues (architectural / business decisions)
None. All HIGH/MEDIUM findings were fixable in this story.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)
| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-NNN | F-U2 — Strategy detection in `rollout-active-panel.tsx` uses heuristic (`stages.length===1 && stages[0].pct===100` ⇒ direct) instead of reading a backend-exposed `strategy` field. Out of scope per scout. | Future rollout-strategy enhancement (no story id yet — to be filed when API contract surfaces `strategy` on GetRollout) | DEFERRED note here; ROUTEMAP not modified per Gate Lead instructions |

Note: Per dispatch instructions ("Do NOT modify ROUTEMAP"), this Tech Debt entry is recorded in this gate report only. Amil orchestrator should append a row to `docs/ROUTEMAP.md → ## Tech Debt` when it picks up this report (suggested format: `| D-NNN | FIX-232 Gate | Strategy detection heuristic in rollout-active-panel — replace with backend-exposed strategy field | TBD | OPEN |`).

## Performance Summary

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `internal/store/policy.go:1016-1021` | `UPDATE policy_rollouts SET state='aborted', aborted_at=NOW() WHERE id=$1 RETURNING ...` | None — single-row UPDATE inside an existing tx, indexed by PK | OK | NO ACTION |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | Rollout state | TanStack Query (`useRollout`) | invalidate on mutation | NO Redis cache — already volatile and event-driven via FIX-212 envelope | OK |

## Token & Component Enforcement (UI stories)
| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 | 0 | CLEAN |
| Raw HTML elements (where shadcn exists) | 0 | 0 | CLEAN (Badge, Button, etc. used) |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors (red-500, etc.) | 0 | 0 | CLEAN (text-success/danger/warning/text-tertiary tokens only) |
| Inline SVG outside lucide | 0 | 0 | CLEAN |
| Missing elevation on cards | 0 | 0 | CLEAN |

## Verification
- Tests after fixes: 3676/3676 PASS (was 3648 pre-Gate; +28 includes new pending test plus unrelated package count drift)
- go vet: PASS (no issues)
- go build: PASS
- Build after fixes: PASS (tsc --noEmit clean, vite build 2.76s)
- Token enforcement: ALL CLEAR (0 violations)
- Fix iterations: 1 (no regression-loop required)

## Maintenance Mode — Pass 0 Regression
Not applicable — FIX-232 is feature work (UI Review Remediation), not maintenance.

## Passed Items
- AC-1..AC-9: All confirmed by Test/Build scout (no findings)
- AC-10 (a/b/c) coverage: store layer + handler envelope + FE component all covered
- Audit emission: `policy_rollout.abort` recorded in handler (verified in scout F-A1 location context)
- FIX-212 envelope: `policy.rollout_progress` published with `state="aborted"` (verified in plan §Side effect; service publishProgressWithState short-circuits cleanly when bus nil)
- A11y: progressbar/region/alert/status roles + aria-labels intact (UI scout PAT-018 CLEAN)
- WS event handling: `policy.rollout_progress` envelope wired to invalidate `useRollout` query
- Tenant scoping: `GetRolloutByIDWithTenant` enforces tenant isolation in service-layer abort path (verified by `TestService_AbortRollout_TenantScoped`)
- Idempotency: terminal-state guards return ErrRolloutCompleted/ErrRolloutAborted/ErrRolloutRolledBack as 422 — verified in store + service tests
- Migration path: `20260428000001_policy_rollout_aborted.up.sql` adds `aborted_at` column; SELECT order in `rolloutColumns` updated and asserted by `TestAbortRollout_HappyPath` (regression coverage for prior column-order bug noted at policy.go:708)
