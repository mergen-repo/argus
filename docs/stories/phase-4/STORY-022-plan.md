# Implementation Plan: STORY-022 - Policy DSL Parser & Evaluator

## Goal
Implement a complete Policy DSL parser and evaluator in `internal/policy/dsl/` that lexes, parses, compiles, and evaluates human-readable policy rules for QoS, FUP, and charging, producing compiled JSON rule trees stored in TBL-14 `policy_versions.compiled_rules`.

## Architecture Context

### Components Involved
- **SVC-05 Policy Engine** (`internal/policy/`): Core policy engine package. This story creates the `internal/policy/dsl/` sub-package containing lexer, parser, AST, compiler, and evaluator.
- **TBL-14 `policy_versions`**: Stores DSL source (`dsl_content TEXT`) and compiled output (`compiled_rules JSONB`). Already exists in migrations.
- **Redis Cache**: Evaluation results cached by `policy_version_id + session_context_hash`, with configurable TTL.

### Project Module
- Module path: `github.com/btopcu/argus`
- Go version: 1.25.6
- Key dependencies: `github.com/google/uuid`, `github.com/rs/zerolog`, `github.com/redis/go-redis/v9`

### Data Flow
```
DSL Source Text
    → Lexer (tokenize)
    → Parser (build AST)
    → Compiler (AST → JSON rule tree)
    → Store in policy_versions.compiled_rules (JSONB)
    → Evaluator loads compiled rules
    → Evaluate(SessionContext, CompiledRules) → PolicyResult
    → Cache result in Redis
```

### Database Schema (ACTUAL — from migrations/20260320000002_core_schema.up.sql)

```sql
-- TBL-14: policy_versions
CREATE TABLE IF NOT EXISTS policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES policies(id),
    version INTEGER NOT NULL,
    dsl_content TEXT NOT NULL,
    compiled_rules JSONB NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    affected_sim_count INTEGER,
    dry_run_result JSONB,
    activated_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);
```

Key columns for this story:
- `dsl_content`: Raw DSL source text
- `compiled_rules`: Compiled JSON rule tree (output of compiler)

### DSL Grammar (Complete EBNF)

```ebnf
(* Top-level *)
policy          = "POLICY" string "{" match_block rules_block charging_block? "}" ;

(* Match block *)
match_block     = "MATCH" "{" match_clause+ "}" ;
match_clause    = identifier operator value_list ;

(* Rules block *)
rules_block     = "RULES" "{" statement* "}" ;
statement       = assignment | when_block ;
when_block      = "WHEN" condition "{" when_body+ "}" ;
when_body       = assignment | action ;
assignment      = identifier "=" value ;
action          = "ACTION" function_call ;
function_call   = identifier "(" argument_list? ")" ;
argument_list   = argument ( "," argument )* ;
argument        = value | identifier "=" value ;

(* Charging block *)
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
```

### Lexical Rules
- **Keywords** (UPPERCASE only): `POLICY`, `MATCH`, `RULES`, `WHEN`, `ACTION`, `CHARGING`, `IN`, `BETWEEN`, `AND`, `OR`, `NOT`
- **Identifiers**: Case-insensitive, stored as lowercase. Cannot be reserved keywords.
- **String values**: Case-sensitive, enclosed in double quotes.
- **Unit suffixes**: Case-insensitive.
- **Whitespace**: Insignificant except inside strings.
- **Comments**: `#` to end of line.

### Compiled JSON Representation

