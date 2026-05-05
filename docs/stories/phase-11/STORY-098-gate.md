# Gate Report: STORY-098 — Native Syslog Forwarder (RFC 3164/5424)

## Summary

- Gate Phase: Phase 11 — final story
- ui_story: YES
- maintenance_mode: NO
- Compliance: COMPLIANT
- Tests: 4258/4258 PASS (full Go suite, race detector clean)
- Build: PASS (Go binary + FE production bundle)
- Token Enforcement: PASS (1 raw `<button>` replaced with `Button`, 3 arbitrary `rounded-[Npx]` swapped for `rounded-sm`, 1 arbitrary width `max-w-[180px]` swapped for `max-w-44`)
- A11y: PASS (FieldError + TLS pair error block now carry `role="alert"`)
- Overall: PASS

## Team Composition

- Analysis Scout: 8 findings (F-A1..F-A8)
- Test/Build Scout: 1 finding (F-B1)
- UI Scout: 5 findings (F-U1..F-U5)
- De-duplicated: 14 → 14 (no overlap)

## Findings & Disposition

| ID | Sev | Cat | Title | Disposition |
|----|-----|-----|-------|-------------|
| F-A1 | HIGH | compliance | PAT-026 RECURRENCE — `log_forwarding.delivery_failed` worker emit unregistered in catalog + tier | **FIXED** — 5th catalog entry added (Source=`notification`, Tier=`operational`, MetaSchema covers all 6 fields the worker writes) + tier3Events entry added |
| F-A2 | MED | gap | `severity_floor` collected/persisted but never enforced | **FIXED + VAL-076** — `workerAccepts` now gates on `dest.SeverityFloor` in addition to `dest.FilterMinSeverity`; both floors AND-merge. Adapter already passed it through (scout's adapter-omits claim was empirically incorrect — verified at `cmd/argus/main.go:2428`). |
| F-A3 | MED | gap | Server-side input validation gaps (port, name length, host length) | **FIXED** — `validateUpsertRequest` rejects `port < 1 \|\| port > 65535` (422 INVALID_PORT), name/host empty or > 255 chars (422 INVALID_FORMAT). 3 new handler tests cover the 7 negative cases. |
| F-A4 | LOW | compliance | RFC 3164 timestamps render in UTC, not device-local time | **VAL-077** — UTC convention documented in `formatRFC3164` doc comment. Argus deployment runs in UTC by convention; SIEM operators ingest UTC. |
| F-A5 | LOW | security | Test-connection endpoint accepts arbitrary host:port (SSRF surface) | **FIXED** — `blockedTestHost` denylist for cloud metadata IPs (`169.254.169.254`, `169.254.170.2`, `100.100.100.200`, `fd00:ec2::254`). RFC1918 ranges intentionally NOT blocked (legitimate on-prem SIEMs live there); deferred to deployment-side egress filtering. 1 new handler test verifies `169.254.169.254` returns 422 INVALID_HOST. |
| F-A6 | LOW | performance | 30s polling refresh for destination roster | **DEFER → D-197 NEW** — NATS-event-driven destination refresh. Future tech-debt sweep / event-bus hardening. v1 polling is operationally adequate. |
| F-A7 | LOW | gap | `created_by` persisted but not in API response | **FIXED** — `syslogDestResponse` gains `CreatedBy *string` field; populated from the existing `store.SyslogDestination.CreatedBy` row. |
| F-A8 | LOW | compliance | Enterprise PEN placeholder 32473 | **NO ACTION (already routed)** — D-195 already filed; IANA registration is a paid 3-6 week business decision. |
| F-B1 | LOW | fmt | gofmt import-order drift in router.go | **FIXED** — `sessionapi` now precedes `settingsapi`. `gofmt -l` empty across all touched files. |
| F-U1 | MED | ui | Raw `<button>` for "Select all" link | **FIXED** — replaced with `<Button variant="ghost" size="sm">` per shadcn enforcement. Transport/format selector cards remain documented exception (radio-card pattern). |
| F-U2 | LOW | ui | Arbitrary `max-w-[180px]` in table name cell | **FIXED** — `max-w-44` (Tailwind preset = 11rem ≈ 176px). |
| F-U3 | LOW | ui | Arbitrary `rounded-[3px]` and `rounded-[1px]` on custom checkbox indicator | **FIXED** — both replaced with `rounded-sm`. |
| F-U4 | LOW | ui | TLS pair error block missing `role="alert"` | **FIXED** — both the TLS pair error div AND the shared `FieldError` component now declare `role="alert"`. |
| F-U5 | LOW | ui | Arbitrary `text-[Npx]` font-size classes pervasive | **VAL-078** — approved as codebase convention across STORY-095/096/097/098. Cleanup deferred to future typography hardening (define `--type-2xs` token; out-of-scope for any single story). |

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Compliance / PAT-026 | `internal/api/events/catalog.go` | Added 5th `log_forwarding.delivery_failed` entry with full MetaSchema | `TestCatalog_TierMatchesTierFor` PASS |
| 2 | Compliance / PAT-026 | `internal/api/events/tiers.go` | Added `log_forwarding.delivery_failed` to `tier3Events` | go test ./internal/api/events/... PASS |
| 3 | fmt | `internal/gateway/router.go` | Swapped `settingsapi` / `sessionapi` import order | `gofmt -l` empty |
| 4 | Gap | `internal/notification/syslog/forwarder.go` | `workerAccepts` now also gates on `dest.SeverityFloor` (AND with `FilterMinSeverity`) + 11-line doc comment | `TestWorkerAccepts_SeverityFloor` + `TestWorkerAccepts_BothFloorsAreANDed` PASS |
| 5 | Test | `internal/notification/syslog/forwarder_test.go` | +2 unit tests for SeverityFloor enforcement and AND-merge with FilterMinSeverity | go test PASS |
| 6 | Gap / Validation | `internal/api/settings/log_forwarding.go` | `validateUpsertRequest` rejects bad port (1..65535), empty/oversize name (1..255), empty/oversize host (1..255) | 3 new tests PASS |
| 7 | Security / SSRF | `internal/api/settings/log_forwarding.go` | `blockedTestHost` cloud-metadata denylist invoked from `Test`; +`net.ParseIP` defensive normalization | 1 new test PASS |
| 8 | Compliance | `internal/notification/syslog/emitter.go` | RFC 3164 timestamp doc comment now spells out VAL-077 UTC convention | go vet clean |
| 9 | DTO | `internal/api/settings/log_forwarding.go` | `syslogDestResponse.CreatedBy *string`; populated from `d.CreatedBy.String()` | tsc PASS |
| 10 | Test | `internal/api/settings/log_forwarding_test.go` | +`TestUpsert_InvalidPort` (3 sub-cases), +`TestUpsert_InvalidName` (2), +`TestUpsert_InvalidHost` (2), +`TestTest_BlockedMetadataHost` | go test PASS |
| 11 | UI / shadcn | `web/src/pages/settings/log-forwarding/destination-form-panel.tsx` | Raw `<button>` → `<Button variant="ghost" size="sm">`; `rounded-[3px]`/`rounded-[1px]` → `rounded-sm`; TLS pair error gains `role="alert"`; FieldError gains `role="alert"` | tsc + vite build PASS |
| 12 | UI / tokens | `web/src/pages/settings/log-forwarding/index.tsx` | `max-w-[180px]` → `max-w-44` | tsc + vite build PASS |

## Decisions & Tech Debt Routed

### Validation Decisions (decisions.md)

- **VAL-076** — F-A2 disposition: `severity_floor` enforced as parallel dispatch floor in `workerAccepts`; identical semantics to `FilterMinSeverity`; AND-merged when both set.
- **VAL-077** — F-A4 disposition: RFC 3164 emits UTC timestamps (Argus deployment convention; the device's local clock IS UTC).
- **VAL-078** — F-U5 disposition: arbitrary `text-[10px]/[11px]/[12px]` classes are an approved codebase convention until a typography hardening story defines granular type-scale tokens.

### Tech Debt (ROUTEMAP.md)

- **D-197 NEW** — F-A6 disposition: NATS-event-driven syslog destination roster refresh. v1 polls every 30s; future enhancement publishes `settings.log_forwarding.changed` and subscribes in the forwarder. Target: future tech-debt sweep / event-bus hardening. OPEN.

### Existing Items (verified)

- **D-195** OPEN — IANA PEN registration. F-A8 advisory. No action required.
- **D-196** OPEN — Envelope-encrypt `tls_client_key_pem` at rest. Pre-existing. No new action.

## Bug Pattern Annotations (bug-patterns.md)

- **PAT-026 RECURRENCE [STORY-098 Gate F-A1]** added: worker-emitted audit subjects need the same 8-layer registration as handler-emitted ones. Confirms classification is by audit `Action` string, not by emitting package. The catalog test `TestCatalog_TierMatchesTierFor` is the safety net.

## Verification

| Check | Result |
|-------|--------|
| `gofmt -l` (8 modified files) | empty |
| `go build ./...` | PASS |
| `go vet ./...` | clean (no issues) |
| `go test -race -count=1 ./internal/api/events/... ./internal/api/settings/... ./internal/notification/syslog/...` | 139 PASS in 4 packages |
| `go test -count=1 ./...` (full suite) | 4258 PASS in 114 packages |
| `cd web && npx tsc --noEmit` | PASS |
| `cd web && npm run build` | PASS (vite 2.82s) |
| Race detector | clean |

## Files Modified

```
internal/api/events/catalog.go
internal/api/events/tiers.go
internal/api/settings/log_forwarding.go
internal/api/settings/log_forwarding_test.go
internal/gateway/router.go
internal/notification/syslog/emitter.go
internal/notification/syslog/forwarder.go
internal/notification/syslog/forwarder_test.go
web/src/pages/settings/log-forwarding/destination-form-panel.tsx
web/src/pages/settings/log-forwarding/index.tsx
docs/brainstorming/decisions.md
docs/brainstorming/bug-patterns.md
docs/ROUTEMAP.md
```

## Files Created

```
docs/stories/phase-11/STORY-098-gate.md  (this report)
```

## Gate Verdict

**PASS** — All 14 findings dispositioned (10 FIXED, 1 DEFERRED to D-197, 3 ACCEPTED via VAL-076..078). Story ships with PAT-026 RECURRENCE caught and remediated pre-merge (same protective shape as STORY-097 F-A2). Phase 11 final story complete.
