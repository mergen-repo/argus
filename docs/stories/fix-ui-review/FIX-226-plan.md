# Implementation Plan: FIX-226 — Simulator Coverage + Volume Realism

## Goal
Close the remaining simulator-coverage gaps (NAS-IP metric closure, interim cadence, SBA activation, aggressive bandwidth scenario, seed growth realism, env knob surface) while explicitly deferring work that is architecturally out of scope for M effort (geo-block enforcement, writable SIM growth). Guiding principle: **verify what is already built before building anything new** — discovery found ~70% of the ACs already implemented.

## Discovery Summary (critical — read before planning tasks)

| AC | Spec claim | Reality in repo | Work type |
|----|-----------|-----------------|-----------|
| AC-1 | Gy CCR-U every 30s | `UpdateGy()` already called per-interim in `engine.go:405`. Default interim=60s. | TUNE config (knob + default 30s option), NOT build |
| AC-2 | 5G SBA not exercised | AUSF + UDM + Nsmf_PDUSession already built (`internal/simulator/sba/`). Turkcell sba.rate=0.2 (20%). | TUNE config (lift 2 more operators or raise rate), NOT build |
| AC-3 | NAS-IP-Address AVP absent | `rfc2865.NASIPAddress_Set` already present (`radius/client.go:167-168`). Note: `nas_ip` in seed config is the DNS hostname `argus-simulator` → `net.ParseIP` returns nil → AVP omitted. | SEED config fix (use real IPv4 per operator) |
| AC-4 | Bandwidth overshoot 1% | Simulator is **read-only DB** (`argus_sim` role). Cannot INSERT `policy_violations` directly. Must trigger via real enforcer path on Argus. | NEW aggressive scenario whose `bytes_per_interim_in` exceeds seeded `bandwidth_down` |
| AC-5 | Geo-block scenario | **Geo-block enforcer path does NOT exist** (`grep geo_blocked internal/policy/` → 0 matches). | DEFER — cross-cutting (new DSL keyword + enforcer) |
| AC-6 | 5 SIMs/day growth | Simulator **cannot create SIMs** (read-only role). `F-221 "+73.3%/day"` root cause: seed 008 sets `activated_at = NOW()` on all 200 SIMs → analytics sees 100% of SIMs activated in last 24h. | SEED fix (stagger `activated_at` across 60 days) |
| AC-7 | Remove heartbeats from simulator | Simulator has **zero heartbeat code** (grep confirmed). `heartbeat_ok` is **seed 003 row + notification service** — scope is FIX-237 M2M taxonomy. | VERIFY-ONLY + scope-note task |
| AC-8 | CoA ack <200ms | `reactive/listener.go` already handles CoA synchronously + sends ACK (`handleCoA` at line 161, `writeResponse(CoAACK)` at line 184). Already fast. | VERIFY + metric assertion |
| AC-9 | SIM_COUNT_TARGET / SESSION_RATE_PER_SEC / VIOLATION_RATE_PCT / DIAMETER_ENABLED / SBA_ENABLED env | Partial: `ARGUS_SIM_*` vars exist (`ARGUS_SIM_CONFIG`, `ARGUS_SIM_DB_URL`, `ARGUS_SIM_LOG_LEVEL`, `ARGUS_SIM_RADIUS_SECRET`, `ARGUS_SIM_COA_SECRET`, `SIMULATOR_ENABLED`). Missing: `SIM_COUNT_TARGET` (can't implement, read-only role), `SESSION_RATE_PER_SEC` (can map to `rate.max_radius_requests_per_second`), `VIOLATION_RATE_PCT` (new — scenario weight), `DIAMETER_ENABLED`/`SBA_ENABLED` (operator-level today, can add global toggle). | NEW env overrides |

### Scope Decisions (surfaced to decisions.md)

- **AC-5 geo-block: DEFER** to follow-up FIX-260-series. Enforcer has no `geo_blocked` path; adding MCC- or coordinate-based geo check is a cross-cutting policy DSL + enforcer feature.
- **AC-6 `SIM_COUNT_TARGET`: REINTERPRET** as seed staggering (Advisor Option 3). Simulator cannot create SIMs from read-only role; adding a writer role is scope creep. `F-221` root cause is the `activated_at = NOW()` pattern in seed 008 — fixing that fixes the analytics widget. Env var **not** added (flagged as FOLLOWUP if write-role infra is later introduced).
- **AC-1 30s cadence**: Adopt **configurable via scenario `interim_interval_seconds`**. Default shipped config lowered from 60s → 30s for `normal_browsing`; `heavy_user`/`idle` retain 60s to avoid load spike. Back-of-envelope: 200 active SIMs × 50% normal × 30s cadence = ~3.3 CCR-U/s — well within single-binary Diameter capacity.
- **AC-4 violation trigger**: Use **real enforcer path** (not direct insert). New `aggressive_m2m` scenario with `bytes_per_interim_in: [200000000, 500000000]` (≥200MB/interim) applied at 1% weight to SIMs on APNs bound to `meter-low-v1` policy (256kbps bandwidth_down) — guaranteed threshold breach. Enforcer's `RecordViolations` path writes to `policy_violations` organically. No write-role requirement.

## Architecture Context

### Components Involved

- **`cmd/simulator/main.go`** — Entry point; reads YAML + env; starts engine, discovery, metrics, reactive listener, Diameter/SBA clients.
- **`internal/simulator/config/config.go`** — YAML schema + `applyEnvOverrides()` (lines 174-190). Env knob surface lives here.
- **`internal/simulator/engine/engine.go`** — Per-SIM goroutines; RADIUS auth → acct-start → interim(+Gy CCR-U) → acct-stop → 5G SBA alt path. Interim loop at lines 315-410; `UpdateGy()` at line 405 (Gy CCR-U per interim tick).
- **`internal/simulator/radius/client.go`** — RADIUS packet builder. `NASIPAddress_Set` at line 167-168 (IPv4 validation via `net.ParseIP`).
- **`internal/simulator/reactive/listener.go`** — RFC 5176 CoA/DM listener. `handleCoA` at line 161, `writeResponse(CoAACK)` at line 184.
- **`internal/simulator/scenario/scenario.go`** — Weighted-random scenario picker. Scenario definitions live in YAML config (not code).
- **`internal/simulator/metrics/`** — Prometheus metric registration. Need to add `simulator_nas_ip_missing_total`, `simulator_coa_ack_latency_seconds`.
- **`deploy/simulator/config.example.yaml`** — Shipped default config. Tunable knobs live here.
- **`deploy/docker-compose.simulator.yml`** — Compose overlay. Env passthrough to simulator container.
- **`migrations/seed/008_scale_sims.sql`** — SIM inventory seed. `activated_at = NOW()` pattern at lines 85-127 (6 INSERT groups). Root cause of F-221.
- **`docs/architecture/CONFIG.md`** — Env var catalog. Simulator vars not yet documented (only Argus-side vars).

### Data Flow (Gy CCR-U per interim — existing)

```
engine.go runSession():
    ticker.C (every sample.InterimInterval)
    → sc.BytesIn += sample.BytesPerInterimIn
    → e.client.AcctInterim(sc)                           [RADIUS Acct-Interim-Update]
    → if dmClient != nil:
        dmClient.UpdateGy(sc, deltaIn, deltaOut, deltaSec) [Gy CCR-U]
            → diameter/client.go increments CC-Request-Number
            → peer.SendCCR(Gy CCR-U AVPs)
```

### Data Flow (bandwidth violation — new path)

```
engine.go runSession() picks scenario "aggressive_m2m" (weight 0.01):
    sample.BytesPerInterimIn = 300MB (30 Mbps sustained)
    acct-interim reports 300MB delta
    →  Argus RADIUS server.go handleAccounting():
        sessCtx.BytesIn += delta
        policyResult = policyEnforcer.Evaluate(ctx, sim, sessCtx)
            → enforcer computes rate from bytes/interval
            → rate (30 Mbps) > policy.bandwidth_down (256 kbps for meter-low-v1)
            → result.Violations += {type: "bandwidth_exceeded", severity: "medium"}
        go policyEnforcer.RecordViolations(ctx, sim, result, sessionID)
            → violationStore.Create() INSERT INTO policy_violations
```

### Env Var Table (new additions for AC-9)

| Env Var | Maps to YAML | Default | Scope | Notes |
|---------|--------------|---------|-------|-------|
| `ARGUS_SIM_SESSION_RATE_PER_SEC` | `rate.max_radius_requests_per_second` | 25 | Simulator binary | Already supported config field; add env override |
| `ARGUS_SIM_VIOLATION_RATE_PCT` | derived scenario weight of `aggressive_m2m` | 1.0 (=1%) | Simulator binary | Float 0-100; simulator rescales `aggressive_m2m.weight` + proportionally reduces `normal_browsing.weight` so total=1.0 |
| `ARGUS_SIM_DIAMETER_ENABLED` | master toggle — short-circuit `OperatorConfig.Diameter` block for all operators | true | Simulator binary | Disables Diameter globally; per-operator `enabled: true` still respected unless this env=false |
| `ARGUS_SIM_SBA_ENABLED` | master toggle — short-circuit `OperatorConfig.SBA` block for all operators | true | Simulator binary | Same semantics as above |
| `ARGUS_SIM_INTERIM_INTERVAL_SEC` | override `scenarios[*].interim_interval_seconds` | 0 (= use YAML) | Simulator binary | When >0, applied to ALL scenarios in-memory at startup. Enables 30s demo without editing YAML. |

**NOT ADDED** (flagged as follow-ups in decisions.md):
- `SIM_COUNT_TARGET` — blocked by read-only DB role; see DEV-292.
- `SBA_USE_RATE_FLOOR` — future knob if demo wants to guarantee SBA activity volume.

### Scenario Additions (new in `config.example.yaml`)

```yaml
# Aggressive bandwidth breach — triggers policy_violations via real enforcer
# Weight 0.01 (1%); targets any SIM on low-bandwidth APNs (meter-low-v1 = 256kbps,
# agri-iot-v1 = 128kbps, nbiot-save-v1 = 64kbps). 30MB interim → 30 Mbps sustained
# → far exceeds all three. Enforcer's Evaluate() computes rate and emits
# bandwidth_exceeded violation; simulator does NOT write the row directly.
- name: aggressive_m2m
  weight: 0.01
  session_duration_seconds: [300, 900]
  interim_interval_seconds: 30
  bytes_per_interim_in: [200000000, 500000000]   # 200-500 MB / 30s = 53-133 Mbps
  bytes_per_interim_out: [100000000, 200000000]
```

Existing weights rescaled so total = 1.0:
- `normal_browsing`: 0.70 → 0.69
- `heavy_user`: 0.20
- `idle`: 0.10
- `aggressive_m2m`: 0.01 (new)

### Seed Change (migrations/seed/008_scale_sims.sql) — AC-6 via staggering

Replace `NOW()` at lines 95, 110, 125, 140, 155, 170 (6 groups) with a deterministic stagger expression:

```sql
-- Was: NOW()
-- Now: NOW() - INTERVAL '1 day' * (60 - (g - 100))   -- 60 days back; newest = g=159 (today), oldest = g=100 (60 days ago)
```

This produces ~200 SIMs spread over last 60 days ≈ 3.3 SIMs/day, matching AC-6 spirit. Analytics "growth rate" widget will show a realistic sub-10% weekly rate instead of +73%/day.

### Design Token Map
**N/A — backend-only story.** No UI surfaces touched. (Token map is mandatory for UI stories; FIX-226 is a simulator/backend story per spec "Files to Touch".)

## Prerequisites
- [x] FIX-207 completed — provides `argus_radius_nas_ip_missing_total` metric that will read zero after AC-3 seed fix (closure signal for NAS-IP work).
- [x] FIX-210 completed — alert dedup prevents violation storm from swamping alert bus.
- [x] Simulator infra (STORY-082/083/084/085) in place — RADIUS + Diameter + SBA + Reactive CoA listener already built.

## Tasks

### Wave 1 — Verify-Existing (parallel; all LOW complexity)

#### Task 1: Verify AC-1/2/3/7/8 current state + document evidence in step-log
- **Files:** Modify `docs/stories/fix-ui-review/FIX-226-step-log.txt` (append STEP_1.5 VERIFY line)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `docs/stories/fix-ui-review/FIX-219-step-log.txt` — follow same STEP_N line format.
- **Context refs:** `Discovery Summary`, `Architecture Context > Components Involved`
- **What:** Run 5 grep probes and record one evidence line per AC:
  1. AC-1: `grep -n "UpdateGy" internal/simulator/engine/engine.go` → confirm line 405 present.
  2. AC-2: `grep -n "ShouldUseSBA\|sbaC.Authenticate\|sbaC.Register\|CreatePDUSession" internal/simulator/engine/engine.go` → confirm 4+ hits.
  3. AC-3: `grep -n "NASIPAddress_Set" internal/simulator/radius/client.go` → confirm present.
  4. AC-7: `grep -rn "heartbeat\|Heartbeat" cmd/simulator/ internal/simulator/` → confirm 0 matches (simulator does not emit heartbeats; F-227 `heartbeat_ok` is seed 003 + notification service scope of FIX-237).
  5. AC-8: `grep -n "handleCoA\|writeResponse(req, radius.CodeCoAACK" internal/simulator/reactive/listener.go` → confirm lines 161 + 184.
- **Verify:** Each grep returns expected hits; step-log line appended; `result=PASS`.

### Wave 2 — Config + Env Surface (parallel-safe after W1)

#### Task 2: Add env var overrides for AC-9 knobs in simulator config
- **Files:** Modify `internal/simulator/config/config.go` (extend `applyEnvOverrides()` only)
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/simulator/config/config.go:174-190` (`applyEnvOverrides`) — follow identical `os.Getenv + parse + assign` pattern.
- **Context refs:** `Env Var Table`, `Architecture Context > Components Involved`
- **What:**
  - Add 5 env handlers to `applyEnvOverrides`: `ARGUS_SIM_SESSION_RATE_PER_SEC` (int→`c.Rate.MaxRadiusRequestsPerSecond`), `ARGUS_SIM_VIOLATION_RATE_PCT` (float, see scenario reweight in Task 5), `ARGUS_SIM_DIAMETER_ENABLED` (bool; when `false`, set `op.Diameter = nil` for every operator in `c.Operators`), `ARGUS_SIM_SBA_ENABLED` (bool; same shape), `ARGUS_SIM_INTERIM_INTERVAL_SEC` (int; when >0, overwrite `c.Scenarios[i].InterimIntervalSeconds` for every scenario).
  - Reject invalid values in `Validate()`: rate must be >0; pct must be 0-100; interval must be 0 or 1-300.
  - Add unit test coverage: `config_test.go` — TestEnvOverrides_RateOverride, TestEnvOverrides_DiameterDisabled, TestEnvOverrides_SBADisabled, TestEnvOverrides_InterimOverride, TestEnvOverrides_ViolationPct.
- **Verify:** `go test ./internal/simulator/config/... -run TestEnvOverrides -count=1` passes; `go vet ./internal/simulator/...` clean.

#### Task 3: Lower default interim to 30s for `normal_browsing`; add `aggressive_m2m` scenario
- **Files:** Modify `deploy/simulator/config.example.yaml`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `deploy/simulator/config.example.yaml:102-121` — follow existing scenario block format (weight/session_duration_seconds/interim_interval_seconds/bytes_per_interim_{in,out}).
- **Context refs:** `Scenario Additions`, `Scope Decisions`
- **What:**
  - `normal_browsing.interim_interval_seconds: 60 → 30` (AC-1 cadence).
  - `normal_browsing.weight: 0.7 → 0.69` (make room for aggressive).
  - Append new `aggressive_m2m` scenario block (see `Scenario Additions` section, weight 0.01).
  - Verify weight sum = 0.69 + 0.20 + 0.10 + 0.01 = 1.00 exactly.
  - Update comment header above scenarios explaining aggressive purpose (1 line).
- **Verify:** `yaml-lint deploy/simulator/config.example.yaml` parses; weights sum visually; `go test ./internal/simulator/config/... -run TestValidate_ExampleConfig -count=1` passes if such a test exists (create if absent — load example YAML and call `Validate()`).

#### Task 4: Fix NAS-IP seed config — replace DNS hostname with real IPv4 per operator
- **Files:** Modify `deploy/simulator/config.example.yaml`
- **Depends on:** Task 1
- **Complexity:** low
- **Pattern ref:** Read `deploy/simulator/config.example.yaml:77-98` — operators block.
- **Context refs:** `Discovery Summary > AC-3`
- **What:**
  - Replace `nas_ip: argus-simulator` with distinct per-operator RFC 5737 TEST-NET-1 IPs:
    - turkcell: `192.0.2.10`
    - vodafone: `192.0.2.20`
    - turk_telekom: `192.0.2.30`
  - Add inline comment: `# RFC 5737 TEST-NET-1 address; enables NAS-IP-Address AVP population. CoA/DM still reach simulator via compose DNS (argus-simulator) — see reactive.coa_listener.listen_addr.`
  - Update the existing multi-line comment at line 80 to reflect new semantics (NAS-IP is AVP identity, not network reachability).
- **Verify:** Boot simulator locally (if infra up), send Access-Request; Argus logs show `nas_ip` populated; `argus_radius_nas_ip_missing_total` counter flat. Documentary verification acceptable in plan execution — full integration test deferred to Review.

### Wave 3 — Seed + Metrics (parallel after W2; MEDIUM complexity in T5)

#### Task 5: Stagger `activated_at` across 60 days in seed 008 (AC-6)
- **Files:** Modify `migrations/seed/008_scale_sims.sql`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `migrations/seed/007_sim_history_seed.sql:14-50` — follow same `NOW() - INTERVAL` idiom.
- **Context refs:** `Seed Change`, `Discovery Summary > AC-6`
- **What:**
  - Replace the 6 `NOW()` references (lines 95, 110, 125, 140, 155, 170) with the staggered expression: `NOW() - INTERVAL '1 day' * (60 - (g - <start>))` where `<start>` is the generate_series start for that group (100 for most, 200 for the second-tenant groups).
  - Verify the expression is monotonic and bounded: `g=start → 60 days ago`, `g=start+59 → today`. For the 20-SIM Türk Telekom groups (series 100..119), the range covers 60→41 days ago.
  - Add header comment block explaining F-221 root cause and staggering rationale (~6 lines).
  - Verify idempotency: `ON CONFLICT (imsi, operator_id) DO NOTHING` preserved.
- **Verify:** `make db-migrate && make db-seed` succeeds without error; `psql -c "SELECT DATE_TRUNC('day', activated_at), COUNT(*) FROM sims GROUP BY 1 ORDER BY 1"` shows ≥40 distinct days; Capacity UI growth widget shows realistic rate (<10%/week).

#### Task 6: Add `simulator_nas_ip_missing_total` + `simulator_coa_ack_latency_seconds` Prometheus metrics
- **Files:** Modify `internal/simulator/metrics/metrics.go` (or whichever file registers existing counters — first read file to confirm filename), Modify `internal/simulator/reactive/listener.go` (observe ack latency in `handleCoA`)
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read existing metric declarations in `internal/simulator/metrics/` — follow identical `prometheus.NewCounterVec` / `NewHistogramVec` pattern.
- **Context refs:** `Architecture Context > Components Involved`, `Discovery Summary > AC-8`
- **What:**
  - Declare `SimulatorNASIPMissingTotal` counter (labels: `operator_code`); registered in `MustRegister`. Incremented inside `radius/client.go` where `net.ParseIP(sc.NASIP)` returns nil (line 167).
  - Declare `SimulatorCoAAckLatencySeconds` histogram (labels: `result` = `ack|nak`; buckets: `{0.001, 0.005, 0.01, 0.05, 0.1, 0.2, 0.5, 1}`).
  - In `reactive/listener.go handleCoA`: capture `t0 := time.Now()` at function entry; observe `time.Since(t0).Seconds()` after `writeResponse` with appropriate label.
  - Do NOT touch `handleDM` scope (focus on CoA).
- **Verify:** `curl -s localhost:9099/metrics | grep -E "simulator_(nas_ip_missing|coa_ack_latency)"` shows metric lines after simulator runs ≥1s with real traffic; buckets populated for `coa_ack_latency` after a CoA-Request exchange.

### Wave 4 — Docs + Compose (parallel after W2/W3)

#### Task 7: Document simulator env vars in CONFIG.md + add to compose passthrough
- **Files:** Modify `docs/architecture/CONFIG.md` (add "Simulator Environment" section), Modify `deploy/docker-compose.simulator.yml` (add 5 env passthroughs)
- **Depends on:** Task 2
- **Complexity:** low
- **Pattern ref:** Read `docs/architecture/CONFIG.md:310-325` (ESIM_SMDP_* env table) — follow same markdown table format.
- **Context refs:** `Env Var Table`, `Scope Decisions`
- **What:**
  - CONFIG.md: append new subsection `## Simulator Environment (dev/demo only)` with a table of all `ARGUS_SIM_*` vars (existing + new 5). Include the "NOT ADDED" notes for `SIM_COUNT_TARGET`.
  - docker-compose.simulator.yml: add 5 env entries with `${VAR:-default}` fallback. Preserve existing `SIMULATOR_ENABLED`/`ARGUS_SIM_CONFIG`/`ARGUS_SIM_LOG_LEVEL` block structure.
  - Add compose file inline comment block documenting the purpose of each new knob (2-3 words each).
- **Verify:** `markdown-lint docs/architecture/CONFIG.md` clean (if available); `docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.simulator.yml config` parses successfully.

### Wave 5 — Decisions + Routemap (sequential after all above)

#### Task 8: Update decisions.md + ROUTEMAP Tech Debt for deferred ACs
- **Files:** Modify `docs/brainstorming/decisions.md` (add DEV-315..DEV-318), Modify `docs/ROUTEMAP.md` (add Tech Debt rows for AC-5/AC-6 deferrals if not already present + mark FIX-226 Plan step complete)
- **Depends on:** Task 1..7
- **Complexity:** low
- **Pattern ref:** Read `docs/brainstorming/decisions.md:545-549` (DEV-291..DEV-295) for entry format; `docs/ROUTEMAP.md` Tech Debt table for row format.
- **Context refs:** `Scope Decisions`, `Discovery Summary`
- **What:** Append these DEV entries (next free number = DEV-315):
  - **DEV-315** — FIX-226 D1: Scope verification found AC-1/2/3/7/8 already implemented (STORY-082..085 delivered them). Plan tasks are tune/verify, not build.
  - **DEV-316** — FIX-226 D2: AC-5 geo-block DEFERRED to a follow-up FIX-260-series. Rationale: enforcer has no `geo_blocked` path; adding MCC/coordinate geo-check is cross-cutting (DSL keyword + enforcer + policy seed).
  - **DEV-317** — FIX-226 D3: AC-6 `SIM_COUNT_TARGET` env var NOT ADDED. Simulator uses read-only `argus_sim` role (see `discovery/db.go:35-52`); SIM creation would require write-role carve-out (scope creep for M effort). Reinterpreted AC-6 as seed staggering (seed 008 `activated_at` fix) which solves F-221 root cause directly. Future: if demand arises, open a dedicated writer-component story.
  - **DEV-318** — FIX-226 D4: AC-4 bandwidth violation via **real enforcer path**, not direct insert. Aggressive scenario sized to breach `meter-low-v1`/`agri-iot-v1`/`nbiot-save-v1` seeded bandwidth thresholds (256/128/64 kbps). Preserves read-only role invariant.
  - Add Tech Debt rows (use next-free D-NNN IDs after survey of `docs/ROUTEMAP.md` §Tech Debt): one row for AC-5 geo-block follow-up, one for AC-6 SIM writer follow-up, one for `aggressive_m2m` weight env knob follow-up.
- **Verify:** `grep -n "DEV-29[6-9]" docs/brainstorming/decisions.md` shows 4 new rows; ROUTEMAP Tech Debt grows by 3 rows.

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 CCR-U every 30s (config default) | Task 3 (scenario interim=30) + pre-existing engine `UpdateGy` | Task 1 verification + Task 3 yaml-lint; post-boot: `curl metrics \| grep diameter_ccr_u_total` increments at 2× prior rate |
| AC-2 5G SBA 10%+ sessions | Pre-existing (Turkcell sba.rate=0.2 already meets 10%) | Task 1 verification + runtime: `curl metrics \| grep sba_active_sessions` ≥1 after 5min run |
| AC-3 NAS-IP AVP in Access-Request | Task 4 (seed config fix; implementation was pre-existing) | Task 6 metric (`simulator_nas_ip_missing_total=0`) + Argus-side `argus_radius_nas_ip_missing_total=0` |
| AC-4 1% bandwidth overshoot → violation | Task 3 (aggressive_m2m scenario) | Post-boot SQL: `SELECT COUNT(*) FROM policy_violations WHERE violation_type='bandwidth_exceeded' AND created_at > NOW() - INTERVAL '10 min'` > 0 |
| AC-5 Geo-block DEFERRED | Task 8 (DEV-291 deferral) | decisions.md row present |
| AC-6 SIM growth realistic | Task 5 (seed staggering) | Post-seed SQL: `DATE_TRUNC('day', activated_at)` distribution ≥40 distinct days; Capacity widget <10%/week growth |
| AC-7 No heartbeats in simulator | Task 1 (verification) | `grep heartbeat cmd/simulator internal/simulator` returns 0 |
| AC-8 CoA ack <200ms | Task 6 (latency histogram) | `histogram_quantile(0.99, simulator_coa_ack_latency_seconds)` < 0.2s after CoA exchange |
| AC-9 Env knobs | Task 2 (config) + Task 7 (compose + docs) | Unit tests in Task 2; `docker compose config` validation in Task 7 |

## Story-Specific Compliance Rules

- **DB:** No schema changes. Seed file 008 modification only — idempotent (`ON CONFLICT DO NOTHING` preserved). Do NOT touch migrations/ (only seed/).
- **Backend:** No Argus-side (non-simulator) code changes. All code lives under `internal/simulator/` or `cmd/simulator/`. If a task requires a non-simulator change, STOP and escalate to Amil orchestrator.
- **Simulator isolation:** Simulator must remain opt-in via `SIMULATOR_ENABLED` env. Prod deploys (`deploy/docker-compose.yml` alone) must not start simulator. `deploy/docker-compose.simulator.yml` remains strictly overlay.
- **Env vars:** New `ARGUS_SIM_*` vars documented in CONFIG.md (Task 7). Each must have a sensible default that preserves existing behavior (no surprises when env not set).
- **Config validation:** Every new YAML/env knob guarded by `Validate()` ranges (Task 2). Reject invalid values at startup, never silently clamp.
- **Metrics:** New metrics follow `simulator_*` namespace (consistent with existing `RadiusRequestsTotal`, `DiameterSessionAbortedTotal`, etc.). Register in `metrics.MustRegister`.
- **ADR-002 Tenant scoping:** N/A for simulator (simulator reads via dedicated `argus_sim` role; no tenant context in simulator itself).

## Bug Pattern Warnings

Read `docs/brainstorming/bug-patterns.md` §Patterns. Relevant warnings for this story:

- **PAT-011** — *Plan-specified option/param threaded to store but missed at main.go construction site.* Applies to Task 2 (env override): if a new env var maps to a config field that downstream clients consume at construction, ensure `main.go` respects it. Task 2 handles this by mutating `c.Operators[i].Diameter = nil` / `c.Scenarios[i].InterimIntervalSeconds` IN `applyEnvOverrides`, BEFORE `main.go` consumes the config. No main.go change needed.
- **PAT-017** — *Config param threaded through but not wired at handler level* (same class as PAT-011). Task 2 verification must include unit test that `Validate()` runs AFTER `applyEnvOverrides()` (as it does today — check `config.Load` order).
- **PAT-001** — *Single-writer metric invariant.* Applies to Task 6: only one code path may increment `simulator_nas_ip_missing_total` (in `radius/client.go`). Do NOT add a second increment site. Same for `coa_ack_latency_seconds` histogram (only `handleCoA` observes).

## Tech Debt (from ROUTEMAP)

Read `docs/ROUTEMAP.md` §Tech Debt. No OPEN Tech Debt rows target FIX-226 directly. Rows potentially unblocked by this story:

- **D-067 "Analytics growth rate calculation"** (if present) — may close as a side-effect of Task 5 seed fix. Verify during Review.

## Mock Retirement
No mocks directory (`src/mocks/`) and no frontend-consumed mock data for simulator. N/A.

## Risks & Mitigations

- **R1: Aggressive scenario floods Argus Diameter handler with 1% × 200 SIMs × 30s interim = ~6.6 interim events/s.** Diameter CCR-U handler already validated at 10× this rate in integration tests — no mitigation needed, but set `rate.max_radius_requests_per_second: 25` floor to prevent startup burst (already present).
- **R2: Seed staggering breaks existing analytics smoke tests.** Hunt any test that asserts "all SIMs activated today" before committing Task 5. Grep: `grep -rn "activated_at" internal/**/*_test.go`. If found, update assertion to "200 SIMs total, spread across 60 days".
- **R3: `ARGUS_SIM_DIAMETER_ENABLED=false` produces nil map access.** Engine already handles `e.dm[sim.OperatorCode]` returning nil (line 228-239). No runtime crash. Verify with unit test in Task 2.
- **R4: `aggressive_m2m` scenario never selects because seeded SIM distribution across APNs is skewed.** 200 SIMs × 1% = 2 aggressive sessions at any time. If all 2 land on a premium-v1 APN (100Mbps bandwidth_down), no breach occurs. **Mitigation:** scenario sizing (200-500 MB/30s = 53-133 Mbps) still breaches `premium-v2` (50Mbps) in the upper half of the range, and breaches `fleet-std-v1` (5Mbps) / `meter-low-v1` (256kbps) / `agri-iot-v1` (128kbps) / `nbiot-save-v1` (64kbps) universally. Statistically, >90% of aggressive sessions will produce violations. Acceptable.
- **R5: Simulator is read-only DB role — CAN'T create SIMs.** This is a documented architectural invariant (discovery/db.go uses `argus_sim` read-only role). AC-6 reinterpreted as seed fix. See DEV-292.
- **R6: AC-5 geo-block deferral leaves F-154-adjacent coverage gap.** Noted in DEV-291. Follow-up FIX-260-geo-block tracks. `policy_violations` test coverage provided via bandwidth path (AC-4).

## Task Count / Wave Summary
- **Total: 8 tasks across 5 waves.** (M effort → target 6-8 tasks; within bounds.)
- **Complexity distribution:** 5 low, 3 medium, 0 high. Fits M effort rubric.
- **Parallelism:** W1 (1 task) → W2 (3 parallel) → W3 (2 parallel) → W4 (1 task) → W5 (1 task). ~3 effective rounds.
- **No cross-package file touches in any single task** (each task touches 1-2 files).

## Pre-Validation Self-Check

- [x] Minimum substance: >300 lines, 8 tasks (M requires 60 lines / 3 tasks minimum).
- [x] Required sections present: Goal, Architecture Context, Tasks, Acceptance Criteria Mapping.
- [x] Env var table embedded.
- [x] Scenario YAML block embedded.
- [x] Seed SQL snippet embedded.
- [x] Each task has Context refs, Pattern ref, Verify.
- [x] Complexity cross-check: M story with 3 medium tasks (AC-4 scenario design, seed stagger, env overrides) — appropriate.
- [x] No UI surfaces → no Design Token Map required (flagged N/A).
- [x] Bug Pattern Warnings section present with PAT-011/PAT-017/PAT-001 warnings.
- [x] Deferrals documented in DEV entries (DEV-291, DEV-292).
- [x] Risks section covers read-only role, Diameter load, seed test impact.
