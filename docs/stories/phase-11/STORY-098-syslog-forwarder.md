# STORY-098: Native Syslog Forwarder (RFC 3164/5424)

## User Story
As a tenant admin, I want to forward Argus audit, alert, session, policy, IMEI, and system events to my SIEM via native syslog (RFC 3164 or 5424) over UDP, TCP, or TLS, with per-destination filter rules, so that my regulated environment ingests Argus telemetry without standing up a third-party log shipper.

## Description
Add a native syslog forwarder that subscribes to the existing NATS event bus and emits each event to one or more configured destinations as either RFC 3164 (BSD legacy, single-line) or RFC 5424 (modern structured-data). Destinations are tenant-scoped, persisted in TBL-61 `syslog_destinations`, configured through API-337 / API-338 and SCR-198. Transports: UDP (fire-and-forget), TCP (with reconnect + retry-with-backoff), TLS (CA cert validation, optional client cert). Per-destination filter rules choose which event categories (auth, audit, alert, session, policy, imei, system) reach which target. Delivery failures populate `last_error` without blocking subsequent deliveries.

This story is independent of the IMEI epic and can run in parallel with STORY-093..097.

## Architecture Reference
- Services: SVC-03 (Core API — settings endpoints), SVC-08 (Notification — bus subscriber), SVC-10 (Audit — destination CRUD audit)
- Packages: `migrations/`, `internal/store/syslog_destination.go` (new), `internal/api/settings/log_forwarding.go` (new), `internal/notification/syslog/emitter.go` (new — RFC 3164 / 5424 formatters), `internal/notification/syslog/transport.go` (new — udp / tcp / tls clients), `web/src/pages/settings/log-forwarding/` (new)
- Source: `docs/architecture/db/_index.md` (TBL-61), `docs/architecture/api/_index.md` (API-337, API-338), `docs/screens/SCR-198-settings-log-forwarding.md`
- Standards: RFC 3164 (BSD syslog), RFC 5424 (syslog protocol), RFC 5425 (TLS transport), RFC 6587 (TCP transport)

## Screen Reference
- SCR-198 — Settings → Log Forwarding (list + add/edit modal + test connection)

## Acceptance Criteria
- [ ] AC-1: Migration `YYYYMMDDHHMMSS_syslog_destinations.up.sql` creates TBL-61 `syslog_destinations` with all columns from `db/_index.md` row TBL-61: `id UUID PK, tenant_id UUID NOT NULL, name VARCHAR(255) NOT NULL, host VARCHAR(255) NOT NULL, port INT NOT NULL, transport VARCHAR(10) NOT NULL CHECK (transport IN ('udp','tcp','tls')), format VARCHAR(10) NOT NULL CHECK (format IN ('rfc3164','rfc5424')), filter_categories TEXT[] NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, last_delivery_at TIMESTAMPTZ NULL, last_error TEXT NULL, created_by UUID NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`. UNIQUE (tenant_id, name). Index (tenant_id, enabled). RLS tenant-scoped. `.down.sql` reverses cleanly.
- [ ] AC-2: API-337 GET `/api/v1/settings/log-forwarding` returns the list of destinations for the caller's tenant, including `last_delivery_at` and `last_error` for each row. Cursor pagination optional (small N — list is fine). RBAC: `tenant_admin+` for read.
- [ ] AC-3: API-338 POST `/api/v1/settings/log-forwarding` accepts the body documented in `api/_index.md`: `{host, port, transport, format, facility (0..23), severity_floor?, filter: {event_categories: [auth,audit,alert,session,policy,imei,system], min_severity?}, tls_ca_pem?, tls_client_cert_pem?, tls_client_key_pem?}`. Validates: `transport` enum (422 `INVALID_TRANSPORT`), `format` enum (422 `INVALID_FORMAT`), facility range, when `transport='tls'` either no PEMs (system trust) or all valid PEMs (422 `TLS_CONFIG_INVALID` on malformed). Audit `log_forwarding.destination_added` (hash-chained).
- [ ] AC-4: Destination CRUD — POST upserts (insert if no row with same (tenant_id, name), else update). Disable via PATCH-equivalent `enabled=false` (per `api/_index.md` note). Audit `log_forwarding.destination_updated` and `log_forwarding.destination_disabled` as appropriate. RBAC: `tenant_admin+` for write.
- [ ] AC-5: RFC 3164 emitter byte-level correctness — message format `<PRI>TIMESTAMP HOSTNAME TAG: MSG`. PRI = facility * 8 + severity. TIMESTAMP = `Mmm dd hh:mm:ss` (BSD format, local time). HOSTNAME = trimmed Argus instance hostname. TAG = `argus[<pid>]`. MSG = compact human-readable summary derived from the bus envelope. Verified by golden-byte test: emit a known event → bytes match the expected RFC 3164 sequence exactly.
- [ ] AC-6: RFC 5424 emitter byte-level correctness — message format `<PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG`. VERSION = `1`. TIMESTAMP = RFC 3339 with millisecond precision and timezone offset. APP-NAME = `argus`. PROCID = pid. MSGID = the event type (e.g., `sim.binding_mismatch`). STRUCTURED-DATA = at least one SD-ELEMENT with SD-ID `argus@<enterprise_number>` carrying tenant, sim, severity. MSG = human-readable summary in UTF-8 BOM-prefixed. Verified by golden-byte test.
- [ ] AC-7: UDP transport — datagram per message, no connect/handshake, fire-and-forget. Emitter never blocks the bus subscriber on UDP errors; failures bump `last_error` and a counter, no retry.
- [ ] AC-8: TCP transport — single persistent connection per destination, lazy-dial, write framing per RFC 6587 (octet-counting preferred, non-transparent framing fallback). On TCP failure: log + bump `last_error`, schedule reconnect with exponential backoff (1s, 2s, 4s, 8s, max 60s). Subsequent events buffer in a bounded in-memory queue (e.g., 1000 messages); overflow drops oldest with a counter increment, and a periodic warning log.
- [ ] AC-9: TLS transport — TLS 1.2+ over TCP. When `tls_ca_pem` provided, validate the server cert against that CA only; else use system trust store. When `tls_client_cert_pem` + `tls_client_key_pem` provided, present them for mutual TLS. Hostname verification ON (matches `host` field). Failures handled per AC-8.
- [ ] AC-10: Test-connection button — a synchronous endpoint (extension of API-338 or sibling) that opens the configured transport, sends a single test message, and returns `{ok: true}` or `{ok: false, error: "<reason>"}`. UI surfaces the result per SCR-198. No persistent state mutated on test.
- [ ] AC-11: Bus subscriber subscribes to the existing NATS subjects (per FIX-212 envelope contract) for the seven categories (`auth`, `audit`, `alert`, `session`, `policy`, `imei`, `system`). For each event, look up enabled destinations whose `filter_categories` includes the event category and whose `min_severity` (if set) is ≤ event severity, then emit. Dispatch is concurrent across destinations.
- [ ] AC-12: Per-destination delivery failure logging — `last_error` and `last_delivery_at` updated atomically after each attempt (success clears `last_error`, failure stores error string truncated to 1 KB). One failed destination MUST NOT block delivery to other destinations.
- [ ] AC-13: SCR-198 frontend — `/settings/log-forwarding` page renders the destinations table per SCR-198 with columns Name, Host:Port, Transport (chip), Format (chip), Categories (chip group), Status (enabled/disabled + last delivery age + last error tooltip), Actions (Edit, Test, Disable, Delete). Add Destination → modal per SCR-198 with all fields. TLS field group reveals on `transport=tls`. Test Connection button on the modal AND per-row.
- [ ] AC-14: Audit on every destination CRUD action and on every delivery failure event (rate-limited so a flapping destination does not flood audit — 1 audit row per destination per minute on continuous failure).
- [ ] AC-15: Performance / safety — adding 5 destinations does not measurably degrade NATS subscriber throughput (≤ 5% over baseline). Slow destinations (e.g., 1 s TCP write) do not stall the bus subscriber — dispatch is non-blocking via a per-destination buffered worker.
- [ ] AC-16: Regression — full `make test` green; existing notification, settings, and audit tests pass unchanged; `make db-seed` produces zero destinations by default.