All data sizes stored in bytes, rates in bits-per-second, durations in seconds:

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
      }
    ]
  },
  "charging": {
    "model": "postpaid",
    "rate_per_mb": 0.01,
    "billing_cycle": "monthly",
    "quota": 1073741824,
    "overage_action": "throttle",
    "rat_type_multiplier": { "nb_iot": 0.5, "lte_m": 1.0, "lte": 2.0, "nr_5g": 3.0 }
  }
}
```

### Match Conditions Reference

| Condition | Type | Operators | Values |
|-----------|------|-----------|--------|
| `apn` | string | `IN`, `=` | APN names |
| `operator` | string | `IN`, `=` | Operator names |
| `rat_type` | enum | `IN`, `=` | `nb_iot`, `lte_m`, `lte`, `nr_5g` |
| `sim_type` | enum | `IN`, `=` | `physical`, `esim` |
| `roaming` | boolean | `=` | `true`, `false` |
| `metadata.*` | string | `=`, `!=`, `IN` | Any string |

### Rule Conditions Reference (WHEN blocks)

| Condition | Type | Operators | Values |
|-----------|------|-----------|--------|
| `usage` | data size | `>`, `>=`, `<`, `<=`, `=`, `BETWEEN` | Number + data unit |
| `time_of_day` | time range | `IN` | Time range HH:MM-HH:MM |
| `rat_type` | enum | `IN`, `=` | RAT type enums |
| `apn` | string | `IN`, `=` | APN names |
| `operator` | string | `IN`, `=` | Operator names |
| `roaming` | boolean | `=` | `true`, `false` |
| `session_count` | integer | `>`, `>=`, `<`, `<=`, `=` | Number |
| `bandwidth_used` | data rate | `>`, `>=`, `<`, `<=` | Number + rate unit |
| `session_duration` | duration | `>`, `>=`, `<`, `<=` | Number + time unit |
| `day_of_week` | enum | `IN`, `=` | `mon`-`sun` |

### Assignable Properties

| Property | Type | Unit |
|----------|------|------|
| `bandwidth_down` | data rate | bps/kbps/mbps/gbps |
| `bandwidth_up` | data rate | bps/kbps/mbps/gbps |
| `session_timeout` | duration | s/min/h/d |
| `idle_timeout` | duration | s/min/h/d |
| `max_sessions` | integer | (none) |
| `qos_class` | integer | (none) |
| `priority` | integer | (none) |

### Available Actions

| Action | Parameters |
|--------|-----------|
| `notify(event_type, threshold)` | string, percentage |
| `throttle(rate)` | data rate with unit |
| `disconnect()` | none |
| `log(message)` | string |
| `block()` | none |
| `suspend()` | none |
| `tag(key, value)` | string, string |

### Policy Evaluation Algorithm (from ALGORITHMS.md Section 6)

```
FUNCTION evaluate_rules(compiled_rules, session_context) → EvaluationResult

1. Start with default assignments:
     result = compiled_rules.rules.defaults

2. Evaluate WHEN blocks in order (top to bottom):
     FOR each when_block in compiled_rules.rules.when_blocks:
       IF evaluate_condition(when_block.condition, session_context):
         result.merge(when_block.assignments)
         result.actions.append(when_block.actions)

3. Within same scope level: last matching WHEN block wins for conflicting assignments.
   Actions from ALL matching WHEN blocks are collected (not overridden).

