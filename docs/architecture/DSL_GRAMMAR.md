# Policy DSL Grammar — Argus

> The Argus Policy DSL is a domain-specific language for defining QoS, FUP, and charging rules.
> Parser implementation: `internal/policy/dsl/` (recursive descent parser in Go).
> Public API: `dsl.Parse()`, `dsl.CompileSource()`, `dsl.EvaluateCompiled()`, `dsl.Validate()`, `dsl.DSLVersion()`.

## Formal EBNF Grammar

```ebnf
(* Top-level *)
policy          = "POLICY" string "{" match_block rules_block charging_block? "}" ;

(* Match block — determines which SIMs/sessions this policy applies to *)
match_block     = "MATCH" "{" match_clause+ "}" ;
match_clause    = identifier operator value_list ;

(* Rules block — defines actions and conditions *)
rules_block     = "RULES" "{" statement* "}" ;
statement       = assignment | when_block ;
when_block      = "WHEN" condition "{" when_body+ "}" ;
when_body       = assignment | action ;
assignment      = identifier "=" value ;
action          = "ACTION" function_call ;
function_call   = identifier "(" argument_list? ")" ;
argument_list   = argument ( "," argument )* ;
argument        = value | identifier "=" value ;

(* Charging block — defines billing and cost model *)
charging_block  = "CHARGING" "{" charging_stmt* "}" ;
charging_stmt   = assignment | multiplier_block ;
multiplier_block = "rat_type_multiplier" "{" multiplier_entry+ "}" ;
multiplier_entry = identifier "=" number ;

(* Conditions *)
condition       = simple_condition | compound_condition | device_predicate | sim_binding_predicate | pool_membership_predicate ;
simple_condition = identifier operator value_list ;
compound_condition = condition ("AND" | "OR") condition ;
                   | "NOT" condition
                   | "(" condition ")" ;

(* Device & SIM-binding predicates — Phase 11, see ADR-004 *)
device_predicate = device_field operator value ;
device_field    = "device.imei"
                | "device.tac"
                | "device.imeisv"
                | "device.software_version"
                | "device.binding_status" ;
sim_binding_predicate = sim_binding_field operator value ;
sim_binding_field = "sim.binding_mode"
                  | "sim.bound_imei"
                  | "sim.binding_verified_at" ;
binding_status_value = "verified" | "pending" | "mismatch" | "unbound" | "disabled" ;
binding_mode_value   = "strict" | "allowlist" | "first-use" | "tac-lock" | "grace-period" | "soft" | "null" ;
pool_membership_predicate = "device.imei_in_pool" "(" pool_kind ")" ;
pool_kind        = '"whitelist"' | '"greylist"' | '"blacklist"' ;
tac_function     = "tac" "(" device_field ")" ;

(* Operators *)
operator        = "IN" | ">" | ">=" | "<" | "<=" | "=" | "!=" | "BETWEEN" ;

(* Values *)
value_list      = "(" value ( "," value )* ")" | value ;
value           = string | number_with_unit | number | identifier | time_range | percentage ;
number_with_unit = number unit ;
percentage      = number "%" ;
time_range      = time "-" time ;
time            = digit digit ":" digit digit ;

(* Units *)
unit            = "bps" | "kbps" | "mbps" | "gbps"
                | "B" | "KB" | "MB" | "GB" | "TB"
                | "s" | "ms" | "min" | "h" | "d" ;

(* Primitives *)
string          = '"' { any_char - '"' | '\"' } '"' ;
number          = digit+ ( "." digit+ )? ;
identifier      = letter_or_underscore { letter_or_underscore | digit }* ;
comment         = "#" { any_char - newline } newline ;

(* Character classes *)
digit               = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" | "8" | "9" ;
letter_or_underscore = "a"-"z" | "A"-"Z" | "_" ;
any_char            = ? any Unicode character ? ;
newline             = ? line feed or carriage return + line feed ? ;
```

## Lexical Rules

