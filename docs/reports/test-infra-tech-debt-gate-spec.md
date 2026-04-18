# Mini Phase Gate Spec — Test Infrastructure + Tech Debt Cleanup

> Written: 2026-04-17
> Scope: verification of AUTOPILOT run covering STORY-083, 084, 085, 087, 088
> Executed by: Phase Gate Agent (opus) after STORY-088 Step 6 post-processing
> Output artifact: `docs/reports/test-infra-tech-debt-gate.md`
> Evidence dir: `docs/e2e-evidence/test-infra/`

## Changelog

- 2026-04-18: STORY-089 Operator SoR Simulator section added (AC-1..AC-14). Runtime Alignment 3/3 complete.
- 2026-04-18: Gate F-A6 — AC-5 psql verify command hardened (`-t -A` flags + `tr`/`grep -qx` matcher) to remove dependency on default psql tuple format.

## Purpose

Standalone tracks (Test Infra + Tech Debt Cleanup) do not have a canonical Phase Gate defined in `phases/development/` — this spec is the explicit contract so the Phase Gate Agent has a deterministic pass/fail target. The scope is narrower than a dev-phase gate (no UI regression sweep, no Turkish i18n pass) because these five stories ship simulator + migration + test fixes, not user-facing features.

## Required Steps

Phase Gate Agent MUST execute ALL steps; each step writes a line to `docs/e2e-evidence/test-infra/step-log.txt` with format `STEP_<n> <name>: EXECUTED | items=... | result=PASS|FAIL`.

### Step 1 — Clean Build & Stack Up

```bash
make down && make build && make up
```

Acceptance:
- `make build` exits 0
- `make up` stabilises: all services healthy (`docker compose ps` → all `Up (healthy)` or `Up` for no-healthcheck services)
- Argus boot log contains `schema integrity check passed tables=12` (STORY-086 schemacheck invariant must still hold)
- No FATAL / ERROR lines in first 60s of `argus-app` logs

### Step 2 — Simulator Smoke

```bash
make sim-up
sleep 10
curl -s http://localhost:9099/metrics | grep -c '^simulator_'
make sim-logs | head -200
make sim-down
```

Acceptance:
- `simulator_*` metric count ≥ 100 (sanity vs STORY-082 baseline 190)
- Simulator logs contain RADIUS lifecycle (Access-Accept / session started), **plus** at least one Diameter CER/CCR line (STORY-083) **plus** at least one 5G AUSF POST (STORY-084)
- If STORY-085 enabled reactive mode in any operator: at least one CoA / Session-Timeout reaction line
- No "panic" / "fatal" in simulator output

### Step 3 — Fresh-Volume Migration (validates STORY-087 / D-032)

```bash
make down
docker volume rm argus_pg_data argus_redis_data 2>/dev/null || true
make up
docker compose exec -T argus argus migrate up
docker compose exec -T postgres psql -U argus -d argus -c "SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 3;"
docker compose exec -T postgres psql -U argus -d argus -c "SELECT to_regclass('sms_outbound');"
```

Acceptance:
- `argus migrate up` exits 0 on fresh volume (was failing pre-087 per D-032)
- `schema_migrations` latest version matches repo head, `dirty=false`
- `to_regclass('sms_outbound')` returns non-null
- `argus-app` boots without FATAL

### Step 4 — Full Go Test Suite

```bash
go test ./... 2>&1 | tee docs/e2e-evidence/test-infra/go-test.log | tail -50
go vet ./... 2>&1 | tee docs/e2e-evidence/test-infra/go-vet.log
```

Acceptance:
- All packages PASS (`FAIL` count = 0)
- PASS count ≥ **2872** (baseline from Phase 10 Gate 2026-04-17 unconditional). Record absolute count.
- `go vet ./...` exits 0 and produces zero diagnostic lines (validates STORY-088 / D-033 — `service_test.go:333` must be clean)

### Step 5 — Frontend Build

```bash
cd web && npm run build 2>&1 | tee ../docs/e2e-evidence/test-infra/web-build.log
```

Acceptance:
- `npm run build` exits 0
- Vite output contains `built in` line with module count ≥ 2800 (Phase 10 baseline ≈ 2825)
- No TypeScript errors in stdout/stderr

### Step 6 — Simulator Config Regression

```bash
docker compose exec -T argus cat /etc/argus/simulator-config.yaml 2>/dev/null || cat deploy/simulator/config.yaml
```

Acceptance:
- Diameter section present with at least one operator opt-in (STORY-083)
- 5G section present with at least one operator opt-in (STORY-084)
- Reactive section present if STORY-085 shipped a config flag

### Step 7 — Architecture Doc Drift Check

```bash
git log --oneline main..HEAD | head -20
grep -E 'STORY-08[3-8]' docs/ROUTEMAP.md | head -10
```

Acceptance:
- ROUTEMAP rows for 083/084/085/087/088 all marked `[x] DONE` with completion dates
- Tech Debt table: D-032 / D-033 marked `✓ RESOLVED`
- `docs/USERTEST.md` has sections for 083, 084, 085, 087, 088

## Pass Criteria

Gate PASS requires ALL 7 steps PASS. Any FAIL → Phase Gate FAIL, STOP autopilot, escalate to user.

## Fail Modes → Required User Decision

