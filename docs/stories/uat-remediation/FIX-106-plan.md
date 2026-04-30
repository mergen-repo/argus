# FIX-106 Plan: Operator Test Connection — Adapter Registry + Mock Seed Schema

**Bug**: `POST /operators/{id}/test` returns 500 for multi-protocol operators; Mock Simulator seed has incorrect adapter_config shape
**Effort**: M-L (4 tasks)
**Regression Risk**: Low — changes are in error mapping, seed data, and a new idempotent migration; no adapter protocol logic changes

---

## Root Cause Analysis

### F-6: 500 "Failed to create adapter" for multi-protocol operators

**Root cause is NOT a missing registry entry.** `NewRegistry()` (`internal/operator/adapter/registry.go:35-49`) registers factories for all 5 protocols (mock, radius, diameter, sba, http). The 500 happens because:

1. `DerivePrimaryProtocol` returns the first enabled protocol in canonical order: `diameter > radius > sba > http > mock` (defined in `adapterschema/schema.go:29`).
2. For an operator with `enabled_protocols=[diameter,radius,mock]`, primary = `diameter`.
3. `testConnectionForProtocol` extracts the diameter sub-config via `SubConfigRaw`, passes it to `GetOrCreate`, which calls `NewDiameterAdapter`.
4. `NewDiameterAdapter` requires `host != ""` (`diameter.go:43-44`). If the diameter sub-config was created via UI with `enabled:true` but no `host`, the factory returns an error.
5. `testConnectionForProtocol` returns this error as `(testResponse{}, 500, err)` (handler.go:1118-1119).
6. The legacy `TestConnection` handler catches `tcErr != nil` and writes a bare `500 INTERNAL_ERROR "Failed to create adapter"` (handler.go:1183-1186), with no actionable detail.

**Fix**: Classify adapter factory errors as 422 `ADAPTER_CONFIG_INVALID` instead of 500, including the factory's error message in the response.

### F-7: "Operator has no enabled protocol" for Mock Simulator

**Root cause**: Stale UAT database. Current seed (`002_system_data.sql:16`) already has `{"mock":{"enabled":true,...}}`, so `DerivePrimaryProtocol` returns `"mock"`. The error only fires if the UAT database was seeded from a pre-STORY-090 snapshot where the mock operator had flat config `{"host":"localhost","port":1812}` without `enabled:true`. The lazy up-convert in `decryptAndNormalize` wraps flat configs into nested with `enabled:true`, but only if heuristic detection succeeds. The flat mock config `{"host":"localhost","port":1812}` has no mock heuristic keys (`latency_ms`, `simulated_imsi_count`, etc.) — `DetectShape` returns `ErrShapeUnknown` and with no `adapter_type` hint (column dropped in STORY-090), `UpConvert` returns `ErrUpConvertMissingHint`. This causes `decryptAndNormalize` to error, and `resolveNestedAdapterConfig` falls back to raw bytes which fail `ParseNested`.

**Fix**: Update seed to use correct mock-specific fields so heuristic detection works AND up-convert path handles this edge case.

### F-8: Mock Simulator adapter_config uses wrong fields

**Root cause**: Seed `002_system_data.sql:16` has `{"mock":{"enabled":true,"host":"localhost","port":1812}}`. Fields `host` and `port` are RADIUS adapter fields, not MockConfig fields. MockConfig (`adapter/mock.go:13-20`) uses `latency_ms`, `fail_rate`, `success_rate`, `healthy_after`, `error_type`, `timeout_ms`. The mock adapter silently ignores unknown fields but gets default values (latency_ms=10), which is functional but misleading. More importantly, `host:localhost` and `port:1812` suggest the config was copy-pasted from a RADIUS template and never corrected for the mock adapter's actual semantics.

**Fix**: Rewrite seed to `{"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}` matching mock adapter's actual config fields.

---

## Design Decisions

### AC-2: DerivePrimaryProtocol for multi-protocol operators

