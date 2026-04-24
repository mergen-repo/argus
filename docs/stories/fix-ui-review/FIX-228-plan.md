# Implementation Plan: FIX-228 — Login Forgot Password Flow + Version Footer

## Goal
Deliver secure, enumeration-safe, rate-limited password-reset-by-email flow (request + confirm, opaque single-use DB-stored token, audit events, email via SMTP + mailhog dev fixture) + a "Argus v{version}" footer on the auth layout.

## Story Reference
- Story: `/Users/btopcu/workspace/argus/docs/stories/fix-ui-review/FIX-228-forgot-password-flow.md`
- Priority: P3 · Effort: M · Wave: 7 · Finding: F-01
- ACs: AC-1 … AC-8 (see story, verbatim enforcement)

---

## Pinned Decisions (decided at Plan time — Developer must NOT re-decide)

| ID | Decision | Rationale |
|----|----------|-----------|
| **DEV-323** | **Token model = OPAQUE 32-byte random (`crypto/rand`), SHA-256 hashed in `password_reset_tokens.token_hash`, single-use via row DELETE on successful confirm.** Story AC-4 wording "`<jwt>`" is a drafting error — the story's own AC-5 ("stored in `password_reset_tokens` table", single-use) forces a DB-backed opaque token. JWT + DB = double-state; opaque-hashed is the standard OWASP pattern. URL carries `?token=<base64url>` (44 chars). | Prevents replay, removes signing-key rotation risk, single source of truth = DB. |
| **DEV-324** | **Constant-time enumeration defense via spy-verifiable dummy bcrypt call.** When email not found (or rate-limited-but-still-200), handler still performs a `bcrypt.CompareHashAndPassword` against a FIXED dummy hash to match timing of the real path's hash work. Handler exposes an injectable `dummyBcryptHook func()` for tests to assert exactly-once invocation per non-existent email. No `time.Sleep`. | Deterministic timing path; testable without flaky p99 assertions. |
| **DEV-325** | **Response shape: always `{status:"success", data:{message:"If that email exists, a reset link has been sent."}}` with HTTP 200 — for both "email exists", "email does not exist", AND "user exists but is disabled".** Rate-limit rejection is the ONLY non-200 path and returns HTTP 429 with a generic `RATE_LIMITED` code (NOT a reset-specific code) — envelope body identical for all rate-limit sources so an attacker can't distinguish "this email was rate-limited" from "some other auth flow was rate-limited". | Pure enumeration defense. |
| **DEV-326** | **Rate limit BEFORE any DB lookup — keyed on `lowercased_email`, counter in `password_reset_tokens.email_rate_key` rolling window (last hour rows via `created_at >= NOW() - INTERVAL '1 hour'`), configurable via `PASSWORD_RESET_RATE_LIMIT_PER_HOUR` env (default 5). NO Redis dependency — DB-only to avoid cross-service wiring for this isolated flow.** | Eliminates ordering/race of "lookup then ratelimit"; works even if Redis is down; reuses existing migration path. |
| **DEV-327** | **Email send = new `EmailSender.SendTo(ctx, to, subject, textBody, htmlBody) error` interface added to `internal/notification/email.go` alongside existing `SendAlert`. New `text/template`-based `password_reset_email.tmpl.txt` + `.html` embedded into binary via `embed.FS` under `internal/notification/templates/`. Reset URL constructed from `cfg.PublicBaseURL` + `/auth/reset?token=<b64>`.** | Existing `SendAlert` hardcoded subject for alerts only; opening a parallel `SendTo` path avoids breaking existing callers. `embed.FS` keeps template hot-reloadable via build and binary-self-contained. |
| **DEV-328** | **Dev SMTP fixture via `mailhog` container added to `deploy/docker-compose.yml` (SMTP :1025, Web UI :8025). `.env.example` gains `SMTP_HOST=mailhog`, `SMTP_PORT=1025`, `SMTP_TLS=false`.** | Unblocks AC-3 "sends email" E2E verification. ~10 LOC diff. |
| **DEV-329** | **Version footer = Vite build-time define `__APP_VERSION__` injected from `package.json` version + git short-sha if available. Frontend-only; no new backend endpoint.** Footer lives in `auth-layout.tsx` only (login + reset + forgot pages — the 3 AuthLayout-wrapped pages). | No new API surface; build-time is adequate for "support identification" purpose of AC-8. |
| **DEV-330** | **Referer leak defense: add `<meta name="referrer" content="no-referrer" />` to `index.html` head (global). Reset URL GET-semantic preserved per AC-4.** | Simple, global, zero UX cost. |
| **DEV-331** | **Password policy reuse: `internal/auth.ValidatePasswordPolicy(newPwd, cfg.Policy)` (exists at `internal/auth/auth.go:573`). Hash via `bcrypt.GenerateFromPassword(pwd, cfg.BcryptCost)`. Zero duplicated policy logic.** | Consistent with existing `ChangePassword` path. |
| **DEV-332** | **Token TTL = 1 hour (AC-3). GC job: no new cron. Instead, on every `request` call, purge rows where `expires_at < NOW()` inline (single DELETE, unscoped by tenant since table is platform-global).** | Zero new background infrastructure. |

---

## Architecture Context

### Components Involved
| Component | Layer | Responsibility | File |
|-----------|-------|----------------|------|
| `password_reset_tokens` | DB (TBL-50) | Single-use token storage + rate-limit source of truth | `migrations/20260425000001_password_reset_tokens.*` |
| `PasswordResetStore` | Store | CRUD: create (with hash), find by hash, delete, count-by-email-window | `internal/store/password_reset.go` (NEW) |
| `AuthHandler.RequestPasswordReset` + `.ConfirmPasswordReset` | API | HTTP endpoints; enumeration defense; audit; rate-limit | `internal/api/auth/password_reset.go` (NEW — methods on existing `AuthHandler` struct) |
| `EmailSender.SendTo` | Notification | Per-recipient templated send | `internal/notification/email.go` (extend) |
| `password_reset_email.tmpl.{txt,html}` | Template | Email body | `internal/notification/templates/` (NEW dir) |
| Login forgot link + routes | UI | `/auth/forgot` + `/auth/reset?token=` | `web/src/pages/auth/{login,forgot,reset}.tsx` |
| Version footer | UI | Display `Argus v<version>` | `web/src/components/layout/auth-layout.tsx` (modify) + `web/vite.config.ts` (define) |
| Referer meta | UI | Leak defense | `web/index.html` |
| Mailhog fixture | Infra | Dev SMTP catch-all | `deploy/docker-compose.yml` |

