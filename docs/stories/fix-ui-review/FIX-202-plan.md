# Implementation Plan: FIX-202 — SIM List & Dashboard DTO: Operator Name Resolution Everywhere

## Goal

Eliminate UUID-only displays across SIM list, SIM detail, Dashboard operator-health, Sessions, Violations, and eSIM by enriching DTOs with `operator_name/operator_code/apn_name/policy_name/policy_version_number` via single-query LEFT JOINs at the store layer (not per-row handler lookups), keeping the list endpoint p95 under 100ms and rendering null-safely for orphan rows.

## Architecture Context

### Components Involved

- **`internal/store/sim.go`** — data access; gets a new `ListEnriched` query path (+ `GetByIDEnriched`) that LEFT JOINs `operators`, `apns`, `policy_versions`, `policies`. Today `sim.List` returns `[]SIM`; plan introduces `[]SIMWithNames` (superset) so other call sites (bulk, AAA, fleet aggregators) do not have to change.
- **`internal/store/policy_violation.go`** — gets a new `ListEnriched` query and `GetByIDEnriched` that LEFT JOIN `sims`, `operators`, `apns`, `policies`, `policy_versions`. Existing `List/GetByID` kept for internal callers (state machine) to avoid breaking other consumers.
- **`internal/api/sim/handler.go`** — removes the per-row enrichment maps (lines 469–562) and the per-call `enrichSIMResponse` (lines 376–411). Handler now maps the store's enriched row straight into `simResponse` (already has `OperatorName/APNName/PolicyName` fields — plan adds `OperatorCode`, `PolicyVersionNumber`).
- **`internal/api/dashboard/handler.go`** — widens `operatorHealthDTO` with `code` (already free from `ListGrantsWithOperators` SELECT), `sla_target` (already free — `operators.sla_uptime_target`), plus 4 best-effort fields populated from existing sources (`active_sessions`, `last_health_check`, nullable `latency_ms`, `auth_rate`). AC-3 scope-cut documented in Risks.
- **`internal/api/session/handler.go`** — session path is **Redis/`session.Manager` backed, not PG**. AC-9 interpretation: switch per-row `enrichSessionDTO` (lines 186–227) to per-page batch enrichment — one `WHERE id = ANY($1)` lookup per tenant per page for each of {sims, operators, apns}, populating lookup maps before building the DTO slice. Single-query JOIN is not possible here.
- **`internal/api/violation/handler.go`** — no DTO layer today; plan introduces `violationDTO` struct and wires it to a new `violationStore.ListEnriched`. Get/List both use it; Remediate response continues to use the raw struct.
- **`internal/api/esim/handler.go`** — widens `profileResponse` with `operator_name` and `operator_code`; eSIM `esim_profiles` has a direct `operator_id` FK — store gets `ListEnriched` / `GetByIDEnriched` that LEFT JOIN operators.
- **`web/src/types/sim.ts`, `web/src/types/session.ts`, `web/src/types/esim.ts`, `web/src/types/dashboard.ts`** — add new optional string fields matching backend DTO.
- **`web/src/pages/sims/index.tsx`, `.../sims/detail.tsx`, `.../sessions/index.tsx`, `.../violations/index.tsx`, `.../esim/*`, `.../dashboard/index.tsx`** — render operator chip with resolved name + code; fall back to `"(Unknown)"` with `AlertCircle` icon when name is null (orphan).

### Data Flow

**Before (N+1 + cache juggle):**
```
GET /api/v1/sims?limit=50
 → simStore.List()                [1 PG query]
 → loop each row:
     operatorStore.GetByID()      [50 PG queries, cached via NameCache]
     apnStore.GetByID()           [50 PG queries, cached via NameCache]
     ippoolStore.GetAddressByID() [50 PG queries]
     ippoolStore.GetByID()        [≤50 pool lookups]
     policyStore.GetVersionByID() [per Get only]
 → serialize
```
Cache paths mitigate steady-state, but cold pages pay the full cost — observed p95 ≥ 500 ms per story.

**After (single JOIN):**
```
GET /api/v1/sims?limit=50
 → simStore.ListEnriched()
     SELECT s.*, o.name AS operator_name, o.code AS operator_code,
            a.name AS apn_name, a.display_name AS apn_display_name,
            pv.version AS policy_version_number,
            p.name AS policy_name
     FROM sims s
     LEFT JOIN operators o ON o.id = s.operator_id
     LEFT JOIN apns a ON a.id = s.apn_id AND a.tenant_id = s.tenant_id
     LEFT JOIN policy_versions pv ON pv.id = s.policy_version_id
     LEFT JOIN policies p ON p.id = pv.policy_id
     WHERE s.tenant_id = $1 [+ filters]
     ORDER BY s.created_at DESC, s.id DESC
     LIMIT $N
 → serialize — no further DB round-trips for operator/apn/policy names
```
IP enrichment (address + pool name) stays on its existing per-page batch path (handler-level) — IP address JOIN is cheaper left out of the primary query because `ip_addresses` is range-partitioned and mixing two partitioned relations in one JOIN risks the planner regressing.

### API Specifications

All endpoints use the existing standard envelope `{ status, data, meta?, error? }`. Only DTO shapes change — no URL/method changes.

#### `GET /api/v1/sims` (list) — widened `simResponse`
**New / changed JSON fields** (all optional / nullable):
- `operator_name: string` — `operators.name`, null if operator row missing.
- `operator_code: string` — `operators.code`, e.g. `"turkcell"`, `"vodafone_tr"`.
- `apn_name: string` — falls back to `apns.display_name` when set, else `apns.name`.
- `policy_name: string` — `policies.name` formatted as `"<name> (v<version>)"` (keeps FIX-201 precedent); null when `policy_version_id IS NULL`.
- `policy_version_number: integer` — raw `policy_versions.version`.
- `policy_version_id: string` — already present; now explicitly nullable in typing.

