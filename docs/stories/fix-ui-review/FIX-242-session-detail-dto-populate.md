# FIX-242: Session Detail Extended DTO Populate — SoR/Policy/Quota Fields

## Problem Statement
`internal/api/session/handler.go:260 Get()`:
```go
detail := sessionDetailDTO{sessionDTO: dto}  // <-- Only base fields
apierr.WriteSuccess(w, http.StatusOK, detail)
// detail.SorDecision = nil
// detail.PolicyApplied = nil
// detail.QuotaUsage = nil
```

Schema defines `sessionDetailDTO` with pointer fields `SorDecision *sorDecisionDTO`, `PolicyApplied *policyAppliedDTO`, `QuotaUsage *quotaUsageDTO` — but handler never populates them. Because of `omitempty` tag, nil pointers don't serialize, so FE sees bare base DTO.

Consequence — Session Detail page tabs empty:
- Policy tab → "No policy applied to this session"
- SoR Decision tab → "SoR decision data not available"
- Quota tab → "No quota configured"
- Audit tab → `/audit?entity_type=session` returns empty (F-161 session-level audit publisher missing too)
- Alerts tab → `/anomalies?sim_id=X` (shifts to SIM-level — OK)

**F-299 code-verified root cause.** Schema ready, handler TODO.

## User Story
As an ops engineer debugging a session, I want to see the session's current policy, SoR routing decision, and quota usage in the detail panel — so I can understand why a session has the behavior it has.

## Architecture Reference
- `internal/api/session/handler.go::Get, enrichSessionDTO`
- `internal/aaa/session/manager.go` — session source of truth
- New/extended store: `sor_decisions` child table (if SoR scoring persists per session)

## Findings Addressed
- F-159 (session DTO missing policy/SoR/quota fields)
- F-161 (5 of 6 tabs empty)
- F-162 (layout — alt yarı boş alan) — populated data fills panel
- F-299 (code-level root cause)

## Acceptance Criteria
- [ ] **AC-1:** `Get` handler populates all 3 extended fields (when data available):
  ```go
  detail := sessionDetailDTO{sessionDTO: dto}
  if sor := fetchSoRDecision(ctx, sessionID); sor != nil {
      detail.SorDecision = sor
  }
  if policy := fetchPolicyApplied(ctx, simID); policy != nil {
      detail.PolicyApplied = policy
  }
  if quota := fetchQuotaUsage(ctx, sessionID); quota != nil {
      detail.QuotaUsage = quota
  }
  ```
- [ ] **AC-2:** **SoR Decision source:** New `sor_decisions` table with columns: `session_id, chosen_operator_id, scoring JSONB (array of {operator_id, score, reason}), decided_at`. Populated at session create by SoR engine. GET retrieves for session.
- [ ] **AC-3:** **Policy Applied source:** Query `policy_assignments` WHERE `sim_id = session.sim_id` → returns `policy_id, version_id`. Enrich with policy_name, version_number (join via FIX-202 pattern). DTO adds `matched_rules` (from DSL evaluator — which rules matched).
- [ ] **AC-4:** **Quota Usage source:** Query session-scoped accounting counter (either direct computation from CDRs or maintained counter). Returns `{limit_bytes, used_bytes, pct, reset_at}` where limit from applied policy's `bandwidth_quota` / `data_cap` rule.
- [ ] **AC-5:** **Session audit events:** F-161 related — session start/end/CoA publish audit entries `entity_type=session, entity_id=<session_id>, action=session.{started|updated|ended|coa_sent}`. Already event-published (FIX-212) — ADD audit_logs write from audit service.
- [ ] **AC-6:** **N+1 avoidance (F-300):** Do not add per-request DB lookups. SoR/Policy/Quota fetchers use store-layer joins or cached aggregates (FIX-208 Aggregates service).
- [ ] **AC-7:** **CoA history sub-resource:** New field `detail.CoaHistory []coaEntry` — list of `{at, reason, policy_version_id, status}` events for this session. Source: audit_logs `entity_type=session, action=session.coa_sent` or similar.
- [ ] **AC-8:** **Quota display UX:** FE Quota tab shows progress bar — `used / limit (pct%)` + reset countdown + warning colors (yellow > 80%, red > 95%).
- [ ] **AC-9:** **Policy display:** FE Policy tab shows policy name (linked to policy detail), version, matched rules (highlighted from policy source), applied rules (bandwidth/rate_limit/time_window effective values).
- [ ] **AC-10:** **SoR display:** Score table sorted descending, chosen operator highlighted, reason per row ("best latency", "lowest cost", etc.).
- [ ] **AC-11:** **Missing data handling:** Tab shows friendly empty state "SoR scoring not persisted for this session" — NOT "unavailable". Distinguish "no data yet" from "feature broken".
- [ ] **AC-12:** **Session Detail layout fix (F-162):** Two top cards (Connection Details + Data Transfer) equal width (grid-cols-2). Below: new "Session Timeline" + "Policy Context" cards fill empty bottom half.

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_sor_decisions_table.up.sql` (NEW or extend sessions JSONB)
- `internal/store/session.go` — extend with SoR/Policy/Quota fetchers
- `internal/api/session/handler.go::Get, enrichSessionDTO` — populate fields
- `internal/aaa/session/manager.go` — persist SoR decision at create
- `internal/audit/service.go` — session-entity audit publisher
- `web/src/pages/sessions/detail.tsx` — all 6 tabs populated rendering
- `web/src/types/session.ts` — extended DTO types

## Risks & Regression
- **Risk 1 — SoR data historical backfill:** Existing sessions created before SoR persistence have no data. AC-11 UX covers. No backfill required.
- **Risk 2 — Quota calculation cost:** On-demand from CDRs may be slow for long sessions. Mitigation: maintain session-scoped counter in Redis or denormalized column.
- **Risk 3 — Policy change mid-session:** CoA history (AC-7) shows transition. PolicyApplied reflects CURRENT (latest) policy_version.
- **Risk 4 — Schema bloat:** SoR JSONB per session adds ~200-500B. At 10M sessions/month = 2-5GB/month. Acceptable; retention with sessions table.

## Test Plan
- Unit: each fetcher returns correct populated struct for fixture session
- Integration: session with SoR + policy + quota data → API returns all 4 extended fields
- Browser: Session Detail page — all 6 tabs show real content
- Regression: session without data (edge case) shows empty states not errors

## Plan Reference
Priority: P0 · Effort: M · Wave: 8 · Depends: FIX-231 (policy canonical source), FIX-241 (global nil-slice — safety net)
