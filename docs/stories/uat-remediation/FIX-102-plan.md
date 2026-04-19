# Fix Plan: FIX-102 — Bulk Import Completeness (Audit + Notifications + Policy Auto-Assign)

> Pre-release BUGFIX. Effort: L. Blocked by: FIX-104 (audit hash chain integrity).
> DO NOT merge before FIX-104 lands — AC-1/AC-2/AC-4 are only meaningful with a transactional chain writer.

## Root Causes

| ID | Finding | Root Cause |
|----|---------|------------|
| F-11 | Only 1 summary audit row for 495 SIMs | `BulkImportProcessor.Process` never calls the audit writer; no `audit.Auditor` dependency injected into the struct |
| F-12 | Zero notification rows after import | `BulkImportProcessor` publishes `SubjectJobCompleted` event but nobody subscribes to that for notifications; no `notification.Service` dependency |
| F-14 | All SIMs have `policy_version_id = NULL` | `allocateIPAndPolicy` passes `apn.DefaultPolicyID` (a `policies.id` UUID) as `policyVersionID` to `SetIPAndPolicy` — **type mismatch** (column expects `policy_versions.id`). Additionally, the function only runs when an IP pool exists; under STORY-092 (auth-time IP), pools are often empty so the function exits early and policy is never set |
| F-11b | 990 state_history rows for 495 SIMs (double write) | Line 228 calls `InsertHistory(nil, "ordered")` explicitly, then line 230 calls `TransitionState("active")` which internally writes `ordered→active`. Result: `NULL→ordered` + `ordered→active` = 2 rows/SIM. The `NULL→ordered` row has no counterpart in the single-SIM path and is **spurious** |
| AC-3 | No summary `sim.bulk_import` audit row | The existing row in UAT data came from seed SQL, not from the import code. `BulkHandler.Import` route has no audit middleware. The summary audit entry must be emitted explicitly in the job runner |

## Key Decisions

### D-1: Per-SIM audit — individual via NATS (not batch)

Emit each `sim.create` and `sim.activate` audit event individually through `FullService.PublishAuditEvent` (NATS async path). This routes through the post-FIX-104 transactional chain writer without blocking the hot loop. The chain writer serializes inserts at the DB level — no batching needed.

**Rejected alternative:** Batch INSERT of N audit rows. Would bypass the hash chain writer and break chain integrity.

### D-2: Policy resolution — match single-SIM activate path (ListReferencingAPN)

The spec says "same policy-resolution function as the single-SIM create path." But the single-SIM **create** path does zero policy work; it's the single-SIM **activate** path (handler.go:919-934) that resolves policy via `policyStore.ListReferencingAPN(tenantID, apn.Name)` → picks first active policy with `CurrentVersionID`.

**The existing `allocateIPAndPolicy` has a type bug:** it passes `apn.DefaultPolicyID` (FK to `policies.id`) as `policyVersionID` (FK to `policy_versions.id`). These are different ID spaces.

**Decision:** Replace the broken policy logic with the same `ListReferencingAPN` + `CurrentVersionID` pattern used by the single-SIM activate handler. Extract a shared `resolvePolicy` function to avoid duplication.

### D-3: Completion notification — seed new `job.completed` template

The `notification_templates` table has no `job.completed` event_type entry (only 14 event types seeded in `004_notification_templates.sql`). The `notifications` seed data has hardcoded `job_completed` rows, but the template-driven `renderContent` path will fall back to a generic subject. A new template row needs to be seeded.

### D-4: Double state_history — spurious, remove

Line 228 (`InsertHistory(nil, "ordered")`) is an explicit write with `from_state=NULL, to_state='ordered'`. Line 230 (`TransitionState("active")`) internally calls `insertStateHistory(ordered→active)`. The single-SIM path writes zero history on create; only the activate/transition writes history. The `NULL→ordered` row is bulk-only dead weight. Remove it.

Regression assertion: exactly 1 `sim_state_history` row per successfully imported SIM (the `ordered→active` transition written by `TransitionState`).

## Affected Files

| File | Change Type | Reason |
|------|-------------|--------|
| `internal/job/import.go` | Major | Inject deps, emit per-SIM audit, emit completion notification, fix policy resolution, remove spurious InsertHistory |
| `cmd/argus/main.go` | Minor | Wire new dependencies into `NewBulkImportProcessor` |
| `internal/job/import_test.go` | Major | Add tests for audit/notification/policy/state_history |
| `migrations/seed/004_notification_templates.sql` | Minor | Add `job.completed` template rows (tr + en) |

## Fix Steps

### Task 1: Inject audit, notification, and policy dependencies into BulkImportProcessor

