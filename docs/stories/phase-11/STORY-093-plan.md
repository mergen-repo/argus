# Implementation Plan: STORY-093 — IMEI Capture (RADIUS + Diameter S6a + 5G SBA)

> Phase 11 — IMEI Epic foundation story. Read-only capture across three AAA protocols. Zero behavioural change to authentication. Effort = **L**.

## Goal

Capture the device IMEI (and optional 2-digit Software Version) from every authentication / notification flow on RADIUS, Diameter S6a, and 5G SBA, normalize to a 15-digit IMEI string + 2-digit SV, and surface both as new flat fields on `dsl.SessionContext` — without changing any auth decision, error path, or audit shape.

> Backend-only story. **Design Token Map = N/A** (no UI). **Component Reuse = N/A** (no UI). **Mock Retirement = N/A** (no FE mocks).

---

## Architecture Context

### Components Involved

| Component | Layer | Responsibility | File path pattern |
|-----------|-------|----------------|-------------------|
| RADIUS server | SVC-04 / `internal/aaa/radius` | Parse `3GPP-IMEISV` VSA on Access-Request; populate SessionContext before policy evaluation | `internal/aaa/radius/imei.go` (NEW), `internal/aaa/radius/server.go` (MOD) |
| Diameter base AVP | SVC-04 / `internal/aaa/diameter` | Define S6a AVP code constants (350/1402/1403/1404); add `ExtractTerminalInformation` parser using existing grouped-AVP infra | `internal/aaa/diameter/avp.go` (MOD), `internal/aaa/diameter/imei.go` (NEW) |
| 5G SBA types | SVC-04 / `internal/aaa/sba` | Add `PEI` field to `AuthenticationRequest` and `Amf3GppAccessRegistration`; parse PEI URI; populate SessionContext | `internal/aaa/sba/types.go` (MOD), `internal/aaa/sba/imei.go` (NEW), `internal/aaa/sba/ausf.go` + `udm.go` (MOD) |
| Policy DSL evaluator | SVC-05 / `internal/policy/dsl` | Extend `SessionContext` with `IMEI` + `SoftwareVersion` flat fields | `internal/policy/dsl/evaluator.go` (MOD) |
| Metrics | observability | `argus_imei_capture_parse_errors_total{protocol}` counter | `internal/observability/metrics/...` (MOD or local export) |

### Data Flow (read-only capture, all three protocols)

```
[RADIUS Access-Request]
   │
   ├─ rfc2865 lookup VSA Type 26 (vendor 10415, attr 20)
   ├─ extract3GPPIMEISV(pkt)  →  (imei string, sv string, ok bool)
   │     │
   │     ├─ ASCII "<15>,<2>"  → preferred (TS 29.061 §16.4.7 modern)
   │     ├─ ASCII bare 16     → split first15 + last1+pad (legacy IMEISV)
   │     └─ BCD 8 bytes       → swap nibbles per BCD; emit 16 digits
   │           (auto-detect: any byte > 0x39 ⇒ BCD path)
   ├─ on parse fail → WARN log + counter inc, ok=false
   ├─ on absence    → silent, ok=false
   │
   └─ sessCtx.IMEI = imei; sessCtx.SoftwareVersion = sv
       (existing dsl.SessionContext{...} literal at radius/server.go:481 EAP + :638 Direct)

[Diameter S6a NTR / ULR]  — parser-only, no live S6a listener in this story
   │
   ├─ DecodeAVPs(payload) → []*AVP
   ├─ FindAVPVendor(avps, AVPCodeTerminalInformation=350, VendorID3GPP)
   ├─ ExtractTerminalInformation(avps) → (imei, sv string, err error)
   │     │
   │     ├─ ti.GetGrouped()
   │     ├─ prefer FindAVP(grouped, 1402) + FindAVP(grouped, 1403)
   │     └─ fallback FindAVP(grouped, 1404)  → split 16 → first15 + last2
   ├─ on err → WARN + counter inc
   └─ caller writes sessCtx.IMEI / SoftwareVersion

[5G SBA Nudm/Namf JSON requests]
   │
   ├─ json.Decode(body, &req)  // req.PEI now populated
   ├─ ParsePEI(req.PEI) → (imei, sv string, ok bool)
   │     │
   │     ├─ "imei-<15>"     → imei=15 digits, sv=""
   │     ├─ "imeisv-<16>"   → imei=first15, sv=last2  (TS 23.003 §6.2A)
   │     ├─ "mac-..."       → ok=true (3GPP non-applicable, ignored, no error)
   │     ├─ "eui64-..."     → ok=true (3GPP non-applicable, ignored, no error)
   │     └─ malformed       → WARN + counter inc, ok=false
   └─ caller writes sessCtx.IMEI / SoftwareVersion
```

### API Specifications

No new external API endpoints. SessionContext is an **internal Go struct** mutation — the only contract is the in-process Go API:

```go
// Source: internal/policy/dsl/evaluator.go (MOD — adds 2 flat fields)
type SessionContext struct {
    SIMID           string            `json:"sim_id"`
    TenantID        string            `json:"tenant_id"`
    Operator        string            `json:"operator"`
    APN             string            `json:"apn"`
    RATType         string            `json:"rat_type"`
    Usage           int64             `json:"usage"`
    TimeOfDay       string            `json:"time_of_day"`
    DayOfWeek       string            `json:"day_of_week"`
    SessionCount    int               `json:"session_count"`
    BandwidthUsed   int64             `json:"bandwidth_used"`
    SessionDuration int64             `json:"session_duration"`
    Metadata        map[string]string `json:"metadata"`
    SimType         string            `json:"sim_type"`
    // Phase 11 STORY-093 — flat fields, zero-value safe.
    IMEI            string            `json:"imei,omitempty"`
    SoftwareVersion string            `json:"software_version,omitempty"`
}
```

> **DO NOT** introduce a nested `Device` struct (PROTOCOLS.md §"SessionContext Population" shows a forward-looking nested form for STORY-094+; this story is flat per AC-1 to avoid a 094 migration burden).

