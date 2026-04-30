# Implementation Plan: STORY-061 — eSIM Model Evolution

```yaml
story: STORY-061
title: eSIM Model Evolution
effort: M
acs: 4
tasks: 13
waves: 4
decisions:
  - DEV-164: On profile switch, old profile transitions to `available` (swapped, not operator-disabled)
  - DEV-165: Max 8 profiles per SIM enforced in Create handler
```

## Goal

Evolve the eSIM data model from single-profile-per-SIM (`UNIQUE(sim_id)`) to multi-profile support with a proper `available` state, atomic profile switch handler with CoA/DM + IP release + policy clear, and a multi-profile UI on the SIM detail page.

## Architecture Context

### Components Involved

| Component | Layer | Responsibility | File Path |
|-----------|-------|----------------|-----------|
| ESimProfileStore | Data Access | CRUD + state transitions for esim_profiles | `internal/store/esim.go` |
| eSIM API Handler | HTTP Handler | REST endpoints for profile operations | `internal/api/esim/handler.go` |
| Gateway Router | Routing | Route registration | `internal/gateway/router.go` |
| SMDPAdapter | External Integration | SM-DP+ communication | `internal/esim/smdp.go` |
| DMSender | AAA Session | Disconnect-Message dispatch | `internal/aaa/session/dm.go` |
| IPPoolStore | Data Access | IP allocation/release | `internal/store/ippool.go` |
| Policy Enforcer | Policy Engine | Policy evaluation (ref only) | `internal/policy/enforcer/enforcer.go` |
| eSIM Types | Frontend Types | TypeScript interfaces | `web/src/types/esim.ts` |
| eSIM Hooks | Frontend Data | React Query hooks | `web/src/hooks/use-esim.ts` |
| eSIM List Page | Frontend Page | Standalone eSIM profiles list | `web/src/pages/esim/index.tsx` |
| SIM Detail Page | Frontend Page | SIM detail with tabs | `web/src/pages/sims/detail.tsx` |

### Data Flow

**Profile Switch (AC-3):**
```
User → POST /api/v1/esim-profiles/{id}/switch { target_profile_id }
  → Handler: validate SIM ownership, check states
  → Handler: disconnectActiveSessionsForSwitch() [existing DM logic]
  → Store.Switch(ctx, tx):
      1. Lock source (must be 'enabled') + target (must be 'available' or 'disabled')
      2. UPDATE source → 'available' (DEV-164: swapped, not operator-disabled)
      3. UPDATE target ��� 'enabled'
      4. UPDATE sims: operator_id, esim_profile_id, apn_id=NULL, ip_address_id=NULL, policy_version_id=NULL
  → Post-commit (non-blocking):
      5. IPPoolStore.ReleaseIP() if old SIM had ip_address_id
      6. Emit NATS event "esim.profile.switched"
  → Return SwitchResult
```

**Load Profile (AC-4 — new Create endpoint):**
```
User → POST /api/v1/esim-profiles { sim_id, eid, operator_id, iccid_on_profile, profile_id }
  → Handler: validate SIM exists + is eSIM type + tenant
  → Handler: check profile count < 8 (DEV-165)
  → SMDPAdapter.DownloadProfile()
  → Store.Create() — insert row with state='available'
  �� Return profile
```

**Delete Profile (AC-4 — new Delete endpoint):**
```
User → DELETE /api/v1/esim-profiles/{id}
  → Handler: validate ownership
  → Handler: check state != 'enabled' (409 if enabled)
  → SMDPAdapter.DeleteProfile()
  → Store.SoftDelete() — set state='deleted'
  → Return success
```

### API Specifications

**Existing endpoints (modified behavior):**

- `GET /api/v1/esim-profiles` — List (add `sim_id` query param support — already exists)
- `GET /api/v1/esim-profiles/{id}` — Get single profile
- `POST /api/v1/esim-profiles/{id}/enable` — Enable (now accepts `available` OR `disabled` state)
- `POST /api/v1/esim-profiles/{id}/disable` — Disable (unchanged)
- `POST /api/v1/esim-profiles/{id}/switch` — Switch (evolved: old→available, IP release, policy clear)

**New endpoints:**

- `POST /api/v1/esim-profiles` — Create (Load Profile)
  - Request: `{ "sim_id": "uuid", "eid": "string", "operator_id": "uuid", "iccid_on_profile": "string", "profile_id"?: "string" }`
  - Success 201: `{ "status": "success", "data": { ...profileResponse } }`
  - Errors: 400 (invalid format), 404 (SIM not found), 409 (DUPLICATE_PROFILE), 422 (NOT_ESIM, PROFILE_LIMIT_EXCEEDED)

