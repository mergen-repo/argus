# Post-Story Review: STORY-005 — Tenant Management & User CRUD

> Date: 2026-03-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-006 | Structured logging / NATS event bus. No direct dependency on tenant/user CRUD. STORY-005 already uses zerolog with component-scoped loggers (`tenant_handler`, `user_handler`). When STORY-006 adds correlation ID middleware, tenant/user handlers will automatically benefit via request context. No code changes needed in STORY-005 outputs. | NO_CHANGE |
| STORY-007 | Audit log service. STORY-005 handlers call `audit.CreateEntry` which is currently a stub (returns empty `&Entry{}`). When STORY-007 implements the real hash-chain audit service, the `audit.CreateEntryParams` struct and `audit.Service` interface are already used correctly by tenant and user handlers. Wire-compatible. | NO_CHANGE |
| STORY-008 | API key management. API keys are scoped per tenant (`tenant_id` FK on TBL-04). STORY-005 provides `TenantStore.GetByID` which STORY-008 can use to validate tenant existence and state before key creation. `CodeResourceLimitExceeded` error code added in STORY-005 may also be useful if API key limits are introduced. Rate limiting needs Redis (STORY-006 dependency). No STORY-005 changes needed. | NO_CHANGE |
| STORY-009 | Operator CRUD. Depends on STORY-005 (tenant management). `TenantStore.GetByID` is needed to validate tenant existence when creating operator grants. Operator grant endpoints (API-026) will use `tenant_id` from context, same pattern as user CRUD. `RouterDeps` struct pattern established in STORY-005 makes it straightforward to add operator handler fields. | NO_CHANGE |
| STORY-038 | Notification engine. Depends on STORY-005 (needs users for notification targets). `UserStore.ListByTenant` provides the user list. `userResponse` DTO excludes sensitive fields (password_hash, totp_secret) which is important for notification rendering. | NO_CHANGE |
| STORY-049 | Frontend settings pages. Depends on STORY-005 APIs. API contracts (API-006 to API-008, API-010 to API-014) are finalized. Response DTOs are stable. Cursor-based pagination in list endpoints matches the frontend pagination pattern from G-041. | NO_CHANGE |

## Architecture Evolution

### RouterDeps Pattern
STORY-005 introduced `RouterDeps` struct in `gateway/router.go` to replace the growing positional parameter list in `NewRouter`. This pattern uses nil-checks (`if deps.TenantHandler != nil`) for optional handler registration. This scales well -- STORY-009 (Operator), STORY-008 (API Key), etc. simply add new fields to `RouterDeps`.

**Backward compatibility preserved:** `NewRouter(health, authHandler, jwtSecret)` still works as a wrapper around `NewRouterWithDeps`.

### RoleLevel/HasRole Moved to apierr
`RoleLevel()` and `HasRole()` functions were moved from `gateway/rbac.go` to `apierr/apierr.go` to resolve an import cycle (handlers in `api/*` packages need role comparison but cannot import `gateway`). This is architecturally sound -- role logic belongs with the context key definitions in `apierr`. The `gateway` package's `RequireRole` middleware still works because it can import `apierr`.

### Store Pattern Consolidation
`TenantStore` follows the same patterns established by `store/job.go` (STORY-001): `pgxpool.Pool`, cursor pagination with `limit+1`, `isDuplicateKeyError`, parameterized queries. This consistency reduces cognitive load for future store implementations (Operator, APN, SIM stores).

### Audit Integration Pattern
Both handlers use a private `createAuditEntry` method that gracefully handles nil `auditSvc` (returns early). This pattern should be extracted to a shared helper or embedded struct before STORY-009 to avoid duplication. Not blocking -- just technical debt to address.

## Glossary Check

| Term | Status | Notes |
|------|--------|-------|
| Tenant | EXISTS | Already in GLOSSARY.md: "An isolated enterprise customer account in Argus" |
| Resource Limits | NEW_CANDIDATE | `max_sims`, `max_apns`, `max_users` per tenant. Concept referenced in F-062, G-022, but not in GLOSSARY. |
| Invited (user state) | EXISTS_IMPLICITLY | User state machine includes "invited" but not explicitly in GLOSSARY. Part of the user lifecycle, not a standalone domain term. |

**Decision:** No GLOSSARY updates needed. "Resource limits" is a standard term that doesn't need a domain-specific definition. The tenant and user state machines are documented in the story specs and implementation.

## FUTURE.md Check

No new items or invalidated items. STORY-005 is core CRUD -- it does not introduce any new capabilities that would suggest future features beyond what's already planned.

## decisions.md Check

No new architectural or technical decisions made in STORY-005. All patterns followed existing conventions:
- Cursor pagination (pre-existing from G-041)
- Dynamic UPDATE building (pre-existing store pattern)
- Role elevation prevention (pre-existing from DEV-009 context)
- Nil-check handler registration (minor pattern, not decision-worthy)

**Decision:** No updates to decisions.md needed.

## Cross-Doc Consistency