4. RETURN result
```

### Validation Rules
1. MATCH block required: Every policy must have at least one match clause.
2. RULES block required (may be empty).
3. CHARGING block optional.
4. No duplicate assignments within the same scope.
5. Unit consistency: bandwidth → rate units, usage → data units, time → duration units.
6. Time range format: HH:MM-HH:MM, midnight wrapping allowed.
7. RAT type values: `nb_iot`, `lte_m`, `lte`, `nr_5g` only.
8. Action parameter types must match fixed signatures.

### Error Reporting Format

```json
{
  "errors": [
    {
      "line": 7, "column": 12, "severity": "error",
      "code": "DSL_SYNTAX_ERROR",
      "message": "Expected '{' after MATCH keyword",
      "snippet": "  MATCH\n       ^"
    }
  ]
}
```

### Unit Conversion Tables

**Data sizes → bytes:**
- B = 1, KB = 1024, MB = 1048576, GB = 1073741824, TB = 1099511627776

**Data rates → bits per second:**
- bps = 1, kbps = 1000, mbps = 1000000, gbps = 1000000000

**Duration → seconds:**
- ms = 0.001, s = 1, min = 60, h = 3600, d = 86400

## Prerequisites
- [x] STORY-001 completed (project scaffold, internal/ directory structure)
- [x] TBL-14 `policy_versions` table exists (created in core schema migration)
- [x] Go module initialized with required dependencies

## Tasks

### Task 1: Token Types and Lexer
- **Files:** Create `internal/policy/dsl/token.go`, Create `internal/policy/dsl/lexer.go`
- **Depends on:** — (none)
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/eap/eap.go` — follow same Go constant/type pattern; Read `internal/aaa/diameter/message.go` — follow same package structure with const blocks and type definitions
- **Context refs:** Architecture Context > DSL Grammar, Architecture Context > Lexical Rules, Architecture Context > Unit Conversion Tables
- **What:**
  - Define `TokenType` as an `int` type with all token constants: `TokenPolicy`, `TokenMatch`, `TokenRules`, `TokenWhen`, `TokenAction`, `TokenCharging`, `TokenIn`, `TokenBetween`, `TokenAnd`, `TokenOr`, `TokenNot`, `TokenIdent`, `TokenString`, `TokenNumber`, `TokenLParen`, `TokenRParen`, `TokenLBrace`, `TokenRBrace`, `TokenComma`, `TokenEq`, `TokenNeq`, `TokenGt`, `TokenGte`, `TokenLt`, `TokenLte`, `TokenPercent`, `TokenColon`, `TokenDash`, `TokenComment`, `TokenEOF`, `TokenIllegal`
  - Define `Token` struct: `Type TokenType`, `Literal string`, `Line int`, `Column int`
  - Define `String()` method on `TokenType` for debug printing
  - Implement `Lexer` struct with fields: `input string`, `pos int`, `readPos int`, `ch byte`, `line int`, `column int`
  - `NewLexer(input string) *Lexer`
  - `NextToken() Token` — main tokenization loop handling: whitespace skip, comment skip (`#` to EOL), strings (double-quoted with `\"` escape), numbers (integer and float), identifiers and keywords (case-insensitive keyword lookup), operators (`=`, `!=`, `>`, `>=`, `<`, `<=`), structural tokens (`{`, `}`, `(`, `)`, `,`, `%`, `:`, `-`)
  - `Tokenize() []Token` — convenience method returning all tokens
  - Keywords are UPPERCASE only: `POLICY`, `MATCH`, `RULES`, `WHEN`, `ACTION`, `CHARGING`, `IN`, `BETWEEN`, `AND`, `OR`, `NOT`
  - Identifiers stored lowercase internally
  - Track line/column for error reporting
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 2: AST Node Types
- **Files:** Create `internal/policy/dsl/ast.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/avp.go` — follow same Go struct + method pattern
- **Context refs:** Architecture Context > DSL Grammar, Architecture Context > Match Conditions Reference, Architecture Context > Rule Conditions Reference, Architecture Context > Assignable Properties, Architecture Context > Available Actions
- **What:**
  - Define AST node types as Go interfaces and structs:
  - `Node` interface with `nodeType() string` method
  - `Policy` struct: `Name string`, `Match *MatchBlock`, `Rules *RulesBlock`, `Charging *ChargingBlock`
  - `MatchBlock` struct: `Clauses []*MatchClause`
  - `MatchClause` struct: `Field string`, `Operator string`, `Values []Value`
  - `RulesBlock` struct: `Statements []Statement`
  - `Statement` interface (implemented by `Assignment` and `WhenBlock`)
  - `Assignment` struct: `Property string`, `Value Value`, `Line int`
  - `WhenBlock` struct: `Condition Condition`, `Body []WhenBody`, `Line int`
  - `WhenBody` interface (implemented by `Assignment` and `ActionCall`)
  - `ActionCall` struct: `Name string`, `Args []Argument`, `Line int`
  - `Argument` struct: `Name string` (optional, for named args), `Value Value`
  - `Condition` interface (implemented by `SimpleCondition`, `CompoundCondition`, `NotCondition`, `GroupCondition`)
  - `SimpleCondition` struct: `Field string`, `Operator string`, `Values []Value`, `Line int`
  - `CompoundCondition` struct: `Left Condition`, `Op string` (AND/OR), `Right Condition`
  - `NotCondition` struct: `Inner Condition`
  - `GroupCondition` struct: `Inner Condition`
  - `ChargingBlock` struct: `Statements []*Assignment`, `RATMultiplier map[string]float64`
  - `Value` interface with `valueType() string` method
  - `StringValue` struct: `Val string`
  - `NumberValue` struct: `Val float64`
  - `NumberWithUnit` struct: `Val float64`, `Unit string`
  - `IdentValue` struct: `Val string`
  - `TimeRange` struct: `Start string`, `End string` (HH:MM format)
  - `PercentValue` struct: `Val float64`
  - `BoolValue` struct: `Val bool`
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 3: Recursive Descent Parser
- **Files:** Create `internal/policy/dsl/parser.go`
- **Depends on:** Task 1, Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/eap/eap.go` — follow same method-based processing pattern with error handling
- **Context refs:** Architecture Context > DSL Grammar, Architecture Context > Lexical Rules, Architecture Context > Validation Rules, Architecture Context > Error Reporting Format, Architecture Context > Match Conditions Reference, Architecture Context > Rule Conditions Reference
- **What:**
  - Implement recursive descent parser
  - `DSLError` struct: `Line int`, `Column int`, `Severity string` (error/warning), `Code string`, `Message string`, `Snippet string`
  - `Parser` struct: `tokens []Token`, `pos int`, `errors []DSLError`
  - `NewParser(tokens []Token) *Parser`
  - `Parse() (*Policy, []DSLError)` — entry point, returns AST + all errors (not just first)
  - `parsePolicy()` — expects POLICY keyword, string name, `{`, match_block, rules_block, optional charging_block, `}`
  - `parseMatchBlock()` — expects MATCH, `{`, one or more match clauses, `}`
  - `parseMatchClause()` — identifier, operator, value or value_list
  - `parseRulesBlock()` — expects RULES, `{`, zero or more statements, `}`
  - `parseStatement()` — dispatches to `parseAssignment()` or `parseWhenBlock()` based on next token
  - `parseWhenBlock()` — expects WHEN, condition, `{`, one or more when_body entries, `}`
  - `parseCondition()` — handles simple conditions, compound (AND/OR), NOT, and parenthesized grouping with proper operator precedence (NOT > AND > OR)
  - `parseChargingBlock()` — expects CHARGING, `{`, assignments and optional `rat_type_multiplier` sub-block, `}`
  - `parseValue()` — dispatches to string, number (with optional unit/percent), identifier, time range
  - `parseValueList()` — handles `(val1, val2, ...)` syntax
  - Error recovery: on error, skip tokens until next `}` or keyword, continue parsing to collect multiple errors
  - Validation during parse: duplicate assignment detection per scope, unit consistency checks, RAT type value validation, action parameter signature validation
  - Generate snippets for error messages showing the problematic line with a `^` pointer
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 4: Compiler (AST to JSON Rule Tree)
- **Files:** Create `internal/policy/dsl/compiler.go`
- **Depends on:** Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/circuit_breaker.go` — follow same clean struct + method pattern
- **Context refs:** Architecture Context > Compiled JSON Representation, Architecture Context > Unit Conversion Tables, Architecture Context > Assignable Properties, Architecture Context > Available Actions
- **What:**
  - `CompiledPolicy` struct matching the JSON rule tree format:
    - `Name string`, `Version int`
    - `Match CompiledMatch` — `Conditions []CompiledCondition`
    - `Rules CompiledRules` — `Defaults map[string]interface{}`, `WhenBlocks []CompiledWhenBlock`
    - `Charging *CompiledCharging` (nullable — CHARGING block is optional)
  - `CompiledCondition` struct: `Field string`, `Op string`, `Value interface{}` (single value), `Values []interface{}` (for IN operator)
  - `CompiledWhenBlock` struct: `Condition CompiledCondition` (can be nested for compound), `Assignments map[string]interface{}`, `Actions []CompiledAction`
  - `CompiledAction` struct: `Type string`, `Params map[string]interface{}`
  - `CompiledCharging` struct: `Model string`, `RatePerMB float64`, `RatePerSession float64`, `BillingCycle string`, `Quota int64`, `OverageAction string`, `OverageRatePerMB float64`, `RATMultiplier map[string]float64`
  - `Compiler` struct (stateless)
  - `Compile(ast *Policy) (*CompiledPolicy, error)` — main entry point
  - Unit normalization during compilation: data sizes → bytes, rates → bps, durations → seconds
  - Convert operator names: `>` → `gt`, `>=` → `gte`, `<` → `lt`, `<=` → `lte`, `=` → `eq`, `!=` → `neq`, `IN` → `in`, `BETWEEN` → `between`
  - Handle compound conditions by nesting: `{op: "and", left: {...}, right: {...}}`
  - `ToJSON() ([]byte, error)` — serialize compiled policy to JSON for storage in `compiled_rules` column
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 5: Evaluator (Session Context → Policy Result)
- **Files:** Create `internal/policy/dsl/evaluator.go`
- **Depends on:** Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/operator/router.go` — follow same interface-based design pattern
- **Context refs:** Architecture Context > Policy Evaluation Algorithm, Architecture Context > Rule Conditions Reference, Architecture Context > Assignable Properties, Architecture Context > Available Actions, Architecture Context > Compiled JSON Representation
- **What:**
  - `SessionContext` struct: `SIMID string`, `TenantID string`, `Operator string`, `APN string`, `RATType string`, `Roaming bool`, `Usage int64` (bytes), `TimeOfDay string` (HH:MM), `DayOfWeek string`, `SessionCount int`, `BandwidthUsed int64` (bps), `SessionDuration int64` (seconds), `Metadata map[string]string`, `SimType string`
  - `PolicyResult` struct: `Allow bool`, `QoSAttributes map[string]interface{}` (bandwidth_down, bandwidth_up, session_timeout, idle_timeout, max_sessions, qos_class, priority), `ChargingParams *ChargingResult`, `Actions []ActionResult`, `MatchedRules int`
  - `ChargingResult` struct: `Model string`, `RatePerMB float64`, `BillingCycle string`, `Quota int64`, `OverageAction string`, `OverageRatePerMB float64`, `RATMultiplier float64`
  - `ActionResult` struct: `Type string`, `Params map[string]interface{}`
  - `Evaluator` struct (stateless)
  - `NewEvaluator() *Evaluator`
  - `Evaluate(ctx SessionContext, compiled *CompiledPolicy) (*PolicyResult, error)` — main entry
  - `matchesPolicy(ctx SessionContext, compiled *CompiledPolicy) bool` — check MATCH block conditions against session context
  - `evaluateRules(ctx SessionContext, rules *CompiledRules) *PolicyResult` — apply defaults, then iterate WHEN blocks top-to-bottom; last match wins for assignments, all actions collected
  - `evaluateCondition(ctx SessionContext, cond CompiledCondition) bool` — evaluate a single condition against session context, handling all operators and field types
  - `evaluateCharging(ctx SessionContext, charging *CompiledCharging) *ChargingResult` — compute charging params with RAT type multiplier
  - Comparison helpers for each field type (data size, rate, duration, time range, enum, boolean)
  - Time range evaluation: handle midnight wrapping (e.g., 22:00-06:00)
  - Default result (allow with no QoS overrides) when no WHEN blocks match
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 6: Public API — Parse, Compile, Evaluate convenience functions
- **Files:** Create `internal/policy/dsl/dsl.go`
- **Depends on:** Task 1, Task 2, Task 3, Task 4, Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/audit/audit.go` — follow same public API facade pattern
- **Context refs:** Architecture Context > Data Flow, Architecture Context > Error Reporting Format
- **What:**
  - Public package-level functions for end-to-end usage:
  - `Parse(source string) (*Policy, []DSLError)` — lex + parse in one call
  - `Compile(source string) (*CompiledPolicy, []DSLError, error)` — lex + parse + compile
  - `CompileAST(ast *Policy) (*CompiledPolicy, error)` — compile from existing AST
  - `Evaluate(source string, ctx SessionContext) (*PolicyResult, error)` — full pipeline: lex + parse + compile + evaluate
  - `EvaluateCompiled(compiled *CompiledPolicy, ctx SessionContext) (*PolicyResult, error)` — evaluate pre-compiled rules
  - `Validate(source string) []DSLError` — lex + parse, return only errors (for editor/linter use)
  - `DSLVersion() string` — returns current DSL grammar version (e.g., "1.0")
  - Error aggregation: combine parse errors with any compile errors
