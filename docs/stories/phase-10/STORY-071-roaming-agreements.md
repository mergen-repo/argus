# STORY-071: Roaming Agreement Management

## User Story
As an operator manager, I want to define, track, and enforce roaming agreements per operator partner, so that SoR decisions, cost calculations, and compliance reports reflect the contractual terms with each partner.

## Description
PRODUCT.md lists roaming agreement management as in-scope (Layer 3) but zero implementation exists — no table, no API, no UI. This story creates the full CRUD lifecycle: agreement entity with SLA terms, cost terms, validity period, link to operator grants, renewal notifications, and SoR engine consultation.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-06 (Operator — SoR engine)
- Packages: internal/api/roaming, internal/store/roaming, internal/model, internal/operator/sor, migrations
- Source: PRODUCT.md F-072, SCOPE.md Layer 3

## Screen Reference
- SCR-150 (Roaming Agreements List — new)
- SCR-151 (Roaming Agreement Detail — new)
- SCR-041 (Operator Detail — agreement summary tab)

## Acceptance Criteria
- [ ] AC-1: `roaming_agreements` table: `id`, `tenant_id`, `operator_id` (FK), `partner_operator_name`, `agreement_type` (national/international/MVNO), `sla_terms` (JSONB: uptime_pct, latency_p95, max_incidents), `cost_terms` (JSONB: cost_per_mb, currency, volume_tiers, settlement_period), `start_date`, `end_date`, `auto_renew`, `state` (active/expired/terminated/draft), `created_by`, `created_at`, `updated_at`. CHECK constraint on state, FK to operators, tenant scoping. Down migration included.
- [ ] AC-2: CRUD API: `POST /api/v1/roaming-agreements`, `GET /api/v1/roaming-agreements` (list, cursor-paginated, filter by operator/state/expiry), `GET /:id`, `PATCH /:id`, `DELETE /:id` (soft-delete). RequireRole `operator_manager` or `tenant_admin`. Audit log on every mutation.
- [ ] AC-3: SoR engine integration: `internal/operator/sor` consults active roaming agreements when making steering decisions. If agreement exists with cost_terms, use agreement cost_per_mb instead of operator default. If agreement expired, log warning and fall back to operator default.
- [ ] AC-4: Renewal notification: Cron job checks agreements expiring within 30 days → notification (email + in-app) to tenant admin. Configurable `ROAMING_RENEWAL_ALERT_DAYS=30`.
- [ ] AC-5: Frontend: Roaming Agreements list page with filters (operator, state, expiry range). Detail page with edit form, SLA/cost terms editors, validity timeline, linked operator detail. Accessible from sidebar under "Operations" section and from Operator Detail as "Agreements" tab.

## Dependencies
- Blocked by: STORY-063 (notification channel wiring for renewal alerts)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] Integration: Create roaming agreement → SoR engine uses agreement cost_per_mb for sessions through that operator.
- [ ] Integration: Agreement expires → SoR falls back to operator default cost + warning logged.
- [ ] Integration: Agreement expiring in 25 days → renewal notification sent to tenant admin.
- [ ] E2E: Navigate to `/roaming-agreements` → list renders, create new → appears in list.

## Effort Estimate
- Size: M
- Complexity: Medium
