# Codex Go-Live Gap Audit

Date: 2026-04-15
Project: Argus
Scope: `docs/PRODUCT.md` + `docs/SCOPE.md` + `docs/architecture/**` + story/AC set + current codebase

## Audit Standard

This report answers one question:

Does the current codebase actually satisfy the product/scope/architecture requirements through the stories and their acceptance criteria, even when docs say `DONE`?

This is a single consolidated sweep. Findings below include:

- requirement exists but has no valid story ownership
- story is marked `DONE` but codebase is still partial / placeholder / stub / non-operable
- doc-to-story-to-code chain does not close

Within this sweep, clearing the findings below closes the currently identified go-live gaps in that chain. This is not a mathematical proof of zero bugs; it is the evidence-backed gap list from this audit pass.

## Verdict

No, the project is not yet in an "everything OK" state.

The current evidence-backed gap set is:

1. `F-057` OAuth2 client-credentials requirement is not actually owned or implemented, while `STORY-008` is marked `DONE`.
2. `F-025` Diameter↔RADIUS bridge exists in product/scope but has no owning story.
3. `STORY-075` is marked `DONE`, but SIM IP allocation history is still explicit placeholder UI.
4. `STORY-077` is marked `DONE`, but undo flow is not operable end-to-end.
5. `STORY-044` is marked `DONE`, but SIM usage tab still lacks the required CDR table.
6. `STORY-073` is marked `DONE`, but tenant resource spark trends are still zero-array placeholders.
7. `STORY-073` is marked `DONE`, but delivery status board is incomplete and partly stubbed.
8. `STORY-069` is marked `DONE`, but reporting still runs on `emptyReportProvider` stub data.
9. `STORY-048` was accepted with partial ACs and still remains partial in code.
10. `STORY-037` is marked `DONE`, but optional test-auth step remains unimplemented placeholder.

## Findings

### 1. Coverage gap + false DONE: `F-057 OAuth2 client credentials`

Severity: Critical

Requirement exists in product/scope/architecture:

- [docs/PRODUCT.md](/Users/btopcu/workspace/argus/docs/PRODUCT.md:106) defines `F-057: OAuth2 client credentials for third-party integration`
- [docs/SCOPE.md](/Users/btopcu/workspace/argus/docs/SCOPE.md:122) includes `OAuth2 client credentials`
- [docs/architecture/api/_index.md](/Users/btopcu/workspace/argus/docs/architecture/api/_index.md:6) states auth supports `OAuth2 (3rd party)`

Supposed owning story does not actually carry the requirement:

- [docs/stories/phase-1/STORY-008-api-key-management.md](/Users/btopcu/workspace/argus/docs/stories/phase-1/STORY-008-api-key-management.md:1) title says `API Key Management, Rate Limiting & OAuth2`
- but its AC/API contract only covers `API-150..154` API-key CRUD and rate limit flows in [STORY-008-api-key-management.md](/Users/btopcu/workspace/argus/docs/stories/phase-1/STORY-008-api-key-management.md:19)
- gate also certifies only those five API-key endpoints in [docs/stories/phase-1/STORY-008-gate.md](/Users/btopcu/workspace/argus/docs/stories/phase-1/STORY-008-gate.md:4) and [docs/stories/phase-1/STORY-008-gate.md](/Users/btopcu/workspace/argus/docs/stories/phase-1/STORY-008-gate.md:50)

Roadmap status creates false confidence:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:43) marks `STORY-008` as `DONE`

Conclusion:

`OAuth2 client credentials` is still uncovered in a usable implementation sense. The current story set closes API keys, not OAuth2.

Recommendation:

- reopen `STORY-008` or create a dedicated story for OAuth2 client credentials
- define explicit endpoints, token issuance flow, client registration/rotation/revocation, scopes, auth middleware path, tests, and gate criteria
- do not treat `F-057` as covered until the OAuth2 flow exists in code and docs

### 2. Coverage gap: `F-025 Diameter ↔ RADIUS bridge` has no valid story ownership

Severity: High

Requirement exists:

- [docs/PRODUCT.md](/Users/btopcu/workspace/argus/docs/PRODUCT.md:66) defines `F-025: Diameter ↔ RADIUS protocol bridge`
- [docs/SCOPE.md](/Users/btopcu/workspace/argus/docs/SCOPE.md:80) includes `Diameter ↔ RADIUS bridge`

But the roadmap has no owning story for it:

- AAA phase in [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:56) through [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:66) contains `STORY-015/016/017/019/020/021`
- none of those are a bridge story, and no separate roadmap item owns `F-025`

