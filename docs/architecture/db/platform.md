# Database: Platform Domain

## TBL-01: tenants

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Tenant identifier |
| name | VARCHAR(255) | NOT NULL | Company name |
| domain | VARCHAR(255) | UNIQUE | Company domain |
| contact_email | VARCHAR(255) | NOT NULL | Primary contact |
| contact_phone | VARCHAR(50) | | Contact phone |
| max_sims | INTEGER | NOT NULL, DEFAULT 100000 | Resource limit: max SIMs |
| max_apns | INTEGER | NOT NULL, DEFAULT 100 | Resource limit: max APNs |
| max_users | INTEGER | NOT NULL, DEFAULT 50 | Resource limit: max users |
| purge_retention_days | INTEGER | NOT NULL, DEFAULT 90 | KVKK/GDPR purge delay |
| settings | JSONB | NOT NULL, DEFAULT '{}' | Tenant-level config (rate limits, notification prefs) |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, suspended, terminated |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |
| created_by | UUID | | Creator user |
| updated_by | UUID | | Last updater |

Indexes:
- `idx_tenants_domain` UNIQUE on (domain)
- `idx_tenants_state` on (state)

---

## TBL-02: users

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | User identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| email | VARCHAR(255) | NOT NULL | Login email |
| password_hash | VARCHAR(255) | NOT NULL | Bcrypt hash (cost 12) |
| name | VARCHAR(100) | NOT NULL | Display name |
| role | VARCHAR(30) | NOT NULL | super_admin, tenant_admin, operator_manager, sim_manager, policy_editor, analyst, api_user |
| totp_secret | VARCHAR(255) | | 2FA TOTP secret (encrypted) |
| totp_enabled | BOOLEAN | NOT NULL, DEFAULT false | 2FA enabled flag |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, disabled, invited |
| last_login_at | TIMESTAMPTZ | | Last successful login |
| failed_login_count | INTEGER | NOT NULL, DEFAULT 0 | Consecutive failed logins |
| locked_until | TIMESTAMPTZ | | Account lockout expiry |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |

Indexes:
- `idx_users_tenant_email` UNIQUE on (tenant_id, email)
- `idx_users_tenant_role` on (tenant_id, role)
- `idx_users_state` on (state)

---

## TBL-03: user_sessions

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Session identifier |
| user_id | UUID | FK → users.id, NOT NULL | User |
| refresh_token_hash | VARCHAR(255) | NOT NULL | Hashed refresh token |
| ip_address | INET | | Client IP |
| user_agent | TEXT | | Client user agent |
| expires_at | TIMESTAMPTZ | NOT NULL | Token expiry |
| revoked_at | TIMESTAMPTZ | | Revocation time |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |

Indexes:
- `idx_user_sessions_user` on (user_id)
- `idx_user_sessions_expires` on (expires_at) WHERE revoked_at IS NULL

---

## TBL-04: api_keys

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Key identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| name | VARCHAR(100) | NOT NULL | Key display name |
| key_prefix | VARCHAR(8) | NOT NULL | First 8 chars for identification |
| key_hash | VARCHAR(255) | NOT NULL | SHA-256 hash of full key |
| scopes | JSONB | NOT NULL, DEFAULT '["*"]' | Allowed endpoint patterns |
| rate_limit_per_minute | INTEGER | NOT NULL, DEFAULT 1000 | Rate limit |
| rate_limit_per_hour | INTEGER | NOT NULL, DEFAULT 30000 | Rate limit |
| expires_at | TIMESTAMPTZ | | Key expiry (null = no expiry) |
| revoked_at | TIMESTAMPTZ | | Revocation time |
| last_used_at | TIMESTAMPTZ | | Last usage time |
| usage_count | BIGINT | NOT NULL, DEFAULT 0 | Total usage count |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| created_by | UUID | FK → users.id | Creator |

Indexes:
- `idx_api_keys_tenant` on (tenant_id)
- `idx_api_keys_prefix` on (key_prefix)
- `idx_api_keys_active` on (tenant_id) WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())
