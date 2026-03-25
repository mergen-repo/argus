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
condition       = simple_condition | compound_condition ;
simple_condition = identifier operator value_list ;
compound_condition = condition ("AND" | "OR") condition ;
                   | "NOT" condition
                   | "(" condition ")" ;

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
| `roaming` | boolean | `=` | `true`, `false` | Match roaming status |
| `metadata.*` | string | `=`, `!=`, `IN` | Any string | Match by SIM metadata field |

Example:
```
MATCH {
    apn IN ("iot.fleet", "iot.meter")
    rat_type IN (nb_iot, lte_m)
    roaming = false
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
| `roaming` | boolean | `=` | `true`, `false` | Is session roaming |
| `session_count` | integer | `>`, `>=`, `<`, `<=`, `=` | Number | Active session count for this SIM |
| `bandwidth_used` | data rate | `>`, `>=`, `<`, `<=` | Number + rate unit (kbps/mbps/gbps) | Current bandwidth utilization |
| `session_duration` | duration | `>`, `>=`, `<`, `<=` | Number + time unit (s/min/h/d) | Current session duration |
| `day_of_week` | enum | `IN`, `=` | `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun` | Current day of week |

### Compound Conditions

WHEN conditions support AND, OR, NOT, and parenthesized grouping:

```
WHEN usage > 500MB AND rat_type = lte {
    ACTION throttle(256kbps)
}

WHEN (time_of_day IN (00:00-06:00) OR day_of_week IN (sat, sun)) AND usage < 1GB {
    bandwidth_down = 4mbps  # weekend/off-peak bonus
}

WHEN NOT roaming = true {
    bandwidth_down = 2mbps
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

        # Roaming restrictions
        WHEN roaming = true {
            bandwidth_down = 256kbps
            bandwidth_up = 128kbps
            ACTION notify(roaming_detected, 0%)
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
