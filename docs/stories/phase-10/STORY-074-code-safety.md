# STORY-074: Code Safety & Config Hardening

## User Story
As a platform engineer deploying Argus to production, I want every goroutine crash-safe with recover, every config default fail-safe, every dev-mode footgun eliminated, and every error message scrubbed of stub-exposing strings, so that a single panic, a misconfigured env var, or a leaked log line cannot crash the system, open CORS to the world, or reveal incomplete integrations to customers.

## Description
Deep scan found 14 goroutines without recover (crash-unsafe), `AppEnv` defaulting to "development" (CORS opens if env unset), `DevCORSAllowAll` defaults true with `IsDev()` check instead of `IsProd()` default-deny, 4 unused dev config flags creating false security, Mock SM-DP+ adding artificial 50ms latency in production, 5 error messages leaking "not yet implemented" strings, frontend cast chain defaulting WS connection to `true`, and hardcoded API baseURL.

## Architecture Reference
- Packages: cmd/argus/main.go, internal/gateway, internal/config, internal/notification/sms, internal/aaa/sba/nrf, internal/job/stubs, internal/esim/smdp, internal/diagnostics, web/src/lib/api, web/src/components/layout/status-bar
- Source: Phase 10 deep placeholder scan (2026-04-11)

## Acceptance Criteria
- [ ] AC-1: **Goroutine panic recovery.** Create shared `func safeGo(name string, fn func())` helper that wraps `fn` in `defer func() { if r := recover() { log.Error().Str("goroutine", name).Interface("panic", r).Stack().Msg("goroutine panic recovered"); metrics.IncrCounter("argus_goroutine_panics_total", name) } }()`. Replace all 14 bare `go func() {...}()` calls: apikey_auth.go (1), ws/server.go (1), dashboard/handler.go (4), scheduler.go (1), timeout.go (1), radius/server.go (4), notification/delivery.go (1), main.go pprof (1). Audit for any others via grep `go func`.
- [ ] AC-2: **`AppEnv` default to `"production"`.** In `config.go`, change default from `"development"` to `"production"`. Explicit `APP_ENV=development` required for dev mode. This closes the CORS footgun (unset env = prod mode = strict CORS).
- [ ] AC-3: **CORS default-deny.** Change `if cfg.DevCORSAllowAll && cfg.IsDev()` pattern to: in `IsProd()` mode, CORS allowed origins read from `CORS_ALLOWED_ORIGINS` env var (comma-separated). Empty = same-origin only. In dev mode, allow all. Remove `DevCORSAllowAll` config flag entirely.
- [ ] AC-4: **Unused dev flags removed.** Remove from config struct: `DevSeedData`, `DevMockOperator`, `DevDisable2FA`, `DevLogSQL`. They were never read by any code, creating false security impression. If needed later, re-add with real implementation.
- [ ] AC-5: **Mock SM-DP+ latency removed from production.** `internal/esim/smdp.go` `simulateLatency()` guarded by `if os.Getenv("APP_ENV") == "development"` or removed entirely (mock adapter is only used when `ESIM_SMDP_PROVIDER=mock`).
- [ ] AC-6: **Error message leak scrubber.** All "not yet implemented" strings in production paths replaced with structured error codes: `notification/sms.go` → `ErrSMSProviderNotConfigured`, `aaa/sba/nrf.go` → `ErrNRFNotConfigured`, `job/stubs.go` → `ErrJobProcessorNotImplemented`, `diagnostics.go` → `ErrDiagnosticStepUnavailable`. Strings never appear in API responses or customer-visible logs. CI grep test: `grep -r "not yet implemented\|not implemented\|placeholder" internal/ cmd/ --include="*.go" | grep -v _test.go | grep -v mock` returns 0 matches.
- [ ] AC-7: **NRF all 4 placeholders.** Extend STORY-063 AC-8 scope: register + deregister + heartbeat + notifyStatus all 4 methods must be real when `SBA_NRF_URL` is set. When empty, return `ErrNRFNotConfigured` (from AC-6). No placeholder log messages.
- [ ] AC-8: **Frontend WS connection status.** Fix `status-bar.tsx:16` cast chain `(wsClient as unknown as { isConnected?: () => boolean }).isConnected?.() ?? true`. Replace with proper typed interface method. Default to `false` (not `true`) when method unavailable — show "disconnected" not fake "connected".
- [ ] AC-9: **Frontend API baseURL configurable.** `web/src/lib/api.ts` reads `import.meta.env.VITE_API_BASE_URL` with fallback to `/api/v1`. Documented in `.env.example`. Enables subdomain deployment.
- [ ] AC-10: **Diagnostics "Test authentication" step real.** `internal/diagnostics/diagnostics.go:373` "Test authentication not yet implemented" replaced with actual RADIUS test-auth attempt against operator adapter (or skip with documented reason if adapter doesn't support test-auth).

## Dependencies
- Blocked by: STORY-063 (NRF real registration), STORY-065 (metrics for panic counter)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] Unit: `safeGo("test", func() { panic("boom") })` → logs error with stack trace, does NOT crash process. Metric incremented.
- [ ] Integration: Start app with `APP_ENV` unset → CORS strict (same-origin only). Start with `APP_ENV=development` → CORS allows all.
- [ ] CI: `grep -r "not yet implemented" internal/ cmd/ --include="*.go" | grep -v _test.go | grep -v mock` → 0 matches.
- [ ] Integration: Mock SM-DP+ in production config (`APP_ENV=production`) → no artificial latency on eSIM operations.
- [ ] E2E: Kill WebSocket server → status bar shows "disconnected" (not fake "connected").
- [ ] E2E: Set `VITE_API_BASE_URL=https://api.argus.io/v1` → frontend calls that URL.

## Effort Estimate
- Size: M
- Complexity: Medium (mostly find-and-replace + shared helper)