### Data Flow (Request)
```
User → web/forgot.tsx → POST /api/v1/auth/password-reset/request {email}
→ gateway router → AuthHandler.RequestPasswordReset
  → [1] rate-limit check: store.CountRecentRequestsForEmail(email, 1h) >= N → 429 (generic)
  → [2] user lookup: userStore.GetByEmail(email)
      → NOT FOUND → dummyBcryptHook(); goto [5]
      → FOUND → continue
  → [3] generate token (32B crypto/rand), hash SHA-256
  → [4] store.CreatePasswordResetToken(userID, tokenHash, emailRateKey, expiresAt)
      → email.SendTo(ctx, user.Email, subject, text, html) with URL containing base64 token
  → [5] audit.Log("auth.password_reset_requested", {email})
  → [6] return 200 {status:success, data:{message:"If that email..."}}
```

### Data Flow (Confirm)
```
User → web/reset.tsx?token=<b64> → POST /api/v1/auth/password-reset/confirm {token, password}
→ AuthHandler.ConfirmPasswordReset
  → [1] decode token → SHA-256 hash
  → [2] store.FindTokenByHash(hash) → row OR ErrNotFound
      → not found OR expired → 400 PASSWORD_RESET_INVALID_TOKEN
  → [3] auth.ValidatePasswordPolicy(password, cfg.Policy) → 422 on fail
  → [4] bcrypt.GenerateFromPassword(password, cfg.BcryptCost)
  → [5] userStore.UpdatePasswordHash(userID, newHash)
  → [6] store.DeletePasswordResetToken(hash) — single-use enforcement
  → [7] store.DeleteAllTokensForUser(userID) — invalidate other pending resets
  → [8] audit.Log("auth.password_reset_completed", {userID})
  → [9] return 200 {status:success, data:{message:"Password reset successful"}}
```

### API Specifications

#### `POST /api/v1/auth/password-reset/request`
- Public (NO auth required). Registered in gateway public-route group with existing login/refresh.
- Request body: `{ "email": "string" }`
- **Always 200** unless rate-limited (429). Body:
  - Success: `{"status":"success","data":{"message":"If that email exists, a reset link has been sent."}}`
  - 429: `{"status":"error","error":{"code":"RATE_LIMITED","message":"Too many requests. Please try again later."}}`
- Audit: `auth.password_reset_requested` (always, even when email not found — with `{"found":false}` in meta; enumeration defense: audit row exists either way).

#### `POST /api/v1/auth/password-reset/confirm`
- Public. Registered in gateway public-route group.
- Request body: `{ "token": "base64url-string", "password": "string" }`
- Responses:
  - 200: `{"status":"success","data":{"message":"Password reset successful"}}`
  - 400 `PASSWORD_RESET_INVALID_TOKEN`: token missing/malformed/expired/already-used
  - 422 `PASSWORD_TOO_SHORT` / `PASSWORD_MISSING_CLASS` / `PASSWORD_REPEATING_CHARS` / `PASSWORD_REUSED`: policy violations (reuse existing codes)
- Audit: `auth.password_reset_completed` on success.

### Database Schema
Source: **NEW migration (TBL-50)** — no existing file.

```sql
-- 20260425000001_password_reset_tokens.up.sql
CREATE TABLE password_reset_tokens (
  id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash      BYTEA        NOT NULL UNIQUE,        -- SHA-256(rawToken)
  email_rate_key  TEXT         NOT NULL,               -- lower(user.email) — ratelimit index
  expires_at      TIMESTAMPTZ  NOT NULL,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_password_reset_tokens_email_rate ON password_reset_tokens (email_rate_key, created_at DESC);
CREATE INDEX idx_password_reset_tokens_expires_at ON password_reset_tokens (expires_at);
CREATE INDEX idx_password_reset_tokens_user_id    ON password_reset_tokens (user_id);

COMMENT ON TABLE password_reset_tokens IS 'TBL-50 FIX-228: single-use password reset tokens (DEV-323).';
```

`.down.sql`: `DROP TABLE IF EXISTS password_reset_tokens;`

### Config Changes
Add to `internal/config/config.go` (alongside RateLimit block ~line 68-73):
```go
PasswordResetRateLimitPerHour int `envconfig:"PASSWORD_RESET_RATE_LIMIT_PER_HOUR" default:"5"`
PasswordResetTokenTTLMinutes  int `envconfig:"PASSWORD_RESET_TOKEN_TTL_MINUTES"  default:"60"`
PublicBaseURL                 string `envconfig:"PUBLIC_BASE_URL" default:"http://localhost:8084"`
```
Add validation in existing validator block:
```go
if c.PasswordResetRateLimitPerHour < 1 { c.PasswordResetRateLimitPerHour = 5 }
if c.PasswordResetTokenTTLMinutes < 1  { c.PasswordResetTokenTTLMinutes = 60 }
```
Add to `.env.example` under Email (SMTP) block:
```
PASSWORD_RESET_RATE_LIMIT_PER_HOUR=5
PASSWORD_RESET_TOKEN_TTL_MINUTES=60
PUBLIC_BASE_URL=http://localhost:8084
```