### Case Sensitivity
- **Keywords**: UPPERCASE only. `POLICY`, `MATCH`, `RULES`, `WHEN`, `ACTION`, `CHARGING`, `IN`, `BETWEEN`, `AND`, `OR`, `NOT`.
- **Identifiers**: Case-insensitive. `bandwidth_down`, `Bandwidth_Down`, and `BANDWIDTH_DOWN` are equivalent. Stored and compared as lowercase internally.
- **String values**: Case-sensitive. `"iot.fleet"` and `"IoT.Fleet"` are different values.
- **Unit suffixes**: Case-insensitive. `mbps`, `Mbps`, `MBPS` are equivalent.

### Whitespace and Comments
- Whitespace (spaces, tabs, newlines) is insignificant except inside strings.
- Comments start with `#` and extend to end of line.
- Comments are allowed anywhere whitespace is allowed.

### Reserved Keywords
```
POLICY  MATCH  RULES  WHEN  ACTION  CHARGING  IN  BETWEEN  AND  OR  NOT
```

These cannot be used as identifiers.

## Match Conditions

The MATCH block determines which SIMs/sessions a policy applies to. All clauses are AND-combined (a SIM must satisfy all clauses to match).

| Condition | Type | Operators | Values | Description |
|-----------|------|-----------|--------|-------------|
| `apn` | string | `IN`, `=` | APN names | Match by APN name |
| `operator` | string | `IN`, `=` | Operator names | Match by operator |
| `rat_type` | enum | `IN`, `=` | `nb_iot`, `lte_m`, `lte`, `nr_5g` | Match by RAT type |
| `sim_type` | enum | `IN`, `=` | `physical`, `esim` | Match by SIM type |
| `metadata.*` | string | `=`, `!=`, `IN` | Any string | Match by SIM metadata field |

> Note: the `roaming` boolean match field was removed by FIX-238 (2026-04-30)
> alongside the roaming agreements feature. Existing policies referencing it
> are auto-archived at boot by `internal/job/roaming_keyword_archiver.go`.

Example:
```
MATCH {
    apn IN ("iot.fleet", "iot.meter")
    rat_type IN (nb_iot, lte_m)
    metadata.fleet_id = "fleet-alpha"
}
```

## Rule Conditions (WHEN blocks)

WHEN blocks inside the RULES section define conditional behavior based on runtime state.