| Check | Status | Notes |
|-------|--------|-------|
| ARCHITECTURE.md RBAC matrix vs implementation | OK | Tenant endpoints use `super_admin` (manage tenants row). User endpoints use `tenant_admin` (manage users row). Both match the RBAC matrix. |
| ARCHITECTURE.md API list vs implementation | OK | API-006 to API-008 (Users) and API-010 to API-014 (Tenants) all implemented and wired. |
| ARCHITECTURE.md project structure | OK | `internal/api/tenant/` and `internal/api/user/` exist as specified. `internal/store/tenant.go` added to existing store package. |
| MIDDLEWARE.md middleware chain | OK | Routes use JWTAuth + RequireRole in correct order. No new middleware introduced. |
| ERROR_CODES.md | DRIFT_NOTED | Two new error codes added (`RESOURCE_LIMIT_EXCEEDED`, `TENANT_SUSPENDED`) in `apierr/apierr.go`. ERROR_CODES.md should be updated to include these. Pre-existing drift: doc references `internal/gateway/errors.go` but codes live in `internal/apierr/apierr.go` (noted in STORY-003 review). Non-blocking. |
| CONFIG.md | OK | No new env vars introduced. |
| Story spec AC vs Gate report | OK | All 10 acceptance criteria passed in gate report. 8/8 endpoints wired. |
| PRODUCT.md F-059 (multi-tenant) | OK | All queries scoped by `tenant_id`. User store methods use `TenantIDFromContext`. Tenant store GetByID is unscoped (super_admin access) by design. |
| PRODUCT.md F-062 (resource limits) | OK | `max_users` enforcement implemented in user Create handler. `max_sims` and `max_apns` will be enforced in respective future stories (STORY-011, STORY-010). |
| PRODUCT.md BR-6 (tenant isolation) | OK | Cross-tenant access blocked at handler level (tenant ID comparison) and store level (WHERE tenant_id clause). |

## Story Updates

No changes needed to STORY-005 spec. All acceptance criteria met. No scope creep, no deferred items.

## Observations

1. **Duplicate `createAuditEntry` pattern.** Both `tenant/handler.go` and `user/handler.go` have nearly identical `createAuditEntry` methods. This will repeat in STORY-009 (Operator), STORY-010 (APN), STORY-011 (SIM). Consider extracting to a shared `api/audit_helper.go` or embedding in a base handler struct. Low priority -- can be addressed as part of a future refactoring pass.

2. **Cursor pagination with UUID-only cursor.** As noted in gate report (and STORY-002 review), using `id < cursor` with `ORDER BY created_at DESC, id DESC` doesn't guarantee deterministic ordering when timestamps collide. This is a known pre-existing pattern. Admin endpoints (tenant/user list) have small result sets where this is unlikely to cause issues. For high-volume endpoints (SIM list in STORY-011), a composite cursor (`created_at + id`) should be considered.

3. **`CodeTenantSuspended` added but not used.** The error code constant was added to `apierr.go` but no handler currently checks tenant state before processing requests. This is a forward-looking addition -- tenant suspension enforcement (rejecting API calls from suspended tenants) will need a TenantContext middleware (referenced in STORY-004 review, observation #2). This should be addressed before Phase 2 when tenants start having real data.

4. **No DELETE endpoints.** Consistent with story spec. Tenants use state machine (active -> suspended -> terminated). Users have no delete (disable instead). This matches the audit-first philosophy (BR-7).

5. **ESC-001 (Linear RBAC) remains active.** STORY-005 operates within the linear RBAC model safely (only uses `super_admin` and `tenant_admin` boundaries). The escalation deadline is pre-STORY-011 (Phase 2). No change to timeline.

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| ROUTEMAP.md | STORY-005 marked DONE, progress updated to 5/55 (9%) | UPDATED |
| decisions.md | No new decisions | NO_CHANGE |
| GLOSSARY.md | No new terms needed | NO_CHANGE |
| ARCHITECTURE.md | No changes needed | NO_CHANGE |
| FUTURE.md | No new items | NO_CHANGE |
| ERROR_CODES.md | Two new codes (RESOURCE_LIMIT_EXCEEDED, TENANT_SUSPENDED) should be added | DRIFT_NOTED |
| MIDDLEWARE.md | No changes | NO_CHANGE |
| CONFIG.md | No changes | NO_CHANGE |
| CLAUDE.md | No changes | NO_CHANGE |
| FRONTEND.md | No changes (backend-only story) | NO_CHANGE |

## Project Health

- Stories completed: 5/55 (9%)
- Current phase: Phase 1 -- Foundation (5/8 stories done, 63% of Phase 1)
- Next story: STORY-006 (Structured Logging, Config & NATS Event Bus)
- Blockers: None
- Escalations: 1 active (ESC-001: linear RBAC hierarchy, deadline pre-STORY-011)
- Quality: 21 new tests (6 store + 7 tenant handler + 8 user handler), all passing. Full suite green. 0 gate fixes needed.
- Cumulative tests: ~50+ test functions across store, api/tenant, api/user, gateway, auth, apierr packages
- Technical debt: duplicate `createAuditEntry` pattern (3 copies expected by Phase 2), cursor pagination needs composite cursor for high-volume endpoints