**Files:** `internal/job/import.go`, `cmd/argus/main.go`

**What:**
- Add fields to `BulkImportProcessor` struct:
  - `auditor audit.Auditor` (interface, not concrete FullService)
  - `notifier *notification.Service`
  - `policies *store.PolicyStore`
- Update `NewBulkImportProcessor` constructor signature to accept these 3 new deps
- Update `cmd/argus/main.go` wiring to pass `auditFullService`, `notifService`, `policyStore` to the constructor
- All 3 are nilable — nil-guard in usage sites (same pattern as handler.go `createAuditEntry`)

**AC coverage:** Prerequisite for Tasks 2-5.

---

### Task 2: Remove spurious `NULL→ordered` state_history write (AC-12)

**Files:** `internal/job/import.go`

**What:**
- Delete line 228: `_ = p.sims.InsertHistory(tenantCtx, sim.ID, nil, "ordered", "bulk_import", nil, nil)`
- Delete lines 227 and 242: `ordered := "ordered"` and `_ = ordered` (dead code)
- After this, each SIM gets exactly 1 state_history row from `TransitionState` (the `ordered→active` transition)

**Regression assertion:** After a 10-SIM import, `SELECT COUNT(*) FROM sim_state_history WHERE sim_id IN (...)` = 10 (not 20).

**AC coverage:** AC-12.

---

### Task 3: Emit per-SIM `sim.create` and `sim.activate` audit events (AC-1, AC-2, AC-3, AC-4)

**Files:** `internal/job/import.go`

**What:**
- After successful `p.sims.Create(...)` (line 202), emit `sim.create` audit event:
  ```
  p.emitAudit(tenantCtx, job, "sim.create", sim.ID.String(), nil, sim)
  ```
- After successful `p.sims.TransitionState(...)` (line 230), emit `sim.activate` audit event:
  ```
  p.emitAudit(tenantCtx, job, "sim.activate", sim.ID.String(), sim, activatedSim)
  ```
- Add helper method `emitAudit(ctx, job, action, entityID, before, after)`:
  - Nil-guard on `p.auditor`
  - Build `audit.CreateEntryParams` with `TenantID=job.TenantID`, `UserID=job.CreatedBy`, `EntityType="sim"`, marshalled before/after data
  - No IP/UserAgent (background job, not HTTP request)
  - Use a common `CorrelationID` derived from `job.ID` for all audit entries in the same import (aids traceability)
- After the main loop ends (before `p.jobs.Complete`), emit `sim.bulk_import` summary audit event:
  - `action="sim.bulk_import"`, `entity_type="job"`, `entity_id=job.ID`, `after_data={total, success, failure, file_name}`

**Performance note:** Each `CreateEntry` call goes through NATS async → chain writer. 500 SIMs = 1001 audit entries (500 create + 500 activate + 1 summary). The NATS consumer processes them sequentially. This is acceptable for import (not real-time critical).

