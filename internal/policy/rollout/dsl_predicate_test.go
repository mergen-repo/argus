package rollout

import (
	"testing"

	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// FIX-230 Task 4 — unit tests for the DSL predicate wiring used by
// StartRollout (totalSIMs fallback) and ExecuteStage (SIM selection).
//
// We focus on the pure helper `compiledMatchFromVersion` plus the chained
// `dsl.ToSQLPredicate` translation, which together prove:
//   - AC-3: StartRollout fallback no longer counts ALL active SIMs blindly —
//     it uses the version's MATCH-derived predicate.
//   - AC-2/AC-6: ExecuteStage hands a real, non-"TRUE" predicate to
//     SelectSIMsForStage when MATCH has clauses.
//   - AC-5: empty MATCH (or empty DSLContent) → predicate "TRUE"
//     (preserves explicit apply-to-all design).
//   - AC-9: predicate string is parameterized — values become $N placeholders.

func TestCompiledMatchFromVersion_NilVersion(t *testing.T) {
	match, err := compiledMatchFromVersion(nil)
	if err != nil {
		t.Fatalf("nil version: unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("nil version: want nil match, got %+v", match)
	}
}

func TestCompiledMatchFromVersion_EmptyDSL(t *testing.T) {
	cases := []struct {
		name string
		dsl  string
	}{
		{"empty string", ""},
		{"whitespace only", "   \n\t  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := &store.PolicyVersion{ID: uuid.New(), DSLContent: tc.dsl}
			match, err := compiledMatchFromVersion(v)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if match != nil {
				t.Errorf("want nil match for empty DSL, got %+v", match)
			}
		})
	}
}

func TestCompiledMatchFromVersion_MissingMatchBlock(t *testing.T) {
	// DSL parser REQUIRES a MATCH block (DSL_MISSING_BLOCK error), so this
	// path can never occur for a stored version (CreateVersion rejects it).
	// If somehow a malformed policy reached the rollout path, the helper
	// MUST fail closed (surface an error) instead of silently degrading to
	// the "TRUE" predicate — see FIX-230 Gate F-A6. Silent degradation would
	// migrate ALL active tenant SIMs to the new version.
	src := `POLICY "no-match" {
		RULES {
			bandwidth_down = 1mbps
		}
	}`
	v := &store.PolicyVersion{ID: uuid.New(), DSLContent: src}
	match, err := compiledMatchFromVersion(v)
	if err == nil {
		t.Fatalf("expected error for missing MATCH block; got nil (match=%+v)", match)
	}
	if match != nil {
		t.Errorf("expected nil match on parse failure, got %+v", match)
	}
}

func TestCompiledMatchFromVersion_SingleEqCondition(t *testing.T) {
	src := `POLICY "single-match" {
		MATCH { apn = "iot.data" }
		RULES { bandwidth_down = 1mbps }
	}`
	v := &store.PolicyVersion{ID: uuid.New(), DSLContent: src}
	match, err := compiledMatchFromVersion(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil || len(match.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %+v", match)
	}
	c := match.Conditions[0]
	if c.Field != "apn" {
		t.Errorf("field = %q, want %q", c.Field, "apn")
	}
	if c.Op != "eq" {
		t.Errorf("op = %q, want %q", c.Op, "eq")
	}
	if c.Value != "iot.data" {
		t.Errorf("value = %v, want %q", c.Value, "iot.data")
	}
}

func TestCompiledMatchFromVersion_InvalidDSL(t *testing.T) {
	// Parser errors return early from CompileSource with (nil, errs, nil).
	// FIX-230 Gate F-A6: helper MUST fail closed by returning a non-nil error
	// when the stored DSL has any parse-time "error"-severity diagnostic.
	// Silent degradation to a nil match (which downstream becomes "TRUE")
	// would migrate ALL active tenant SIMs to the new version.
	src := `POLICY "broken" { MATCH { invalid_syntax_here }`
	v := &store.PolicyVersion{ID: uuid.New(), DSLContent: src}
	match, err := compiledMatchFromVersion(v)
	if err == nil {
		t.Fatalf("expected error for corrupt stored DSL; got nil (match=%+v)", match)
	}
	if match != nil {
		t.Errorf("expected nil match on parse failure, got %+v", match)
	}
}

// TestStartRollout_DslPredicateWiring proves the chain:
//
//	stored DSL → compiledMatchFromVersion → ToSQLPredicate → CountWithPredicate args.
//
// We don't run StartRollout (that needs a real PolicyStore + DB); instead we
// verify the function signatures / outputs the production code feeds into
// CountWithPredicate are EXACTLY what we expect — that's enough to prove
// AC-3 wiring without a DB round-trip.
func TestStartRollout_DslPredicateWiring(t *testing.T) {
	tests := []struct {
		name           string
		dslSource      string
		wantPredicate  string
		wantArgsLen    int
		wantArgValues  []interface{}
	}{
		{
			name:          "empty DSL → predicate TRUE",
			dslSource:     "",
			wantPredicate: "TRUE",
			wantArgsLen:   0,
		},
		{
			name: "MATCH with apn → tenant-scoped sub-select",
			dslSource: `POLICY "p" {
				MATCH { apn = "iot.data" }
				RULES { bandwidth_down = 1mbps }
			}`,
			// caller passes tenantArgIdx=1 startArgIdx=2 (matches StartRollout).
			wantPredicate: "s.apn_id = (SELECT id FROM apns WHERE tenant_id = $1 AND name = $2)",
			wantArgsLen:   1,
			wantArgValues: []interface{}{"iot.data"},
		},
		{
			name: "MATCH with rat_type → simple equality",
			dslSource: `POLICY "p" {
				MATCH { rat_type = nb_iot }
				RULES { bandwidth_down = 1mbps }
			}`,
			wantPredicate: "s.rat_type = $2",
			wantArgsLen:   1,
			wantArgValues: []interface{}{"nb_iot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &store.PolicyVersion{ID: uuid.New(), DSLContent: tt.dslSource}
			match, err := compiledMatchFromVersion(v)
			if err != nil {
				t.Fatalf("compiledMatchFromVersion: %v", err)
			}
			// Mirror StartRollout call: tenantArgIdx=1, startArgIdx=2.
			predicate, args, _, err := dsl.ToSQLPredicate(match, 1, 2)
			if err != nil {
				t.Fatalf("ToSQLPredicate: %v", err)
			}
			if predicate != tt.wantPredicate {
				t.Errorf("predicate = %q, want %q", predicate, tt.wantPredicate)
			}
			if len(args) != tt.wantArgsLen {
				t.Errorf("args len = %d, want %d (args=%v)", len(args), tt.wantArgsLen, args)
			}
			for i, want := range tt.wantArgValues {
				if i >= len(args) {
					t.Errorf("missing arg %d (want %v)", i, want)
					continue
				}
				if args[i] != want {
					t.Errorf("arg[%d] = %v, want %v", i, args[i], want)
				}
			}
		})
	}
}

// TestExecuteStage_DslPredicateWiring covers the ExecuteStage call site:
// argument-index offset depends on PreviousVersionID presence (3 vs 4).
func TestExecuteStage_DslPredicateWiring(t *testing.T) {
	src := `POLICY "p" {
		MATCH { rat_type = lte_m }
		RULES { bandwidth_down = 1mbps }
	}`
	v := &store.PolicyVersion{ID: uuid.New(), DSLContent: src}
	match, err := compiledMatchFromVersion(v)
	if err != nil {
		t.Fatalf("compiledMatchFromVersion: %v", err)
	}

	// Case 1: PreviousVersionID nil → DSL args start at $3.
	pred1, args1, _, err := dsl.ToSQLPredicate(match, 1, 3)
	if err != nil {
		t.Fatalf("ToSQLPredicate (no prevVer): %v", err)
	}
	wantPred1 := "s.rat_type = $3"
	if pred1 != wantPred1 {
		t.Errorf("predicate (no prevVer) = %q, want %q", pred1, wantPred1)
	}
	if len(args1) != 1 || args1[0] != "lte_m" {
		t.Errorf("args (no prevVer) = %v, want [lte_m]", args1)
	}

	// Case 2: PreviousVersionID set → DSL args start at $4.
	pred2, args2, _, err := dsl.ToSQLPredicate(match, 1, 4)
	if err != nil {
		t.Fatalf("ToSQLPredicate (with prevVer): %v", err)
	}
	wantPred2 := "s.rat_type = $4"
	if pred2 != wantPred2 {
		t.Errorf("predicate (with prevVer) = %q, want %q", pred2, wantPred2)
	}
	if len(args2) != 1 || args2[0] != "lte_m" {
		t.Errorf("args (with prevVer) = %v, want [lte_m]", args2)
	}

	// Critical: the predicate must NEVER be the literal "TRUE" when MATCH has
	// conditions. If it were, ExecuteStage would assign every active SIM in
	// the tenant to the new version, ignoring the policy author's MATCH.
	if pred1 == "TRUE" || pred2 == "TRUE" {
		t.Errorf("predicate must not degrade to TRUE for non-empty MATCH (got %q / %q)", pred1, pred2)
	}
}

// AC-5: empty MATCH → predicate "TRUE" preserves the explicit
// "apply-to-all-active-SIMs" design.
func TestExecuteStage_EmptyMatchYieldsTRUE(t *testing.T) {
	v := &store.PolicyVersion{ID: uuid.New(), DSLContent: ""}
	match, err := compiledMatchFromVersion(v)
	if err != nil {
		t.Fatalf("compiledMatchFromVersion: %v", err)
	}
	predicate, args, _, err := dsl.ToSQLPredicate(match, 1, 4)
	if err != nil {
		t.Fatalf("ToSQLPredicate: %v", err)
	}
	if predicate != "TRUE" {
		t.Errorf("empty MATCH predicate = %q, want %q", predicate, "TRUE")
	}
	if len(args) != 0 {
		t.Errorf("args len = %d, want 0", len(args))
	}
}
