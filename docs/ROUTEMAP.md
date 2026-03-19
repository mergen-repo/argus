# Project Roadmap: Argus

> Last updated: 2026-03-20
> Current phase: DEVELOPMENT — Phase 1: Foundation
> Overall progress: 2%

---

## Planning Phase

| Step | Name | Status | Completed |
|------|------|--------|-----------|
| 1 | Discovery (Brainstormer) | [x] DONE | 2026-03-18 |
| 2 | Gap Analysis (Gap Analyst) | [x] DONE | 2026-03-18 |
| 3 | Product Definition (Product Analyst) | [x] DONE | 2026-03-18 |
| 4 | Feature Discovery (Feature Researcher) | [x] DONE | 2026-03-18 |
| 5 | Architecture (Architect) | [x] DONE | 2026-03-18 |
| 6 | Screen Design (Screen Designer) | [x] DONE | 2026-03-18 |
| 6.5 | Theme & Visual Design (Theme Designer) | [x] DONE | 2026-03-18 |
| 7 | Story Writing (Story Writer) | [x] DONE | 2026-03-18 |
| 8 | Final Review (Reviewer) | [x] DONE | 2026-03-18 |
| 9 | Development Readiness Audit | [x] DONE | 2026-03-18 |

---

## Development Phase [IN PROGRESS]

> Stories completed: 1/55 (2%)
> Current story: STORY-002
> Current step: Runner

### Phase 1: Foundation [IN PROGRESS]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-001 | Project Scaffold & Docker Infrastructure | M | [x] DONE | — | — | 2026-03-20 |
| STORY-002 | Core Database Schema & Migrations | L | [~] IN PROGRESS | Runner | STORY-001 | — |
| STORY-003 | Authentication — JWT + Refresh + 2FA | M | [ ] PENDING | — | STORY-002 | — |
| STORY-004 | RBAC Middleware & Permission Enforcement | M | [ ] PENDING | — | STORY-003 | — |
| STORY-005 | Tenant Management & User CRUD | M | [ ] PENDING | — | STORY-004 | — |
| STORY-006 | Structured Logging, Config & NATS Event Bus | M | [ ] PENDING | — | STORY-001 | — |
| STORY-007 | Audit Log Service — Tamper-Proof Hash Chain | L | [ ] PENDING | — | STORY-006 | — |
| STORY-008 | API Key Management & Rate Limiting | M | [ ] PENDING | — | STORY-004, STORY-006 | — |

### Phase 2: Core SIM & APN [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-009 | Operator CRUD & Health Check | L | [ ] PENDING | — | STORY-005 | — |
| STORY-010 | APN CRUD & IP Pool Management | L | [ ] PENDING | — | STORY-009 | — |
| STORY-011 | SIM CRUD & State Machine | XL | [ ] PENDING | — | STORY-010 | — |
| STORY-012 | SIM Segments & Group-First UX | M | [ ] PENDING | — | STORY-011 | — |
| STORY-013 | Bulk SIM Import (CSV) | L | [ ] PENDING | — | STORY-011, STORY-006 | — |
| STORY-014 | MSISDN Number Pool Management | S | [ ] PENDING | — | STORY-011 | — |

### Phase 3: AAA Engine [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-018 | Pluggable Operator Adapter + Mock Simulator | L | [ ] PENDING | — | STORY-009 | — |
| STORY-015 | RADIUS Authentication & Accounting Server | XL | [ ] PENDING | — | STORY-011, STORY-018 | — |
| STORY-016 | EAP-SIM/AKA/AKA' Authentication | L | [ ] PENDING | — | STORY-015 | — |
| STORY-017 | Session Management & Force Disconnect | L | [ ] PENDING | — | STORY-015 | — |
| STORY-019 | Diameter Protocol Server (Gx/Gy) | XL | [ ] PENDING | — | STORY-015 | — |
| STORY-020 | 5G SBA HTTP/2 Proxy (AUSF/UDM) | L | [ ] PENDING | — | STORY-019 | — |
| STORY-021 | Operator Failover & Circuit Breaker | L | [ ] PENDING | — | STORY-018 | — |

### Phase 4: Policy & Orchestration [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-022 | Policy DSL Parser & Evaluator | XL | [ ] PENDING | — | STORY-006 | — |
| STORY-023 | Policy CRUD & Versioning | M | [ ] PENDING | — | STORY-022 | — |
| STORY-024 | Policy Dry-Run Simulation | L | [ ] PENDING | — | STORY-023, STORY-011 | — |
| STORY-025 | Policy Staged Rollout (Canary) | XL | [ ] PENDING | — | STORY-024, STORY-017 | — |
| STORY-026 | Steering of Roaming Engine | L | [ ] PENDING | — | STORY-018 | — |
| STORY-027 | RAT-Type Awareness (All Layers) | M | [ ] PENDING | — | STORY-015, STORY-022 | — |

### Phase 5: eSIM & Advanced Ops [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-031 | Background Job Runner & Dashboard | L | [ ] PENDING | — | STORY-006, STORY-013 | — |
| STORY-028 | eSIM Profile Management & SM-DP+ | L | [ ] PENDING | — | STORY-011 | — |
| STORY-029 | OTA SIM Management (APDU) | M | [ ] PENDING | — | STORY-011, STORY-031 | — |
| STORY-030 | Bulk State Change / Policy / Operator Switch | L | [ ] PENDING | — | STORY-012, STORY-028, STORY-031 | — |

