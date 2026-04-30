# FIX-303 — Onboarding Route Alias + First-Login Redirect (RECURRENCE batch1 F-9)

> **Status:** PLAN
> **Source:** `docs/reports/uat-acceptance-2026-04-30.md` finding **F-1 (CRITICAL)** + UAT-001 step 3.5
> **Recurrence of:** batch1 FIX-101 ("Onboarding Flow Complete", DONE 2026-04-19) — story shipped, but the route-name + first-login redirect contract regressed (or was never wired end-to-end).
> **Effort:** **M** (S surface area on FE rename, but BE auth DTO + Service wiring is real work and four UserInfo construction sites must change in lockstep)

---

## 1. Symptom + Reproduction

**UAT-001 step 3.5:** "After login, redirected to `/onboarding` (SCR-003)" → **FAIL — CRITICAL**.

Reproduction (manual):
1. Reset DB: `make db-reset` (or seed a fresh tenant_admin with no completed onboarding session).
2. Login as that tenant_admin at `http://localhost:8084/login`.
3. **Observed:** Lands on `/` (Dashboard).
4. Manually navigate to `/onboarding` → **404**.
5. Manually navigate to `/setup` → wizard renders (page exists, just at the wrong path).

---

## 2. Triage Findings — What Exists vs What's Missing

### Wizard FE (EXISTS, intact)
- `web/src/pages/auth/onboarding.tsx` — page wrapper around `<OnboardingWizard />`.
- `web/src/components/onboarding/wizard.tsx` — 5-step wizard with localStorage resume + bootstrap session create.
- `web/src/hooks/use-onboarding.ts` — calls `/onboarding/status`, `/onboarding/start`, `/onboarding/{id}`, `/onboarding/{id}/step/{n}`, `/onboarding/{id}/complete`.
- `web/src/router.tsx:113` — registered at **`/setup`** (the bug).

### Wizard BE (EXISTS, fully wired)
- `internal/api/onboarding/handler.go` — full handler set (`status`, `start`, `get`, `step/{n}`, `complete`).
- `internal/store/onboarding_session_store.go` — `Create`, `GetByID`, `GetLatestByTenant`, `UpdateStep`, `MarkCompleted` all implemented.
- `internal/gateway/router.go:879..883` — handler mounted under `/api/v1`.

### Documentation Disagreement (canonical name conflict)
| Source | Says |
|---|---|
| `docs/screens/SCR-003-onboarding.md:6` | `Route: /setup` |
| `docs/SCREENS.md:12` | `/setup` |
| Backend API namespace (`/api/v1/onboarding/*`) | `onboarding` |
| FE hook (`use-onboarding.ts`) calls | `/onboarding/...` |
| UAT report F-1 expects | `/onboarding` |
| FIX-101 spec (`FIX-101-plan.md` L17) refers to FE files under `web/src/components/onboarding/` and `web/src/pages/onboarding/` | `onboarding` |

**Every signal except SCR-003/SCREENS.md row points to `/onboarding`.** SCR-003 is the outlier — likely a pre-FIX-101 artifact that was never updated when the API namespace was finalized.

### First-Login Redirect Logic (BROKEN — phantom field)
- `web/src/components/auth/protected-route.tsx:13`:
  `if (user && user.onboarding_completed === false && location.pathname !== '/setup') { Navigate('/setup') }`
- `web/src/pages/auth/login.tsx:74`:
  `if (data.user.onboarding_completed === false) { navigate('/setup') }`
- **Backend `internal/auth/auth.go:117` `UserInfo` struct has only `id/email/name/role`.** The `onboarding_completed` field is **never serialized** by the BE.
- Result: FE always sees `user.onboarding_completed === undefined`, the strict-equality check `undefined === false` is `false`, the redirect never fires, and the user lands on Dashboard.

---

## 3. Root Cause

**Two compounding defects** that together produce F-1:

1. **Route name mismatch.** The frontend route is registered at `/setup`; the canonical name (per backend API, FE hook usage, and UAT contract) is `/onboarding`. Every external link/test that hits `/onboarding` 404s.
2. **`onboarding_completed` is a phantom field.** Frontend redirect guards check `user.onboarding_completed === false`, but the backend `UserInfo` DTO does not include this field. The field is `undefined` in every login response, so the strict-equality guard is always falsy and the first-login redirect never triggers.