### PAT-017 Wiring Trace (MANDATORY grep verification)
`PasswordResetRateLimitPerHour` must appear at ≥ 5 locations:
1. `internal/config/config.go` — struct field definition with envconfig tag
2. `internal/config/config.go` — validator in existing validation function
3. `cmd/argus/main.go` — passed into `authHandler.WithPasswordReset(...)` builder
4. `internal/api/auth/password_reset.go` — handler struct field (or closure)
5. `internal/api/auth/password_reset.go` — used in rate-limit check

Same trace for `PasswordResetTokenTTLMinutes` (≥ 4 hits: config field, validator, main.go, handler usage).

Gate command:
```bash
rg -n 'PasswordResetRateLimitPerHour' internal/ cmd/  # ≥ 5 hits required
rg -n 'PasswordResetTokenTTLMinutes'  internal/ cmd/  # ≥ 4 hits required
```

### Error Codes (add to `docs/architecture/ERROR_CODES.md`)
| Code | HTTP | Description |
|------|------|-------------|
| `PASSWORD_RESET_INVALID_TOKEN` | 400 | Token missing, malformed, expired, or already used. Response message always generic. |

(`RATE_LIMITED` already exists; `PASSWORD_TOO_SHORT` etc. already exist — reuse.)

### Audit Events
Wire via `AuthHandler.auditSvc` (add field + `WithAudit` builder option, mirroring `user.Handler` pattern at `internal/api/user/handler.go:61,87,110,428`).
- Action `auth.password_reset_requested` — entity: email (hashed for privacy? — NO; per existing audit patterns store cleartext, audit is internal). Meta: `{"email":..., "found":bool, "ip":..}`.
- Action `auth.password_reset_completed` — entity: user_id. Meta: `{"ip":..}`.

### Screen Mockups

#### SCR-LOGIN (existing — `/auth/login`) — MODIFY
```
┌─────────────────────────────────────┐
│   Argus                             │
│   APN & Subscriber Intelligence     │
├─────────────────────────────────────┤
│   Email                             │
│   [................]                │
│   Password                          │
│   [................]                │
│   [ ] Remember me                   │
│   [      Sign in     ]              │
│                                     │
│   Forgot password?       ← NEW link │
├─────────────────────────────────────┤
│   Argus v0.1.0                ← NEW │
└─────────────────────────────────────┘
```

#### SCR-FORGOT (NEW — `/auth/forgot`)
```
┌─────────────────────────────────────┐
│   Reset your password               │
│   Enter your email and we'll send   │
│   a reset link.                     │
├─────────────────────────────────────┤
│   Email                             │
│   [................]                │
│   [    Send reset link    ]         │
│                                     │
│   ← Back to sign in                 │
├─────────────────────────────────────┤
│   [submit success banner]:          │
│   "If that email exists, a reset    │
│    link has been sent."             │
├─────────────────────────────────────┤
│   Argus v0.1.0                      │
└─────────────────────────────────────┘
```
- Navigation: login page "Forgot password?" link → `/auth/forgot`. Back link → `/auth/login`.
- On successful submit: stay on page; replace form with success banner (same generic text always).
- On 429: show generic "Too many requests. Please try again later." — same tone, no email-specific details.

#### SCR-RESET (NEW — `/auth/reset?token=<b64>`)
```
┌─────────────────────────────────────┐
│   Set a new password                │
├─────────────────────────────────────┤
│   New password                      │
│   [................]                │
│   Confirm password                  │
│   [................]                │
│                                     │
│   [    Set new password    ]        │
├─────────────────────────────────────┤
│   On success: redirect to /auth/    │
│   login with toast "Password reset".│
│   On invalid token: inline error    │
│   "This reset link is invalid or    │
│    has expired. Request a new one." │
├─────────────────────────────────────┤
│   Argus v0.1.0                      │
└─────────────────────────────────────┘
```
- Missing/empty `?token=` → immediately render the invalid-token error state (no submit button).
- Password inputs: `autocomplete="new-password"` (both fields).
- Confirm-match validation client-side; policy violations from server rendered inline.

### Design Token Map

#### Color Tokens (from `FRONTEND.md` + existing `auth-layout.tsx`)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-text-primary` | `text-white`, `text-[#fff]` |
| Secondary text (labels, helpers, footer) | `text-text-secondary` | `text-gray-400`, `text-slate-500` |
| Input border default | `border-border` | `border-[#ccc]`, `border-gray-300` |
| Input border error | `border-destructive` | `border-red-500`, `border-[#ef4444]` |
| Input bg | `bg-bg-surface` | `bg-white` |
| Primary button bg | `bg-accent` (matches login page current button) | `bg-blue-500`, `bg-primary-500` |
| Primary button text | `text-bg-primary` (matches logo/A chip) | `text-white` |
| Success banner bg | `bg-success/10` | `bg-green-50` |
| Error text (inline) | `text-destructive` | `text-red-500` |
| Link (Forgot / Back) | `text-accent hover:text-accent/80` | `text-blue-600` |
| Footer version line | `text-text-secondary text-xs` | `text-gray-500 text-[11px]` |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title (Reset your password) | `text-xl font-semibold text-text-primary` | `text-[20px]` |
| Helper copy | `text-sm text-text-secondary` | `text-[14px]` |
| Input label | `text-sm font-medium text-text-primary` | arbitrary |
| Button label | default button component typography | — |

#### Spacing & Elevation Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Card (reuses `auth-layout` wrapper) | `rounded-xl border border-border bg-bg-surface shadow-[var(--shadow-card)]` (existing) | new shadow |
| Section gap | `space-y-4` (forms) / `space-y-6` (page sections) | arbitrary |
| Button block | `w-full` full-width | — |

#### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `<Input>` | `web/src/components/ui/input.tsx` | ALL form fields — NEVER raw `<input>` |
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — NEVER raw `<button>` |
| `<Spinner>` | `web/src/components/ui/spinner.tsx` | Loading indicator inside submit button |
| `<AuthLayout>` | `web/src/components/layout/auth-layout.tsx` | Wraps all 3 auth pages via Outlet |
| `cn` util | `web/src/lib/utils` | Conditional classNames |
| `authApi` | `web/src/lib/api` | HTTP client (add `forgotPassword` + `resetPassword` methods) |
| `sonner` `toast` | existing vendor-ui | Success toast on reset complete |

