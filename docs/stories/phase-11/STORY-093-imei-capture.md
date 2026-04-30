# STORY-093: IMEI Capture — RADIUS + Diameter S6a + 5G SBA

## User Story
As a platform operator, I want Argus to capture the device IMEI from every authentication and notification flow across RADIUS, Diameter S6a, and 5G SBA, so that downstream binding logic, audit, and reporting can reason about device identity without changing how authentication itself behaves.

## Description
Implement read-only IMEI capture across the three AAA protocol surfaces. The captured value is normalized into the in-memory `SessionContext` (15-digit IMEI plus optional 2-digit software version) and made available to subsequent policy and audit pipelines. No enforcement happens in this story; if the IMEI is missing, malformed, or unparseable, authentication MUST proceed exactly as it does today. This story is the foundation for STORY-094..097 (binding model, pools, enforcement, change detection) and is gated by ADR-004 / DEV-411.

The three capture points:
- **RADIUS:** parse the `3GPP-IMEISV` Vendor-Specific Attribute (vendor 10415, attribute 20) on Access-Request. Wire format is BCD-encoded `IMEI,SoftwareVersion` (16 BCD digits). Normalize to 15-digit IMEI + 2-digit SV.
- **Diameter S6a:** parse the grouped `Terminal-Information` AVP (350) on inbound `Notify-Request` / `Update-Location-Request`. Inner AVPs: `IMEI` (1402, UTF8String), `Software-Version` (1403, UTF8String), `IMEI-SV` (1404, UTF8String).
- **5G SBA:** parse the `PEI` (Permanent Equipment Identifier) JSON field from `Nudm_UEAuthentication_Get` and `Namf_Communication_*` payloads. Format is `imei-<15 digits>` or `imeisv-<16 digits>` per TS 23.501 §5.9.

## Architecture Reference
- Services: SVC-04 (AAA Engine)
- Packages: `internal/aaa/radius`, `internal/aaa/diameter`, `internal/aaa/sba`, `internal/policy/dsl/evaluator.go` (SessionContext extension)
- Source: `docs/architecture/PROTOCOLS.md` (IMEI Capture section), `docs/architecture/DSL_GRAMMAR.md` (`device.imei`, `device.imeisv`, `device.software_version`)
- Spec: `docs/adrs/ADR-004-imei-binding-architecture.md`, `docs/brainstorming/decisions.md` DEV-411
- Standards: 3GPP TS 29.061 (RADIUS 3GPP VSA), TS 29.272 (Diameter S6a, AVP 350/1402/1403/1404), TS 23.003 (IMEI/IMEISV format), TS 23.501 §5.9 (PEI), TS 29.510 (5G SBA)

## Screen Reference
- No new screens. SCR-021f (Device Binding tab) and SCR-050 (Live Sessions) consume captured IMEI starting in STORY-094 / STORY-097; this story only ensures the value is present in `SessionContext`.