Conclusion:

This is a doc-to-story planning gap. Diameter server and RADIUS server exist, but the product requirement for a bridge is not traceably owned by a story/AC set.

Recommendation:

- add a dedicated bridge story with protocol translation scope, supported message types, routing rules, failure semantics, and interoperability tests
- if the bridge is intentionally out of v1, remove it from product/scope or mark as deferred explicitly

### 3. False DONE: `STORY-075` SIM IP allocation history is still placeholder

Severity: High

Story requirement:

- [docs/stories/phase-10/STORY-075-cross-entity-context.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-075-cross-entity-context.md:24) requires `IP Allocation History`

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:198) marks `STORY-075` as `DONE`
- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:257) claims the story completed

Code evidence says otherwise:

- [web/src/pages/sims/_tabs/ip-history-tab.tsx](/Users/btopcu/workspace/argus/web/src/pages/sims/_tabs/ip-history-tab.tsx:11) explicitly documents a scope-down and future follow-up
- [web/src/pages/sims/_tabs/ip-history-tab.tsx](/Users/btopcu/workspace/argus/web/src/pages/sims/_tabs/ip-history-tab.tsx:12) says full history requires a dedicated `ip_assignments` table + hook + endpoint
- [web/src/pages/sims/_tabs/ip-history-tab.tsx](/Users/btopcu/workspace/argus/web/src/pages/sims/_tabs/ip-history-tab.tsx:96) renders `Historical IP changes tracking coming soon`

Conclusion:

This AC is not complete. Current UI shows current allocation metadata, not allocation history.

Recommendation:

- implement persistent IP assignment history storage and query path
- replace the placeholder tab with actual allocation / release / rotation timeline data
- re-open `STORY-075` until AC-7 IP allocation history is really complete

### 4. False DONE: `STORY-077` undo flow is wired superficially but not operable

Severity: Critical

Story requirement:

- [docs/stories/phase-10/STORY-077-enterprise-ux-polish.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-077-enterprise-ux-polish.md:15) AC-2 requires undo for destructive actions via `POST /api/v1/undo/:action_id`

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:200) marks `STORY-077` as `DONE`

Backend endpoint exists but runtime chain is incomplete:

- [internal/api/undo/handler.go](/Users/btopcu/workspace/argus/internal/api/undo/handler.go:36) exposes `RegisterExecutor`
- [internal/api/undo/handler.go](/Users/btopcu/workspace/argus/internal/api/undo/handler.go:69) fails if no executor exists for the action
- [cmd/argus/main.go](/Users/btopcu/workspace/argus/cmd/argus/main.go:1044) creates `undoRegistry`
- [cmd/argus/main.go](/Users/btopcu/workspace/argus/cmd/argus/main.go:1045) creates `undoHandler`
- but there is no runtime executor registration callsite; repo search returns only the method declaration

Frontend support exists but is not backed by real action IDs:

- [web/src/hooks/use-undo.ts](/Users/btopcu/workspace/argus/web/src/hooks/use-undo.ts:33) posts to `/undo/:actionId`
- [web/src/components/shared/undo-toast.tsx](/Users/btopcu/workspace/argus/web/src/components/shared/undo-toast.tsx:1) exists

But destructive handlers are not producing an undo envelope/action registration path:

- search found no evidence of action registration in runtime code
- current implementation evidence shows undo handler wiring only, not end-to-end usable rollback

Conclusion:

Undo UI exists, undo endpoint exists, but actual destructive actions are not connected to registered inverse operations. This is a false-complete enterprise UX feature.

Recommendation:

- register executors during boot
- store undo payloads for all promised destructive actions
- return `action_id` from those operations
- verify end-to-end rollback for bulk suspend, bulk terminate, policy delete, segment delete, API key revoke

### 5. False DONE: `STORY-044` SIM usage tab still lacks required CDR table

Severity: High

Story requirement:

- [docs/stories/phase-8/STORY-044-frontend-sim.md](/Users/btopcu/workspace/argus/docs/stories/phase-8/STORY-044-frontend-sim.md:18) screen reference says usage tab includes `usage charts, CDR list`
- [docs/stories/phase-8/STORY-044-frontend-sim.md](/Users/btopcu/workspace/argus/docs/stories/phase-8/STORY-044-frontend-sim.md:34) AC explicitly requires `usage chart (30-day trend), CDR table`

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:114) marks `STORY-044` as `DONE`

Current code:

