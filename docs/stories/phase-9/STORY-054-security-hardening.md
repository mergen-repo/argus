# STORY-054: Security Hardening

## User Story
As a platform operator, I want TLS everywhere, input validation, security headers, and dependency auditing, so that the platform meets enterprise security standards.

## Description
Security hardening across all layers: TLS for HTTPS (API + portal), RadSec (RADIUS over TLS), Diameter/TLS. Input validation and sanitization on all API endpoints. CORS configuration per tenant. Content Security Policy headers. Dependency audit with govulncheck and npm audit. Rate limiting hardening.

## Architecture Reference
- Services: SVC-01 (API Gateway — TLS, CORS, CSP), SVC-04 (AAA — RadSec, Diameter/TLS)
- Packages: internal/gateway, internal/aaa, internal/protocol/radius, internal/protocol/diameter
- Source: docs/architecture/services/_index.md (SVC-01, SVC-04)

## Screen Reference
- SCR-120: System Health — TLS certificate status, security scan results

## Acceptance Criteria
- [x] HTTPS: TLS 1.2+ on all HTTP endpoints (API, portal, WebSocket)
- [x] TLS certificates: configurable via environment variables, auto-renewal support (Let's Encrypt placeholder)
- [x] RadSec: RADIUS over TLS (TCP :2083) as alternative to UDP :1812 (RFC 6614)
- [x] Diameter/TLS: TLS on TCP :3868 for Diameter peers
- [x] 5G SBA: mTLS support on :8443
- [x] Input validation: all API request bodies validated against JSON schema
- [x] Input sanitization: strip HTML/script tags from string fields
- [x] SQL injection prevention: parameterized queries only (no string concatenation)
- [x] CORS: per-tenant origin whitelist, configurable in tenant settings
- [x] CSP headers: Content-Security-Policy restricting script-src, style-src, img-src
- [x] Security headers: X-Content-Type-Options, X-Frame-Options, X-XSS-Protection, Strict-Transport-Security
- [x] Rate limiting: per-IP, per-user, per-API-key rate limits (configurable)
- [x] Brute force protection: progressive delay on failed auth attempts
- [x] Dependency audit: govulncheck in CI, npm audit for frontend
- [x] No secrets in logs: filter password, token, API key from log output
- [x] Audit: all security events logged (failed auth, CORS violation, rate limit hit)

## Dependencies
- Blocked by: STORY-001 (scaffold), STORY-003 (auth), STORY-015 (RADIUS), STORY-019 (Diameter)
- Blocks: None (hardening story)

## Test Scenarios
- [x] HTTPS: HTTP request redirected to HTTPS
- [x] TLS 1.1 connection attempt → rejected
- [x] RadSec: RADIUS over TLS handshake succeeds, auth works
- [x] XSS in request body → sanitized, no script execution
- [x] SQL injection in query param → parameterized, no injection
- [x] CORS: request from unauthorized origin → blocked
- [x] CSP header present in all responses
- [x] Rate limit: 101st request in 1 minute → 429 response
- [x] govulncheck: no known vulnerabilities in dependencies
- [x] npm audit: no high/critical vulnerabilities
- [x] Password field in log output → masked as "***"

## Effort Estimate
- Size: L
- Complexity: High