| Condition | Type | Operators | Values | Description |
|-----------|------|-----------|--------|-------------|
| `usage` | data size | `>`, `>=`, `<`, `<=`, `=`, `BETWEEN` | Number + data unit (KB/MB/GB/TB) | Total data usage in current billing period |
| `time_of_day` | time range | `IN` | Time range (HH:MM-HH:MM) | Current time in tenant timezone |
| `rat_type` | enum | `IN`, `=` | `nb_iot`, `lte_m`, `lte`, `nr_5g` | Current session RAT type |
| `apn` | string | `IN`, `=` | APN names | Current session APN |
| `operator` | string | `IN`, `=` | Operator names | Current session operator |
| `session_count` | integer | `>`, `>=`, `<`, `<=`, `=` | Number | Active session count for this SIM |
| `bandwidth_used` | data rate | `>`, `>=`, `<`, `<=` | Number + rate unit (kbps/mbps/gbps) | Current bandwidth utilization |
| `session_duration` | duration | `>`, `>=`, `<`, `<=` | Number + time unit (s/min/h/d) | Current session duration |
| `day_of_week` | enum | `IN`, `=` | `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun` | Current day of week |
| `device.imei` | string | `=`, `!=`, `IN` | 15-digit IMEI | Device IMEI from current auth (RADIUS 3GPP-IMEISV / Diameter S6a Terminal-Information / 5G PEI). Empty/null when capture failed. |
| `device.tac` | string | `=`, `!=`, `IN` | 8-digit TAC | Type Allocation Code — first 8 digits of IMEI; identifies device model. Computed by Argus, not transmitted on the wire. |
| `device.imeisv` | string | `=`, `!=` | 16-digit IMEISV | Concatenated IMEI + Software-Version (16 digits). Phase 11. |
| `device.software_version` | string | `=`, `!=`, `IN` | 2-digit SV | Device firmware revision sub-field (last 2 digits of IMEISV). Phase 11. |
| `device.binding_status` | enum | `=`, `IN` | `verified`, `pending`, `mismatch`, `unbound`, `disabled` | Result of the binding pre-check — `disabled` when SIM has `binding_mode=NULL`; otherwise reflects the pre-check verdict for the current auth. Phase 11. |
| `device.imei_in_pool('<kind>')` | predicate | (call) | `whitelist`, `greylist`, `blacklist` | Membership check against `imei_whitelist`/`imei_greylist`/`imei_blacklist` (TBL-56/57/58). Matching applies both exact full-IMEI and TAC-range logic: a pool entry with `kind=tac_range` matches any 15-digit IMEI whose first 8 digits equal the stored TAC. Result is cached per evaluation pass (keyed `<pool>:<imei>`) so repeated calls within one policy evaluation never issue duplicate DB queries. Returns `false` (not error) when `SessionContext.IMEI` is empty. **Functional as of STORY-095** (was `return false` placeholder in STORY-094). Phase 11, STORY-094/095. |
| `sim.binding_mode` | enum | `=`, `IN`, `!=` | `strict`, `allowlist`, `first-use`, `tac-lock`, `grace-period`, `soft`, `null` | Per-SIM binding posture (`null` = binding disabled). Phase 11, STORY-094. |
| `sim.bound_imei` | string | `=`, `!=` | 15-digit IMEI or empty | The IMEI this SIM is currently locked to (relevant for `strict`, `first-use`, `tac-lock`, `grace-period`). |
| `sim.binding_verified_at` | timestamp | `>`, `>=`, `<`, `<=` | ISO-8601 / relative duration | Last successful verification timestamp; useful for staleness rules in `grace-period` mode. |
| `tac(<device-field>)` | function | (returns string) | `device.imei` only | Extracts TAC (first 8 digits) from an IMEI field, e.g., `tac(device.imei) = "35982110"`. Phase 11. |

### Compound Conditions

WHEN conditions support AND, OR, NOT, and parenthesized grouping:

```
WHEN usage > 500MB AND rat_type = lte {
    ACTION throttle(256kbps)
}

WHEN (time_of_day IN (00:00-06:00) OR day_of_week IN (sat, sun)) AND usage < 1GB {
    bandwidth_down = 4mbps  # weekend/off-peak bonus
}
```

## Assignable Properties

These properties can be set in the RULES block (either at top level or inside WHEN blocks):

| Property | Type | Unit | Description |
|----------|------|------|-------------|
| `bandwidth_down` | data rate | bps/kbps/mbps/gbps | Download bandwidth limit |
| `bandwidth_up` | data rate | bps/kbps/mbps/gbps | Upload bandwidth limit |
| `session_timeout` | duration | s/min/h/d | Maximum session duration |
| `idle_timeout` | duration | s/min/h/d | Idle timeout (no traffic) |
| `max_sessions` | integer | (none) | Maximum concurrent sessions |
| `qos_class` | integer | (none) | QoS class identifier (QCI for LTE, 5QI for 5G) |
| `priority` | integer | (none) | Session priority (1=highest) |

## Available Actions

Actions are functions called with `ACTION` keyword inside WHEN blocks:

| Action | Parameters | Description |
|--------|-----------|-------------|
| `notify(event_type, threshold)` | `event_type`: string (quota_warning, quota_exceeded, etc.); `threshold`: percentage | Send notification via configured channels |
| `throttle(rate)` | `rate`: data rate with unit | Reduce bandwidth to specified rate. Sends CoA to active session. |
| `disconnect()` | (none) | Disconnect active session. Sends DM (Disconnect-Message). |
| `log(message)` | `message`: string | Write entry to audit/analytics log |
| `block()` | (none) | Block new session establishment (reject auth) |
| `suspend()` | (none) | Trigger SIM state transition to SUSPENDED |
| `tag(key, value)` | `key`: string; `value`: string | Add/update SIM metadata tag |

