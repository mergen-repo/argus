# Scope — Argus

## Project Name & Description

Argus is an APN & Subscriber Intelligence Platform that enables enterprises to manage 10M+ IoT/M2M SIM cards across multiple mobile operators from a single, unified portal. It combines a custom-built AAA engine (RADIUS + Diameter), SIM/APN lifecycle management, multi-operator orchestration, a policy & charging engine (mini-PCRF), and real-time business intelligence — all in one product. Argus is delivered as a single artifact deployable both on-premise and as a cloud SaaS.

## Vision

Become the industry-standard platform for enterprise IoT connectivity management — competing with global leaders (Enea, Alepo, Nokia AAA) on protocol depth and IoT platforms (emnify, Cisco Jasper) on management capabilities, while surpassing all Turkish alternatives. No single competitor today covers all 5 layers in one product — that is Argus's core differentiator.

## Target Users

### Persona 1: Tenant Admin (Primary)
- **Role:** Enterprise IoT/M2M fleet manager (e.g., utility company connectivity lead)
- **Goals:** Manage millions of SIM cards, ensure uptime, control costs, comply with regulations
- **Pain Points:** Fragmented operator portals, no unified view, manual bulk operations, no policy automation, poor analytics

### Persona 2: SIM Manager
- **Role:** Day-to-day SIM operations staff
- **Goals:** Provision/activate/suspend SIMs, handle bulk imports, troubleshoot connectivity
- **Pain Points:** Can't handle 10M SIMs individually — needs group-first UX, segments, bulk actions

### Persona 3: Policy Editor
- **Role:** Network policy administrator
- **Goals:** Define QoS rules, FUP policies, charging rules per APN/operator/RAT-type
- **Pain Points:** No dry-run or rollback, policy changes affect millions instantly without safety net

### Persona 4: Operator Manager
- **Role:** Manages operator relationships and SLA monitoring
- **Goals:** Monitor operator health, manage failover, track SLA compliance
- **Pain Points:** No visibility into operator performance, manual failover

### Persona 5: Analyst
- **Role:** Business intelligence / reporting
- **Goals:** Usage analytics, cost optimization, anomaly detection, compliance reporting
- **Pain Points:** Data scattered across operator portals, no unified analytics

### Persona 6: API User (M2M Service Account)
- **Role:** External system integration
- **Goals:** Programmatic SIM management, event subscription, automation
- **Pain Points:** No unified API across operators

### Persona 7: Super Admin (Platform Operator)
- **Role:** Manages the Argus platform itself (tenants, system config, operator connections)
- **Goals:** Onboard new tenants, manage shared operator adapters, monitor system health
- **Pain Points:** N/A (new role specific to Argus)

## In Scope (v1)

### Layer 1: AAA Core Engine
- RADIUS server (RFC 2865/2866)
- Diameter base (RFC 6733) + 3GPP Gx/Gy applications
- 5G SBA proxy (HTTP/2 AUSF/UDM interface)
- EAP-SIM / EAP-AKA / EAP-AKA'
- Network slice authentication
- CoA/DM (Change of Authorization / Disconnect Message)
- Session management (concurrent session control, configurable max per SIM)
- High-availability (active-active clustering)
- Protocol resilience (retry, circuit breaker, dead letter queue per operator)
- RadSec (RADIUS/TLS), Diameter/TLS

### Layer 2: SIM & APN Lifecycle Management
- SIM provisioning (single + bulk import with auto-activate)
- SIM state machine: ORDERED → ACTIVE ↔ SUSPENDED → TERMINATED → PURGED + STOLEN/LOST
- Configurable KVKK/GDPR purge retention period
- eSIM first-class citizen (SM-DP+ API integration, cross-operator profile switch, bulk provisioning)
- APN CRUD with soft-delete (ARCHIVED state), hard block on delete with active SIMs
- IP address management (per-APN/per-operator pools, static reservation, conflict detection, dual-stack IPv4+IPv6, utilization alerts, configurable reclaim grace period)
- IMSI/MSISDN/ICCID inventory + MSISDN number pooling
- OTA SIM management (APDU commands)
- Bulk operations (async queue, partial success, retry failed, error report CSV, undo/rollback)

### Layer 3: Multi-Operator Orchestration
- Operator adapter framework (pluggable, mock simulator for dev)
- IMSI-prefix based intelligent routing
- Steering of Roaming (SoR) engine with RAT-type preference
- Configurable operator failover policy (reject / fallback-to-next / queue-with-timeout)
- Operator health check heartbeat
- Operator SLA monitoring + violation events
- Diameter ↔ RADIUS bridge
- Roaming agreement management
- RAT-type awareness (NB-IoT, LTE-M, 4G, 5G) across all layers

### Layer 4: Policy & Charging Control (mini-PCRF)
- QoS enforcement (bandwidth limit per APN/subscriber/RAT-type)
- Dynamic policy rules (time-of-day, location, quota, RAT-type)
- Charging rules (prepaid/postpaid, quota management)
- Fair usage policy (FUP) enforcement
- Slice-aware policy rules
- Policy DSL / rule engine (per-operator configurable)
- Policy versioning + rollback
- Dry-run simulation ("affects N SIMs")
- Staged rollout (canary: 1% → 10% → 100%) with concurrent policy versions
- CoA enforcement on policy changes