### Database Schema

**No DB changes in STORY-093.** `bound_imei`/`binding_mode`/pool tables land in STORY-094 / STORY-095. Source: ADR-004 §Architecture step 1 ("Capture, read-only, all three auth protocols") — capture writes only into in-memory SessionContext.

---

## Wire-format Specifications (EMBEDDED — Developer reference)

### A. RADIUS — `3GPP-IMEISV` VSA (Vendor-Id 10415, Vendor-Type 20)

**Source of truth precedence:** PROTOCOLS.md §"IMEI Capture (Cross-Protocol) — Phase 11" line 519-534 IS canonical. Story spec line 10 ("BCD-encoded") is true for the **legacy IE shape** but the project standard accepts **all three shapes** documented in PROTOCOLS.md:528 — auto-detect by inspection.

**RFC 2865 VSA framing** (already used by `extract3GPPRATType` at `radius/server.go:1128` — Pattern ref):

```
Octet 0..3   : Vendor-Id (4 bytes BE) = 10415
Octet 4      : Vendor-Type            = 20  (3GPP-IMEISV)
Octet 5      : Vendor-Length          = N+2  (covers itself + value)
Octet 6..    : Value                  (N bytes)
```

**Three accepted value shapes**:

| Shape | Length | Wire bytes example | Decode rule |
|-------|--------|---------------------|-------------|
| ASCII with comma (modern, TS 29.061 §16.4.7) | 18 | `"359211089765432,01"` | `strings.Split(",", 2)` → `imei=part[0]` (15 digits), `sv=part[1]` (2 digits) |
| ASCII bare 16 (legacy IMEISV) | 16 | `"3592110897654321"` | `imei=value[0:15]`, `sv=value[15:16]+"0"` (left-pad to 2 digits per TS 23.003 §6.2.2) |
| BCD-packed 8 bytes (rare CDMA/legacy NAS) | 8 | `0x53 0x29 0x12 0x80 0x79 0x56 0x43 0x12` | Per-octet **nibble swap** (low nibble first, high nibble second); concatenate 16 BCD digits; then split 15+1 like the bare-16 rule. BCD digit `0xF` is fill — strip from end before length check. |

**Auto-detect algorithm** (deterministic, no false-positive on ASCII):

```go
// pseudo-code in plan; do NOT copy verbatim — Developer writes idiomatic Go
//   if any byte v[i] > 0x39 (i.e. above ASCII '9') → BCD path
//   else if bytes.Contains(v, []byte{','))         → ASCII-with-comma path
//   else if len(v) == 16                            → ASCII bare path
//   else                                            → malformed → WARN + counter
```

**BCD nibble-swap rule** (TS 23.003 §6.2.2 / §6.2A — same as MSISDN/IMSI BCD):

```
byte 0x53  → low nibble 0x3, high nibble 0x5 → digits "35"
byte 0x29  → "92"
byte 0x12  → "21"
byte 0x80  → "08"
byte 0x79  → "97"
byte 0x56  → "65"
byte 0x43  → "34"
byte 0x12  → "21"   (last nibble may be 0xF = fill; strip)
result    → "3592110897654321"  → imei="359211089765432", sv="21"  (split 15+2 if 17 digits, else 15+1 padded)
```

**Validation** (on every shape after decode):

- `len(imei) == 15` && all digits `0..9` → else malformed
- `len(sv) == 2`    && all digits `0..9` → else `sv = ""` (acceptable — IMEI alone is valid)

**Failure mode (AC-5 / AC-6):** silent on absence; `WARN` log + `argus_imei_capture_parse_errors_total{protocol="radius"}` increment on malformed; `sessCtx.IMEI = ""` either way; auth proceeds.

### B. Diameter S6a — `Terminal-Information` AVP (350, grouped, M-bit, vendor=10415)

**Reference:** TS 29.272 §7.3.3.

**Outer AVP** (uses existing `NewAVPGrouped` / `GetGrouped` helpers in `internal/aaa/diameter/avp.go:144`):

```
AVP code = 350
Flags    = M (0x40) | V (0x80) = 0xC0
Vendor   = 10415
Data     = encoded sub-AVPs concatenated
```

**Sub-AVPs** (all UTF8String, vendor=10415, M-bit set):

| Sub-AVP | Code | Type | Length | Notes |
|---------|------|------|--------|-------|
| IMEI | 1402 | UTF8String | 15 chars | digits-only |
| Software-Version | 1403 | UTF8String | 2 chars | digits-only |
| IMEI-SV | 1404 | UTF8String | 16 chars | concatenated alt to 1402+1403 |

**Decode contract** (`ExtractTerminalInformation`):

1. `outer.GetGrouped()` → `inner []*AVP` (existing helper, returns err on malformed).
2. If `imei := FindAVP(inner, 1402); imei != nil && len(imei.GetString()) == 15` → use it.
   And if `sv := FindAVP(inner, 1403); sv != nil && len(sv.GetString()) == 2` → use it.
3. Else if `imeisv := FindAVP(inner, 1404); imeisv != nil && len(imeisv.GetString()) == 16` →
   `imei = imeisv.GetString()[0:15]; sv = imeisv.GetString()[15:16]+"0"` (pad to 2).
4. Else → `err = ErrIMEICaptureMalformed`.

**Pattern ref:** `internal/aaa/diameter/avp.go:321 ExtractSubscriptionID` — exact same shape (find outer, GetGrouped, find inner-by-code, return tuple, swallow malformed inner).

**S6a application/listener is OUT OF SCOPE for STORY-093.** This story ships:
- AVP code constants (`AVPCodeTerminalInformation = 350`, `AVPCodeIMEI = 1402`, `AVPCodeSoftwareVersion = 1403`, `AVPCodeIMEISV = 1404`) in `avp.go`.
- `ExtractTerminalInformation(avps []*AVP) (imei, sv string, err error)` in a new `imei.go`.
- Golden-byte fixture tests via `NewAVPGrouped` + `Encode` + `DecodeAVP` round-trip.