**AC coverage:** AC-1, AC-2, AC-3, AC-4 (chain verified by FIX-104's `VerifyChain`).

---

### Task 4: Fix policy auto-assignment to match single-SIM activate path (AC-9, AC-10, AC-11)

**Files:** `internal/job/import.go`

**What:**
- Replace the broken policy logic in `allocateIPAndPolicy` (or inline after activation):
  1. After successful `TransitionState("active")`, if `activatedSim.APNID != nil`:
  2. Look up APN: `apn` is already resolved from `apnCache` (available in the loop)
  3. Call `p.policies.ListReferencingAPN(ctx, job.TenantID, apn.Name, 10, "")` (same as handler.go:922)
  4. Find first policy with `State == "active" && CurrentVersionID != nil`
  5. If found: call `p.sims.SetIPAndPolicy(ctx, sim.ID, activatedSim.IPAddressID, policy.CurrentVersionID)`
  6. If not found: no-op (SIM keeps `policy_version_id = NULL` — AC-11)
- Remove or refactor `allocateIPAndPolicy` — IP allocation at import time is out of scope per STORY-092. The IP part should be stripped. The method is now only needed for `reserveSpecificIP` (CSV `ip_address` column). Policy resolution should be inlined after activation or extracted to a shared helper.
- Nil-guard on `p.policies` — if nil, skip policy resolution silently (same pattern as handler.go:920 `if h.policyStore != nil`)

**AC coverage:** AC-9, AC-10, AC-11.

---

### Task 5: Emit completion notification (AC-5, AC-6, AC-7, AC-8)

**Files:** `internal/job/import.go`, `migrations/seed/004_notification_templates.sql`

**What:**

**A. Notification dispatch (import.go):**
- After `p.jobs.Complete(...)` and before the `eventBus.Publish(SubjectJobCompleted)`, call:
  ```go
  if p.notifier != nil {
      p.notifier.Notify(ctx, notification.NotifyRequest{
          TenantID:   job.TenantID,
          UserID:     job.CreatedBy,
          EventType:  notification.EventJobCompleted,
          ScopeType:  notification.ScopeSystem,
          ScopeRefID: &job.ID,
          Title:      "Bulk import complete",
          Body:       fmt.Sprintf("%s: %d/%d successful, %d failed", payload.FileName, processed, totalRows, failed),
          Severity:   "info",
          ExtraFields: map[string]string{
              "job_id":        job.ID.String(),
              "total":         strconv.Itoa(totalRows),
              "success_count": strconv.Itoa(processed),
              "fail_count":    strconv.Itoa(failed),
              "file_name":     payload.FileName,
          },
      })
  }
  ```
- Also emit when `totalRows == 0` (empty CSV early return) — edge case for AC-5 "both success and partial-success paths"
- `Notify` handles preference lookup (AC-8), channel dispatch (in-app + webhook if configured), and notifStore persistence (AC-7 unread count increment)

**B. Template seed (004_notification_templates.sql):**
- Add 2 rows (tr + en) for `event_type = 'job.completed'`:
  - Subject: `Toplu İşlem Tamamlandı` / `Job Completed: {{.ExtraFields.file_name}}`
  - Body: Summary with counts from ExtraFields
- Use ON CONFLICT idempotent pattern matching existing rows

**AC coverage:** AC-5, AC-6, AC-7, AC-8.

---

### Task 6: Integration tests (AC-13)

**Files:** `internal/job/import_test.go`

**What:**
- Add mock implementations for `audit.Auditor`, `notification.Service`, `store.PolicyStore` interfaces
- Test: `TestBulkImport_AuditEvents` — 10-SIM CSV → assert 20 per-SIM audit calls (10 `sim.create` + 10 `sim.activate`) + 1 `sim.bulk_import` summary
- Test: `TestBulkImport_CompletionNotification` — assert `Notify` called once with correct payload including counts
- Test: `TestBulkImport_PolicyAutoAssign` — assert `SetIPAndPolicy` called with correct `CurrentVersionID` (not `DefaultPolicyID`)
- Test: `TestBulkImport_NoPolicyAvailable` — APN has no referencing active policy → SIM created with `policy_version_id = NULL`, no error
- Test: `TestBulkImport_StateHistoryCount` — assert `InsertHistory` NOT called separately (only `TransitionState` writes history)
- Test: `TestBulkImport_AllInvalidRows` — 0 SIMs created, still 1 notification with `success_count=0`

**AC coverage:** AC-13 (end-to-end green requires UAT rerun, but these unit/integration tests validate all ACs programmatically).

## Execution Order

```
Task 1 (inject deps) → Task 2 (state_history fix) → Task 3 (audit) → Task 4 (policy) → Task 5 (notification) → Task 6 (tests)
```

Tasks 2-5 can be done in any order after Task 1, but Task 6 depends on all of them.

## Implementation Notes

- **Verify `Job.CreatedBy` type:** Task 3 assumes `*uuid.UUID`. Confirm from `store.Job` struct before wiring.
- **Template variable naming:** Existing templates use `{{ .tenant_name }}` but `TemplatePayload` uses Go fields (`TenantName`, `ExtraFields`). The explicit `Title`/`Body` in `NotifyRequest` satisfies AC-6 as fallback regardless. Verify naming convention renders correctly before committing the seed.
- **Event type naming drift (pre-existing):** `EventJobCompleted = "job.completed"` (dot) but seed notification rows in `003_comprehensive_seed.sql` use `job_completed` (underscore). This mismatch affects preference matching (AC-8). Out of FIX-102 scope but flag for data cleanup.
- **`entity_type="job"` for summary audit:** Verify no existing audit query filters assume `entity_type="job"` has a specific shape.

## Regression Risk

**Low-Medium.** Changes are confined to the bulk import loop. The struct signature change in Task 1 touches `main.go` wiring but nothing else. Policy resolution (Task 4) reuses an existing proven pattern. The spurious history removal (Task 2) is a deletion of dead code.

Risk mitigations:
- All new deps are nil-guarded — existing imports continue to work even if wiring is incomplete
- Policy resolution uses the same `ListReferencingAPN` + `CurrentVersionID` pattern proven in the single-SIM activate handler
- Audit emission uses the NATS async path, so a slow chain writer cannot block the import loop
