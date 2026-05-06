# CLAUDE.md — Argus

## Project Overview

Argus is an APN & Subscriber Intelligence Platform built as a Go modular monolith with a React SPA frontend. It manages 10M+ IoT/M2M SIM cards across multiple mobile operators with built-in AAA (RADIUS/Diameter/5G), policy engine, analytics, and multi-operator orchestration.


## Development
- Run `/amil` to start or continue any development work — it manages planning, implementation, quality gates, and deployment.
## Tech Stack

- Go 1.22+ backend (single binary, multiple protocol listeners)
- React 19 + Vite + Tailwind CSS + shadcn/ui frontend
- PostgreSQL 16 + TimescaleDB (OLTP + time-series)
- Redis 7 (cache, sessions, rate limiting)
- NATS JetStream (events, job queue)
- Docker Compose deployment

## Quick Commands

- `make help` — Show all available commands
- `make up` — Start all Docker services
- `make down` — Stop all services
- `make infra-up` — Start only PG, Redis, NATS
- `make test` — Run Go tests
- `make db-migrate` — Run database migrations
- `make db-seed` — Seed database
- `make web-dev` — Start React dev server
- `make web-build` — Build React for production

## Docker Services

| Service | URL/Port | Purpose |
|---------|----------|---------|
| Nginx | http://localhost:8084 | Reverse proxy + SPA |
| Argus | :8080 (HTTP), :8081 (WS), :1812/:1813 (RADIUS), :3868 (Diameter), :8443 (5G SBA) | Go monolith |
| PostgreSQL | localhost:5432 | Database |
| Redis | localhost:6379 | Cache |
| NATS | localhost:4222, :8222 (monitor) | Events |
| Operator Sim | :9595 (API), :9596 (health+metrics) | Passive operator SoR HTTP simulator (argus-operator-sim) |
| Mailhog | :1025 (SMTP), http://localhost:8025 (Web UI) | Dev SMTP catch-all — inspect password reset emails (FIX-228 DEV-328) |

## Admin Access

- URL: http://localhost:8084/login
- Email: admin@argus.io
- Password: admin

## Project Structure

```
cmd/argus/       → Entry point (main.go)
internal/        → All Go packages
  gateway/       → SVC-01: HTTP API gateway
  ws/            → SVC-02: WebSocket server
  api/           → SVC-03: Core CRUD handlers
  aaa/           → SVC-04: RADIUS/Diameter/5G engine
  policy/        → SVC-05: Policy DSL engine
  operator/      → SVC-06: Multi-operator routing
  analytics/     → SVC-07: CDR, metrics, anomaly
  notification/  → SVC-08: Email, Telegram, webhook
  job/           → SVC-09: Background jobs
  audit/         → SVC-10: Tamper-proof logging
  model/         → Domain models
  store/         → PostgreSQL data access
  cache/         → Redis layer
  bus/           → NATS event bus
  auth/          → JWT, 2FA, API keys
  tenant/        → Multi-tenant middleware
  config/        → Env var config
web/             → React SPA
  src/components → Atomic design (atoms/molecules/organisms/templates/pages)
migrations/      → SQL up/down migrations
deploy/          → Docker Compose, Nginx, Dockerfile
docs/            → All documentation
```

## Conventions

- API responses: Standard envelope `{ status, data, meta?, error? }`
- Components: Atomic design (atoms → molecules → organisms → templates → pages)
- Migrations: `YYYYMMDDHHMMSS_description.up.sql` / `.down.sql` via golang-migrate
- Naming: Go=camelCase, React=PascalCase, routes=kebab-case, DB=snake_case
- All DB queries scoped by tenant_id (enforced in store layer)
- Cursor-based pagination (not offset) for all list endpoints
- Every state-changing operation creates an audit log entry

## Architecture References

- Services: SVC-01 to SVC-10 (see docs/architecture/services/_index.md)
- APIs: API-001 to API-182 (see docs/architecture/api/_index.md)
- Tables: TBL-01 to TBL-24 (see docs/architecture/db/_index.md)
- ADRs: ADR-001 to ADR-003 (see docs/adrs/)