A live `Notify-Request` / `Update-Location-Request` listener would require Application-Id 16777251 registration in CER and a full S6a state machine — that lands when (or if) STORY-094+ requires it. Per ADR-004 §Out-of-Scope and DEV-409, S13 EIR is also explicitly out — keep this story surgical.

### C. 5G SBA — `PEI` JSON field

**Reference:** TS 23.003 §6.2A (PEI URI form), TS 29.503 (Nudm), TS 29.518 (Namf).

**Wire format** — string field embedded in request body JSON:

```json
{
  "supiOrSuci": "imsi-286010123456789",
  "servingNetworkName": "5G:mnc001.mcc286.3gppnetwork.org",
  "pei": "imei-359211089765432"
}
```

**Tagged URI prefixes**:

| Prefix | Length total | Decode | Note |
|--------|--------------|--------|------|
| `imei-` | 5 + 15 = 20 chars | `imei = pei[5:]`, `sv = ""` | 4G-style identity |
| `imeisv-` | 7 + 16 = 23 chars | `imei = pei[7:22]` (15 digits), `sv = pei[22:23]+"0"` | concatenated, last digit zero-padded to 2 per TS 23.003 |
| `mac-` | 4 + 12 hex | ignore (not 3GPP) — `ok=true`, both empty | non-3GPP access |
| `eui64-` | 6 + 16 hex | ignore (not 3GPP) — `ok=true`, both empty | non-3GPP access |
| any other / empty / malformed | — | `ok=false`, WARN + counter | |

**Validation** identical to RADIUS: 15 digits, all `0..9`. SV optional.

**Existing call sites** (PAT-006 audit must touch these struct literals):

- `internal/aaa/sba/types.go:27 AuthenticationRequest` (POST `/nausf-auth/v1/ue-authentications` — body decoded at `ausf.go:53`).
- `internal/aaa/sba/types.go:96 Amf3GppAccessRegistration` (PUT `/nudm-uecm/v1/{supi}/registrations/...` — body decoded at `udm.go:142`).

Both gain a new field `PEI string \`json:"pei,omitempty"\``. The PEI parser is invoked **after** body decode but **before** any session creation or response write, populating SessionContext on the path. STORY-093 does not yet wire SessionContext-into-AAA-evaluator on the SBA path because no SBA policy enforcement exists today; the captured value is logged and made available via `session.Manager` metadata (or a new `SessionContext` adapter — Developer chooses) for STORY-094 enricher consumption.

---

## Acceptance Criteria (verbatim from story)

- AC-1 — `SessionContext` extended with `IMEI string` + `SoftwareVersion string`, exported, zero-value safe.
- AC-2 — RADIUS `3GPP-IMEISV` VSA parsed → SessionContext populated.
- AC-3 — Diameter S6a `Terminal-Information` AVP 350 parser handles 1402+1403 pair OR 1404 fallback.
- AC-4 — 5G SBA `pei` JSON field parsed (`imei-` / `imeisv-` / unknown-prefix-ignored).
- AC-5 — Capture is read-only and null-safe. Missing IMEI MUST NOT block auth.
- AC-6 — Malformed input → WARN log + counter, no panic, auth proceeds with empty IMEI.
- AC-7 — Golden-byte fixture tests: 3 RADIUS + 3 Diameter + 3 5G PEI cases (per protocol: valid / alt / malformed).
- AC-8 — STORY-015/019/020 test suites pass unchanged.
- AC-9 — Audit log shape unchanged; new fields are additive context.
- AC-10 — Capture overhead ≤200 µs p95 added to auth path on 1M-SIM bench.

---

## Constraints (verbatim from story spec)

- Capture is **read-only**. Missing/malformed IMEI MUST NOT block auth.
- No new audit action; downstream consumers (STORY-094 enricher) read the new `SessionContext` fields.
- Zero regression: STORY-015 / 019 / 020 test suites must remain green unchanged.
- Latency: ≤200 µs p95 added to auth path on 1M-SIM bench (record measurement in plan addendum).
- Effort = L → at least one task `Complexity: high`.

---

## Tasks

> Ordering: AC-1 SessionContext extension first (every other task depends on it). Then per-protocol parser+test bundles in parallel-safe waves. Final waves: SessionContext audit (PAT-006 guard), perf bench (AC-10), zero-regression sweep (AC-8).

### Task 1 — Extend `SessionContext` with IMEI + SoftwareVersion fields

- **Files:** Modify `internal/policy/dsl/evaluator.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/policy/dsl/evaluator.go:9-23` — append the two new flat fields to the existing struct, JSON tags `json:"imei,omitempty"` / `json:"software_version,omitempty"`.
- **Context refs:** "Architecture Context > API Specifications", "Acceptance Criteria > AC-1"
- **What:** Add `IMEI string` and `SoftwareVersion string` fields to `SessionContext`. NO nested struct — flat per AC-1. Both zero-value safe (empty string when unset). Update no other code in this task; downstream RADIUS/SBA tasks consume it.
- **Verify:**
  - `go build ./internal/policy/dsl/...` PASS.
  - `grep -n 'IMEI\s*string' internal/policy/dsl/evaluator.go` returns the new line.
  - `go test ./internal/policy/...` PASS unchanged (existing tests build literals — zero-value compat).

### Task 2 — RADIUS 3GPP-IMEISV VSA parser (auto-detect 3 shapes)

