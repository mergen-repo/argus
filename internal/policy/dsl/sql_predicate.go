package dsl

import (
	"fmt"
	"strings"
)

// ToSQLPredicate builds a parameterized SQL WHERE fragment from a CompiledMatch.
//
// startArgIdx is the next $N placeholder to use; the caller appends `args` to
// its own args slice and continues numbering. tenantArgIdx is the placeholder
// already bound to the tenant_id at the caller level (used for tenant-scoped
// sub-selects such as apns.tenant_id = $T).
//
// Returns:
//
//	sqlFragment — e.g. "(s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1
//	              AND name = $3))" or "TRUE" when match is empty.
//	args        — values bound to placeholders, in order.
//	nextArgIdx  — startArgIdx + len(args).
//	err         — non-nil on unknown field, unsupported operator, or
//	              type-mismatched literal value.
//
// Security (FIX-230 AC-9):
//   - Field names are NEVER concatenated into SQL. Every supported field is
//     mapped to a fixed, vetted SQL fragment template via a whitelist below.
//   - Literal values are ALWAYS bound via $N placeholders — no Sprintf("'%s'").
//   - For imsi_prefix, the value is bound as `<prefix>%` so pgx LIKE-binds it
//     as a parameter, never as inline SQL.
func ToSQLPredicate(match *CompiledMatch, tenantArgIdx int, startArgIdx int) (string, []interface{}, int, error) {
	if match == nil || len(match.Conditions) == 0 {
		return "TRUE", nil, startArgIdx, nil
	}

	args := make([]interface{}, 0, len(match.Conditions))
	parts := make([]string, 0, len(match.Conditions))
	argIdx := startArgIdx

	for _, cond := range match.Conditions {
		fragment, condArgs, nextIdx, err := compileMatchConditionToSQL(cond, tenantArgIdx, argIdx)
		if err != nil {
			return "", nil, startArgIdx, err
		}
		parts = append(parts, fragment)
		args = append(args, condArgs...)
		argIdx = nextIdx
	}

	return strings.Join(parts, " AND "), args, argIdx, nil
}

func compileMatchConditionToSQL(cond CompiledMatchCondition, tenantArgIdx int, startArgIdx int) (string, []interface{}, int, error) {
	switch cond.Op {
	case "eq":
		return buildEqualityPredicate(cond.Field, cond.Value, false, tenantArgIdx, startArgIdx)
	case "neq":
		return buildEqualityPredicate(cond.Field, cond.Value, true, tenantArgIdx, startArgIdx)
	case "in":
		return buildInPredicate(cond.Field, cond.Values, tenantArgIdx, startArgIdx)
	default:
		return "", nil, startArgIdx, fmt.Errorf("dsl: operator %q not allowed in MATCH→SQL", cond.Op)
	}
}