- `DELETE /api/v1/esim-profiles/{id}` — Soft Delete
  - Success 200: `{ "status": "success", "data": { ...profileResponse with state="deleted" } }`
  - Errors: 404 (not found), 409 (CANNOT_DELETE_ENABLED_PROFILE)

### Database Schema

**Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL current schema)**

```sql
CREATE TABLE IF NOT EXISTS esim_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL UNIQUE,              -- ← REMOVE this UNIQUE
    eid VARCHAR(32) NOT NULL,
    sm_dp_plus_id VARCHAR(255),
    operator_id UUID NOT NULL REFERENCES operators(id),
    profile_state VARCHAR(20) NOT NULL DEFAULT 'disabled',
    iccid_on_profile VARCHAR(22),
    last_provisioned_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_esim_profiles_sim ON esim_profiles (sim_id);
CREATE INDEX IF NOT EXISTS idx_esim_profiles_eid ON esim_profiles (eid);
CREATE INDEX IF NOT EXISTS idx_esim_profiles_operator ON esim_profiles (operator_id);
```

**Target schema after migration:**

```sql
ALTER TABLE esim_profiles ADD COLUMN profile_id VARCHAR(64);
ALTER TABLE esim_profiles ALTER COLUMN profile_state SET DEFAULT 'available';

-- Remove old single-profile UNIQUE constraint
DROP INDEX IF EXISTS idx_esim_profiles_sim;
ALTER TABLE esim_profiles DROP CONSTRAINT IF EXISTS esim_profiles_sim_id_key;

-- Add new partial unique (only one enabled per SIM)
CREATE UNIQUE INDEX idx_esim_profiles_sim_enabled
  ON esim_profiles (sim_id) WHERE profile_state = 'enabled';

-- Add multi-profile unique (one row per sim+profile_id combo)
CREATE UNIQUE INDEX idx_esim_profiles_sim_profile
  ON esim_profiles (sim_id, profile_id) WHERE profile_id IS NOT NULL;

-- Add state filter index for listing
CREATE INDEX idx_esim_profiles_sim_state
  ON esim_profiles (sim_id, profile_state);

-- Valid states check
ALTER TABLE esim_profiles ADD CONSTRAINT chk_esim_profile_state
  CHECK (profile_state IN ('available', 'enabled', 'disabled', 'deleted'));
```

**Down migration strategy:**
- For each sim_id with multiple rows: keep the `enabled` one (or most recently updated if none enabled), DELETE the rest
- Drop new indexes/constraints
- Restore `UNIQUE(sim_id)` and old index
- Drop `profile_id` column
- Revert default to `'disabled'`

### Screen Mockups

**eSIM Tab on SIM Detail (new tab — only when sim_type='esim'):**

```
┌───���─────────────────────────────────────────────────────────┐
│  eSIM Profiles (3)                        [+ Load Profile]  │
├─��──────────────────────────────────────��────────────────────┤
│  ┌──────────────────────────────────────────────────────┐   │
│  │ ● ENABLED  Profile: prof-abc123   Op: Turkcell       │   │
│  │   EID: 89001012...  ICCID: 8990101200...            │   │
│  │   Provisioned: 2026-04-10 14:30                     │   │
│  │                                      [Disable] [Switch] │
│  └──────────��─────────────────────��─────────────────────┘   │
│  ┌────────────��───────────────────────────��─────────────┐   │
│  │ ○ AVAILABLE  Profile: prof-def456   Op: Vodafone     │   │
│  │   EID: 89001012...  ICCID: 8990101200...            │   │
│  │   Provisioned: 2026-04-08 09:15                     │   │
│  │                               [Enable] [Delete]      │   │
│  └────────────────��─────────────────────────────────────┘   │
│  ┌────────────────────────────────────────��─────────────┐   │
│  │ ○ DISABLED  Profile: prof-ghi789   Op: Turk Telekom  │   │
│  │   EID: 89001012...  ICCID: 8990101200...            │   │
│  │   Provisioned: 2026-04-05 16:45                     │   │
│  │                               [Enable] [Delete]      │   │
│  └─────────────────────────────────���────────────────────┘   │
└─────────────────────────────────────────────���───────────────┘
```

- Navigation: SIM Detail → eSIM tab (visible only for `sim_type === 'esim'`)
- Actions per state:
  - `enabled`: Disable, Switch (opens dropdown/dialog to pick target)
  - `available`: Enable, Delete
  - `disabled`: Enable, Delete
  - `deleted`: none (grayed out, or hidden)

### Design Token Map

