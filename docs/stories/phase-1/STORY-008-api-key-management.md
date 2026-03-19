# STORY-008: API Key Management, Rate Limiting & OAuth2

## User Story
As a tenant admin, I want to create API keys with scoped permissions and configurable rate limits for M2M integrations, and support OAuth2 client credentials for third-party systems.

## Description
CRUD for API keys with SHA-256 hashing (never store plaintext), scope restriction (endpoint patterns), configurable rate limits (Redis sliding window), rotation, and revocation.

## Architecture Reference
- Services: SVC-01 (Gateway — API key auth + rate limiting), SVC-03 (API key CRUD)
- API Endpoints: API-150 to API-154
- Database Tables: TBL-04 (api_keys)
- Source: docs/architecture/db/platform.md (TBL-04)
- Spec: docs/architecture/ALGORITHMS.md (Section 3: Rate Limiting), docs/architecture/MIDDLEWARE.md, docs/architecture/ERROR_CODES.md

## Screen Reference
- SCR-111: Settings — API Keys (docs/screens/SCR-111-settings-apikeys.md)

## Acceptance Criteria
- [ ] POST /api/v1/api-keys creates key, returns plaintext ONCE in response (argus_{prefix}_{secret})
- [ ] Only key_prefix and SHA-256(full_key) stored in DB
- [ ] GET /api/v1/api-keys lists keys (prefix shown, not secret)
- [ ] API key auth via X-API-Key header in gateway middleware
- [ ] Scopes restrict access: `["sims:read", "cdrs:*"]` → can read SIMs and all CDR endpoints
- [ ] Rate limiting per key: configurable per_minute and per_hour limits
- [ ] Rate limiting uses Redis sliding window counters
- [ ] POST /api/v1/api-keys/:id/rotate generates new key, old key has 24h grace period
- [ ] DELETE /api/v1/api-keys/:id revokes immediately
- [ ] 429 TOO_MANY_REQUESTS returned with Retry-After header when rate limited
- [ ] Usage count and last_used_at updated on each API call

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-151 | POST | /api/v1/api-keys | `{name,scopes,rate_limit_per_minute,rate_limit_per_hour,expires_at?}` | `{id,name,prefix,key:"argus_8f_...",scopes,rate_limits}` | JWT(tenant_admin+) | 201 |
| API-150 | GET | /api/v1/api-keys | `?cursor&limit` | `[{id,name,prefix,scopes,rate_limits,usage_count,last_used_at,expires_at}]` | JWT(tenant_admin+) | 200 |
| API-153 | POST | /api/v1/api-keys/:id/rotate | — | `{id,name,prefix,key:"argus_2a_...",grace_period_ends}` | JWT(tenant_admin+) | 200 |
| API-152 | PATCH | /api/v1/api-keys/:id | `{name?,scopes?,rate_limit_per_minute?,rate_limit_per_hour?}` | `{id,name,prefix,scopes,rate_limits}` | JWT(tenant_admin+) | 200, 400, 404 |
| API-154 | DELETE | /api/v1/api-keys/:id | — | — | JWT(tenant_admin+) | 204 |

## Dependencies
- Blocked by: STORY-004 (RBAC), STORY-006 (Redis for rate limiting)
- Blocks: None (enhances existing auth)

## Test Scenarios
- [ ] Create API key → key shown once, subsequent GET shows prefix only
- [ ] Authenticate with valid API key → 200
- [ ] Authenticate with revoked key → 401
- [ ] Exceed per_minute limit → 429 with Retry-After
- [ ] API key with scope "sims:read" accessing POST /sims → 403
- [ ] Rotate key → old key works for 24h, new key works immediately
- [ ] After 24h grace → old key stops working

## Effort Estimate
- Size: M
- Complexity: Medium
