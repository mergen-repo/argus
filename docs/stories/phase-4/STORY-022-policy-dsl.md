# STORY-022: Policy DSL Parser & Evaluator

## User Story
As a policy editor, I want to write human-readable policy rules using a domain-specific language, so that I can define complex authentication, QoS, and charging rules without writing code.

## Description
Design and implement a Policy DSL in pkg/dsl. The DSL supports POLICY, MATCH, RULES, WHEN/ACTION, and CHARGING blocks. The parser produces an AST, which is compiled to a JSON rule tree for storage in TBL-14 (policy_versions.compiled_rules). The evaluator takes a session context (SIM, operator, APN, RAT, time, usage) and returns the matching actions (accept/reject, QoS attributes, charging model). Includes syntax validation, error reporting with line numbers, and a built-in linter.

## Architecture Reference
- Services: SVC-05 (Policy Engine — internal/policy)
- Packages: pkg/dsl (parser, lexer, ast, compiler, evaluator)
- Database Tables: TBL-14 (policy_versions — dsl_source, compiled_rules JSONB)
- Source: docs/architecture/services/_index.md (SVC-05)
- Spec: docs/architecture/DSL_GRAMMAR.md (complete EBNF, conditions, actions, compiled representation), docs/architecture/ALGORITHMS.md (Section 6: Policy Evaluation)

## Screen Reference
- SCR-062: Policy Editor — DSL text editor with syntax highlighting, error markers

## Acceptance Criteria
- [ ] Lexer: tokenize DSL source into POLICY, MATCH, RULES, WHEN, ACTION, CHARGING keywords + operators
- [ ] Parser: produce AST from token stream with error recovery (report all errors, not just first)
- [ ] POLICY block: `POLICY "name" { ... }` — policy container with metadata
- [ ] MATCH block: `MATCH { operator IN ("op1","op2"), apn = "iot.data", rat_type = "4G" }` — scope filter
- [ ] RULES block: `RULES { WHEN condition ACTION { ... } }` — ordered rule list (first match wins)
- [ ] WHEN conditions: `usage > 1GB`, `time_of_day BETWEEN "08:00" AND "22:00"`, `roaming = true`
- [ ] ACTION block: `ACTION { qos_profile = "premium", max_bandwidth = "10Mbps", session_timeout = 3600 }`
- [ ] CHARGING block: `CHARGING { model = "volume", rate_per_mb = 0.01, currency = "USD" }`
- [ ] Compiler: AST → JSON rule tree with normalized conditions and actions
- [ ] Evaluator: `Evaluate(ctx SessionContext, rules CompiledRules) → PolicyResult`
- [ ] PolicyResult contains: allow/deny, qos_attributes, charging_params, session_limits
- [ ] Evaluation cached in Redis (key: policy_version_id + SIM context hash, TTL from policy)
- [ ] Syntax errors include line number, column, and helpful message
- [ ] DSL version field for future grammar evolution

## Dependencies
- Blocked by: STORY-001 (scaffold — pkg/ directory)
- Blocks: STORY-023 (policy CRUD stores DSL), STORY-024 (dry-run evaluates DSL), STORY-025 (rollout applies DSL)

## Test Scenarios
- [ ] Parse valid POLICY with MATCH + RULES + CHARGING → valid AST
- [ ] Parse policy with syntax error → error with line:column
- [ ] Compile AST → JSON rule tree matches expected structure
- [ ] Evaluate: SIM matching WHEN condition → correct ACTION returned
- [ ] Evaluate: SIM not matching any WHEN → default action returned
- [ ] Evaluate: first matching WHEN wins (rule ordering)
- [ ] Evaluate: CHARGING block returns correct rate_per_mb and model
- [ ] Complex condition: `usage > 500MB AND rat_type = "4G" AND time_of_day BETWEEN "00:00" AND "06:00"` evaluates correctly
- [ ] Empty RULES block → default allow with no QoS overrides
- [ ] Roundtrip: DSL source → compile → evaluate → same result as direct AST evaluation

## Effort Estimate
- Size: XL
- Complexity: Very High