#### Color Tokens (from FRONTEND.md)
| Usage | Token/CSS Var | NEVER Use |
|-------|---------------|-----------|
| Page bg | `var(--bg-primary)` / `bg-bg-primary` | `bg-[#06060B]` |
| Card bg | `var(--bg-surface)` / `bg-bg-surface` | `bg-white`, `bg-[#0C0C14]` |
| Elevated bg | `var(--bg-elevated)` / `bg-bg-elevated` | `bg-[#12121C]` |
| Primary text | `text-text-primary` | `text-[#E4E4ED]`, `text-gray-100` |
| Secondary text | `text-text-secondary` | `text-[#7A7A95]`, `text-gray-500` |
| Tertiary text | `text-text-tertiary` | `text-[#4A4A65]` |
| Accent (cyan) | `text-accent` / `bg-accent-dim` | `text-[#00D4FF]`, `bg-blue-500` |
| Success (green) | `text-success` / `bg-success-dim` | `text-[#00FF88]`, `text-green-400` |
| Warning (yellow) | `text-warning` / `bg-warning-dim` | `text-[#FFB800]` |
| Danger (red) | `text-danger` / `bg-danger-dim` | `text-[#FF4466]`, `text-red-500` |
| Purple (eSIM) | `text-purple` | `text-[#A855F7]` |
| Border | `border-border` | `border-[#1E1E30]` |
| Border subtle | `border-border-subtle` | `border-gray-800` |

#### Typography Tokens
| Usage | Token | NEVER Use |
|-------|-------|-----------|
| Page title | `text-[16px] font-semibold` | `text-2xl`, `text-xl` |
| Section label | `text-xs uppercase tracking-wider` | arbitrary sizes |
| Body text | `text-sm` (14px) | `text-[14px]` |
| Table data | `text-xs` (13px) | `text-[13px]` |
| Mono data | `font-mono text-xs` | custom font stacks |
| Badge text | `text-[10px]` or `text-xs` | arbitrary |

#### Spacing & Elevation Tokens
| Usage | Token | NEVER Use |
|-------|-------|-----------|
| Card shadow | `shadow-[var(--shadow-card)]` | `shadow-none` |
| Card radius | `rounded-[var(--radius-md)]` or `rounded-xl` | inconsistent |
| Spacing | `gap-3`, `gap-4`, `p-4`, `space-y-4` | arbitrary px |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons |
| `<Card>` | `web/src/components/ui/card.tsx` | Profile cards |
| `<Badge>` | `web/src/components/ui/badge.tsx` | State badges |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Confirm dialogs |
| `<Input>` | `web/src/components/ui/input.tsx` | Form fields |
| `<Tabs>` | `web/src/components/ui/tabs.tsx` | SIM detail tabs |
| `<Table>` | `web/src/components/ui/table.tsx` | Profile list (if table layout) |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | Loading states |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Action pending |
| `<InfoRow>` | `web/src/components/ui/info-row.tsx` | Key-value display |
| `<DropdownMenu>` | `web/src/components/ui/dropdown-menu.tsx` | Action menus |

## Prerequisites

- [x] STORY-057 completed (SIM detail page with tabs)
- [x] STORY-060 AC-6 completed (DM sender + `SetSessionDeps` already wired)
- [x] STORY-063 completed (GetProfileInfo on SMDPAdapter)
- [x] Existing `internal/store/esim.go` with Enable/Disable/Switch methods
- [x] Existing `internal/api/esim/handler.go` with DM integration

## Decisions

| ID | Decision | Rationale |
|----|----------|-----------|
| DEV-164 | On profile switch, old profile goes to `available` (not `disabled`) | Switch is a swap, not an operator-disabled action. `disabled` reserved for explicit operator deactivation. Enables immediate re-switch without manual re-enable. |
| DEV-165 | Max 8 profiles per SIM enforced in Create handler | Industry standard (GSMA SGP.22 recommends max 8). Prevents unbounded rows. Configurable later if needed. |

## Tasks