## Acceptance Criteria
- [ ] AC-1: `SessionContext` struct (in `internal/policy/dsl/evaluator.go` or equivalent) is extended with two new fields: `IMEI string` (15 digits, empty when not captured) and `SoftwareVersion string` (2 digits, empty when not captured). Both fields are zero-value safe and exported.
- [ ] AC-2: RADIUS Access-Request handler parses the `3GPP-IMEISV` VSA (vendor 10415, attribute 20) when present. BCD decoding produces a 15-digit IMEI and 2-digit SV. The normalized values populate `SessionContext.IMEI` / `SessionContext.SoftwareVersion`.
- [ ] AC-3: Diameter S6a handler parses the grouped `Terminal-Information` AVP (350) on `Notify-Request` and `Update-Location-Request`. Sub-AVPs `IMEI` (1402), `Software-Version` (1403), `IMEI-SV` (1404) are decoded. When both `IMEI` and `Software-Version` are present, `SessionContext.IMEI` is set from 1402 and `SessionContext.SoftwareVersion` from 1403; when only `IMEI-SV` (1404) is present, the 16-digit value is split into 15 digits + 2 digits.
- [ ] AC-4: 5G SBA handler parses the `pei` JSON field from `Nudm_UEAuthentication_Get` request and `Namf_Communication_*` payloads. Format `imei-<15>` populates `SessionContext.IMEI`; format `imeisv-<16>` populates both `IMEI` (first 15 of 16) and `SoftwareVersion` (last 2). Unknown prefixes (e.g., `mac-`, `eui-`) are ignored without error.
- [ ] AC-5: Capture is read-only and null-safe. Missing VSA / AVP / PEI MUST NOT block authentication or change any existing decision. `SessionContext.IMEI` simply remains the empty string. No new error paths are introduced into the AAA happy path.
- [ ] AC-6: Malformed input (non-numeric digits, wrong length, BCD parity errors, malformed grouped AVP, malformed JSON `pei`) is logged at `WARN` level with correlation ID and protocol tag, but the auth flow continues with empty IMEI/SV. No panics, no goroutine leaks.
- [ ] AC-7: Unit tests with golden byte fixtures: at least 3 RADIUS VSA cases (valid, malformed BCD, absent), 3 Diameter cases (full Terminal-Information, IMEI-SV only, malformed grouped AVP), 3 5G PEI cases (`imei-` valid, `imeisv-` valid, unknown prefix). Each test asserts the resulting `SessionContext.IMEI` / `SoftwareVersion` values.
- [ ] AC-8: Existing RADIUS, Diameter, and 5G SBA test suites (introduced in STORY-015, STORY-019, STORY-020) pass unchanged — zero behavioral regression. CI green on full `make test` matrix.
- [ ] AC-9: Audit log entries created by AAA on auth decisions are unchanged in shape; the new IMEI / SV fields are made available to downstream consumers (e.g., STORY-094 enricher) but no new audit action is added in this story.
- [ ] AC-10: Capture latency overhead measured: VSA / AVP / PEI parsing adds ≤ 200 µs p95 per Access-Request on the existing 1M-SIM bench. Recorded in story-093 plan addendum or perf note.

## Dependencies
- Blocked by: STORY-015 (RADIUS server), STORY-019 (Diameter server), STORY-020 (5G SBA proxy)
- Blocks: STORY-094, STORY-095, STORY-096, STORY-097

## Test Scenarios
- [ ] Unit: RADIUS — Access-Request with valid `3GPP-IMEISV` VSA → `SessionContext.IMEI` = 15 digits, `SoftwareVersion` = 2 digits.
- [ ] Unit: RADIUS — Access-Request with no VSA → `SessionContext.IMEI` empty, auth proceeds.
- [ ] Unit: RADIUS — Access-Request with malformed BCD VSA → WARN log, `SessionContext.IMEI` empty, no panic.
- [ ] Unit: Diameter S6a — `Notify-Request` carrying full Terminal-Information AVP (350) with sub-AVPs 1402/1403 → `IMEI` and `SoftwareVersion` populated.
- [ ] Unit: Diameter S6a — `Update-Location-Request` carrying only IMEI-SV (1404) → `IMEI` = first 15, `SoftwareVersion` = last 2.
- [ ] Unit: Diameter S6a — malformed grouped AVP → WARN log, no panic, `SessionContext.IMEI` empty.
- [ ] Unit: 5G SBA — `Nudm_UEAuthentication_Get` with `pei = "imei-359211089765432"` → `IMEI` populated, `SoftwareVersion` empty.
- [ ] Unit: 5G SBA — `Namf_Communication_N1MessageNotify` with `pei = "imeisv-3592110897654321"` → `IMEI` (15) and `SoftwareVersion` (2) both populated.
- [ ] Unit: 5G SBA — `pei = "mac-aabbccddeeff"` → ignored, `IMEI` empty, no error.
- [ ] Integration: end-to-end RADIUS auth against the test rig with VSA → audit log captures auth result, `SessionContext.IMEI` available to downstream consumers, no behavior change.
- [ ] Integration: existing RADIUS / Diameter / 5G test suites (STORY-015/019/020) all pass unchanged.
- [ ] Performance: 1M-SIM bench — auth path p95 latency increase ≤ 200 µs vs. baseline.

## Effort Estimate
- Size: L
- Complexity: High (three protocols, BCD + grouped AVP + JSON parsing, golden-fixture unit tests, zero-regression mandate)
- Notes: Foundation for the IMEI epic. Read-only by design — keep the patch surgical and isolated to capture/normalize logic. Enforcement lands in STORY-096.