- **Verify:** `go build ./internal/policy/dsl/...`

### Task 7: Lexer and Parser Tests
- **Files:** Create `internal/policy/dsl/lexer_test.go`, Create `internal/policy/dsl/parser_test.go`
- **Depends on:** Task 1, Task 2, Task 3
- **Complexity:** high
- **Pattern ref:** Read `internal/auth/jwt_test.go` — follow same table-driven test pattern
- **Context refs:** Architecture Context > DSL Grammar, Architecture Context > Lexical Rules, Architecture Context > Validation Rules, Architecture Context > Error Reporting Format, Acceptance Criteria Mapping
- **What:**
  - **Lexer tests** (`lexer_test.go`):
    - Tokenize keywords: verify all 11 keywords produce correct token types
    - Tokenize identifiers: verify case-insensitive storage (lowercase)
    - Tokenize strings: double-quoted, with escaped quotes
    - Tokenize numbers: integers and floats
    - Tokenize operators: `=`, `!=`, `>`, `>=`, `<`, `<=`
    - Tokenize structural: `{`, `}`, `(`, `)`, `,`, `%`, `:`, `-`
    - Skip whitespace and comments
    - Track line/column numbers correctly
    - Tokenize complete example policy from DSL_GRAMMAR.md
  - **Parser tests** (`parser_test.go`):
    - Parse valid POLICY with MATCH + RULES + CHARGING → valid AST (AC)
    - Parse policy with syntax error → error with line:column (AC)
    - Parse MATCH block with IN operator and value list
    - Parse WHEN block with simple condition
    - Parse compound conditions: AND, OR, NOT, parenthesized
    - Parse ACTION calls with various parameter signatures
    - Parse CHARGING block with rat_type_multiplier
    - Parse empty RULES block → valid AST
    - Error recovery: multiple errors reported (not just first)
    - Duplicate assignment detection
    - Invalid RAT type value → error
    - Unit consistency validation
  - Use `testing` + table-driven tests with descriptive subtests
