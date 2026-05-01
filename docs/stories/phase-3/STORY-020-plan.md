# Implementation Plan: STORY-020 — 5G SBA HTTP/2 Proxy (AUSF/UDM)

> BACKFILL — STORY-020 was completed 2026-03-20 before the per-story plan-file convention was formalized.
> Full original spec: `docs/stories/phase-3/STORY-020-5g-sba-proxy.md`
> Closure artefacts: `STORY-020-deliverable.md`, `STORY-020-gate.md`, `STORY-020-review.md` in this directory.
> Implementation: `internal/aaa/sba/` (server.go, ausf.go, udm.go, types.go, server_test.go) plus simulator counterparts under `internal/simulator/sba/`.

## Reference
- Story spec: STORY-020-5g-sba-proxy.md
- Architecture: SVC-04 AAA Engine, 5G SBA layer (Nausf/Nudm), TS 29.503 / TS 29.518
- Deliverable summary: STORY-020-deliverable.md

This file exists solely to satisfy the modern story-done-guard hook which expects every DONE story to have a `*-plan.md` artefact. The original implementation plan was tracked in conversation + the deliverable doc.
