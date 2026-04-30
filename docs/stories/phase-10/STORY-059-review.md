# STORY-059 Post-Story Review

**Story**: Security & Compliance Hardening
**Reviewer**: Reviewer Agent
**Date**: 2026-04-12
**Gate result**: PASS — 1775/1775 tests

---

## 1. Impact on Upcoming Stories

| Story | Dependency type | Impact |
|-------|----------------|--------|
| STORY-069 (Onboarding, Reporting & Notification Completeness) | Direct dependency | Notification rate limiter (`NOTIFICATION_RATE_LIMIT_PER_MINUTE`) and webhook validation framework are prerequisites for STORY-069's notification completeness work. No spec changes needed — STORY-069 can proceed as written. |
| STORY-062 (Documentation & Consistency Pass) | Tech debt target | D-004 (bruteforce.go extractIP) and D-005 (compliance API index gap) both target STORY-062. STORY-062 plan must include: (a) fix `internal/gateway/bruteforce.go:91` per PAT-002 pattern, ideally extracting shared utility to `internal/net/addr.go`; (b) add compliance API section to `docs/architecture/api/_index.md` covering STORY-039 + STORY-059 compliance endpoints. |
| All remaining Phase 10 stories | Security baseline | TOTP encryption at rest, pseudonymization unification, NATS tenant isolation, and cookie hardening are now in baseline. No downstream stories need to re-implement or work around these. |

---

## 2. Documents Updated

| Document | Change |
|----------|--------|
| `docs/architecture/CONFIG.md` | Added `NOTIFICATION_RATE_LIMIT_PER_MINUTE` to Rate Limiting table; added `notif:rl:` to Redis Key Namespaces table; updated `ENCRYPTION_KEY` description to include `totp_secret` |
| `docs/ROUTEMAP.md` | STORY-059 marked `[x] DONE` (2026-04-12); counter bumped 3/22 → 4/22; current story updated to STORY-060; D-004 and D-005 added to Tech Debt table; changelog entry added |
| `docs/stories/phase-10/STORY-059-review.md` | This file (created) |

---

## 3. Cross-Doc Consistency

| Check | Result |
|-------|--------|
| CONFIG.md — NOTIFICATION_RATE_LIMIT_PER_MINUTE | **FIXED** — variable was absent from Rate Limiting section; now added with default 60, description referencing notif:rl: namespace and DeliveryTracker. |
| CONFIG.md — notif:rl: Redis namespace | **FIXED** — prefix was absent from Redis Key Namespaces table; now added with sliding-window TTL and ZADD/ZREMRANGEBYSCORE/ZCARD method. |
| CONFIG.md — ENCRYPTION_KEY description | **FIXED** — totp_secret was missing from the list of encrypted fields; now reads "adapter_config, sm_dp_plus_config, totp_secret". |
| ARCHITECTURE.md — Go version (1.22+ vs 1.25.9) | **OK (no action)** — ARCHITECTURE.md intentionally says "Go 1.22+" as a minimum requirement; go.mod is the source of truth for the actual version. Not a contradiction. |
| decisions.md — DEV-142..147 present | **VERIFIED** — All six decisions confirmed present and ACCEPTED: DEV-142 (TOTP AES-256-GCM), DEV-143 (pseudonymization salted SHA-256), DEV-144 (NATS tenant isolation), DEV-145 (Redis notification rate limiter), DEV-146 (Go toolchain 1.25.9), DEV-147 (bcrypt cost validation ≥ 12). |
| decisions.md — PAT-001 present | **VERIFIED** — Pattern confirmed: "BR tests must be updated when ACs change business rules; grep for *_br_test.go". Gate applied this: 2 BR tests rewritten for BR-1 compliance. |
| decisions.md — PAT-002 present | **VERIFIED** — Pattern confirmed: "When fixing a utility, grep for other occurrences; long-term: extract to internal/net/addr.go". D-004 tracks the open instance in bruteforce.go. |
| api/_index.md — PDF format for BTK export | **DEFERRED as D-005** — The compliance API section is entirely absent from api/_index.md (pre-existing gap from STORY-039; STORY-059 adds PDF variant to BTK report endpoint). Too large to retroactively reconstruct in this review; deferred to STORY-062 which already targets doc consistency. |
| GLOSSARY.md — new terms | **OK (no action)** — No strictly new terms introduced. Existing entries cover: Pseudonymization, Sliding Window, Rate Limiting, WS Hub (with BroadcastAll/BroadcastToTenant). The NATS→WebSocket tenant_id extraction detail is implementation-level, not worth a new term. |
| STORY-059-plan.md — stale file reference | **NOTED (no action)** — Plan references `web/src/lib/sim-colors.ts` which doesn't exist; actual locations are `sim-utils.ts` and `dashboard/index.tsx`. Plan files are historical; no correction needed. |