#### `GET /api/v1/sims/:id` — same widened shape as list.

#### `GET /api/v1/dashboard` — widened `operatorHealthDTO` entries
**New fields** (per AC-3):
- `code: string` — operator code (free — already selected by `ListGrantsWithOperators`).
- `sla_target: number | null` — `operators.sla_uptime_target` (free — add to SELECT).
- `active_sessions: integer | null` — pulled from `sessionStore.GetActiveStats().ByOperator[operatorID]`; null if session store unset.
- `last_health_check: string (RFC3339) | null` — best-effort: read `operators.updated_at` (updated by health loop) OR leave null until health-history table exists (Risk R-3).
- `latency_ms: number | null` — null for MVP; populated later when metrics source wires in (Risk R-3).
- `auth_rate: number | null` — null for MVP; same reason.

#### `GET /api/v1/sessions` (list) + `GET /api/v1/sessions/:id`
**Existing fields `operator_name, apn_name, iccid, msisdn, imsi` stay** — but are batch-populated per page (not per-row). **New fields:**
- `operator_code: string`
- `policy_name: string` (via `sims.policy_version_id → policies`, looked up through the batch SIM fetch)
- `policy_version_number: integer`

#### `GET /api/v1/violations` (list) + `GET /api/v1/violations/:id` — new `violationDTO`
**Fields:**
- `id, tenant_id, sim_id, policy_id, version_id, rule_index, violation_type, action_taken, details, session_id, operator_id, apn_id, severity, created_at, acknowledged_at, acknowledged_by, acknowledgment_note` — all existing.
- **NEW:** `iccid, imsi, msisdn` (from sims) · `operator_name, operator_code` (from operators) · `apn_name` (from apns) · `policy_name, policy_version_number` (from policies + policy_versions).

#### `GET /api/v1/esim/profiles` (list) + `GET /api/v1/esim/profiles/:id` — widened `profileResponse`
**New fields:**
- `operator_name: string`
- `operator_code: string`

### Database Schema

Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL). No schema changes — all columns already exist.

```sql
-- TBL-10: sims — LIST-PARTITIONED by operator_id; composite PK (id, operator_id)
CREATE TABLE sims (
  id                        UUID NOT NULL DEFAULT gen_random_uuid(),
  tenant_id                 UUID NOT NULL,
  operator_id               UUID NOT NULL,
  apn_id                    UUID,                   -- nullable, LEFT JOIN target
  iccid                     VARCHAR(22) NOT NULL,
  imsi                      VARCHAR(15) NOT NULL,
  msisdn                    VARCHAR(20),
  ip_address_id             UUID,
  policy_version_id         UUID,                   -- nullable, LEFT JOIN target
  esim_profile_id           UUID,
  sim_type                  VARCHAR(10) NOT NULL,
  state                     VARCHAR(20) NOT NULL,
  rat_type                  VARCHAR(10),
  max_concurrent_sessions   INTEGER NOT NULL DEFAULT 1,
  session_idle_timeout_sec  INTEGER NOT NULL DEFAULT 3600,
  session_hard_timeout_sec  INTEGER NOT NULL DEFAULT 86400,
  metadata                  JSONB NOT NULL DEFAULT '{}',
  activated_at              TIMESTAMPTZ,
  suspended_at              TIMESTAMPTZ,
  terminated_at             TIMESTAMPTZ,
  purge_at                  TIMESTAMPTZ,
  created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);
-- Existing indexes covering the JOIN predicates (verified in migration file):
--   idx_sims_tenant_operator ON sims (tenant_id, operator_id)
--   idx_sims_tenant_apn      ON sims (tenant_id, apn_id)
--   idx_sims_tenant_policy   ON sims (tenant_id, policy_version_id)
-- PAT-004 note: sims is LIST-partitioned — cannot add new FKs INTO sims(id), but
--   JOINs FROM sims to parent tables are fine and partition-pruning-aware.

-- operators — unpartitioned, PK id
CREATE TABLE operators (
  id                  UUID PRIMARY KEY,
  name                VARCHAR(100) NOT NULL UNIQUE,  -- display name
  code                VARCHAR(20)  NOT NULL UNIQUE,  -- machine code: turkcell|vodafone_tr|turk_telekom
  mcc                 VARCHAR(3)  NOT NULL,
  mnc                 VARCHAR(3)  NOT NULL,
  health_status       VARCHAR(20) NOT NULL DEFAULT 'unknown',
  sla_uptime_target   DECIMAL(5,2) DEFAULT 99.90,
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ...
);

-- apns — tenant-scoped, PK id
CREATE TABLE apns (
  id             UUID PRIMARY KEY,
  tenant_id      UUID NOT NULL REFERENCES tenants(id),
  operator_id    UUID NOT NULL REFERENCES operators(id),
  name           VARCHAR(100) NOT NULL,       -- machine name
  display_name   VARCHAR(255),                -- optional pretty name (preferred if non-empty)
  ...
);

-- policy_versions — PK id, FK policy_id → policies(id)
CREATE TABLE policy_versions (
  id          UUID PRIMARY KEY,
  policy_id   UUID NOT NULL REFERENCES policies(id),
  version     INTEGER NOT NULL,
  ...
);

-- policies — tenant-scoped
CREATE TABLE policies (
  id         UUID PRIMARY KEY,
  tenant_id  UUID NOT NULL REFERENCES tenants(id),
  name       VARCHAR(100) NOT NULL,
  ...
);

-- policy_violations — unpartitioned, has sim_id/policy_id/version_id/operator_id/apn_id
CREATE TABLE policy_violations (
  id, tenant_id, sim_id, policy_id, version_id, rule_index,
  violation_type, action_taken, details, session_id,
  operator_id, apn_id, severity, created_at,
  acknowledged_at, acknowledged_by, acknowledgment_note
);

-- esim_profiles — has direct operator_id FK
CREATE TABLE esim_profiles (
  id, sim_id, eid, sm_dp_plus_id,
  operator_id UUID NOT NULL REFERENCES operators(id),
  profile_state, iccid_on_profile, last_provisioned_at, last_error,
  created_at, updated_at
);
```