## Dependencies
- Blocked by: STORY-006 (NATS event bus subscriber pattern) — already DONE.
- Blocks: none in Phase 11. Independent of IMEI work.

## Test Scenarios
- [ ] Integration: migrate up → TBL-61 exists with constraints; migrate down clean.
- [ ] Integration: API-338 POST a UDP / RFC 3164 destination → API-337 GET returns it; audit row written.
- [ ] Integration: API-338 with `transport='bogus'` → 422 `INVALID_TRANSPORT`, no DB write.
- [ ] Integration: API-338 with `transport='tls'` and malformed `tls_ca_pem` → 422 `TLS_CONFIG_INVALID`.
- [ ] Integration: Test-connection button against a mock UDP listener → `{ok: true}`.
- [ ] Integration: Test-connection against a closed TCP port → `{ok: false, error: "connection refused"}`.
- [ ] Integration: emit a synthetic `sim.binding_mismatch` event → mock UDP receiver receives an RFC 3164 message with the right PRI, timestamp, hostname, tag, and human summary.
- [ ] Integration: same event → mock RFC 5424 receiver receives a VERSION=1 message with structured-data SD-ID `argus@<enterprise_number>` carrying tenant, sim, severity.
- [ ] Integration: emit 1000 events with one TCP destination intentionally backed up → bus subscriber stays responsive, overflow counter increments, no panic.
- [ ] Integration: TLS destination with self-signed cert + matching CA PEM → delivers successfully; same destination without CA PEM → fails with cert verification error in `last_error`.
- [ ] Integration: per-destination filter — destination filtering only `audit, alert` does not receive `session` events; another destination filtering `imei, alert` does receive `imei` events. Both are consistent.
- [ ] Integration: cross-tenant API-337 / API-338 access returns 404 / 403.
- [ ] Unit: RFC 3164 byte-formatter — golden test: input event → expected byte sequence.
- [ ] Unit: RFC 5424 byte-formatter — golden test: input event → expected byte sequence with correct STRUCTURED-DATA.
- [ ] Unit: RFC 6587 framing — octet-counting frames `123 <msg>` for a 123-byte body.
- [ ] Unit: backoff — sequence on 5 consecutive failures is 1s, 2s, 4s, 8s, 16s (capped at 60s).
- [ ] E2E (Playwright): Settings → Log Forwarding → Add Destination → fill UDP form → save → row appears with "ok" status badge.
- [ ] E2E: Edit destination → switch to TLS → upload PEMs → Test Connection → success toast.
- [ ] E2E: Disable destination → row shows disabled badge; subsequent events do not flow there (verified via mock receiver).
- [ ] Regression: existing notification / audit suites green.

## Effort Estimate
- Size: S
- Complexity: Medium (two RFC formats × three transports + UI; small surface but byte-level correctness gates the value)
- Notes: Independent track — schedule alongside STORY-093/094 to keep the wave parallelizable. Mock UDP/TCP/TLS receivers should live under `internal/notification/syslog/syslogtest/` for reuse across unit + integration tests.