- **Verify:** `go test ./internal/policy/dsl/... -run 'TestLexer|TestParser' -v`

### Task 8: Compiler and Evaluator Tests
- **Files:** Create `internal/policy/dsl/compiler_test.go`, Create `internal/policy/dsl/evaluator_test.go`
- **Depends on:** Task 4, Task 5, Task 6
- **Complexity:** high
- **Pattern ref:** Read `internal/auth/auth_test.go` — follow same test structure with setup helpers
- **Context refs:** Architecture Context > Compiled JSON Representation, Architecture Context > Unit Conversion Tables, Architecture Context > Policy Evaluation Algorithm, Architecture Context > Assignable Properties, Architecture Context > Available Actions, Acceptance Criteria Mapping
- **What:**
  - **Compiler tests** (`compiler_test.go`):
    - Compile AST → JSON rule tree matches expected structure (AC)
    - Unit normalization: 1mbps → 1000000, 1GB → 1073741824, 24h → 86400
    - Operator normalization: `>` → `gt`, `IN` → `in`, etc.
    - Compound condition compilation (nested AND/OR/NOT)
    - CHARGING block compilation with RAT multiplier
    - Optional CHARGING block (nil when omitted)
    - Compiled output serializable to valid JSON
  - **Evaluator tests** (`evaluator_test.go`):
    - SIM matching WHEN condition → correct ACTION returned (AC)
    - SIM not matching any WHEN → default action returned (AC)
    - First matching WHEN wins (rule ordering) — actually last match wins for assignments, all actions collected (AC)
    - CHARGING block returns correct rate_per_mb and model (AC)
    - Complex condition: `usage > 500MB AND rat_type = "4G" AND time_of_day BETWEEN "00:00" AND "06:00"` evaluates correctly (AC)
    - Empty RULES block → default allow with no QoS overrides (AC)
    - Roundtrip: DSL source → compile → evaluate → same result as direct AST evaluation (AC)
    - MATCH block filtering: policy matches/doesn't match session context
    - Time range evaluation with midnight wrapping (22:00-06:00)
    - RAT type multiplier applied correctly in charging
    - All action types: notify, throttle, disconnect, log, block, suspend, tag
    - Multiple WHEN blocks matching: assignments override, actions accumulate
  - Use `testing` + table-driven tests