## Architecture Docs

- `docs/ARCHITECTURE.md` — System design summary
- `docs/PRODUCT.md` — Features, business rules, workflows
- `docs/SCOPE.md` — Project boundaries
- `docs/GLOSSARY.md` — Domain terminology
- `docs/FUTURE.md` — Future roadmap
- `docs/FRONTEND.md` — Design system tokens & visual patterns
- `docs/SCREENS.md` — Screen index (26 screens)
- `docs/ROUTEMAP.md` — Project progress tracking
- `docs/stories/` — Individual story specs (55 stories, 9 phases)
- `docs/architecture/MIDDLEWARE.md` — Chi middleware chain
- `docs/architecture/ERROR_CODES.md` — Error code catalog
- `docs/architecture/DSL_GRAMMAR.md` — Policy DSL grammar (EBNF)
- `docs/architecture/PROTOCOLS.md` — RADIUS/Diameter/5G protocol details
- `docs/architecture/ALGORITHMS.md` — Key algorithms (IP, hash chain, rate limit, anomaly, cost)
- `docs/architecture/WEBSOCKET_EVENTS.md` — WebSocket event schemas + canonical bus.Envelope wire format (FIX-212)
- `docs/architecture/TESTING.md` — Test strategy & frameworks
- `docs/architecture/CONFIG.md` — Environment variable reference
- `docs/architecture/DEPLOYMENT.md` — Container restart policy + recovery runbook

## Active Session

