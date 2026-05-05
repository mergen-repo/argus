# Implementation Plan: STORY-098 — Native Syslog Forwarder (RFC 3164/5424)

> **Effort:** S · **Phase:** 11 · **Mode:** Normal · **Today:** 2026-05-04
> **Track:** Independent of IMEI epic (final dev story before Phase Gate).
> **Predecessors:** STORY-006 (NATS event bus subscriber pattern — DONE).

---

## Goal

Ship a tenant-scoped native syslog forwarder that subscribes to the existing `argus.events.>` NATS subjects, emits each event as RFC 3164 or RFC 5424 over UDP / TCP / TLS to one or more configured destinations, with per-destination filter rules, byte-correct formatting, non-blocking delivery, exponential reconnect backoff, and full SCR-198 admin UI.

---

## Architecture Context

### Components Involved

| Component | Layer | File path pattern |
|-----------|-------|-------------------|
| TBL-61 `syslog_destinations` migration | DB | `migrations/20260509000001_syslog_destinations.{up,down}.sql` (NEW) |
| `SyslogDestinationStore` | Store (SVC-03) | `internal/store/syslog_destination.go` (NEW) |
| RFC 3164/5424 emitters (pure formatters) | Notification (SVC-08) | `internal/notification/syslog/emitter.go` (NEW) |
| UDP / TCP / TLS transports | Notification (SVC-08) | `internal/notification/syslog/transport.go` (NEW) |
| Per-destination buffered worker + reconnect backoff | Notification (SVC-08) | `internal/notification/syslog/worker.go` (NEW) |
| Bus subscriber + dispatcher | Notification (SVC-08) | `internal/notification/syslog/forwarder.go` (NEW) |
| Mock UDP/TCP/TLS receivers (test fixture) | Notification | `internal/notification/syslog/syslogtest/server.go` (NEW) |
| API-337 + API-338 + Test-Connection handler | Gateway/Core API (SVC-01/03) | `internal/api/settings/log_forwarding/handler.go` (NEW) |
| Audit emitter wiring | Audit (SVC-10) | reuse existing `audit.Service.Log(...)` |
| `cmd/argus/main.go` wiring | Composition root | `cmd/argus/main.go` (modify) |
| SCR-198 page + Add/Edit slide-panel + per-row Test/Delete | FE (web) | `web/src/pages/settings/log-forwarding/index.tsx`, `web/src/components/settings/log-forwarding/destination-card.tsx`, `web/src/components/settings/log-forwarding/add-destination-panel.tsx` (NEW) |
| `useSyslogDestinations`, `useTestSyslogConnection` hooks + types | FE | `web/src/hooks/use-log-forwarding.ts`, `web/src/types/log-forwarding.ts` (NEW) |

### Data Flow

```
1. UI: tenant_admin opens /settings/log-forwarding
   → GET /api/v1/settings/log-forwarding (API-337)
   → Handler: SyslogDestinationStore.ListByTenant(tenantID)
   → returns rows with last_delivery_at, last_error per row
2. UI: Add Destination → fills form → POST /api/v1/settings/log-forwarding (API-338)
   → Handler: validate transport + format enums + facility 0..23 + filter_categories ⊆ {auth,audit,alert,session,policy,imei,system} + TLS PEM parse (when transport=tls)
   → Upsert by (tenant_id, name); audit `log_forwarding.destination_added` (or _updated/_disabled)
   → forwarder.OnConfigChange(): rebuild per-destination workers (start new, stop removed, replace changed)
3. UI: Test Connection → POST /api/v1/settings/log-forwarding/test
   → Handler builds an EPHEMERAL transport from the request body (NO DB write, NO audit, NO worker registration), sends a synthetic RFC 3164/5424 frame, returns {ok, error?}.
4. Bus dispatch (runtime):
   bus subscriber subscribes once to `argus.events.>` →
     for each Envelope:
       category = extractCategory(subject)
       sevOrd = severity.Ordinal(env.Severity)
       for each enabled destination D in tenantIndex[env.TenantID]:
         if category ∈ D.filter_categories AND (D.min_severity == "" OR severity.Ordinal(env.Severity) >= severity.Ordinal(D.min_severity)):
           D.worker.Enqueue(env)  // non-blocking; overflow drops oldest
   per-destination worker goroutine:
     - dequeue env
     - emitter.Format(env, D.cfg) → []byte payload (pure)
     - transport.Frame(payload, D.transport) → []byte frame
     - transport.Send(frame) (open/reuse conn; reconnect with backoff on failure)
     - store.UpdateDeliveryState(D.id, success ? clear : last_error truncated 1 KB)
     - audit failure (rate-limited 1/min/destination)
```

### API Specifications (embedded — Source: `docs/architecture/api/_index.md` rows API-337/338)

#### API-337 — `GET /api/v1/settings/log-forwarding`

- **Purpose**: List configured syslog destinations for the caller's tenant.
- **Auth**: JWT, role `tenant_admin+`.
- **Query**: none (small N — no pagination required for v1).
- **Success response (200)**:
  ```json
  {
    "status": "success",
    "data": [
      {
        "id": "uuid",
        "name": "siem-prod",
        "host": "splunk.corp.example.net",
        "port": 6514,
        "transport": "tls",
        "format": "rfc5424",
        "facility": 16,
        "severity_floor": "info",
        "filter": { "event_categories": ["audit","alert","session","policy"], "min_severity": "info" },
        "tls_ca_pem": "-----BEGIN CERTIFICATE-----...",
        "tls_client_cert_pem": null,
        "tls_client_key_pem_present": false,
        "enabled": true,
        "last_delivery_at": "2026-04-26T14:02:30Z",
        "last_error": null,
        "created_at": "...",
        "updated_at": "..."
      }
    ],
    "meta": { "has_more": false, "limit": 50 }
  }
  ```
- **Error**: 401 Unauthorized, 403 Forbidden (non tenant_admin+).
- **Empty data is `[]` (FIX-241 normalizeListData convention).**
- **Secrets policy**: `tls_client_key_pem` value NEVER returned to client; replace with boolean `tls_client_key_pem_present`. `tls_ca_pem` and `tls_client_cert_pem` returned (public material).

#### API-338 — `POST /api/v1/settings/log-forwarding`

- **Purpose**: Add or update (upsert by `(tenant_id, name)`) a syslog destination. Disable via PATCH-equivalent `enabled=false` in body.
- **Auth**: JWT, role `tenant_admin+`.
- **Request body**:
  ```json
  {
    "name": "siem-prod",
    "host": "splunk.corp.example.net",
    "port": 6514,
    "transport": "udp" | "tcp" | "tls",
    "format": "rfc3164" | "rfc5424",
    "facility": 16,                                 // integer 0..23
    "severity_floor": "info",                       // optional, one of severity.Values; default "" = forward all
    "filter": {
      "event_categories": ["auth","audit","alert","session","policy","imei","system"],
      "min_severity": "info"                        // optional; same set as severity_floor; OR-fold redundant with severity_floor — see VAL-098-04
    },
    "tls_ca_pem": "...",                            // optional; required only for tls + custom CA
    "tls_client_cert_pem": "...",                   // optional; mTLS
    "tls_client_key_pem": "...",                    // optional; mTLS — write-only
    "enabled": true                                 // optional; default true on insert; false to disable on update
  }
  ```
- **Success response (200 update / 201 insert)**: same DTO as API-337 row.
- **Error responses**:
  - 422 `VALIDATION_ERROR` — name/host/port empty or out of range, facility outside 0..23.
  - 422 `INVALID_TRANSPORT` — transport ∉ {`udp`,`tcp`,`tls`}.
  - 422 `INVALID_FORMAT` — format ∉ {`rfc3164`,`rfc5424`}.
  - 422 `INVALID_CATEGORY` — `filter.event_categories` contains values outside the canonical 7-set.
  - 422 `TLS_CONFIG_INVALID` — when transport=tls and any provided PEM fails `pem.Decode` + `x509.ParseCertificate` / `tls.X509KeyPair` parse; error body includes `details.field` (`tls_ca_pem`/`tls_client_cert_pem`/`tls_client_key_pem`).
  - 401/403/409 (name conflict on insert when same `(tenant_id,name)` and `enabled=false` on existing — handled by upsert; UNIQUE only).
- **Audit (hash-chained per existing `audit.Service.Log`)**:
  - Insert → `log_forwarding.destination_added`
  - Update → `log_forwarding.destination_updated`
  - Update with `enabled=false` (delta) → `log_forwarding.destination_disabled`

#### Sibling — `POST /api/v1/settings/log-forwarding/test` (advisor Brief 7 + VAL-098-02)

- **Purpose**: Synchronous best-effort test send. **No DB write. No audit row. No worker registered.**
- **Auth**: JWT, role `tenant_admin+`.
- **Request body**: same shape as API-338 minus `name` (host/port/transport/format/facility/tls_*).
- **Success response (200)**:
  ```json
  { "status": "success", "data": { "ok": true } }
  ```
