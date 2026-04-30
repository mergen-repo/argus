# FIX-223 — Gate Scout: Test/Build

Scout executed inline by Gate Lead (subagent nesting constraint).

## Commands Run
| Check | Command | Result |
|-------|---------|--------|
| Go build | `go build ./...` | PASS |
| Go vet | `go vet ./...` | PASS |
| Store tests | `go test ./internal/store/...` | 452 PASS |
| IP-pool handler tests (pre-fix) | `go test ./internal/api/ippool/...` | 37 PASS |
| IP-pool handler tests (post-fix) | `go test ./internal/api/ippool/...` | 38 PASS (+1 new) |
| Full Go suite | `go test ./...` | 3520 PASS (109 packages) |
| TypeScript | `cd web && npx tsc --noEmit` | PASS |
| Vite build | `cd web && npm run build` | PASS (2.63s) |
| Raw HTML button scan | `grep '<button ' ip-pool-detail.tsx apns/detail.tsx` | 0 matches |
| Hex color scan | `grep '#[0-9A-Fa-f]{6}' ip-pool-detail.tsx apns/detail.tsx` | 0 matches |
| Migration up/down lint | Visual SQL review | OK — symmetric ALTER TABLE pair, idempotent |

<SCOUT-TESTBUILD-FINDINGS>
F-B1 | LOW | testbuild
- Title: No handler-level test for the new `q` query parameter
- File: internal/api/ippool/handler_test.go
- Detail: `ListAddresses` grew a new input (`q`, trimmed, len ≤ 64) that previously had zero test coverage. Existing `TestListAddressesInvalidID` only covers UUID-parse failure.
- Fixable: YES (add TestListAddressesRejectsLongQ covering `q` length guard)
- Severity: LOW — now FIXED

F-B2 | NOTE | testbuild
- Title: db-migrate was skipped — Docker not running in Gate environment
- Detail: The migration file is syntactically correct and symmetric with its down-pair, but `make db-migrate` was not executed end-to-end. Not a blocker — the CI/CD pipeline runs migrations during deploy, and the SQL is a trivial `ADD COLUMN IF NOT EXISTS`. No behavioral change possible.
- Fixable: N/A (environmental; will run on first deploy)

F-B3 | PASS | testbuild
- Title: Full suite regression-clean
- Evidence: 3520 Go tests PASS, including `gx_ipalloc_test.go` which exercises the FOR-UPDATE / SKIP-LOCKED allocation paths that rely on the unjoined `ipAddressColumns`. DEV-306 correctness verified empirically.
- Fixable: N/A
</SCOUT-TESTBUILD-FINDINGS>