- [web/src/pages/sims/detail.tsx](/Users/btopcu/workspace/argus/web/src/pages/sims/detail.tsx:311) starts `UsageTab`
- [web/src/pages/sims/detail.tsx](/Users/btopcu/workspace/argus/web/src/pages/sims/detail.tsx:331) renders usage chart
- [web/src/pages/sims/detail.tsx](/Users/btopcu/workspace/argus/web/src/pages/sims/detail.tsx:402) renders summary cards
- there is no CDR table in the tab implementation

Conclusion:

The story’s usage-tab AC is still not fully met.

Recommendation:

- add paginated/sortable CDR table to SIM usage tab
- bind it to real SIM-scoped CDR data and align UX with the screen contract

### 6. False DONE: `STORY-073` tenant resource spark trends are still placeholders

Severity: High

Story requirement:

- [docs/stories/phase-10/STORY-073-admin-compliance-screens.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-073-admin-compliance-screens.md:29) AC-1 requires `Spark trend per metric`

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:192) marks `STORY-073` as `DONE`
- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:259) claims the story completed

Backend code explicitly returns placeholder spark arrays:

- [internal/api/admin/tenant_resources.go](/Users/btopcu/workspace/argus/internal/api/admin/tenant_resources.go:22) includes `Spark []int`
- [internal/api/admin/tenant_resources.go](/Users/btopcu/workspace/argus/internal/api/admin/tenant_resources.go:55) comment states time-series integration is future work
- [internal/api/admin/tenant_resources.go](/Users/btopcu/workspace/argus/internal/api/admin/tenant_resources.go:58) returns `Spark: make([]int, 7)`

Conclusion:

The sparkline AC is not complete. This is an explicit placeholder, not a real trend implementation.

Recommendation:

- source per-tenant time-series metric history
- populate real spark arrays for each resource card
- only keep story closed when frontend trend visuals reflect actual data

### 7. False DONE: `STORY-073` delivery status board is incomplete and partly stubbed

Severity: High

Story requirement:

- [docs/stories/phase-10/STORY-073-admin-compliance-screens.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-073-admin-compliance-screens.md:40) AC-12 requires per-channel health plus failed-delivery list, retry button, latency percentiles, channel health indicator

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:259) says `STORY-073 completed`

Backend still contains stubs:

- [internal/api/admin/delivery_status.go](/Users/btopcu/workspace/argus/internal/api/admin/delivery_status.go:67) labels email as stub with default success
- [internal/api/admin/delivery_status.go](/Users/btopcu/workspace/argus/internal/api/admin/delivery_status.go:70) labels telegram as stub / not instrumented

Frontend page is summary-only:

- [web/src/pages/admin/delivery.tsx](/Users/btopcu/workspace/argus/web/src/pages/admin/delivery.tsx:84) page starts
- [web/src/pages/admin/delivery.tsx](/Users/btopcu/workspace/argus/web/src/pages/admin/delivery.tsx:149) renders only channel cards
- there is no failed-delivery list and no retry action in the page

Conclusion:

This AC is only partially implemented. The board exists as high-level cards, but required operational controls and some channel instrumentation are missing.

Recommendation:

- instrument email/telegram delivery sources properly
- add failed delivery listing with retry action
- expose queue/dead-letter operational data per channel

### 8. False DONE: `STORY-069` reporting still runs on `emptyReportProvider`

Severity: Critical

Story requirement:

- [docs/stories/phase-10/STORY-069-onboarding-reporting.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-069-onboarding-reporting.md:29) AC-2 requires scheduled report generation
- [docs/stories/phase-10/STORY-069-onboarding-reporting.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-069-onboarding-reporting.md:34) AC-3 requires on-demand report generation
- [docs/stories/phase-10/STORY-069-onboarding-reporting.md](/Users/btopcu/workspace/argus/docs/stories/phase-10/STORY-069-onboarding-reporting.md:38) AC-4 requires report formats with real report types

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:188) marks `STORY-069` as `DONE`
- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:265) explicitly notes `emptyReportProvider stub per DEV-201`

Code evidence:

- [cmd/argus/main.go](/Users/btopcu/workspace/argus/cmd/argus/main.go:969) wires `report.NewEngine(&emptyReportProvider{})`
- [cmd/argus/main.go](/Users/btopcu/workspace/argus/cmd/argus/main.go:1893) documents `emptyReportProvider` as stub provider
- [cmd/argus/main.go](/Users/btopcu/workspace/argus/cmd/argus/main.go:1900) through [cmd/argus/main.go](/Users/btopcu/workspace/argus/cmd/argus/main.go:1921) return empty/minimal datasets for all report types
- [internal/api/reports/handler.go](/Users/btopcu/workspace/argus/internal/api/reports/handler.go:103) states generation always enqueues async and
- [internal/api/reports/handler.go](/Users/btopcu/workspace/argus/internal/api/reports/handler.go:105) explicitly says sync path is not implemented

