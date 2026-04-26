# Implementation Plan: FIX-231 — Policy Version State Machine + Dual-Source Fix

## Goal
Make `policy_assignments` the canonical source of truth for "active policy per SIM", with `sims.policy_version_id` maintained as a trigger-synced denormalised pointer for the RADIUS hot path; enforce single-active-version + single-active-rollout invariants; reconcile existing dual-source drift; recover stuck rollouts via a reaper job; document the 1-SIM-1-policy doctrine; visualise version state transitions in the Policy editor.

## Architecture Context

### Components Involved
- **DB schema (TBL-14, TBL-15, TBL-16)** — `migrations/*.sql`. Adds triggers, two partial unique indexes, and a `policy_id` column to `policy_rollouts`.
- **Reconciliation migration** — `migrations/20260427000002_reconcile_policy_assignments.up.sql`. One-shot data fix.
- **Policy store** — `internal/store/policy.go`. Tightens `CompleteRollout`, drops redundant write in `AssignSIMsToVersion`, adds `ListStuckRollouts` query, updates `GetActiveRolloutForPolicy` to use the new column.
- **Rollout service** — `internal/policy/rollout/service.go`. `StartRollout` validation (concurrent rollouts → `ErrRolloutInProgress`); `CompleteRollout` keeps service-layer transition gates.
- **Job — stuck-rollout reaper (NEW)** — `internal/job/stuck_rollout_reaper.go`. Cron-fired processor following the per-tenant pattern from `internal/job/alerts_retention.go`.
- **Cron registration** — `cmd/argus/main.go` (cron scheduler block ~line 887). New entry "stuck_rollout_reaper" at `*/5 * * * *`.
- **Config** — `internal/config/config.go`. New env knob `ARGUS_STUCK_ROLLOUT_GRACE_MINUTES` (default 10).
- **UI: Version state chart** — `web/src/components/policy/versions-tab.tsx`. Inline timeline visualisation appended to the existing tab.
- **Doctrine doc** — `docs/PRODUCT.md`. New §"1 SIM = 1 Policy" subsection.

### Data Flow
1. **Assignment write (canonical)**: rollout service → `PolicyStore.AssignSIMsToVersion` → `INSERT INTO policy_assignments ... ON CONFLICT (sim_id) DO UPDATE`. Trigger `sims_policy_version_sync` fires → propagates the new `policy_version_id` to `sims.policy_version_id` for the matching `(sim_id)`.
2. **RADIUS hot path read**: AAA engine reads `sims.policy_version_id` directly (no JOIN on `policy_assignments`). Trigger guarantees consistency.
3. **CompleteRollout**: SQL transaction holds `policy_rollouts.id` `FOR UPDATE` → set rollout state=`completed` → set target version state=`active`, `activated_at=NOW()` → set ALL other versions of this policy whose state=`active` to `superseded` → update `policies.current_version_id`. Atomic; rollback on any failure.
4. **Reaper**: every 5 min, cron job fires → processor selects `policy_rollouts` where `state='in_progress' AND migrated_sims = total_sims AND COALESCE(updated_at, created_at) < NOW() - INTERVAL '10 minutes'` → calls `CompleteRollout` for each → emits `policy.rollout_progress` event with state='completed'.
5. **StartRollout**: pre-check uses the new partial unique index — `INSERT INTO policy_rollouts (...)` fails with unique-violation if another `(pending|in_progress)` rollout exists for the same `policy_id`. Service layer catches the error code and returns `ErrRolloutInProgress` (clean 422). Defence-in-depth: explicit `GetActiveRolloutForPolicy` precheck remains.

### API Specifications
No new HTTP endpoints in this story. The behavioural contract on existing endpoints tightens:

- `POST /api/v1/policies/{id}/versions/{vid}/rollout` (StartRollout)
  - **422** when another rollout for the same policy is `pending|in_progress` (now enforced by partial unique index).
  - Error envelope: `{ status: "error", error: { code: "ROLLOUT_IN_PROGRESS", message: "another rollout is already active for this policy" } }`
- `POST /api/v1/rollouts/{id}/advance` and the reaper share `CompleteRollout` semantics — atomic 4-row transition (rollout + target version + previous active versions + policies.current_version_id).

### Database Schema

**Source: `migrations/20260320000002_core_schema.up.sql` (TRUTH).** Existing schema, embedded for Developer reference:

