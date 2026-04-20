# Post-Story Review: FIX-207 — Session/CDR Data Integrity

> Date: 2026-04-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-226 | Simulator Coverage + Volume Realism — AC for NAS-IP injection on simulator side. FIX-207 AC-7 explicitly declares `argus_radius_nas_ip_missing_total` as the closure signal for FIX-226: once the simulator sends NAS-IP-Address AVP, the counter must drop to 0 for new acct sessions. Counter is now live and verified. FIX-226 cannot declare NAS-IP AC closed without checking this counter. | UPDATED (dependency note added to ROUTEMAP FIX-226 row) |
| FIX-208 | Cross-Tab Data Aggregation — no impact. FIX-207 CHECK constraints and IMSI validator do not touch aggregation math or query paths. | NO_CHANGE |
| FIX-209 | Unified alerts table — no impact. FIX-207 DataIntegrityDetector uses metric+log surface, not the alerts table. D-070 tracks future notification-store wiring. | NO_CHANGE |
| FIX-231 | Policy Version State Machine — no impact. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| docs/USERTEST.md | FIX-207 section added — 4 scenarios: (1) IMSI reject 400 INVALID_IMSI_FORMAT via REST and RADIUS, (2) CHECK constraint psql probe rejecting ended_at < started_at and duration_sec < 0, (3) daily DataIntegrityDetector trigger + metric assertion for all 4 invariant kinds, (4) NAS-IP missing counter probe via radclient acct packet without NAS-IP-Address AVP. | UPDATED |
| docs/architecture/db/_index.md | TBL-52 row added for session_quarantine: plain (non-hypertable) quarantine store, original_table CHECK, FIX-207 AC-6 reference. | UPDATED |
| docs/brainstorming/bug-patterns.md | PAT-011 added: "Plan-specified ManagerOption/builder wiring missing at main.go construction sites" — lesson from F-A1 CRITICAL finding. Root cause, prevention steps (grep all NewXxx call sites, smoke/build-time nil guard, Gate checklist requirement), and coverage pointers documented. | UPDATED |
| docs/ARCHITECTURE.md | DataIntegrityDetector bullet added under Backup Infrastructure section (SVC-09 job scheduler): daily cron, 4 invariants, quarantine/metric surface, argus_data_integrity_violations_total counter, FIX-207 AC-5 reference. | UPDATED |
| docs/ROUTEMAP.md | FIX-207 status flipped `[~] IN PROGRESS (Review)` → `[x] DONE (2026-04-20)`; FIX-226 dependency column unchanged (`—` — FIX-207 provides a verification signal, not a hard dependency); Change Log REVIEW row added (2026-04-20). | UPDATED |
| CLAUDE.md | Active Session Story advanced from FIX-207 → FIX-208; Step: Plan. | UPDATED |
| docs/brainstorming/decisions.md | DEV-268 already present from developer/gate: "FIX-207 AC-1: Option B (source predicate) for sessions duration invariant". Accuracy verified — rationale matches story Implementation Notes and migration file. | NO_CHANGE |
| docs/architecture/ERROR_CODES.md | INVALID_IMSI_FORMAT row at line 80 present and correct: code, HTTP 400, AC-4 reference, details block at lines 83–93. Format matches INVALID_REFERENCE sibling row. Go constants ledger entry at line 311 confirmed. | NO_CHANGE |
| docs/ROUTEMAP.md Tech Debt | D-067 (Migration B prod cutover runbook), D-069 (sims-quarantine surface), D-070 (notification-store wiring), D-071 (NAS-IP DB-gated E2E) — all four confirmed present with accurate descriptions. D-068 intentionally NOT in ROUTEMAP (conditional on benchmark threshold — per Gate decision). | NO_CHANGE |
| docs/architecture/api/_index.md | No new API endpoints added in FIX-207 (backend validation story). No API index changes needed. | NO_CHANGE |
| docs/architecture/services/_index.md | DataIntegrityDetector is a job-runner component, not a new service. SVC-09 entry unchanged. | NO_CHANGE |
| docs/PRODUCT.md | No product narrative changes needed. IMSI validation, session quarantine, and daily scan are backend data-quality mechanisms; no customer-facing feature added. | NO_CHANGE |
| docs/GLOSSARY.md | session_quarantine, DataIntegrityDetector, IMSI format validation — no new domain terms require glossary entries; all are self-describing technical identifiers. | NO_CHANGE |

## Cross-Doc Consistency

- Contradictions found: 0
- db/_index.md TBL-17 (sessions, hypertable) — CHECK constraint added via FIX-207 Migration B. Index entry describes partitioning correctly; constraint presence is implementation detail, not index scope. No contradiction.
- ARCHITECTURE.md job/ section previously listed only BackupProcessor; DataIntegrityDetector addition is additive, no conflict.
- ERROR_CODES.md: INVALID_IMSI_FORMAT code, HTTP status, and Go constant all consistent with apierr.go (line 39) and validator/imsi.go usage. PASS.
- config/config.go IMSI_STRICT_VALIDATION env var — documented in CONFIG.md? Out of reviewer scope (config.md check not in 14-check protocol); flagged as informational only.

## Decision Tracing

- Decisions checked: DEV-268
- DEV-268 (FIX-207 AC-1: Option B — source predicate `ended_at IS NULL OR ended_at >= started_at` instead of denormalized `duration_sec`): verified: migration `20260421000002_session_cdr_invariants.up.sql` uses the source-predicate form; story Implementation Notes confirm rationale (derived quantity, avoids sync burden, TimescaleDB hypertable compatible). PASS.
- AC-3 "log + audit + continue" policy decision: captured in story Implementation Notes. No DEV entry required (operational policy, not architectural pivot). PASS.
- Orphaned decisions: 0

