# ADR-004: IMEI Binding Architecture — AAA-Side Local Enforcement

## Status: Accepted (2026-04-27)

## Context

Customer pre-sale Q&A revealed M2M-critical capability gaps:
- Q11: IMEI-based security and IMEI pool management
- Q12: SIM-to-device binding (SIM lock to device)
- Q13: IMEI/IMSI/CID validation
- Q14: Action and reporting for unverified devices
- Q40: Profiling (device fingerprinting)

The dominant M2M attack vector is SIM extraction: a SIM card on a low-cost IoT data plan is removed from its intended device (e.g., a smart meter) and inserted into a personal handset to abuse the data plan. Defense requires binding each SIM to its authorized device(s) and rejecting auth attempts from unauthorized devices.

The 3GPP-canonical solution is the **Equipment Identity Register (EIR)** — a network function queried by MME/SGSN/AMF over Diameter S13 (4G/EPC) or HTTP-based N17 (5G/SBA). EIR returns whitelist/greylist/blacklist verdicts for the IMEI presented at attach.

We must decide whether to integrate with operator EIRs or implement enforcement locally in the Argus AAA engine.

## Decision

**Implement IMEI binding enforcement locally in the Argus AAA engine; do not integrate with EIR (S13/N17) in v1.**

### Architecture

1. **Capture** (read-only, all three auth protocols):
   - RADIUS: parse `3GPP-IMEISV` VSA (vendor 10415, attr 20) on Access-Request
   - Diameter S6a: parse `Terminal-Information` AVP (350) — grouped, contains IMEI + Software-Version
   - 5G SBA: parse `PEI` (Permanent Equipment Identifier) from `Nudm_UEAuthentication` and `Namf_Communication`
   - Each parser normalizes to a 15-digit IMEI string + optional 2-digit Software Version, injected into SessionContext

2. **State storage** (PostgreSQL):
   - SIM table extended with: `bound_imei` (varchar 15), `binding_mode` (enum), `binding_status` (enum), `binding_verified_at`, `last_imei_seen_at`
   - `imei_whitelist` table: TAC ranges or full IMEIs that are pre-approved org-wide
   - `imei_greylist` table: quarantine — allow but log/alert
   - `imei_blacklist` table: deny outright (e.g., GSMA stolen-device exports)
   - `imei_history` table: append-only log of IMEI observations per SIM, for change-detection and forensics

3. **Binding modes** (per-SIM, set independently of pool membership):
   - `NULL` (default for migration): off, no binding logic runs
   - `strict` (1:1): SIM auth only when IMEI exactly matches `bound_imei`
   - `allowlist` (1:N): SIM auth allowed for any IMEI in a SIM-specific list
   - `first-use`: SIM auto-locks to first observed IMEI, then strict afterward
   - `tac-lock`: SIM allowed for any IMEI sharing the same TAC (first 8 digits)
   - `grace-period`: like first-use, but allows IMEI changes within a window (e.g., 72h)
   - `soft`: alert-only — never reject, but emit `imei_changed`/`device_mismatch` events

4. **Enforcement** (in-process, AAA path):
   - On Access-Request, evaluator looks up the SIM's `binding_mode`:
     - `NULL` → skip binding logic, proceed to existing policy evaluation
     - non-NULL → run binding check before policy DSL, emit `binding_status` into SessionContext
   - Policy DSL gains `device.*` namespace: `device.imei`, `device.tac`, `device.binding_status`, `device.imei_in_pool('whitelist'|'greylist'|'blacklist')`
   - Reject path emits Access-Reject with reason code, audit log, and notification event

5. **Operator/admin UX** (frontend):
   - Per-SIM control: SIM Detail → Device Binding tab
   - Bulk control: SIM List → Bulk Actions
   - Pool management: Settings → IMEI Pools (white/grey/black, bulk CSV import)
   - Policy gating: Policy Editor → `device.*` predicates
   - Cross-reference: IMEI Lookup tool, Pool Detail "Bound SIMs" link

## Alternatives Considered

### Option A: Stub-only EIR scaffolding
- **Description:** Implement S13/N17 handler skeletons that always-allow + log; defer real EIR integration to a future phase
- **Rejected because:** Adds protocol complexity (Diameter S13 grouped AVPs, 5G HTTP/2 service registration, NRF discovery) for zero v1 value. Risk of half-done scaffolding rotting before real integration arrives.

### Option C: Full functional S13 (and N17) integration
- **Description:** Real Diameter S13 client, peer to operator EIR, real-time MICR/MICA exchanges
- **Rejected because:** Our customer profile (M2M operators, enterprise IoT fleets) places the policy decision point at our AAA, not in an upstream HSS/MME. Forcing customers to operate or peer with a 3GPP EIR for our product to function adds operational burden disproportionate to the value. Test cost is high (real Diameter S13 inter-op testing, +2-3 weeks). Can be added later as an external lookup adapter without touching the v1 schema.

## Consequences

### Positive
- v1 ships with a complete, self-contained IMEI control plane — customers don't need an EIR
- Single source of truth (Argus DB) for binding state, audit, and reporting; no external dependency to debug when things go wrong
- Local pool management lets enterprises curate "approved devices" without touching their operator's EIR
- Policy DSL gains a new namespace cleanly; existing policies are unaffected
- All six binding modes (strict/allowlist/first-use/tac-lock/grace-period/soft) are achievable as pure SQL + in-process evaluation

### Negative
- No automatic propagation of GSMA CEIR (global stolen device list) — customers must import CSV exports manually if they want it
- We carry the responsibility for IMEI database hygiene (versus operator EIR which already exists)
- Future EIR integration will be an additive feature, not a refactor — requires care to keep the local enforcement path as the primary, EIR as an enrichment

### Risks
- **Operational risk:** customers may misconfigure binding_mode (e.g., enabling `first-use` on SIMs in a swap pool) and lock themselves out. Mitigation: NULL default for existing SIMs, opt-in workflow, admin re-pair tooling, audit logging of mode changes
- **Privacy risk:** IMEI is PII in many jurisdictions. Mitigation: same KVKK pseudonymization framework used elsewhere; IMEI columns participate in the existing data-portability and right-to-erasure flows

### Migration
- Existing 1M+ SIM rows: `binding_mode = NULL` on migration day → no behavior change, no risk
- New SIMs created post-migration: also `NULL` by default; admin chooses mode at provisioning or later
- No data backfill required

## Future Considerations

- EIR integration (S13/N17) added later as an external adapter that populates `imei_blacklist` from peer responses, leaving the local enforcement path unchanged
- GSMA CEIR connector for global stolen-device feed
- Device fingerprinting beyond IMEI (RAT, software version trends) builds on the same `imei_history` foundation

## References
- 3GPP TS 23.003 — IMEI and IMEISV format
- 3GPP TS 29.061 — RADIUS 3GPP VSA dictionary (incl. IMEISV attr 20, vendor 10415)
- 3GPP TS 29.272 — Diameter S6a/S13, Terminal-Information AVP (350)
- 3GPP TS 23.501, 29.510 — 5G SBA, PEI definition
- GSMA TS.06 — IMEI Database governance
- ADR-003 — Custom Go AAA engine (this ADR extends its policy pipeline)