## Charging Block

The optional CHARGING block defines the billing model:

| Property | Type | Values | Description |
|----------|------|--------|-------------|
| `model` | enum | `prepaid`, `postpaid`, `hybrid` | Billing model |
| `rate_per_mb` | decimal | Any positive number | Cost per megabyte (in tenant currency) |
| `rate_per_session` | decimal | Any positive number | Fixed cost per session |
| `billing_cycle` | enum | `hourly`, `daily`, `monthly` | Billing aggregation period |
| `quota` | data size | Number + data unit | Data quota per billing cycle |
| `overage_action` | enum | `throttle`, `block`, `charge` | What to do when quota exceeded |
| `overage_rate_per_mb` | decimal | Any positive number | Overage cost per MB (when overage_action = charge) |

### RAT Type Multiplier

The `rat_type_multiplier` sub-block applies a cost multiplier based on the radio access technology:

```
CHARGING {
    model = postpaid
    rate_per_mb = 0.01
    billing_cycle = monthly
    quota = 1GB
    overage_action = throttle

    rat_type_multiplier {
        nb_iot = 0.5     # NB-IoT is cheapest
        lte_m  = 1.0     # LTE-M is baseline
        lte    = 2.0     # 4G LTE costs 2x
        nr_5g  = 3.0     # 5G NR costs 3x
    }
}
```

## Complete Example

```
# IoT Fleet Standard Policy - covers NB-IoT and LTE-M devices
POLICY "iot-fleet-standard" {
    MATCH {
        apn IN ("iot.fleet", "iot.meter")
        rat_type IN (nb_iot, lte_m)
    }

    RULES {
        # Default bandwidth limits
        bandwidth_down = 1mbps
        bandwidth_up = 256kbps
        session_timeout = 24h
        idle_timeout = 1h
        max_sessions = 1

        # Quota warning at 80%
        WHEN usage > 800MB {
            ACTION notify(quota_warning, 80%)
        }

        # Hard throttle at 1GB
        WHEN usage > 1GB {
            bandwidth_down = 64kbps
            bandwidth_up = 32kbps
            ACTION notify(quota_exceeded, 100%)
            ACTION log("FUP throttle applied")
        }

        # Off-peak bonus
        WHEN time_of_day IN (00:00-06:00) {
            bandwidth_down = 2mbps
            bandwidth_up = 512kbps
        }

        # Anti-abuse: too many sessions
        WHEN session_count > 3 {
            ACTION notify(anomaly_detected, 0%)
            ACTION log("Multiple concurrent sessions detected")
        }
    }

    CHARGING {
        model = postpaid
        rate_per_mb = 0.01
        billing_cycle = monthly
        quota = 1GB
        overage_action = throttle

        rat_type_multiplier {
            nb_iot = 0.5
            lte_m  = 1.0
            lte    = 2.0
            nr_5g  = 3.0
        }
    }
}
```

## Device Binding Examples (Phase 11)

> See ADR-004. The `device.*` and `sim.*` namespaces run AFTER the AAA capture pipeline has populated SessionContext but the **binding pre-check** itself is enforced in the AAA engine BEFORE policy DSL evaluation when `sim.binding_mode IS NOT NULL`. DSL rules below operate on the post-pre-check `binding_status` and are intended for **post-policy** enrichment (notify on soft mismatch, log + tag on TAC change, etc.) — they cannot weaken a hard reject already issued by the pre-check in `strict`/`allowlist`/`first-use`/`tac-lock` modes.
>
> **Protocol scope (VAL-055 — STORY-096):** `device.binding_status` is populated for ALL three protocols (RADIUS, Diameter S6a, 5G SBA) by the binding enforcer. However, the DSL policy evaluator is invoked on the **RADIUS leg only** — Diameter S6a applies policy via Gx/Gy PCC rules and 5G SBA applies policy via PCF. DSL rules matching on `device.binding_status` therefore only fire on RADIUS authentications; Diameter and SBA paths receive the enforcer verdict directly without DSL post-processing.