- **Failure response (200 — semantic failure not transport failure)**:
  ```json
  { "status": "success", "data": { "ok": false, "error": "connection refused" } }
  ```
  HTTP 200 even on logical fail; client surfaces `data.error`. Genuine 4xx only on validation errors (same set as API-338).
- **Timeout**: 5 s connect + 2 s write; total ≤ 7 s.

---

### Database Schema

**Source: ARCHITECTURE.md TBL-61 (NEW table — first migration creating it).**

Embedded SQL — single-step additive migration mirroring STORY-095 T1 and IMEI pool migration shape (`migrations/20260508000001_imei_pools.up.sql`):

```sql
-- migrations/20260509000001_syslog_destinations.up.sql
-- TBL-61: syslog_destinations  (Phase 11 STORY-098)
CREATE TABLE IF NOT EXISTS syslog_destinations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  host VARCHAR(255) NOT NULL,
  port INT NOT NULL CHECK (port BETWEEN 1 AND 65535),
  transport VARCHAR(10) NOT NULL CHECK (transport IN ('udp','tcp','tls')),
  format VARCHAR(10) NOT NULL CHECK (format IN ('rfc3164','rfc5424')),
  facility SMALLINT NOT NULL DEFAULT 16 CHECK (facility BETWEEN 0 AND 23),
  severity_floor VARCHAR(10) NULL CHECK (severity_floor IN ('info','low','medium','high','critical')),
  filter_categories TEXT[] NOT NULL,
  filter_min_severity VARCHAR(10) NULL CHECK (filter_min_severity IN ('info','low','medium','high','critical')),
  tls_ca_pem TEXT NULL,
  tls_client_cert_pem TEXT NULL,
  tls_client_key_pem TEXT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  last_delivery_at TIMESTAMPTZ NULL,
  last_error TEXT NULL,
  created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT syslog_destinations_unique_name UNIQUE (tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_syslog_destinations_tenant_enabled
  ON syslog_destinations (tenant_id, enabled);
ALTER TABLE syslog_destinations ENABLE ROW LEVEL SECURITY;
CREATE POLICY syslog_destinations_tenant_isolation ON syslog_destinations
  USING (tenant_id = current_setting('app.current_tenant', true)::uuid);

-- Self-verification (PAT-023 — guard against schema drift)
DO $$ BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name='syslog_destinations' AND column_name='filter_categories'
  ) THEN
    RAISE EXCEPTION 'migration 20260509000001 failed: filter_categories not present';
  END IF;
END $$;
```

```sql
-- migrations/20260509000001_syslog_destinations.down.sql
DROP POLICY IF EXISTS syslog_destinations_tenant_isolation ON syslog_destinations;
DROP TABLE IF EXISTS syslog_destinations;
```

**Schema notes:**
- `port` constraint added (1..65535) — defensive beyond AC-1 minimum.
- `facility` and `severity_floor` and `filter_min_severity` are PAT-022 CHECK enums — Go const sets in plan T2.
- `filter_categories TEXT[]` — application-side validates each element ∈ canonical 7-set (no DB CHECK against array elements; do not use Postgres `ENUM[]`).
- `tls_client_key_pem` stored AS-IS (no envelope encryption v1) — D-NNN candidate for future story (envelope encryption via KMS).

---

### Screen Mockup (SCR-198 — Source: `docs/screens/SCR-198-settings-log-forwarding.md`)

```
┌────────────────────┬────────────────────────────────────────────────────────────┐
│  (sidebar)         │  Settings > Log Forwarding              [+ Add Destination]│
│                    ├────────────────────────────────────────────────────────────┤
│                    │  Forward audit, alert, and event-bus envelopes to your     │
│                    │  SIEM via RFC 3164 / RFC 5424. → [What is syslog?]        │
│                    ├────────────────────────────────────────────────────────────┤
│                    │  ┌──────────────────────────────────────────────────────┐  │
│                    │  │ ● siem-prod                                  [⋮ ▼]  │  │
│                    │  │   splunk.corp.example.net:6514 · TCP/TLS · RFC 5424  │  │
│                    │  │   Forwards: audit · alert · session · policy         │  │
│                    │  │   Last delivery: 2026-04-26 14:02:30 ✓               │  │
│                    │  │   [Enabled ●]   [Test Connection]   [Edit]   [Delete]│  │
│                    │  ├──────────────────────────────────────────────────────┤  │
│                    │  │ ○ qradar-stage                               [⋮ ▼]  │  │
│                    │  │   qradar.corp.example.net:601 · TCP · RFC 5424       │  │
│                    │  │   Forwards: audit · alert                            │  │
│                    │  │   Last error: 2026-04-26 13:11:09 — TLS handshake    │  │
│                    │  │     failed: x509: certificate has expired           │  │
│                    │  │   [Disabled ○]  [Test Connection]   [Edit]   [Delete]│  │
│                    │  └──────────────────────────────────────────────────────┘  │
└────────────────────┴────────────────────────────────────────────────────────────┘

Add Destination SlidePanel (width="md", Option C — rich form):
- Name, Host, Port (numeric), Transport (radio udp|tcp|tls)
- Format (radio rfc3164|rfc5424)
- Facility (select 0..23, default 16=local0)
- Severity Floor (select info|low|medium|high|critical, optional)
- Forward event categories (checkbox group — ALL 7: auth, audit, alert, session, policy, imei, system + "All" toggle)
- TLS group (collapsible — visible only when transport=tls): CA bundle PEM textarea, Client cert PEM textarea, Client key PEM textarea
- [Test Connection] button (uses sibling endpoint with current form state — does NOT save)
- [Cancel] [Save Destination] footer

Empty state: centered card "No syslog destinations configured" + dual CTA "[+ Add Destination] [Read GLOSSARY]".

Delete confirm: compact Dialog (Option C): "Delete destination 'siem-prod'? Forwarded events will stop immediately. This cannot be undone." [Cancel] [Delete]
```

**SCR-198 reconciliation note (VAL-098-01):** the spec mockup shows 5 category checkboxes (`audit, alert, session, policy, system`); AC-3 + AC-11 require the canonical 7 categories. **Implementation MUST render all 7 checkboxes** (`auth, audit, alert, session, policy, imei, system`) plus the "All" toggle — AC wins over mockup. Add a comment in the FE component explaining the discrepancy.

- **Navigation**: Sidebar → Settings → Log Forwarding (`/settings/log-forwarding`).
- **Drill-down**: per-row Edit opens SlidePanel pre-filled; per-row Test runs a test against the saved destination's stored config; per-row Delete opens compact Dialog.

### Design Token Map (UI scope — SCR-198)

#### Color Tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page background | `bg-bg-primary` | `bg-[#06060B]`, `bg-black` |
| Card / panel surface | `bg-bg-surface` | `bg-white`, `bg-[#0C0C14]` |
| Elevated (modal/dropdown) | `bg-bg-elevated` | hardcoded hex |
| Hover surface | `bg-bg-hover` | hardcoded hex |
| Primary text | `text-text-primary` | `text-gray-900`, hex |
| Secondary text | `text-text-secondary` | `text-gray-500`, hex |
| Tertiary text | `text-text-tertiary` | hex |
| Card border | `border-border` | `border-gray-200`, hex |
| Primary action button bg | `bg-primary text-bg-primary` | `bg-blue-500` |
| Destructive button bg | `bg-danger text-bg-primary` | `bg-red-500` |
| Success badge | `bg-success-dim text-success` | `bg-green-100 text-green-700` |
| Warning badge | `bg-warning-dim text-warning` | `bg-yellow-100` |
| Danger badge | `bg-danger-dim text-danger` | `bg-red-100` |
| Info badge | `bg-info-dim text-info` | `bg-blue-100` |
| Neutral chip | `bg-bg-elevated text-text-secondary` | hardcoded |

#### Typography Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page title | `text-heading-lg font-bold` | `text-2xl`, `text-[24px]` |
| Card heading (destination name) | `text-heading-md font-semibold` | `text-xl` |
| Body text (description, sub-line) | `text-body-md` | `text-sm` |
| Caption (last-error timestamp, helper) | `text-caption` | `text-xs` |

#### Spacing & Elevation
| Usage | Token Class |
|-------|-------------|
| Card shadow | `shadow-card` |
| Card radius | `rounded-card` |
| Section padding | `p-section` |