- **Step 1 FAIL** (build/up broken) → regression from one of 083..088; identify story via `git bisect`; re-dispatch Developer on that story
- **Step 3 FAIL** (D-032 migration still broken) → STORY-087 did not actually fix the root cause; re-open STORY-087
- **Step 4 FAIL** (test count regression) → identify failing package; re-dispatch Developer
- **Step 4 `go vet` dirty** → STORY-088 did not fix D-033 or introduced new vet findings
- **Step 5 FAIL** (frontend broken) → shouldn't happen (no FE work in 083..088); if it does, escalate

## Evidence Requirements

Phase Gate Agent MUST produce:
1. `docs/e2e-evidence/test-infra/step-log.txt` — all 7 steps with EXECUTED status
2. `docs/e2e-evidence/test-infra/go-test.log` — full go test output
3. `docs/e2e-evidence/test-infra/go-vet.log` — full go vet output
4. `docs/e2e-evidence/test-infra/web-build.log` — full vite build output
5. `docs/e2e-evidence/test-infra/docker-ps.txt` — `docker compose ps` output after Step 1 and Step 3
6. `docs/e2e-evidence/test-infra/sim-metrics.txt` — simulator Prometheus output from Step 2
7. `docs/reports/test-infra-tech-debt-gate.md` — final gate report summarising all 7 steps + PASS/FAIL + any in-gate fixes

## Post-Gate Actions

On PASS:
- Update ROUTEMAP: Test Infra track → `[DONE]`, Tech Debt Cleanup track → `[DONE]`
- Update CLAUDE.md: Mode cleared, note `Test Infra + Tech Debt Cleanup DONE <date>`
- Add Change Log entry with gate outcome
- Display AUTOPILOT summary banner
- STOP — do NOT continue into Documentation Phase

On FAIL:
- Update ROUTEMAP: mark affected story `[~] IN PROGRESS`, Step = `Gate`
- Do NOT mark track DONE
- Present failure details to user, offer `düzelt / atla / dur`

## STORY-089 — Operator SoR Simulator

### Acceptance Criteria

- AC-1: Binary compiles and container builds clean (`make operator-sim-build`)
- AC-2: Container comes up healthy via docker-compose (`docker compose ps` → `(healthy)`)
- AC-3: Per-operator health endpoints respond correctly (`GET /{operator}/health`)
- AC-4: Per-operator forward-looking stubs respond (`GET /{operator}/subscribers/:imsi`, `POST /{operator}/cdr`)
- AC-5: Seed 003 materialization — http sub-key present on 3 real operators
- AC-6: Seed 005 materialization — parallel coverage, 3 operators
- AC-7: API-307 per-protocol Test Connection succeeds for http on 3 real operators
- AC-8: `enabled_protocols` array reflects http on all real operators
- AC-9: HealthChecker reports http protocol healthy end-to-end (`argus_operator_adapter_health_status{protocol="http"}` = 1 or above)
- AC-10: D-039 closed — AUSF/UDM/NRF indexed in api/_index.md (API-308..API-312)
- AC-11: Prometheus metrics populated after 1 request (`operator_sim_requests_total` > 0)
- AC-12: All new Go code passes vet/race/coverage (coverage ≥ 70% on non-integration packages)
- AC-13: Mini Phase Gate spec extended (this file — verified by self-diff)
- AC-14: Runtime Alignment track counter advances 2/3 → 3/3

### Verification Commands

```bash
# AC-1: Binary builds
make operator-sim-build

# AC-2: Container healthy (after make up)
docker compose -f deploy/docker-compose.yml ps operator-sim | grep -q '(healthy)'

# AC-3/4: Simulator endpoints respond (from host after compose up)
for op in turkcell vodafone_tr turk_telekom; do
  curl -fsS "http://localhost:9595/${op}/health" | jq -e '.status == "ok"' >/dev/null
done

# AC-5: Seed 003 http sub-key count (robust: -t -A strips headers/alignment)
psql -t -A -c "SELECT COUNT(*) FROM operators WHERE code IN ('turkcell','vodafone_tr','turk_telekom') AND (adapter_config->'http'->>'enabled')::boolean = true;" \
  | tr -d '[:space:]' | grep -qx '3'

# AC-6: Seed 005 http sub-key count (same query on seed-005 operator IDs — run after seed 005 loaded)
# [parallel to AC-5 — see STORY-090 section for seed 005 verification pattern]

# AC-7/8/9/11: Full integration test
go test -tags=integration -run TestOperatorSimE2E ./test/e2e/

# AC-10: D-039 indexing
grep -c 'API-308\|API-309\|API-310\|API-311\|API-312' docs/architecture/api/_index.md  # must be ≥ 5
grep -E '^\| D-039 \|.*RESOLVED' docs/ROUTEMAP.md  # must match

# AC-12: Go quality
go vet ./cmd/operator-sim/... ./internal/operatorsim/...
go test -race -cover ./internal/operatorsim/... | grep -E 'coverage: [0-9]+\.[0-9]+%'  # must show ≥ 70%

# AC-13: This spec file extended (self-verify)
grep -c '^## STORY-089 —' docs/reports/test-infra-tech-debt-gate-spec.md  # must be 1

# AC-14: ROUTEMAP counter
grep -c 'Runtime Alignment.*\[DONE\]' docs/ROUTEMAP.md  # must be ≥ 1 after ship
```

### PASS Criteria

- All 14 ACs verified.
- Integration test green (AC-7/8/9/11 in a single run).
- Coverage ≥ 70% on `internal/operatorsim/...`.
- No regression on existing 3087+ test suite (STORY-090 baseline).
- `git diff docs/reports/test-infra-tech-debt-gate-spec.md` shows ONLY additions under the new STORY-089 section + a changelog line — zero deletions in any existing STORY-NNN section.