### Task 1: Migration — Multi-Profile Schema + `available` State
- **Files:** Create `migrations/20260412000001_esim_multiprofile.up.sql`, Create `migrations/20260412000001_esim_multiprofile.down.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260320000002_core_schema.up.sql` lines 252-270 — existing esim_profiles table DDL
- **Context refs:** ["Database Schema"]
- **What:**
  - UP: Add `profile_id VARCHAR(64)` column. Drop `idx_esim_profiles_sim` unique index and `esim_profiles_sim_id_key` constraint. Create partial unique `idx_esim_profiles_sim_enabled ON esim_profiles (sim_id) WHERE profile_state = 'enabled'`. Create unique `idx_esim_profiles_sim_profile ON esim_profiles (sim_id, profile_id) WHERE profile_id IS NOT NULL`. Create index `idx_esim_profiles_sim_state ON esim_profiles (sim_id, profile_state)`. Add CHECK constraint `chk_esim_profile_state CHECK (profile_state IN ('available', 'enabled', 'disabled', 'deleted'))`. Change column default from `'disabled'` to `'available'`.
  - DOWN: For each sim_id with multiple rows, keep the enabled or most recently updated one, DELETE the rest. Drop new indexes/constraints. Restore `UNIQUE(sim_id)` index. Drop `profile_id` column. Revert default to `'disabled'`. Drop CHECK constraint.
  - NOTE: `migrations/seed/003_comprehensive_seed.sql` has esim_profiles inserts. Verify the seed doesn't insert two rows for same sim_id (it shouldn't — old schema had UNIQUE(sim_id)). Existing seed rows survive fine since `disabled` is a valid state and `profile_id` is nullable.
- **Verify:** `make db-migrate` succeeds. `psql -c "\d esim_profiles"` shows new column and constraints. Run down then up again to verify reversibility.

### Task 2: Error Codes + Decisions + Go Constants
- **Files:** Modify `docs/architecture/ERROR_CODES.md`, Modify `docs/brainstorming/decisions.md`, Modify `internal/apierr/codes.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/ERROR_CODES.md` lines 172-179 — existing eSIM errors section. Read `internal/apierr/codes.go` — find `CodeProfileAlreadyEnabled` and follow same const pattern.
- **Context refs:** ["API Specifications"]
- **What:**
  - Add 5 new error codes to ERROR_CODES.md under "eSIM Errors":
    1. `PROFILE_LIMIT_EXCEEDED` (422) — Max profiles per SIM reached
    2. `CANNOT_DELETE_ENABLED_PROFILE` (409) — Cannot delete a profile in enabled state
    3. `DUPLICATE_PROFILE` (409) — Profile with same sim_id+profile_id already exists
    4. `PROFILE_NOT_AVAILABLE` (422) — Profile is not in available/disabled state for enable
    5. `IP_RELEASE_FAILED` (warning, non-blocking) — IP release failed during switch post-handler
  - Add DEV-164 and DEV-165 to decisions.md development decisions section.
  - Add Go constants in `internal/apierr/codes.go`:
    - `CodeProfileLimitExceeded = "PROFILE_LIMIT_EXCEEDED"`
    - `CodeCannotDeleteEnabled = "CANNOT_DELETE_ENABLED_PROFILE"`
    - `CodeDuplicateProfile = "DUPLICATE_PROFILE"`
    - `CodeProfileNotAvailable = "PROFILE_NOT_AVAILABLE"`
    - `CodeIPReleaseFailed = "IP_RELEASE_FAILED"`
- **Verify:** `grep -c "PROFILE_LIMIT_EXCEEDED\|CANNOT_DELETE_ENABLED_PROFILE\|DUPLICATE_PROFILE\|PROFILE_NOT_AVAILABLE\|IP_RELEASE_FAILED" docs/architecture/ERROR_CODES.md` returns 5. `go build ./internal/apierr/...` compiles.

### Task 3: Store — Model Update + Create/SoftDelete/CountBySIM Methods
- **Files:** Modify `internal/store/esim.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/esim.go` — follow existing `Enable()` transaction pattern
- **Context refs:** ["Database Schema", "Architecture Context > Data Flow"]
- **What:**
  - Add `ProfileID *string` field to `ESimProfile` struct (after `SMDPPlusID`).
  - Update `esimProfileColumns` to include `ep.profile_id`.
  - Update `scanESimProfile` to scan the new column.
  - Add new error vars: `ErrProfileLimitExceeded`, `ErrDuplicateProfile`, `ErrCannotDeleteEnabled`.
  - Add `Create(ctx, params CreateESimProfileParams) (*ESimProfile, error)` — insert with state='available'. `CreateESimProfileParams` has: SimID, EID, SMDPPlusID, OperatorID, ICCIDOnProfile, ProfileID.
  - Add `SoftDelete(ctx, tenantID, profileID uuid.UUID) (*ESimProfile, error)` — check state != 'enabled' (return ErrCannotDeleteEnabled), then UPDATE set profile_state='deleted', updated_at=NOW().
  - Add `CountBySIM(ctx, simID uuid.UUID) (int, error)` — `SELECT COUNT(*) FROM esim_profiles WHERE sim_id = $1 AND profile_state != 'deleted'`.
  - Modify `Enable()`: change `currentState != "disabled"` → `currentState != "disabled" && currentState != "available"` (accept both).
  - Modify `Switch()`: change `tgtState != "disabled"` → `tgtState != "disabled" && tgtState != "available"`. Change source profile update from `'disabled'` to `'available'` (DEV-164). Add `policy_version_id = NULL, ip_address_id = NULL` to the sims UPDATE in Switch.
