# Gate Scout — Test/Build (FIX-225)

Story: **FIX-225 — Docker Restart Policy + Infra Stability**
Scope: no Go/TS code changes — regression baseline only.

## Checks Performed

| # | Check | Command | Result |
|---|-------|---------|--------|
| B-1 | Compose syntax valid | `docker compose -f deploy/docker-compose.yml config --quiet` | PASS (exit=0) |
| B-2 | Merged compose shows NATS healthcheck wired correctly | `docker compose -f deploy/docker-compose.yml config` | PASS — test=`wget -qO- http://localhost:8222/healthz || exit 1`, interval=5s, timeout=3s, retries=5, start_period=10s |
| B-3 | Merged compose shows argus→nats `service_healthy` | grep merged output | PASS — all 4 deps `service_healthy, required: true` |
| B-4 | `go build ./...` still PASS | `go build ./...` | PASS (no Go changes expected; build success confirms no indirect breakage) |
| B-5 | TypeScript type-check still PASS | `cd web && npx tsc --noEmit` | PASS (no TS changes; confirms no indirect breakage) |
| B-6 | `go vet ./...` impact | n/a | SKIPPED — no Go source touched |
| B-7 | Docker live `up` test (optional) | SKIPPED per scout guidance — optional and Docker state in current env not guaranteed; compose `config --quiet` is the primary gate |

## Regression Surface

- Only `deploy/docker-compose.yml` touched for runtime impact — no service image, env var, port, or network change.
- NATS healthcheck is new but additive (does not alter NATS command or config).
- `argus.depends_on.nats.condition` change from `service_started` → `service_healthy` is stricter (waits longer on cold boot) but has no behavioral effect once NATS reports healthy. Start-period 10s + retries 5×5s = 35s ceiling; NATS typically ready <3s in alpine image.
- No migration, no seed, no API contract change.

## Findings

<SCOUT-TESTBUILD-FINDINGS>
No findings. All build/regression checks PASS.
</SCOUT-TESTBUILD-FINDINGS>
