# FIX-235 Review — M2M eSIM Provisioning Pipeline (SGP.22 → SGP.02)

**Reviewer:** Reviewer Agent
**Date:** 2026-04-27
**Story size:** XL
**Gate verdict:** PASS (3896/3896 tests; 17 fixes applied at Gate; 8 deferred D-172..D-179)
**Review verdict:** PASS

---

## 1. Summary

FIX-235 delivered a complete M2M eSIM provisioning pipeline (GSMA SGP.02 push model) replacing the prior consumer-eSIM stubs (SGP.22). The implementation spans 3 new DB tables, 7 new BE packages/files, 4 new REST endpoints, 3 background jobs (OTA dispatcher, timeout reaper, stock alerter), and a major eSIM list/detail UI overhaul. All 22 planned tasks were executed and PASS-ed per the step-log.

---

## 2. Cross-doc Checks

| Check | Status | Notes |
|-------|--------|-------|
| ROUTEMAP D-172..D-179 present | PASS | All 8 deferred items confirmed at lines 809-816 |
| ROUTEMAP FIX-235 story entry | PASS | Line 464 |
| SCREENS.md SCR-070 annotated | DONE | FIX-235 column overhaul + BE endpoints documented |
| SCREENS.md SCR-041 annotated | DONE | eSIM Profiles tab added |
| SCREENS.md SCR-021 annotated | DONE | eSIM sub-tab + AllocateFromStockPanel + ProfileHistoryPanel |
| GLOSSARY.md new terms | DONE | SM-SR, OTA Command (M2M), eSIM Profile Stock, formatEID, Bulk Switch (M2M) — 5 rows added |
| USERTEST.md FIX-235 section | DONE | 10 scenarios added (UT-235-01..UT-235-10) |
| bug-patterns.md PAT-025 | DONE | EID/ICCID semantic column confusion — PAT-006 variant |
| decisions.md DEV-571..DEV-574 | DONE | Config defaults, PAT-017 compliance, HMAC replay window, filter-based bulk |
| ui-review-2026-04-19.md F-172..F-184 | DONE | F-172..F-182, F-184 RESOLVED; F-179 PARTIAL (D-176); F-183 routed to FIX-236 |

---

## 3. Key Findings

### 3.1 Architecture correctness — PASS
SGP.22 artefacts (sm_dp_plus_id, activation code path) fully replaced by SGP.02 push model. SM-SR role implemented as OTA dispatcher + esim_ota_commands queue. ES8+ callback path authenticated via HMAC-SHA-256 + Redis replay guard (300s window).

### 3.2 PAT-017 compliance — PASS (Gate fix confirmed)
Gate scout caught `cfg.MaxRetries` threading gap (store wired, handler not). Fix confirmed: BulkEsimSwitchHandler now accepts `cfg EsimOTAConfig`; main.go construction site updated. DEV-572 records the decision.

### 3.3 EID/ICCID confusion (PAT-025) — PASS (Gate fix confirmed, test coverage strong)
EsimUndoRecord.EID field omission fixed at Gate. Integration test `TestBulkEsimSwitch_Integration_100Profiles_100OTACommands` uses value-equality assertion (`prof.EID == params.EID`) with format-distinct seed values (32-hex EID vs 19-digit ICCID) — regression guard is structurally strong. New PAT-025 documents prevention rules.

### 3.4 Config defaults reconciliation — PASS (DEV-571)
batchSize=100, maxConcurrency=5, ratePerOperatorRPS=10, maxRetries=3 locked as authoritative (config.go). Plan doc inconsistencies corrected at Gate.

### 3.5 Test quality — PASS
- 25/25 unit tests on dispatcher + reaper
- 7/7 mock client tests
- T20: invalid transition regression (PAT-024 guard)
- T21: 100-profile integration test (EID value-equality assertions)
- 3896/3896 total test suite

### 3.6 F-179 (Export 400) — PARTIAL / deferred D-176
Export endpoint bug not addressed by FIX-235. Correctly deferred. F-179 annotated as PARTIAL in ui-review.

### 3.7 Stock alert dedup — PASS
`TestESimStockAlerter_AlertSourceIsSystem` confirms dedup_key pattern `esim_stock:{tenant}:{operator}`; source="system" (PAT-024 compliance).

---

## 4. Deferred Items (open, tracked in ROUTEMAP)

| ID | Item |
|----|------|
| D-172 | Filter-based bulk switch API contract (locked, not full impl) |
| D-173 | Distinct type wrappers EID/ICCID/IMSI (Go type safety) |
| D-174 | PAT-017 handler config audit — other handlers |
| D-175 | Stock alert auto-resolve on threshold recovery |
| D-176 | Export CSV 400 fix (F-179) |
| D-177 | EID format validation at construction (runtime assertions) |
| D-178 | DB integration tests for OTA CHECK constraints |
| D-179 | SM-SR real-world provider adapter (HTTPSMSRAdapter) |

---

## 5. Verdict

**PASS — story is production-ready within scope.** No open P0/P1 findings. All 13 F-finding closures annotated. PAT-025 and DEV-571..DEV-574 documented. Cross-doc updates complete.