- **Verify:** `go build ./internal/store/...` compiles. `go test ./internal/store/ -run ESimProfile` passes.

### Task 4: Store Tests — New Methods + Updated Transitions
- **Files:** Modify `internal/store/esim_test.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/esim_test.go` — follow existing test patterns
- **Context refs:** ["Database Schema", "Architecture Context > Data Flow"]
- **What:**
  - Add test: `TestCreate_HappyPath` — insert profile, verify state='available', profile_id stored.
  - Add test: `TestCreate_DuplicateProfile` — insert same sim_id+profile_id twice, expect unique violation error.
  - Add test: `TestSoftDelete_Available` — delete available profile, verify state='deleted'.
  - Add test: `TestSoftDelete_Enabled_Fails` — try delete enabled profile, expect ErrCannotDeleteEnabled.
  - Add test: `TestCountBySIM` — insert 3 profiles (1 deleted), expect count=2.
  - Add test: `TestEnable_FromAvailable` — enable profile in 'available' state, expect success.
  - Add test: `TestSwitch_OldGoesToAvailable` — switch profiles, verify source.ProfileState == 'available'.
  - Add test: `TestSwitch_TargetFromAvailable` — switch when target is 'available', expect success.
  - Add test: `TestUniqueConstraint_OnlyOneEnabled` — insert 2 profiles for same SIM, enable both, expect second to fail with partial unique violation.
- **Verify:** `go test ./internal/store/ -run ESimProfile -v` — all pass.

### Task 5: Handler — Create (Load Profile) + Delete + Route Registration
- **Files:** Modify `internal/api/esim/handler.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 3
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/esim/handler.go` `Enable()` method — follow same validation + SMDP + store pattern
- **Context refs:** ["API Specifications", "Architecture Context > Data Flow"]
- **What:**
  - Add `ipPoolStore` field to Handler struct (type: interface with `ReleaseIP(ctx, poolID, simID) error`). Add setter method `SetIPPoolStore(store ipPoolReleaser)`.
  - Add `Create(w, r)` handler:
    1. Parse JSON body: `{ sim_id, eid, operator_id, iccid_on_profile, profile_id? }`
    2. Validate sim exists, is eSIM type, belongs to tenant
    3. Check `CountBySIM < 8` (DEV-165) → 422 PROFILE_LIMIT_EXCEEDED
    4. Call `smdpAdapter.DownloadProfile()` (fire-and-forget per DEV-086). If `profile_id` is empty in request, use `DownloadProfileResponse.ProfileID` as the stored value.
    5. Call `esimStore.Create()` — handle duplicate constraint → 409 DUPLICATE_PROFILE
    6. Audit entry
    7. Return 201 with profile response
  - Add `Delete(w, r)` handler:
    1. Parse profile ID from URL
    2. Get profile, verify tenant
    3. Call `smdpAdapter.DeleteProfile()` (fire-and-forget)
    4. Call `esimStore.SoftDelete()` — handle ErrCannotDeleteEnabled → 409
    5. Audit entry
    6. Return 200 with deleted profile
  - Register routes in `internal/gateway/router.go`:
    - `r.Post("/api/v1/esim-profiles", deps.ESimHandler.Create)`
    - `r.Delete("/api/v1/esim-profiles/{id}", deps.ESimHandler.Delete)`
- **Verify:** `go build ./internal/api/esim/...` and `go build ./internal/gateway/...` compile.

### Task 6: Handler — Switch Evolution (IP Release + Policy Clear + Event)
- **Files:** Modify `internal/api/esim/handler.go`
- **Depends on:** Task 5
- **Complexity:** high
- **Pattern ref:** Read `internal/api/esim/handler.go` `Switch()` method (lines 334-465) — the existing DM dispatch pattern
- **Context refs:** ["Architecture Context > Data Flow", "API Specifications"]
- **What:**
  - The store-level Switch (Task 3) already clears `ip_address_id` and `policy_version_id` on the SIM row.
  - After `esimStore.Switch()` returns success, add post-commit side effects (non-blocking, log errors):
    1. **IP Release:** If the SIM had an `ip_address_id` before switch (capture it before calling Switch), look up the old IP address to get its `pool_id`, then call `ipPoolStore.ReleaseIP(ctx, poolID, simID)`. Log warning on failure (IP_RELEASE_FAILED). Do NOT block the response.
    2. **NATS Event:** Publish `"esim.profile.switched"` event with payload `{ sim_id, old_profile_id, new_profile_id, new_operator_id, timestamp }` to the event bus (if available). This enables downstream jobs to re-resolve policy.
  - Add `eventBus` field (interface: `Publish(subject string, data []byte) error`) to Handler. Add setter `SetEventBus(bus eventPublisher)`.
  - The existing Switch handler already fetches the SIM at line 376 (`h.simStore.GetByID`). Use the existing `sim` variable to capture `sim.IPAddressID` — do NOT fetch twice.
  - Update `switchResponse` to include `ip_released bool` and `policy_cleared bool` fields.