- Mode: E2E_POLISH (Production Cutover Re-run — user directive 2026-05-05)
- Phase: E2E & Polish (Production Cutover Re-run) [IN PROGRESS] — started 2026-05-06; post-Phase-11; full E0..E5 sequence
- Story: — (E-step agents are project-wide, not story-scoped)
- Step: E4 UI Polisher (next dispatch)
- E3 closed: [DONE 2026-05-06] — 0 CRITICAL/0 HIGH; detail-screen p95 4-92ms (SLO 500ms, 5-100x margin); SIM 4-92ms / Operator 5-11ms / APN 3-4ms / Session 13-26ms; index scan on all hot paths; infra Phase-10-tuned (TimescaleDB hypertable indexes, pgx pool QueryExecModeExec 50 conns + slow-query tracer, pgbouncer transaction pooling 6432, Redis name-cache 5-min TTL, nginx epoll/gzip/keepalive 64, Vite 213KB gz initial + manualChunks lazy charts/codemirror); 5 findings deferred D-199..D-203 (all scale-dependent / non-blocking); commit pending close-out
- E2 closed: [DONE 2026-05-06] — D-181 systemic refactor complete; 5 store files hardened (cdr/policy/ippool/session_radius/notification — 19 inline scan drift surfaces eliminated); 10 new drift-guard tests + bonus rows.Err() check; 4222→4235 tests pass; race-detection sweep clean; Phase 11 hot-path coverage policy/binding 91.2% syslog 84.7% validator 100%; commits 5d09d72 + c71427d
- E1 closed: [DONE 2026-05-06] — initial run + 1 fix loop; 64 screenshots in polish-2026-05-06/; 23 routes 0 placeholder; 6 detail screens × 30+ tabs covered; Pass 4 functional 62/62 (100%) post-fix-loop; 5 live-bug fixes routed (commits 58e607b sim-store PAT-006 #4 + drift-guard, 4d05851 oracle drift, 9fc2f5f session protocol_type DTO, plus pre-existing 1d69278/18737e5/90d662d/edb54b2 from E0)
- E0 closed: [DONE 2026-05-06] — 2 loops; 12 screens initial + 10 demo fixtures (3 SIM + 2 Op + 2 APN + 3 Sess) with 62 oracle assertion targets; 38 screenshots; make db-seed idempotent 4×; hash chain verified 28439 entries
- Phase 11 closed: [DONE 2026-05-05] — 6/6 stories + Phase Gate PASS (10/10 steps; 4222 Go tests + tsc clean; 12 screenshots; 21/21 functional; 100% compliance)
- Last closed: STORY-098 [x] DONE 2026-05-05 (Phase 11 W6 S — Native Syslog Forwarder RFC 3164/5424; commit 55e044d; 48 files +9143/-17; Plan→Dev (5 waves, 7 tasks)→Lint→Gate→Review→Commit→Postproc full pipeline; Gate 14 findings (1H+3M+10L); 9 fixed + D-197 deferred + 4 VAL (VAL-076..078 + D-195); Review 7 findings (4 cross-doc + 2 GLOSSARY + 1 API-index) all FIXED; AC 16/16 PASS; PAT-026 RECURRENCE [F-A1] worker-emit 5th audit action filed; D-195/D-196/D-197 routed; VAL-068..078; USERTEST 12 Turkish scenarios; siblings: docs(uat) commit bc14056 — 13 retest screenshots separated per advisor)
- Last closed: STORY-097 [x] DONE 2026-05-04 (Phase 11 W5 M — IMEI Change Detection & Re-pair Workflow; 8 tasks/5 waves; commit 70ff149; 76 files +4990/-302; 14 binding regression+severity tests + 4 cross-task integration + 10 grace scanner + 7 RePair + 12 store + 6 FE smoke; Gate 13 findings (1C+4H+4M+4L); F-A1 CRITICAL imei.changed wire-contract FIXED + F-A2 HIGH 8-layer event catalog sweep PAT-026 RECURRENCE pre-merge mitigation + F-A3 HIGH FE BindingMode union 7 canonical modes + F-A4/A5/A6/U1/U5/U6/U7 fixed; F-U2/U3 → D-194 NEW deferred (allowlist sub-table + Force Re-verify); F-U4 → VAL-067 deferred (re-pair reason radio); D-183 ✓ RESOLVED + D-188 ✓ RESOLVED + D-193+D-194 NEW; VAL-059..067; PAT-026 RECURRENCE filed; 4129 tests PASS; tsc + vite build PASS)
- Last closed: STORY-095 [x] DONE 2026-05-03 (Phase 11 W3 M — IMEI Pool Management; commit c46fc34; 57 files +7897/-24; 13 Gate findings (1C+4H+4M+4L); D-187/D-189 routed; PAT-026 RECURRENCE filed)
- Last closed: STORY-093 [x] DONE 2026-05-01 (Phase 11 W1 L — IMEI Capture across RADIUS+Diameter S6a+5G SBA; commit 42b70c5; 42 files +2299/-31; 21 new tests + 3 microbenches; Gate F-A1+F-A2+F-A4+F-A6 all FIXED; D-182/183/184 deferred — D-182 since closed in 094)
- Last closed: FIX-247 [x] DONE 2026-04-30 (Wave 10 P2 S — Remove Admin Global Sessions UI; F-320 closed; 9/9 ACs PASS; UI-only removal pattern — page/hook/route/sidebar deleted, BE handler+routes+store retained per AC-5; DEV-580; PAT-026 RECURRENCE [FIX-247] — limited sweep documented exception; D-180 routed for dormant handler cleanup; tsc PASS, vite build 2.65s, go build/vet/test PASS; combined Gate+Review S-story dispatch)
- Wave 10 P2 6/6 COMPLETE (FIX-240 + FIX-246 + FIX-235 + FIX-245 + FIX-238 + FIX-247)
- Earlier: FIX-238 [x] DONE 2026-04-30 (Wave 10 P2 L — Remove Roaming Feature full stack; F-229 closed; 10/10 ACs PASS; full BE/FE/DB/DSL/test/doc sweep; AC-10 boot-time keyword archiver + idempotent audit; DEV-579; PAT-026 RECURRENCE; GLOSSARY 7 entries updated; 4 USERTEST scenarios added; 3803 Go tests PASS; tsc 0; 0 deferred D-NNN)
- Earlier: FIX-245 [x] DONE 2026-04-30 (Wave 10 P2 L — Remove 5 Admin Sub-pages + Kill Switches→env; PAT-026 NEW; DEV-575..578)
- Earlier: FIX-235 [x] DONE 2026-04-27 (Wave 10 P2 XL — M2M eSIM Provisioning Pipeline; commit 124ff00; PAT-025; D-172..D-179)
- Earlier: FIX-246 [x] DONE 2026-04-27 (Wave 10 P2 M — Quotas+Resources merge; commit 6e57b81; D-170/D-171; PAT-024)
- Earlier: FIX-240 [x] DONE 2026-04-27 (Wave 10 P2 M — Unified Settings Page + Tabbed Reorganization; commit c543ed7)
- Earlier: FIX-248 [x] DONE 2026-04-27 (Wave 9 P1 XL — Reports Subsystem Refactor; commit 4663b03; D-165..D-167)
- Wave 9 P1 COMPLETE (5/5 — FIX-243, FIX-244, FIX-239, FIX-236, FIX-248)
- Earlier: FIX-236 [x] DONE 2026-04-27 (10M Scale Readiness; commit 0d91ce7; D-162..D-164), FIX-239 [x] DONE 2026-04-27 (KB Ops Runbook; commit d1ed95d; D-160/D-161), FIX-244 [x] DONE 2026-04-27 (Violations Lifecycle UI; commit 2f4ccbd; D-157..D-159), FIX-243 [x] DONE 2026-04-27 (Policy DSL Realtime Validate)
- Prior closure: FIX-237 [x] DONE 2026-04-27 (Wave 8 P0 last — M2M Event Taxonomy + Notification Redesign; 3-tier classification, digest worker, env-gated migration, NATS retention 168h, FE Preferences tier filter, 12 USERTEST scenarios; DEV-501..509; D-150..D-156 routed; commit 8c5553c, 43 files +4120/-142)
- Prior closures: FIX-242 [x] DONE 2026-04-26 (Wave 8 P0 Session Detail DTO populate; DEV-398..407; D-147 + D-148 deferred), FIX-241 [x] DONE 2026-04-26 (global WriteList nil-slice; DEV-394..397), FIX-253 [x] DONE 2026-04-26 (DEV-390..393), FIX-251 [x] DONE 2026-04-26 (DEV-389), FIX-252 [x] DONE 2026-04-26 (DEV-386..388)
- Prior closures: FIX-253 [x] DONE 2026-04-26 (Suspend atomic IP release + Activate 422 + audit; DEV-390..393), FIX-251 [x] DONE 2026-04-26 (PAT-006 RECURRENCE #3; DEV-389), FIX-252 [x] DONE 2026-04-26 (zero-code PAT-023 schema drift; DEV-386..388)
- Plan: `docs/reviews/ui-review-remediation-plan.md` (44 FIX stories, FIX-201..FIX-248)
- Findings: `docs/reviews/ui-review-2026-04-19.md` (107 aktif finding + Phase 2 additions)
- ROUTEMAP: `docs/ROUTEMAP.md` "UI Review Remediation [IN PROGRESS]" track (10 waves)
- User directive 2026-04-20: Full AUTOPILOT, dikkatli geliştirme, doğru spec, hatasız, canlıya hazırlık
- Modal decision: Option C (Dialog compact confirm + SlidePanel rich form)
- AUTOPILOT scope: Runs until ESCALATED / FAILED / end-of-track Phase Gate PASS
- Key architectural threads:
  - **Data integrity foundation** (FIX-206): 200 orphan SIM + FK constraints + seed fix → unblocks FIX-202/207/208
  - **Alert architecture** (FIX-209/210/211): unified alerts table + dedup + taxonomy → unblocks FIX-213/215/229
  - **Event envelope** (FIX-212): unified schema + name resolution + missing publishers → unblocks FIX-213/219
  - **Cross-tab aggregation** (FIX-208): DONE — single source of truth for usage/cost/sessions math via aggregates facade
  - **Seed discipline** (FIX-206): `make db-seed` must stay clean after FK migration — never defer