```
# Soft-mode tenant: never reject, but flag every device change as a high-severity audit event.
POLICY "m2m-fleet-soft-binding" {
    MATCH {
        apn IN ("m2m.industrial")
        metadata.binding_profile = "soft"
    }
    RULES {
        WHEN device.binding_status = "mismatch" {
            ACTION notify(device_mismatch, 0%)
            ACTION log("IMEI mismatch under soft binding")
            ACTION tag("last_mismatch_at", now())
        }
    }
}

# TAC-lock tenant: alert when a device of a different model is observed (different TAC).
POLICY "m2m-meters-taclock" {
    MATCH {
        apn IN ("m2m.water", "m2m.electric")
    }
    RULES {
        WHEN sim.binding_mode = "tac-lock" AND tac(device.imei) != tac(sim.bound_imei) {
            ACTION notify(device_mismatch, 0%)
            ACTION log("TAC drift — different device model on locked SIM")
        }
    }
}

# Defensive layer on top of pool enforcement: belt-and-suspenders block of greylisted devices.
POLICY "iot-fleet-greylist-quarantine" {
    MATCH {
        apn IN ("iot.fleet")
    }
    RULES {
        WHEN device.imei_in_pool("greylist") = true {
            bandwidth_down = 64kbps
            bandwidth_up  = 32kbps
            ACTION notify(device_greylisted, 0%)
            ACTION log("Greylist quarantine throttle applied")
        }
        WHEN device.imei_in_pool("blacklist") = true {
            ACTION block()
            ACTION notify(device_blacklisted, 100%)
        }
    }
}
```

## Compiled Representation

The DSL parser compiles source code into a JSON rule tree stored in `policy_versions.compiled_rules` (JSONB column). This compiled form is cached in Redis for fast evaluation during AAA requests.

```json
{
  "name": "iot-fleet-standard",
  "match": {
    "conditions": [
      { "field": "apn", "op": "in", "values": ["iot.fleet", "iot.meter"] },
      { "field": "rat_type", "op": "in", "values": ["nb_iot", "lte_m"] }
    ]
  },
  "rules": {
    "defaults": {
      "bandwidth_down": 1048576,
      "bandwidth_up": 262144,
      "session_timeout": 86400,
      "idle_timeout": 3600,
      "max_sessions": 1
    },
    "when_blocks": [
      {
        "condition": { "field": "usage", "op": "gt", "value": 838860800 },
        "actions": [
          { "type": "notify", "params": { "event_type": "quota_warning", "threshold": 80 } }
        ]
      },
      {
        "condition": { "field": "usage", "op": "gt", "value": 1073741824 },
        "assignments": { "bandwidth_down": 65536, "bandwidth_up": 32768 },
        "actions": [
          { "type": "notify", "params": { "event_type": "quota_exceeded", "threshold": 100 } },
          { "type": "log", "params": { "message": "FUP throttle applied" } }
        ]
      }
    ]
  },
  "charging": {
    "model": "postpaid",
    "rate_per_mb": 0.01,
    "billing_cycle": "monthly",
    "quota": 1073741824,
    "overage_action": "throttle",
    "rat_type_multiplier": {
      "nb_iot": 0.5,
      "lte_m": 1.0,
      "lte": 2.0,
      "nr_5g": 3.0
    }
  }
}
```

Note: All data sizes are stored in bytes, all rates in bits-per-second, all durations in seconds in the compiled form.

## Parser Error Reporting

The parser provides precise error locations for syntax and semantic errors:

