package dsl

import (
	"strings"
	"testing"
)

func TestFormat_CanonicalUnchanged(t *testing.T) {
	src := `POLICY "p1" {
  MATCH {
    apn = "internet"
  }
  RULES {
    bandwidth_down = 10mbps
  }
}
`
	out, err := Format(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != src {
		t.Errorf("canonical input was changed.\nWANT:\n%s\nGOT:\n%s", src, out)
	}
}

func TestFormat_NormalizesMangledInput(t *testing.T) {
	mangled := `POLICY    "p1"   {
MATCH{apn="internet"
rat_type=lte}
RULES{bandwidth_down=10mbps}
}`
	out, err := Format(mangled)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Required structural normalizations:
	if !strings.Contains(out, `POLICY "p1" {`) {
		t.Errorf("expected canonical POLICY header, got:\n%s", out)
	}
	if !strings.Contains(out, "  MATCH {") {
		t.Errorf("expected 2-space-indented MATCH, got:\n%s", out)
	}
	if !strings.Contains(out, `    apn = "internet"`) {
		t.Errorf("expected 4-space-indented MATCH body with spaces around '=', got:\n%s", out)
	}
	if !strings.Contains(out, "  RULES {") {
		t.Errorf("expected 2-space-indented RULES, got:\n%s", out)
	}
	if !strings.HasSuffix(out, "}\n") {
		t.Errorf("expected output to end with closing brace + newline, got:\n%s", out)
	}
	// Final newline should be exactly one.
	if strings.HasSuffix(out, "\n\n") {
		t.Errorf("expected exactly one trailing newline, got multiple")
	}
}

func TestFormat_Idempotent(t *testing.T) {
	cases := []string{
		`POLICY "p1" {
  MATCH {
    apn = "internet"
  }
  RULES {
    bandwidth_down = 10mbps
  }
}
`,
		`POLICY    "p2"   {
MATCH{apn="m2m"
rat_type=lte}
RULES{bandwidth_down=1mbps
session_timeout=24h}
CHARGING{model="prepaid"
rate_per_mb=0.02}
}`,
	}
	for i, src := range cases {
		first, err := Format(src)
		if err != nil {
			t.Fatalf("case %d: first format error: %v", i, err)
		}
		second, err := Format(first)
		if err != nil {
			t.Fatalf("case %d: second format error: %v", i, err)
		}
		if first != second {
			t.Errorf("case %d: not idempotent.\nFIRST:\n%s\nSECOND:\n%s", i, first, second)
		}
	}
}

func TestFormat_InvalidSourceReturnedUnchanged(t *testing.T) {
	bad := `POLICY "broken" {`
	out, err := Format(bad)
	if err != nil {
		t.Fatalf("unexpected error on invalid input: %v", err)
	}
	if out != bad {
		t.Errorf("invalid input should be returned unchanged.\nINPUT:\n%s\nGOT:\n%s", bad, out)
	}
}

func TestFormat_PreservesComments(t *testing.T) {
	src := `# top comment
POLICY "p1" {
  # match block comment
  MATCH {
    apn = "internet"
  }
  RULES {
    bandwidth_down = 10mbps
  }
}
`
	out, err := Format(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "# top comment") {
		t.Errorf("top-level comment was dropped:\n%s", out)
	}
	if !strings.Contains(out, "# match block comment") {
		t.Errorf("inline comment was dropped:\n%s", out)
	}
}

func TestFormat_BlankSourcePassesThrough(t *testing.T) {
	// Empty source is technically not valid DSL — Format should leave it
	// alone (Validate returns errors).
	out, err := Format("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "" {
		t.Errorf("empty source should remain empty, got %q", out)
	}
}
