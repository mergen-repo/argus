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
- [ ] HTTPS: TLS 1.2+ on all HTTP endpoints (API, portal, WebSocket)
- [ ] TLS certificates: configurable via environment variables, auto-renewal support (Let's Encrypt placeholder)
- [ ] RadSec: RADIUS over TLS (TCP :2083) as alternative to UDP :1812 (RFC 6614)
- [ ] Diameter/TLS: TLS on TCP :3868 for Diameter peers
- [ ] 5G SBA: mTLS support on :8443
- [ ] Input validation: all API request bodies validated against JSON schema
- [ ] Input sanitization: strip HTML/script tags from string fields
- [ ] SQL injection prevention: parameterized queries only (no string concatenation)
- [ ] CORS: per-tenant origin whitelist, configurable in tenant settings
- [ ] CSP headers: Content-Security-Policy restricting script-src, style-src, img-src
- [ ] Security headers: X-Content-Type-Options, X-Frame-Options, X-XSS-Protection, Strict-Transport-Security
- [ ] Rate limiting: per-IP, per-user, per-API-key rate limits (configurable)
- [ ] Brute force protection: progressive delay on failed auth attempts
- [ ] Dependency audit: govulncheck in CI, npm audit for frontend
- [ ] No secrets in logs: filter password, token, API key from log output
- [ ] Audit: all security events logged (failed auth, CORS violation, rate limit hit)

## Dependencies
- Blocked by: STORY-001 (scaffold), STORY-003 (auth), STORY-015 (RADIUS), STORY-019 (Diameter)
- Blocks: None (hardening story)

## Test Scenarios
- [ ] HTTPS: HTTP request redirected to HTTPS
- [ ] TLS 1.1 connection attempt → rejected
- [ ] RadSec: RADIUS over TLS handshake succeeds, auth works
- [ ] XSS in request body → sanitized, no script execution
- [ ] SQL injection in query param → parameterized, no injection
- [ ] CORS: request from unauthorized origin → blocked
- [ ] CSP header present in all responses
- [ ] Rate limit: 101st request in 1 minute → 429 response
- [ ] govulncheck: no known vulnerabilities in dependencies
- [ ] npm audit: no high/critical vulnerabilities
- [ ] Password field in log output → masked as "***"

## Effort Estimate
- Size: L
- Complexity: High
