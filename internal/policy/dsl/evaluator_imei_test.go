package dsl

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// PAT-006 SessionContext construction-site audit (snapshot 2026-05-01).
//
// STORY-093 Phase 11 added IMEI / SoftwareVersion fields to SessionContext.
// The fields are `omitempty`, so a zero-value at any construction site is
// wire-shape safe — but a hot-path site that *forgets* to populate them
// silently produces a stale-zero-value bug (PAT-006).
//
// Audited literal-construction sites (output of
// `grep -rn 'dsl\.SessionContext{' internal/ cmd/`
// + `grep -rn 'SessionContext{' internal/policy/dsl/`):
//
//   PRODUCTION (must populate IMEI/SV when transport carries them):
//     internal/aaa/radius/server.go:481  — EAP path                  [IMEI POPULATED via Extract3GPPIMEISV — T5]
//     internal/aaa/radius/server.go:641  — Direct-Auth path          [IMEI POPULATED via Extract3GPPIMEISV — T5]
//
//   PRODUCTION (intentionally IMEI-skipped — out of IMEI-bearing path):
//     internal/policy/enforcer/enforcer.go:356 — RecordUsageCheck    [no transport context — usage-check only,
//                                                                     IMEI captured at auth-time, not re-evaluated here]
//     internal/policy/dryrun/service.go:412   — DryRun simulation    [synthetic context built from sim.Metadata,
//                                                                     IMEI not part of dry-run inputs]
//
//   TESTS / FIXTURES (IMEI irrelevant to scenarios under test):
//     internal/aaa/radius/enforcer_nilcache_integration_test.go:386  — buildMinimalSessionContext fixture
//     internal/aaa/bench/bench_test.go:205, :298, :348               — DSL eval microbench (no transport)
//     internal/policy/dsl/evaluator_test.go:* (28 sites)             — DSL evaluator unit tests
//
//   SBA (5G) NOTE: AUSF/UDM do NOT construct dsl.SessionContext directly —
//   IMEI is stored on AuthContext (internal/aaa/sba/ausf.go:105-106) and
//   propagated to the DSL context downstream. Tasks T6 wired this path.
//
// If a new construction site is added in production code without IMEI/SV
// population on the IMEI-bearing transport path, the audit comment above
// MUST be updated AND TestSessionContext_IMEIFields_ConstructionSiteAudit
// will surface the new site in CI logs for review.

func TestSessionContext_IMEIFields_ZeroValueSafe(t *testing.T) {
	t.Run("zero_value_struct_has_empty_imei_and_sv", func(t *testing.T) {
		sessCtx := SessionContext{}
		if sessCtx.IMEI != "" {
			t.Errorf("zero-value IMEI: got %q, want empty string", sessCtx.IMEI)
		}
		if sessCtx.SoftwareVersion != "" {
			t.Errorf("zero-value SoftwareVersion: got %q, want empty string", sessCtx.SoftwareVersion)
		}
	})

	t.Run("populated_fields_marshal_with_correct_keys", func(t *testing.T) {
		sessCtx := SessionContext{
			SIMID:           "11111111-2222-3333-4444-555555555555",
			IMEI:            "490154203237518",
			SoftwareVersion: "23",
		}
		raw, err := json.Marshal(sessCtx)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		imeiVal, ok := got["imei"]
		if !ok {
			t.Fatalf("expected key 'imei' in marshaled JSON, got keys: %v (raw=%s)", keysOf(got), string(raw))
		}
		if imeiVal != "490154203237518" {
			t.Errorf("imei value: got %v, want %q", imeiVal, "490154203237518")
		}

		svVal, ok := got["software_version"]
		if !ok {
			t.Fatalf("expected key 'software_version' in marshaled JSON, got keys: %v (raw=%s)", keysOf(got), string(raw))
		}
		if svVal != "23" {
			t.Errorf("software_version value: got %v, want %q", svVal, "23")
		}
	})

	t.Run("empty_strings_elided_by_omitempty_pat006_wire_shape_guard", func(t *testing.T) {
		sessCtx := SessionContext{
			SIMID:           "11111111-2222-3333-4444-555555555555",
			IMEI:            "",
			SoftwareVersion: "",
		}
		raw, err := json.Marshal(sessCtx)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
		if _, present := got["imei"]; present {
			t.Errorf("imei key MUST be elided by omitempty when empty, got: %v (raw=%s)", got["imei"], string(raw))
		}
		if _, present := got["software_version"]; present {
			t.Errorf("software_version key MUST be elided by omitempty when empty, got: %v (raw=%s)", got["software_version"], string(raw))
		}
	})
}

func TestSessionContext_IMEIFields_ConstructionSiteAudit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping construction-site audit (CI parity)")
	}

	cmd := exec.Command("git", "grep", "-n", `dsl\.SessionContext{`, "--", "internal/", "cmd/")
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		// `git grep` exits 1 when no matches found — that itself is a regression.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			t.Fatalf("git grep returned NO matches for 'dsl.SessionContext{' — production sites have vanished, this is a regression")
		}
		t.Fatalf("git grep failed: %v (stderr from ExitError if any)", err)
	}

	lines := splitLines(strings.TrimSpace(string(out)))
	productionSites := 0
	for _, line := range lines {
		t.Logf("audit-site: %s", line)
		if strings.Contains(line, "internal/aaa/radius/server.go") {
			productionSites++
		}
	}

	if productionSites < 2 {
		t.Errorf("expected >= 2 production RADIUS SessionContext sites (EAP + Direct), got %d; audited lines:\n%s",
			productionSites, strings.Join(lines, "\n"))
	}
	if len(lines) < 2 {
		t.Errorf("expected >= 2 total dsl.SessionContext{ sites repository-wide, got %d", len(lines))
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("cannot resolve repo root via git: %v", err)
	}
	return strings.TrimSpace(string(out))
}
