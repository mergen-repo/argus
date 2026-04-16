# Codex DONE Stories Audit

- Scope: `ROUTEMAP.md` stories marked `[x] DONE` vs current codebase
- Status: `fail`
- Stories With Findings: `3`
- Findings: `3`
- Method: story ACs reconstructed from story docs, then verified against routers/handlers/components; roadmap/gates treated as context only

## Findings

### 1. [HIGH] STORY-008 is marked DONE while the OAuth2 portion is still absent
- Expected:
  `STORY-008` says Argus should support OAuth2 client credentials for third-party systems, and the API index advertises OAuth2 as a supported auth mode.
- Actual:
  The shipped story implements only API-key CRUD and API-key auth/rate limiting. The story ACs/API contract stop at `API-150..154`, and the gate report declares PASS over those same 5 endpoints. There is no OAuth2 endpoint or OAuth2 acceptance criterion in the completed story.
- Evidence:
  - `docs/ROUTEMAP.md:43`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:1`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:4`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:19`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:32`
  - `docs/stories/phase-1/STORY-008-gate.md:4`
  - `docs/stories/phase-1/STORY-008-gate.md:5`
  - `docs/stories/phase-1/STORY-008-gate.md:52`
  - `docs/stories/phase-1/STORY-008-gate.md:58`
  - `docs/architecture/api/_index.md:6`

### 2. [HIGH] STORY-075’s SIM IP Allocation History is still a placeholder
- Expected:
  STORY-075 AC-7 requires SIM detail to show IP Allocation History by querying `ip_addresses` for the SIM.
- Actual:
  The delivered `IPHistoryTab` explicitly scopes the feature down to current allocation only, says full history is deferred to later stories, and renders a visible “Historical IP changes tracking coming soon” placeholder. This contradicts the DONE story’s accepted SIM-detail enrichment.
- Evidence:
  - `docs/ROUTEMAP.md:198`
  - `docs/stories/phase-10/STORY-075-cross-entity-context.md:23`
  - `docs/stories/phase-10/STORY-075-cross-entity-context.md:24`
  - `web/src/pages/sims/_tabs/ip-history-tab.tsx:11`
  - `web/src/pages/sims/_tabs/ip-history-tab.tsx:13`
  - `web/src/pages/sims/_tabs/ip-history-tab.tsx:91`
  - `web/src/pages/sims/_tabs/ip-history-tab.tsx:96`

### 3. [HIGH] STORY-077’s undo flow is wired in docs/UI language but not operable in runtime
- Expected:
  STORY-077 AC-2 requires destructive actions to produce an undoable action id, show an Undo toast, and restore state through `POST /api/v1/undo/:action_id`.
- Actual:
  The undo handler can only execute actions that were registered in its executor map, but the runtime wiring never registers any executors. The destructive handlers audited here (`apikey.revoke`, `segment.delete`, `policy.delete`) simply return `204` and do not register undo payloads or return an action id. The frontend undo helper exists, but I found no production call sites that feed it a real action id.
- Evidence:
  - `docs/ROUTEMAP.md:200`
  - `docs/stories/phase-10/STORY-077-enterprise-ux-polish.md:15`
  - `internal/api/undo/handler.go:24`
  - `internal/api/undo/handler.go:36`
  - `internal/api/undo/handler.go:69`
  - `internal/api/undo/handler.go:72`
  - `cmd/argus/main.go:1044`
  - `cmd/argus/main.go:1045`
  - `cmd/argus/main.go:1048`
  - `internal/api/apikey/handler.go:439`
  - `internal/api/apikey/handler.go:452`
  - `internal/api/segment/handler.go:157`
  - `internal/api/segment/handler.go:170`
  - `internal/api/policy/handler.go:427`
  - `internal/api/policy/handler.go:445`
  - `web/src/hooks/use-undo.ts:24`
  - `web/src/hooks/use-undo.ts:33`
  - `web/src/components/shared/undo-toast.tsx:13`
  - `web/src/components/shared/undo-toast.tsx:22`

## Recommendations

1. Re-open `STORY-008` or create a follow-up story that explicitly delivers OAuth2 client credentials instead of treating API keys as sufficient closure.
2. Re-open the STORY-075 SIM IP history slice and either implement the actual history query or downgrade the story/doc status from DONE to partial.
3. Re-open STORY-077 undo: register executors in `main.go`, persist undo payloads from each destructive action, return `action_id` to the frontend, and add end-to-end verification for revoke/delete/bulk flows.