```json
{
  "errors": [
    {
      "line": 7,
      "column": 12,
      "severity": "error",
      "code": "DSL_SYNTAX_ERROR",
      "message": "Expected '{' after MATCH keyword",
      "snippet": "  MATCH\n       ^"
    },
    {
      "line": 15,
      "column": 5,
      "severity": "warning",
      "code": "DSL_UNREACHABLE",
      "message": "WHEN block is unreachable: condition 'usage > 500MB' is always true when 'usage > 1GB' matches (line 12)"
    }
  ]
}
```

## Validation Rules

1. **MATCH block required**: Every policy must have at least one match clause.
2. **RULES block required**: Every policy must have a rules block (may be empty).
3. **CHARGING block optional**: If omitted, no cost calculation is performed.
4. **No duplicate assignments**: Within the same scope (top-level or WHEN block), assigning the same property twice is an error.
5. **Unit consistency**: Bandwidth properties must use rate units (kbps/mbps/gbps). Usage conditions must use data units (KB/MB/GB/TB). Time properties must use duration units (s/min/h/d).
6. **Time range format**: Must be HH:MM-HH:MM in 24-hour format. Range wrapping past midnight is allowed (e.g., `22:00-06:00`).
7. **RAT type values**: Must be one of `nb_iot`, `lte_m`, `lte`, `nr_5g`.
8. **Action parameter types**: Each action has a fixed parameter signature; mismatched types are errors.

## Predicate Execution (SQL Backend)

`dsl.ToSQLPredicate` is the canonical builder that translates a compiled MATCH block into a parameterized SQL WHERE fragment. It is the **only** permitted way to produce SQL from DSL — callers must never construct predicates manually.

### Whitelisted Fields

| Field | SQL Fragment | Notes |
|-------|-------------|-------|
| `apn` | `s.apn_id = (SELECT id FROM apns WHERE tenant_id = $T AND name = $N)` | Tenant-scoped sub-select |
| `operator` | `s.operator_id = (SELECT id FROM operators WHERE code = $N)` | Global — no tenant_id on operators |
| `imsi_prefix` | `s.imsi LIKE $N` | Value appended with `%` before binding |
| `rat_type` | `s.rat_type = $N` | Direct string column |
| `sim_type` | `s.sim_type = $N` | Direct string column |
| `sim.binding_mode` | `s.binding_mode = $N` | Phase 11 — used by SIM list cohort filters; NULL never matches via `=` (use `IS NULL` form when adding to MATCH). |
| `sim.bound_imei` | `s.bound_imei = $N` | Phase 11 — exact-match only; cohort/forensics use case. |

> **Runtime-only fields**: `device.imei`, `device.tac`, `device.imeisv`, `device.software_version`, `device.binding_status`, and `device.imei_in_pool(...)` are session-context predicates evaluated by `dsl.EvaluateCompiled` at AAA time only — they are NOT permitted in MATCH→SQL and the whitelist explicitly rejects them. The `tac()` function is also runtime-only.

### Rules

- **Unknown field** → `err = fmt.Errorf("dsl: field %q not allowed in MATCH→SQL", field)`. The API handler returns HTTP 422 INVALID_DSL. The field name is NEVER concatenated into SQL.
- **All values** bound via `$N` pgx placeholders — never via `fmt.Sprintf` string interpolation. This makes SQL injection structurally impossible through this path.
- **Empty MATCH** (`match == nil` or zero conditions) → returns `("TRUE", nil, nextArgIdx, nil)`. AND-joined with the base query predicate; effectively no SIM narrowing.
- **Multiple conditions** → joined with ` AND `.
- **Fail-closed**: if `dsl.CompileSource` returns error-severity diagnostics, `compiledMatchFromVersion` returns a non-nil error rather than falling back to `TRUE` (which would migrate ALL active tenant SIMs).

### Usage Pattern

```go
predicate, args, _, err := dsl.ToSQLPredicate(&compiled.Match, tenantArgIdx, startArgIdx)
// predicate is safe to embed in WHERE clause; args bound by pgx
count, err := simStore.CountWithPredicate(ctx, tenantID, predicate, args)
```
