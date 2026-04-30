# FIX-223: IP Pool Detail Polish — Server-side Search, Last Seen, Reserve Modal ICCID

## Problem Statement
IP Pool Detail page: search is client-side only (fails at scale — F-306); "Last Seen" column missing; Reserve IP modal doesn't show ICCID of reserving SIM.

## User Story
As a pool admin, I want to search IPs server-side, see when each IP was last used, and confirm SIM identity during reservation.

## Findings Addressed
F-74, F-75, F-76, F-78, F-79, F-306 (server-side search deep-trace)

## Acceptance Criteria
- [ ] **AC-1:** Backend `ListAddresses` accepts `?q=<string>` param — SQL `WHERE address_v4::text LIKE %q% OR reserved_by_note ILIKE %q%`. Debounced 300ms on FE.
- [ ] **AC-2:** FE removes client-side filter; uses API call per search.
- [ ] **AC-3:** Column "Last Seen" added — from `ip_addresses.last_seen_at` (may need DB column add if missing).
- [ ] **AC-4:** Reserve IP Modal (SlidePanel per FIX-216) shows: Target SIM selector with ICCID + IMSI + current IP; IP preview + conflict check.
- [ ] **AC-5:** Static IP decision per F-79 — documented in APN config UI (FIX-222 tooltip).

## Files to Touch
- `internal/api/ippool/handler.go::ListAddresses` — add q param
- `internal/store/ippool.go` — query support
- `web/src/pages/settings/ip-pool-detail.tsx`

## Risks & Regression
- **Risk 1 — Search query index:** Consider `ADD INDEX` on `address_v4::text` if missing for LIKE performance.

## Test Plan
- /22 pool (1024 IPs) — search specific IP, server-side finds in any page
- Reserve flow: modal shows SIM details

## Plan Reference
Priority: P2 · Effort: M · Wave: 6