### Layer 5: BI & Analytics
- Real-time usage dashboards (per SIM/APN/operator/RAT-type)
- Anomaly detection (SIM cloning, abuse, data spikes)
- Cost optimization engine (cheapest operator routing)
- CDR processing & rating engine
- Carrier cost tracking + per RAT-type cost differentiation
- Compliance reporting (BTK/KVKK/GDPR)
- Built-in observability (metrics dashboard: auth/s, latency, error rate, session count)

### Portal & API
- Web management portal (React/Vite SPA, premium dark-first UI)
- Group-first UX (segments, saved filters, bulk actions — individual SIM is drill-down)
- Dashboard hierarchy (tenant overview → sections → detail)
- REST API (API-first, all operations)
- Event streaming (WebSocket/SSE)
- SMS Gateway (outbound for device management)
- Command palette (Ctrl+K)
- Connectivity diagnostics (auto-diagnosis, test, troubleshooting wizard)

### Cross-Cutting
- Multi-tenant (tenant_id everywhere, same code on-prem and SaaS)
- RBAC: Super Admin, Tenant Admin, Operator Manager, SIM Manager, Policy Editor, Analyst, API User
- Deep audit log (tamper-proof hash chain, before/after diff, pseudonymization on KVKK purge)
- API key management (rotation, rate limiting, scope restriction, revoke)
- Notification system (in-app + email + webhook + Telegram, multi-scope: per-SIM/APN/operator/system-wide with percentage thresholds)
- Tenant onboarding wizard
- 2FA (TOTP), JWT auth, OAuth2 client credentials
- Configurable rate limiting (per-tenant, per-API-key, per-endpoint)
- Full compliance suite (BTK, KVKK, GDPR, ISO 27001 audit logging)

### Enterprise Defaults
- Empty states, loading/skeleton, confirm dialogs, keyboard shortcuts
- Server pagination (50/page), filter debounce (300ms), virtual scrolling (500+ records)
- Data export (CSV), health check endpoint, DB migrations (versioned, reversible)
- Code splitting (React.lazy + Suspense)
- Credential security (.env, encrypted DB, masked API)

## Out of Scope

| Item | Rationale | Revisit? |
|------|-----------|----------|
| Own SM-DP+ server | GSMA SAS-SM certification + FIPS 140-2 L3 HSM unrealistic for solo dev; BTK requires local operator anyway | No |
| SGP.32 eIM | Ecosystem immature (spec released 2023), limited device support | Post-v1 when ecosystem matures |
| White-label | Reduces frontend complexity; can add theming later without architectural changes | Post-v1 |
| VoWiFi / WiFi Offload AAA | IoT/M2M focus, voice not in scope | Only if market demands |
| TACACS+ | Network device admin protocol, not IoT SIM management | Only if market demands |
| Geo-fencing | Nice-to-have, can be added to policy engine later | Post-v1 |
| Device management (firmware/health) | Different product category; Argus manages SIM/connectivity, not the device | No |
| EIR (S13/N17) integration | Phase 11 IMEI binding does **local** enforcement only per ADR-004 — no real-time queries to operator EIR | Post-v1 if operators expose interfaces |
| GSMA CEIR auto-feed | Phase 11 IMEI Pool supports manual CSV import of blacklists; live CEIR feed requires GSMA membership + commercial agreement | Post-v1 |
| Multi-Framed-Route per SIM (RFC 2865 attr 22) | Single Framed-Route per SIM is sufficient for IoT/M2M v1 fleet patterns | Backlog |
| HA Kubernetes / Helm chart deployment | v1 ships Docker Compose for both on-prem and SaaS; K8s manifests deferred | Backlog |
| Billing / PDF invoice module | Argus exposes CDR + webhook for downstream BSS systems; not a billing product | No (BSS-ready by design) |

## Success Metrics

| Metric | Target |
|--------|--------|
| AAA throughput | 10K+ auth/s per node |
| Auth latency | p50 <5ms, p95 <20ms, p99 <50ms |
| SIM scale | 10M+ SIMs managed |
| Operator support | 3+ operators concurrently |
| Portal response | <500ms page load (data-populated) |
| Uptime | 99.9% AAA core availability |
| Bulk operations | 10K+ SIMs per batch |
| Compliance | BTK + KVKK + GDPR + ISO 27001 audit-ready |

## Assumptions

1. Turkish mobile operators (Turkcell, Vodafone, TT Mobile) provide RADIUS/Diameter interfaces and SM-DP+ API access to enterprise customers
2. Enterprise customers have or will obtain private APN agreements with operators
3. Operators support standard 3GPP attributes for IMSI, MSISDN, APN selection
4. BTK regulations allow third-party platforms to manage SIMs via operator APIs (not direct provisioning)
5. IoT device SIMs use standard authentication (EAP-SIM/AKA), not proprietary methods

## Constraints

| Constraint | Impact |
|-----------|--------|
| Solo dev + Claude Code | Small stories, automated testing mandatory, AUTOPILOT mode |
| BTK regulation | eSIM must go through local operators, data localization |
| KVKK/GDPR | Personal data retention limits, purge automation, pseudonymization |
| Operator relationships from scratch | Mock simulator required for development, real integration later |
| On-prem + cloud same artifact | No hard cloud dependencies, S3-compatible, env-var config |
| Go backend performance | Sub-millisecond internal operations, Redis-first hot path |
