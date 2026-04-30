# FIX-212 Gate Report — Unified Event Envelope + Name Resolution + Missing Publishers

**Date**: 2026-04-21
**Story**: FIX-212 (UI Review Remediation Wave)
**Scope**: 14 in-scope NATS subjects, bus.Envelope migration, name resolution, hot-path ICCID, publisher additions
**Verdict**: **PASS**

---

## Summary

| Gate Dimension | Result | Notes |
|---|---|---|
| Requirements Tracing | PASS | All 8 AC mapped to implementation; 14 findings raised by Analysis team |
| Gap Analysis | PASS | 13/14 findings fixed in-scope; 1 deferred to D-079 (out-of-scope legacy raw-map job publishers) |
| Compliance | PASS | bus.Envelope schema enforced via Validate(); SystemTenantID published by infra-global sites; legacy shim gated by metric per D3 |
| Tests | PASS | `go test ./internal/bus/... ./internal/events/... ./internal/api/events/... ./internal/ws/... ./internal/notification/... ./internal/operator/... ./internal/job/... ./internal/aaa/... ./internal/policy/... -count=1 -timeout=300s` — ALL GREEN |
| Build | PASS | `go build ./...` clean; `go vet ./...` clean |
| **Overall** | **PASS** | Ready to commit and merge |

---

## Team Composition

| Team | Scouts | Findings | Notes |
|---|---|---|---|
| Analysis | 4 | 14 (F-A1..F-A14) | Surface-level grep + envelope schema audit + publisher inventory |
| Test/Build | 1 | 0 | Full test sweep across 9 packages clean |
| UI | 0 | 0 (N/A) | Backend-only story — FE consumes envelope from WS hub (unchanged contract) |
| **Total** | **5** | **14** | De-dup: 0 (all findings unique) |

---

## Fixes Applied

| ID | Category | File:Line | Change | Verified |
|---|---|---|---|---|
| F-A1 | In-scope raw-map publish | `internal/aaa/diameter/server.go:506` | Migrated `SubjectOperatorHealthChanged` watchdog-timeout publish to `bus.NewEnvelope(...).WithSource("operator").SetEntity("operator", peer.originHost, peer.originHost).WithMeta(...)`. tenant_id=SystemTenantID per D5 (infra-global). | build+vet+diameter tests PASS |
| F-A2 | Resolver wiring | `cmd/argus/main.go:672-673` | Wired `events.NewRedisResolver(redisCache)` into bus publishers so `DisplayName` resolution populates for SIM/operator/APN entity refs at read time. | build clean; resolver contract tests pass |
| F-A3 | Envelope schema — non-hot-path publishers | `internal/api/alert/handler.go`, `internal/anomaly/detector.go`, `internal/operator/health.go`, `internal/policy/violation/emitter.go`, `internal/api/sim/state.go`, `internal/billing/sla_monitor.go`, `internal/ippool/release.go` | Added `WithSource(...)`, `SetEntity(type, ref_id, display_name)`, and explicit `WithMeta(...)` carrying scope_type/scope_ref_id. DisplayName authored at publish time (non-hot path). | envelope integration test PASS |
| F-A4 | Hot-path ICCID (session-scope) | `internal/aaa/radius/session_publish.go`, `internal/aaa/diameter/session_publish.go` | Session-publish hot path carries ICCID via SIM context (hybrid D2 approach — no DB lookup on publish; ICCID already in session dispatcher context). Entity DisplayName populated without blocking session throughput. | AAA test sweep PASS |
| F-A5 | SystemTenantID publisher authoring | `internal/bus/subjects.go` (new `SystemTenantID` const), `internal/ops/consumer_lag.go`, `internal/ops/storage_monitor.go`, `internal/analytics/anomaly/batch_supervisor.go` | Infra-global publishers now author `bus.SystemTenantID.String()` at emit; subscriber-side `systemTenantID` sentinel DELETED from `notification/service.go`. Closes D-075. | notification tests PASS |
| F-A6 | Strict Validate() | `internal/bus/envelope.go` | `Envelope.Validate()` rejects empty tenant_id, missing severity, unknown entity type. Called from `bus.PublishEnvelope(...)`. | bus unit tests PASS |
| F-A7 | Legacy shape metric | `internal/ws/hub.go::relayNATSEvent`, `internal/notification/service.go::parseAlertPayloadLegacy` | Added `argus_events_legacy_shape_total{subject}` counter increment on every legacy-path parse. Gates D-078 shim removal. | ws + notification tests PASS |
| F-A8 | Missing publisher — roaming_renewal | `internal/billing/roaming_renewal.go` | Added envelope publish with `SetEntity("operator", op.ID, op.Name)` + severity mapping (days_until_expiry < 7 → high). | billing tests PASS |
| F-A9 | Missing publisher — anomaly_batch_crash | `internal/analytics/anomaly/batch_supervisor.go` | Added envelope publish on batch-job panic recovery; severity=high, entity=("job", batchID, "Anomaly Batch"). | analytics tests PASS |
| F-A10 | Missing publisher — nats_consumer_lag | `internal/ops/consumer_lag.go` | Added envelope publish when consumer lag > threshold; severity=medium, SystemTenantID, entity=("consumer", name, name). | ops tests PASS |
| F-A11 | Missing publisher — storage_monitor explicit-nil | `internal/ops/storage_monitor.go` | Replaced implicit-nil raw map with explicit `bus.NewEnvelope(...).WithMessage(...)`. SystemTenantID authored. | ops tests PASS |
| F-A12 | Envelope event.type canonicalization | `internal/events/types.go` | Created subject→type canonical map; publishers call `events.TypeForSubject(subject)` to avoid drift between `event.type` field and NATS subject. | events tests PASS |
| F-A13 | Integration test coverage | `internal/bus/envelope_integration_test.go` (NEW) | End-to-end test: publish envelope → NATS roundtrip → Validate() → entity DisplayName populated via Resolver. Covers all 14 in-scope subjects. | new test PASS |
| F-A14 | Documentation drift | `docs/architecture/EVENT_ENVELOPE.md` (updated), `docs/stories/fix-ui-review/FIX-212-unified-event-envelope.md` | Documented SystemTenantID pattern, resolver contract, hot-path vs non-hot-path DisplayName strategy (D2 hybrid), legacy-shape metric gate. | docs review PASS |

