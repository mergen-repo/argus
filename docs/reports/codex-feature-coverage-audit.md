# Codex Feature Coverage Audit

- Scope: `PRODUCT.md` + `SCOPE.md` + `ARCHITECTURE.md` + `ROUTEMAP.md` + relevant story docs
- Status: `fail`
- Findings: `2`
- Method: only evidence-backed doc-to-story ownership checks; roadmap/review docs treated as context, not proof

## Findings

### 1. [HIGH] F-057 OAuth2 client credentials has no implementable story coverage
- Expected:
  `PRODUCT.md` and `SCOPE.md` both include OAuth2 client-credentials support in v1 scope, so at least one story should define OAuth2-specific acceptance criteria, endpoints, and verification.
- Actual:
  The requirement is only mentioned in `STORY-008` title/user-story text, but the story contract itself is API-key CRUD only. The API index still advertises `OAuth2 (3rd party)` as a supported auth mode, while the only story-owned endpoints are `API-150..154` for API keys. This leaves OAuth2 without an actionable story or endpoint contract.
- Evidence:
  - `docs/PRODUCT.md:106`
  - `docs/SCOPE.md:122`
  - `docs/architecture/api/_index.md:6`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:1`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:4`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:19`
  - `docs/stories/phase-1/STORY-008-api-key-management.md:32`
  - `docs/architecture/api/_index.md:198`
  - `docs/architecture/api/_index.md:202`

### 2. [HIGH] F-025 Diameter↔RADIUS bridge is in scope but has no story owner
- Expected:
  The v1 scope explicitly includes a Diameter↔RADIUS bridge, so roadmap/story coverage should include a dedicated story or a clearly-owned AC under the AAA phase.
- Actual:
  The feature is present in product/scope docs, but the Phase 3 AAA roadmap only covers adapter, RADIUS, EAP, session management, Diameter server, 5G SBA, and operator failover. I found no story file that owns a protocol-bridge workflow or acceptance criteria for translating traffic between Diameter and RADIUS.
- Evidence:
  - `docs/PRODUCT.md:66`
  - `docs/SCOPE.md:80`
  - `docs/ROUTEMAP.md:56`
  - `docs/ROUTEMAP.md:60`
  - `docs/ROUTEMAP.md:64`

## Recommendations

1. Open a new story for OAuth2 client credentials, or split `STORY-008` into API-key and OAuth2 scopes with explicit token endpoint, client lifecycle, auth flow, error model, and tests.
2. Add a dedicated AAA bridge story for Diameter↔RADIUS, including ownership in `ROUTEMAP.md`, protocol translation ACs, failure semantics, and observability requirements.
3. After story creation, update `docs/architecture/api/_index.md` so every advertised auth/protocol capability is traceable to a concrete story and endpoint contract.
