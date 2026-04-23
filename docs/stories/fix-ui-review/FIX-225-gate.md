# Gate Report: FIX-225 — Docker Restart Policy + Infra Stability

## Summary
- Requirements Tracing: AC-1/AC-2/AC-3/AC-4 implemented+verified; AC-5 DEFERRED (DEV-314) with documented rationale.
- Gap Analysis: 4/5 ACs PASS; 1/5 explicitly deferred with audit trail.
- Compliance: COMPLIANT (restart policy uniform, service_healthy rule enforced, docs cross-linked, decisions logged).
- Tests: `go build ./...` PASS, `cd web && npx tsc --noEmit` PASS, `docker compose config --quiet` PASS (exit 0).
- Build: PASS.
- Screen Mockup Compliance: n/a (not a UI story).
- UI Quality: n/a.
- Token Enforcement: n/a.
- Turkish Text: n/a.
- Overall: **PASS**.

## Team Composition
- Analysis Scout: 14 checks, 0 findings (`docs/stories/fix-ui-review/FIX-225-gate-scout-analysis.md`)
- Test/Build Scout: 7 checks, 0 findings (`docs/stories/fix-ui-review/FIX-225-gate-scout-testbuild.md`)
- UI Scout: 6 checks, 0 findings (`docs/stories/fix-ui-review/FIX-225-gate-scout-ui.md`) — reduced scope, infra story
- De-duplicated: 0 → 0 findings

## Fixes Applied
None. All scout checks PASS; no remediation required.

## Escalated Issues
None.

## Deferred Items
| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|--------------|---------------------|
| — | AC-5 crash-loop detection (Compose v3 limitation — needs external watchdog) | Future ops story (DEV-314) | PRE-EXISTING — tracked via DEV-314 in `docs/brainstorming/decisions.md`; documented in `docs/architecture/DEPLOYMENT.md` §Limitations. No new ROUTEMAP row created by Gate (pre-existing deferral from plan). |

Note: This Gate introduces NO new Tech Debt rows. The AC-5 deferral was locked during PLAN (DEV-314); the decisions.md entry + DEPLOYMENT.md Limitations section constitute the audit trail.

## Verification
- Compose syntax: `docker compose -f deploy/docker-compose.yml config --quiet` → exit 0.
- Merged compose inspection: NATS healthcheck present (`wget -qO- http://localhost:8222/healthz`), argus depends_on all 4 services `service_healthy, required: true`.
- `grep service_started deploy/docker-compose.yml` → 0 matches.
- `go build ./...` → PASS.
- `cd web && npx tsc --noEmit` → PASS.
- `grep "DEPLOYMENT.md" CLAUDE.md` → 1 match (L113).
- `grep "DEV-31[234]" docs/brainstorming/decisions.md` → 3 matches (L566/567/568).
- `grep "^##" docs/architecture/DEPLOYMENT.md | wc -l` → 13 (≥7 required).
- `grep "TODO\|FIXME" docs/architecture/DEPLOYMENT.md` → 0 matches.
- `git diff --stat -- web/` → empty for FIX-225 scope.
- Fix iterations: 0 (no fixes needed).

## Passed Items
- **AC-1** — All 7 services have `restart: unless-stopped` (nginx, argus, postgres, redis, nats, operator-sim, pgbouncer). Verified in DEPLOYMENT.md Service Restart & Health Matrix.
- **AC-2** — argus hard deps (postgres, redis, nats, operator-sim) all use `condition: service_healthy`. NATS leg closed here (was `service_started`).
- **AC-3** — Nginx healthcheck probes `http://localhost/health` (wget); compose-level implementation. Coupling note documented (probe proxies to argus).
- **AC-4** — `docs/architecture/DEPLOYMENT.md` created with 13 sections: Purpose, Service Restart & Health Matrix, Restart Policy Rationale, Dependency Ordering, Recovery Runbook (5 playbooks), Limitations, References. Cross-linked from CLAUDE.md.
- **AC-5** — DEFERRED per DEV-314, rationale: Compose v3 `restart: on-failure:N` loses host-reboot auto-start; proper crash-loop detection needs external watchdog. Documented in DEPLOYMENT.md §Limitations.
- **Audit trail** — DEV-312/313/314 all appended to `docs/brainstorming/decisions.md` with date 2026-04-23 and FIX-225 tag.
- **Backward compat** — `make up`/`make down`/`make infra-up` unchanged; no image rebuild; no API/DB surface change.
</content>
</invoke>