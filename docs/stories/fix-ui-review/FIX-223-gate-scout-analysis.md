# FIX-223 — Gate Scout: Analysis

Scout executed inline by Gate Lead (subagent nesting constraint; scout dispatch architecture requires main-session delegation, not available here).

## Scope
Static review of FIX-223 changes in `internal/store/ippool.go`, `internal/api/ippool/handler.go`, `migrations/20260424000003_ip_addresses_last_seen.{up,down}.sql`, plus mutation-path parity audit (DEV-306).

<SCOUT-ANALYSIS-FINDINGS>
F-A1 | LOW | analysis
- Title: Handler ordered `GetByID` before cheap query-param validation
- File: internal/api/ippool/handler.go::ListAddresses
- Detail: The `q` length guard sat AFTER `ippoolStore.GetByID` — so a request with q>64 would hit the DB first, only to be rejected on a free input check. Defensive ordering should validate cheap user inputs before any DB trip; also blocks easy mock-based unit testing of the validation path.
- Fixable: YES (reorder validation to precede store call)
- Severity: LOW

F-A2 | LOW | analysis
- Title: LIKE wildcard characters in user `q` are unescaped
- File: internal/store/ippool.go::ListAddresses (q branch)
- Detail: `q := "%" + userInput + "%"` is parameterized via pgx (safe from SQL injection), but `%` and `_` from the user act as LIKE wildcards. A user typing `10.0.1._` matches `10.0.1.0`..`.9`. Not a security issue; behavior is consistent with typical search UX; escaping `%`/`_` would add little value at this stage. Defer.
- Fixable: NO (plan did not require; behavior is standard non-SQL-literal ILIKE)
- Severity: LOW → DEFERRED

F-A3 | LOW | analysis
- Title: DEV-305 tenant predicate on SIM JOIN tracked but not enforced
- File: internal/store/ippool.go::ListAddresses
- Detail: JOIN uses `ON s.id = ip.sim_id` with no `s.tenant_id = ?` predicate. Safe today because `pool_id → tenant_id` invariant bounds the result set via the pre-flight `GetByID(tenantID, poolID)` check in the handler. Parallels existing `ListGraceExpired` (line 856-885 of ippool.go) which also JOINs sims without explicit tenant predicate. Acceptable ship-as-is — defer explicit belt-and-suspenders predicate.
- Fixable: NO in this story (architectural invariant audit needed)
- Severity: LOW → DEFERRED

F-A4 | LOW | analysis
- Title: DEV-304 `last_seen_at` writer missing (AAA Accounting-Interim / Diameter CCR-U)
- File: internal/aaa/radius/*, internal/aaa/diameter/* (not touched this story)
- Detail: Column + DTO + UI all in place; column stays NULL until the AAA protocol paths update it. Plan explicitly deferred writer. UI renders `—`.
- Fixable: NO (separate feature)
- Severity: LOW → DEFERRED

F-A5 | PASS | analysis
- Title: DEV-306 column-constant split verified — mutations all use unjoined
- Evidence: grep across ippool.go shows `ipAddressColumns` (unjoined 10 cols) used by all mutation paths (`ReserveStaticIP` x3, `AllocateIP` x2, `GetAddressByID` inline, `GetIPAddressByID`). `ipAddressColumnsJoined` (13 cols) used ONLY in `ListAddresses`. No `FOR UPDATE`/`SKIP LOCKED` path cross-contaminated with the JOIN. Correctness preserved.
- Fixable: N/A

F-A6 | PASS | analysis
- Title: SQL injection surface on `q`
- Evidence: `q` interpolated via pgx placeholder `$N` (not string-concatenated). Handler trims + enforces len ≤ 64 before store. No injection vector.
- Fixable: N/A

F-A7 | PASS | analysis
- Title: Empty `q` yields no WHERE predicate on search terms
- Evidence: `if q != ""` guard at ippool.go:535. Empty string or absent param skips the ILIKE branch entirely — full list returned, preserving AC-1 semantics.
- Fixable: N/A

F-A8 | PASS | analysis
- Title: Cursor pagination with `q` — limit+1 peek correct with JOIN
- Evidence: LEFT JOIN preserves one row per ip_addresses row; cursor still `ip.address_v4 > $N::inet`. Per-page sparsity under filter is expected and handled by existing peek semantics.
- Fixable: N/A

F-A9 | PASS | analysis
- Title: Migration up/down symmetric and idempotent
- Evidence: `ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ` ↔ `DROP COLUMN IF EXISTS last_seen_at`. No index, no backfill. Reversible.
- Fixable: N/A

F-A10 | PASS | analysis
- Title: No race risk introduced
- Evidence: Lists are read-only; mutation paths unchanged; new column is NULLable and written only by a future AAA writer. No cross-transaction lock or invariant altered.
- Fixable: N/A
</SCOUT-ANALYSIS-FINDINGS>