- **Verify:** `go build ./internal/api/esim/...` compiles. Manual review of Switch flow.

### Task 7: Handler Tests — Create, Delete, Switch Evolution
- **Files:** Modify `internal/api/esim/handler_test.go`
- **Depends on:** Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/esim/handler_test.go` — follow existing test patterns
- **Context refs:** ["API Specifications", "Architecture Context > Data Flow"]
- **What:**
  - Add test: `TestCreate_HappyPath` — POST with valid body, expect 201 + profile with state=available.
  - Add test: `TestCreate_NotESIM` — SIM type is 'physical', expect 422 NOT_ESIM.
  - Add test: `TestCreate_LimitExceeded` — mock CountBySIM returning 8, expect 422 PROFILE_LIMIT_EXCEEDED.
  - Add test: `TestCreate_DuplicateProfile` — mock store returning ErrDuplicateProfile, expect 409.
  - Add test: `TestDelete_HappyPath` — DELETE available profile, expect 200 + state=deleted.
  - Add test: `TestDelete_EnabledProfile` — DELETE enabled profile, expect 409 CANNOT_DELETE_ENABLED_PROFILE.
  - Add test: `TestSwitch_IPReleased` — verify IP release called post-switch when SIM had ip_address_id.
  - Add test: `TestSwitch_PolicyCleared` — verify switch result includes policy_cleared=true.
- **Verify:** `go test ./internal/api/esim/ -v` — all pass.

### Task 8: Wire Dependencies in main.go/server.go
- **Files:** Modify `cmd/argus/main.go` (or wherever Handler dependencies are wired)
- **Depends on:** Task 5, Task 6
- **Complexity:** low
- **Pattern ref:** Read `cmd/argus/main.go` — find where `esimapi.NewHandler()` is called and `SetSessionDeps()` is called
- **Context refs:** ["Architecture Context > Components Involved"]
- **What:**
  - Wire `SetIPPoolStore()` on the eSIM handler with the existing `IPPoolStore` instance.
  - Wire `SetEventBus()` on the eSIM handler with the existing NATS event bus instance (if available at that point in the initialization).
  - Ensure the new routes are registered (they should be automatic since router.go reads from `deps.ESimHandler`).
- **Verify:** `go build ./cmd/argus/...` compiles. `go vet ./...` passes.

### Task 9: Frontend Types + Hook Updates
- **Files:** Modify `web/src/types/esim.ts`, Modify `web/src/hooks/use-esim.ts`
- **Depends on:** Task 5
- **Complexity:** low
- **Pattern ref:** Read `web/src/hooks/use-esim.ts` — follow existing mutation pattern
- **Context refs:** ["Design Token Map", "Architecture Context > Components Involved"]
- **What:**
  - In `web/src/types/esim.ts`:
    - Add `profile_id?: string` field to `ESimProfile` interface.
    - Add `ESimCreateRequest` type: `{ sim_id: string, eid: string, operator_id: string, iccid_on_profile: string, profile_id: string }`.
    - Update `ESimSwitchResult` to add `ip_released?: boolean`, `policy_cleared?: boolean`.
  - In `web/src/hooks/use-esim.ts`:
    - Add `useESimListBySim(simId: string)` hook — calls `GET /esim-profiles?sim_id={simId}&limit=50`. Returns non-paginated (max 8). Uses queryKey `[...ESIM_KEY, 'by-sim', simId]`.
    - Add `useCreateProfile()` mutation — `POST /esim-profiles` with body.
    - Add `useDeleteProfile()` mutation — `DELETE /esim-profiles/{id}`.
    - All mutations invalidate ESIM_KEY on success.
- **Verify:** `cd web && npx tsc --noEmit` — no type errors.

### Task 10: SIM Detail — eSIM Tab Component
- **Files:** Create `web/src/pages/sims/esim-tab.tsx`
- **Depends on:** Task 9
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/esim/index.tsx` — follow same card/table patterns and state badges
- **Context refs:** ["Screen Mockups", "Design Token Map", "Architecture Context > Components Involved"]
- **What:**
  - Create `ESimTab` component accepting `simId: string` prop.
  - Call `useESimListBySim(simId)` to fetch profiles.
  - Render card-based list of profiles (not table — it's max 8 items, cards are more appropriate per mockup).
  - Each profile card shows: state badge, profile_id, operator_id, eid (truncated), iccid, last_provisioned_at.
  - Action buttons per state:
    - `enabled`: [Disable] [Switch → dropdown of available/disabled profiles]
    - `available`/`disabled`: [Enable] [Delete]
    - `deleted`: hidden or grayed row
  - "Load Profile" button opens a dialog/modal with form fields: eid, operator_id (dropdown), iccid_on_profile, profile_id.
  - Confirmation dialogs for Enable, Disable, Delete actions.
  - Switch action: show dropdown of other profiles (available/disabled) on same SIM as switch targets.
  - Loading state: skeleton cards. Empty state: "No eSIM profiles loaded" with Load Profile CTA.
  - Tokens: Use ONLY classes from Design Token Map — zero hardcoded hex/px.
  - Components: Reuse Button, Card, Badge, Dialog, Input, Skeleton, Spinner, DropdownMenu from ui/ — NEVER raw HTML.
  - Note: Invoke `frontend-design` skill for professional quality.
- **Verify:** `cd web && npx tsc --noEmit` passes. `grep -r '#[0-9a-fA-F]' web/src/pages/sims/esim-tab.tsx` returns ZERO matches.

### Task 11: SIM Detail — Integrate eSIM Tab
- **Files:** Modify `web/src/pages/sims/detail.tsx`
- **Depends on:** Task 10
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/sims/detail.tsx` lines 758-812 — existing tab structure
- **Context refs:** ["Screen Mockups", "Architecture Context > Components Involved"]
- **What:**
  - Import `ESimTab` from `./esim-tab`.
  - Import `Smartphone` icon from lucide-react (already imported in the file).
  - Add a new `TabsTrigger` for "eSIM" with `Smartphone` icon — conditionally rendered only when `sim.sim_type === 'esim'`.
  - Add corresponding `TabsContent` rendering `<ESimTab simId={sim.id} />`.
  - Position the eSIM tab after "Overview" and before "Sessions" (or after "Sessions" — fits logically after overview).
- **Verify:** `cd web && npx tsc --noEmit` passes. Visual: navigate to an eSIM SIM detail page, eSIM tab visible.

### Task 12: Standalone eSIM Page — Add Delete Action
- **Files:** Modify `web/src/pages/esim/index.tsx`
- **Depends on:** Task 9
- **Complexity:** low
- **Pattern ref:** Read `web/src/pages/esim/index.tsx` — existing action buttons pattern (lines 311-347)
- **Context refs:** ["Design Token Map", "Architecture Context > Components Involved"]
- **What:**
  - Import `useDeleteProfile` from hooks.
  - Add "Delete" button for profiles in `available` or `disabled` state (next to Enable button).
  - Add delete to `actionDialog` action type union: `'enable' | 'disable' | 'switch' | 'delete'`.
  - Handle delete mutation in `handleAction`.
  - Add delete confirmation dialog text.
  - Style delete button: `border-danger/30 text-danger hover:bg-danger-dim`.
  - Import `Trash2` icon from lucide-react.
- **Verify:** `cd web && npx tsc --noEmit` passes.

### Task 13: Integration Test — Full Multi-Profile Flow
- **Files:** Create `internal/api/esim/integration_test.go` (or add to existing test file)
- **Depends on:** Task 7, Task 8 (handler tests + wiring must be done first)
- **Complexity:** high
- **Pattern ref:** Read `internal/api/esim/handler_test.go` — follow existing httptest patterns
- **Context refs:** ["Architecture Context > Data Flow", "API Specifications", "Database Schema"]
- **What:**
  - Test scenario: "Load 3 profiles → Enable B → Switch B→C → Delete A"
    1. POST /esim-profiles × 3 (for same SIM) → all return state=available
    2. POST /{B}/enable → B becomes enabled, A and C remain available
    3. POST /{B}/switch with target=C → B becomes available (DEV-164), C becomes enabled
    4. Verify partial unique: try POST /{A}/enable while C is enabled → should fail PROFILE_ALREADY_ENABLED
    5. DELETE /{A} → A becomes deleted
    6. Verify count: GET /esim-profiles?sim_id=X → returns 2 non-deleted profiles (B=available, C=enabled)
  - Test scenario: "Delete enabled profile fails"
    1. DELETE /{C} (enabled) → 409 CANNOT_DELETE_ENABLED_PROFILE
  - Test scenario: "Profile limit enforcement"
    1. Create 8 profiles for one SIM → all succeed
    2. Create 9th → 422 PROFILE_LIMIT_EXCEEDED
  - Uses testdb setup (if available) or mock store for integration. Follow existing test infrastructure patterns.
- **Verify:** `go test ./internal/api/esim/ -run Integration -v` — all pass.

## Wave Execution Plan

| Wave | Tasks | Parallel | Rationale |
|------|-------|----------|-----------|
| 1 | T1, T2 | Yes (independent) | Schema + docs/error constants — foundation |
| 2 | T3, T4 | Sequential (T3→T4) | Store methods then store tests |
| 3 | T5, T6, T7, T8 | T5→T6→T7 (sequential), T8 after T5+T6 | Handler creation + evolution + tests + wiring |
| 4 | T9, T10, T11, T12, T13 | T9→T10→T11, T12 after T9, T13 after T7+T8 | Frontend + integration test |

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1: `available` state distinct from `disabled`, state machine transitions | Task 1 (migration), Task 3 (store logic) | Task 4 (store tests), Task 13 (integration) |
| AC-2: Multi-profile schema — partial unique + (sim_id, profile_id) unique | Task 1 (migration) | Task 4 (TestUniqueConstraint), Task 13 |
| AC-3: Switch post-handler (disable old→enable new→CoA/DM→IP realloc→policy) | Task 3 (store), Task 6 (handler evolution) | Task 7 (handler tests), Task 13 (integration) |
| AC-4: UI multi-profile view + Load/Enable/Disable/Delete + 5 error codes | Task 2 (error codes), Task 9-12 (frontend) | Visual verification, type check |

## Story-Specific Compliance Rules

- **API:** Standard envelope `{ status, data, meta?, error? }` for all endpoints. Cursor pagination for list.
- **DB:** Migration must have both up and down scripts. Both tested. golang-migrate naming convention.
- **UI:** Design tokens from FRONTEND.md — no hardcoded colors. `frontend-design` skill for UI tasks. shadcn/ui components only.
- **Business:** Only one `enabled` profile per SIM (enforced by DB constraint + store logic). Max 8 profiles per SIM (DEV-165). Tenant isolation via JOIN sims (DEV-085 pattern).
- **ADR:** JWT auth required for all endpoints. Audit entry for all state-changing operations (ADR-002 pattern).

## Bug Pattern Warnings

- PAT-001 [STORY-059]: BR-assertion tests — check if any `*_br_test.go` touches eSIM state transitions. If so, update assertions to accept `available` state.
- PAT-002 [STORY-059]: Duplicated utility patterns — the eSIM handler already uses `extractIP`-like logic for NAS IP parsing (line 489 in handler.go: `strings.Index(nasIP, ":")`). Not a blocker for this story but note the pattern.

## Tech Debt

- D-003 (STORY-058 Review): Stale SCR IDs — story references SCR-072/073 which don't exist in current SCREENS.md. Doc-only issue, no implementation impact for this story.

## Mock Retirement

No mock retirement for this story — all endpoints use real store + SMDP mock adapter (which is the production mock per DEV-086).

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Down migration data loss (multi→single profile) | Keep most relevant profile (enabled > most recent). Add NOTICE in migration file. |
| Partial unique index performance on large tables | PostgreSQL handles partial indexes efficiently. Monitor query plans post-deploy. |
| IP release failure leaves stale allocations | Log warning, emit event. Background reclaim job (existing) will clean up via grace period. |
| Policy re-resolution timing | Clear `policy_version_id` immediately. Next auth will trigger policy evaluation via enforcer. No stale policy applied. |

## QG Self-Report

- [x] Min plan lines: >60 (M-sized story) — PASS
- [x] Required sections present: Goal, Architecture Context, Tasks, AC Mapping — PASS
- [x] Embedded specs (not references): API specs inline, DB schema inline, design tokens inline — PASS
- [x] Task complexity: has high-complexity tasks (T6, T14) — PASS for M story
- [x] Context refs validation: all refs point to existing plan sections — PASS
- [x] Pattern refs: all new-file tasks have pattern ref — PASS
- [x] Task granularity: max 3 files per task — PASS
- [x] Dependency ordering: DB first → store → handler �� frontend — PASS
- [x] Self-containment: no "see ARCHITECTURE.md" references — PASS
- [x] Design Token Map populated — PASS
- [x] Component Reuse table populated — PASS
- [x] Frontend tasks reference token map — PASS