### Screen Mockups

Tightly bounded to what changes on these 6 screens.

**SIMs list — operator chip + APN chip row cell**
```
┌────────────────────────────────────────────────────────────────────────────┐
│  ICCID            IMSI       MSISDN    Operator       APN      State  RAT  │
├────────────────────────────────────────────────────────────────────────────┤
│  893000000001...  26201999…  +90555…   ● Turkcell    corp.iot  Active LTE  │
│                                        (turkcell)                          │
│  893000000002...  22201999…  +49177…   ● Vodafone    iot.vf    Active NR5  │
│                                        (vodafone_tr)                       │
│  893000000003...  28601999…  +90533…   ⚠ (Unknown)   corp.iot  Suspended — │
└────────────────────────────────────────────────────────────────────────────┘
```
- Operator chip: colored dot + display name; code rendered as 11px mono below in `text-text-tertiary`.
- Orphan rows (operator_name null): `AlertCircle` icon in `text-warning` + `"(Unknown)"` in `text-text-secondary`. Title attribute exposes raw UUID for ops triage.
- Clickable: operator chip → `/operators/:operator_id`; APN chip → `/apns/:apn_id`; policy chip → `/policies/:policy_id`. Orphan chip non-clickable.

**Dashboard — Operator Health card**
```
┌───────────────── Operator Health ──────────────────┐
│  ● Turkcell        (turkcell)            99.95%    │
│    Healthy · 12,483 active · SLA 99.90%            │
│  ● Vodafone TR     (vodafone_tr)         94.12%    │
│    Degraded · 3,201 active · SLA 99.90%            │
│  ● Türk Telekom    (turk_telekom)        100.00%   │
│    Healthy · 9,512 active · SLA 99.50%             │
└────────────────────────────────────────────────────┘
```
- Status dot tinted by operator code (Turkcell=yellow `#FFCB00`-adjacent, Vodafone=red, Türk Telekom=blue) — see Operator Chip Color Map below.
- `active_sessions` shown when non-null; hidden (no "null") when unavailable.
- `latency_ms`, `auth_rate`, `last_health_check` rendered only when non-null — FE gated on truthy.

**Sessions list — operator column**: same chip pattern as SIMs.

**Violations list — new columns**: `ICCID | Operator | APN | Policy (v) | Severity | Type | Detected`.

**eSIM profile list — operator column**: chip with name + code; orphan fallback identical.

### Design Token Map (UI — MANDATORY)

Project uses **Tailwind v4 `@theme`** (see `web/src/index.css`) — tokens registered as `--color-*` generate Tailwind classes directly (e.g. `text-text-primary`, `bg-bg-surface`, `border-border`, `text-accent`). CSS variables (e.g. `var(--color-bg-surface)`) work for inline styles too. **Prefer the Tailwind class form** for consistency with existing code (`web/src/pages/sims/index.tsx` uses `text-text-primary`, `bg-bg-elevated`, `border-border`).

#### Color Tokens (from `web/src/index.css` + `docs/FRONTEND.md`)

