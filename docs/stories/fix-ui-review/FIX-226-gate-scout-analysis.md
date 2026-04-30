# Scout Analysis — FIX-226

Story: FIX-226 — Simulator Coverage + Volume Realism
Scope: Static code review, seed math verification, env-precedence check, PAT compliance.

## Probe Matrix

| Probe | File:Line | Result |
|-------|-----------|--------|
| Env precedence (env > YAML) | config.go:168–171 | PASS. `applyEnvOverrides()` runs AFTER `yaml.Unmarshal`, BEFORE `Validate()`. Correct order. |
| 5 new env knobs wired | config.go:196–234 | PASS. All 5 (`SESSION_RATE_PER_SEC`, `DIAMETER_ENABLED`, `SBA_ENABLED`, `INTERIM_INTERVAL_SEC`, `VIOLATION_RATE_PCT`) implemented. |
| Rate guard semantic change | config.go:318–320 | PASS. Now errors with actionable message referencing env knob. Validate() already requires it — no example configs silently depend on default. |
| `applyViolationRatePct` math | config.go:242–266 | PASS. Guards 0 ≤ pct ≤ 100 (line 243–245). Early-returns if either scenario absent (safe no-op). Delta-shifts to preserve total weight. |
| `applyViolationRatePct` negative-weight risk | config.go:262–265 | OBSERVATION. At `pct=100`, `newWeight=1.0`, `delta=0.99`, `normal_browsing.Weight = 0.69 - 0.99 = -0.30` → `Validate()` rejects at `scenarios[i].weight must be > 0` (line 304–306). Fail-safe behavior is correct (no silent clamp). Advisor concern already tracked as D-128. |
| NAS-IP missing metric — single writer (PAT-001) | client.go:167–177 | PASS. Only `setCommonNAS` increments the counter. No second writer. |
| NAS-IP missing metric — operator label | metrics.go:160–166 + client.go:171 | PASS. Label is `operator_code` via `sc.SIM.OperatorCode`. Consistent with other `operator` labels on simulator counters. |
| CoA ack latency — single writer (PAT-001) | listener.go:161–189 | PASS. Only `handleCoA` observes. `handleDM` untouched (scope honored). |
| CoA ack latency — observation timing | listener.go:167–168, 186–187 | PASS. `t0` captured at function entry; observe AFTER `writeResponse` on both NAK (168) and ACK (187) paths. Matches plan. |
| CoA ack latency histogram buckets | metrics.go:170–177 | PASS. Custom buckets `{1ms, 5ms, 10ms, 50ms, 100ms, 200ms, 500ms, 1s}` — appropriate for local UDP CoA paths (AC-8 threshold 200ms has a bucket boundary). |
| Env > YAML precedence in `INTERIM_INTERVAL_SEC` | config.go:219–225 | PASS. Only overwrites when `n > 0`; sentinel value 0 leaves YAML alone. |
| `DIAMETER_ENABLED=false` nil-map safety (R3) | config.go:204–208 | PASS. Sets `op.Diameter = nil` for every operator; engine already handles nil per plan R3. |
| Seed stagger expression math | 008_scale_sims.sql:105,120,135,150,165,180 | PASS. `NOW() - INTERVAL '1 day' * (60 - (g - 100))`. For 40-SIM groups (g=100..139): range is `60 - 0 = 60d ago` (oldest, g=100) to `60 - 39 = 21d ago` (newest, g=139). For 20-SIM TT groups (g=100..119): `60d` to `41d` ago. No future dates, no negatives, monotonic. |
| Seed stagger comment accuracy | 008_scale_sims.sql:1–11 | OBSERVATION. Header states "g=start+59 → today (newest; only 40-SIM groups reach this)". Actual: newest is `g=start+39` → 21 days ago (40-SIM groups never reach today). Documentation drift — not functionally incorrect. Minor doc fix applied below. |
| ON CONFLICT preservation | 008_scale_sims.sql:107,122,137,152,167,182 | PASS. All 6 inserts retain `ON CONFLICT (imsi, operator_id) DO NOTHING`. Idempotent. |
| aggressive_m2m policy match (R4) | 003_seed policies ↔ 008 seed APNs | OBSERVATION. Seed 008 APN IDs are `iot.<tenant>.local` / `m2m.<tenant>.local` / `private.<tenant>.local`. Seed 003 low-bandwidth policies match `apn = "m2m.meter"` / `"iot.fleet"` (exact match). 008-seeded SIMs do NOT match those APN predicates directly. HOWEVER: the broader `premium-v2` (50Mbps, matches rat_type LTE/5G) WILL be breached by 53-133 Mbps. Breach still occurs via the catchall path. Acceptable per plan R4. Follow-up D-128 tracks a tighter APN alignment. |
| Scenario weight sum | config.example.yaml:107+114+121+128 = 0.69+0.20+0.10+0.01 | PASS. Exact 1.00. |
| NAS-IP values per operator | config.example.yaml:82,93,98 | PASS. 192.0.2.10/20/30 (RFC 5737 TEST-NET-1). Distinct per operator. |
| CONFIG.md Simulator section | CONFIG.md:436–462 | PASS. Table present, placed BEFORE `## Complete .env.example`. All 5 new env vars documented + NAS-IP note. |
| Docker compose env passthrough | docker-compose.simulator.yml:26–31 | PASS. 5 new vars with `${VAR:-}` fallback (empty = unset; YAML wins). Inline comments present. |
| web/ diff | git diff --stat HEAD -- web/ | PASS. 0 changes (backend-only story, as expected). |
| Heartbeat references in simulator | grep heartbeat cmd/simulator internal/simulator | PASS. 0 matches. AC-7 satisfied. |

<SCOUT-ANALYSIS-FINDINGS>

F-A1 | LOW | scout-analysis
- Title: Seed 008 header comment states "g=start+59 → today" but actual newest is g=start+39 → 21 days ago
- Fixable: YES
- Evidence: 008_scale_sims.sql:9 vs. actual generate_series range 100..139 and 100..119
- Fix: tighten the header comment to reflect observed range (60d → 21d for 40-SIM groups; 60d → 41d for 20-SIM groups).

F-A2 | LOW | scout-analysis
- Title: applyViolationRatePct documentation drift — pct>=69 produces negative normal_browsing weight that Validate() rejects with a generic "weight must be > 0" error, no hint
- Fixable: NO (already tracked as D-128)
- Evidence: config.go:262–265
- Escalate reason: Already captured as tech debt row D-128 (aggressive_m2m weight env knob guard). Defensive clamp deferred.

F-A3 | LOW | scout-analysis
- Title: aggressive_m2m scenario's intended meter-low-v1/agri-iot-v1/nbiot-save-v1 breach path relies on policy.apn match that 008-seeded SIMs do not satisfy; breach still occurs via premium-v2 (50Mbps) catchall
- Fixable: NO (already tracked as D-128 follow-up + documented in plan R4)
- Evidence: seed 003 policy APN predicates vs. seed 008 APN IDs
- Escalate reason: APN alignment is out of M-effort scope; breach still occurs via higher-bandwidth policies.

</SCOUT-ANALYSIS-FINDINGS>