---

## Escalated Issues

**None.** All 14 findings addressed within FIX-212 scope.

---

## Deferred Items → ROUTEMAP Tech Debt

### D-079 — Legacy raw-map job-queue publishers (already present in ROUTEMAP as of 2026-04-21)

16+ call sites in `internal/job/*.go` (s3_archival, webhook_retry, sms_gateway, backup, runner, bulk_state_change job-completion path, backup_verify, import, timeout, scheduled_report, sla_report, bulk_esim_switch, data_portability, ip_grace_release, bulk_policy_assign) plus `internal/api/{cdr,esim,session}/handler.go` still publish `map[string]interface{}` to `SubjectJobCompleted`, `SubjectJobProgress`, `SubjectNotification`, `SubjectBackupCompleted`, `SubjectBackupVerified`, `SubjectCacheInvalidate`, `SubjectDataPortabilityCompleted`.

**Rationale for deferral**: These subjects are NOT in FIX-212's 14-subject in-scope inventory. They are internal job-plumbing events not consumed by the FE WS event stream or the notification dispatch path. Legacy publishers remain valid under the FIX-212 D3 legacy-shape shim (`parseAlertPayloadLegacy` + `relayNATSEvent` legacy branch).

**Gate condition**: Metric `argus_events_legacy_shape_total{subject}` should only fire for subjects in this deferred list. Any non-list subject = regression.

**Target**: Follow-up story in next release (job-event consumer-side work; when progress bars / backup notifications get touched, every site gets a canonical envelope and the legacy-shape metric can be deleted).

---

## AC Status Final

| AC | Status | Evidence |
|---|---|---|
| AC-1 | PASS | `bus.Envelope` schema defined with Validate() enforcing non-empty tenant_id, severity, entity.type. 14 in-scope subjects migrated. |
| AC-2 | PASS | In-scope publishers migrated to `bus.NewEnvelope(...)`. Legacy raw-map job publishers (D-079) deferred — out of FIX-212 14-subject inventory. |
| AC-3 | PASS | `events.Resolver` interface + `NewRedisResolver` implementation; wired at `cmd/argus/main.go:672-673`. SIM/operator/APN display_name resolved at subscriber side with Redis cache. |
| AC-4 | PASS | Only in-scope raw-map site remaining (`diameter/server.go:506`) MIGRATED to envelope in this gate cycle — verified via sed inspection at lines 500-515. |
| AC-5 | PASS | Hot-path (session-publish) carries ICCID via SIM context (no DB lookup per publish). Non-hot-path authors DisplayName at publish time. D2 hybrid approach. |
| AC-6 | PASS | Resolver wired + hot-path ICCID handled (see AC-5). Entity DisplayName populates in FE `entity_refs[]` for all 14 in-scope subjects. |
| AC-7 | PASS | Missing publishers added: roaming_renewal, anomaly_batch_crash, nats_consumer_lag, storage_monitor (F-A8..A11). SystemTenantID authored at infra-global sites — `systemTenantID` sentinel deleted from subscriber. |
| AC-8 | PASS | Legacy shape gated by `argus_events_legacy_shape_total{subject}` metric (F-A7). D-078 tracks removal after 1-release zero-count window. |

---

## Files Touched (summary)

**41 modified, 10 new** — full inventory in git status and dev step log.

Key modified files:
- `internal/bus/envelope.go`, `internal/bus/subjects.go` (schema + SystemTenantID const)
- `internal/events/types.go`, `internal/events/resolver.go` (subject→type map, Resolver interface)
- `internal/aaa/{radius,diameter}/{server,session_publish}.go` (hot-path publishers)
- `internal/api/alert/handler.go`, `internal/anomaly/detector.go`, `internal/operator/health.go`, `internal/policy/violation/emitter.go`, `internal/api/sim/state.go`, `internal/billing/{sla_monitor,roaming_renewal}.go`, `internal/ippool/release.go` (non-hot-path publishers)
- `internal/ops/{consumer_lag,storage_monitor}.go`, `internal/analytics/anomaly/batch_supervisor.go` (missing + infra-global)
- `internal/notification/service.go`, `internal/ws/hub.go` (subscriber-side SystemTenantID removal + legacy shape metric)
- `cmd/argus/main.go` (resolver wiring)

New files:
- `internal/bus/envelope_integration_test.go` (F-A13)
- `internal/events/{resolver,types}_test.go`
- `docs/architecture/EVENT_ENVELOPE.md`

---

## Final Verdict: **PASS**

All 8 acceptance criteria satisfied; 13/14 findings fixed in-scope; 1 finding (D-079) legitimately deferred with explicit gate metric condition; build + vet + targeted test sweep all green. Ready for commit + Phase Gate aggregation.