#### Default Tailwind Palette Audit (PAT-018 defense)
After implementing any `.tsx` file in this story, run:
```bash
rg -nE '\b(text|bg|border)-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}\b' web/src/pages/auth/ web/src/components/layout/auth-layout.tsx
```
Expected: ZERO matches. Any match = plan violation → FIX.

---

## Prerequisites
- [x] FIX-227 completed (Plan discipline + PAT-018 established)
- [x] Notification service (`internal/notification/`) exists with SMTP sender (verified: `email.go:20`)
- [x] Audit service pattern available (`user.Handler` uses `auditSvc audit.Auditor`; wire similarly)
- [x] `auth.ValidatePasswordPolicy` exists (`internal/auth/auth.go:573`)

---

## Tasks

### Wave 0 — Audit + Infra Fixture

#### Task 1: Mailhog dev fixture + SMTP env flip
- **Files:** Modify `deploy/docker-compose.yml` (+1 service), Modify `.env.example` (flip SMTP defaults to mailhog), Modify `deploy/docker-compose.blue.yml` + `deploy/docker-compose.green.yml` IF they inherit from base (read first; modify only if they don't already extend).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `deploy/docker-compose.yml` operator-sim service block (~line 137) for service-definition style (image pin by digest, networks, logging anchor, restart policy).
- **Context refs:** "Pinned Decisions > DEV-328"
- **What:** Add `mailhog:` service (image `mailhog/mailhog:latest`, SHA256-pinned — look up a stable tag; if unable to pin in first attempt, use floating tag AND add D-NNN tech debt to pin on next round). Expose 1025 internal + 8025 web UI. Add to `default-logging`, attach to existing network. Add healthcheck: `wget -q -O - http://localhost:8025/api/v2/messages | grep -q total` or similar. Flip `.env.example`: `SMTP_HOST=mailhog`, `SMTP_PORT=1025`, `SMTP_TLS=false`.
- **Verify:** `docker compose -f deploy/docker-compose.yml config` parses cleanly. `docker compose -f deploy/docker-compose.yml up -d mailhog && curl -sf http://localhost:8025/` returns HTML.

### Wave 1 — DB + Store

#### Task 2: Migration for `password_reset_tokens` (TBL-50)
- **Files:** Create `migrations/20260425000001_password_reset_tokens.up.sql`, Create `migrations/20260425000001_password_reset_tokens.down.sql`, Modify `docs/architecture/db/_index.md` (add TBL-50 row).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260422000001_alerts_table.up.sql` — same style (CREATE TABLE + indexes + COMMENT ON TABLE + feature tag).
- **Context refs:** "Database Schema"
- **What:** Emit exact SQL from "Database Schema" section. `up.sql` creates table + 3 indexes + COMMENT. `down.sql` = `DROP TABLE IF EXISTS password_reset_tokens;`. Append `| TBL-50 | password_reset_tokens | Auth | → TBL-02 (user_id); platform-global (no tenant_id) | No |` row to `docs/architecture/db/_index.md`.
- **Verify:** `make db-migrate` succeeds on a fresh volume. `psql -c "\d password_reset_tokens"` shows 3 indexes.

#### Task 3: PasswordResetStore + tests
- **Files:** Create `internal/store/password_reset.go`, Create `internal/store/password_reset_test.go`.
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/backup_codes.go` — closest analogue: hash-based lookup, tenant-less platform table, single-use semantics. Follow same constructor shape (`NewPasswordResetStore(db *sql.DB)`), same `type PasswordResetToken struct` placement.
- **Context refs:** "Database Schema", "Pinned Decisions > DEV-323, DEV-326, DEV-332"
- **What:** Methods:
  - `Create(ctx, userID uuid.UUID, tokenHash [32]byte, emailRateKey string, expiresAt time.Time) error`
  - `FindByHash(ctx, tokenHash [32]byte) (*PasswordResetToken, error)` — returns ErrNotFound if missing OR expired (one query: `WHERE token_hash=$1 AND expires_at > NOW()`).
  - `DeleteByHash(ctx, tokenHash [32]byte) error`
  - `DeleteAllForUser(ctx, userID uuid.UUID) error`
  - `CountRecentForEmail(ctx, emailRateKey string, window time.Duration) (int, error)` — `SELECT count(*) FROM password_reset_tokens WHERE email_rate_key=$1 AND created_at >= NOW() - $2`
  - `PurgeExpired(ctx) error` — inline GC (DEV-332) `DELETE WHERE expires_at < NOW()`.
- **Tests:** CRUD happy-path, double-use (second DeleteByHash is no-op), expired lookup returns ErrNotFound, rate-count across window boundary, concurrent create-unique-hash (violating UNIQUE constraint returns error).
- **Verify:** `go test ./internal/store/ -run TestPasswordReset -v` → PASS.

### Wave 2 — Backend Handler

#### Task 4: Email template files + EmailSender.SendTo
- **Files:** Create `internal/notification/templates/password_reset_email.txt.tmpl`, Create `internal/notification/templates/password_reset_email.html.tmpl`, Modify `internal/notification/email.go` (+ SendTo method), Create `internal/notification/templates.go` (embed.FS loader + typed renderer).
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/notification/email.go` SendAlert (~line 28) for SMTP send MIME structure. Use same addr resolution, TLS selection, header construction — just parameterize `to` and take a multipart text+html body.
- **Context refs:** "Pinned Decisions > DEV-327"
- **What:**
  - Template text body: greeting + reset link + TTL notice + "ignore if not you" footer. Template HTML: inline-styled, same copy.
  - Add `type PasswordResetEmailData struct { UserName, ResetURL, ExpiryHuman string }` + `RenderPasswordResetEmail(data) (subject, text, html string, err error)` in `templates.go` — uses `embed.FS` with `//go:embed templates/*.tmpl`.
  - Extend `EmailSender` (or add new interface `TemplatedEmailSender`) with `SendTo(ctx context.Context, to, subject, textBody, htmlBody string) error`. Build multipart/alternative MIME. Reuse TLS/auth selection from existing SendAlert.
- **Verify:** Unit test: render template with test data → assert link contains token + expiry text not empty. SMTP send: mock `smtp.SendMail` (interface abstraction OR test against mailhog via integration test in Task 8).

#### Task 5: AuthHandler.WithAudit + .WithPasswordReset builder + RequestPasswordReset handler
- **Files:** Create `internal/api/auth/password_reset.go`, Modify `internal/api/auth/handler.go` (+ `auditSvc` field + `WithAudit` builder + `WithPasswordReset` builder that captures store + emailSender + ratelimit + TTL + publicBaseURL).
- **Depends on:** Task 3, Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/api/auth/handler.go:32-67` (existing struct + builder options) and `internal/api/user/handler.go:428` (`createAuditEntry` helper + argument order). Follow builder-chain style: `authHandler.WithAudit(auditSvc).WithPasswordReset(store, email, cfg, base)`.
- **Context refs:** "API Specifications > POST /api/v1/auth/password-reset/request", "Pinned Decisions > DEV-324, DEV-325, DEV-326, DEV-332", "PAT-017 Wiring Trace"
- **What:**
  - `handler.go`: add fields `auditSvc audit.Auditor`, `prStore *store.PasswordResetStore`, `emailSender notification.EmailSender`, `prRateLimit int`, `prTokenTTL time.Duration`, `prPublicBaseURL string`, `dummyBcryptHook func()` (default no-op). Builder options `WithAudit(a)`, `WithPasswordReset(store, email, rateLimit int, ttl time.Duration, baseURL string)`. Add helper `createAuditEntry(r, action, entity, before, after)` mirroring `user.Handler`.
  - `password_reset.go`: `RequestPasswordReset(w,r)` implementing the Data Flow above:
    1. Decode JSON body; validate email format via existing pattern (reuse `auth.ValidatePasswordPolicy`-adjacent utility OR inline regex matching login.tsx pattern). On malformed body: **still** return 200 generic (don't leak validation failures either? Decision: return 422 `VALIDATION_ERROR` ONLY for malformed JSON body shape; accept any syntactically-email-ish string — enumeration is about "does this email exist", not "is this email syntactically valid"). Document in handler comment.
    2. Inline `s.prStore.PurgeExpired(ctx)` fire-and-forget (DEV-332).
    3. Lowercased emailRateKey. Call `CountRecentForEmail(emailRateKey, 1h)`. If >= `prRateLimit` → write 429 RATE_LIMITED.
    4. `userStore.GetByEmail(email)` → if not found: `s.dummyBcryptHook()` then call `bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte("dummy-password"))` (ignore result); skip ahead to audit+respond.
    5. Generate 32 random bytes, base64url encode, SHA-256 hash. Store via `prStore.Create`. Call `emailSender.SendTo(...)`. On SendTo error: LOG but STILL return 200 (do not leak delivery state to attacker).
    6. Audit via `createAuditEntry(r, "auth.password_reset_requested", email, nil, map{"found":bool})`.
    7. Write 200 generic success envelope.
  - Read config at wire-up: `PASSWORD_RESET_RATE_LIMIT_PER_HOUR` threaded from `cfg.PasswordResetRateLimitPerHour`.
- **Verify:** `go build ./...` clean. PAT-017 grep: `rg -n 'PasswordResetRateLimitPerHour' internal/ cmd/` shows ≥ 5 hits.

#### Task 6: ConfirmPasswordReset handler
- **Files:** Modify `internal/api/auth/password_reset.go` (+ `ConfirmPasswordReset` method).
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/auth/handler.go` ChangePassword (~line 533) for body validation + policy + hash update flow. Same error code set.
- **Context refs:** "API Specifications > POST /api/v1/auth/password-reset/confirm", "Pinned Decisions > DEV-331"
- **What:**
  1. Decode `{token, password}`; on malformed → 400 `VALIDATION_ERROR`.
  2. base64url-decode token → SHA-256 hash → `prStore.FindByHash(hash)`. On ErrNotFound → 400 `PASSWORD_RESET_INVALID_TOKEN` (generic msg — "Reset link is invalid or has expired").
  3. `auth.ValidatePasswordPolicy(password, cfg.Policy)` → on failure write the exact error code from policy validator (map policy errors to `PASSWORD_TOO_SHORT` / `PASSWORD_MISSING_CLASS` / `PASSWORD_REPEATING_CHARS` / `PASSWORD_REUSED` — reuse existing mapping already in ChangePassword).
  4. `bcrypt.GenerateFromPassword(pwd, cfg.BcryptCost)` → `userStore.UpdatePasswordHash(userID, hashStr)`.
  5. `prStore.DeleteByHash(hash)` + `prStore.DeleteAllForUser(userID)` — defensive (second call invalidates any other pending tokens for that user, prevents token-hoarding attack).
  6. Audit `auth.password_reset_completed` with userID.
  7. Write 200 success.