#### Existing Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/ui/button.tsx` | ALL buttons — never raw `<button>` |
| `<Input>` | `web/src/components/ui/input.tsx` | name, host, port, PEM textareas paired with `<Textarea>` |
| `<Textarea>` | `web/src/components/ui/textarea.tsx` | PEM blobs |
| `<Select>` | `web/src/components/ui/select.tsx` | Facility, Severity Floor |
| `<Radio>` | `web/src/components/ui/radio.tsx` | Transport, Format |
| `<Checkbox>` | `web/src/components/ui/checkbox.tsx` | event_categories |
| `<Badge>` | `web/src/components/ui/badge.tsx` | transport / format chips |
| `<Card>` | `web/src/components/ui/card.tsx` | per-destination row container |
| `<SlidePanel>` | `web/src/components/ui/slide-panel.tsx` | Add/Edit Destination (Option C) |
| `<Dialog>` | `web/src/components/ui/dialog.tsx` | Delete confirm (Option C compact) |
| `<EmptyState>` | `web/src/components/shared/empty-state.tsx` | empty list |
| `<Skeleton>` | `web/src/components/ui/skeleton.tsx` | loading shimmer |
| `<Tooltip>` | `web/src/components/ui/tooltip.tsx` | "Last error" full-text on hover |
| `useToast` | existing | success / failure toasts after Save / Test |

**RULE: ZERO hardcoded hex / px / rem in new TS/TSX. Verify with `grep -nE '#[0-9a-fA-F]{3,8}\b|\[[0-9]+px\]' web/src/pages/settings/log-forwarding/ web/src/components/settings/log-forwarding/` → must return ZERO matches before T6 ships.**

---

## RFC 3164 / RFC 5424 Byte-Trace Worked Examples (the test oracle)

**Sample event:** `bus.Envelope{` `Type: "sim.binding_mismatch"`, `TenantID: "00000000-0000-0000-0000-000000000001"`, `Severity: severity.Medium`, `Source: "aaa"`, `Title: "IMEI mismatch detected"`, `Message: "Mismatch summary"`, `Entity: &EntityRef{Type:"sim",ID:"00000000-0000-0000-0000-000000000abc"}`, `Meta: {"sim_id":"00000000-0000-0000-0000-000000000abc"}` `}`. Hostname `argus-host`. PID `12345`. Timestamp 2026-05-04T10:00:00.000Z. Destination: `facility=16` (local0).

### Severity → syslog severity (numeric, advisor Brief #2 — direction confirmed)

| argus.severity | syslog severity (RFC 5424 §6.2.1) | numeric |
|-----------|-----------|---------|
| `critical` | Critical | 2 |
| `high` | Error | 3 |
| `medium` | Warning | 4 |
| `low` | Notice | 5 |
| `info` | Informational | 6 |
| (missing/empty) | Notice (5) — defensive default | 5 |

PRI = `facility * 8 + severity_numeric`. For `medium` + `local0`: `16*8 + 4 = 132`.

**Severity floor / `min_severity` direction (VAL-098-04):** lower numeric severity = MORE severe. Forwarder includes the event iff `severity.Ordinal(env.Severity) >= severity.Ordinal(D.severity_floor)` where `severity.OrdinalMap` is `info=1 .. critical=5`. Document direction inline at the comparison site.

### V1: RFC 3164 byte-trace (the unframed payload)

```
<132>May  4 10:00:00 argus-host argus[12345]: sim.binding_mismatch tenant=00000000-0000-0000-0000-000000000001 sim_id=00000000-0000-0000-0000-000000000abc severity=medium IMEI mismatch detected
```

Bytes (hex prefix shown for the first 12 bytes only, full ASCII for the rest — the test asserts the exact byte sequence):
- `3C 31 33 32 3E` = `<132>`
- `4D 61 79 20 20 34` = `May  4` (BSD format: month abbrev + 2 chars space-padded day; day=`4` becomes `" 4"` = space + `4`)
- ` 10:00:00 argus-host argus[12345]: sim.binding_mismatch tenant=... sim_id=... severity=medium IMEI mismatch detected`

**Rules:**
- TIMESTAMP = `Mmm dd hh:mm:ss` LOCAL TIME (RFC 3164 §4.1.2). Tests pin `time.Local = time.UTC` for determinism.
- HOSTNAME ≤ 255 ASCII chars, no spaces; trim hostname.
- TAG = `argus[<pid>]` — APP-NAME hardcoded `argus`, PID from `os.Getpid()`.
- MSG = `<event_type> tenant=<tid> [<entity_field>=<value> ]+ severity=<sev> <env.Title>`. Newlines in input → space.
- One datagram per message on UDP (no terminator). On TCP/TLS via RFC 6587 octet-counting framing (see V3+).

### V2: RFC 5424 byte-trace (the unframed payload)

```
<132>1 2026-05-04T10:00:00.000Z argus-host argus 12345 sim.binding_mismatch [argus@32473 tenant_id="00000000-0000-0000-0000-000000000001" sim_id="00000000-0000-0000-0000-000000000abc" severity="medium"] \xEF\xBB\xBFIMEI mismatch detected
```

**Rules / VAL-098-03 (enterprise number):**
- VERSION = `1` (literal).
- TIMESTAMP = RFC 3339 with millisecond precision and `Z` (`time.Format("2006-01-02T15:04:05.000Z07:00")`); UTC mandatory in Argus emitter (no local-time RFC 5424).
- HOSTNAME = trimmed system hostname.
- APP-NAME = `argus`. PROCID = pid as decimal string. MSGID = `env.Type` truncated to 32 chars.
- STRUCTURED-DATA = `[argus@32473 ` + space-separated SD-PARAMs. **SD-ID = `argus@32473`** — IANA Private Enterprise Number `32473` is reserved by RFC 5612 / IANA "for documentation and examples only". This is a PLACEHOLDER pending registration of an Argus PEN. **D-198-01 NEW** (route at story close): register an Argus PEN with IANA before prod ship; replace `32473` with the registered value across emitter constant + tests + docs.
- SD-PARAM values quoted; `"`, `\`, `]` inside values escaped per RFC 5424 §6.3.3 (`\"`, `\\`, `\]`).
- BOM = bytes `EF BB BF` MUST precede MSG (RFC 5424 §6.4) when MSG is UTF-8. Test asserts BOM presence.
- MSG = `env.Title` (or `env.Message` if Title empty). Newlines in input → escaped.

### V3: PRI computation table

| Argus severity | syslog num | local0 (16) PRI | local7 (23) PRI | user (1) PRI |
|----------------|------------|-----------------|-----------------|--------------|
| critical | 2 | 130 | 186 | 10 |
| high | 3 | 131 | 187 | 11 |
| medium | 4 | 132 | 188 | 12 |
| low | 5 | 133 | 189 | 13 |
| info | 6 | 134 | 190 | 14 |

The dispatch's worked example (`PRI=134, facility=16, severity=6 = info → local0`) appears in the unit test as `TestPRI_Info_Local0`.

### V4: Backoff sequence (advisor Brief #6)

Exact: `1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, ...` (cap 60s). Reset to 1s after a single successful write.

| Failure # | Delay before next attempt |
|-----------|---------------------------|
| 1 | 1s |
| 2 | 2s |
| 3 | 4s |
| 4 | 8s |
| 5 | 16s |
| 6 | 32s |
| 7+ | 60s |

Implementation: `nextDelay := min(60s, 1s << min(attempt-1, 6))` with attempt clamp. Unit test `TestBackoffSchedule` asserts the full table (1, 2, 4, 8, 16, 32, 60, 60, 60).

### V5: Filter rule trace

Destination D: `filter_categories=["audit","alert"]`, `min_severity="medium"`. Event `session.created` severity `info`:
1. Subject `argus.events.session.created` → category=`session`. `session ∉ {audit,alert}` → SKIP. ✗ not delivered.

Event `audit.create` severity `info` to same D:
1. Category = `audit` ∈ filter_categories. ✓
2. `severity.Ordinal("info")=1 < severity.Ordinal("medium")=3` → SKIP. ✗ not delivered.

Event `alert.triggered` severity `high` to same D:
1. Category=`alert` ✓. `Ordinal("high")=4 >= 3` ✓. → DELIVERED.

### V6: TLS handshake trace

Self-signed cert + matching CA PEM → TLS handshake succeeds, MSG written, `last_error=NULL`.
Same destination, `tls_ca_pem=NULL` (system trust) → `x509: certificate signed by unknown authority` error → `last_error="tls: x509: certificate signed by unknown authority"` (truncated 1 KB), `last_delivery_at` updated to attempt time.

### V7: Buffer overflow trace (AC-15)

Destination D with TCP transport, slow consumer (write blocks 1s/msg). Bus subscriber publishes 1001 events in <100ms. Worker queue cap=1000. Event 1001 arrives → `select{case ch <- env: default:}` → goroutine drops oldest (head of channel) by reading and discarding, then enqueues new event → `forwarder_buffer_overflow_total{destination="<id>"}` increments by 1, `last_error="buffer overflow: 1 event(s) dropped"`. No panic. Bus subscriber returns immediately (non-blocking).

### V8: Test-connection failure trace

