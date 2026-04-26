# Implementation Plan: FIX-232 — Rollout UI Active State + Abort Endpoint + WS Live Push

## Goal
Make the policy Rollout tab a true mission-control surface for an in-flight rollout: when one is active, render an active-rollout panel with progress / per-stage status / CoA counter / ETA / drill-downs and Advance/Rollback/**Abort** actions; live-update via the existing FIX-212 envelope (`policy.rollout_progress`); add a brand-new backend `AbortRollout` endpoint that stops a rollout without reverting assignments.

## Story Reality Check (vs FIX-232 spec)

The spec contains three drift items the gather phase verified — the plan corrects them:

| Spec claim | Reality (verified) | Plan disposition |
|---|---|---|
| FE calls wrong path `/rollouts/{id}/advance` (F-145, AC-4) | `web/src/hooks/use-policies.ts:163,177,190,203` already uses `/policy-rollouts/{id}/...`. No bug. | AC-4 demoted to **grep verification** task — no path fix needed. Add `useAbortRollout` to the same file. |
| FE WS subject `policy.rollout.progressed` (AC-5) | Backend publishes envelope `type="policy.rollout_progress"` (`service.go:546`, `WEBSOCKET_EVENTS.md §9`). Existing `rollout-tab.tsx:130` already subscribes to it. | Plan uses **`policy.rollout_progress`** everywhere. |
| Files: `web/src/hooks/use-rollout.ts`, `rollout-selection-cards.tsx` | Neither exists. Selection cards are inline in `rollout-tab.tsx`; rollout hooks live in `use-policies.ts`. | Plan extracts `rollout-active-panel.tsx` (per spec) and **keeps selection cards inline** in `rollout-tab.tsx` (gated by state). |

Author of the spec is informed via this plan — implementation follows the verified-code reality.

## Architecture Context

### Components Involved

| Layer | Component | Path | Responsibility |
|---|---|---|---|
| DB migration | `20260428000001_policy_rollout_aborted.up.sql` (NEW) | `migrations/` | Add `aborted_at TIMESTAMPTZ` column + down migration |
| Store | `store.PolicyRollout` struct + scan-order | `internal/store/policy.go:558-563,602` | Add `AbortedAt *time.Time` field; update Scan rows everywhere `&r.RolledBackAt` appears |
| Store | `ErrRolloutAborted` sentinel | `internal/store/policy.go:23-28` | New error for abort guards |
| Store | `AbortRollout(ctx, rolloutID)` (NEW) | `internal/store/policy.go` | UPDATE `state='aborted', aborted_at=NOW()` |
| Service | `Service.AbortRollout` (NEW) + Advance/Rollback aborted guards | `internal/policy/rollout/service.go` | State-machine transition; reuse `publishProgressWithState` to push `state='aborted'` envelope |
| Handler | `Handler.AbortRollout` (NEW) | `internal/api/policy/handler.go` | HTTP wrapper, audit `policy_rollout.abort` |
| Router | `POST /api/v1/policy-rollouts/{id}/abort` (NEW) | `internal/gateway/router.go:587` | Route registration |
| FE hook | `useAbortRollout` (NEW) | `web/src/hooks/use-policies.ts` | TanStack mutation calling new endpoint |
| FE panel | `rollout-active-panel.tsx` (NEW) | `web/src/components/policy/rollout-active-panel.tsx` | Active-rollout mission-control panel |
| FE tab | `rollout-tab.tsx` (MODIFY) | `web/src/components/policy/rollout-tab.tsx` | State-aware render: active → panel; idle/terminal → selection cards |
| FE side drawer | `web/src/components/ui/slide-panel.tsx` (REUSE) | — | "Open expanded view" with drill-down links |
| Docs | `docs/architecture/api/_index.md` | — | Document AbortRollout endpoint |

### Data Flow (Abort)

```
User clicks "Abort rollout"
  ↓ Confirm <Dialog> opens (FIX-216 pattern)
  ↓ Confirm
useAbortRollout.mutateAsync(rolloutID)
  ↓ POST /api/v1/policy-rollouts/{id}/abort
Handler.AbortRollout
  ↓ tenantID from context, parse rolloutID
  ↓ rolloutSvc.AbortRollout(ctx, tenantID, rolloutID)
Service.AbortRollout
  ↓ store.GetRolloutByIDWithTenant
  ↓ guard: state ∈ {completed, rolled_back, aborted} → return sentinel error
  ↓ store.AbortRollout(ctx, rolloutID) — UPDATE state='aborted', aborted_at=NOW()
  ↓ publishProgressWithState(..., state="aborted") — emits FIX-212 envelope
Handler returns rolloutResponse(ro), audit "policy_rollout.abort"
  ↓ FE invalidates ['policies','rollout', id] query
  ↓ WS envelope arrives → wsClient.on('policy.rollout_progress') → refetchRollout()
  ↓ rollout-tab re-renders: state='aborted' → goes back to selection cards (with terminal banner "Rollout aborted at X by Y")
```

### Data Flow (Active Panel WS Live Push)

```
ExecuteStage migrates batch → publishProgress → bus.Envelope(type="policy.rollout_progress")
  → NATS → WS hub → client → wsClient.on('policy.rollout_progress')
  → if envelope.meta.rollout_id === current rolloutId
  → refetchRollout() → React Query updates → panel re-renders progress bar / stages / CoA / ETA
```

### API Specifications

#### NEW — `POST /api/v1/policy-rollouts/{id}/abort`

- **Auth:** Bearer JWT, tenant-scoped, role=`policy_editor` (per existing chain `JWTAuth → RequireRole("policy_editor")` — same as sibling rollout routes `/advance` and `/rollback`)
- **Request body:** `{ "reason"?: string }` (optional)
- **Success 200:** Envelope `{ status: "success", data: rolloutResponse }` — same DTO shape as `GetRollout`
- **Errors:**
  - `400` `INVALID_FORMAT` — bad UUID
  - `404` `NOT_FOUND` — rollout not found
  - `422` `ROLLOUT_COMPLETED` — already completed
  - `422` `ROLLOUT_ROLLED_BACK` — already rolled back
  - `422` `ROLLOUT_ABORTED` — already aborted (idempotent: optionally return current state instead)
  - `500` `INTERNAL_ERROR` — unexpected
- **Audit:** `action="policy_rollout.abort"`, entity_id=rolloutID, after={state:"aborted", aborted_at, reason}
- **Side effect:** publishes `policy.rollout_progress` envelope with `state="aborted"`. **Does NOT revert assignments** — already-migrated SIMs stay on new policy. **Does NOT fire CoA.**

#### Existing endpoints (no change — verified)

- `POST /api/v1/policy-versions/{versionId}/rollout` (start)
- `POST /api/v1/policy-rollouts/{id}/advance`
- `POST /api/v1/policy-rollouts/{id}/rollback`
- `GET  /api/v1/policy-rollouts/{id}`

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql:361-381` (ACTUAL).

```sql
-- Existing TBL-16 (no CHECK constraint on state — adding 'aborted' string is free)
CREATE TABLE policy_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES policies(id),    -- added by 20260427000001 (FIX-231)
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    previous_version_id UUID REFERENCES policy_versions(id),
    strategy VARCHAR(20) NOT NULL DEFAULT 'canary',
    stages JSONB NOT NULL,
    current_stage INTEGER NOT NULL DEFAULT 0,
    total_sims INTEGER NOT NULL,
    migrated_sims INTEGER NOT NULL DEFAULT 0,
    state VARCHAR(20) NOT NULL DEFAULT 'pending',       -- enum (no CHECK): pending|in_progress|completed|rolled_back|aborted
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    -- aborted_at TIMESTAMPTZ                           ← FIX-232 ADDS
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);
```

NEW migration `20260428000001_policy_rollout_aborted.up.sql`:

```sql
ALTER TABLE policy_rollouts ADD COLUMN IF NOT EXISTS aborted_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_policy_rollouts_aborted_at
  ON policy_rollouts (aborted_at) WHERE aborted_at IS NOT NULL;
COMMENT ON COLUMN policy_rollouts.aborted_at IS
  'FIX-232: timestamp set when admin aborts an in-progress rollout via POST /policy-rollouts/{id}/abort. Aborted rollouts retain migrated assignments (no revert).';
```

Down migration: `DROP INDEX IF EXISTS idx_policy_rollouts_aborted_at; ALTER TABLE policy_rollouts DROP COLUMN IF EXISTS aborted_at;`

### Screen Mockups

**SCR — Rollout tab, ACTIVE state (state ∈ pending | in_progress)**

```
┌───────────────────────── Rollout ─────────────────────────────────┐
│  ┌─Active Rollout─────────────────────────────────────────────┐    │
│  │ [IN_PROGRESS]   ID: 7c4f…0040    Started 14:00 (2h 25m ago)│    │
│  │                                                            │    │
│  │ Strategy: Staged Canary       Open expanded view ↗         │    │
│  │ ┌─Stage 1 (1%)─┐ ┌─Stage 2 (10%)─┐ ┌─Stage 3 (100%)──┐    │    │
│  │ │ ✓ completed  │ │ ▶ in_progress │ │ ○ pending       │    │    │
│  │ │ 23,456 SIMs  │ │ 1,500,000     │ │                 │    │    │
│  │ └──────────────┘ └───────────────┘ └─────────────────┘    │    │
│  │                                                            │    │
│  │ Progress: ████████████████░░░░░░░  74.97%                  │    │
│  │           1,758,016 / 2,345,600 SIMs                        │    │
│  │                                                            │    │
│  │ CoA: 1,757,800 acked · 0 pending · 216 failed              │    │
│  │ ETA: ~12m for current stage                                │    │
│  │                                                            │    │
│  │ [▶ Advance]  [↩ Rollback]  [✕ Abort]   [→ View SIMs]       │    │
│  └────────────────────────────────────────────────────────────┘    │
│  Errors (if any):                                                  │
│  ! Stage 2 partial — 216 CoA failed   [Retry failed]               │
└────────────────────────────────────────────────────────────────────┘
```

**SCR — Expanded view (SlidePanel right drawer)**

```
┌─Rollout 7c4f…0040 — Expanded ────────────────────────×┐
│ State: in_progress    Strategy: staged canary          │
│ Progress: 74.97%   1,758,016 / 2,345,600 SIMs         │
│ ─────────────────────────────────────────────────     │
│ Stages (expanded with timestamps):                    │
│   ✓ Stage 1 — 1% — 23,456 SIMs — 13:00→13:08         │
│   ▶ Stage 2 — 10% — 1,500,000 / 234,560 SIMs (74%)   │
│   ○ Stage 3 — 100% — pending                         │
│ ─────────────────────────────────────────────────     │
│ Drill-downs:                                          │
│   →  View Migrated SIMs (cohort filter)               │
│   →  CDR Explorer (rollout SIMs)                      │
│   →  Sessions filtered to rollout cohort              │
│   →  Audit log entries for this rollout              │
│ ─────────────────────────────────────────────────     │
│ Errors:  None                                         │
│ Connection: WS connected · Last update 2s ago         │
└────────────────────────────────────────────────────────┘
```

**SCR — Rollout tab, IDLE / TERMINAL state (rollout = null | completed | rolled_back | aborted)**

```
┌────────────────────── Rollout ─────────────────────────┐
│ [Optional terminal banner if last rollout terminal]   │
│ ⓘ Last rollout (id 5e8b…) completed at 2026-04-22     │
│   1,000,000 SIMs migrated · v2 → v3                   │
│   [→ View summary]                                    │
│ ─────────────────────────────────────────────────     │
│ ┌─Direct Assign (100%)─┐ ┌─Staged (1→10→100)──┐      │
│ │ ◉  Apply immediately │ │ ○  Canary rollout   │      │
│ └──────────────────────┘ └─────────────────────┘      │
│         [▶ Start Rollout (canStartRollout)]           │
└────────────────────────────────────────────────────────┘
```

- Navigation: Policies → policy → editor → tab "Rollout"
- Drill-down targets:
  - "View Migrated SIMs" → `/sims?rollout_id={rolloutID}` (FIX-233 wires URL param parsing — link emits the URL today, filter activates after FIX-233 deploys)
  - "CDR Explorer" → `/cdr?rollout_id={rolloutID}` (FIX-214 page accepts `rollout_id` filter)
  - "Sessions" → `/sessions?rollout_id={rolloutID}` (FIX-233 wires; link emits today)
  - "Audit log" → `/audit?entity_id={rolloutID}&action_prefix=policy_rollout`

### Design Token Map

#### Color Tokens (from `docs/FRONTEND.md` + verified `web/src/index.css @theme`)

| Usage | Token Class | NEVER Use |
|---|---|---|
| Page / panel body bg | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card / panel bg | `bg-bg-surface` | `bg-[#0C0C14]`, `bg-white` |
| Elevated surface (dropdown, modal) | `bg-bg-elevated` | `bg-[#12121C]` |
| Hover row / skeleton | `bg-bg-hover` | `bg-gray-100` |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-gray-900` |
| Secondary text / labels | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-500` |
| Tertiary / placeholder | `text-text-tertiary` | `text-gray-400` |
| Primary accent (links, CTAs, active stage) | `text-accent`, `bg-accent`, `border-accent` | `text-[#00D4FF]`, `text-cyan-400` |
| Accent dim bg (active row) | `bg-accent-dim` | `bg-cyan-50` |
| Success (completed stage) | `text-success`, `bg-success-dim`, `border-success/20` | `text-green-500`, `text-[#00FF88]` |
| Warning (degraded, partial) | `text-warning`, `bg-warning-dim` | `text-yellow-500`, `text-[#FFB800]` |
| Danger (failed, abort, rollback) | `text-danger`, `bg-danger-dim`, `border-danger/30` | `text-red-500`, `text-[#FF4466]` |
| Border default | `border-border` | `border-[#1E1E30]`, `border-gray-200` |
| Subtle border | `border-border-subtle` | `border-gray-100` |
| CoA counter mono | `font-mono text-text-primary` | hardcoded fonts |
| Progress bar fill (in-progress) | `bg-gradient-to-r from-accent to-accent/70` | `bg-blue-500` |
| Progress bar fill (completed) | `bg-success` | `bg-green-500` |
| Progress bar fill (aborted) | `bg-warning` | `bg-yellow-500` |
| Progress bar fill (rolled_back) | `bg-danger` | `bg-red-500` |

**PAT-018 guard (CRITICAL):** Developer MUST NOT use any default Tailwind palette utility (`text-red-500`, `bg-blue-50`, `text-purple-400`, etc.). Run `grep -nE '\b(text|bg|border)-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}\b'` over every new/modified `.tsx` file — any match outside the project's semantic shortcuts is a Gate violation.

#### Typography Tokens

| Usage | Token Class | NEVER Use |
|---|---|---|
| Panel title | `text-sm font-semibold text-text-primary` | `text-[15px]` |
| Section label (uppercase) | `text-xs font-medium text-text-secondary uppercase tracking-wider` | hardcoded letter-spacing |
| Body | `text-xs text-text-secondary` | `text-[12px]` |
| Mono data (counts, IDs) | `font-mono text-xs text-text-primary` | inline `font-family` |
| Stage % label | `text-sm font-semibold` | `text-[13px]` |
| Caption / timestamp | `text-[10px] text-text-tertiary` | `text-xs` (already small enough only at 10px tier) |

#### Spacing & Elevation Tokens

| Usage | Token Class | NEVER Use |
|---|---|---|
| Panel padding | `p-4` | `p-[16px]`, `p-section` (project doesn't use) |
| Card radius (sm) | `rounded-[var(--radius-sm)]` | `rounded-md` |
| Card radius (md) | `rounded-[var(--radius-md)]` | `rounded-lg` |
| Stage gap | `gap-1` (between stages) | `gap-[4px]` |
| Action toolbar gap | `gap-2` | `gap-[8px]` |
| Progress bar height | `h-3 rounded-full` | `h-[12px]` |

#### Existing Components to REUSE (DO NOT recreate)

| Component | Path | Use For |
|---|---|---|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL action buttons (Advance, Rollback, Abort, Retry) |
| `<Badge>` | `web/src/components/ui/badge.tsx` | State badge (in_progress, completed, rolled_back, aborted) |
| `<Dialog>` (+ Header/Title/Description/Footer) | `web/src/components/ui/dialog.tsx` | All confirm flows (Advance / Rollback / Abort) — FIX-216 pattern |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | "Open expanded view" right drawer with drill-downs |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | In-flight mutation indicator |
| Lucide icons (`Play`, `RotateCcw`, `XCircle`, `CheckCircle2`, `AlertCircle`, `Loader2`, `ChevronRight`, `ExternalLink`) | `lucide-react` | All icons — NEVER inline SVG |
| `wsClient.on('policy.rollout_progress', …)` | `web/src/lib/ws.ts` | WS subscription |
| `wsClient.onStatus(…)` | `web/src/lib/ws.ts` | Polling-fallback trigger when status='disconnected' |
| `useRollout(id)`, `useAdvanceRollout()`, `useRollbackRollout()`, `useStartRollout(policyId)` | `web/src/hooks/use-policies.ts` | TanStack Query hooks |
| `<Link>` (react-router) | — | Drill-down links (NEVER raw `<a href>`) |

### WS Subject + Payload Shape

Subject (envelope `type`): **`policy.rollout_progress`** (NOT `policy.rollout.progressed` — story typo).

Source: `internal/policy/rollout/service.go:545` (`bus.NewEnvelope("policy.rollout_progress", …)`) and `docs/architecture/WEBSOCKET_EVENTS.md §9`.

Per FIX-212 envelope (canonical schema):

```jsonc
{
  "id": "evt_…",
  "type": "policy.rollout_progress",
  "timestamp": "2026-04-26T14:25:00.567Z",
  "tenant_id": "<uuid>",
  "severity": "info",
  "source": "policy",
  "title": "Policy rollout progress",
  "entity": { "kind": "policy", "id": "<policy_version_id>", "name": "policy <short>" },
  "meta": {
    "rollout_id": "<uuid>",
    "version_id": "<uuid>"
    // additional fields from publishProgressWithState — current_stage, state, progress_pct, etc.
  },
  "data": {
    "rollout_id": "<uuid>",
    "policy_id": "<uuid>",
    "state": "in_progress | completed | rolled_back | aborted",
    "current_stage": 1,
    "total_stages": 3,
    "stages": [{ "index": 0, "pct": 1, "status": "completed", "sim_count": 23456 }, …],
    "total_sims": 2345600,
    "migrated_sims": 1758016,
    "progress_pct": 74.97,
    "started_at": "2026-03-18T13:00:00Z"
    // optional: coa_sent_count, coa_acked_count, coa_failed_count
  }
}
```

FE matches `envelope.meta.rollout_id === currentRolloutId` (existing pattern in `rollout-tab.tsx:130-138`) → invokes `refetchRollout()`. WS-only push (no panel mutation from envelope payload — DB is source of truth via GET).

### Drill-down URL Contracts

| Link | URL | Backend support |
|---|---|---|
| View Migrated SIMs | `/sims?rollout_id={id}` | FIX-233 wires URL param parsing on SIM list |
| CDR Explorer (rollout SIMs) | `/cdr?rollout_id={id}` | FIX-214 already accepts `rollout_id` query param |
| Sessions filtered to rollout cohort | `/sessions?rollout_id={id}` | FIX-233 wires |
| Audit log for this rollout | `/audit?entity_id={id}&action_prefix=policy_rollout` | Existing audit list filter |

Plan emits the link today; filter activation lives in the dependent stories. Acceptable per FIX-232 §AC-11 ("links out to").

## Prerequisites

- [x] FIX-212 deployed — canonical `bus.Envelope` is the wire format. (Existing `service.go:545` already publishes correctly.)
- [x] FIX-230 deployed (this session) — DSL match correctness; rollout cohort calculation produces accurate `total_sims` / `migrated_sims`.
- [x] FIX-231 deployed (this session) — `policy_rollouts.policy_id` column populated; state-machine enforcement of single-active-version per policy.
- [x] FIX-216 modal pattern available (Dialog for compact confirm).

## Plan Content Rules

- No full function bodies, no full JSX. Plan is context pool. Developer reads each task's `Pattern ref` to inherit project conventions.
- All UI tasks invoke `frontend-design` skill.
- Backend tasks reference existing handler/service patterns by file:line.

## Tasks

### Task 1: DB migration — `aborted_at` column + struct + sentinel

- **Files:** Create `migrations/20260428000001_policy_rollout_aborted.up.sql`, Create `migrations/20260428000001_policy_rollout_aborted.down.sql`, Modify `internal/store/policy.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260427000001_policy_state_machine.up.sql` for migration header / comment / DO-block style; Read `internal/store/policy.go:558-602` for `PolicyRollout` struct + scan-order pattern.
- **Context refs:** "Database Schema", "API Specifications" (sentinel list), "Architecture Context > Components Involved" (rows 1-3)
- **What:**
  - Up: `ALTER TABLE policy_rollouts ADD COLUMN IF NOT EXISTS aborted_at TIMESTAMPTZ;` + partial index `idx_policy_rollouts_aborted_at` WHERE NOT NULL + COMMENT.
  - Down: drop the index then drop the column.
  - In `internal/store/policy.go`:
    - Struct `PolicyRollout`: add `AbortedAt *time.Time \`json:"aborted_at"\`` after `RolledBackAt` (line ~563).
    - Update **EVERY** scan call site that selects rollout columns to append `&r.AbortedAt` in the same position the SELECT places `aborted_at`. Currently `&r.RolledBackAt` appears at line 602, ~722 (other Get methods). Search: `grep -n '&r.RolledBackAt' internal/store/policy.go`. Update each SELECT clause to include `aborted_at`. PAT-016 cousin: scan-order drift causes silent column shift — verify each touched function compiles cleanly.
    - New sentinel near line 23-28: `ErrRolloutAborted = errors.New("store: rollout already aborted")`.
    - New method `(s *PolicyStoreImpl) AbortRollout(ctx context.Context, rolloutID uuid.UUID) error`: `UPDATE policy_rollouts SET state='aborted', aborted_at=NOW() WHERE id=$1 AND state IN ('pending','in_progress')` returning rowsAffected; if 0, return `ErrRolloutNotFound` (caller is expected to have already validated state via guard, so 0 rows means race).
- **Verify:** `make db-migrate` runs cleanly; `make db-migrate-down` then `make db-migrate` round-trips; `go build ./...` compiles; `grep -c '&r.AbortedAt' internal/store/policy.go` ≥ number of `&r.RolledBackAt` occurrences.

### Task 2: Service `AbortRollout` + Advance/Rollback aborted-state guards

- **Files:** Modify `internal/policy/rollout/service.go`, Modify `internal/store/policy.go` (helper `MapAbortError` if needed — kept minimal)
- **Depends on:** Task 1
- **Complexity:** **high**
- **Pattern ref:** Read `internal/policy/rollout/service.go:424-470` (`RollbackRollout`) — mirror this exact structure (load → guard → mutate → publish → reload).
- **Context refs:** "API Specifications > AbortRollout", "Architecture Context > Data Flow (Abort)", "WS Subject + Payload Shape"
- **What:**
  - New method:
    ```
    func (s *Service) AbortRollout(ctx context.Context, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error)
    ```
  - Steps:
    1. `policyStore.GetRolloutByIDWithTenant(ctx, rolloutID, tenantID)` — propagate `ErrRolloutNotFound`.
    2. Guards: `state == "completed"` → `ErrRolloutCompleted`; `"rolled_back"` → `ErrRolloutRolledBack`; `"aborted"` → `ErrRolloutAborted`. Allowed: `pending` / `in_progress`.
    3. `policyStore.AbortRollout(ctx, rolloutID)` — sets state + aborted_at.
    4. **Do NOT** call `RevertRolloutAssignments`. Do NOT iterate SIMs. Do NOT send CoA. (AC-6.)
    5. `var stages []store.RolloutStage; _ = json.Unmarshal(rollout.Stages, &stages)` — for envelope payload.
    6. `s.publishProgressWithState(ctx, rollout, stages, rollout.MigratedSIMs, rollout.CurrentStage, "aborted")` — pushes the canonical FIX-212 envelope so FE auto-refetches.
    7. Reload via `policyStore.GetRolloutByID(ctx, rolloutID)` and return.
  - **Cross-method updates** (PAT-016 ripple guard):
    - In `AdvanceRollout` (line 360): add early return — `if rollout.State == "aborted" { return nil, store.ErrRolloutAborted }`. Place before the existing `state == "completed"` check.
    - In `RollbackRollout` (line 424): same — add `if rollout.State == "aborted" { return nil, 0, store.ErrRolloutAborted }`. Document via inline change-note: "FIX-232: aborted rollouts are terminal — neither advance nor rollback applies."
- **Verify:** `go build ./internal/policy/rollout/...`; `grep -nE 'ErrRolloutAborted' internal/policy/rollout/service.go` returns ≥ 3 hits (AbortRollout sentinel + Advance guard + Rollback guard).

### Task 3: Handler `AbortRollout` + Route + DTO + Audit

- **Files:** Modify `internal/api/policy/handler.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/policy/handler.go:1234-1297` (`RollbackRollout`) — mirror exactly: parse UUID → nil-check service → decode optional `{reason}` body → call service → switch on sentinel errors → audit → write success.
- **Context refs:** "API Specifications > AbortRollout", "Architecture Context > Components Involved" (rows 6-7), "Architecture Context > Data Flow (Abort)"
- **What:**
  - In `handler.go`:
    - Add types: `type abortRequest struct { Reason *string \`json:"reason,omitempty"\` }` (reuse `rollbackRequest` shape if identical — do NOT alias; explicit is clearer).
    - Extend `rolloutResponse` (line 1035) to include `AbortedAt *string \`json:"aborted_at,omitempty"\``. Update `toRolloutResponse` (line 1071) to format `r.AbortedAt` via `time.RFC3339Nano` when non-nil.
    - **Optionally extend** `rolloutResponse` with CoA counters (`CoaSentCount`, `CoaAckedCount`, `CoaFailedCount` — `*int`) — see Risk note "CoA counter on first load". For FIX-232 minimum, gate this on `policyStore.CountCoAStatuses(ctx, rolloutID)` if such a method already exists; else skip and document limitation in plan §Risks. **Decision: skip in T3, document; CoA counter is WS-only initial-render limitation.** (Reduces scope creep.)
    - New handler `func (h *Handler) AbortRollout(w http.ResponseWriter, r *http.Request)` mirroring `RollbackRollout`:
      - Parse tenantID, rolloutID; return 400 on bad UUID.
      - Decode optional `abortRequest`.
      - Call `h.rolloutSvc.AbortRollout(...)` → switch on `ErrRolloutNotFound (404)`, `ErrRolloutCompleted / ErrRolloutRolledBack / ErrRolloutAborted (422 with code)`.
      - On success: `resp := toRolloutResponse(ro)`; `h.createAuditEntry(r, "policy_rollout.abort", ro.ID.String(), nil, map[string]any{ "state": ro.State, "aborted_at": resp.AbortedAt, "reason": req.Reason })`; `apierr.WriteSuccess(w, http.StatusOK, resp)`.
  - In `router.go` line 587 — insert new line: `r.Post("/api/v1/policy-rollouts/{id}/abort", deps.PolicyHandler.AbortRollout)`.
  - In `docs/architecture/api/_index.md` — add row for `POST /policy-rollouts/{id}/abort` mirroring rollback row format.
- **Verify:** `go build ./...`; `curl -sX POST http://localhost:8080/api/v1/policy-rollouts/<uuid>/abort -H "Authorization: Bearer …"` returns `200` with `data.state == "aborted"` and `data.aborted_at` set.

### Task 4: Backend tests — AbortRollout state-machine coverage (AC-10)

- **Files:** Create `internal/api/policy/abort_rollout_test.go`, Modify `internal/policy/rollout/service_test.go` (or create `service_abort_test.go` if file is unwieldy)
- **Depends on:** Task 2, Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/policy/rollout/service_test.go` (existing rollout tests) — follow same harness (`testStore`, `testEventBus`); Read `internal/api/policy/rollback_test.go` if present, else nearest handler-level test (e.g. `internal/api/policy/handler_test.go`) for httptest pattern.
- **Context refs:** "API Specifications > AbortRollout", "Architecture Context > Data Flow (Abort)", AC-10 (3 scenarios)
- **What:** Add tests covering:
  - **TestService_AbortRollout_FromInProgress**: state transitions to 'aborted', `aborted_at` set, NO CoA sent (verify mock counter == 0), NO assignments reverted, envelope published with state='aborted'.
  - **TestService_AbortRollout_FromPending**: same expected outcome.
  - **TestService_AbortRollout_FromCompleted**: returns `store.ErrRolloutCompleted`.
  - **TestService_AbortRollout_FromRolledBack**: returns `store.ErrRolloutRolledBack`.
  - **TestService_AbortRollout_FromAborted**: returns `store.ErrRolloutAborted`.
  - **TestService_Advance_FromAborted_Rejected**: previously-allowed Advance now returns `ErrRolloutAborted`.
  - **TestService_Rollback_FromAborted_Rejected**: same for Rollback.
  - **TestHandler_AbortRollout_404OnUnknownID**, **TestHandler_AbortRollout_422OnCompleted**, **TestHandler_AbortRollout_200OnInProgress** (httptest with mock service).
  - **TestHandler_AbortRollout_AuditEntryCreated** — assert audit entry contains action `policy_rollout.abort` and reason from request.
- **Verify:** `go test ./internal/policy/rollout/... ./internal/api/policy/...` PASSES; coverage check `go test -cover ./internal/policy/rollout/...` shows AbortRollout fully covered.

### Task 5: FE hook `useAbortRollout` + endpoint-path verification grep

- **Files:** Modify `web/src/hooks/use-policies.ts`, Modify `web/src/types/policy.ts` (if AbortedAt needs adding)
- **Depends on:** Task 3 (so endpoint exists for live test)
- **Complexity:** low
- **Pattern ref:** Read `web/src/hooks/use-policies.ts:200-210` (`useRollbackRollout`) — copy structure.
- **Context refs:** "Architecture Context > Components Involved" (FE hook row), "API Specifications > AbortRollout", "Story Reality Check" (AC-4 demotion)
- **What:**
  - Add `useAbortRollout` mutation following the rollback pattern: `POST /policy-rollouts/${rolloutId}/abort` with optional `{ reason }` body; on success invalidate `[...POLICIES_KEY, 'rollout']`.
  - In `web/src/types/policy.ts` (find the `PolicyRollout` interface — likely already mirrors `rolloutResponse`), add `aborted_at?: string | null` field. If `state` is a union literal type, extend it to include `'aborted'`.
  - **AC-4 verification (no code change expected):** run `grep -rnE "'/rollouts/" web/src/` and `grep -rnE 'rollouts/' web/src/hooks web/src/lib web/src/components/policy web/src/pages/policies` — must return ZERO hits for the wrong path. All hits must be `/policy-rollouts/`. Document the grep result in the test plan.
- **Verify:** `cd web && npm run typecheck` PASSES; the verification grep returns no `/rollouts/{` matches outside `policy-rollouts`.

### Task 6: Active rollout panel component (NEW)

- **Files:** Create `web/src/components/policy/rollout-active-panel.tsx`
- **Depends on:** Task 5
- **Complexity:** **high**
- **Pattern ref:** Read existing `web/src/components/policy/rollout-tab.tsx` (the inline `RolloutProgress` sub-component, lines 27-127) — extract & enrich. Read `web/src/components/ui/slide-panel.tsx` for drawer usage. Read `web/src/components/sims/connected-sims-slide-panel.tsx` (FIX-227) for compose pattern.
- **Context refs:** "Screen Mockups > SCR Active state + Expanded view", "Design Token Map" (full), "Existing Components to REUSE", "WS Subject + Payload Shape", "Drill-down URL Contracts", "API Specifications > AbortRollout"
- **What:**
  - Component signature: `<RolloutActivePanel rollout={PolicyRollout} onAdvance={fn} onRollback={fn} onAbort={fn} onRetryFailed?={fn} />`. (Caller — `rollout-tab.tsx` — owns the mutations and confirm state.)
  - Render specs (per AC-2 / AC-3 / AC-9 / AC-11):
    - **Header row:** `<Badge>` for state (variant by state: in_progress→default, completed→success, rolled_back→danger, aborted→warning, pending→secondary). Right side: rollout id (`{id.slice(0,8)}…{id.slice(-4)}`) + started_at relative time + "Open expanded view" button → opens `<SlidePanel>`.
    - **Strategy row:** "Direct" vs "Staged Canary" computed from `stages.length===1 && stages[0].pct===100`.
    - **Stages visualization:** map `stages[]` to per-stage cards. Status icon + pct + per-stage progress (`stage.migrated / stage.sim_count`). Active stage highlighted (`border-accent`, `bg-accent-dim`). Completed stage `border-success/20`, `bg-success-dim/30`. Pending stage `border-border-subtle`. Failed stage (if any) `border-danger/30 bg-danger-dim/30`.
    - **Progress bar:** `migrated_sims / total_sims` percent. Color by state per Design Token Map.
    - **CoA counter line:** if envelope provides `coa_acked_count / coa_failed_count` (read from latest WS payload via parent state OR omit on first paint — see Risk 5). Format: `1,757,800 acked · 216 failed`. Use `font-mono`.
    - **ETA:** if `state === 'in_progress'` AND current stage migrated_count / sim_count > 0: linear extrapolation `(remaining / rate) where rate = migrated / (now - stage.started_at)`. Display as `~12m for current stage`. If insufficient data: render "—".
    - **Action toolbar:**
      - `[Advance Stage]` — visible if Staged AND `current_stage.status==='completed'` AND `current_stage < stages.length-1`. Triggers `onAdvance`.
      - `[Rollback]` — visible if state ∈ {pending, in_progress}. `variant="outline"` + `border-danger/30 text-danger`. Triggers `onRollback`.
      - `[Abort]` — visible if state ∈ {pending, in_progress}. `variant="outline"` + `border-warning/30 text-warning`. Triggers `onAbort`.
      - `[View Migrated SIMs]` — `<Link to="/sims?rollout_id={id}">` styled as outline button.
    - **Error/retry block:** if any `stage.status === 'failed'`: render warning panel with stage error reason + `[Retry failed]` button if `onRetryFailed` provided (otherwise stub to `null` until FIX-233's CoA retry endpoint lands — for now button calls onRetryFailed which is undefined → button hidden).
    - **Connection status:** small footer "WS connected · Last update Ns ago" using `wsClient.getStatus()` subscription (parent supplies via prop or component reads directly via `wsClient.onStatus`). When status='disconnected': show amber text "WS disconnected · polling every 5s".
    - **a11y:** `role="region" aria-label="Active rollout panel"`; progress bar `role="progressbar" aria-valuenow={progressPct} aria-valuemin={0} aria-valuemax={100}`; action buttons have `aria-label` matching their visible text + state context (e.g., `aria-label="Abort rollout {short_id} — does not revert assignments"`).
    - **Loading/empty:** parent guarantees rollout is non-null when panel renders; component asserts `rollout` truthy via type-safe prop, no own loading state.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px. No default Tailwind palette utilities (PAT-018).
- **Components:** Reuse atoms/molecules from "Existing Components to REUSE" — NEVER raw `<button>`, `<a>`, `<svg>`.
- **Note:** Invoke `frontend-design` skill for visual quality.
- **Verify:**
  - `cd web && npm run typecheck` PASSES.
  - `grep -nE '#[0-9a-fA-F]{3,8}|\b(text|bg|border)-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}\b' web/src/components/policy/rollout-active-panel.tsx` returns ZERO matches.
  - Visual smoke: render panel in Storybook OR via running editor with seeded in_progress rollout — all states (pending/in_progress/completed/failed/rolled_back/aborted) render without layout break.

### Task 7: Wire active panel into `rollout-tab.tsx` + Expanded SlidePanel + state-aware re-render + polling fallback

- **Files:** Modify `web/src/components/policy/rollout-tab.tsx`, Create `web/src/components/policy/rollout-expanded-slide-panel.tsx` (small)
- **Depends on:** Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Existing `web/src/components/policy/rollout-tab.tsx` — preserve its WS subscription pattern (line 130) and confirm-dialog state machine. Read `web/src/components/sims/connected-sims-slide-panel.tsx` for SlidePanel composition.
- **Context refs:** "Screen Mockups > SCR Active state, IDLE state, Expanded view", "Design Token Map", "Drill-down URL Contracts", "API Specifications > AbortRollout"
- **What:**
  - **State-aware top-level render** (replaces existing `rollout ? <Progress/> : <SelectionCards/>` ternary):
    - If `rollout && rollout.state ∈ {pending, in_progress}` → render `<RolloutActivePanel ... />` + `<RolloutExpandedSlidePanel ... />` drawer.
    - If `rollout && rollout.state ∈ {completed, rolled_back, aborted}` → render terminal summary banner (compact: state + summary + "View summary" link to expanded panel) + selection cards (so admin can start a new rollout).
    - If `!rollout` → render selection cards as today.
  - **Confirm dialogs (FIX-216 pattern):** extend the existing `confirmAction` state machine to add `'abort'` variant. Three `<Dialog>` instances differentiated by content:
    - Advance: "Advance to next stage. Current stage is complete. Continue?" → `[Cancel] [Advance]`.
    - Rollback: "Rollback rollout? This will revert all migrated SIMs to the previous policy version and fire CoA. **Destructive.**" → `[Cancel] [Confirm Rollback]` (danger styling).
    - Abort: "Abort rollout? Already-migrated SIMs WILL stay on the new policy. CoA will NOT fire. Use this when the rollout was started by mistake but the new policy is correct." → `[Cancel] [Confirm Abort]` (warning styling).
  - **Mutations:** add `useAbortRollout`; wire `onAbort = () => setConfirmAction('abort')`; on confirm → `abortMutation.mutateAsync(rolloutId).then(refetchRollout)`.
  - **Expanded SlidePanel (`rollout-expanded-slide-panel.tsx`):** receives `rollout` + `onClose`. Renders the SCR Expanded mockup: header, expanded stage list with timestamps, drill-down link list (`/sims?rollout_id=...`, `/cdr?rollout_id=...`, `/sessions?rollout_id=...`, `/audit?entity_id=...&action_prefix=policy_rollout`), error block, connection status footer.
  - **Polling fallback (AC-8):** subscribe to `wsClient.onStatus`. If status === 'disconnected' AND `rolloutId` is set AND `rollout.state ∈ {pending, in_progress}` — start a 5-second `setInterval(refetchRollout, 5000)`. Clear interval when status flips back to 'connected' or rollout state moves to terminal. Cleanup interval on unmount. Surface state in the panel ("WS disconnected · polling every 5s").
- **Tokens:** Use ONLY Design Token Map classes. No default Tailwind palette utilities.
- **Components:** Reuse `<SlidePanel>`, `<Dialog>`, `<Button>`, `<Badge>` — NEVER inline equivalents.
- **Note:** Invoke `frontend-design` skill.
- **Verify:**
  - `cd web && npm run typecheck` PASSES.
  - `cd web && npm run build` PASSES.
  - PAT-018 grep: zero default Tailwind palette utilities and zero `#[0-9a-fA-F]` hex literals in changed files.
  - Manual: with seeded `v3 Direct Assign` (state=in_progress) rollout — Rollout tab renders active panel (no more selection cards); clicking Abort → Dialog → Confirm → state flips to 'aborted' → tab re-renders to selection cards with terminal banner.
  - WS smoke: tail backend logs for `policy.rollout_progress` envelope on Abort; FE refetches.
  - Polling smoke: in browser devtools block ws://localhost:8081 for 10s — within 10s the panel footer shows "polling every 5s" and the panel still updates from the GET endpoint.

### Task 8: FE/browser tests + e2e smoke + ARCHITECTURE doc update

- **Files:** Create `web/src/components/policy/__tests__/rollout-active-panel.test.tsx`, Modify `docs/architecture/api/_index.md` (if not done in Task 3 — confirm)
- **Depends on:** Task 6, Task 7
- **Complexity:** medium
- **Pattern ref:** Read existing test under `web/src/components/policy/__tests__/` if present; else the closest pattern in `web/src/pages/__tests__/` or `web/src/components/sims/__tests__/`.
- **Context refs:** AC-1, AC-2, AC-3, AC-5, AC-8, AC-11; "Screen Mockups"; "WS Subject + Payload Shape"
- **What:**
  - Unit tests for `<RolloutActivePanel>`:
    - **renders all 6 states** (pending, in_progress, completed, rolled_back, aborted, failed) — snapshot or DOM-assertion of state badge, progress fill class, action button visibility.
    - **action button gating**: Advance hidden when current stage status != completed; Rollback / Abort hidden in terminal states; "View Migrated SIMs" link href contains rollout id.
    - **a11y**: progress bar exposes `aria-valuenow` matching `progress_pct`; action buttons have non-empty `aria-label`.
    - **disconnect indicator**: when `wsClient.getStatus()` mock returns 'disconnected', footer shows polling text.
  - Integration test for `rollout-tab.tsx`:
    - When rollout.state = 'in_progress' → active panel rendered, no selection cards.
    - When rollout.state = 'aborted' → selection cards rendered + terminal banner.
    - Clicking Abort → Dialog appears → confirm → mutation called with correct rolloutId.
  - Skim/extend backend integration test (already in Task 4) ensuring AC-10 scenarios all PASS.
- **Verify:** `cd web && npm test` PASSES; backend `go test ./internal/policy/... ./internal/api/policy/...` PASSES; `docs/architecture/api/_index.md` row for `/policy-rollouts/{id}/abort` exists.

## Acceptance Criteria Mapping

| AC | Implemented In | Verified By |
|---|---|---|
| AC-1 (state-aware render: active vs idle) | Task 7 | Task 8 (integration test) |
| AC-2 (active panel: id, started_at, strategy, stages, progress, CoA counter, ETA) | Task 6 | Task 8 (unit) |
| AC-3 (actions: Advance/Rollback/Abort/View SIMs with confirm) | Task 6 (UI), Task 7 (wiring) | Task 8 |
| AC-4 (FE path is `/policy-rollouts/...`) | Task 5 (verify-grep, no code) | grep + Task 8 typecheck |
| AC-5 (WS `policy.rollout_progress` subscription) | Task 7 (existing subscription preserved + extended for status fallback) | Task 8 manual smoke |
| AC-6 (NEW abort endpoint, no revert, audit) | Tasks 1-3 | Task 4 |
| AC-7 (Rollback destructive — explicit dialog) | Task 7 | Task 8 |
| AC-8 (polling fallback when WS disconnected) | Task 7 | Task 8 manual smoke |
| AC-9 (failed-stage error surfacing + retry) | Task 6 (error block) | Task 8 (state coverage) |
| AC-10 (Abort regression-tests: in_progress→aborted, completed→422, rolled_back→422) | Task 4 | `go test` |
| AC-11 (expanded SlidePanel with drill-downs to SIMs/CDR/Sessions/Audit) | Task 7 (`rollout-expanded-slide-panel.tsx`) | Task 8 (URL assertion) |

## Story-Specific Compliance Rules

- **API**: Standard envelope `{status, data, meta?, error?}` for `/policy-rollouts/{id}/abort` — use `apierr.WriteSuccess` / `apierr.WriteError` (existing helpers in `internal/apierr`).
- **DB**: Migration up + down required; index on `aborted_at WHERE NOT NULL` is partial — this is the project pattern (see `idx_policy_rollouts_state`).
- **UI**: All colors via Tailwind v4 `@theme` classes (`text-accent`, `bg-bg-surface`, etc.) — NO default palette utilities (PAT-018), NO hex literals. All confirm flows via `<Dialog>` (FIX-216), expanded view via `<SlidePanel>` (FIX-227 pattern).
- **Audit**: Every abort writes an audit entry with `action="policy_rollout.abort"`, entity_id=rolloutID, before=null, after={state, aborted_at, reason}. Required by ADR (audit on every state-changing operation).
- **Tenant scoping**: `AbortRollout` service/handler load via `GetRolloutByIDWithTenant` — never `GetRolloutByID` from REST path. (Internal reload after mutation may use `GetRolloutByID` — already the rollback pattern.)
- **WS envelope**: Type literal MUST be `policy.rollout_progress` (not `policy.rollout.progressed`). Source: `service.go:545`, `WEBSOCKET_EVENTS.md §9`.
- **State enum**: `policy_rollouts.state` allowed values: `pending | in_progress | completed | rolled_back | aborted` (no DB CHECK; enforce in service guards).

## Bug Pattern Warnings

- **PAT-016** [`docs/brainstorming/bug-patterns.md:23`] — Cross-store PK confusion (FIX-209). RIPPLE here: when adding `AbortedAt` to `PolicyRollout`, **every Scan(... &r.RolledBackAt ...) call site MUST be updated** to also scan `&r.AbortedAt` in the corresponding column position. A missed call site causes silent column shift (e.g., `created_at` lands in `aborted_at` slot), test passes locally if the missing test path doesn't exercise that getter. Mitigation: in Task 1, `grep -n '&r.RolledBackAt' internal/store/policy.go` and update each.
- **PAT-017** [`docs/brainstorming/bug-patterns.md:28`] — Config not threaded through all consumer paths. Not directly applicable (no new config), but the analogue here is: when adding `useAbortRollout`, ensure callers in `rollout-tab.tsx` use the new hook AND that the response type extension propagates. `grep -nE 'PolicyRollout' web/src/types web/src/hooks` and verify the `aborted_at` field is reachable everywhere `state === 'rolled_back'` is checked.
- **PAT-018** [`docs/brainstorming/bug-patterns.md:27`] — Default Tailwind palette where token mandated. CRITICAL for Tasks 6 & 7. Run `grep -nE '\b(text|bg|border)-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}\b'` on every new/modified `.tsx` — any match outside the project's semantic shortcuts is a Gate violation.
- **PAT-019** [`docs/brainstorming/bug-patterns.md:26`] — Typed-nil interface (FIX-228). Not directly applicable (no new optional interface params introduced); but `s.eventBus` is checked for nil at line 522 (correct pattern — preserved). Do not change that guard.

## Tech Debt (from ROUTEMAP)

No tech debt items target FIX-232. (D-139 from FIX-230 targets FIX-243 — N/A.)

## Mock Retirement

No frontend mocks directory exists. N/A.

## Risks & Mitigations

- **Risk 1 — Multi-tab concurrent action:** Two admins click Advance + Abort simultaneously. Mitigation: backend store-layer guard `state IN ('pending','in_progress')` in the UPDATE (Task 1); whichever wins flips state, the loser's mutation gets `ErrRolloutAborted` / `ErrRolloutCompleted` and FE surfaces a toast.
- **Risk 2 — WS payload drift:** Relies on FIX-212 envelope. Already deployed. Mitigation: FE matches on `meta.rollout_id` only (existing `rollout-tab.tsx:130-138` pattern) — the rest of payload is sourced from a fresh GET, so envelope schema drift only delays refresh, never corrupts state.
- **Risk 3 — Rollback storm:** Mitigated by destructive confirm dialog showing affected SIM count + the project's existing CoA throttle (per `service.go::RollbackRollout`).
- **Risk 4 — Endpoint path 404s:** AC-4 was a stale finding; verified the FE already uses `/policy-rollouts/...`. Task 5's grep is the regression guard.
- **Risk 5 — CoA counter on first load:** REST `GET /policy-rollouts/{id}` does not return CoA counters today (`rolloutResponse` lacks them). Active panel will show CoA counter only after the first WS push for in-flight rollouts (envelope contains `coa_*_count`). Initial paint shows "—". **Acceptable per FIX-232 minimum** — full coverage tracked separately if business surfaces a need (out-of-scope for this story).
- **Risk 6 — Aborted-state ripple:** Adding `'aborted'` to the state literal type may unmask existing FE switches that didn't account for it (e.g., `state === 'completed' ? success : danger`). Mitigation: Task 5 typecheck after literal-union widen; fix any non-exhaustive switches that surface.
- **Risk 7 — `aborted_at` column scan-order drift (PAT-016 cousin):** Covered explicitly in Task 1 verify-step.
- **Risk 8 — Polling fallback memory leak:** `setInterval` not cleared on unmount → stale fetches. Mitigation: Task 7 cleanup contract (test in Task 8 asserts `clearInterval` called via fake timers).

## Decisions Recorded

- **DEV-357**: `policy_rollouts` state enum extended with `'aborted'` — store-level guard only; no DB CHECK constraint needed (none exists today). State machine terminal: aborted blocks Advance and Rollback (added in Task 2 ripple).
- **DEV-358**: `aborted_at` migration 20260428000001 — partial index, COMMENT, additive only; pairs with full down-migration symmetry.
- **DEV-359**: WS envelope type for abort — reuse existing `policy.rollout_progress` with `state="aborted"`. Do NOT introduce a new envelope type. FE matches on `meta.rollout_id` only (existing pattern preserved).
- **DEV-360**: Selection cards stay inline in `rollout-tab.tsx` (no separate `rollout-selection-cards.tsx`). Active panel extracted to its own file (per spec). Rationale: extracting cards would duplicate state without enabling reuse — only one consumer.
- **DEV-361**: AC-4 endpoint path bug demoted to grep-verification task — verified `use-policies.ts` already uses correct `/policy-rollouts/...` paths. F-145 is a stale finding.
- **DEV-362**: WS subject literal — `policy.rollout_progress` (story typo `policy.rollout.progressed` corrected). Source: `service.go:545`, `WEBSOCKET_EVENTS.md §9`.
- **DEV-363**: Active panel CoA counter is WS-only on first paint — initial GET returns no `coa_*_count`. Documented as Risk 5; acceptable scope.
- **DEV-364**: Abort is destructive-but-non-reverting — confirm dialog uses warning styling (not danger) to differentiate from Rollback (danger). UX intent: Abort = "stop ASAP, accept current state"; Rollback = "undo everything".

## Wave Plan (for Amil dispatch)

- **Wave 1:** Task 1 (DB + struct).
- **Wave 2:** Task 2 (Service) + Task 5 (FE hook). Parallel.
- **Wave 3:** Task 3 (Handler + route).
- **Wave 4:** Task 6 (Active panel) + Task 4 (Backend tests). Parallel.
- **Wave 5:** Task 7 (Wire-up + SlidePanel + polling fallback).
- **Wave 6:** Task 8 (FE tests + docs).

Total: 8 tasks, 6 waves.

## Pre-Validation Checklist

- [x] Lines ≥ 100 (this plan ≈ 380 lines)
- [x] Tasks ≥ 5 (8 tasks)
- [x] At least 1 high-complexity task (Tasks 2 & 6 = high)
- [x] Required sections present (Goal, Architecture Context, Tasks, Acceptance Criteria Mapping, Risks)
- [x] API specs embedded (NEW endpoint fully specified inline)
- [x] DB schema embedded with source line annotation (`migrations/20260320000002_core_schema.up.sql:361-381`)
- [x] Design Token Map populated (4 tables: colors, typography, spacing, components)
- [x] Pattern refs on every task
- [x] Context refs on every task
- [x] Bug patterns scanned (PAT-016/017/018/019)
- [x] State-machine ripple covered (Advance + Rollback aborted guards)
- [x] Story drift items called out and resolved (3 items: AC-4, hook filename, WS subject)
