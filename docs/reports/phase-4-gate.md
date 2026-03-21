# Phase 4 Gate Report — Policy & Orchestration

**Date:** 2026-03-21
**Phase:** 4 — Policy & Orchestration
**Status:** PASS
**Stories:** STORY-022 to STORY-027 (all backend-only)

---

## Step 1: Deploy

| Check | Result |
|-------|--------|
| `make down` | OK — all containers stopped |
| `make build` | OK — Go binary + React SPA built |
| `make up` | OK — 5 containers started |
| Container health | All 5 healthy (argus, postgres, redis, nats, nginx) |

**Verdict:** PASS

---

## Step 2: Smoke Test

| Check | Result |
|-------|--------|
| Frontend (https://localhost:8084) | 200 |
| API Health (`/api/health`) | `{"status":"success","data":{"db":"ok","redis":"ok","nats":"ok","aaa":{"radius":"ok","diameter":"ok","sessions_active":0}}}` |
| PostgreSQL (`pg_isready`) | Accepting connections |

**Verdict:** PASS

---

## Step 2.5: Full Test Suite

| Metric | Value |
|--------|-------|
| Total tests | 672 |
| Passed | 672 |
| Failed | 0 |
| Packages | 36 (2 no test files) |

**Verdict:** PASS

---

## Step 3: E2E Browser Tests

**SKIPPED** — Phase 4 is all backend (no UI screens).

---

## Step 3.5: Functional Verification

### STORY-022: Policy DSL Parser & Evaluator

| Test | Result |
|------|--------|
| `go test ./internal/policy/dsl/...` | 47 tests PASS |
| Lexer, Parser, Compiler, Evaluator | All test files present |

### STORY-023: Policy CRUD & Versioning

| Test | Result |
|------|--------|
| Create policy (POST /api/v1/policies) | 201 + policy with v1 draft |
| List policies (GET /api/v1/policies) | 200 + policy list |
| Get policy detail (GET /api/v1/policies/{id}) | 200 + versions |
| Create version v2 (POST /api/v1/policies/{id}/versions) | 201 + v2 draft |
| Activate version (POST /api/v1/policy-versions/{id}/activate) | 200 + state=active |
| Invalid DSL rejected | 422 INVALID_DSL |
| Unauthenticated access | 401 |
| Delete policy (DELETE /api/v1/policies/{id}) | 204 |

### STORY-024: Policy Dry-Run Simulation

| Test | Result |
|------|--------|
| Dry-run (POST /api/v1/policy-versions/{id}/dry-run) | 200 + total_affected=3, sample_sims, by_operator, by_apn, by_rat |
| Standard envelope response | Correct (`{status, data}`) |

### STORY-025: Policy Staged Rollout (Canary)

| Test | Result |
|------|--------|
| Start rollout (POST /api/v1/policy-versions/{id}/rollout) | 201 + rollout_id, stages, state=in_progress |
| Get rollout (GET /api/v1/policy-rollouts/{id}) | 200 + current_stage, migrated_sims |
| Advance rollout (POST /api/v1/policy-rollouts/{id}/advance) | 200 |
| Rollback rollout (POST /api/v1/policy-rollouts/{id}/rollback) | 200 + state=rolled_back |

### STORY-026: Steering of Roaming (SoR) Engine

| Test | Result |
|------|--------|
| `go test ./internal/operator/sor/...` | 16 tests PASS |
| SoR migration (sor_fields) | Applied |

### STORY-027: RAT-Type Awareness

| Test | Result |
|------|--------|
| `go test ./internal/aaa/rattype/...` | 9 tests PASS |

**Functional Verdict:** PASS (14/14 API tests, all stories verified)

---

## Step 4-6: UI Tests

**SKIPPED** — Phase 4 is all backend (no frontend code).

---

## Step 6.5: Compliance Audit

### Router Endpoints (all registered in `internal/gateway/router.go`)

| Endpoint | Present |
|----------|---------|
| GET /api/v1/policies | Yes |
| POST /api/v1/policies | Yes |
| GET /api/v1/policies/{id} | Yes |
| PATCH /api/v1/policies/{id} | Yes |
| DELETE /api/v1/policies/{id} | Yes |
| POST /api/v1/policies/{id}/versions | Yes |
| PATCH /api/v1/policy-versions/{id} | Yes |
| POST /api/v1/policy-versions/{id}/activate | Yes |
| POST /api/v1/policy-versions/{id}/dry-run | Yes |
| POST /api/v1/policy-versions/{id}/rollout | Yes |
| GET /api/v1/policy-versions/{id1}/diff/{id2} | Yes |
| POST /api/v1/policy-rollouts/{id}/advance | Yes |
| POST /api/v1/policy-rollouts/{id}/rollback | Yes |
| GET /api/v1/policy-rollouts/{id} | Yes |

### DB Migrations

| Migration | Present |
|-----------|---------|
| Core schema (policies, policy_versions, policy_assignments, policy_rollouts) | Yes (20260320000002) |
| SoR fields | Yes (20260321000001) |

### Test Files

| Package | Test File |
|---------|-----------|
| `internal/policy/dsl` | lexer_test, parser_test, compiler_test, evaluator_test |
| `internal/policy/dryrun` | service_test |
| `internal/policy/rollout` | service_test |
| `internal/operator/sor` | engine_test |
| `internal/aaa/rattype` | rattype_test |
| `internal/api/policy` | handler_test |

**Compliance Verdict:** PASS

---

## Fixes Applied

### Fix 1: Ambiguous Column References in Policy Store (commit 5d73ca0)

**Problem:** Three SQL queries in `internal/store/policy.go` used unqualified column names (`id`, `state`, `created_at`) in JOIN contexts, causing PostgreSQL error `42702: column reference is ambiguous`.

**Affected methods:**
- `GetVersionWithTenant` — JOIN policy_versions + policies
- `GetRolloutByIDWithTenant` — JOIN policy_rollouts + policy_versions + policies
- `GetActiveRolloutForPolicy` — JOIN policy_rollouts + policy_versions

**Fix:** Replaced unqualified `policyVersionColumns`/`rolloutColumns` constants with explicitly table-aliased column lists (`pv.id`, `pr.id`, etc.).

**Impact:** Dry-run simulation and staged rollout API calls were returning 500 errors. After fix, both work correctly.

---

## Summary

| Area | Status |
|------|--------|
| Deploy | PASS |
| Smoke | PASS |
| Tests | 672/672 PASS |
| Functional API | 14/14 PASS |
| Compliance | PASS |
| Fixes | 1 (SQL ambiguity) |
| Escalated | 0 |

**PHASE 4 GATE: PASS**