### Phase 6: Analytics & BI [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-032 | CDR Processing & Rating Engine | L | [ ] PENDING | — | STORY-015 | — |
| STORY-033 | Real-Time Metrics & Observability | M | [ ] PENDING | — | STORY-006, STORY-015 | — |
| STORY-034 | Usage Analytics Dashboard | M | [ ] PENDING | — | STORY-032 | — |
| STORY-035 | Cost Analytics & Optimization | M | [ ] PENDING | — | STORY-032 | — |
| STORY-036 | Anomaly Detection Engine | L | [ ] PENDING | — | STORY-032, STORY-017 | — |
| STORY-037 | Connectivity Diagnostics | M | [ ] PENDING | — | STORY-015, STORY-011 | — |

### Phase 7: Notifications & Compliance [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-038 | Notification Engine (Multi-Channel) | L | [ ] PENDING | — | STORY-006, STORY-005 | — |
| STORY-039 | Compliance Reporting & Auto-Purge | M | [ ] PENDING | — | STORY-007, STORY-011 | — |
| STORY-040 | WebSocket Event Server | L | [ ] PENDING | — | STORY-006 | — |

### Phase 8: Frontend Portal [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-041 | React Scaffold & Routing | L | [ ] PENDING | — | STORY-001 | — |
| STORY-042 | Frontend: Auth (Login + 2FA) | M | [ ] PENDING | — | STORY-041, STORY-003 | — |
| STORY-043 | Frontend: Main Dashboard | L | [ ] PENDING | — | STORY-042, STORY-040 | — |
| STORY-044 | Frontend: SIM List + Detail | XL | [ ] PENDING | — | STORY-043, STORY-011 | — |
| STORY-045 | Frontend: APN + Operator Pages | M | [ ] PENDING | — | STORY-043, STORY-009 | — |
| STORY-046 | Frontend: Policy DSL Editor | XL | [ ] PENDING | — | STORY-043, STORY-022 | — |
| STORY-047 | Frontend: Sessions + Jobs + Audit | L | [ ] PENDING | — | STORY-043, STORY-040 | — |
| STORY-048 | Frontend: Analytics Pages | L | [ ] PENDING | — | STORY-043, STORY-032 | — |
| STORY-049 | Frontend: Settings Pages | M | [ ] PENDING | — | STORY-043, STORY-005 | — |
| STORY-050 | Frontend: Onboarding + Notifications | M | [ ] PENDING | — | STORY-043, STORY-038 | — |

### Phase 9: Integration & Polish [PENDING]

| # | Story | Effort | Status | Step | Dependencies | Completed |
|---|-------|--------|--------|------|-------------|-----------|
| STORY-051 | E2E Auth → SIM → Policy Flow Test | L | [ ] PENDING | — | Phase 8 | — |
| STORY-052 | AAA Performance Tuning & Benchmarks | L | [ ] PENDING | — | STORY-015, STORY-017 | — |
| STORY-053 | Data Volume Optimization & Archival | M | [ ] PENDING | — | STORY-032 | — |
| STORY-054 | Security Hardening (TLS, CSP, Audit) | M | [ ] PENDING | — | Phase 8 | — |
| STORY-055 | Tenant Onboarding E2E Wizard | M | [ ] PENDING | — | STORY-050 | — |

---

## E2E & Polish Phase [NOT STARTED]

| Step | Name | Status | Completed |
|------|------|--------|-----------|
| E1 | E2E Browser Testing (E2E Tester) | [ ] PENDING | — |
| E2 | Test Hardening (Test Hardener) | [ ] PENDING | — |
| E3 | Performance Optimization (Perf Optimizer) | [ ] PENDING | — |
| E4 | UI Polish (UI Polisher) | [ ] PENDING | — |

---

## Documentation Phase [NOT STARTED]

| Step | Name | Status | Completed |
|------|------|--------|-----------|
| D1 | Specification | [ ] PENDING | — |
| D2 | Presentations (Sales + Technical) | [ ] PENDING | — |
| D3 | Rollout Guide | [ ] PENDING | — |
| D4 | User Guide | [ ] PENDING | — |

---

## Change Log

| Date | Type | Description | Affected |
|------|------|-------------|----------|
| 2026-03-18 | INIT | Project initialized — Argus RADIUS/APN Management Platform | — |

---

## Status Legend
- `[ ] PENDING` — Not started
- `[~] IN PROGRESS` — Currently being worked on
- `[x] DONE` — Completed and verified
- `[!] NEEDS_REPLAN` — Affected by change, needs re-planning
- `[!!] BLOCKED_BY_CHANGE` — Cannot proceed until change is applied
- `[S] SKIPPED` — User kararıyla atlandı (autopilot escalation)
- Effort: S (Small) | M (Medium) | L (Large) | XL (Extra Large)

## Step Values
- `—` — Not started
- `Plan` — Implementation planning
- `Dev` — Developer implementing
- `Gate` — Combined Gate (Gap + Compliance + Tests + Perf + Build)
- `Commit` — Close & Commit
- `Review` — Reviewer checking (after every story)
- `Handoff` — Session handoff
- `Runner` — Story Runner subprocess'te çalışıyor (AUTOPILOT)
- `Escalated` — Story Runner escalate etti, user bekleniyor
- `Failed` — Story Runner failed
- `E1` — E2E Browser Testing
- `E2` — Test Hardening
- `E3` — Performance Optimization
- `E4` — UI Polish
- `D1` — Specification document
- `D2` — Presentations (Sales + Technical)
- `D3` — Rollout Guide
- `D4` — User Guide