- **Verify:** `go test ./internal/policy/dsl/... -v`

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| Lexer tokenizes DSL keywords + operators | Task 1 | Task 7 (lexer_test.go) |
| Parser produces AST with error recovery | Task 3 | Task 7 (parser_test.go) |
| POLICY block parsing | Task 3 | Task 7 |
| MATCH block parsing | Task 3 | Task 7 |
| RULES block with WHEN/ACTION | Task 3 | Task 7 |
| WHEN conditions (usage, time_of_day, roaming) | Task 3, Task 5 | Task 7, Task 8 |
| ACTION block properties | Task 2, Task 3 | Task 7 |
| CHARGING block with rat_type_multiplier | Task 3, Task 4 | Task 7, Task 8 |
| Compiler: AST → JSON rule tree | Task 4 | Task 8 (compiler_test.go) |
| Evaluator: Evaluate(ctx, rules) → PolicyResult | Task 5 | Task 8 (evaluator_test.go) |
| PolicyResult: allow/deny, qos, charging, limits | Task 5 | Task 8 |
| Syntax errors with line number, column, message | Task 3 | Task 7 |
| DSL version field | Task 6 | Task 8 |

## Story-Specific Compliance Rules

- **Architecture:** All code goes in `internal/policy/dsl/` package. No external dependencies beyond stdlib + existing project deps.
- **Naming:** Go camelCase for all identifiers. Package name: `dsl`.
- **No DB access in this package:** The DSL package is a pure computation library. Store integration happens in STORY-023.
- **No Redis access in this package:** Caching integration happens in STORY-023/024.
- **Error handling:** All errors include line/column. Use Go error wrapping with `fmt.Errorf("...: %w", err)`.
- **ADR-001:** Modular monolith — DSL package is internal, imported by other internal packages.
- **ADR-003:** Custom AAA engine — DSL evaluator will be called by AAA session handling in future stories.
- **Business rules:** First-match-wins semantics is WRONG per algorithm spec. Last matching WHEN block wins for assignments; ALL matching actions collected.

## Risks & Mitigations

- **Parser complexity:** Recursive descent with compound conditions (AND/OR/NOT with precedence) is non-trivial. Mitigated by well-defined EBNF grammar and comprehensive test coverage.
- **Unit conversion precision:** Float arithmetic for data sizes could cause precision issues. Mitigated by using int64 for all compiled values (bytes, bps, seconds).
- **Grammar evolution:** Future DSL versions may add new keywords/constructs. Mitigated by DSL version field and clear token/AST separation.