Conclusion:

Reports may generate valid files structurally, but the data source is still a stub. For go-live readiness, this is not a complete reporting implementation.

Recommendation:

- replace `emptyReportProvider` with real data-backed provider
- verify each report type uses actual tenant data
- decide whether sync path is required by spec; if not, update story/spec, otherwise implement it

### 9. Still partial: `STORY-048` analytics page does not satisfy all accepted ACs

Severity: Medium

Story requirement:

- [docs/stories/phase-8/STORY-048-frontend-analytics.md](/Users/btopcu/workspace/argus/docs/stories/phase-8/STORY-048-frontend-analytics.md:24) requires comparison mode overlay previous period as dashed line
- [docs/stories/phase-8/STORY-048-frontend-analytics.md](/Users/btopcu/workspace/argus/docs/stories/phase-8/STORY-048-frontend-analytics.md:25) requires filter bar with `operator, APN, RAT type, segment`

Gate already admitted partial coverage:

- [docs/stories/phase-8/STORY-048-gate.md](/Users/btopcu/workspace/argus/docs/stories/phase-8/STORY-048-gate.md:29) marks dashed overlay as `PARTIAL`
- [docs/stories/phase-8/STORY-048-gate.md](/Users/btopcu/workspace/argus/docs/stories/phase-8/STORY-048-gate.md:30) marks segment filter as `PARTIAL`

Current code still reflects those gaps:

- [web/src/pages/dashboard/analytics.tsx](/Users/btopcu/workspace/argus/web/src/pages/dashboard/analytics.tsx:255) filter bar contains group, metric, operator, APN, RAT
- there is no segment filter in the filter bar
- [web/src/pages/dashboard/analytics.tsx](/Users/btopcu/workspace/argus/web/src/pages/dashboard/analytics.tsx:377) chart renders only current series
- no dashed previous-period overlay series exists in chart rendering

Conclusion:

This remains a known partial implementation. It should not be treated as fully closed if the standard is “fix this report and then everything is OK”.

Recommendation:

- add segment filter
- implement previous-period overlay or formally revise AC/spec to match shipped behavior

### 10. False DONE or accepted functional gap: `STORY-037` test authentication step is still placeholder

Severity: Medium

Story requirement:

- [docs/stories/phase-6/STORY-037-connectivity-diagnostics.md](/Users/btopcu/workspace/argus/docs/stories/phase-6/STORY-037-connectivity-diagnostics.md:32) AC-7 says optional test authentication should trigger through operator adapter

Story is marked done:

- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:297) says `STORY-037 completed`
- [docs/ROUTEMAP.md](/Users/btopcu/workspace/argus/docs/ROUTEMAP.md:295) review itself notes test auth is a placeholder

Code evidence:

- [internal/diagnostics/diagnostics.go](/Users/btopcu/workspace/argus/internal/diagnostics/diagnostics.go:366) defines `checkTestAuth`
- [internal/diagnostics/diagnostics.go](/Users/btopcu/workspace/argus/internal/diagnostics/diagnostics.go:372) sets status to warn
- [internal/diagnostics/diagnostics.go](/Users/btopcu/workspace/argus/internal/diagnostics/diagnostics.go:373) says `Test authentication not yet implemented`

Conclusion:

If strict go-live interpretation is used, this AC is still open. If business decides optional Step 7 can remain deferred, that deferral should be explicit and visible, not silently absorbed into `DONE`.

Recommendation:

- either implement operator-adapter-backed test auth
- or explicitly reclassify the AC as deferred / out of v1 and update story/status language

## What Was Checked

This sweep explicitly checked both of the user’s concern paths:

1. product/scope/architecture requirements vs story ownership
2. stories marked `DONE` vs actual codebase behavior

That means the report includes not only “missing story” gaps, but also “doc says done, code is not actually done” gaps.

## Close-Out Standard

For this audit sweep, the project should not be treated as go-live clean until:

- `F-057` has real story ownership and implementation
- `F-025` is either implemented with an owning story or explicitly deferred out of scope
- the false-DONE story gaps above are closed or formally reclassified in docs/status

If all findings in this report are fixed, the currently identified doc/story/AC/code mismatches from this sweep are closed.
