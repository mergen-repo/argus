# SCR-198: Settings — Log Forwarding (Syslog)

**Type:** Page
**Layout:** DashboardLayout
**Auth:** JWT (tenant_admin+)
**Route:** `/settings/log-forwarding`

## Mockup

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
│                    │  │ ● siem-dr                                    [⋮ ▼]  │  │
│                    │  │   10.4.18.22:514 · UDP · RFC 3164                    │  │
│                    │  │   Forwards: audit only                               │  │
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
```

### Empty state

```
│                                                                              │
│              ┌────────────────────────────────────────┐                      │
│              │   No syslog destinations configured.    │                     │
│              │                                          │                     │
│              │   Forward Argus events (audit, alerts,   │                     │
│              │   sessions, policy, IMEI) to your SIEM   │                     │
│              │   via RFC 3164 or RFC 5424 over UDP,     │                     │
│              │   TCP, or TLS.                           │                     │
│              │                                          │                     │
│              │   [+ Add Destination]   [Read GLOSSARY] │                      │
│              └────────────────────────────────────────┘                      │
```

### Add Destination slide-panel (rich form — Option C)

```
┌────────────────────────────── Add Destination ──────────────────[×]┐
│                                                                    │
│  Name (label):       [ siem-prod                              ]   │
│                                                                    │
│  Host:               [ splunk.corp.example.net                ]   │
│  Port:               [ 6514                                   ]   │
│                                                                    │
│  Transport:          ( ) UDP    ( ) TCP    (●) TLS                │
│                                                                    │
│  Format:             ( ) RFC 3164 (BSD legacy)                    │
│                      (●) RFC 5424 (modern, structured)             │
│                                                                    │
│  Facility (0–23):    [ 16  (local0) ▼                         ]   │
│  Severity floor:     [ informational ▼  ⓘ                     ]   │
│                                                                    │
│  Forward event categories:                                         │
│   ☑ Audit             ☑ Alerts          ☑ Sessions                 │
│   ☑ Policy            ☐ System          ☐ All (override)           │
│                                                                    │
│  ─── TLS (transport=tls only) ────────────────────────────────     │
│  CA bundle (PEM):    [ -----BEGIN CERTIFICATE-----            ▾]  │
│  Client cert (PEM):  [ -----BEGIN CERTIFICATE-----            ▾]  │
│  Client key (PEM):   [ -----BEGIN PRIVATE KEY-----            ▾]  │
│                                                                    │
│  [Test Connection]                                                 │
│                                                                    │
│                                  [Cancel]  [Save Destination]     │
└────────────────────────────────────────────────────────────────────┘
```

## Features
- **Per-destination card**: name + endpoint + transport/format + filter summary + last delivery state (success timestamp ✓ or last error message)
- **Enabled toggle**: live state pill (green ●) / disabled (grey ○); toggle persists via API-338 PATCH-to-disabled (per API _index v1 scope note)
- **Test Connection**: emits a synthetic RFC 3164/5424 frame to the destination; shows success ✓ or error message inline + toast
- **Filter rules**: checkboxes map to `filter.event_categories` array (audit, alert, session, policy, system); "All" toggles all five
- **TLS form**: CA / client cert / client key text-areas only shown when transport=`tls`; client validation enforces PEM headers
- **Severity floor**: optional minimum severity; events below the floor are dropped (drives RFC 5424 PRI computation)
- **Reuse**: rich form lives in a SlidePanel (width="md") per Option C; Delete uses compact Dialog confirm

## Empty state
- No destinations → centered illustrative card with brief explainer (what syslog is, when to enable, link to GLOSSARY) + dual CTA "+ Add Destination" / "Read GLOSSARY"

## Error states
- 422 `INVALID_TRANSPORT` / `INVALID_FORMAT` → inline field error
- 422 `TLS_CONFIG_INVALID` → inline error on the offending PEM field with parse reason
- Test Connection failure → inline red banner with the underlying network/TLS error string

## Permissions
- View / Add / Edit / Delete: `tenant_admin+` (per API-337/338)

## Components used
- **Atoms**: Badge (transport, format), Toggle (Enabled), Button (Add Destination, Test Connection, Edit, Delete), Pill (severity)
- **Molecules**: DestinationCard (compact summary row), CategoryCheckboxGroup, TLSPanel (collapsible PEM textareas)
- **Organisms**: DestinationListPanel, AddDestinationSlidePanel (rich form — Option C), DeleteDestinationDialog (compact confirm — Option C)

## API endpoints used
- API-337 GET `/api/v1/settings/log-forwarding` — list destinations + per-destination state
- API-338 POST `/api/v1/settings/log-forwarding` — add or update (PATCH-to-disabled drives enable/disable + delete-equivalent per API _index v1 scope)

## Tables used
- TBL-61 `syslog_destinations` (Phase 11 — allocated 2026-04-27 in db/_index.md; columns: id, tenant_id, name, host, port, transport `udp|tcp|tls`, format `rfc3164|rfc5424`, filter_categories TEXT[], enabled, last_delivery_at, last_error, created_by/at, updated_at; UNIQUE (tenant_id, name); RLS tenant-scoped)

## Stories
- STORY-098 (primary — Native Syslog Forwarder)