| Usage | Tailwind class | NEVER use |
|-------|----------------|-----------|
| Primary text / chip label | `text-text-primary` | `text-[#E4E4ED]`, `text-white`, `text-gray-100` |
| Secondary text / meta lines | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-400` |
| Tertiary / code suffix "(turkcell)" | `text-text-tertiary` | `text-[#4A4A65]`, `text-gray-500` |
| Page background | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card background | `bg-bg-surface` | `bg-white`, `bg-[#0C0C14]` |
| Elevated panel / chip background | `bg-bg-elevated` | `bg-gray-800`, `bg-[#12121C]` |
| Card border | `border-border` | `border-[#1E1E30]`, `border-gray-700` |
| Subtle divider (table row sep) | `border-border-subtle` | `border-[#16162A]` |
| Primary accent (link, active state) | `text-accent` / `bg-accent` | `text-cyan-400`, `text-[#00D4FF]` |
| Success dot / healthy | `text-success` / `bg-success-dim` | `text-green-400`, `bg-[rgba(0,255,136,0.12)]` |
| Warning dot / degraded / "(Unknown)" icon | `text-warning` / `bg-warning-dim` | `text-yellow-400`, `bg-[rgba(255,184,0,0.12)]` |
| Danger dot / down | `text-danger` / `bg-danger-dim` | `text-red-400`, `bg-[rgba(255,68,102,0.12)]` |

#### Operator Chip Color Map (NEW — embed in `web/src/lib/operator-chip.ts`)

| `operator_code` | Dot color class | Background dim | Rationale |
|-----------------|-----------------|----------------|-----------|
| `turkcell` | `bg-warning text-warning` | `bg-warning-dim` | Turkcell brand yellow — maps to `--color-warning` (`#FFB800`) |
| `vodafone_tr` | `bg-danger text-danger` | `bg-danger-dim` | Vodafone brand red — maps to `--color-danger` (`#FF4466`) |
| `turk_telekom` | `bg-info text-info` | `bg-info-dim` | TT brand blue — maps to `--color-info` (`#6C8CFF`) |
| other / null | `bg-text-tertiary text-text-tertiary` | `bg-bg-elevated` | Generic muted |

Chip component MUST read `operator_code` (stable machine key), not `operator_name` (locale/display string), for color routing.

#### Typography Tokens

| Usage | Tailwind class |
|-------|----------------|
| Operator chip name | `text-[13px] font-medium text-text-primary` |
| Operator code suffix | `text-[11px] font-mono text-text-tertiary` |
| Table header | `text-[11px] uppercase tracking-[0.5px] text-text-secondary` |
| Table cell default | `text-[13px]` |
| Table mono data (ICCID, IMSI, code) | `text-[12px] font-mono` |
| "(Unknown)" fallback | `text-[13px] italic text-text-secondary` |

#### Spacing & Elevation Tokens

| Usage | Tailwind class |
|-------|----------------|
| Card radius | `rounded-[var(--radius-md)]` (10px) |
| Chip radius | `rounded-[var(--radius-sm)]` (6px) |
| Table cell padding | `px-3 py-2` |
| Chip padding | `px-2 py-0.5` |
| Card shadow | `shadow-[var(--shadow-card)]` |

#### Existing Components to REUSE (NEVER recreate)

| Component | Path | Use for |
|-----------|------|---------|
| `Badge` | `web/src/components/ui/badge.tsx` | State badges (already used for SIM state) — operator chip is a NEW molecule, do not force-fit Badge |
| `RATBadge` | `web/src/components/ui/rat-badge.tsx` | RAT type rendering — already in use |
| `Card`, `CardHeader`, `CardContent` | `web/src/components/ui/card.tsx` | Dashboard operator-health card |
| `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableCell`, `TableHead` | `web/src/components/ui/table.tsx` | All list renderings |
| `Tooltip` | `web/src/components/ui/tooltip.tsx` | Orphan chip tooltip exposing raw UUID |
| `AlertCircle` from `lucide-react` | — | Orphan "(Unknown)" warning icon |
| **NEW** `OperatorChip` | `web/src/components/shared/operator-chip.tsx` | Name + code + dot; handles `operator_code=null` (orphan) with `(Unknown)` fallback. Reused by SIMs/Sessions/Violations/eSIM/Dashboard. Max diameter: 28 chars. |

## Prerequisites

- [x] FIX-201 completed — shared payload DTO field-propagation lesson captured as PAT-006.
- [ ] FIX-206 **not required** — AC-8 (LEFT JOIN null-safe orphan handling) is the explicit handoff here. Plan does not depend on FIX-206 orphan cleanup.
- [x] Schema already in place — no migrations needed (see Database Schema).
- [x] Required indexes already exist: `idx_sims_tenant_operator`, `idx_sims_tenant_apn`, `idx_sims_tenant_policy` (verified in `20260320000002_core_schema.up.sql:309-311`).

## Task Decomposition Rules

Each task lists `Context refs` — Amil extracts those sections from this plan and hands them to the Developer. No task touches 5+ files.

## Tasks

### Task 1: `simStore.ListEnriched` + `GetByIDEnriched` — store-layer JOIN
- **Files:** Modify `internal/store/sim.go`
- **Depends on:** — (first)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim.go:1073-1098` (`AggregateByOperator` — already uses `JOIN operators o ON s.operator_id = o.id`). Follow same JOIN/alias style.
- **Context refs:** Architecture Context > Data Flow; Database Schema; Bug Pattern Warnings (PAT-006).
- **What:** Add a new struct `SIMWithNames` that embeds `SIM` and adds `OperatorName, OperatorCode *string; APNName *string; PolicyName *string; PolicyVersionNumber *int`. Add `ListEnriched(ctx, tenantID, p ListSIMsParams) ([]SIMWithNames, string, error)` and `GetByIDEnriched(ctx, tenantID, id) (*SIMWithNames, error)`. Query shape is the one in **Data Flow** — LEFT JOIN operators, apns, policy_versions, policies. Keep the existing filter builder logic (prefix `s.` on every `sims` column reference; prefix joined columns too). APN display precedence: `COALESCE(NULLIF(a.display_name, ''), a.name)`. Keep `ORDER BY s.created_at DESC, s.id DESC` and the cursor predicate `s.id < $N`. Existing `List` / `GetByID` are UNCHANGED — other callers (bulk, AAA, fleet agg) keep using them. **PAT-006 mandate:** the new struct has a dedicated scan helper `scanSIMWithNames(row)` — do not write inline scans. If for any refactor reason you touch `scanSIM`, update both `List` and `FetchSample` inline scans in the same commit.
- **Verify:** `go build ./...` passes; add a unit test `internal/store/sim_list_enriched_test.go` that seeds 3 SIMs (one orphan: operator_id pointing to a non-existent operator) + 1 APN + 1 policy version, calls `ListEnriched`, asserts: 3 rows returned, 2 have non-nil operator_name, 1 (orphan) has nil operator_name, apn_name fallback precedence works when display_name is empty string vs null.

### Task 2: Query-plan verification (AC-10 performance evidence)
- **Files:** Create `internal/store/sim_list_enriched_explain_test.go`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `internal/store/sim_test.go` — follow the `newTestSIMStore(t)` harness.
- **Context refs:** Database Schema; Acceptance Criteria Mapping (AC-10).
- **What:** Integration-style test (gated behind `testing.Short()` skip) that seeds 1,000 SIMs across 3 operators, runs `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) <ListEnriched query>` for a 50-item page, parses output, asserts: (a) no `Seq Scan on sims` at top level — must be Index Scan or Bitmap Index Scan using `idx_sims_tenant_*`; (b) no `Seq Scan on operators/apns/policy_versions/policies` (all joined via PK lookup); (c) total execution time < 100ms. Writes full EXPLAIN output to testdata file on failure for Gate evidence. **Do NOT add a new migration** — required indexes already exist per Architecture Context.
- **Verify:** `make test` runs the test (skipped in short mode); `go test -run ListEnriched_Explain ./internal/store` passes against `make infra-up`.

### Task 3: SIM handler — swap to enriched store + widen DTO + delete per-row lookups
- **Files:** Modify `internal/api/sim/handler.go`; Modify `web/src/types/sim.ts`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler.go:162-213` — `toSIMResponse` — follow same builder style but extend to consume `*store.SIMWithNames`.
- **Context refs:** Architecture Context > Components Involved; API Specifications > SIM list; Design Token Map.
- **What:**
  1. Add `OperatorCode`, `PolicyVersionNumber` fields to `simResponse` (json tags `operator_code,omitempty` / `policy_version_number,omitempty`). `OperatorName, APNName, PolicyName, PolicyVersionID` already exist.
  2. Rewrite `toSIMResponse` to accept `*store.SIMWithNames`. Old `*store.SIM` callers (Create / Activate / Suspend / Resume / Terminate / ReportLost / Patch) — add a helper `toSIMResponseBase(*store.SIM) simResponse` that fills only the non-enriched fields, **plus** a follow-up batch enrichment for those mutation endpoints via a single `GetByIDEnriched` call AFTER the state change. This keeps PAT-006 safe: the enriched path has ONE scan helper.
  3. In `List`, replace the entire per-row enrichment block (`handler.go:469-591`) with: `sims, nextCursor, err := h.simStore.ListEnriched(...)`; loop mapping each `SIMWithNames` → `simResponse`. IP pool enrichment stays on its batch path (keep the `ipMap` / `poolNameMap` blocks — these are NOT in scope of FIX-202). Remove `nameCache` usage for operator/apn (JOIN replaces it; the cache is now dead weight for this handler — comment-remove, do not delete the NameCache type since other handlers use it).
  4. In `Get` and `Patch`, replace `simStore.GetByID` + `enrichSIMResponse` with `simStore.GetByIDEnriched`. Delete `enrichSIMResponse` and its call sites. Keep IP enrichment logic inline.
  5. Update `web/src/types/sim.ts` `SIM` interface: add `operator_code?: string`, `policy_version_number?: number` — both optional (nullable for orphan).