---

## 4. Decision Tracing

| Decision | Present | Match |
|----------|---------|-------|
| DEV-142: TOTP AES-256-GCM via `internal/crypto/aes.go` | Yes | Yes — `internal/auth/totp_crypto.go` exists; startup migration is idempotent decrypt-probe-then-encrypt |
| DEV-143: Pseudonymization via DeriveTenantSalt() + salted SHA-256 | Yes | Yes — shared helper used by both RightToErasure and RunPurgeSweep |
| DEV-144: NATS tenant isolation via hub.relayNATSEvent extracting tenant_id | Yes | Yes — system events (nil tenant_id) → BroadcastAll; tenant events → BroadcastToTenant |
| DEV-145: Redis sliding window rate limiter, namespace `notif:rl:`, default 60/min | Yes | Yes — `internal/notification/redis_ratelimiter.go` exists; CONFIG.md now documents the variable |
| DEV-146: Go toolchain 1.25.9 (5 stdlib CVEs resolved) | Yes | Yes — go.mod updated, govulncheck passes |
| DEV-147: bcrypt cost < 12 rejected in non-development environments | Yes | Yes — validation on startup |
| PAT-001: BR test alignment on AC rule changes | Yes | Applied — gate rewrote 2 BR tests for stolen_lost→terminated transition |
| PAT-002: extractIP duplication grep-and-fix | Yes | Partially applied — AC-1 fixed anomaly detector; D-004 tracks bruteforce.go remainder |

---

## 5. USERTEST Completeness

File: `docs/USERTEST.md`, line 1144.
Entry confirmed present with 16 scenarios covering all 10 ACs:

| Scenario group | ACs covered |
|---------------|------------|
| 2FA setup (1–3) | AC-7 (TOTP encryption) |
| Compliance PDF export (4–6) | AC-8 (PDF format) |
| Webhook validation (7–10) | AC-4 (webhook HMAC-SHA256) |
| SIM stolen_lost state (11–12) | AC-6 (BR-1 transition) |
| WS tenant isolation (13–14) | AC-9 (NATS→WS scoping) |
| System events (15) | AC-9 (nil tenant → BroadcastAll) |
| vuln-check / web-audit (16) | AC-10 (security tooling) |

Turkish text quality: good. Correct terminology used throughout (pseudonimleştirme, kayan pencere, HMAC, TOTP, AES-256-GCM). No gaps.

---

## 6. Tech Debt Pickup

Pre-existing debt entries D-001, D-002, D-003 all target other stories (STORY-077, STORY-077, STORY-062 respectively). None were resolved by or blocked by STORY-059. Status unchanged.

New entries added by this review:
- **D-004** — `internal/gateway/bruteforce.go:91` extractIP last-colon split bug (pre-existing, flagged by gate as out-of-scope). Target: STORY-062.
- **D-005** — Compliance API section absent from `docs/architecture/api/_index.md`. Target: STORY-062.

---

## 7. Mock / Stub Status

No new mocks introduced by STORY-059. All implementation is real:
- Redis rate limiter: real ZADD/ZREMRANGEBYSCORE/ZCARD against Redis
- TOTP crypto: real AES-256-GCM via `internal/crypto/aes.go`
- PDF export: real go-pdf/fpdf output
- NATS isolation: real hub.relayNATSEvent with tenant_id extraction

Existing stubs/mocks from earlier phases: unaffected by this story.

---

## 8. Issues

| # | Severity | Status | Description |
|---|----------|--------|-------------|
| 1 | Medium | **FIXED** | CONFIG.md missing `NOTIFICATION_RATE_LIMIT_PER_MINUTE`, `notif:rl:` namespace, and `totp_secret` in ENCRYPTION_KEY description. All three gaps closed in this review. |
| 2 | Low | **FIXED (2026-04-12 close-out)** | `internal/gateway/bruteforce.go:91` extractIP rewritten with `net.SplitHostPort` + `net.ParseIP` pattern matching AC-1. `TestExtractIP` expanded from 3 IPv4 cases to 7 subtests including `ipv6_bracketed_with_port`, `ipv6_loopback_bracketed`, `ipv6_bare`. Zero-deferral policy honored — no longer parked as D-004. |
| 3 | Low | **FIXED (2026-04-12 close-out)** | Compliance API section added to `docs/architecture/api/_index.md` as "Compliance & Data Governance (5 endpoints)" — API-175 (dashboard), API-176 (BTK report with json/csv/pdf), API-177 (retention), API-178 (DSAR), API-179 (right to erasure). STORY-059 PDF format variant documented on API-176 entry. Zero-deferral policy honored — no longer parked as D-005. |

No escalations required. All findings resolved in-story per Phase 10 zero-deferral policy.
