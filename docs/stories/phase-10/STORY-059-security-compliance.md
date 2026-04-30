# STORY-059: Security & Compliance Hardening

## User Story
As a security officer and compliance auditor, I want every deferred security item closed — encrypted TOTP secrets, multi-tenant NATS isolation, salted pseudonymization everywhere, PDF compliance exports, webhook input validation — so that Argus passes production security review with no open deferrals.

## Description
Close all security and compliance findings from reviews (STORY-036 IPv6, STORY-037 tenant filter, STORY-038 webhook/rate-limiter, STORY-039 pseudonymization, STORY-040 NATS tenant isolation, STORY-003 TOTP plaintext) plus the items that were explicitly deferred in the original DEFER list. Zero-deferral policy per production gate requirements.

## Architecture Reference
- Services: SVC-01 (Gateway), SVC-02 (WebSocket), SVC-03 (Core API), SVC-07 (Analytics — anomaly), SVC-08 (Notification), SVC-10 (Audit — compliance)
- Packages: internal/auth, internal/analytics/anomaly, internal/store/session, internal/store/compliance, internal/notification, internal/bus, internal/api/compliance, web/src/lib/sim-colors
- Source: docs/stories/phase-3/STORY-003-review.md, phase-6/STORY-036-review.md, phase-6/STORY-037-review.md, phase-7/STORY-038-review.md, phase-7/STORY-039-review.md, phase-7/STORY-040-review.md, docs/reports/ui-polisher-report.md

## Screen Reference
- SCR-015 (2FA Setup — encrypted secret storage invisible to user), SCR-125 (Compliance Reports — PDF button), SCR-110 (Notification Channels — webhook validation)

## Acceptance Criteria
- [ ] AC-1: `extractIP()` in `internal/analytics/anomaly/detector.go:237` handles IPv6. Use `net.ParseIP` + `SplitHostPort` instead of last-colon split. Unit test with IPv6 session addresses.
- [ ] AC-2: `GetLastSessionBySIM` in `internal/store/session.go` filters by `tenant_id` in addition to `sim_id`. Defense-in-depth against cross-tenant SIM ID collision.
- [ ] AC-3: Pseudonymization unified — `RightToErasure` uses salted SHA-256 matching `RunPurgeSweep` pattern via shared `deriveTenantSalt` helper. Remove unsalted path entirely.
- [ ] AC-4: Webhook channel validates non-empty URL (must be valid HTTPS) and non-empty secret before accepting. `dispatchToChannels` rejects empty-config webhooks with a structured error, not runtime failure. Frontend mirrors validation.
- [ ] AC-5: `DeliveryTracker.rateLimiter` wired to Redis in `cmd/argus/main.go`. Rate limiting active for notification delivery in production. Config var `NOTIFICATION_RATE_LIMIT_PER_MINUTE`.
- [ ] AC-6: SIM state machine adds `stolen_lost → terminated` transition (BR-1 from PRODUCT.md). Transition requires `sim_manager` role, logs `sim_state_history`, releases IP/MSISDN with grace period. `STATE_COLORS` map in `web/src/lib/sim-colors.ts` includes `stolen_lost` entry.
- [ ] AC-7: **DEFER→impl:** TOTP secret stored encrypted at rest. Use libsodium sealed box or age encryption keyed by `ENCRYPTION_KEY` env var (already used for operator secrets). Decrypt on verify only. Migration re-encrypts existing rows. Decision recorded (replaces plaintext deviation from STORY-003).
- [ ] AC-8: **DEFER→impl:** Compliance report PDF export implemented alongside CSV. Use `github.com/go-pdf/fpdf` or `github.com/jung-kurt/gofpdf`. Endpoint `GET /api/v1/compliance/reports/:id/export?format=pdf`. Frontend toggle on Compliance Reports page. AC-13 of STORY-039 now fully met.
- [ ] AC-9: **DEFER→impl:** NATS event payloads carry `tenant_id` field. All publishers (operator health, policy, job, session, anomaly, bulk, notification) populate it. `relayNATSEvent` in `internal/ws/hub.go` extracts `tenant_id` and broadcasts only to matching tenant connections (replaces `BroadcastAll`). Cross-tenant isolation verified. System-wide events explicitly tagged `tenant_id=nil` → broadcast all.
- [ ] AC-10: Security audit clean — `govulncheck ./...` zero high/critical, `npm audit --audit-level=high` zero issues. Bcrypt cost ≥ 12. Cookie flags `Secure`, `HttpOnly`, `SameSite=Strict` on refresh token. Rate limits enforced on all auth endpoints.

## Dependencies
- Blocked by: STORY-056 (runtime fixes unblock testing)
- Blocks: Phase 10 Gate (security gate pass mandatory for prod)

## Test Scenarios
- [ ] Unit: `extractIP("[2001:db8::1]:4500")` returns `2001:db8::1`, not truncated.
- [ ] Unit: `GetLastSessionBySIM` with mismatched tenant returns nil (defense-in-depth test).
- [ ] Unit: `RightToErasure` pseudonym matches `RunPurgeSweep` pseudonym for same tenant+identifier.
- [ ] Unit: Empty webhook URL rejected at dispatch — no partial delivery attempt.
- [ ] Integration: Notification rate limiter blocks 61st message in 60s for a tenant.
- [ ] Integration: `PATCH /sims/:id {state: "terminated"}` from `stolen_lost` succeeds, history row created, IP released (grace delayed).
- [ ] Integration: Create user + enable 2FA → inspect `users.totp_secret` column → ciphertext, not plaintext. Verify TOTP still works on login.
- [ ] Integration: `GET /compliance/reports/:id/export?format=pdf` returns PDF bytes with correct headers.
- [ ] Integration: Tenant A connects WS, tenant B publishes policy event → A does NOT receive event.
- [ ] Integration: System event (no tenant_id) → all connected tenants receive it.
- [ ] CI: `govulncheck ./...` exits 0. `npm audit --audit-level=high` exits 0.

## Effort Estimate
- Size: L
- Complexity: High (crypto rotation migration, NATS payload schema change, PDF library)