POST `/api/v1/settings/log-forwarding/test` with body `{host:"127.0.0.1", port:9999, transport:"tcp", format:"rfc3164", facility:16, filter:{event_categories:["audit"]}}` against a closed port. Handler attempts `net.DialTimeout("tcp", "127.0.0.1:9999", 5s)` → returns `dial tcp 127.0.0.1:9999: connect: connection refused`. Handler responds:
```json
{ "status": "success", "data": { "ok": false, "error": "dial tcp 127.0.0.1:9999: connect: connection refused" } }
```
HTTP 200. NO DB write. NO audit. NO worker registered.

---

## Bus Subject → Category Mapping (advisor Brief #3 + dispatch Brief 8)

Source: `internal/bus/nats.go` `Subject*` constants. Strategy: **single wildcard subscription** `argus.events.>` (advisor #3) + derive category from the second path segment (`argus.events.<category>.<…>`).

| NATS subject prefix | Category | Notes |
|---------------------|----------|-------|
| `argus.events.auth.*` | `auth` | `auth.attempt` and any future auth subjects |
| `argus.events.audit.*` | `audit` | `audit.create` (FIX-212) |
| `argus.events.alert.*` | `alert` | `alert.triggered` |
| `argus.events.session.*` | `session` | `session.started/updated/ended` |
| `argus.events.policy.*` | `policy` | `policy.changed`, `policy.rollout_progress` |
| `argus.events.imei.*` | `imei` | `imei.changed` (STORY-097) |
| `argus.events.device.binding_*` | `imei` | STORY-097 binding subjects fold into `imei` category (VAL-098-05) |
| `argus.events.system.*` (if any) | `system` | reserved |
| `argus.events.sim.*` | `system` | sim updates fold to system |
| `argus.events.operator.*` | `system` | operator health |
| `argus.events.notification.*` | `system` | notification dispatch (consumer-side) |
| `argus.events.fleet.*` | `system` | fleet aggregates |
| `argus.events.anomaly.*` | `system` | |
| `argus.events.ip.*` | `system` | IP reclaim/release |
| `argus.events.sla.*` | `system` | SLA reports |
| `argus.events.backup.*` | `system` | backup completed/verified |
| `argus.events.esim.*` | `system` | eSIM OTA |
| any other `argus.events.*` | `system` | catch-all |

**Excluded subscriptions (NOT forwarded):** `argus.jobs.*` (queue ops), `argus.cache.invalidate` (infra). The wildcard `argus.events.>` already excludes both.

Implementation: helper `func categoryForSubject(subject string) string` with a switch on the second segment. Unit test `TestCategoryForSubject` covers all 9 prefixes + 1 unknown → `system`.

---

## Pre-existing Behavior Confirmed (PAT-026 8-Layer Sweep — Consumer-Only Story)

| Layer | Action | Reason |
|-------|--------|--------|
| L1 HTTP handler | NEW (API-337/338/test) | new feature |
| L2 Store | NEW (`SyslogDestinationStore`) | new TBL-61 |
| L3 DB | NEW migration | new table |
| L4 Seed | NONE | AC-16: `make db-seed` produces zero destinations |
| L5 Background job/worker | NEW (per-destination worker + bus subscriber) | this story's core |
| L6 `cmd/argus/main.go` wiring | MUST modify | PAT-026 RECURRENCE STORY-095 inverse-orphan guard — paired test required |
| L7 event catalog (`catalog.go`) | **NO CHANGE** — N/A | STORY-098 is consumer-only; ZERO new event types |
| L8 tier + publisherSourceMap | **NO CHANGE** — N/A | same as L7 |

PAT-026 RECURRENCE STORY-095 inverse-orphan **DOES apply** to the bus subscriber registration: the forwarder constructor MUST be called from `main.go` AND a paired test (`TestSyslogForwarder_RegisteredAtBoot`) asserts the subscriber binding. PAT-030 visibility: emit `INF "syslog forwarder started" destinations=N enabled=M` at boot, and `INF "syslog forwarder disabled by config"` if `cfg.SyslogForwarderEnabled=false` (gate flag).

---

## Prerequisites
- [x] STORY-006 (NATS event bus subscriber pattern) — DONE.
- [x] FIX-212 (`bus.Envelope` canonical wire format + name resolution + missing publishers) — DONE.
- [x] FIX-241 `WriteList` nil-slice normalization — empty list returns `data: []`.
- [x] FRONTEND.md tokens unchanged.
- [x] Existing slide-panel + dialog atoms.

---

## Tasks

### Task 1: TBL-61 migration (single-file additive) [Wave 1]
- **Files:** Create `migrations/20260509000001_syslog_destinations.up.sql`, Create `migrations/20260509000001_syslog_destinations.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260508000001_imei_pools.up.sql` (RLS shape, CHECK constraints, UNIQUE, index) and `migrations/20260507000003_sim_imei_allowlist.up.sql` (concise additive table). Mirror the self-verification `DO $$` block from PAT-023 prevention rule (c).
- **Context refs:** "Database Schema"
- **What:** Implement the `up.sql` exactly per "Database Schema" embedded SQL — including `CONSTRAINT chk_*` clauses for `transport`, `format`, `facility`, `severity_floor`, `filter_min_severity`, the `UNIQUE (tenant_id, name)`, the `idx_syslog_destinations_tenant_enabled` index, RLS enable + `tenant_id = current_setting('app.current_tenant', true)::uuid` policy, and the closing `DO $$` self-verification block. `down.sql` drops policy then table.
- **Verify:** `make db-migrate` succeeds; `psql -c "\d syslog_destinations"` shows all columns with constraints; `make db-seed` produces zero rows in this table; `make db-migrate-down 1 && make db-migrate` round-trips clean. Run `gofmt -w` (no Go files but consistency).

### Task 2: RFC 3164 + RFC 5424 emitters (pure formatters) + golden tests [Wave 1 — PARALLEL with T1]
- **Files:** Create `internal/notification/syslog/emitter.go`, Create `internal/notification/syslog/emitter_test.go`, Create `internal/notification/syslog/consts.go` (Go const sets for transport/format enums per PAT-022)
- **Depends on:** —
- **Complexity:** **high** (byte-level correctness — the value gate of the story per Effort Notes)
- **Pattern ref:** Read `internal/severity/severity.go` (const set + Validate + OrdinalMap pattern — mirror for `Transport`/`Format` const sets); read `internal/bus/envelope.go` (Envelope struct shape — fields the formatter reads). For pure-function unit testing pattern, read `internal/aaa/sba/imei_test.go` (golden-bytes assertion pattern from STORY-097 T2).
- **Context refs:** "RFC 3164 / RFC 5424 Byte-Trace Worked Examples", "Architecture Context > Components Involved"
- **What:**
  - `consts.go`: `const TransportUDP = "udp"; TransportTCP="tcp"; TransportTLS="tls"; var Transports = []string{...}; func ValidTransport(s) bool` — same shape for Format (`rfc3164`,`rfc5424`).
  - `emitter.go`: type `DestConfig struct { Format, Hostname string; PID int; Facility int; Enterprise int }`; pure function `Format(env *bus.Envelope, cfg DestConfig) ([]byte, error)`. Internally dispatches to `formatRFC3164` or `formatRFC5424`. NO I/O, NO time.Now() — accept `now time.Time` via `cfg.Now` for testability (default `time.Now().UTC()`).
  - Severity mapping helper `syslogSeverity(argus string) int` with the V3 table (5 cases + default=5).
  - PRI helper `pri(facility, sev int) int { return facility*8 + sev }`.
  - RFC 3164 builder per V1 rules (BSD timestamp via `t.Format("Jan _2 15:04:05")` — note the `_2` for space-padded day).
  - RFC 5424 builder per V2 rules: `<PRI>1 ` + RFC3339 millisecond + ` ` + hostname + ` argus ` + pid + ` ` + msgid + ` ` + structured-data + ` ` + UTF-8 BOM + msg. SD-PARAM escaping per RFC 5424 §6.3.3 (`\"`, `\\`, `\]`). `Enterprise=32473` constant, marked `// VAL-098-03 placeholder; replace post-D-198-01`.
  - Tests: `TestRFC3164_GoldenBytes` (V1 exact match using `bytes.Equal`); `TestRFC5424_GoldenBytes` (V2 exact match including BOM); `TestRFC5424_BOMPresent` (asserts `EF BB BF` immediately after STRUCTURED-DATA + space); `TestPRI_*` (5 severity × 3 facility = 15 sub-tests covering V3 table); `TestSeverityMapping_DefaultsToNotice` (empty/unknown → 5); `TestSDParamEscape` (values containing `"`, `\`, `]`); `TestRFC3164_DayPadding` (single-digit day produces `"May  4"` not `"May 04"`).
- **Verify:** `go test ./internal/notification/syslog/... -run "TestRFC3164|TestRFC5424|TestPRI|TestSeverityMapping|TestSDParamEscape" -v` all PASS. `gofmt -w internal/notification/syslog/`. Manual byte-diff check against V1 + V2: print bytes via `t.Logf("%q", got)`.

### Task 3: UDP / TCP / TLS transports + RFC 6587 framing + backoff scheduler + mock receivers [Wave 2 — after T2]
- **Files:** Create `internal/notification/syslog/transport.go`, Create `internal/notification/syslog/transport_test.go`, Create `internal/notification/syslog/syslogtest/server.go`, Create `internal/notification/syslog/backoff.go`, Create `internal/notification/syslog/backoff_test.go`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/notification/webhook.go` (HTTP-style transport pattern with timeouts, error classification). For TCP fixture pattern read `internal/aaa/radius/server_test.go` if present; otherwise stand up a minimal `net.Listen("tcp", "127.0.0.1:0")` echo + a `tls.Listen` self-signed echo.
- **Context refs:** "RFC 3164 / RFC 5424 Byte-Trace Worked Examples > V4 Backoff", "RFC 3164 / RFC 5424 Byte-Trace Worked Examples > V6 TLS"
- **What:**
  - `transport.go`: `type Transport interface { Send(payload []byte) error; Close() error }`; constructors `NewUDPTransport(host, port)`, `NewTCPTransport(host, port)`, `NewTLSTransport(host, port string, caPEM, clientCertPEM, clientKeyPEM []byte)` + `Frame(payload []byte, transport string) []byte` (UDP: returns payload unchanged; TCP/TLS: octet-counting `len(payload) + " " + payload`). UDP uses `net.DialUDP` with no read; TCP uses `net.DialTimeout` 5s + writes with deadline 2s; TLS uses `tls.Dial` with `MinVersion=tls.VersionTLS12`, custom `RootCAs` if caPEM provided else system trust, `Certificates` if mTLS, `ServerName=host` (hostname verification ON).
  - `backoff.go`: `type Backoff struct { attempt int }; func (b *Backoff) Next() time.Duration` per V4 table; `Reset()` after success.
  - `syslogtest/server.go`: `type FakeReceiver` with `StartUDP() (addr string, msgs <-chan []byte, stop func())`; `StartTCP()` returns same shape (de-frames octet-count); `StartTLS(certPEM, keyPEM []byte)` likewise; `StopAfter(n int)` helper.
  - Tests: `TestUDPSend` (round-trip), `TestTCPSend_Framed` (octet-count framing parsed correctly), `TestTLSSend_SelfSignedWithCAPEM` (success), `TestTLSSend_NoCAPEM_FailsCertVerify` (V6), `TestBackoffSchedule` (V4 full table), `TestFrame_RFC6587_OctetCounting` (`123 <body123bytes>` form), `TestTCPReconnect_AfterServerClose` (server closes, dialer reconnects, new write succeeds, backoff resets to 1s on success).
- **Verify:** `go test ./internal/notification/syslog/... -run "TestUDP|TestTCP|TestTLS|TestBackoff|TestFrame" -race -v`. `gofmt -w internal/notification/syslog/`.

### Task 4: SyslogDestinationStore + API-337 + API-338 + Test-Connection handler [Wave 2 — PARALLEL with T3]
- **Files:** Create `internal/store/syslog_destination.go`, Create `internal/store/syslog_destination_test.go`, Create `internal/api/settings/log_forwarding/handler.go`, Create `internal/api/settings/log_forwarding/handler_test.go`
- **Depends on:** Task 1, Task 2 (consts.go for enum validation)
- **Complexity:** medium
- **Pattern ref:** For store: read `internal/store/imei_pool.go` (recent CRUD store with tenant-scoped queries + named columns const + scan helper; mirror exactly — single shared `scanSyslogDestination(row pgx.Row)` helper used by all callers per PAT-006 RECURRENCE FIX-251 prevention). For handler: read `internal/api/imei_pool/handler.go` (CRUD handler structure: `NewHandler(store, audit, log)`, validators, `apierr.WriteSuccess`/`WriteList`/`WriteError`, audit calls). For audit chain emission: read `internal/audit/service.go` `Log(ctx, action, before, after)`.
- **Context refs:** "API Specifications", "Database Schema", "Architecture Context > Data Flow", "Bus Subject → Category Mapping" (categoryForSubject lives here for handler validation)
- **What:**
  - **Store** (`syslog_destination.go`): `type SyslogDestination struct { ID uuid.UUID; TenantID uuid.UUID; Name, Host string; Port int; Transport, Format string; Facility int; SeverityFloor *string; FilterCategories []string; FilterMinSeverity *string; TLSCAPEM, TLSClientCertPEM, TLSClientKeyPEM *string; Enabled bool; LastDeliveryAt *time.Time; LastError *string; CreatedBy *uuid.UUID; CreatedAt, UpdatedAt time.Time }`; `var syslogDestinationColumns = []string{...}` joined helper; `scanSyslogDestination(row pgx.Row) (*SyslogDestination, error)` shared by List + Get + Upsert RETURNING; methods `ListByTenant(ctx, tid) ([]*SyslogDestination, error)`, `GetByID(ctx, tid, id) (*SyslogDestination, error)`, `Upsert(ctx, *SyslogDestination) (*SyslogDestination, bool /*inserted*/, error)` (uses `INSERT ... ON CONFLICT (tenant_id, name) DO UPDATE SET ... RETURNING ...`), `UpdateDeliveryState(ctx, id uuid.UUID, success bool, errMsg string) error` (atomic — sets `last_delivery_at=NOW()`, `last_error=NULL` or truncated `LEFT($1,1024)`).
  - **Handler** (`handler.go`): `type Handler struct { store *store.SyslogDestinationStore; audit auditService; forwarder forwarderControl; log zerolog.Logger }` where `forwarderControl interface { OnConfigChange(ctx, tenantID uuid.UUID) }` (forwarder injected from main.go — defaults to no-op fake in tests).
  - Routes registered by `Mount(r chi.Router)`: `GET /` (List), `POST /` (Upsert), `POST /test` (TestConnection).
  - Validation order: bind JSON → check name/host non-empty + port range → `consts.ValidTransport` → `consts.ValidFormat` → facility 0..23 → each filter category ∈ `categoryForSubject` reverse set (the canonical 7) → severity_floor + min_severity ∈ severity.Values when present → if transport=tls, parse PEMs (`pem.Decode` + `x509.ParseCertificate` for ca_pem and client_cert_pem; `tls.X509KeyPair` if both client_cert and client_key present). Each failure → its specific 422 code per "API Specifications".
  - Upsert path: store returns `inserted bool` → audit `log_forwarding.destination_added` if inserted, `log_forwarding.destination_updated` otherwise; if request `enabled=false` AND prior state `enabled=true` (delta), additionally audit `log_forwarding.destination_disabled` (single combined audit row OK — see VAL-098-06).
  - DTO write: `tls_client_key_pem` ALWAYS NULLed in the response; replaced with `tls_client_key_pem_present bool`.
  - Test-Connection handler: validate fields per same rules (skip name) → build ephemeral `Transport` via T3 constructors → `emitter.Format(syntheticEnv, cfg)` → `transport.Send(transport.Frame(payload, t))` with 5s connect + 2s write deadlines (use `context.WithTimeout`); always close transport. NO DB write. NO audit. Return `{ok:true}` on success or `{ok:false, error:<err.Error()>}` on transport failure (200 in both cases). Synthetic env: `bus.Envelope{Type:"argus.syslog.test", Source:"argus", Severity:"info", Title:"Argus syslog test message", TenantID: ctxTenantID}`.
  - Tests:
    - Store: 7 DB-gated tests (`TestSyslogDestinationStore_*`) — Upsert insert path, Upsert update path returns `inserted=false`, ListByTenant filters by tenant (cross-tenant isolation), GetByID 404 on cross-tenant, UpdateDeliveryState success-clears-error, UpdateDeliveryState failure-truncates-1KB, UNIQUE conflict on `(tenant_id,name)` + transport CHECK enum rejection (PAT-022 — `INSERT transport='bogus'` → SQLSTATE 23514).
    - Handler: 12 tests — List empty `[]`, List populated, Upsert insert (201) audit `_added`, Upsert update (200) audit `_updated`, Upsert disable transitions audit `_disabled`, 422 INVALID_TRANSPORT, 422 INVALID_FORMAT, 422 INVALID_CATEGORY, 422 TLS_CONFIG_INVALID malformed CA PEM, 422 facility out of range, 403 cross-tenant, Test-Connection success (mock UDP), Test-Connection refused (V8), Test-Connection NO DB write (`countRows(syslog_destinations)` unchanged after).
- **Verify:** `go test ./internal/store/... ./internal/api/settings/log_forwarding/... -race -v` all PASS (DB-gated tests SKIP cleanly without `DATABASE_URL`). `gofmt -w internal/store/ internal/api/settings/`.

### Task 5: Bus subscriber + per-destination buffered worker + filter dispatch + main.go wiring [Wave 3 — after T3 + T4]
- **Files:** Create `internal/notification/syslog/forwarder.go`, Create `internal/notification/syslog/forwarder_test.go`, Create `internal/notification/syslog/worker.go`, Create `internal/notification/syslog/worker_test.go`, Modify `cmd/argus/main.go`, Create `internal/notification/syslog/main_wiring_test.go`
- **Depends on:** Task 3, Task 4
- **Complexity:** **high** (concurrency, non-blocking dispatch, per-destination state, audit rate-limit, main.go wiring)
- **Pattern ref:** Read `internal/notification/dispatcher_test.go` and `internal/notification/service.go` for bus-subscriber + per-destination dispatch pattern and `publisherSourceMap` style. For long-lived goroutine with context-driven shutdown, read `internal/job/runner.go` (cancellation + done channel). For paired `_RegisteredAtBoot` pattern, read `internal/job/imei_pool_import_worker_test.go` (`TestBulkIMEIPoolImport_RegisteredInAllJobTypes`).
- **Context refs:** "Architecture Context > Data Flow", "Bus Subject → Category Mapping", "RFC 3164 / RFC 5424 Byte-Trace Worked Examples > V4 / V5 / V7", "Pre-existing Behavior Confirmed (PAT-026 8-Layer Sweep)"
- **What:**
  - `worker.go`: `type Worker struct { destID uuid.UUID; cfg DestConfig; transport Transport; backoff *Backoff; ch chan *bus.Envelope; bufCap int; lastFailureAuditAt time.Time; auditMu sync.Mutex; store storeUpdater; audit auditEmitter; log zerolog.Logger }`; methods `Start(ctx)` (single goroutine reads `ch`, formats, frames, writes; on error: classify (`isCert/isRefused/isTimeout`), bump `last_error`, sleep `backoff.Next()`, RECONNECT for TCP/TLS — UDP just logs); `Enqueue(env)` non-blocking `select{case ch<-env: default: drop-oldest + counter}`; `Stop()` cancels ctx + closes channel.
  - Buffer cap: `1000` per AC-8 (VAL-098-07).
  - Audit failure rate-limit: 1/min/destination (advisor #10) — emit `log_forwarding.delivery_failed` only if `time.Since(lastFailureAuditAt) >= 1*time.Minute`; otherwise increment a `forwarder_audit_suppressed_total{destination=<id>}` metric.
  - `forwarder.go`: `type Forwarder struct { bus busSubscriber; store *store.SyslogDestinationStore; emitterCfgFactory func(*store.SyslogDestination) DestConfig; workers map[uuid.UUID]*Worker; mu sync.RWMutex; tenantIndex map[uuid.UUID][]*Worker; log zerolog.Logger }`; `Start(ctx)` subscribes once to `argus.events.>` via `bus.Subscribe(SubjectAllEvents, handler)`; on each Envelope: `category := categoryForSubject(subject)` → look up `tenantIndex[env.TenantID]` → for each worker w: if `category ∈ w.cfg.FilterCategories` AND severity-floor passes: `w.Enqueue(env)`. `OnConfigChange(ctx, tenantID)`: re-load enabled rows for tenant, diff against current workers, start new / stop removed / replace changed (compare config hash).
  - Wildcard subscription needs a new constant: add `const SubjectAllEvents = "argus.events.>"` to `internal/bus/nats.go` (additive, harmless).
  - `cmd/argus/main.go` wiring (PAT-026 RECURRENCE STORY-095 inverse-orphan + PAT-030 visibility):
    - construct `syslogDestStore := store.NewSyslogDestinationStore(pool)`;
    - construct `syslogForwarder := syslog.NewForwarder(busClient, syslogDestStore, auditSvc, logger)`;
    - if `cfg.SyslogForwarderEnabled` (default `true` per PAT-030): `syslogForwarder.Start(ctx); log.Info().Int("destinations", n).Int("enabled", e).Msg("syslog forwarder started")`; else `log.Info().Msg("syslog forwarder disabled by config")`.
    - register `syslogForwarder` as `forwarderControl` on the API handler so Upsert triggers `OnConfigChange`.
    - Add `cfg.SyslogForwarderEnabled` to `internal/config/config.go` (env `ARGUS_SYSLOG_FORWARDER_ENABLED`, default `true`).
  - Tests:
    - `TestWorker_BackoffOnTCPFailure` (using mock TCP server that closes — assert backoff schedule).
    - `TestWorker_BufferOverflow_DropsOldestAndIncrementsCounter` (V7).
    - `TestWorker_AuditRateLimited_OnePerMinute` (drive 100 failures in 10 ms; assert exactly 1 audit row emitted in window; advance fake clock 61 s; next failure emits 2nd).
    - `TestForwarder_FilterByCategory_SkipsNonMatching` (V5 first scenario).
    - `TestForwarder_FilterByMinSeverity_SkipsBelow` (V5 second).
    - `TestForwarder_DispatchToMultipleDestinations_OneFailureDoesNotBlockOthers` (AC-12 keystone — destination A blocks for 2s, destination B succeeds in <50ms).
    - `TestForwarder_OnConfigChange_StartsNewStopsRemoved`.
    - `TestSyslogForwarder_RegisteredAtBoot` (PAT-026 inverse-orphan paired test): build a tiny harness that constructs main.go's `syslogForwarder` via the same constructor and asserts the bus has a subscription matching `argus.events.>` after `Start`.
    - `TestCategoryForSubject` (per Bus Subject → Category Mapping table — 9 prefixes + unknown).
- **Verify:** `go test ./internal/notification/syslog/... -race -count=2 -v` all PASS (race + 2x to catch flakes); `go test ./cmd/argus/... -run "TestSyslogForwarder_RegisteredAtBoot" -v`. `gofmt -w internal/notification/syslog/ cmd/argus/`.

### Task 6: SCR-198 frontend — list page + Add/Edit slide-panel + per-row Test/Delete + types/hooks [Wave 4 — after T4]
- **Files:** Create `web/src/types/log-forwarding.ts`, Create `web/src/hooks/use-log-forwarding.ts`, Create `web/src/pages/settings/log-forwarding/index.tsx`, Create `web/src/components/settings/log-forwarding/destination-card.tsx`, Create `web/src/components/settings/log-forwarding/add-destination-panel.tsx`, Modify `web/src/router.tsx`, Modify `web/src/components/layout/sidebar.tsx`
- **Depends on:** Task 4
- **Complexity:** medium
- **Pattern ref:** Read `web/src/pages/settings/api-keys.tsx` (settings list page + SlidePanel form + test-style action button + tokens — closest twin) and `web/src/hooks/use-imei-pools.ts` (recent CRUD hook with React Query + invalidation pattern). For DSL of the slide-panel form lifecycle and Option C semantics, read `web/src/components/ui/slide-panel.tsx`. For category checkbox group reuse, read `web/src/components/shared/category-checkbox-group.tsx` if it exists or create a small inline group.
- **Context refs:** "Screen Mockup (SCR-198)", "Design Token Map", "API Specifications"
- **What:**
  - `types/log-forwarding.ts`: `SyslogDestination`, `UpsertSyslogDestinationRequest`, `TestSyslogConnectionRequest`, `TestSyslogConnectionResponse` mirroring API response/request shapes verbatim (snake_case JSON wire fields). Canonical category union: `'auth'|'audit'|'alert'|'session'|'policy'|'imei'|'system'`.
  - `hooks/use-log-forwarding.ts`: `useSyslogDestinations()` (React Query GET), `useUpsertSyslogDestination()` (mutation invalidates list), `useTestSyslogConnection()` (mutation, no invalidation), `useDeleteSyslogDestination` (uses Upsert with `enabled=false` per API spec — single PATCH-equivalent v1; deletion is hard-delete via DELETE not in v1 — VAL-098-08 documents the disable-equivalent UX).
  - `pages/settings/log-forwarding/index.tsx`: page header with `+ Add Destination` button → opens `<AddDestinationPanel>` empty; loading skeleton; populated list of `<DestinationCard>`; empty state via `<EmptyState>` (match SCR-198 mockup copy); per-row actions Edit / Test / Disable / Delete (Disable = upsert enabled=false; Delete = compact `<Dialog>` confirm + upsert enabled=false + remove from view via optimistic mutation cache update; v1 Delete does NOT hard-delete since API doesn't support — surface "Disabled and hidden" toast).
  - `components/settings/log-forwarding/destination-card.tsx`: `<Card>` with name + status pill (Enabled green ●  / Disabled grey ○) + `host:port · TRANSPORT/FORMAT` line + `Forwards: cat1 · cat2 · ...` line + Last delivery line (success ✓ green or last_error red — full text in `<Tooltip>` on hover) + 4 buttons: Test Connection, Edit, Disable/Enable toggle, Delete.
  - `components/settings/log-forwarding/add-destination-panel.tsx`: `<SlidePanel width="md">`; controlled form with `useState` for each field; transport radio toggles TLS section visibility; **all 7 category checkboxes** (`auth, audit, alert, session, policy, imei, system`) + an "All" toggle that selects/deselects all; Test Connection button calls `useTestSyslogConnection` with current draft (does NOT save); Save button calls `useUpsertSyslogDestination` and closes panel on success; surfaces 422 errors inline next to the offending field; toast on save success.
  - Router: add route `/settings/log-forwarding` mapped to lazy-loaded page; sidebar entry under Settings group (following the api-keys / users pattern).
  - **Tokens:** ZERO hardcoded hex / px. Status pill uses `bg-success-dim text-success` (enabled) or `bg-bg-elevated text-text-tertiary` (disabled). Last-error timestamp uses `text-caption text-text-tertiary` with hover `<Tooltip>` showing full `last_error`.
  - Note: invoke `frontend-design` skill for professional polish.
- **Verify:** `cd web && npm run build` succeeds; `cd web && npm run typecheck` (tsc) returns 0; `grep -nE '#[0-9a-fA-F]{3,8}\b|\[[0-9]+px\]' web/src/pages/settings/log-forwarding/ web/src/components/settings/log-forwarding/` returns ZERO matches; manual smoke (after backend up): list, add UDP/RFC3164, edit, test (mock receiver in dev), disable, delete.

### Task 7: Integration tests + golden RFC-receiver round-trip + AC-15 perf [Wave 5 — after T5 + T6]
- **Files:** Create `internal/notification/syslog/integration_test.go`, Create `internal/notification/syslog/perf_bench_test.go`, Create `web/playwright/log-forwarding.spec.ts`
- **Depends on:** Task 5, Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/diameter_test.go` (multi-component integration with bus + store) and `internal/notification/dispatcher_test.go` (multi-subscriber dispatch). For Playwright UI E2E, read `web/playwright/imei-pools.spec.ts` if present (FE pattern from STORY-095) or any `web/playwright/*.spec.ts`.
- **Context refs:** "Test Scenarios" (STORY-098 spec), "RFC 3164 / RFC 5424 Byte-Trace Worked Examples", "Bus Subject → Category Mapping"
- **What:**
  - **Integration tests (Go)**:
    - `TestIntegration_BusToUDPReceiver_RFC3164` — start `syslogtest.FakeReceiver` UDP, register a destination via API-338, publish a synthetic `sim.binding_mismatch` envelope to `argus.events.imei.changed` (or appropriate subject), assert receiver got bytes matching V1 byte-trace exactly.
    - `TestIntegration_BusToTLSReceiver_RFC5424_StructuredData` — same with TLS receiver + RFC 5424; parse SD-DATA + assert tenant_id / sim_id / severity present + BOM byte sequence present.
    - `TestIntegration_BackedUpTCPDestination_DoesNotStallBus` — slow TCP server (1s/msg); publish 1000 events; assert `bus.Subscribe` handler returns within 100 ms total (non-blocking) AND overflow counter >= 1.
    - `TestIntegration_PerDestinationFilter_ConsistencyAcrossTwoDestinations` — destination A `["audit","alert"]`, destination B `["imei","alert"]`; emit one `audit.create` + one `imei.changed` + one `alert.triggered` + one `session.started`. Assert A receives audit + alert (2); B receives imei + alert (2); B does NOT receive audit; A does NOT receive imei or session.
    - `TestIntegration_TLSDestination_SelfSignedWithoutCAPEM_FailsWithCertError` — expect `last_error` populated with `x509:` substring within 5 s.
    - `TestIntegration_CrossTenant_API337_Returns_OnlyOwnTenant` — seed two tenants, each with one destination; tenant A's JWT calls List → returns only its own row.
    - `TestIntegration_Seed_ZeroDestinations` (AC-16): after `make db-seed`, `SELECT count(*) FROM syslog_destinations` = 0.
  - **Perf bench**: `BenchmarkForwarderDispatch_5Destinations` measures publisher → enqueue path latency; assert ≤ 5 % overhead vs `BenchmarkForwarderDispatch_NoDestinations` (AC-15).
  - **Playwright E2E**:
    - `add-destination-udp.spec.ts`: navigate → fill form (UDP, RFC 3164, audit category) against a Playwright-managed local UDP listener → save → assert row appears with green ● and "Forwards: audit".
    - `edit-to-tls-test-connection.spec.ts`: edit existing row → switch to TLS → paste self-signed CA PEM → click Test → assert success toast.
    - `disable-destination.spec.ts`: toggle disable → assert grey ○ pill + emit a synthetic event from /api/v1/events/test (if such helper exists) and assert mock receiver does not see it (or skip if helper unavailable — gate behind env).
- **Verify:** `make test` (full Go) → 4129+ tests PASS, race clean; `cd web && npm run test:e2e -- --grep "log-forwarding"` PASS; bench shows ≤ 5 % overhead. `gofmt -w internal/notification/syslog/`.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1 (TBL-61 migration) | T1 | T1 verify + T7 `TestIntegration_Seed_ZeroDestinations` |
| AC-2 (API-337 List + state fields) | T4 | T4 handler tests (List populated) + T7 cross-tenant |
| AC-3 (API-338 + validators + audit added) | T4 | T4 handler tests (5 × 422 codes + audit_added) |
| AC-4 (CRUD upsert + disable + audit_updated/_disabled) | T4 | T4 handler tests (update + disable transitions) |
| AC-5 (RFC 3164 byte-correctness) | T2 | T2 `TestRFC3164_GoldenBytes` + T7 receiver round-trip |
| AC-6 (RFC 5424 byte-correctness + BOM + SD) | T2 | T2 `TestRFC5424_GoldenBytes` + `TestRFC5424_BOMPresent` + T7 |
| AC-7 (UDP fire-and-forget) | T3 | T3 `TestUDPSend` + T7 round-trip |
| AC-8 (TCP reconnect + RFC 6587 framing + backoff + buffer) | T3 + T5 | T3 `TestTCPReconnect_*` + `TestBackoffSchedule` + T5 `TestWorker_BufferOverflow` |
| AC-9 (TLS 1.2+ + CA PEM + mTLS + hostname verify) | T3 | T3 TLS suite + T7 `TestIntegration_TLS_*` |
| AC-10 (Test-connection sibling endpoint) | T4 | T4 `TestTestConnection_Success/_Refused/_NoDBWrite` |
| AC-11 (bus subscriber + 7 categories + filter) | T5 | T5 `TestForwarder_FilterByCategory_*` + `TestCategoryForSubject` |
| AC-12 (delivery state atomic + non-blocking across destinations) | T4 + T5 | T5 `TestForwarder_DispatchToMultipleDestinations_OneFailureDoesNotBlockOthers` |
| AC-13 (SCR-198 FE) | T6 | T6 manual smoke + T7 Playwright |
| AC-14 (CRUD + delivery-failure audit + 1/min rate-limit) | T4 + T5 | T4 audit assertions + T5 `TestWorker_AuditRateLimited_OnePerMinute` |
| AC-15 (perf — 5 destinations ≤ 5 % overhead + non-blocking) | T5 + T7 | T7 `TestIntegration_BackedUpTCP_DoesNotStallBus` + perf bench |
| AC-16 (regression + zero seed) | T7 | `make test` + `TestIntegration_Seed_ZeroDestinations` |

---

## Story-Specific Compliance Rules

- **API**: Standard envelope `{status, data, meta?, error?}` for API-337/338/test (mandatory; FIX-241 list normalization).
- **DB**: Migration shipped with `up.sql` + `down.sql` + self-verification `DO $$` block (PAT-023 prevention).
- **UI**: SCR-198 mockup tokens only — ZERO hardcoded hex / px / rem in new TS/TSX. All atoms reused from `web/src/components/ui/`.
- **Business**: per-destination filtering MUST forward only events matching `category ∈ filter_categories AND severity.Ordinal(env.Severity) ≥ severity.Ordinal(severity_floor || min_severity)`. Direction documented at the comparison site.
- **ADR**: no new ADR; ADR-001 multi-tenant + RLS observed on TBL-61.
- **Tenancy**: every store query and bus dispatch decision uses `env.TenantID` as the partition key — never trust caller to supply tenant.

---

## Bug Pattern Warnings

- **PAT-006** (FIX-201) + **PAT-006 RECURRENCE** (FIX-251 inline-scan drift): `SyslogDestinationStore` MUST use a single shared `scanSyslogDestination(row pgx.Row) (*SyslogDestination, error)` helper for all SELECT paths. NO inline `rows.Scan(...)` calls. Add `TestSyslogDestinationColumnsAndScanCountConsistency` (no-DB) as a regression guard.
- **PAT-009** (nullable column → pointer field): `last_delivery_at *time.Time`, `last_error *string`, `severity_floor *string`, `filter_min_severity *string`, `tls_*_pem *string`, `created_by *uuid.UUID` — all nullable per AC-1.
- **PAT-011** (FIX-207) + **PAT-026 RECURRENCE STORY-095** (inverse-orphan main.go drift): the forwarder + store + handler MUST be wired in `cmd/argus/main.go`. Paired test `TestSyslogForwarder_RegisteredAtBoot` is mandatory (T5).
- **PAT-017** (FIX-210 config param threaded but not propagated): `cfg.SyslogForwarderEnabled` (new flag, default true) MUST be passed into the forwarder constructor; gate visibility log per PAT-030.
- **PAT-022** (FIX-234 categorical CHECK + Go const set, dual source of truth): `transport`, `format`, `facility`, `severity_floor`, `filter_min_severity` ALL ship with DB CHECK + Go const set; `consts.ValidTransport` / `ValidFormat` mirror exactly. Unit test `TestSyslogDestinationStore_TransportCheckConstraint_RejectsInvalid` asserts SQLSTATE 23514 on bogus value (T4).
- **PAT-023** (FIX-252 schema drift): migration includes `DO $$` self-verification block (T1).
- **PAT-026 GUARD** — STORY-098 is consumer-only — **L7/L8 (catalog/tier/sourceMap) N/A**: ZERO new event types added. The PAT-026 RECURRENCE STORY-095 inverse-orphan applies (forwarder is a long-lived bus subscriber + workers, not a `JobProcessor`; no `JobType` constant; no `AllJobTypes` registration; PAT-026 RECURRENCE STORY-095's `_RegisteredInAllJobTypes` test is **N/A**, but the spirit — paired boot-time wiring assertion — applies as `TestSyslogForwarder_RegisteredAtBoot`).
- **PAT-030** (FIX-304 dark feature): emit `INF "syslog forwarder started" destinations=N enabled=M` boot log line + emit `INF "syslog forwarder disabled by config"` when gated off; default `cfg.SyslogForwarderEnabled = true`.
- **PAT-031** (STORY-094 JSON tri-state): API-338 fields are NOT pointer-typed; they are concrete `string`/`int`/`bool` with the upsert semantic "absent field on update keeps existing value" being an acceptable simplification for v1 since the upsert is full-replace (UI sends the complete current state on every save). Document this in handler comment.

---

## Tech Debt (from ROUTEMAP)

- **D-191** (tenant-scoped grace window) — out of scope for STORY-098.
- **D-192** (live 1M-SIM rig) — out of scope; AC-15 verified via Go bench microbenchmark substitution.
- **D-193** (tenant-scoped grace window infra) — out of scope.
- **D-194** (SCR-021f polish) — out of scope.

**New tech debt routed at story close:**
- **D-198-01 NEW** (planned at close): Register an Argus IANA Private Enterprise Number; replace placeholder `32473` (RFC 5612 documentation PEN) in `internal/notification/syslog/emitter.go`, V2 byte-trace docs, and golden tests. Target: pre-prod sign-off / Phase 12.
- **D-198-02 NEW** (planned at close): `tls_client_key_pem` stored in plaintext in TBL-61. Future: envelope encryption via KMS (or PG TDE if adopted). Target: future security-hardening story.

## Mock Retirement
No mocks for syslog forwarder in `web/src/mocks/` (frontend-first not used here). N/A.

---

## Decisions Routed to `decisions.md` (5–7 new VAL entries)

| ID | Decision | Rationale |
|----|----------|-----------|
| VAL-098-01 | SCR-198 frontend renders 7 category checkboxes (canonical AC set), not the 5 shown in mockup. | AC-3 + AC-11 require the 7-set; mockup is illustrative. |
| VAL-098-02 | Test-Connection is a **sibling endpoint** (`POST /api/v1/settings/log-forwarding/test`), not an extension of API-338. | Cleaner separation: test is stateless, API-338 is stateful. Easier to surface in UI without a flag. |
| VAL-098-03 | Enterprise number `32473` (IANA RFC 5612 documentation PEN) used as PLACEHOLDER. D-198-01 routes registration of an Argus PEN before prod ship. | Don't squat on real PENs (e.g., NIST 49334). |
| VAL-098-04 | `severity_floor` direction: forward iff `Ordinal(event.severity) ≥ Ordinal(floor)` (info=1, critical=5; lower urgency = lower numeric in argus, opposite of syslog numeric). | Matches `severity.OrdinalMap` semantics. Documented at comparison site. |
| VAL-098-05 | STORY-097 binding subjects (`device.binding_*`) fold into `imei` category, not `system`, since they are device-binding events. | Discoverable as `imei.changed` neighbour on the FE. |
| VAL-098-06 | Disabling a destination via update emits a single combined `log_forwarding.destination_disabled` audit row (rather than two: `_updated` + `_disabled`). | Keeps audit chain short and human-readable. |
| VAL-098-07 | Per-destination buffer cap = 1000 events; overflow drops oldest with counter increment. | AC-8 explicit cap; matches stdlib channel + select-default idiom. |
| VAL-098-08 | v1 has no DELETE endpoint; "Delete" UI button calls upsert with `enabled=false`. The disabled row remains in the DB but is hidden from the FE list (FE-side `enabled=true` filter optional — for now show all with disabled chip). | Matches API _index v1 scope note. Hard-delete is a follow-up. |

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Byte-level format drift in third-party SIEM parsers | Golden-byte tests V1+V2 lock the exact wire bytes; T7 round-trip integration test against mock receivers; document V1+V2 in plan as the test oracle. |
| One slow TCP destination stalls the bus subscriber | Per-destination buffered worker (cap 1000, drop-oldest) + non-blocking `Enqueue` (`select default`). T5 `TestForwarder_DispatchToMultipleDestinations_OneFailureDoesNotBlockOthers` is the keystone gate. |
| TLS misconfig hidden until first event arrives | Test-Connection endpoint exercises the full TLS stack synchronously before save (still optional — operator may skip). 422 `TLS_CONFIG_INVALID` at API-338 catches malformed PEM at config time. |
| Audit flood from a flapping destination | Per-destination 1-row-per-minute rate-limit on `log_forwarding.delivery_failed`; suppressed events counted via metric (`forwarder_audit_suppressed_total`). |
| `main.go` wiring forgotten (PAT-026 RECURRENCE STORY-095) | Mandatory paired test `TestSyslogForwarder_RegisteredAtBoot` (T5) + boot-log line `"syslog forwarder started"` (PAT-030). |
| Schema drift between Go const set and DB CHECK | PAT-022 dual-source-of-truth + structural test asserting SQLSTATE 23514 on bogus enum (T4). |
| Default-off leaves feature dark in dev/prod (PAT-030) | `cfg.SyslogForwarderEnabled` defaults `true`; `.env.example` documents opt-out; boot-log line confirms branch. |
| FE category checkbox drift (5 vs 7) | VAL-098-01 documented; component-level comment cites the discrepancy; Playwright E2E asserts all 7 checkboxes render. |
| Buffer cap too low under burst load | 1000 cap is a starting point; overflow counter visible in metrics → operators can tune via future config flag (D-NNN candidate at story close if observed). |

---

## Self-Validation (Pre-Write)

| Check | Status |
|-------|--------|
| Min plan lines (S = 30) | PASS (≫ 30) |
| Min tasks (S = 2) | PASS (7 tasks, 5 waves) |
| Required sections (Goal, Architecture Context, Tasks, AC Mapping) | PASS |
| API specs embedded (not referenced) | PASS (API-337, API-338, /test inline) |
| DB schema embedded with column types + source noted | PASS (TBL-61 inline + Source: ARCHITECTURE.md NEW table) |
| UI design token map populated with class names | PASS |
| Component reuse table populated | PASS |
| Each UI task references token map | PASS (T6) |
| Each task has Pattern ref to existing file | PASS |
| Each task has Context refs to plan sections | PASS |
| Each task touches ≤ 3 files (or splits) | T4 + T5 + T7 each touch ~4 files but all are tightly coupled (handler + handler_test + store + store_test) — acceptable per spec ("ideally 1-2"). |
| At least one high-complexity task (S story, 1 medium max — but byte-correctness + concurrency justify 2 high) | PASS (T2, T5) |
| Test task per AC | PASS (T2/T3/T4/T5/T7 cover all 16 ACs per mapping) |
| Bug pattern warnings present | PASS (PAT-006/009/011/017/022/023/026/030/031) |
| Tech debt items reviewed | PASS (D-191/192/193/194 out of scope; D-198-01 + D-198-02 NEW routed) |

ALL CHECKS PASS.