- **Verify:** `curl localhost:8080/api/v1/sims?limit=3` returns JSON with `operator_name`, `operator_code`, `policy_name`, `policy_version_number` populated; `go build ./...` + `go vet ./...` clean.

### Task 4: SIM list + detail UI — OperatorChip component & orphan fallback
- **Files:** Create `web/src/components/shared/operator-chip.tsx`; Create `web/src/lib/operator-chip.ts` (color map); Modify `web/src/pages/sims/index.tsx`; Modify `web/src/pages/sims/detail.tsx`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `web/src/components/ui/rat-badge.tsx` — follow same prop contract + variant map pattern. Read `web/src/pages/sims/index.tsx:400-417` for the existing dropdown chip class convention.
- **Context refs:** Screen Mockups (SIMs list); Design Token Map (Operator Chip Color Map); Architecture Context > Components Involved; Bug Pattern Warnings.
- **What:**
  1. `operator-chip.ts`: `export type OperatorCode = 'turkcell' | 'vodafone_tr' | 'turk_telekom' | string | null`; `export const operatorChipColor(code: OperatorCode): { dot: string, bg: string }` mapping per the Operator Chip Color Map. Default returns muted class set.
  2. `operator-chip.tsx`: component `<OperatorChip name={string|null} code={string|null} clickable?: boolean, onClick?: () => void />`. When `name` is null or empty: render `AlertCircle` in `text-warning` + `"(Unknown)"` in `text-text-secondary italic`, title=`"Orphan operator reference — see admin"` OR raw UUID via extra prop. When non-null: colored dot (6px circle) + name + code suffix (mono, tertiary color). Uses the design tokens verbatim — no hex, no arbitrary px except the explicit text sizes in the token map.
  3. `pages/sims/index.tsx`: replace the current Operator column cell (currently blank or UUID) with `<OperatorChip name={sim.operator_name} code={sim.operator_code} clickable onClick={() => nav('/operators/' + sim.operator_id)} />`. Same for APN column — render name only, fallback `"(Unknown)"` when `apn_name` missing. Policy column: `<span>{sim.policy_name ?? '—'}</span>` (no chip).
  4. `pages/sims/detail.tsx`: wherever the "Overview" panel shows operator/apn/policy fields, switch to `OperatorChip` for operator and null-safe span for apn/policy.
  5. ZERO hardcoded hex colors. ZERO arbitrary pixel values outside the token map.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px outside the explicit 13/11/12/6 px values called out in the token map.
- **Components:** Reuse `Table`, `Tooltip`, `RATBadge`. Create `OperatorChip` ONCE — reused in tasks 5 and 7.
- **Note:** Invoke `frontend-design` skill for professional quality.
- **Verify:** `grep -nE '#[0-9a-fA-F]{6}' web/src/components/shared/operator-chip.tsx web/src/lib/operator-chip.ts web/src/pages/sims/index.tsx web/src/pages/sims/detail.tsx` → **zero matches** (hex colors forbidden in any modified lines; pre-existing hex in unmodified lines allowed). `npm --prefix web run build` passes; visually: SIMs list shows "Turkcell / (turkcell)" not UUID, orphan row shows "(Unknown)" with warning icon.