## USERTEST Completeness

- Entry exists after this review: YES — `docs/USERTEST.md` after FIX-206 section (appended at line 2951)
- Type: Backend/DB + metric story — 4 manual verification scenarios (bash curl + psql + radclient + metrics scrape)
- Scenarios cover: AC-4 IMSI reject (REST + RADIUS), AC-1/AC-2 CHECK constraint psql probe (rejection + expected SQLSTATE), AC-5 daily scan job trigger + metric assertions for all 4 kinds, AC-7 NAS-IP missing counter probe
- AC-3 (framed_ip pool validation): policy is log+continue, not reject — no HTTP-level test scenario appropriate. Covered by unit tests; USERTEST scenario omitted by design.
- AC-6 (session_quarantine): covered implicitly in AC-1/2 scenario (quarantine table populated by retro Migration A). Direct quarantine-row count probe not added — considered dev-DB specific and not reliably reproducible in user-test context.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 pre-existing D-items targeted FIX-207 specifically.
- New D-items created by this story: 5 (D-067, D-068 conditional, D-069, D-070, D-071).
  - D-067 (Migration B plain-CHECK prod cutover runbook): OPEN — correctly deferred. ACCESS EXCLUSIVE lock on hypertable requires per-chunk partition strategy for prod. Mirrors D-065 precedent from FIX-206.
  - D-068 (framed_ip hot-path cache): INTENTIONALLY NOT in ROUTEMAP — conditional on benchmark result. Reviewer confirms this is correct: D-068 should only open if post-dev benchmark shows >2ms added latency per session-create.
  - D-069 (IMSI violations can't be quarantined — session_quarantine.original_table CHECK excludes 'sims'): OPEN — correct. IMSI malformed detection emits log + metric only; quarantine requires table enum expansion in a future story.
  - D-070 (DataIntegrityDetector per-tenant notification-store wiring): OPEN — correct. Currently only metric surface; notification integration is future.
  - D-071 (NAS-IP DB-gated E2E persistence test): OPEN — correct. Current tests are helper-level only (`TestExtractNASIPFromPacket_*`). Full RADIUS → sessionStore persistence path needs DATABASE_URL-gated test.
- Items resolved this story: 0 pre-existing D-items.

## Mock Status

N/A — backend-only story. No frontend mocks involved. `src/mocks/` not applicable.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | USERTEST.md missing FIX-207 section. All prior FIX-20x reviews added a USERTEST section — same pattern required here. | NON-BLOCKING | FIXED | Added FIX-207 section with 4 scenarios covering AC-4/1/2/5/7. |
| 2 | db/_index.md TBL-52 (session_quarantine) missing. New table added by Migration A; not indexed in architecture doc. | NON-BLOCKING | FIXED | Added TBL-52 row with table description, domain, relationships, and FIX-207 AC-6 reference. |
| 3 | bug-patterns.md missing PAT-011 for "plan-specified ManagerOption wiring missed at main.go construction sites". F-A1 was the sole CRITICAL Gate finding — the pattern is novel and not subsumed by PAT-006 (which covers struct literal field omission, not functional-option wiring). | NON-BLOCKING | FIXED | Added PAT-011 with root cause, prevention steps (grep all construction sites, nil-guard, Gate checklist), and behavioral coverage pointer. |
| 4 | ARCHITECTURE.md DataIntegrityDetector unmentioned despite being a new SVC-09 job registered in main.go cron. BackupProcessor already documented at same level of specificity. | NON-BLOCKING | FIXED | Added DataIntegrityDetector bullet under Backup Infrastructure section with cron schedule, invariant list, and metric name. |
| 5 | FIX-226 NAS-IP closure signal not documented. Story spec states `argus_radius_nas_ip_missing_total` is the signal FIX-226 uses to verify its NAS-IP fix. | NON-BLOCKING | DOCUMENTED | Captured in Story Impact table above. FIX-226 Dependencies column left as `—` (counter is a verification signal, not a hard dependency blocker). |
| 6 | CLAUDE.md Active Session still points at FIX-207/Step:Plan — must advance to next story. | NON-BLOCKING | FIXED | Advanced to Story: FIX-208, Step: Plan. |
| 7 | D-068 (framed_ip cache) not in ROUTEMAP. Gate correctly deferred it as conditional. | INFORMATIONAL | ACCEPTED | D-068 should NOT be in ROUTEMAP until benchmark result is available. Gate decision confirmed correct. No action needed. |

## Project Health

- FIX-207 Gate: PASS (all CRITICAL + HIGH findings fixed; 3381 tests PASS; build + vet + race all PASS).
- Story AC coverage: 7/7 ACs addressed (AC-3 framed_ip wired at all 3 main.go call sites post-Gate F-A1 fix).
- New tech debt: 4 tracked D-items (D-067, D-069, D-070, D-071) + 1 conditional (D-068). All correctly captured.
- Production readiness notes: D-067 (ACCESS EXCLUSIVE hypertable CHECK add) is a pre-prod blocker for Migration B on 10M+ session tables — same class as D-065 from FIX-206. Must be addressed before production deploy.
- Next story: FIX-208 (Cross-Tab Data Aggregation Unify) — unblocked by FIX-206 + FIX-207 data integrity foundation.