**Decision: Keep current behavior** — `DerivePrimaryProtocol` returns the first enabled protocol in canonical order (`diameter > radius > sba > http > mock`). This is already deterministic and correct for the legacy single-protocol TestConnection endpoint. The per-protocol endpoint (`POST /operators/{id}/test/{protocol}`) already allows explicit protocol targeting. No redesign needed.

**Rationale**: The 500 error is caused by factory validation failure on incomplete config, not by incorrect protocol selection. The fix is to classify factory errors as 422 with actionable detail, not to change protocol derivation logic.

### AC-8: Migration for legacy flat adapter_config

**Decision: Write an explicit idempotent SQL migration** rather than relying on seed-only fix. Reason:
- `decryptAndNormalize` does lazy up-convert on read, but it silently fails for flat mock configs without heuristic keys (see F-7 analysis).
- An explicit migration guarantees all rows are in nested shape before the next UAT run.
- Pre-release, so no production data risk. The migration is cheap and safe.
- Migration must handle the encryption: if `adapter_config` is encrypted, it should skip the row (the lazy up-convert handles encrypted rows via `decryptAndNormalize` on next read). For unencrypted dev/test databases, the migration can directly transform the JSON.

---

## Tasks

### Task 1: Fix error classification in testConnectionForProtocol (AC-1, AC-4)

**Files**:
- `internal/api/operator/handler.go` — `testConnectionForProtocol`, `TestConnection`, `TestConnectionForProtocol`
- `internal/apierr/apierr.go` — add `CodeAdapterConfigInvalid = "ADAPTER_CONFIG_INVALID"`

**Changes**:
1. In `testConnectionForProtocol` (line 1117-1119): When `GetOrCreate` returns an error, classify it:
   - If the error is `ErrUnsupportedProtocol` → return `(testResponse{}, 400, nil)` with appropriate error (should not happen since `IsValidProtocol` gates earlier, but defensive)
   - Otherwise (factory validation failure) → return `(testResponse{}, 422, nil)` and construct a `testResponse` with `Success:false, Error: err.Error()` so the caller can emit `422 ADAPTER_CONFIG_INVALID` with the factory's specific error message
2. In `TestConnection` (line 1183-1186): Replace the bare `500 INTERNAL_ERROR "Failed to create adapter"` with the classified error from step 1. When `tcErr != nil`, emit `422 ADAPTER_CONFIG_INVALID` with the error detail.
3. In `TestConnectionForProtocol` (line 1231-1233): Same treatment as step 2.
4. Add `CodeAdapterConfigInvalid` constant to `apierr.go`.

**Important distinction**: This fix only reclassifies *adapter factory* errors (config validation at construction time) to 422. HealthCheck errors (network timeout, dial refused, peer unreachable) continue to return `200 + success:false` — this is correct existing behavior and matches AC-4's intent. Connection-level failures are not 5xx; they are a successful test run whose result is "connection failed."

**Test**: Verify existing tests still pass. Add a test case where `testConnectionForProtocol` is called with a config that will fail factory validation (e.g., RADIUS sub-config without `shared_secret`) and assert 422 instead of 500.

### Task 2: Fix Mock Simulator seed data (AC-6, AC-7)

**Files**:
- `migrations/seed/002_system_data.sql`

**Changes**:
1. Update Mock Simulator `adapter_config` from:
   ```json
   {"mock":{"enabled":true,"host":"localhost","port":1812}}
   ```
   to:
   ```json
   {"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}
   ```
   This uses actual MockConfig fields, matches the mock sub-config pattern used in 003/005 seeds for the real operators' mock siblings, and ensures heuristic detection works (`latency_ms` is a mock heuristic key in `detect.go:35`).

**Test**: `make db-seed` succeeds. `POST /operators/{mock_id}/test` returns 200 with `success:true`.

### Task 3: Idempotent migration for legacy flat adapter_config (AC-8)

**Files**:
- `migrations/YYYYMMDDHHMMSS_fix_legacy_flat_adapter_config.up.sql` (new)
- `migrations/YYYYMMDDHHMMSS_fix_legacy_flat_adapter_config.down.sql` (new, no-op)

