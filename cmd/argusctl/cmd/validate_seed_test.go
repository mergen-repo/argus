package cmd

import (
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/policy/dsl"
)

func TestExtractDSLStrings_SingleQuoted(t *testing.T) {
	body := `INSERT INTO policy_versions (id, dsl_content) VALUES
('51000000-0000-0000-0000-000000000001',
 'POLICY "ok-v1" { MATCH { apn = "iot.fleet" } RULES { bandwidth_down = 5mbps } }')
ON CONFLICT DO NOTHING;`
	got := extractDSLStrings(body)
	if len(got) != 1 {
		t.Fatalf("expected 1 extracted DSL string, got %d", len(got))
	}
	if !strings.HasPrefix(got[0].Source, "POLICY ") {
		t.Errorf("expected POLICY prefix, got %q", got[0].Source)
	}
}

func TestExtractDSLStrings_DollarQuoted(t *testing.T) {
	body := `INSERT INTO policy_versions VALUES ($$POLICY "dq-v1" { MATCH { apn = "x" } RULES { bandwidth_down = 1mbps } }$$);`
	got := extractDSLStrings(body)
	if len(got) != 1 {
		t.Fatalf("expected 1 dollar-quoted DSL string, got %d", len(got))
	}
}

func TestExtractDSLStrings_SkipsNonDSLLiterals(t *testing.T) {
	body := `INSERT INTO policy_versions (id, name, dsl_content) VALUES
('uuid-here', 'descriptive label', 'POLICY "v" { MATCH { apn = "a" } RULES { bandwidth_down = 1mbps } }');`
	got := extractDSLStrings(body)
	if len(got) != 1 {
		t.Fatalf("expected only POLICY string extracted, got %d items: %+v", len(got), got)
	}
}

func TestValidateExtractedFixtures(t *testing.T) {
	good := `POLICY "ok-v1" { MATCH { apn = "iot.fleet" } RULES { bandwidth_down = 5mbps } }`
	bad := `POLICY "broken" { MATCH apn = "x" RULES { bandwidth_down = 5mbps }`

	if errs := dsl.Validate(good); hasErrors(errs) {
		t.Errorf("expected good DSL to validate, got errors: %+v", errs)
	}
	if errs := dsl.Validate(bad); !hasErrors(errs) {
		t.Errorf("expected bad DSL to fail, but it passed")
	}
}

func hasErrors(errs []dsl.DSLError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

func TestExtractDSLStrings_EscapedQuotes(t *testing.T) {
	body := `INSERT INTO policy_versions VALUES
('POLICY "x''y" { MATCH { apn = "a" } RULES { bandwidth_down = 1mbps } }');`
	got := extractDSLStrings(body)
	if len(got) != 1 {
		t.Fatalf("expected 1 extracted, got %d", len(got))
	}
	if !strings.Contains(got[0].Source, `x'y`) {
		t.Errorf("expected '' → ' escape, got %q", got[0].Source)
	}
}