Defect (2) is the deeper one. Even if FIX-303 only renamed the route to `/onboarding`, the user would still land on Dashboard on first login because the redirect itself is dead code.

### Why FIX-101 didn't catch this
FIX-101 focused on aligning wizard step numbering and operator-test role scoping. Its ACs covered the wizard's *internal* flow (steps 1–5, save/resume) but did **not** assert end-to-end first-login behavior. There was no Phase Gate UI test that loaded `/onboarding`, performed a fresh tenant_admin login, and asserted URL redirection. That gap is what allowed the route-name + phantom-field defect to ship undetected.

---

## 4. Fix Approach

### 4.1 Canonical route choice: **rename `/setup` → `/onboarding`**

**Decision:** Rename `/setup` to `/onboarding`. The vast majority of evidence (BE namespace, FE hook calls, UAT contract, FIX-101 file paths) already uses `onboarding`. SCR-003 and SCREENS.md row 12 are the only outliers and both will be updated.

**Transition:** Keep `/setup` as a one-line redirect (`<Navigate to="/onboarding" replace />`) for one wave to protect any deep links / browser-bookmarks / external docs. Add a `// TODO(D-NNN): remove /setup redirect after Wave 11 P1` comment so it's tracked for cleanup.

### 4.2 Fix the phantom `onboarding_completed` field

Use `onboarding_sessions.state == "completed"` as the source of truth. **No new column on `tenants` is added** (avoids denormalization, avoids a migration).

**BE changes (in priority order):**
1. Add `OnboardingCompleted bool \`json:"onboarding_completed"\`` to `UserInfo` struct (`internal/auth/auth.go:117`).
2. Add `OnboardingSessionRepository` interface dependency on `Service` (lookup by tenant ID, return `state == "completed"`):
   ```go
   type OnboardingSessionRepository interface {
       GetLatestByTenant(ctx context.Context, tenantID uuid.UUID) (*store.OnboardingSession, error)
   }
   ```
   Wire via a setter `WithOnboardingSessions(r OnboardingSessionRepository)` (mirroring `WithPasswordHistory`).
3. Populate `UserInfo.OnboardingCompleted` at **all four construction sites** in `auth.go`:
   - `:250` (password change required path) — populate.
   - `:267` (2FA challenge path) — populate.
   - `:287` (full-session login path) — populate.
   - `:701` (token refresh / re-login path) — populate.
   Helper: `s.isOnboardingCompleted(ctx, tenantID) bool` that returns false on store-nil or error (fail-safe: redirect to wizard rather than skip it).
4. Wire the dependency in `cmd/argus/main.go` (or wherever `auth.NewService` is constructed).

### 4.3 FE changes

1. `web/src/router.tsx:113` — change path from `/setup` to `/onboarding`. Add a sibling `{ path: '/setup', element: <Navigate to="/onboarding" replace /> }` for transition.
2. `web/src/components/auth/protected-route.tsx:13-14` — replace `'/setup'` → `'/onboarding'` (both occurrences).
3. `web/src/pages/auth/login.tsx:75` — replace `navigate('/setup')` → `navigate('/onboarding')`.
4. `web/src/components/layout/topbar.tsx:150` — replace `navigate('/setup')` → `navigate('/onboarding')`.

### 4.4 Doc updates

1. `docs/screens/SCR-003-onboarding.md:6` — change `Route: /setup` → `Route: /onboarding`.
2. `docs/SCREENS.md:12` — change `/setup` → `/onboarding` in the SCR-003 row.

---

## 5. Acceptance Criteria