func buildEqualityPredicate(field string, value interface{}, negate bool, tenantArgIdx int, startArgIdx int) (string, []interface{}, int, error) {
	switch field {
	case "apn":
		s, ok := stringValue(value)
		if !ok {
			return "", nil, startArgIdx, fmt.Errorf("dsl: apn value must be string, got %T", value)
		}
		// Tenant-scoped sub-select. tenantArgIdx ($T) is already in caller's args slice.
		frag := fmt.Sprintf("s.apn_id = (SELECT id FROM apns WHERE tenant_id = $%d AND name = $%d)", tenantArgIdx, startArgIdx)
		if negate {
			frag = "NOT (" + frag + ")"
		}
		return frag, []interface{}{s}, startArgIdx + 1, nil

	case "operator":
		s, ok := stringValue(value)
		if !ok {
			return "", nil, startArgIdx, fmt.Errorf("dsl: operator value must be string, got %T", value)
		}
		// operators table has no tenant_id column — operators are global.
		frag := fmt.Sprintf("s.operator_id = (SELECT id FROM operators WHERE code = $%d)", startArgIdx)
		if negate {
			frag = "NOT (" + frag + ")"
		}
		return frag, []interface{}{s}, startArgIdx + 1, nil

	case "imsi_prefix":
		s, ok := stringValue(value)
		if !ok {
			return "", nil, startArgIdx, fmt.Errorf("dsl: imsi_prefix value must be string, got %T", value)
		}
		op := "LIKE"
		if negate {
			op = "NOT LIKE"
		}
		frag := fmt.Sprintf("s.imsi %s $%d", op, startArgIdx)
		return frag, []interface{}{s + "%"}, startArgIdx + 1, nil

	case "rat_type":
		s, ok := stringValue(value)
		if !ok {
			return "", nil, startArgIdx, fmt.Errorf("dsl: rat_type value must be string, got %T", value)
		}
		op := "="
		if negate {
			op = "<>"
		}
		return fmt.Sprintf("s.rat_type %s $%d", op, startArgIdx), []interface{}{s}, startArgIdx + 1, nil

	case "sim_type":
		s, ok := stringValue(value)
		if !ok {
			return "", nil, startArgIdx, fmt.Errorf("dsl: sim_type value must be string, got %T", value)
		}
		op := "="
		if negate {
			op = "<>"
		}
		return fmt.Sprintf("s.sim_type %s $%d", op, startArgIdx), []interface{}{s}, startArgIdx + 1, nil

	default:
		return "", nil, startArgIdx, fmt.Errorf("dsl: field %q not allowed in MATCH→SQL", field)
	}
}

func buildInPredicate(field string, values []interface{}, tenantArgIdx int, startArgIdx int) (string, []interface{}, int, error) {
	if len(values) == 0 {
		return "", nil, startArgIdx, fmt.Errorf("dsl: IN list for field %q is empty", field)
	}

	switch field {
	case "apn":
		strs, err := allStrings(field, values)
		if err != nil {
			return "", nil, startArgIdx, err
		}
		frag := fmt.Sprintf("s.apn_id IN (SELECT id FROM apns WHERE tenant_id = $%d AND name = ANY($%d))", tenantArgIdx, startArgIdx)
		return frag, []interface{}{strs}, startArgIdx + 1, nil

	case "operator":
		strs, err := allStrings(field, values)
		if err != nil {
			return "", nil, startArgIdx, err
		}
		frag := fmt.Sprintf("s.operator_id IN (SELECT id FROM operators WHERE code = ANY($%d))", startArgIdx)
		return frag, []interface{}{strs}, startArgIdx + 1, nil

	case "rat_type":
		strs, err := allStrings(field, values)
		if err != nil {
			return "", nil, startArgIdx, err
		}
		return fmt.Sprintf("s.rat_type = ANY($%d)", startArgIdx), []interface{}{strs}, startArgIdx + 1, nil

	case "sim_type":
		strs, err := allStrings(field, values)
		if err != nil {
			return "", nil, startArgIdx, err
		}
		return fmt.Sprintf("s.sim_type = ANY($%d)", startArgIdx), []interface{}{strs}, startArgIdx + 1, nil

	case "imsi_prefix":
		strs, err := allStrings(field, values)
		if err != nil {
			return "", nil, startArgIdx, err
		}
		clauses := make([]string, len(strs))
		args := make([]interface{}, len(strs))
		idx := startArgIdx
		for i, p := range strs {
			clauses[i] = fmt.Sprintf("s.imsi LIKE $%d", idx)
			args[i] = p + "%"
			idx++
		}
		return "(" + strings.Join(clauses, " OR ") + ")", args, idx, nil

	default:
		return "", nil, startArgIdx, fmt.Errorf("dsl: field %q not allowed in MATCH→SQL", field)
	}
}

func stringValue(v interface{}) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func allStrings(field string, values []interface{}) ([]string, error) {
	out := make([]string, len(values))
	for i, v := range values {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("dsl: %s IN list element #%d must be string, got %T", field, i, v)
		}
		out[i] = s
	}
	return out, nil
}
