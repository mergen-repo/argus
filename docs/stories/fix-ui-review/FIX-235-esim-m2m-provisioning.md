# FIX-235: M2M eSIM Provisioning Pipeline — SGP.22 → SGP.02 Refactor + SM-SR Integration + Bulk Operations

## Problem Statement
Current eSIM subsystem designed around consumer telecom (GSMA SGP.22 RSP) — assumes QR-code scan + LPA pull model where end user interacts. Argus serves IoT/M2M fleets (sayaç modem, fleet telematics) — devices are headless, have no UI, no human user. Correct model is GSMA SGP.02 M2M RSP where platform **pushes** profiles to eUICC via SM-SR over OTA SMS/HTTP.

**Current state verified:**
- Schema fields `sm_dp_plus_id` (SGP.22 consumer)
- Per-profile Enable/Disable/Switch actions — fine per-operation
- **Missing:**
  - Bulk operations (millions of eUICC cannot be handled one-by-one)
  - SM-SR integration — platform → eUICC push mechanism
  - OTA command queue with state tracking
  - EID pool / stock management
  - Profile state sync callbacks (device ACK)
  - Operator Detail has no "eSIM Profiles" tab (F-184)

## User Story
As a platform operator managing 10M IoT eUICC devices, I want to switch subsets of my fleet from Operator A to Operator B via a single operation (filter-based or CSV-targeted), with platform-driven OTA push and real-time acknowledgment tracking — so I can respond to operator outages or cost negotiations in bulk without device touch.

## Architecture Reference
- Current: `internal/api/esim/handler.go` (CRUD + Enable/Disable/Switch)
- New subsystem: `internal/smsr/` (SM-SR client)
- Event bus: new subjects `esim.command.issued`, `esim.command.acked`, `esim.command.failed`
- UI: Admin tool for bulk ops (different from consumer flow)

## Findings Addressed
- F-172 (SGP.22 vs SGP.02 mismatch — architectural)
- F-178 (bulk operations missing)
- F-180 (lifecycle audit)
- F-181 (EID pool/stock management)
- F-182 (SIM detail eSIM reverse link)
- F-184 (Operator Detail eSIM tab)
- F-173/F-174/F-175/F-176 (UI polish — included in scope)

## Acceptance Criteria
- [ ] **AC-1:** New table `esim_ota_commands`: `id, eid, profile_id, command_type (enable|disable|switch|delete), target_operator_id, status (queued|sent|acked|failed|timeout), retry_count, created_at, sent_at, acked_at, error_message`. Indices on `(status, created_at)` for worker + `(eid, created_at DESC)` for per-device history.
- [ ] **AC-2:** New `esim_profile_stock` table: `operator_id, total, allocated, available, updated_at` — per-operator EID inventory tracking.
- [ ] **AC-3:** SM-SR adapter interface `internal/smsr/client.go`:
  ```go
  type Client interface {
      Push(ctx, eid, command OTACommand) (commandID string, err error)
      Callback(ctx, commandID, status string) error  // device ACK
  }
  ```
  Initial implementation: mock for dev; real SM-SR HTTPS API integration future.
- [ ] **AC-4:** Worker `internal/job/esim_ota_dispatcher.go`: dequeues `status=queued` commands, calls SM-SR Push, updates state. Retries with exponential backoff up to 5x.
- [ ] **AC-5:** Bulk endpoint `POST /api/v1/esim-profiles/bulk-switch`:
  ```json
  { "filter": { "operator_id": "<from>" }, "target_operator_id": "<to>", "reason": "..." }
  OR
  { "eids": ["E1", "E2", ...], "target_operator_id": "<to>" }
  ```
  Returns `{ job_id, affected_count }`. Async — processor enqueues OTA commands.
- [ ] **AC-6:** Callback endpoint `POST /api/v1/esim-profiles/callbacks/ota-status` — SM-SR posts device ACK. Updates command status. Secured with shared secret in header.
- [ ] **AC-7:** UI changes:
  - eSIM list — add bulk checkbox + sticky "Bulk Switch Operator" button
  - Operator Detail — new "eSIM Profiles" tab with stock summary (total/allocated/available) + linked list
  - SIM Detail — new "eSIM Profile" card showing EID, current operator, state, last provisioned, switch history link
- [ ] **AC-8:** Polish:
  - Operator column shows operator NAME not UUID (F-173)
  - EID masking: "89000000...971523" (first 8 + "..." + last 6) with copy button (F-174)
  - Remove redundant "SIM ID" column — use ICCID as clickable link (F-175)
  - State filter dropdown drops invalid "Failed" option (F-176) — add migration if `failed` state legitimate (yes, for timeout cases in OTA — keep in enum)
- [ ] **AC-9:** Remove "Create Profile" single-entry UI — replaced with "Allocate from Stock" (tenant-level) (F-177 replacement). Admin picks target SIM + operator, allocation pulls from `esim_profile_stock`.
- [ ] **AC-10:** Scheduled job `esim_stock_alert`: if available < 10% → fire alert (FIX-209) `type=esim_stock_low`.
- [ ] **AC-11:** Audit coverage: all OTA dispatches + callbacks → audit_logs `entity_type=esim_profile` (F-180).
- [ ] **AC-12:** Docs — `docs/architecture/PROTOCOLS.md` eSIM section documents SGP.02 flow + SM-SR integration contract.

## Files to Touch
- **DB:** `migrations/*` — 3 new tables (ota_commands, profile_stock), enum updates
- **Backend:**
  - `internal/smsr/` (NEW package)
  - `internal/api/esim/handler.go` — bulk endpoint, callback, stock endpoints
  - `internal/job/esim_ota_dispatcher.go` (NEW)
  - `internal/job/esim_stock_alert.go` (NEW)
- **Frontend:**
  - `web/src/pages/esim/*` — bulk bar, stock view
  - `web/src/pages/operators/detail.tsx` — new eSIM tab
  - `web/src/pages/sims/detail.tsx` — eSIM card
  - `web/src/hooks/use-esim.ts`
- **Docs:** `PROTOCOLS.md`, `ARCHITECTURE.md` eSIM chapter

## Risks & Regression
- **Risk 1 — Existing SGP.22 callers:** If mock/test flows rely on current single-profile APIs, preserve them via adapter. Bulk is additive.
- **Risk 2 — SM-SR vendor lock-in:** Abstraction via `Client` interface; mock for dev; real integration per-vendor contract.
- **Risk 3 — OTA command volume at 10M eUICC:** Worker rate-limited — configurable ops/sec per operator (operator-side SM-SR rate limits). Queue-backed — surges absorbed.
- **Risk 4 — Callback security:** Shared secret header + HMAC signature preferred; document in `DEPLOYMENT.md`.
- **Risk 5 — Partial bulk failure:** Some eUICC ACK fails — UI exposes per-EID detail for retry.

## Test Plan
- Unit: OTA dispatcher state transitions; stock allocation/deallocation
- Integration: bulk switch 100 profiles → 100 ota_commands created → worker processes → states update → stock decremented
- Browser: operator detail eSIM tab shows stock; bulk switch flow end-to-end (mock SM-SR)
- Load: 10K bulk switch — worker throughput, queue depth monitoring

## Plan Reference
Priority: P2 · Effort: XL · Wave: 10 · Depends: FIX-209 (stock alert), FIX-212 (event envelope for command events)