| AC | Description | Verification |
|----|-------------|---|
| AC-1 | `GET /onboarding` returns 200 (page renders) for an authenticated tenant_admin with incomplete onboarding | dev-browser: navigate to `/onboarding`, assert wizard step 1 visible |
| AC-2 | Fresh tenant_admin (no completed onboarding session) first login → auto-redirect to `/onboarding` (no manual nav) | Go test on `/auth/login` response: `user.onboarding_completed == false`. Dev-browser: login flow ends on `/onboarding`. |
| AC-3 | After wizard completion (`POST /onboarding/{id}/complete` returns 200), the next login response sets `user.onboarding_completed == true` and lands on Dashboard | Go test: complete a session, re-login, assert flag true. **Note:** Source of truth is `onboarding_sessions.state == "completed"` for the tenant, NOT `tenants.onboarded_at` (no such column; we deliberately avoid the denormalization). |
| AC-4 | Existing tenants with a previously-completed onboarding session skip the wizard and land on Dashboard on every login | Go test: seeded tenant with completed session → login response `onboarding_completed: true` → no redirect. |
| AC-5 | dev-browser smoke (UAT-001 step 3.5): full first-login flow ends at `/onboarding`, wizard step 1 visible | Phase Gate UI suite step. |
| AC-6 | Legacy `/setup` URL redirects to `/onboarding` (transition path) | dev-browser: `GET /setup` → `Navigate` to `/onboarding`, assert URL final = `/onboarding`. |
| AC-7 | Documentation: `docs/screens/SCR-003-onboarding.md` and `docs/SCREENS.md` row 12 both say `/onboarding` | grep check; doc reviewer. |

---

## 6. Reproduction Tests (regression coverage)

The unit-level test that would have caught this regression is at the **BE auth-DTO layer**, not the FE redirect:

### 6.1 Backend Go test — `internal/api/auth/handler_login_onboarding_test.go` (NEW)

| Test case | Setup | Assert |
|-----------|-------|--------|
| `Test_Login_OnboardingIncomplete_FlagFalse` | tenant_admin user; no row in `onboarding_sessions` for tenant | `loginResponse.User.OnboardingCompleted == false` |
| `Test_Login_OnboardingInProgress_FlagFalse` | tenant_admin user; `onboarding_sessions.state = 'in_progress'` for tenant | `loginResponse.User.OnboardingCompleted == false` |
| `Test_Login_OnboardingCompleted_FlagTrue` | tenant_admin user; `onboarding_sessions.state = 'completed'` for tenant | `loginResponse.User.OnboardingCompleted == true` |
| `Test_Login_OnboardingStoreNil_FailSafeFalse` | auth service constructed without `WithOnboardingSessions` | `OnboardingCompleted == false` (redirect to wizard rather than silently skip) |

These four tests would have caught the regression: a fresh tenant always failed (1) and it would never have shipped.

### 6.2 Dev-browser E2E — Phase Gate UI suite (NEW)

Add to `docs/qa/phase-gate-ui-suite.md` (or equivalent dev-browser playbook):

```
PHASE-GATE-UI: Onboarding First-Login Redirect
1. Reset DB (db-reset).
2. Navigate to http://localhost:8084/login.
3. Login as fresh tenant_admin (e.g., a new seed-created admin@<freshtenant>.io / seed pwd).
4. Assert: URL ends with /onboarding (NOT /).
5. Assert: Wizard step 1 ("Welcome") visible.
6. Navigate to /setup → assert URL ends with /onboarding (transition redirect works).
7. Complete wizard or stub completion: PATCH `onboarding_sessions.state` = 'completed' for the tenant.
8. Logout, re-login.
9. Assert: URL ends with / (Dashboard, not /onboarding).
```

This Phase Gate step is what *would have caught the route-name regression* even if BE tests passed.

### 6.3 Frontend unit test (low priority, defensive)
- `web/src/components/auth/__tests__/protected-route.test.tsx` (NEW or extend) — assert that `user.onboarding_completed === false` triggers `Navigate('/onboarding')`. Catches FE typos in the path string post-rename.

---

## 7. Files Changed