**Changes**:
1. Write an idempotent SQL migration that:
   - Scans `operators` for rows where `adapter_config` is a JSON object but does NOT have any key in the protocol set (`radius`, `diameter`, `sba`, `http`, `mock`) at the top level — i.e., it's a flat config.
   - For each such row, detects the protocol via heuristic keys (same logic as Go's `DetectShape`):
     - Has `shared_secret` or `listen_addr` → wrap under `"radius"`
     - Has `origin_host` or `origin_realm` → wrap under `"diameter"`
     - Has `nrf_url` or `nf_instance_id` → wrap under `"sba"`
     - Has `base_url` or `auth_type` → wrap under `"http"`
     - Has `latency_ms` or `simulated_imsi_count` or `fail_rate` → wrap under `"mock"`
     - None match → wrap under `"mock"` with `enabled:true` as a safe fallback (the Mock Simulator catch-all in `002_system_data.sql` is the only known ambiguous case)
   - Wraps the flat config into `{"<protocol>": {"enabled": true, ...originalFields}}`.
   - Only updates rows where the top-level JSON is NOT already in nested shape (idempotent).
   - Skips encrypted adapter_config (first byte is `"` not `{`) — those are handled by the lazy up-convert in Go.
2. Down migration: no-op comment (pre-release, no rollback needed; the nested shape is backward-compatible via `decryptAndNormalize`).

**Test**: Apply migration to a test DB with a manually-inserted flat-config operator row, verify it gets converted.

### Task 4: Unit tests for adapter factory error path + DerivePrimaryProtocol coverage (AC-5)

**Files**:
- `internal/api/operator/handler_test.go`
- `internal/operator/adapterschema/schema_test.go`

**Changes**:
1. **handler_test.go** — Add test cases:
   - `TestTestConnection_PerProtocol_HelperAdapterFactoryError`: Call `testConnectionForProtocol` with a RADIUS sub-config missing `shared_secret` → assert 422 (not 500), assert response contains "shared_secret" in error message.
   - `TestTestConnection_PerProtocol_HelperDiameterFactoryError`: Same for diameter missing `host` → assert 422.
   - `TestTestConnection_PerProtocol_HelperHTTPFactoryError`: Same for HTTP missing `base_url` → assert 422.
2. **schema_test.go** — Add test case:
   - `TestDerivePrimaryProtocol_MultiProtocol`: Nested config with `diameter:enabled + radius:enabled + mock:enabled` → assert primary is `"diameter"` (canonical order).
   - `TestDerivePrimaryProtocol_SkipsDisabledFirst`: Nested config with `diameter:disabled + radius:enabled + mock:enabled` → assert primary is `"radius"`.

---

## Files Affected Summary

| File | Change Type |
|------|-------------|
| `internal/api/operator/handler.go` | Edit — error classification in 3 functions |
| `internal/apierr/apierr.go` | Edit — add `CodeAdapterConfigInvalid` |
| `migrations/seed/002_system_data.sql` | Edit — fix mock adapter_config |
| `migrations/YYYYMMDDHHMMSS_fix_legacy_flat_adapter_config.up.sql` | New — idempotent migration |
| `migrations/YYYYMMDDHHMMSS_fix_legacy_flat_adapter_config.down.sql` | New — no-op down |
| `internal/api/operator/handler_test.go` | Edit — add factory error tests |
| `internal/operator/adapterschema/schema_test.go` | Edit — add DerivePrimaryProtocol coverage |

**Total: 7 files (5 edits + 2 new)**

---

## Verification Checklist

- [ ] `make test` passes (all existing + new tests)
- [ ] `make db-seed` succeeds cleanly
- [ ] `POST /operators/{mock_id}/test` → 200, success:true
- [ ] `POST /operators/{turkcell_id}/test` → 200 (primary=radius, connects or timeout, not 500)
- [ ] `POST /operators/{turkcell_id}/test/mock` → 200, success:true
- [ ] Operator with incomplete diameter config → `POST .../test` → 422 `ADAPTER_CONFIG_INVALID` (not 500)
- [ ] UAT-001 Step 4 can complete without any 500 errors