```sql
-- TBL-14 policy_versions (line 162)
CREATE TABLE policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES policies(id),
    version INTEGER NOT NULL,
    dsl_content TEXT NOT NULL,
    compiled_rules JSONB NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',     -- enum-checked since 20260412000003
    affected_sim_count INTEGER,
    dry_run_result JSONB,
    activated_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);
-- existing: idx_policy_versions_policy_ver UNIQUE (policy_id, version)
-- existing: idx_policy_versions_policy_state (policy_id, state)
-- existing CHECK (20260412000003_enum_check_constraints.up.sql:111):
--   chk_policy_versions_state CHECK (state IN ('draft','active','rolling_out','superseded','archived'))

-- TBL-15 policy_assignments (line 345) — CANONICAL after FIX-231
CREATE TABLE policy_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id UUID,    -- FK added later in same migration → policy_rollouts(id)
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at TIMESTAMPTZ,
    coa_status VARCHAR(20) DEFAULT 'pending'
);
-- existing: idx_policy_assignments_sim UNIQUE (sim_id)        ← already 1-SIM-1-policy
-- existing: idx_policy_assignments_version (policy_version_id)
-- existing: idx_policy_assignments_rollout  (rollout_id)

-- TBL-16 policy_rollouts (line 363)
CREATE TABLE policy_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),  -- CURRENTLY ONLY linked via this
    previous_version_id UUID REFERENCES policy_versions(id),
    strategy VARCHAR(20) NOT NULL DEFAULT 'canary',
    stages JSONB NOT NULL,
    current_stage INTEGER NOT NULL DEFAULT 0,
    total_sims INTEGER NOT NULL,
    migrated_sims INTEGER NOT NULL DEFAULT 0,
    state VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);
-- existing: idx_policy_rollouts_version (policy_version_id)
-- existing: idx_policy_rollouts_state (state)

-- TBL-10 sims (line 275, partitioned by operator_id)
-- Relevant column: policy_version_id UUID                      ← read-optimised denorm pointer
-- existing: idx_sims_tenant_policy (tenant_id, policy_version_id)
```

#### New schema (FIX-231 schema migration — `20260427000001_policy_state_machine.up.sql`)

```sql
-- 1. Add policy_id to policy_rollouts so the partial unique index in AC-5 has a column to target.
--    (Story spec assumed it existed; migration shows it doesn't — see DEV-345.)
ALTER TABLE policy_rollouts ADD COLUMN policy_id UUID;
UPDATE policy_rollouts pr
   SET policy_id = pv.policy_id
  FROM policy_versions pv
 WHERE pv.id = pr.policy_version_id;
ALTER TABLE policy_rollouts ALTER COLUMN policy_id SET NOT NULL;
ALTER TABLE policy_rollouts ADD CONSTRAINT fk_policy_rollouts_policy
    FOREIGN KEY (policy_id) REFERENCES policies(id);
CREATE INDEX idx_policy_rollouts_policy ON policy_rollouts (policy_id);

-- 2. AC-2: trigger that propagates policy_assignments → sims.policy_version_id (single-writer).
CREATE OR REPLACE FUNCTION sims_policy_version_sync() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE sims SET policy_version_id = NULL WHERE id = OLD.sim_id;
        RETURN OLD;
    END IF;
    -- INSERT or UPDATE
    UPDATE sims SET policy_version_id = NEW.policy_version_id WHERE id = NEW.sim_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_sims_policy_version_sync
    AFTER INSERT OR UPDATE OF policy_version_id OR DELETE
    ON policy_assignments
    FOR EACH ROW
    EXECUTE FUNCTION sims_policy_version_sync();

-- 3. AC-5: at most one (pending|in_progress) rollout per policy.
CREATE UNIQUE INDEX policy_active_rollout
    ON policy_rollouts (policy_id)
    WHERE state IN ('pending','in_progress');

-- 4. AC-6: at most one active version per policy.
--    (No data fix here — assumed already correct after AC-7 reconciliation runs.
--     Migration order: AC-2/5/6 run BEFORE AC-7 in the deploy.)
CREATE UNIQUE INDEX policy_active_version
    ON policy_versions (policy_id)
    WHERE state = 'active';

-- 5. NOTE: AC-3 DB enum CHECK already exists (chk_policy_versions_state from 20260412000003).
--    No CHECK changes here — service layer enforces transition legality.

-- DOWN: drop trigger, function, both partial unique indexes, the policy_rollouts.policy_id column.
```

#### Reconciliation migration (`20260427000002_reconcile_policy_assignments.up.sql`)

Runs AFTER the schema migration. Two-phase, transactional, idempotent:

```sql
BEGIN;

-- Phase 1: BACKFILL — sims rows with policy_version_id but NO assignment row.
--   These are legacy direct writes that will silently vanish the moment any
--   future trigger fires; we materialise them as canonical assignments now.
INSERT INTO policy_assignments (sim_id, policy_version_id, assigned_at, coa_status)
SELECT s.id, s.policy_version_id, NOW(), 'acked'   -- 'acked' = no fresh CoA needed
  FROM sims s
 LEFT JOIN policy_assignments pa ON pa.sim_id = s.id
 WHERE s.policy_version_id IS NOT NULL
   AND pa.id IS NULL
ON CONFLICT (sim_id) DO NOTHING;

-- Phase 2: RECONCILE — sims rows whose policy_version_id mismatches the assignment.
--   Assignment row wins (canonical). The trigger will fire on the UPDATE below,
--   but it would set the same value, so it is a no-op on this path.
WITH mismatched AS (
    SELECT s.id AS sim_id, s.policy_version_id AS sim_pv, pa.policy_version_id AS asn_pv
      FROM sims s
      JOIN policy_assignments pa ON pa.sim_id = s.id
     WHERE s.policy_version_id IS DISTINCT FROM pa.policy_version_id
)
UPDATE sims s SET policy_version_id = m.asn_pv
  FROM mismatched m
 WHERE s.id = m.sim_id;

-- Phase 3: LOG — write a one-shot audit trail.
--   We can't write to a partitioned event table without tenant resolution;
--   simplest = NOTICE per row group via a DO block.
DO $$
DECLARE
    n_backfilled INTEGER;
    n_reconciled INTEGER;
BEGIN
    SELECT count(*) INTO n_backfilled FROM policy_assignments WHERE assigned_at >= NOW() - INTERVAL '10 seconds';
    SELECT count(*) INTO n_reconciled
      FROM sims s
      JOIN policy_assignments pa ON pa.sim_id = s.id
     WHERE s.policy_version_id IS NOT DISTINCT FROM pa.policy_version_id;
    RAISE NOTICE 'FIX-231 reconciliation: backfilled≈% assignments, total in-sync sims=%', n_backfilled, n_reconciled;
END $$;

COMMIT;
```