### Task 5: Dashboard operatorHealthDTO widening + FE card update
- **Files:** Modify `internal/api/dashboard/handler.go`; Modify `internal/store/operator.go` (`ListGrantsWithOperators` SELECT expansion); Modify `web/src/types/dashboard.ts`; Modify `web/src/pages/dashboard/index.tsx`
- **Depends on:** Task 4 (OperatorChip)
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/dashboard/handler.go:224-243` — follow same goroutine/mutex pattern to add the `active_sessions` merge.
- **Context refs:** API Specifications > Dashboard; Architecture Context > Components Involved; Screen Mockups (Dashboard); Design Token Map (Operator Chip Color Map); Risks & Mitigations (R-3).
- **What:**
  1. `internal/store/operator.go` `ListGrantsWithOperators`: extend the SELECT list with `o.sla_uptime_target, o.updated_at` (already selects `o.code`). Extend `GrantWithOperator` struct with `OperatorCode string`, `SLATarget *float64`, `OperatorUpdatedAt time.Time` fields (name must match SELECT order); update the row scan accordingly.
  2. `dashboard/handler.go`: widen `operatorHealthDTO` with `Code string`, `SLATarget *float64`, `ActiveSessions *int64`, `LastHealthCheck *string`, `LatencyMs *float64`, `AuthRate *float64` (all JSON-tagged `,omitempty` or explicit null in envelope).
  3. In the operator-health goroutine: populate `Code` and `SLATarget` from `GrantWithOperator`. After `stats, _ := h.sessionStore.GetActiveStats(...)` already runs in a sibling goroutine — extend the shared `resp` path to JOIN active_sessions per operator: use `stats.ByOperator[operatorID.String()]` lookup. Since goroutines race, protect the merge under the existing `mu` mutex. `LatencyMs, AuthRate` stay nil in this story (scoped per Risk R-3); `LastHealthCheck` = `OperatorUpdatedAt.Format(time.RFC3339)` (best-effort — operator health loop writes to this column today).
  4. `web/src/types/dashboard.ts`: extend `operatorHealthDTO` type with the 6 new optional fields.
  5. `web/src/pages/dashboard/index.tsx`: replace the current operator-health item rendering with `<OperatorChip name={op.name} code={op.code} />` + a right-aligned health pct (current) + a sub-line showing `active_sessions` ("12,483 active") when truthy + SLA target ("SLA 99.90%") when truthy. Hide nulls completely — do NOT render "null" strings.
- **Verify:** `curl localhost:8080/api/v1/dashboard` returns `operator_health[].code` and `sla_target` populated, `active_sessions` populated when RADIUS sim is running, `latency_ms`/`auth_rate` omitted/null. Visual: Dashboard card shows chip + metrics; nulls do not leak.

### Task 6: Session handler — batch-enrich per page (no per-row DB)
- **Files:** Modify `internal/api/session/handler.go`; Modify `internal/store/sim.go` (add `GetManyByIDsEnriched` helper); Modify `web/src/types/session.ts`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/sim/handler.go:512-562` for the per-page lookup-map idiom — this task replaces per-row enrichment with the same idiom but against `ANY($1)` queries.
- **Context refs:** Architecture Context > Components Involved; API Specifications > Sessions; Acceptance Criteria Mapping (AC-4, AC-9).
- **What:**
  1. Add `sim.GetManyByIDsEnriched(ctx, tenantID, ids []uuid.UUID) (map[uuid.UUID]*SIMWithNames, error)` — single `WHERE id = ANY($2)` with the same JOINs as Task 1. Returns a map keyed by sim.id.
  2. Rewrite `handler.List` (`session/handler.go:264-317`) and `handler.Get` enrichment path:
     - Collect all unique sim UUIDs from the page.
     - Single call to `simStore.GetManyByIDsEnriched` → map.
     - For each session DTO, pull operator_name/operator_code/apn_name/policy_name/policy_version_number from the map entry. IMSI/ICCID/MSISDN fallbacks (existing behavior) come from the same map.
     - Fall back to "" / nil if the sim is missing (orphan session).
  3. Delete the per-row `enrichSessionDTO` function.
  4. Widen `sessionDTO` with `OperatorCode string; PolicyName string; PolicyVersionNumber int` — all `,omitempty`.
  5. Update `web/src/types/session.ts` to mirror.
- **Verify:** `curl /api/v1/sessions?limit=50` → count DB round-trips (via `argus-app` log): exactly 1 for `session.Manager.ListActive` + 1 for `GetManyByIDsEnriched`. Previously the `enrichSessionDTO` path did up to 3 DB round-trips per row.