- **Files:** Create `internal/aaa/radius/imei.go`, Create `internal/aaa/radius/imei_test.go`
- **Depends on:** Task 1
- **Complexity:** **high**
- **Pattern ref:** Read `internal/aaa/radius/server.go:1123-1166` (`extract3GPPRATType`) — exact VSA-decode pattern: lookup Type 26, parse 4-byte vendor-id + 1-byte vendor-type + 1-byte vendor-len + value bytes; bounds-check at every step; return zero-value on any failure.
- **Context refs:** "Wire-format Specifications > A. RADIUS", "Bug Pattern Warnings", "Acceptance Criteria > AC-2, AC-5, AC-6, AC-7"
- **What:**
  - Implement `Extract3GPPIMEISV(pkt *radius.Packet) (imei, sv string, ok bool)` in `imei.go`. Walk all VSA Type 26 attributes (loop — `pkt.Lookup` returns first only; need `pkt.Attributes` slice walk like Vendor-Type discrimination), match vendor-id=10415 + vendor-type=20.
  - Auto-detect shape:
    - any `value[i] > 0x39` ⇒ BCD path → nibble-swap each byte → 16 BCD digits → strip trailing `0xF` fill → split 15+SV.
    - else if `bytes.IndexByte(value, ',') >= 0` ⇒ ASCII-with-comma → split.
    - else if `len(value) == 16` and all digits ⇒ ASCII bare → split 15+1 (pad SV).
    - else ⇒ malformed.
  - On malformed: log `WARN` with `protocol="radius"`, `correlation_id` (use existing `radius_session_id` field if available else `User-Name`), increment counter `argus_imei_capture_parse_errors_total{protocol="radius"}`. Return `ok=false`.
  - On absence (no matching VSA): silent, `ok=false`.
  - Validation: 15-digit IMEI all-digits; SV (when present) 2-digit all-digits.
- **Tests** (in `imei_test.go`, per AC-7 — exactly 3 cases minimum, prefer 5):
  - `TestExtract3GPPIMEISV_ASCIIComma` — packet with VSA `"359211089765432,01"` → imei="359211089765432", sv="01", ok=true.
  - `TestExtract3GPPIMEISV_ASCIIBare16` — packet with VSA `"3592110897654321"` → imei="359211089765432", sv="10" (pad), ok=true.
  - `TestExtract3GPPIMEISV_BCDLegacy` — packet with VSA bytes `[0x53 0x29 0x12 0x80 0x79 0x56 0x43 0x12]` → imei="359211089765432", sv="21", ok=true.
  - `TestExtract3GPPIMEISV_AbsentVSA` — packet without VSA → ok=false, no log, no panic.
  - `TestExtract3GPPIMEISV_MalformedShortValue` — packet with VSA value `"abc"` → ok=false, WARN log emitted (use a captured zerolog.TestWriter or an injectable logger), counter incremented.
- **Verify:**
  - `go test ./internal/aaa/radius/ -run TestExtract3GPPIMEISV -v` → all 5 PASS.
  - `go vet ./internal/aaa/radius/...` clean.
  - `grep -c 'extract3GPPIMEISV\|Extract3GPPIMEISV' internal/aaa/radius/server.go` ≥ 0 (call site wired in Task 5).

### Task 3 — Diameter S6a `Terminal-Information` parser + AVP code constants

- **Files:** Modify `internal/aaa/diameter/avp.go` (constants only), Create `internal/aaa/diameter/imei.go`, Create `internal/aaa/diameter/imei_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/avp.go:321 ExtractSubscriptionID` — exact shape for grouped-AVP extraction (FindAVP outer → GetGrouped → FindAVP inner-by-code → tuple return). Read `internal/aaa/diameter/diameter_test.go:90 TestAVPGroupedEncodeDecode` — exact pattern for golden-byte fixture (use `NewAVPGrouped` + `Encode` then feed to your parser; round-trip).
- **Context refs:** "Wire-format Specifications > B. Diameter S6a", "Acceptance Criteria > AC-3, AC-7"
- **What:**
  - In `avp.go`: append constants `AVPCodeTerminalInformation uint32 = 350`, `AVPCodeIMEI uint32 = 1402`, `AVPCodeSoftwareVersion uint32 = 1403`, `AVPCodeIMEISV uint32 = 1404`. Place inside the existing `const ( ... )` block near `AVPCodeRATType3GPP`.
  - In `imei.go`: implement `ExtractTerminalInformation(avps []*AVP) (imei, sv string, err error)` and a sentinel `ErrIMEICaptureMalformed = errors.New("imei capture: malformed Terminal-Information AVP")`.
    - Find outer 350 (vendor 10415) via `FindAVPVendor(avps, 350, VendorID3GPP)`. If nil → `return "", "", nil` (absence, not error).
    - `outer.GetGrouped()` → if err: return ErrIMEICaptureMalformed (counter++, WARN with `protocol="diameter_s6a"`).
    - Prefer 1402+1403 path: `if a := FindAVP(inner, 1402); a != nil && validate(a.GetString(), 15) { imei = ... }`; same for 1403.
    - Fallback: 1404 → 16 chars → split 15+pad-to-2.
    - Validation: digits-only check.
  - **Do not** add a S6a application listener, CER negotiation, or NTR/ULR command handlers. Parser-only.
- **Tests** (`imei_test.go`, per AC-7):
  - `TestExtractTerminalInformation_FullPair` — grouped AVP with 1402="359211089765432" + 1403="01" → imei, sv populated, err=nil.
  - `TestExtractTerminalInformation_IMEISVOnly` — grouped AVP with only 1404="3592110897654321" → imei="359211089765432", sv="10", err=nil.
  - `TestExtractTerminalInformation_MalformedGrouped` — outer 350 with garbage payload (non-AVP-encoded bytes) → err=ErrIMEICaptureMalformed, no panic.
  - `TestExtractTerminalInformation_AbsentAVP` — `avps` without 350 → "", "", nil (silent absence).
  - `TestExtractTerminalInformation_BadInnerLengths` — 1402="abc" (3 chars) → err.
- **Verify:**
  - `go test ./internal/aaa/diameter/ -run TestExtractTerminalInformation -v` → all 5 PASS.
  - `go test ./internal/aaa/diameter/...` full package green (no regression).

### Task 4 — 5G SBA PEI JSON field + parser