DOWN migration: no-op (data fix is intentionally one-way; the schema migration's DOWN drops the trigger so subsequent rollback to old code paths is safe).

### CompleteRollout (atomic transaction — pseudocode)

Existing implementation at `internal/store/policy.go:740-808` is already transactional. FIX-231 tightens the supersede clause to use `policy_id` (defence in depth, dependent on AC-6 unique index):

```go
// internal/store/policy.go::CompleteRollout (pseudocode of tightened supersede step)
tx.Exec(`UPDATE policy_rollouts SET state='completed', completed_at=NOW() WHERE id=$1`, rolloutID)

tx.Exec(`UPDATE policy_versions SET state='active', activated_at=NOW() WHERE id=$1`, r.PolicyVersionID)

// CHANGED: was "WHERE id=$previous_version_id AND state='active'"
//          now "WHERE policy_id=$X AND state='active' AND id != $target"
tx.Exec(`UPDATE policy_versions
            SET state='superseded'
          WHERE policy_id = (SELECT policy_id FROM policy_versions WHERE id = $1)
            AND state = 'active'
            AND id != $1`,
        r.PolicyVersionID)

tx.Exec(`UPDATE policies SET current_version_id=$1 WHERE id=$2`, r.PolicyVersionID, policyID)

tx.Commit()
```

### AssignSIMsToVersion redundant-write removal

`internal/store/policy.go:959-967` currently runs an explicit `UPDATE sims SET policy_version_id = $1 WHERE id IN (...)` after each batch of `policy_assignments` upserts. With AC-2's trigger live, this is a redundant second write to the partitioned `sims` table. **Task 2 must delete those lines** so the trigger is the sole writer (single-writer contract per AC-2). The function still returns `assigned` correctly — change the count source to `tag.RowsAffected()` of the upsert exec, not the now-removed UPDATE.

### Stuck-rollout reaper (AC-8)

Cron entry `*/5 * * * *`. Per-tenant pattern, copied from `internal/job/alerts_retention.go`:

```
processor.Process(ctx, job):
  1. Read grace minutes from cfg (default 10).
  2. Page through tenants (1000/page).
  3. For each tenant, query:
       SELECT id FROM policy_rollouts
        WHERE state = 'in_progress'
          AND migrated_sims >= total_sims
          AND total_sims > 0
          AND COALESCE(completed_at, created_at) < NOW() - make_interval(mins => $grace)
        FOR UPDATE SKIP LOCKED
        LIMIT 100
  4. For each row → CompleteRollout(rolloutID). Already transactional + idempotent.
  5. Emit per-tenant aggregate { reaped: N, skipped: M, errors: [...] } to job.result.
  6. Publish bus event "policy.rollout_progress" with state='completed' for each reaped row
     (reuses existing publishProgressWithState shape from rollout service.go:445).
```

Edge cases:
- `total_sims = 0` → skip (degenerate; not stuck, just empty rollout — separate concern).
- `CompleteRollout` returns `ErrRolloutNotFound` (race with manual finish) → swallow, mark skipped.
- Two reaper runs race → `FOR UPDATE SKIP LOCKED` + the existing `FOR UPDATE` in `CompleteRollout` make it safe; partial unique index also prevents two concurrent finalisations.

### Screen Mockup (AC-10)

Inline addition at the top of `web/src/components/policy/versions-tab.tsx` (existing 162-line component):

```
┌──────────────────────────────────────────────────────────────────┐
│ Version Lifecycle                                                │
│                                                                  │
│  v1            v2 (rolling_out)        v3 (active)               │
│  ●───activated──●─────superseded ··· ──●                         │
│  draft         active@04-15           active@04-22               │
│  04-01         (now superseded)       (current)                  │
│                                                                  │
│  Legend:  ● draft   ● rolling_out   ● active   ● superseded     │
└──────────────────────────────────────────────────────────────────┘
```

Implementation notes:
- Read source: existing `versions: PolicyVersion[]` prop already wired into the component.
- Render order: chronological by `created_at` ASC.
- State→colour mapping uses Design Token Map below (NOT `text-purple-400` etc.).
- Hover shows tooltip with `activated_at`, `rolled_back_at` if set.
- Empty state: when `versions.length === 0` show existing empty state (don't render the chart).
- Single active highlighted with `--color-success` ring; rolling_out pulses with `--color-warning`.

### Design Token Map

**Source: `web/src/index.css` (truth) + `docs/FRONTEND.md`.**

#### Color Tokens (state chart)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| `draft` chip | `text-text-secondary` + `bg-bg-elevated` | `text-gray-400`, `bg-gray-200` |
| `rolling_out` chip | `text-warning` + `bg-warning/10` (or `style={{color:'var(--color-warning)'}}`) | `text-yellow-500`, `text-amber-400` |
| `active` chip + ring | `text-success` + `border-success` | `text-green-500`, `text-emerald-400` |
| `superseded` chip | `text-text-tertiary` (or `text-text-secondary` if tertiary not defined) + line-through | `text-gray-500` |
| `rolled_back` chip | `text-danger` + `bg-danger/10` | `text-red-500` |
| Timeline connector | `border-border` (default) | `border-gray-300`, `border-[#e5e7eb]` |
| Card surface | `bg-bg-surface` | `bg-white`, `bg-[#0c0c14]` |
| Card border | `border-border` | `border-[#1e1e30]` |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Section title "Version Lifecycle" | `text-sm font-semibold uppercase tracking-wide text-text-secondary` | `text-[14px]`, hardcoded sizes |
| Version label `v1`, `v2` | `text-xs font-mono text-text-primary` | `text-gray-900` |
| State label | `text-xs uppercase tracking-wide` | hardcoded px |
| Timestamp under chip | `text-[10px] text-text-tertiary` | hardcoded color |

#### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `<Badge>` | `web/src/components/ui/badge.tsx` | All state chips — never raw `<span>` styled inline. Use `variant="success" \| "warning" \| "danger" \| "secondary" \| "default"`. |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | Hover-detail popovers on each version node. |
| `cn()` helper | `web/src/lib/utils.ts` | All conditional className composition. |

**RULE**: zero hardcoded hex / `text-{color}-{NNN}` from the default Tailwind palette in any new code. PAT-018.

## Prerequisites
- [x] FIX-206 completed (orphan SIM cleanup + FK constraints — referential integrity for assignment writes).
- [x] FIX-209 alerts table (pattern for per-tenant retention reaper, copied here for rollout reaper).
- [x] DB CHECK on `policy_versions.state` already in place (`migrations/20260412000003_enum_check_constraints.up.sql:111`).
- [x] Existing `CompleteRollout` atomic tx skeleton (`internal/store/policy.go:740`).

## Story-Specific Decisions

- **DEV-345** (FIX-231 — `policy_rollouts.policy_id` column): Story AC-5 names a partial unique index on `policy_rollouts(policy_id)` but the live schema has no such column — only `policy_version_id`. Adding the column + backfill from `policy_versions.policy_id` + FK is the only way to express "one active rollout per policy" (the alternative — uniquing on `policy_version_id` — only blocks two concurrent rollouts of the *same version*, not the same policy). Migration includes the backfill in the same up-script. ACCEPTED.
- **DEV-346** (FIX-231 — single-writer trigger; `AssignSIMsToVersion` redundant write removed): Once the trigger from AC-2 fires on `policy_assignments` upserts, the explicit `UPDATE sims SET policy_version_id = $1` block at `internal/store/policy.go:959-967` becomes a redundant second write to a partitioned table. Task 2 deletes that block; the trigger is the sole writer. `assigned` count switches to `tag.RowsAffected()` of the upsert exec. ACCEPTED.
- **DEV-347** (FIX-231 — AC-3 DB CHECK already present): `chk_policy_versions_state` (migration `20260412000003`) already enforces enum values. AC-3 collapses to service-layer transition validation only — no new CHECK. Plan does not duplicate. ACCEPTED.
- **DEV-348** (FIX-231 — AC-4 atomic tx already exists; tighten supersede clause): Existing `CompleteRollout` (`internal/store/policy.go:740-808`) is fully transactional with `FOR UPDATE` lock. FIX-231 only tightens the supersede `WHERE` from `id=$prev AND state='active'` to `policy_id=$X AND state='active' AND id != $target` — defence in depth that depends on AC-6's unique index. ACCEPTED.
- **DEV-349** (FIX-231 — reconciliation order: schema first, then data): The schema migration (triggers + partial unique on active version) MUST run BEFORE the reconciliation migration, otherwise reconciliation could violate the unique index mid-run. Migration timestamps order them: `20260427000001` schema, `20260427000002` reconcile. ACCEPTED.
- **DEV-350** (FIX-231 — reconciliation handles BOTH directions): Two cases of dual-source drift exist: (a) `policy_assignments` has row, `sims.policy_version_id` mismatches → assignment wins; (b) `sims.policy_version_id` non-null, no assignment row exists → backfill assignment from sims (legacy direct write). Reconciliation migration covers BOTH; without (b), the next assignment write triggers the sims-sync and the legacy value silently vanishes. ACCEPTED.
- **DEV-351** (FIX-231 — reaper grace period env-knob): `ARGUS_STUCK_ROLLOUT_GRACE_MINUTES` (default 10), config-time clamp `[5, 120]`. Cron schedule `*/5 * * * *` so reaper observes the grace period properly. PAT-017 wiring trace: env → config → constructor → struct field → SQL `make_interval` parameter. ACCEPTED.
- **DEV-352** (FIX-231 — single active version: post-reconcile invariant): The partial unique `policy_active_version` index (AC-6) is created in the schema migration. If reconciliation finds two `state='active'` rows for the same policy at run time, the schema migration's index creation will FAIL FAST. Mitigation: schema migration includes a guard query `RAISE EXCEPTION` if two active versions detected before the index creates. Then operators run a manual cleanup before retrying; we do NOT auto-pick a winner here (silent data loss risk). ACCEPTED.
- **DEV-353** (FIX-231 — multi-policy decision per AC-11): KEEP 1 SIM = 1 policy. Future multi-layer (base + override) is OUT OF SCOPE. Doctrine recorded in `docs/PRODUCT.md` §"Policy Model Doctrine — 1 SIM = 1 Policy". ACCEPTED.

## Tasks

### Task 1: Schema migration (triggers, constraints, policy_rollouts.policy_id column)
- **Files:** Create `migrations/20260427000001_policy_state_machine.up.sql`, `migrations/20260427000001_policy_state_machine.down.sql`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260423000001_alerts_dedup_statemachine.up.sql` — same shape (ADD COLUMN + backfill + partial unique indexes + trigger function + DOWN that drops everything created).
- **Context refs:** `Database Schema` (full embedded SQL above), `Story-Specific Decisions DEV-345/346/347/352`.
- **What:** Implement the 5-step migration exactly as embedded in the Database Schema section. Step 4 (partial unique on `state='active'`) MUST include a defensive `DO $$ BEGIN IF (SELECT count(*) FROM (SELECT policy_id FROM policy_versions WHERE state='active' GROUP BY policy_id HAVING count(*) > 1) x) > 0 THEN RAISE EXCEPTION 'multiple active versions per policy detected — run reconciliation first or fix manually'; END IF; END $$;` BEFORE creating the index. DOWN: drop trigger `trg_sims_policy_version_sync`, function `sims_policy_version_sync()`, indexes `policy_active_rollout` and `policy_active_version`, FK `fk_policy_rollouts_policy`, index `idx_policy_rollouts_policy`, column `policy_rollouts.policy_id`.
- **Verify:** `make db-migrate` runs cleanly. `\d policy_rollouts` shows `policy_id` NOT NULL with FK. `\di policy_active_*` shows both partial unique indexes. Trigger function `sims_policy_version_sync` listed via `\df`. `make db-seed` STILL passes (no defer per memory:feedback_no_defer_seed).

### Task 2: Reconciliation data migration
- **Files:** Create `migrations/20260427000002_reconcile_policy_assignments.up.sql`, `migrations/20260427000002_reconcile_policy_assignments.down.sql`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260420000001_sims_orphan_cleanup.up.sql` — same shape (data-fix migration with NOTICE logging; DOWN is no-op).
- **Context refs:** `Database Schema > Reconciliation migration`, `Story-Specific Decisions DEV-349/350`.
- **What:** Phase 1 backfills missing assignments from `sims.policy_version_id`. Phase 2 reconciles mismatches (assignment wins). Phase 3 logs counts via `RAISE NOTICE`. Wrap the whole thing in `BEGIN..COMMIT`. DOWN file: comment-only no-op (`-- intentional no-op; data fix is one-way`). Important: Phase 1 INSERTs use `coa_status='acked'` so the trigger does not enqueue spurious CoA reissues.
- **Verify:** Run on a snapshot with manual divergence (one mismatch + one missing assignment). After migrate: every `sims.policy_version_id IS NOT NULL` row has matching `policy_assignments` row. `SELECT count(*) FROM sims s LEFT JOIN policy_assignments pa ON pa.sim_id=s.id WHERE s.policy_version_id IS NOT NULL AND (pa.id IS NULL OR pa.policy_version_id != s.policy_version_id)` returns 0.

### Task 3: Tighten CompleteRollout supersede clause + remove redundant write in AssignSIMsToVersion
- **Files:** Modify `internal/store/policy.go`
- **Depends on:** Task 1
- **Complexity:** high
- **Pattern ref:** Existing `CompleteRollout` (lines 740-808) and `AssignSIMsToVersion` (lines 908-975) ARE the pattern. Modify in place; do not rewrite.
- **Context refs:** `CompleteRollout (atomic transaction — pseudocode)`, `AssignSIMsToVersion redundant-write removal`, `Story-Specific Decisions DEV-346/348`.
- **What:**
  1. In `CompleteRollout`: replace the supersede `UPDATE` block (lines 783-790) with the `policy_id`-scoped form — uses a sub-SELECT to derive policy_id, condition `state='active' AND id != $target_version_id`. Keep the `if r.PreviousVersionID != nil` guard removed (now unconditional — the `id != target` clause makes it safe).
  2. In `AssignSIMsToVersion`: delete lines 951-968 (the `UPDATE sims SET policy_version_id` block). Switch `assigned` accumulator to read `tag.RowsAffected()` from the upsert exec (line 936). Trigger now handles the sims update.
  3. Update `GetActiveRolloutForPolicy` (lines 707-726): change the SELECT to use the new `pr.policy_id = $1` predicate directly — drop the `JOIN policy_versions pv` since the column is now denormalised.
  4. Add new function `ListStuckRollouts(ctx, graceMinutes int) ([]uuid.UUID, error)` that returns rollout IDs matching the reaper criteria (used by Task 5).
- **Verify:** `go build ./...` clean. `go test ./internal/store/...` and `./internal/policy/...` pass. New unit test asserts: (a) `CompleteRollout` supersedes ALL prior active versions of the same policy (test fixture: one policy with v1 active + v2 active erroneously — both end up `superseded`); (b) `AssignSIMsToVersion` no longer issues the explicit UPDATE (assert via SQL spy / count of UPDATEs against `sims`).

### Task 4: StartRollout error handling for unique-violation; service-layer transition validation
- **Files:** Modify `internal/policy/rollout/service.go`, `internal/store/policy.go` (error mapping helper)
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/policy.go` existing error handling for unique-violation (search for `pgconn.PgError` / `SQLState() == "23505"`). If none exist, follow the pattern in `internal/store/sim.go` (any handler that maps a pg unique violation to a domain error).
- **Context refs:** `Architecture Context > API Specifications`, `Story-Specific Decisions DEV-345`.
- **What:** Wrap `CreateRollout` SQL exec error: if pg error code is `23505` and constraint name is `policy_active_rollout`, return `store.ErrRolloutInProgress`. Service layer (`StartRollout`) keeps its existing `GetActiveRolloutForPolicy` precheck (defence in depth) AND now relies on the unique index as the source of truth — so a concurrent `StartRollout` race that passes the precheck still fails cleanly at INSERT.
- **Verify:** New integration test `TestStartRollout_ConcurrentReturns422` — fire two goroutines in parallel; exactly one succeeds, the other returns `ErrRolloutInProgress`. HTTP layer maps to 422 (existing behaviour).

### Task 5: Stuck-rollout reaper (job processor)
- **Files:** Create `internal/job/stuck_rollout_reaper.go`, `internal/job/stuck_rollout_reaper_test.go`. Modify `internal/job/types.go` (add `JobTypeStuckRolloutReaper = "stuck_rollout_reaper"`).
- **Depends on:** Task 3 (uses new `ListStuckRollouts` helper)
- **Complexity:** high
- **Pattern ref:** Read `internal/job/alerts_retention.go` end-to-end — same per-tenant skeleton, processor interface, result aggregation JSON shape, logger setup, error wrapping.
- **Context refs:** `Stuck-rollout reaper (AC-8)` section, `Story-Specific Decisions DEV-351`.
- **What:** Implement processor:
  - Constructor `NewStuckRolloutReaperProcessor(jobs, policyStore, eventBus, graceMinutes int, logger)`. Clamp grace to [5, 120].
  - `Type()` returns `JobTypeStuckRolloutReaper`.
  - `Process(ctx, job)`:
    1. `ids, err := policyStore.ListStuckRollouts(ctx, p.graceMinutes)`.
    2. For each id: `err := policyStore.CompleteRollout(ctx, id)`. On `ErrRolloutNotFound` → mark skipped. On other errors → mark failed but continue.
    3. After loop, publish per-rollout `policy.rollout_progress` with state=`completed` (reuse the existing publish helper signature from `internal/policy/rollout/service.go:445` — refactor `publishProgressWithState` to be callable from the job package, or inline a minimal version here).
    4. Return aggregate `{ reaped: N, skipped: M, failed: K }` to job result.
  - Test file mirrors `alerts_retention_test.go` shape: fake `PolicyStore` interface, table-driven tests for stuck/not-stuck/race-collision/grace-not-elapsed.
- **Verify:** `go test ./internal/job/ -run TestStuckRolloutReaper` passes. Manual: insert a stuck rollout (`state='in_progress', migrated_sims=total_sims, created_at=NOW()-INTERVAL '15 min'`), trigger job, observe `state='completed'` after run.

### Task 6: Cron registration + config knob + main.go wiring (PAT-017)
- **Files:** Modify `internal/config/config.go`, `cmd/argus/main.go`
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Read `cmd/argus/main.go:914-919` for the alerts_retention cron registration; read `internal/config/config.go` for the existing `AlertsRetentionDays` env-binding pattern.
- **Context refs:** `Stuck-rollout reaper (AC-8)`, `Story-Specific Decisions DEV-351`. **PAT-017 trace MUST appear in code comments** at all four sites: env binding, struct field, processor constructor call, processor struct field.
- **What:**
  1. `config.go`: add `StuckRolloutGraceMinutes int` field, env `ARGUS_STUCK_ROLLOUT_GRACE_MINUTES`, default `10`, clamp `[5, 120]`. Comment: `// PAT-017 wiring: env → cfg.StuckRolloutGraceMinutes → NewStuckRolloutReaperProcessor → p.graceMinutes → ListStuckRollouts SQL`.
  2. `main.go`: instantiate `stuckRolloutProc := job.NewStuckRolloutReaperProcessor(jobStore, policyStore, eventBus, cfg.StuckRolloutGraceMinutes, log.Logger)`. Register with the runner. Add cron entry inside the `if cfg.CronEnabled` block: `cronScheduler.AddEntry(job.CronEntry{Name: "stuck_rollout_reaper", Schedule: "*/5 * * * *", JobType: job.JobTypeStuckRolloutReaper})`.
- **Verify:** `go build ./...` clean. `make up` starts cleanly; logs show `cron entry registered name=stuck_rollout_reaper schedule=*/5 * * * *`. `rg -n cfg.StuckRolloutGraceMinutes` shows ≥4 hits (env, struct, constructor, downstream — PAT-017 gate).

### Task 7: Doctrine doc — 1 SIM = 1 policy
- **Files:** Modify `docs/PRODUCT.md`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Open `docs/PRODUCT.md` and append a new H2 section. Existing section style is the pattern.
- **Context refs:** Story file §AC-11, `Story-Specific Decisions DEV-353`.
- **What:** Add §"Policy Model Doctrine — 1 SIM = 1 Policy". State: (a) every SIM has at most one active `policy_version_id` at any moment; (b) `policy_assignments` is the canonical source; `sims.policy_version_id` is a trigger-synced read denorm; (c) multi-layer policies (base + override) are explicitly OUT OF SCOPE for the current architecture; revisit when product driver lands. Reference FIX-231 + this plan.
- **Verify:** `rg -n '1 SIM = 1 Policy' docs/PRODUCT.md` returns the new section.

### Task 8: Version state chart UI
- **Files:** Modify `web/src/components/policy/versions-tab.tsx`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read the existing `versions-tab.tsx` (162 lines) — append a new section above the existing version list. Follow the visual language of `web/src/pages/policies/index.tsx::stateVariant` for state→variant mapping (lines 73-...).
- **Context refs:** `Screen Mockup (AC-10)`, `Design Token Map`. **Invoke `frontend-design` skill BEFORE coding.**
- **What:** Render a horizontal timeline above the existing list. Each node = a version chip (`<Badge variant={stateVariant(v.state)}>v{v.version}</Badge>`) + below-chip caption (date / state label). Connector lines between nodes = `<div className="h-px bg-border flex-1" />`. The single `state='active'` version (if any) gets a `ring-1 ring-success` glow. `state='rolling_out'` pulses via `animate-pulse`. Hover on chip → `<Tooltip>` shows `activated_at`, `rolled_back_at` (if set), `created_at`. Empty `versions` array → render nothing (existing empty state handles the parent). Use ONLY the classes from the Design Token Map. NEVER raw `<button>` or hardcoded hex.
- **Verify:** `rg -nE 'text-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}' web/src/components/policy/versions-tab.tsx` → 0 matches. `rg -n '#[0-9a-fA-F]{6}' web/src/components/policy/versions-tab.tsx` → 0 matches. `make web-build` clean. Visual smoke test on `/policies/{id}` editor → versions tab.

### Task 9: Integration tests — bulk trigger performance + concurrent rollout + reconciliation
- **Files:** Create `internal/store/policy_state_machine_test.go`, `internal/policy/rollout/service_state_test.go`
- **Depends on:** Tasks 1-5
- **Complexity:** high
- **Pattern ref:** Read `internal/store/policy_test.go` for existing PolicyStore test scaffolding; `internal/policy/rollout/service_test.go` for rollout service test fixtures (including session/CoA stubs).
- **Context refs:** Story §Test Plan, AC-2/4/5/6/7/8.
- **What:** Test scenarios:
  1. **Trigger correctness**: insert 1000 `policy_assignments` rows in a transaction → assert all 1000 matching `sims.policy_version_id` rows updated. Time bound: < 1 s on local Postgres. (AC-2)
  2. **Trigger DELETE**: delete an assignment → corresponding `sims.policy_version_id` becomes NULL.
  3. **CompleteRollout — atomic 4-row transition**: setup v1 active + v2 rolling_out + rollout in_progress; call CompleteRollout; assert all 4 rows transition correctly in one tx (verify by killing the tx mid-way with `pg_terminate_backend` and asserting NO partial state remains). (AC-4)
  4. **Concurrent StartRollout**: two goroutines call StartRollout for the same policy → exactly one succeeds. (AC-5)
  5. **Single active version**: try to manually set two versions of the same policy to `active` → second UPDATE fails on the partial unique index. (AC-6)
  6. **Reaper happy path**: stuck rollout (`state=in_progress, migrated=total, age>10min`) → after reaper run → state=`completed`, target version=`active`. (AC-8)
  7. **Reaper grace period respected**: stuck rollout with age 5min < grace 10min → reaper skips it.
- **Verify:** `go test ./internal/store/... ./internal/policy/...` passes. All 7 scenarios green.

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (canonical source decision) | Task 1, Task 7 (doctrine doc) | Task 9 #1 |
| AC-2 (trigger sims_policy_version_sync) | Task 1 | Task 9 #1, #2 |
| AC-3 (state CHECK already exists; service-layer transitions) | Task 4 (service guards) | Existing CHECK + Task 9 #5 |
| AC-4 (atomic CompleteRollout) | Task 3 | Task 9 #3 |
| AC-5 (one active rollout per policy) | Task 1 (index), Task 4 (error mapping) | Task 9 #4 |
| AC-6 (one active version per policy) | Task 1 | Task 9 #5 |
| AC-7 (reconciliation migration) | Task 2 | Manual snapshot test (per Task 2 Verify) |
| AC-8 (stuck rollout reaper) | Task 5, Task 6 | Task 9 #6, #7 |
| AC-9 (RADIUS hot path reads sims.policy_version_id) | Task 1 (trigger guarantees), Task 3 (single-writer) | Task 9 #1, #2 |
| AC-10 (version state chart UI) | Task 8 | Visual smoke test |
| AC-11 (1 SIM = 1 policy doctrine) | Task 7 | grep doctrine doc |

## Story-Specific Compliance Rules
- **API**: Standard envelope on errors. `ErrRolloutInProgress` → 422 `{status:"error", error:{code:"ROLLOUT_IN_PROGRESS", message:"..."}}`.
- **DB**: Two NEW migrations (schema + data fix). Both must have `.up.sql` AND `.down.sql`. The data-fix DOWN is no-op (commented). Reconciliation runs in a single transaction.
- **DB seed discipline (memory:feedback_no_defer_seed)**: `make db-seed` MUST stay green. Verify after Task 1 + Task 2 land.
- **UI (PAT-018)**: Zero hardcoded hex / numbered Tailwind palette utilities. State→colour mapping uses semantic vars only.
- **Business**: 1 SIM = 1 policy is now an architectural invariant (DB-enforced via `idx_policy_assignments_sim` unique + the new partial unique on `policy_versions.state='active'`).
- **ADR**: Single-writer contract on `sims.policy_version_id` aligns with the broader "denormalised pointer + trigger" pattern used elsewhere (no ADR violation; record DEV decisions as architectural notes).

## Bug Pattern Warnings
- **PAT-016 (cross-store PK confusion)**: Multiple ID types live near each other here — `policy_id`, `policy_version_id`, `policy_rollouts.id`, `policy_assignments.id`. Trigger DDL writes `NEW.policy_version_id` to `sims.policy_version_id` — never `NEW.id` (which is the assignment's PK). Developer MUST verify the trigger function references `NEW.policy_version_id` and `NEW.sim_id` exclusively. Service-layer code must distinguish `rolloutID` from `versionID` consistently.
- **PAT-017 (config wiring trace)**: `cfg.StuckRolloutGraceMinutes` MUST appear at: (1) config struct, (2) env binding, (3) constructor parameter, (4) processor struct field, (5) SQL query interpolation. Missing ANY of these = FIX-grade gate finding. Code comments at each site reference PAT-017.
- **PAT-018 (default Tailwind palette in token-disciplined codebase)**: Task 8 UI MUST grep clean against `text-{color}-{NNN}` and hex literals. Use `text-success`, `text-warning`, `text-danger`, `bg-bg-surface`, `border-border` semantic shortcuts only.
- **PAT-019 (typed-nil interface)**: Reaper processor takes `*store.PolicyStore` (concrete) — but if Task 5 introduces an interface for testability, ensure the constructor takes the interface AND the test fakes return non-nil interface values. No `var x *PolicyStoreImpl; pass to interface param` smell.

## Tech Debt (from ROUTEMAP)
No prior tech debt items target FIX-231 specifically (verified by absence in ROUTEMAP `## Tech Debt` table). Story creates the following NEW debt candidates only if scope is cut — none planned to defer.

## Mock Retirement
No mock files apply (backend-driven story; no `src/mocks/`).

## Risks & Mitigations
- **R1 — Trigger amplification on bulk rollout**: 10K-SIM batch `INSERT...ON CONFLICT` → 10K row-level trigger fires. Mitigation: trigger function is single-statement UPDATE per row, indexed by `sims.id` PK; benchmark in Task 9 #1 confirms < 1 s for 1000 rows. If 10K rollouts show >5s lock contention, follow-up FIX promotes trigger to statement-level using transition tables (`REFERENCING NEW TABLE AS new_assignments`).
- **R2 — Reconciliation picks wrong canonical**: Two-direction reconciliation (DEV-350) chooses `policy_assignments` row when both exist, materialises an `acked` assignment when only `sims` row exists. Both paths logged. Manual spot-check via DBA query post-deploy.
- **R3 — Reaper false positive (force-completes a rollout still doing work)**: Grace period 10 min + condition `migrated_sims == total_sims` + advisory locks via `FOR UPDATE SKIP LOCKED`. False positive requires (a) job updated `migrated_sims=total` AND (b) no further activity for 10 min AND (c) operator did not manually complete. Acceptable trade-off for recovering pre-FIX-231 zombie rows. Reaper publishes a bus event so operators are notified.
- **R4 — Concurrent rollout prevention breaks current workflow**: Index forces "second StartRollout for same policy fails fast". If two admins try simultaneously, second sees clear `ROLLOUT_IN_PROGRESS` 422 with the existing rollout's id. Accepted per AC-5 + Story §Risks R4.
- **R5 — Schema migration fails on prod due to multiple active versions**: Defensive `RAISE EXCEPTION` in Task 1 detects this and aborts BEFORE the unique index is built. Operator must run reconciliation or manual cleanup, then retry. Better than silent data loss from auto-picking a winner.
- **R6 — `AssignSIMsToVersion` `assigned` count semantic change**: Currently counted from `UPDATE sims` `RowsAffected`; after Task 3 it reads from the upsert exec. Test fixtures relying on the count must be re-checked (the upsert affects N rows = batch size; the previous UPDATE could affect 0..N depending on which rows already had the right `policy_version_id`). Verify in Task 3 unit tests; semantic shift = "assigned" now means "rows we wrote into policy_assignments", which matches the canonical-source intent.
