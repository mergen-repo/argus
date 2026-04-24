# FIX-228 Gate Report

**Story**: FIX-228 — Login Forgot Password Flow + Version Footer
**Date**: 2026-04-25
**Gate Lead**: Amil AUTOPILOT Gate Team
**Verdict**: PASS

## Scout Summary

| Scout | Findings | Notes |
|-------|----------|-------|
| Scout A (Analysis) | 4 | C-01, M-01, M-02, M-03 |
| Scout B (Test+Build) | 1 | F1 — stray `auth.test` binary; cleaned during gate (no source change) |
| Scout U (UI) | 0 | All PAT-018 + a11y greps clean |

After dedup: **4 unique findings**. F1 is a non-source artifact already cleaned.

## Merged Findings Table

| ID | Severity | File:Line | Disposition |
|----|----------|-----------|-------------|
| C-01 | CRITICAL | `cmd/argus/main.go:525-535` | FIX (typed-nil interface trap) |
| M-01 | MEDIUM | `web/src/pages/auth/reset.tsx:28-33` | FIX (inline invalid-token state) |
| M-02 | MEDIUM | `internal/api/auth/password_reset.go:55-58` | FIX (audit on 429 path) |
| M-03 | MEDIUM | `internal/store/password_reset_test.go` (7 sites) | FIX (FK seed helper) |

ESCALATE: 0 — DEFER: 0 — All four are in-scope and low-blast-radius.

## Fix Log

### C-01 — Typed-nil interface panic when SMTP unconfigured
- **File**: `cmd/argus/main.go:523-549`
- **What**: Removed `var smtpEmailSender *notification.SMTPEmailSender`. The construct + `WithPasswordReset` call now lives inside the `if cfg.SMTPHost != ""` branch; the `else` branch passes a literal `nil` to `WithPasswordReset`. A literal `nil` passed into an interface parameter produces a true nil interface, so the existing `if h.emailSender != nil` guard in `password_reset.go:97` works correctly and there is no nil-pointer deref on `SendTo`.
- **Why this option** (vs. exporting the interface or splitting types): the unexported `passwordResetEmailSender` interface lives in `internal/api/auth/handler.go`; exporting it would have rippling docs/API impact. The chosen path is purely a caller-side change (smaller scope; 1 file; preserves all existing tests).
- **Impact**: Eliminates the panic-after-token-persisted state in dev/empty-SMTP environments. Audit + writeGenericSuccess still emit; "no email sent" silent skip is the documented unconfigured-SMTP behavior.

### M-01 — Reset page redirected on missing token
- **File**: `web/src/pages/auth/reset.tsx:28-32`
- **What**: Changed the `useEffect` body from `toast.error(...) + navigate('/login')` to `setTokenInvalid(true)`. The dead `if (!token) return null` branch on line 109 is now unreachable in practice but harmless.
- **Impact**: SCR-RESET requirement met — missing/empty `?token=` renders the inline invalid-token panel with the "Yeni istek oluştur" link, no toast spam, no redirect leak.

### M-02 — Audit not emitted on rate-limit 429 path
- **File**: `internal/api/auth/password_reset.go:55-59`
- **What**: Added `h.createAuditEntry(r, "auth.password_reset_requested", rateKey, nil, map[string]any{"rate_limited": true})` BEFORE the 429 write.
- **Impact**: Rate-limited reset attempts now surface in the audit log with `meta.rate_limited=true` for forensics; AC-6 guarantees uniform "every request is audited."

### M-03 — Store integration tests violated FK to users
- **File**: `internal/store/password_reset_test.go` (helper at L48; 7 call-sites at L87, 120, 144, 170, 204, 248, 271)
- **What**: Added `seedPRUser(t, pool) uuid.UUID` helper that inserts a tenant + a user with valid columns (id, tenant_id, email, password_hash, name, role, state) and registers cascading cleanup (tokens → users → tenants). Replaced all 7 `userID := uuid.New()` + `cleanupPasswordResetUser(t, pool, userID)` pairs with a single `userID := seedPRUser(t, pool)` call.
- **Impact**: Tests will pass against a real DB instead of failing FK violation; old `cleanupPasswordResetUser` retained as dead helper (unused unexported func — no compile error in Go) for binary-compatibility with any future test that wants raw cleanup-only semantics. Leaving it is the minimum-diff choice; can be pruned in a follow-up.

## Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS (success) |
| `go vet ./...` | PASS (no issues found) |
| `go test ./internal/api/auth ./internal/store -short -count=1` | PASS (466 tests, 0 fail) |
| `cd web && npm run build` | PASS (0 TS errors; 2.49s build) |
| Regression grep `rate_limited` in `password_reset.go` | 1 hit (was 0) |
| Regression grep `setTokenInvalid(true)` in `reset.tsx` | 3 hits incl. the missing-token branch |
| Regression grep `seedPRUser` in `password_reset_test.go` | 11 hits (1 decl, 7 callers, 3 docstring/log refs) |
| Regression grep `cfg.SMTPHost` in `main.go` | 4 hits (guard in place; no typed-nil pattern) |

No new dependencies. No `go.mod` / `package.json` / `package-lock.json` changes.

## Bug-Pattern Candidate (defer to Commit step)

**PAT-019 candidate — Typed-nil pointer wrapped in non-nil interface**

- C-01 illustrates a Go-specific gotcha: a `var x *T` (nil pointer) assigned to an interface variable produces an interface value whose **type half is non-nil** even though the **value half is nil**. `if iface != nil` returns TRUE, but any method call dereferences the underlying nil pointer and panics.
- Greps confirm `docs/brainstorming/bug-patterns.md` has zero existing entries for this pattern (no matches for `typed.?nil|nil interface|interface.*nil`).
- Recommended PAT-019 entry (to be authored at Commit step, not at Gate):
  - **Pattern**: A typed-nil pointer assigned to an interface-typed parameter or field is NOT equal to nil; nil-checks pass and the next call panics.
  - **Detection**: grep for `var \w+ \*\w+` immediately followed by an interface-accepting function call without a guard, or a function signature taking an interface where the caller declares the concrete-pointer type.
  - **Fix recipe**: pass `nil` literal directly into the interface parameter on the unconfigured branch, OR define the variable with the interface type so it remains a true nil interface, OR guard with the concrete type's nilness before assigning.

## Final Verdict

**PASS** — All 4 findings fixed in-scope, all verifications green, no regressions, no escalations.
Ready to advance to Review + Finding Resolution → Commit.