### Task 7: Violation DTO + enriched store query + UI column wiring
- **Files:** Modify `internal/store/policy_violation.go`; Modify `internal/api/violation/handler.go`; Modify `web/src/types/notification.ts` (violation shape if shared) OR create `web/src/types/violation.ts` if it does not exist; Modify `web/src/pages/violations/index.tsx`
- **Depends on:** Task 4 (OperatorChip)
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/policy_violation.go:107-210` — existing `List` builder idiom. Read `internal/api/sim/handler.go:104-133` for DTO struct style.
- **Context refs:** Architecture Context > Components Involved; API Specifications > Violations; Database Schema (policy_violations + sims + operators + apns + policies + policy_versions); Screen Mockups (Violations list).
- **What:**
  1. `policy_violation.go`: add `type PolicyViolationWithNames struct { PolicyViolation; ICCID, IMSI *string; MSISDN *string; OperatorName, OperatorCode, APNName, PolicyName *string; PolicyVersionNumber *int }`. Add `ListEnriched(ctx, tenantID, p ListViolationsParams)` and `GetByIDEnriched(ctx, id, tenantID)` — LEFT JOIN sims (s on v.sim_id = s.id) + operators (o on v.operator_id = o.id OR fallback s.operator_id) + apns (a on v.apn_id) + policies (p on v.policy_id) + policy_versions (pv on v.version_id). Prefer v.operator_id over sims.operator_id for authoritativeness (violation recorded operator at evaluation time).
  2. `violation/handler.go`: introduce `violationDTO` struct explicitly mapping all fields listed in the API spec. Replace `apierr.WriteSuccess(w, ..., v)` in `Get` and `apierr.WriteList(w, ..., violations, ...)` in `List` with a mapping loop that converts `*PolicyViolationWithNames` to `violationDTO`. `Remediate` response can continue to return the raw struct (internal endpoint with less UI surface).
  3. FE: render new columns `ICCID / Operator (chip) / APN / Policy (v<n>) / Severity / Type / Detected`. Orphan fallback: "(Unknown)" with warning icon.
- **Verify:** `curl /api/v1/violations` returns iccid, imsi, operator_name, operator_code, apn_name, policy_name, policy_version_number populated; violations list page shows resolved names, no UUIDs.

### Task 8: eSIM profile DTO widening + operator chip in list/detail
- **Files:** Modify `internal/store/esim.go`; Modify `internal/api/esim/handler.go`; Modify `web/src/types/esim.ts`; Modify `web/src/pages/sims/esim-tab.tsx` OR the relevant eSIM list page
- **Depends on:** Task 4 (OperatorChip)
- **Complexity:** low
- **Pattern ref:** Read `internal/store/esim.go:105-...` `List` — follow same filter/builder shape; just add one `LEFT JOIN operators o ON o.id = p.operator_id` with selected columns.
- **Context refs:** API Specifications > eSIM; Database Schema (esim_profiles); Design Token Map (Operator Chip Color Map).
- **What:**
  1. `esim.go`: add `ESimProfileWithNames` (embeds `ESimProfile`, adds `OperatorName, OperatorCode *string`). Add `ListEnriched` and `GetByIDEnriched`.
  2. `esim/handler.go`: extend `profileResponse` with `OperatorName, OperatorCode` (omitempty). Switch `List` and `Get` call sites. Other mutation endpoints (`Enable`, `Disable`, `Switch`, `Create`, `Delete`) may continue returning the non-enriched response — acceptable for now; add a TODO comment noting they could switch to `GetByIDEnriched` if UIs consume them directly.
  3. `web/src/types/esim.ts`: add optional fields.
  4. FE: replace UUID display with `<OperatorChip>`.
- **Verify:** `curl /api/v1/esim/profiles` returns operator_name/operator_code; UI renders chip.

### Task 9: Notification entity reference wrapping (AC-7 — scoped)
- **Files:** Modify `internal/store/notification.go` OR `internal/notification/service.go` (whichever emits SIM-entity refs); Modify `web/src/types/notification.ts`; Modify `web/src/pages/notifications/*.tsx`
- **Depends on:** — (independent)
- **Complexity:** low
- **Pattern ref:** Read existing notification render code to find where SIM references are injected into notification bodies.
- **Context refs:** Architecture Context > Components Involved; API Specifications.
- **What:** **SCOPE-LIMITED.** FIX-212 owns the full unified event envelope. FIX-202 ONLY does: when a notification body references a SIM/operator/APN/policy entity, the reference object carries `{entity_type, entity_id, display_name}` where `display_name` is resolved from the relevant store at notification-emit time (not at read time). If the emit path already has the parent entity in hand, use it; if not, do a single resolve per notification (not per viewer). FE: render `display_name` as a clickable link to `/<entity_type>s/<entity_id>`; fall back to "(Unknown)" when display_name empty. DO NOT build the unified envelope here — that is FIX-212.
- **Verify:** Trigger a test notification (e.g. via policy violation path) — persisted body carries `display_name`; FE renders link.

### Task 10: Tests — unit + integration + regression
- **Files:** Create/extend `internal/store/sim_list_enriched_test.go`; Create `internal/api/sim/handler_list_enriched_test.go`; Extend `internal/api/session/handler_test.go`; Create `internal/api/violation/handler_enriched_test.go`
- **Depends on:** Tasks 1, 3, 6, 7
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim_test.go` (store test harness with migrations) and `internal/api/sim/handler_test.go` (handler test harness with httptest server + sql mock).
- **Context refs:** Acceptance Criteria Mapping; Database Schema; API Specifications.
- **What:**
  - **Unit (store):** `ListEnriched` — 3 SIMs (healthy + orphan + no-policy), asserts enrichment fields per AC-1/AC-2. `policy_violation.ListEnriched` — equivalent case.
  - **Handler:** `GET /api/v1/sims` returns the 6 new fields; orphan SIM returns `operator_name: null` not a crash (AC-8).
  - **Regression:** Existing bulk/AAA callers using `simStore.List` (unchanged) still pass — a grep check that only `handler.go` call sites were changed.
  - **E2E:** `curl` smoke test in `scripts/smoke-fix-202.sh` — hits 6 endpoints, greps response JSON for presence of the new field names.
- **Verify:** `make test` green; new tests pass; zero regressions in existing SIM/session tests.

### Complexity Guide
- low: Task 2, Task 8 (if wired correctly), Task 9 (scope-limited).
- medium: Tasks 1, 3, 4, 5, 6, 7, 10.
- high: None — story is M. No opus-grade work required.

## Acceptance Criteria Mapping

| AC | Implemented in | Verified by |
|----|----------------|-------------|
| AC-1: SIM DTO adds operator_name/operator_code/policy_name/policy_version_number/policy_version_id | Task 3 | Task 10 (unit + handler) |
| AC-2: Store `sim.List` uses JOIN — single query, no N+1 | Task 1 | Task 2 (EXPLAIN verifies no Seq Scan of operators/apns/policies) |
| AC-3: Dashboard operatorHealthDTO adds code/latency_ms/active_sessions/auth_rate/last_health_check/sla_target | Task 5 | Task 10 (handler test); latency_ms/auth_rate scoped null per Risk R-3 |
| AC-4: Session DTO adds policy_name/policy_version/operator_name | Task 6 | Task 10 (session handler test) |
| AC-5: Violation DTO adds iccid/policy_name/policy_version/operator_name/apn_name | Task 7 | Task 10 (violation handler test) |
| AC-6: eSIM DTO adds operator_name/operator_code | Task 8 | `curl` response smoke in Task 10 |
| AC-7: Notification body entity refs carry `{entity_type, entity_id, display_name}` | Task 9 | Manual verification (scoped to SIM refs; FIX-212 handles full envelope) |
| AC-8: Orphan entity handling via LEFT JOIN nullability + FE "(Unknown)" | Task 1 (LEFT JOIN) + Task 4 (FE OperatorChip fallback) | Task 10 orphan row test |
| AC-9: Backend enrichment centralized in store query — no per-row handler lookups | Tasks 1, 3, 6, 7, 8 | Task 10 regression grep + session round-trip count assertion |
| AC-10: SIM list p95 < 100 ms for 50-item page | Task 2 | EXPLAIN ANALYZE test (Task 2); manual p95 measurement before PR merge |

## Story-Specific Compliance Rules

- **API envelope**: Standard `{status, data, meta?, error?}` — unchanged. New fields are additive; no breaking change to existing consumers.
- **DB scoping**: Every new JOIN predicate includes `tenant_id` on the sims/violations/apns/policies side. Operators are tenant-agnostic; do not add a tenant predicate to operators JOIN.
- **DB schema**: No migrations. AC-10 p95 evidenced by existing indexes. If Task 2 EXPLAIN reveals a missed index only then — and that is a known-zero outcome today — a follow-up migration would be filed as tech debt, not in-story.
- **UI**: Tailwind v4 `@theme` tokens (`text-text-primary`, `bg-bg-elevated`, `text-accent`, `text-warning`, `text-danger`, `text-info`, `*-dim` backgrounds) per Design Token Map. No hex. No `text-gray-*`, `bg-blue-*`, etc.
- **UI clickability**: Operator chip routes to `/operators/:id`; APN to `/apns/:id`; policy to `/policies/:id`. Orphan chip non-clickable (title tooltip exposes raw UUID).
- **Business rule**: APN name precedence = `COALESCE(NULLIF(display_name, ''), name)`. Policy display format = `"<name> (v<version>)"`.
- **ADR compliance**: No ADR impact — pure DTO widening with zero protocol/auth changes.

## Bug Pattern Warnings

Read `docs/brainstorming/bug-patterns.md` entries PAT-001..PAT-006 before implementing.

- **PAT-006 (PRIMARY, FIX-201 Gate F-A1)**: Shared payload struct field silently omitted at construction sites. `store.SIM` has **THREE** scan sites — `scanSIM` (sim.go:137), inline scan in `List` (sim.go:292-298), inline scan in `FetchSample` (sim.go:1177-1181). **Mitigation:** this story introduces a NEW struct `SIMWithNames` with exactly ONE scan helper (`scanSIMWithNames`). Do NOT inline-scan `SIMWithNames` in multiple places. If any refactor during this story touches `scanSIM`, update all three existing sites in the same commit and add a `// PAT-006: keep in sync with scanSIM` comment above each inline scan block.
- **PAT-004 contextual note (sims partitioning)**: `sims` is LIST-partitioned by `operator_id` with composite PK `(id, operator_id)`. Docs (`db/_index.md` TBL-42, `phase-5/6-gate.md`) capture that no new `REFERENCES sims(id)` FK can be added — **this plan adds NO FKs into sims**; only JOINs FROM sims to parent tables, which the planner handles via partition-pruning. Verify Task 2 EXPLAIN output shows partition-pruned plan (`Append` → per-partition `Index Scan`), not `Seq Scan on sims_default`.
- **PAT-001/002/003/005**: Not applicable (metric double-writers, timer deadlines, metric labels, masked-secret sentinel are all out of scope).

## Tech Debt (from ROUTEMAP)

`docs/ROUTEMAP.md` Tech Debt table entries D-041..D-045 (the FIX-201 Gate deferred set) all target **FIX-216 / POST-GA / future stories** — none target FIX-202.

**No tech debt items for this story.**

## Mock Retirement

`web/src/mocks/` does not exist in this project — mocking is done at test level via `@tanstack/react-query` wrappers. No mock retirement for this story.

## Risks & Mitigations

- **R-1 (JOIN performance regression — partitioned sims):** LIST-partitioned sims + 3 LEFT JOINs could regress vs the current per-row-cache path under tenant with hot operators. **Mitigation:** Task 2 EXPLAIN test captures plan shape + execution time; indexes `idx_sims_tenant_operator`, `idx_sims_tenant_apn`, `idx_sims_tenant_policy` already exist; the WHERE predicate starts with `s.tenant_id = $1` so partition elimination combined with tenant-keyed index scans is the expected plan. If Task 2 shows `Seq Scan on sims_default`, escalate to Gate.
- **R-2 (Orphan row dropping under INNER vs LEFT JOIN):** accidental `JOIN` (inner) would hide orphan SIMs, breaking AC-8. **Mitigation:** explicit `LEFT JOIN` keyword; Task 10 includes an orphan-row assertion.
- **R-3 (AC-3 dashboard fields not all free):** `latency_ms`, `auth_rate`, `last_health_check` have no single authoritative column today. **Mitigation:** `code`, `sla_target` shipped as populated; `active_sessions` pulled from existing `sessionStore.GetActiveStats`; `last_health_check` best-effort from `operators.updated_at`; `latency_ms` and `auth_rate` explicitly NULL in Wave 1 with FIX-229 (alert enhancements) or a follow-up story owning them. Document this scope-cut in the Gate report.
- **R-4 (Session path cannot do one store JOIN):** sessions are Redis/in-memory. **Mitigation:** batch-enrich pattern in Task 6 — 1 query per page for all sessions combined. Round-trip count is the regression guard, not query count.
- **R-5 (Test fixtures break):** existing tests may mock `operatorStore.GetByID` / `apnStore.GetByID` expecting per-row calls. **Mitigation:** Task 10 includes a grep-based regression check; handler tests that stubbed those calls must be removed or converted to stub the new `simStore.ListEnriched` call.
- **R-6 (PAT-006 regression):** adding `SIMWithNames` creates a NEW struct with scan site; future contributors might silently add fields to `SIMWithNames` without updating `scanSIMWithNames`. **Mitigation:** the single-scan-helper discipline + header comment pointing readers to PAT-006.