- **Files:** Modify `internal/aaa/sba/types.go`, Create `internal/aaa/sba/imei.go`, Create `internal/aaa/sba/imei_test.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/sba/types.go:27 AuthenticationRequest` and `:96 Amf3GppAccessRegistration` for struct shape. Read `internal/aaa/sba/server_test.go:33 TestAUSFAuthenticationInitiation` for SBA test pattern (httptest body POST + JSON decode).
- **Context refs:** "Wire-format Specifications > C. 5G SBA", "Bug Pattern Warnings > PAT-006", "Acceptance Criteria > AC-4, AC-7"
- **What:**
  - `types.go`: add `PEI string \`json:"pei,omitempty"\`` to **both** `AuthenticationRequest` (line 27) and `Amf3GppAccessRegistration` (line 96). Tag `omitempty` so absence yields the same wire form as today (AC-8 zero-regression).
  - `imei.go`: implement `ParsePEI(pei string) (imei, sv string, ok bool)`:
    - empty string → ok=false, no log.
    - `strings.HasPrefix(pei, "imei-")` && `len(pei) == 20` && all-digits suffix → imei = pei[5:], sv = "", ok=true.
    - `strings.HasPrefix(pei, "imeisv-")` && `len(pei) == 23` && all-digits suffix → imei = pei[7:22], sv = pei[22:23]+"0", ok=true. (Per TS 23.003 §6.2A, last SVN digit pads to 2; the project rule above is the canonical normalization.)
    - `strings.HasPrefix(pei, "mac-")` || `strings.HasPrefix(pei, "eui64-")` → ok=true with imei="", sv="" (silently ignored — not 3GPP, not malformed).
    - else → WARN log `protocol="5g_sba"`, counter++, ok=false.
- **Tests** (`imei_test.go`):
  - `TestParsePEI_IMEI15` — `"imei-359211089765432"` → imei="359211089765432", sv="", ok=true.
  - `TestParsePEI_IMEISV16` — `"imeisv-3592110897654321"` → imei="359211089765432", sv="10", ok=true.
  - `TestParsePEI_MAC_Ignored` — `"mac-aabbccddeeff"` → ok=true, imei="", sv="" (no error, no warn).
  - `TestParsePEI_EUI64_Ignored` — `"eui64-0123456789abcdef"` → same as MAC.
  - `TestParsePEI_Malformed_BadDigits` — `"imei-abc211089765432"` → ok=false, counter++, WARN.
  - `TestParsePEI_Empty` — `""` → ok=false, no log.
- **Verify:**
  - `go test ./internal/aaa/sba/ -run TestParsePEI -v` → all PASS.
  - `grep -n '"pei"' internal/aaa/sba/types.go` shows both struct additions.

### Task 5 — Wire RADIUS parser into Access-Request handlers (both EAP + Direct paths)