### Backend
| File | Change |
|---|---|
| `internal/auth/auth.go` | Add `OnboardingCompleted` to `UserInfo` (line 117); add `OnboardingSessionRepository` interface + `Service.onboardingSessions` field + `WithOnboardingSessions` setter; add `s.isOnboardingCompleted(ctx, tenantID)` helper; populate the flag at all four `UserInfo{}` construction sites (lines 250, 267, 287, 701). |
| `internal/api/auth/handler.go` | No struct change — `loginResponse.User` is already `authpkg.UserInfo` so the new field flows through automatically. |
| `cmd/argus/main.go` (or wherever `auth.NewService` is wired) | Call `.WithOnboardingSessions(onboardingSessionStore)` on the auth service. |
| `internal/api/auth/handler_login_onboarding_test.go` (NEW) | Four-case Go test (see §6.1). |

### Frontend
| File | Change |
|---|---|
| `web/src/router.tsx` | Rename `/setup` → `/onboarding`; add `/setup` → `Navigate('/onboarding')` redirect with `// TODO(D-NNN)` cleanup comment. |
| `web/src/components/auth/protected-route.tsx` | Replace `'/setup'` → `'/onboarding'` (both occurrences, lines 13–14). |
| `web/src/pages/auth/login.tsx` | Replace `navigate('/setup')` → `navigate('/onboarding')` (line 75). |
| `web/src/components/layout/topbar.tsx` | Replace `navigate('/setup')` → `navigate('/onboarding')` (line 150). |

### Docs
| File | Change |
|---|---|
| `docs/screens/SCR-003-onboarding.md` | Line 6: `Route: /setup` → `Route: /onboarding`. |
| `docs/SCREENS.md` | Row 12: `/setup` → `/onboarding`. |
| `docs/qa/phase-gate-ui-suite.md` (or new file under `docs/qa/`) | Add the dev-browser test from §6.2. |

### Story metadata
| File | Change |
|---|---|
| `docs/stories/uat-remediation/FIX-303-onboarding-route-alias.md` (NEW story spec) | Story doc with ACs above. |
| `docs/ROUTEMAP.md` | Add FIX-303 row under "UI Review Remediation" / "UAT batch2 RECURRENCE" track. |

---

## 8. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Renaming a route breaks existing browser bookmarks / docs / Slack links to `/setup` | Keep `/setup` as a one-wave redirect to `/onboarding`. Track removal via `// TODO(D-NNN)` and a deferred entry. |
| Auth service circular import: `internal/auth` importing `internal/store/OnboardingSession` may create a cycle | Use a small interface (`OnboardingSessionRepository`) defined locally in `internal/auth`, not the concrete store type. Mirrors existing `UserRepository`/`SessionRepository` pattern. |
| `WithOnboardingSessions` not called → `s.onboardingSessions == nil` → flag would be undefined behavior | `isOnboardingCompleted` helper returns `false` on `nil` store (fail-safe to "show wizard"). Covered by AC-4 case 4 in §6.1. |
| Token refresh path (auth.go:701) is not the primary login but still emits a `LoginResult` — easy to forget to populate the flag | Explicitly enumerated in §4.2 as one of the four sites, and covered by an integration test. |
| Multi-tenant: if a user belongs to many tenants (current model is single-tenant per user), `GetLatestByTenant` is correct. If model evolves, revisit | Current `User` model has a single `TenantID`. Out of scope for FIX-303. |
| FIX-101 didn't catch this; FIX-303 must include a Phase Gate UI test or it'll regress again | §6.2 dev-browser test added to Phase Gate UI suite as a hard requirement of this story. |

---

## 9. Out of Scope

- Adding `tenants.onboarded_at` column (deliberately avoided; `onboarding_sessions.state == 'completed'` is sufficient single source of truth).
- Reworking SCR-003 wizard steps or copy.
- Removing the `/setup` transition redirect — that's a follow-up D-NNN.
- Multi-tenant onboarding (one wizard per tenant per user) — current model is single-tenant per user.

---

## 10. Effort Estimate

**M.** Net surface area: 1 BE struct field, 1 interface, 1 helper, 4 BE call-site updates, 1 main.go wiring, 1 BE test file, 4 FE files (1-line changes each), 2 doc files, 1 Phase Gate UI test. The "S" surface size is misleading because the BE wiring (interface + setter + main.go + four sites) must be coordinated, and the regression test is non-trivial to author (needs DB seed for an in-progress + completed onboarding session).