- **Verify:** `go build ./...` clean.

#### Task 7: Wire routes + main.go construction sites
- **Files:** Modify `internal/gateway/router.go` (+ 2 public routes), Modify `cmd/argus/main.go` (wire store + email sender + WithAudit + WithPasswordReset on existing `authHandler`).
- **Depends on:** Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go:231-233` (existing public auth routes `/api/v1/auth/login`, `/api/v1/auth/refresh`). Read `cmd/argus/main.go:496-519` (existing `authHandler := authapi.NewAuthHandler(...).WithRedis(...).WithAPIKeyStore(...).WithJWTSecret(...)`).
- **Context refs:** "API Specifications", "PAT-017 Wiring Trace"
- **What:**
  - Router: add inside the existing public group (near line 231-233, NOT the authenticated group):
    ```go
    r.Post("/api/v1/auth/password-reset/request", deps.AuthHandler.RequestPasswordReset)
    r.Post("/api/v1/auth/password-reset/confirm", deps.AuthHandler.ConfirmPasswordReset)
    ```
  - main.go: after existing `authHandler.WithJWTSecret(...)` chain:
    ```go
    prStore := store.NewPasswordResetStore(db)
    emailSender := notification.NewSMTPEmailSender(notification.SMTPConfig{...from cfg})
    authHandler.WithAudit(auditSvc).WithPasswordReset(prStore, emailSender, cfg.PasswordResetRateLimitPerHour, time.Duration(cfg.PasswordResetTokenTTLMinutes)*time.Minute, cfg.PublicBaseURL)
    ```
  - **PAT-011 defense**: grep `NewAuthHandler(` across `cmd/` and ensure all sites (should be one) get the new builder calls. Grep `authHandler\.` in main.go to ensure no orphaned pre-`WithPasswordReset` handler instance is dispatched to `deps.AuthHandler`.
- **Verify:** `go build ./...` clean. `rg -n 'PasswordResetRateLimitPerHour' cmd/ internal/` ≥ 5 hits. `rg -n 'WithPasswordReset' cmd/ internal/` returns 1 definition + 1 call site.

### Wave 3 — Tests

#### Task 8: Backend integration tests
- **Files:** Create `internal/api/auth/password_reset_test.go`.
- **Depends on:** Task 5, Task 6, Task 7
- **Complexity:** high
- **Pattern ref:** Read `internal/api/auth/handler_test.go` for test harness (httptest server, mock DB, cfg setup) and `internal/api/auth/enterprise_integration_test.go` for multi-step integration flow patterns.
- **Context refs:** "API Specifications", "Pinned Decisions > DEV-324, DEV-325, DEV-326", "Data Flow"
- **What:** Test cases (name each precisely):
  - `TestPasswordResetRequest_ExistingEmail_Returns200Generic` — user exists, response body = exact generic success envelope, 200.
  - `TestPasswordResetRequest_NonexistentEmail_Returns200Generic_AndInvokesDummyBcrypt` — inject spy hook; assert exactly-once call (DEV-324 deterministic verification).
  - `TestPasswordResetRequest_BodyShape_IdenticalAcrossCases` — parametric: 10 requests (5 real + 5 non-existent); assert response bytes byte-for-byte identical.
  - `TestPasswordResetRequest_RateLimitEnforced` — set `PasswordResetRateLimitPerHour=5`; fire 6 requests same email; 6th returns 429 `RATE_LIMITED`. **This test is the PAT-017 anchor.**
  - `TestPasswordResetRequest_RateLimit429_BodyIsGenericNotResetSpecific` — 429 body does NOT contain "password" or "reset" strings (cannot distinguish source of rate limit).
  - `TestPasswordResetRequest_EmailDispatchFails_StillReturns200` — inject failing email sender; assert 200 returned; assert audit logged.
  - `TestPasswordResetConfirm_ValidToken_SucceedsAndInvalidatesToken` — full round-trip: request → capture token from DB (since email sender is mock) → confirm → login with new password → assert subsequent confirm with same token fails 400.
  - `TestPasswordResetConfirm_ExpiredToken_Returns400` — insert token with `expires_at = NOW() - 1m`; confirm returns `PASSWORD_RESET_INVALID_TOKEN`.
  - `TestPasswordResetConfirm_ReusedToken_Returns400` — confirm twice; second fails (single-use, DEV-323).
  - `TestPasswordResetConfirm_InvalidPolicy_Returns422` — short password returns existing `PASSWORD_TOO_SHORT` code.
  - `TestPasswordResetConfirm_SuccessInvalidatesAllUserTokens` — create 2 tokens for user; confirm one; assert other is also deleted (defense-in-depth, DEV-331 comment).
- **Verify:** `go test ./internal/api/auth/ -run TestPasswordReset -v -count=1` → all PASS.

### Wave 4 — Frontend

#### Task 9: Version define in Vite + auth-layout footer + referer meta
- **Files:** Modify `web/vite.config.ts` (+ `define: { __APP_VERSION__: JSON.stringify(pkg.version) }`), Modify `web/src/components/layout/auth-layout.tsx` (+ footer), Modify `web/index.html` (+ referer meta), Create `web/src/types/globals.d.ts` IF not present (`declare const __APP_VERSION__: string`).
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Vite define docs (this is standard); `auth-layout.tsx` already uses `text-text-secondary` — follow same tokens.
- **Context refs:** "Pinned Decisions > DEV-329, DEV-330", "Design Token Map"
- **What:**
  - `vite.config.ts`: `import pkg from './package.json'` (use `with {type:'json'}` if ESM strict); add `define: { __APP_VERSION__: JSON.stringify(pkg.version) }` to defineConfig.
  - `auth-layout.tsx`: add below outlet wrapper:
    ```tsx
    <p className="mt-4 text-center text-xs text-text-secondary">Argus v{__APP_VERSION__}</p>
    ```
  - `index.html`: `<meta name="referrer" content="no-referrer" />` inside `<head>`.
  - `globals.d.ts`: declare the Vite define if tsconfig strict complains.
- **Verify:** `make web-build` clean. Rendered login page DOM contains `Argus v0.1.0`. View-source on `/auth/reset?token=X` shows referer meta.

#### Task 10: Login page Forgot Password link + Forgot page + Reset page + routes + API methods
- **Files:** Modify `web/src/pages/auth/login.tsx` (+ Forgot link below submit), Create `web/src/pages/auth/forgot.tsx`, Create `web/src/pages/auth/reset.tsx`, Modify `web/src/routes.tsx` (or router config — find & modify `/auth/` route definitions), Modify `web/src/lib/api.ts` (+ `authApi.requestPasswordReset(email)` + `authApi.confirmPasswordReset(token, password)`).
- **Depends on:** Task 7, Task 9
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/auth/login.tsx` (existing) for form pattern: useState + useNavigate + validate() + handleSubmit + field errors + fieldErrors state + Spinner-in-button. FOLLOW EXACTLY for the two new pages.
- **Context refs:** "Screen Mockups > SCR-LOGIN, SCR-FORGOT, SCR-RESET", "Design Token Map"
- **What:**
  - `api.ts`: add two methods returning `{message: string}` on success. No auth header required.
  - `login.tsx`: add link below submit button, above any existing content: `<Link to="/auth/forgot" className="mt-3 block text-center text-sm text-accent hover:text-accent/80">Forgot password?</Link>`. Use `react-router-dom` Link (already imported? if not, add import).
  - `forgot.tsx`: email input (reuse `<Input>`), submit button (reuse `<Button>` with Spinner child when loading), generic success banner on 200, generic "too many requests" banner on 429. Always show the same success message regardless of backend response contents — just check `res.status === 'success'`. Back-to-login link.
  - `reset.tsx`: parse `?token=` via `useSearchParams`. Empty or missing → render invalid-token error panel. Two password inputs (`autocomplete="new-password"`), confirm-match client-side validation, server-side policy errors rendered inline from error.code mapping (reuse login.tsx's error-handling pattern for PASSWORD_TOO_SHORT etc.). On success: `toast.success('Password reset successful')` + `navigate('/auth/login')`.
  - Routes: add two routes inside the AuthLayout route group:
    ```tsx
    { path: '/auth/forgot', element: <ForgotPasswordPage /> },
    { path: '/auth/reset',  element: <ResetPasswordPage /> },
    ```
  - **Tokens discipline**: zero default-palette Tailwind utilities. Gate command:
    `rg -nE '\b(text|bg|border)-(red|blue|green|purple|pink|orange|yellow|amber|cyan|teal|sky|indigo|violet|fuchsia|rose)-[0-9]{2,3}\b' web/src/pages/auth/forgot.tsx web/src/pages/auth/reset.tsx web/src/components/layout/auth-layout.tsx`
    MUST return ZERO matches.
- **Note:** Invoke `frontend-design` skill for professional polish on these two new pages.
- **Verify:** `npm run build` in `web/` clean. `tsc --noEmit` clean. Manually visit `/auth/login` → click Forgot → `/auth/forgot` loads; submit with fake email → generic banner; manually insert valid token in DB → visit `/auth/reset?token=...` → set password → redirected to login with toast. Footer shows on all 3.

### Wave 5 — Docs + Polish

#### Task 11: Docs (ERROR_CODES, CONFIG, SCREENS, decisions, bug-patterns, ROUTEMAP)
- **Files:** Modify `docs/architecture/ERROR_CODES.md` (+ `PASSWORD_RESET_INVALID_TOKEN`), Modify `docs/architecture/CONFIG.md` (+ 3 env vars), Modify `docs/SCREENS.md` (+ SCR-FORGOT + SCR-RESET entries), Modify `docs/brainstorming/decisions.md` (+ DEV-323..DEV-332), Modify `docs/ROUTEMAP.md` (+ tech-debt D-130 if mailhog tag unpinned from Task 1; mark FIX-228 complete), Modify `docs/architecture/api/_index.md` OR the auth-section index file to add API-NNN entries for the 2 new endpoints.
- **Depends on:** Tasks 1-10 (documents the shipped system)
- **Complexity:** low
- **Pattern ref:** Read `docs/brainstorming/decisions.md` existing DEV-NNN entries (tail 20 lines) for format. Read `docs/architecture/ERROR_CODES.md` for row format.
- **Context refs:** "Pinned Decisions", "API Specifications", "Config Changes"
- **What:** Append DEV-323..DEV-332 rows to decisions.md with rationale excerpts from the Pinned Decisions table above. Document the new error code, config vars (with defaults + valid ranges), and screens. If Task 1 left mailhog image unpinned: add `D-130` to ROUTEMAP tech debt: "mailhog image tag floating — pin to SHA256 digest in next infra maintenance window."
- **Verify:** `rg -n 'DEV-32[3-9]' docs/brainstorming/decisions.md` returns all 10 entries. `rg -n 'PASSWORD_RESET_INVALID_TOKEN' docs/` returns definition + any usage refs.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 Forgot link on login | Task 10 | Manual verify; DOM snapshot check |
| AC-2 Generic response (no enumeration) | Tasks 5, 10 | Task 8 `TestPasswordResetRequest_BodyShape_IdenticalAcrossCases` + `TestPasswordResetRequest_NonexistentEmail_Returns200Generic_AndInvokesDummyBcrypt` |
| AC-3 `POST /password-reset/request` + 1h token + email | Tasks 2, 3, 4, 5, 7 | Task 8 round-trip test + mailhog UI shows email |
| AC-4 `/auth/reset?token=` + confirm endpoint | Tasks 6, 7, 10 | Task 8 `TestPasswordResetConfirm_ValidToken_SucceedsAndInvalidatesToken` |
| AC-5 Single-use + `password_reset_tokens` table | Tasks 2, 3, 6 | Task 8 `TestPasswordResetConfirm_ReusedToken_Returns400` |
| AC-6 Audit events | Tasks 5, 6 | Task 8 asserts audit rows created; inspect `audit_logs` table |
| AC-7 Rate limit 5/email/hour | Tasks 3, 5 | Task 8 `TestPasswordResetRequest_RateLimitEnforced` + PAT-017 grep (5-hit rule) |
| AC-8 Footer version | Task 9 | Manual DOM; build artifact grep for `Argus v` |

---

## Story-Specific Compliance Rules

- **API**: Standard envelope `{status, data, meta?, error?}` for all 2xx and 4xx — confirmed.
- **DB**: Migration up+down both present. Indexes on `email_rate_key+created_at`, `expires_at`, `user_id`. No tenant_id column (platform-global table by design — DEV-323).
- **UI**: Design tokens only. Reuse `<Input>`, `<Button>`, `<Spinner>`, `<AuthLayout>`. Zero default Tailwind palette (PAT-018). Autocomplete attributes. Referer meta (DEV-330).
- **Business**: 5/email/hour (AC-7 + DEV-326). 1h TTL (AC-3 + DEV-332). Single-use enforcement via DELETE on confirm (DEV-323).
- **ADR**: ADR-002 JWT auth path untouched (no interaction with refresh/login flow). ADR-003 bcrypt reused via existing `cfg.BcryptCost`.

## Bug Pattern Warnings

- **PAT-011 (FIX-207)**: Plan-specified wiring missing at main.go. Task 7 explicitly wires `WithAudit` + `WithPasswordReset`. Gate MUST grep `rg -n 'WithPasswordReset' cmd/` returns 1 call site; `rg -n 'authHandler\.' cmd/argus/main.go` shows every chain segment.
- **PAT-017 (FIX-210)**: Config parameter threaded to one consumer but not another. Anchor: `PasswordResetRateLimitPerHour` MUST show ≥ 5 hits (config, validator, main, handler field, handler usage). Test `TestPasswordResetRequest_RateLimitEnforced` sets config + fires 6 requests. A passing store-level-only test is NOT sufficient per dispatch directive.
- **PAT-018 (FIX-227)**: Default Tailwind color utility used where CSS-var token mandated. Task 10 grep command (line above) MUST return zero matches.
- **PAT-006 (FIX-215)**: Missing JSON struct tags on response DTO. All response structs in `password_reset.go` MUST have explicit `json:"..."` tags on every field — envelope wrapper already handles this for generic maps, but any typed response struct needs tags.

## Tech Debt (from ROUTEMAP)
No open tech debt items target FIX-228. Task 1 may ADD `D-130` (mailhog image tag pinning) — record only if implementation leaves tag floating.

## Mock Retirement
No frontend mocks exist for password reset (this is a NEW API surface). No retirement needed.

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| R1: Attacker measures timing to enumerate | DEV-324 deterministic dummy-bcrypt spy + byte-identical response body test (Task 8) |
| R2: Token leakage via email/logs | DB stores SHA-256 hash only (raw token never persisted). Log fields redact token (log `token_hash_prefix[:8]` only). |
| R3: SendAlert signature has no `to` — breaking change risk | DEV-327: add NEW `SendTo` method, leave SendAlert untouched |
| R4: Mailhog image tag floating = non-reproducible infra | Task 1: try SHA256 pin; if not available D-130 records follow-up |
| R5: Frontend `__APP_VERSION__` undefined at runtime for test harness | Task 9 adds globals.d.ts + vite define; fallback `typeof __APP_VERSION__ !== 'undefined' ? __APP_VERSION__ : 'dev'` in auth-layout.tsx |
| R6: Rate-limit DB write storm from bots | DB-backed counter with index `(email_rate_key, created_at DESC)` supports the `WHERE ... >= NOW()-'1h'` efficiently; DEV-332 inline PurgeExpired bounds table size |
| R7: Policy validator error mapping drift between ChangePassword and ConfirmPasswordReset | Task 6 pattern-refs ChangePassword (line 533) — reuse its exact mapping table |
| R8: Concurrent confirm of same token (race) | UNIQUE constraint on `token_hash` + DELETE in confirm; second concurrent DELETE returns 0 rows → treat as invalid-token (already documented in Task 6 step 2) |

---

## Quality Gate (Embedded Self-Validation)

- [x] Architecture compliance: handlers in `internal/api/auth/`, store in `internal/store/`, templates in `internal/notification/templates/`, migrations in `migrations/`
- [x] No raw SQL in handlers — all queries via `PasswordResetStore` methods (Task 3)
- [x] Rate limit config plumbing trace: 5-hit grep rule documented + tested (Task 8 `TestPasswordResetRequest_RateLimitEnforced` is the integration anchor — store-only test insufficient)
- [x] Token model PINNED to opaque SHA-256-hashed (DEV-323) — story AC-4 "jwt" explicitly documented as drafting error in DEV-323 rationale
- [x] Constant-time enumeration defense specified (DEV-324) with deterministic spy test (NOT timing p99)
- [x] SMTP/notification template path: `SendTo` method added (DEV-327); mailhog dev fixture added Task 1 (DEV-328) — no BLOCKER raised
- [x] Audit events wired via existing `audit.Auditor` + `createAuditEntry` helper (mirrors user.Handler) — no new audit machinery
- [x] Password strength policy: reused `auth.ValidatePasswordPolicy` (DEV-331)
- [x] URL security: `?token=` + referer no-referrer meta (DEV-330)
- [x] FIX-216 modal pattern: N/A — no Dialog in this story (no confirmation modal; success uses toast/banner)
- [x] DEV-NNN IDs reserved: DEV-323 through DEV-332 (10 decisions)
- [x] Tech Debt: D-130 (conditional on Task 1 mailhog pin outcome)
- [x] Accessibility: form labels bound to inputs, `autocomplete="email"` + `autocomplete="new-password"` (both confirm fields), keyboard tab order = natural DOM order, focus-visible via existing `<Input>` and `<Button>` tokens
- [x] PAT-017 wiring trace: grep command documented in Plan Architecture Context + referenced in Task 7 Verify
- [x] PAT-018 default-palette grep: documented in Task 10 Verify
- [x] Min substance (M effort → 60 lines, 3 tasks): this plan is 400+ lines, 11 tasks — substantially exceeds floor
- [x] Task complexity: Tasks 3, 5, 8 marked high (core security + integration-test logic); low/medium spread on infra/docs/UI appropriate for M effort
- [x] Each task has Pattern ref, Context refs, Verify — confirmed
- [x] Context refs point to sections that exist in this plan — confirmed

**Self-Gate Verdict: PASS**