- **Files:** Modify `internal/aaa/radius/server.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/radius/server.go:478-491 (EAP)` and `:635-649 (Direct)` — both build `dsl.SessionContext{}` literals **before** `s.policyEnforcer.Evaluate(ctx, sim, sessCtx)`. The new fields plug in at the same call site.
- **Context refs:** "Architecture Context > Data Flow", "Acceptance Criteria > AC-2, AC-5"
- **What:**
  - At both literal-construction sites add (after RAT-Type extraction, before `Evaluate`):
    - `imei, sv, _ := Extract3GPPIMEISV(r.Packet)`
    - `sessCtx.IMEI = imei`
    - `sessCtx.SoftwareVersion = sv`
  - Do **not** branch on `ok` — empty strings are explicitly safe (AC-5). The third return is intentionally discarded; capture failure is logged inside the parser.
  - Do not change error paths, do not block auth on missing IMEI.
  - Optional latency-budget guard: avoid double-walk if SessionContext already populated (no — it's a single-shot per request).
- **Verify:**
  - `go build ./...` PASS.
  - `go test ./internal/aaa/radius/...` — STORY-015 suite + new IMEI test all green (AC-8).
  - `grep -n 'sessCtx.IMEI' internal/aaa/radius/server.go` shows BOTH wire sites (EAP + Direct).

### Task 6 — Wire 5G SBA PEI parser into AUSF + UDM handlers

- **Files:** Modify `internal/aaa/sba/ausf.go`, Modify `internal/aaa/sba/udm.go`
- **Depends on:** Task 1, Task 4
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/sba/ausf.go:53` (body decode) and `internal/aaa/sba/udm.go:142` (body decode). Insert PEI parse **after** decode validation but **before** session creation / response write.
- **Context refs:** "Architecture Context > Data Flow", "Wire-format Specifications > C. 5G SBA", "Acceptance Criteria > AC-4, AC-5"
- **What:**
  - `ausf.go HandleAuthentication`: after `if req.SUPIOrSUCI == ""` guard, call `imei, sv, _ := ParsePEI(req.PEI)`; attach to `authCtx` (extend `AuthContext` struct in types.go: `IMEI string; SoftwareVersion string`) AND log them in the Info line at line 125-130 (`Str("imei", imei).Str("imei_sv", sv)` — use empty string sentinel; AC-9 audit shape unchanged means the existing `auth_event_id` audit row is untouched, the **logger** line is enrichment).
  - `udm.go HandleRegistration`: same — parse `reg.PEI`, populate `session.Session` metadata field (extend `Session` only if not present; otherwise stash in a string map). Confirm with existing `session.Session` struct shape — if no IMEI field exists yet, use the existing metadata string field (do not add a SQL column — AC-9 / AC-8). Acceptable alternative: pass-through via a per-request context value retrieved by STORY-094 enricher; Developer chooses lower-touch route.
  - Same null-safe behaviour: empty imei is fine, no auth blocked.
- **Verify:**
  - `go build ./...` PASS.
  - `go test ./internal/aaa/sba/...` — existing STORY-020 suite green (AC-8).
  - Manual: extend one existing TestAUSFAuthenticationInitiation case body with `"pei":"imei-359211089765432"` and assert no behaviour change in the response (status, auth ctx URI).

### Task 7 — SessionContext construction-site audit + zero-value regression test

- **Files:** Create `internal/policy/dsl/evaluator_imei_test.go`
- **Depends on:** Task 1, Task 5, Task 6
- **Complexity:** **high**
- **Pattern ref:** Existing tests in `internal/policy/dsl/evaluator_test.go` (or nearest existing `*_test.go` in same package) for table-driven SessionContext construction.
- **Context refs:** "Bug Pattern Warnings > PAT-006", "Architecture Context > API Specifications", "Acceptance Criteria > AC-1, AC-5"
- **What:**
  - Run `grep -rn 'dsl.SessionContext{' internal/ cmd/` — capture every literal construction site (expected ≥ 2: radius/server.go:481, :638; possibly more in tests, diameter, sba). Document the list inline as a comment block at the top of the new test file.
  - Write `TestSessionContext_IMEIFields_ZeroValueSafe`:
    - construct `sessCtx := dsl.SessionContext{}` with NO IMEI/SV → assert `IMEI == ""` && `SoftwareVersion == ""`.
    - construct one with both populated → JSON marshal → assert keys `imei` and `software_version` present with correct values.
    - construct one with empty strings → JSON marshal → assert `omitempty` elides keys (PAT-006 wire-shape guard).
  - Write `TestSessionContext_IMEIFields_NotInLegacyConstructions` (smoke): runs the `grep` programmatically via `os/exec` of `git grep` (or skip on non-git env) — list every file matching `dsl.SessionContext{` and `t.Logf` so audit drift is visible in CI logs. Optional but recommended per PAT-006 prevention.
- **Verify:**
  - `go test ./internal/policy/dsl/ -run TestSessionContext_IMEIFields -v` → PASS.
  - `grep -rn 'dsl\.SessionContext{' internal/ cmd/ | wc -l` ≥ 2 (radius EAP + Direct minimum). All sites manually checked: zero-value-safe (no compile error from missing field).

### Task 8 — Per-protocol microbenchmarks (AC-10 latency budget evidence)

- **Files:** Create `internal/aaa/radius/imei_bench_test.go`, Modify `internal/aaa/diameter/imei_test.go` (add `Benchmark*`), Modify `internal/aaa/sba/imei_test.go` (add `Benchmark*`)
- **Depends on:** Task 2, Task 3, Task 4
- **Complexity:** medium
- **Pattern ref:** Standard `go test -bench` shape; see any existing `Benchmark*` in `internal/aaa/` (if none, the structure is `func BenchmarkX(b *testing.B) { for i := 0; i < b.N; i++ { ... } }`).
- **Context refs:** "Acceptance Criteria > AC-10", "Constraints (verbatim)"
- **What:**
  - `BenchmarkExtract3GPPIMEISV_RADIUS` — pre-built `radius.Packet` carrying ASCII-comma VSA; loop over `Extract3GPPIMEISV`; report ns/op.
  - `BenchmarkExtractTerminalInformation_S6a` — pre-built grouped AVP byte slice; loop over `DecodeAVPs` + `ExtractTerminalInformation`.
  - `BenchmarkParsePEI_5G` — loop over `ParsePEI("imeisv-3592110897654321")`.
  - **Write a perf addendum subsection** in `STORY-093-plan.md` — see `## AC-10 Perf Addendum` below — and record the measured ns/op values from `go test -run=^$ -bench=. -benchmem ./internal/aaa/...`. Target: each parser < 5 µs single-call → safety margin 40× under the 200 µs p95 auth-path budget.
- **Verify:**
  - `go test -run=^$ -bench=BenchmarkExtract3GPPIMEISV_RADIUS -benchtime=1s ./internal/aaa/radius/` reports a number; ns/op < 5000 (5 µs).
  - Same for Diameter and SBA bench targets.
  - Update the `## AC-10 Perf Addendum` table at end of plan with actual numbers (Developer fills in during T8).

### Task 9 — Zero-regression sweep (AC-8) + integration smoke

- **Files:** No new files. Run the project test matrix.
- **Depends on:** Task 5, Task 6, Task 7, Task 8
- **Complexity:** low
- **Pattern ref:** —
- **Context refs:** "Constraints (verbatim)", "Acceptance Criteria > AC-8, AC-9"
- **What:**
  - Run `make test` (or `go test ./...`) and confirm full matrix green.
  - Specifically grep that no STORY-015 / STORY-019 / STORY-020 test was modified — `git diff --stat` should show only:
    - `internal/policy/dsl/evaluator.go` (Task 1)
    - `internal/aaa/radius/imei*.go`, `internal/aaa/radius/server.go` (Tasks 2, 5, 8)
    - `internal/aaa/diameter/avp.go`, `internal/aaa/diameter/imei*.go` (Tasks 3, 8)
    - `internal/aaa/sba/types.go`, `internal/aaa/sba/imei*.go`, `internal/aaa/sba/ausf.go`, `internal/aaa/sba/udm.go` (Tasks 4, 6, 8)
    - `internal/policy/dsl/evaluator_imei_test.go` (Task 7)
  - **Audit log shape verification**: `grep -rn 'audit.Log\|auditService.Log\|auditStore.Insert' internal/aaa/` → no new call sites added by this story. Capture is read-only (AC-9).
- **Verify:**
  - `make test` PASS, full matrix.
  - `go vet ./...` clean.
  - `git diff --stat` matches the expected file list above.
  - Per-spec AC-9 check: `git diff -- internal/audit/` is empty.

---

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|----------------|-------------|
| AC-1 SessionContext flat fields | Task 1 | Task 7 (`TestSessionContext_IMEIFields_ZeroValueSafe`) |
| AC-2 RADIUS VSA parse | Task 2 + Task 5 | Task 2 (5 unit tests) + Task 9 (matrix) |
| AC-3 Diameter S6a parser | Task 3 | Task 3 (5 unit tests) |
| AC-4 5G SBA PEI parse | Task 4 + Task 6 | Task 4 (6 unit tests) + Task 6 (smoke) |
| AC-5 read-only / null-safe | Task 2, 3, 4 (parser semantics) + Task 5, 6 (wire policy) | Task 2 `_AbsentVSA` + Task 3 `_AbsentAVP` + Task 4 `_Empty` + Task 9 (no audit shape change) |
| AC-6 malformed → WARN + counter, no panic | Task 2, 3, 4 | Task 2 `_Malformed*` + Task 3 `_Malformed*` + Task 4 `_Malformed*` |
| AC-7 golden-byte fixtures (3 per protocol) | Task 2, 3, 4 | 5 RADIUS + 5 Diameter + 6 5G PEI tests |
| AC-8 STORY-015/019/020 unchanged | Task 9 | `make test` green; `git diff --stat` audit |
| AC-9 audit shape unchanged | Task 5, 6 (no audit call sites added) | Task 9 `grep -rn 'audit.Log' internal/aaa/` shows zero new lines |
| AC-10 latency ≤ 200 µs p95 | Task 8 | `BenchmarkExtract*` ns/op + plan addendum table |

---

## Story-Specific Compliance Rules

- **API:** No external API changes. SessionContext is internal Go contract — JSON tags `omitempty` per AC-9 (zero-regression on the wire).
- **DB:** None. STORY-093 ships zero migrations; binding columns land in STORY-094.
- **Protocol correctness:** RADIUS auto-detect MUST handle all 3 shapes from PROTOCOLS.md:528 (ASCII-comma, ASCII-bare-16, BCD). Skipping any shape is a CRITICAL gap.
- **ADR-004 compliance:**
  - Capture is read-only (ADR-004 §Architecture step 1).
  - No EIR / S13 / N17 scaffolding (ADR-004 §Out-of-Scope, DEV-409).
  - SessionContext write happens BEFORE policy DSL evaluation (binding pre-check is in STORY-094, not here — but the field must be available).
- **Logging:** WARN level on parse error with `protocol={radius|diameter_s6a|5g_sba}` and a correlation ID (User-Name for RADIUS, Session-Id for Diameter, SUPI for SBA).
- **Metrics:** `argus_imei_capture_parse_errors_total{protocol="..."}` — name fixed by PROTOCOLS.md:532. Even though story AC-6 does not name the counter, PROTOCOLS.md does — implement to project canon.
- **DSL grammar:** `device.imei`, `device.tac`, `device.imeisv`, `device.software_version`, `device.binding_status`, `device.imei_in_pool(...)` are reserved in DSL_GRAMMAR.md but **DSL evaluator integration is STORY-094 scope**. STORY-093 only populates SessionContext.

---

## Bug Pattern Warnings

> Read `docs/brainstorming/bug-patterns.md` ## Patterns. Patterns affecting AAA / RADIUS / Diameter / SBA layers:

- **PAT-006 (and recurrences in FIX-201, FIX-215, FIX-251):** Shared payload struct field silently omitted at construction sites. Applies here in **two ways**: (a) `dsl.SessionContext{}` literals at `radius/server.go:481` (EAP) and `:638` (Direct) MUST be updated; Task 7 audits all sites with `grep -rn 'dsl\.SessionContext{' internal/ cmd/`. (b) `AuthenticationRequest` / `Amf3GppAccessRegistration` struct literal callers in `sba/server_test.go` and any test fixtures must remain zero-value safe — `pei` field is `omitempty` so absence is wire-identical. **Verification rule (PAT-006 prevention):** add `TestSessionContext_IMEIFields_ZeroValueSafe` (Task 7) and add a JSON shape test that the new `pei` field elides when empty (round-trip test in Task 4 fixtures). Compiler will NOT catch missing field assignments in struct literals.

- **PAT-009 (FIX-204):** Nullable column → COALESCE before scan. **Not applicable** here — no DB columns, no SELECT projections. Note for future STORY-094 when `bound_imei` lands.

- **PAT-017 (FIX-210):** Config parameter threaded to store but not propagated through REST handler. **Not directly applicable** (no config flag in this story) but the **pattern shape** — wire a thing into multiple call paths — applies: RADIUS has TWO sites (EAP + Direct) and SBA has TWO sites (AUSF + UDM). Task 5 + Task 6 explicitly verify both wires per protocol; missing one is the PAT-017 failure mode.

- **PAT-022 (FIX-234):** CHECK-constraint enum drift. **Not applicable** — no DB schema in this story.

- **PAT-023 (FIX-252):** `schema_migrations` can lie. **Not applicable** — no migrations.

- **PAT-026 (FIX-245):** Orphan publisher after deletion. **Inverse risk here:** STORY-093 introduces a parser without a consumer for the SBA path (no SBA policy enforcement exists today). Task 6's note explicitly accepts this — the parser populates context that STORY-094 will read. Document this as a forward dependency, NOT an orphan. If STORY-094 slips significantly, file a tech-debt item.

- **PAT-024 (FIX-246):** Fake-store hides CHECK violations. **Not applicable** — no INSERT in this story.

- **PAT-025 (FIX-235):** Semantic confusion between identifiers of the same Go type. **Mildly applicable** — `IMEI`, `IMEISV`, `Software-Version`, `IMSI` are all `string` and can be cross-assigned silently. Mitigation: explicit length validation in every parser (15 vs 16 vs 2), and unique field names (`IMEI` and `SoftwareVersion` — never just `ID` or `Identifier`). Future refactor to `type IMEI string; type IMSI string` is **out of scope** for this story (would touch every consumer).

---

## Tech Debt (from ROUTEMAP)

> Reviewed `docs/ROUTEMAP.md` ## Tech Debt for items targeting STORY-093.

**No tech debt items for this story.** D-174 (`SMSR_CALLBACK_SECRET` length validation) is Phase 11 hardening but targets `cmd/argus/main.go`, not the AAA capture path — out of STORY-093 scope.

---

## Mock Retirement

**No mock retirement for this story.** STORY-093 has no frontend surface and no mocked API endpoints.

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| RADIUS BCD vs ASCII shape ambiguity → wrong shape picked | Medium | Wrong IMEI captured → silent wrong audit context downstream | Auto-detect by inspection (any byte > 0x39 ⇒ BCD); golden fixtures cover all 3 shapes (AC-7); per-shape unit test per advisor guidance. |
| Test rig PEI examples differ from production carrier PEI shapes | Low | False positives in dev, real traffic differs | Use the canonical TS 23.003 §6.2A examples in fixtures; cross-check against `pei = "imei-..."` examples in TS 29.503 / 29.518. |
| `SessionContext` literal in a test file forgotten | Medium | Test compiles, runtime sets stale empty IMEI | Task 7 PAT-006 audit grep + zero-value test. |
| 1M-SIM bench unavailable in CI | High | AC-10 unverifiable in unit run | Microbenchmarks per parser (`Benchmark*`) target ≤5 µs; aggregate budget = 5 + 5 + 5 = 15 µs single-call worst case ≪ 200 µs p95 budget. Captured in `## AC-10 Perf Addendum`. |
| New `pei` JSON field breaks SBA simulator / external SBA peers expecting strict body schema | Low | 5G simulator might reject unknown fields | Tag `omitempty` AND `json.Decoder` in `ausf.go:53` does NOT call `DisallowUnknownFields()` — confirm by reading `udm.go:142` similarly. Both decoders are tolerant by default. |
| Diameter S6a parser used by STORY-094 in a subtly different shape than expected | Low | STORY-094 has to refactor | Parser signature `(avps []*AVP) (imei, sv string, err error)` is the smallest possible — leaves dispatch (which command this came from) to caller. ADR-004 reviewed → consistent with `Notify-Request` and `Update-Location-Request` capture points. |

---

## AC-10 Perf Addendum (Developer fills in during Task 8)

| Benchmark | ns/op (single call) | Budget | Margin |
|-----------|---------------------|--------|--------|
| `BenchmarkExtract3GPPIMEISV_RADIUS` | 42 ns/op | 5000 ns (5 µs) | 119× |
| `BenchmarkExtractTerminalInformation_S6a` | 104 ns/op | 5000 ns (5 µs) | 48× |
| `BenchmarkParsePEI_5G` | 23 ns/op | 5000 ns (5 µs) | 217× |
| **Aggregate worst-case (sum)** | 169 ns | **200 000 ns (200 µs p95 budget)** | 1183× |

Bench command: `go test -run=^$ -bench=. -benchmem -benchtime=1s ./internal/aaa/radius/ ./internal/aaa/diameter/ ./internal/aaa/sba/ | tee bench-story-093.txt`

If any single parser exceeds 5 µs single-call: optimize before close (likely culprit: BCD nibble-swap implementation — prefer simple `for` over reflection or fmt.Sprintf). If aggregate sum ≥ 50 µs: escalate to gate review — still under 200 µs but the budget is the auth path, not the parser sum.

---

## Plan Self-Validation (Quality Gate)

- [x] Min plan lines for L (≥100): exceeded.
- [x] Min task count for L (≥5): 9 tasks.
- [x] Required sections present: `## Goal`, `## Architecture Context`, `## Tasks`, `## Acceptance Criteria Mapping` — all present.
- [x] At least 1 task `Complexity: high`: Task 2 (RADIUS auto-detect parser), Task 7 (cross-protocol SessionContext audit + zero-value regression test). Both flagged `**high**`.
- [x] Every task has `Pattern ref` pointing to a real existing file:
  - T1 → `evaluator.go:9-23` (verified line range).
  - T2 → `radius/server.go:1123-1166 extract3GPPRATType` (verified).
  - T3 → `diameter/avp.go:321 ExtractSubscriptionID` + `diameter_test.go:90 TestAVPGroupedEncodeDecode` (verified).
  - T4 → `sba/types.go:27,96` + `sba/server_test.go:33` (verified).
  - T5 → `radius/server.go:478-491,635-649` (verified construction sites).
  - T6 → `sba/ausf.go:53` + `sba/udm.go:142` (verified body-decode lines).
  - T7 → `policy/dsl/evaluator_test.go` (existing package test pattern).
  - T8 → standard `Benchmark*` shape (no specific file ref required for shape tests).
  - T9 → matrix sweep (no file ref).
- [x] Every task has `Context refs` pointing to actual sections of this plan: cross-checked against the section headers above.
- [x] API specs embedded: SessionContext Go struct shown verbatim.
- [x] DB schema embedded: explicitly noted "no DB changes".
- [x] Architecture compliance: all tasks scoped to the correct layer (`internal/aaa/<protocol>/` for parsers; `internal/policy/dsl/` for SessionContext extension only). No cross-layer imports planned (DSL evaluator does not import aaa packages — capture writes the field, evaluator reads it via STORY-094 wiring).
- [x] Self-Containment: all 3 wire formats fully embedded with worked-example bytes; no "see ARCHITECTURE.md" references in task bodies.
- [x] PAT-006 audit explicitly tasked (Task 7).
- [x] AC-10 verification path explicitly named (Task 8 + addendum).
- [x] S6a scope clarification: parser-only, no listener / CER / Application-Id wiring (advisor flag #2).
- [x] PEI field added to BOTH `AuthenticationRequest` and `Amf3GppAccessRegistration` (advisor flag #3).
- [x] Effort=L → high-complexity tasks present.

---

> Plan size: large but self-contained. Tasks 2, 3, 4 are independent (parallel-safe wave 2 after Task 1). Tasks 5 and 6 are independent (parallel-safe wave 3). Task 7 depends on 5+6+1. Task 8 depends on 2+3+4. Task 9 depends on all preceding. Suggested dispatch wave plan:
>
> - **Wave 1:** Task 1 (SessionContext extension — gate for everything else).
> - **Wave 2 (parallel):** Task 2 (RADIUS), Task 3 (Diameter), Task 4 (SBA types+parser).
> - **Wave 3 (parallel):** Task 5 (RADIUS wire), Task 6 (SBA wire).
> - **Wave 4 (parallel):** Task 7 (PAT-006 audit), Task 8 (perf benches).
> - **Wave 5:** Task 9 (zero-regression sweep + AC-9 audit-shape verification).
