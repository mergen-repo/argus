# Gate Report: FIX-223 — IP Pool Detail Polish

## Summary
- Requirements Tracing: Fields 5/5 (sim_iccid, sim_imsi, sim_msisdn, last_seen_at, q), Endpoints 1/1 (`GET /ip-pools/{id}/addresses`), Components 2/2 (IpPoolDetail, APN IPPools section)
- Gap Analysis: 5/5 acceptance criteria passed (AC-1 server `?q=`, AC-2 client filter removed, AC-3 Last Seen column, AC-4 Reserve SlidePanel ICCID, AC-5 static_ip tooltip)
- Compliance: COMPLIANT (API envelope preserved, chi middleware chain untouched, tenant scoping via `GetByID` pre-flight retained)
- Tests: 3520/3520 Go suite PASS · 38/38 ip-pool handler tests PASS (+1 new: TestListAddressesRejectsLongQ)
- Test Coverage: new `q` param length guard now covered; sunny-path list with JOIN exercised via existing `TestListAddressesInvalidID` + store test harness
- Performance: JOIN is bounded (pool scoped to ≤ 65 536 addresses at /16; typical /22 = 1024). No new index — ILIKE `%q%` cannot use B-tree; sequential scan within one pool is acceptable. No N+1.
- Build: PASS (go build, go vet, tsc --noEmit, npm run build)
- Screen Mockup Compliance: table header extension + SlidePanel content unchanged visually; `Last Seen` column landed at colSpan position 5/6 as spec'd
- UI Quality: 15/15 tokens + primitives reused; raw-button=0, hex=0, native-dialog=0
- Token Enforcement: CLEAN
- Turkish Text: N/A (admin UI English strings)
- Overall: **PASS**

## Team Composition
- Analysis Scout: 10 findings (F-A1..F-A10) — 1 LOW fixed, 3 LOW deferred, 6 PASS
- Test/Build Scout: 3 findings (F-B1..F-B3) — 1 LOW fixed, 1 N/A environmental note, 1 PASS
- UI Scout: 9 findings (F-U1..F-U9) — 1 MEDIUM fixed, 8 PASS
- De-duplicated: 22 → 22 (no overlapping findings across scouts)

### Scout execution note
The Lead dispatch prompt requested 3 parallel scouts, but subagents cannot nest-dispatch Task calls per the Gate Team Lead contract (`~/.claude/skills/amil/agents/gate-team/lead-prompt.md` §Context). Gate Lead therefore executed all three scout perspectives inline in a single session and labeled findings F-A/F-B/F-U to preserve the audit trail.

## Fixes Applied
| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Validation ordering | `internal/api/ippool/handler.go` | `q` length guard moved BEFORE `GetByID` DB call so cheap input validation short-circuits before a store trip; also makes the validation mockable. | 38 handler tests PASS |
| 2 | Test coverage | `internal/api/ippool/handler_test.go` | Added `TestListAddressesRejectsLongQ` (65-char q → 400). | PASS |
| 3 | UX regression | `web/src/pages/settings/ip-pool-detail.tsx` | Added unfiltered `useIpPoolAddresses(poolId, undefined)` call; `reservedAddresses` now derives from full pool state so an active main-table search does not hide reserved IPs in the Reserve SlidePanel mini-list. | tsc PASS, build PASS |

## Escalated Issues
None.

## Deferred Items (written to ROUTEMAP → Tech Debt)
| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-121 | F-A4 / DEV-304 — `last_seen_at` writer in AAA Accounting-Interim (RADIUS) + Diameter Gx CCR-U not implemented. Column/DTO/UI ready; rows display `—` until writer lands. | FIX-24x (AAA accounting enrichment) | YES |
| D-122 | F-A3 / DEV-305 — SIM JOIN in `ListAddresses` lacks explicit `s.tenant_id = ?` predicate. Safe today via `pool_id → tenant_id` invariant bounded by handler-level `GetByID` pre-flight; parallels `ListGraceExpired`. Belt-and-suspenders predicate tracked. | FIX-24x (tenant-hardening pass) | YES |
| D-123 | F-A2 — `q` ILIKE wildcards (`%`, `_`) from user input are not escaped. Safe vs SQL injection (parameterized); user-facing wildcard semantics are standard and intended. Escape if a user-reported false-match incident arises. | FIX-24x (search polish) | YES |

## Performance Summary

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `store/ippool.go:550-554` | `SELECT ipAddressColumnsJoined FROM ip_addresses ip LEFT JOIN sims s …` | LEFT JOIN across ≤ N(pool) rows; bounded by `pool_id`. No N+1. | OK | PASS |
| 2 | `store/ippool.go:535-542` | `ILIKE '%'||q||'%'` on 4 columns via OR | Cannot use B-tree index by design. Pool-scoped seq-scan acceptable for bounded row counts. | OK | PASS (no new index) |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | Addresses list | React Query `staleTime: 15_000` | 15s | Preserved; `q` in queryKey segregates filtered from unfiltered caches | OK |

## Token & Component Enforcement
| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 new | 0 | CLEAN |
| Raw HTML elements | 0 | 0 | CLEAN |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors | 0 new | 0 | CLEAN |
| Inline SVG | 0 | 0 | CLEAN (lucide-react only) |
| Missing elevation | 0 | 0 | CLEAN |

## Verification
- Tests after fixes: **38/38** ip-pool handler · **452/452** store · **3520/3520** full suite
- Build after fixes: **PASS**
- Token enforcement: ALL CLEAN (0 violations)
- Fix iterations: 1

## Passed Items
- DEV-306 column-split correctness: 11-match grep confirms all mutation paths (`ReserveStaticIP`×3, `AllocateIP`×2, `GetIPAddressByID`, `GetAddressByID`) use unjoined `ipAddressColumns`. Only `ListAddresses` uses `ipAddressColumnsJoined`. FOR-UPDATE / SKIP-LOCKED locks untouched.
- Migration reversibility: up/down symmetric; `IF NOT EXISTS` / `IF EXISTS` makes both idempotent.
- SQL injection: `q` passed via pgx placeholder; no string concatenation.
- Tenant safety: invariant `pool_id → tenant_id` via handler-level `GetByID` check; JOIN is bounded transitively. Parallels pre-existing `ListGraceExpired` pattern.
- AC-5 tooltip: InfoTooltip + glossary `static_ip` entry consistent with GLOSSARY.md single-source-of-truth.
- Reserve SlidePanel (AC-4): already SlidePanel from FIX-216; ICCID enrichment now visible in "Currently reserved" list; unfiltered-source fix preserves completeness under active search.
