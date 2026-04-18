# FIX-106: Operator Test Connection — Adapter Registry + Mock Seed Schema

> Tier 3 (flow completeness) — `POST /operators/{id}/test` returns 500 for
> multi-protocol operators because the adapter registry has no constructor
> for the derived primary protocol. Mock Simulator's seed is still in the
> pre-STORY-090 flat schema.

## User Story

As a tenant_admin wiring up an operator during onboarding, I click "Test"
on a Turkcell/Vodafone/Mock row and get a clear, actionable response —
success if the adapter connects, or a specific error pointing at the
misconfigured field — never a bare `500 Failed to create adapter`.

## Source Findings Bundled

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md`
- **F-6 HIGH** — `POST /operators/{id}/test` returns `500 INTERNAL_ERROR (Failed to create adapter)` for operators with `enabled_protocols=[diameter,radius,mock]`. `POST /test/mock` works, implying mock adapter constructor is present but diameter/radius ones are missing OR the primary-protocol derivation picks an unregistered one
- **F-7 HIGH** — Mock Simulator operator row has no protocol marked enabled; Test endpoint errors with "Operator has no enabled protocol"
- **F-8 HIGH** — Mock Simulator `adapter_config` is in legacy flat shape `{"host":"localhost","port":1812}` rather than nested STORY-090 schema `{radius: {...}, diameter: {...}, sba: {...}, http: {...}, mock: {...}}`

## Acceptance Criteria

### A. Adapter registry coverage (F-6)

- [ ] AC-1: Adapter registry has a constructor entry for every protocol a seeded operator can enable — at minimum `radius`, `diameter`, `sba`, `http`, `mock`. Missing constructors cause a compile-time or startup-time error, not a runtime 500.
- [ ] AC-2: `DerivePrimaryProtocol` handles the multi-protocol case deterministically — either (a) picks the first enabled protocol with a valid nested config, or (b) requires the caller to pass `protocol` as a query param. Plan decision must be explicit.
- [ ] AC-3: `POST /api/v1/operators/{id}/test` returns 200 with per-protocol result array for multi-protocol operators, OR 200 with single-protocol result when a specific protocol is targeted
- [ ] AC-4: On failure, response is 4xx with actionable error code (`NO_ADAPTER_FOR_PROTOCOL`, `CONFIG_INVALID`, `CONNECTION_FAILED`) — never a bare 5xx with opaque string
- [ ] AC-5: Test coverage: unit test exercises every enabled protocol's constructor path and error path

### B. Mock Simulator seed migration (F-7, F-8)

- [ ] AC-6: Seed data for "Mock Simulator" operator uses nested `adapter_config` schema matching STORY-090: `{"mock": {"host": "argus-operator-sim", "port": 9595, ...}}` (actual required fields per current adapter)
- [ ] AC-7: `enabled_protocols` column on Mock Simulator row includes `mock` (so Test endpoint does not bounce with "no enabled protocol")
- [ ] AC-8: Migration script for existing databases: identifies any operator row with legacy flat `adapter_config`, migrates to nested schema idempotently
- [ ] AC-9: After migration, `POST /operators/{mock_id}/test` returns 200 with successful connection to the `argus-operator-sim` container

### C. End-to-end green

- [ ] AC-10: All three operators (Turkcell, Vodafone TR, Turk Telekom, Mock Simulator) pass the Test button click as tenant_admin within their legitimate role scope (combined with FIX-101 AC-11/AC-12)
- [ ] AC-11: UAT-001 Step 4 "Connect operators" completes without any operator test returning 500

## Out of Scope

- New protocol support beyond what STORY-090 shipped
- Adapter internals (RADIUS/Diameter/SBA protocol details)
- Operator grant management (separate flow)

## Dependencies

- Blocked by: —
- Depends on: FIX-101 for role-scoped test access (the tenant_admin-can-call-test case)
- Blocks: UAT-001 Step 4 rerun, UAT-005 (operator failover tests)

## Architecture Reference

- Handler: `internal/api/operator/handler.go` — `Test` (~line 1151 per prior audit), `testConnectionForProtocol` (~line 1183), `DerivePrimaryProtocol`
- Registry: grep `adapterRegistry`, `NewAdapter`, `RegisterAdapter` in `internal/operator/` or `internal/aaa/adapter/`
- STORY-090 schema: `docs/stories/test-infra/STORY-090-plan.md` — nested adapter_config + Protocols tab
- Seed: `internal/seed/` — operator fixtures
- Mock adapter: existing constructor (grep `mock` in adapter packages)
- argus-operator-sim endpoint: port 9595 (per CLAUDE.md)

## Test Scenarios

- [ ] Unit: `DerivePrimaryProtocol` returns first enabled protocol; errors if none
- [ ] Unit: adapter factory returns correct adapter type per protocol; errors for unknown
- [ ] Integration: POST Test for each seeded operator → all return 200 with connection OK
- [ ] Regression: UAT-001 Step 4-4.7 passes

## Effort

M-L — adapter registry audit + seed migration + deterministic primary-protocol selection. One Dev iteration plus thorough Gate scout on adapter registration points.
