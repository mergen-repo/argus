package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validYAML = `
argus:
  radius_host: argus-app
  radius_auth_port: 1812
  radius_accounting_port: 1813
  radius_shared_secret: test-secret-at-least-16chars
  db_url: postgres://user:pass@host/db
operators:
  - code: turkcell
    nas_identifier: sim-turkcell
    nas_ip: 10.99.0.1
scenarios:
  - name: normal
    weight: 1.0
    session_duration_seconds: [60, 120]
    interim_interval_seconds: 30
    bytes_per_interim_in: [1000, 5000]
    bytes_per_interim_out: [500, 2500]
rate:
  max_radius_requests_per_second: 5
  initial_jitter_seconds: [0, 10]
metrics:
  listen: ":9099"
log:
  level: info
  format: console
`

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write temp cfg: %v", err)
	}
	return p
}

func TestLoad_Valid(t *testing.T) {
	p := writeTemp(t, validYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Argus.RadiusHost != "argus-app" {
		t.Errorf("host: got %q", cfg.Argus.RadiusHost)
	}
	if len(cfg.Operators) != 1 || cfg.Operators[0].Code != "turkcell" {
		t.Errorf("operators: got %+v", cfg.Operators)
	}
	if cfg.OperatorByCode("turkcell") == nil {
		t.Error("OperatorByCode(turkcell) returned nil")
	}
	if cfg.OperatorByCode("nonexistent") != nil {
		t.Error("OperatorByCode(nonexistent) should be nil")
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("ARGUS_SIM_RADIUS_HOST", "env-override-host")
	p := writeTemp(t, validYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Argus.RadiusHost != "env-override-host" {
		t.Errorf("env override not applied: got %q", cfg.Argus.RadiusHost)
	}
}

func TestValidate_FailsOnMissingFields(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no radius host", strings.Replace(validYAML, "radius_host: argus-app", "radius_host: \"\"", 1), "radius_host"},
		{"no secret", strings.Replace(validYAML, "radius_shared_secret: test-secret-at-least-16chars", "radius_shared_secret: \"\"", 1), "radius_shared_secret"},
		{"no operators", strings.Replace(validYAML, "operators:\n  - code: turkcell\n    nas_identifier: sim-turkcell\n    nas_ip: 10.99.0.1", "operators: []", 1), "operator"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeTemp(t, c.body)
			_, err := Load(p)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("expected error containing %q, got: %v", c.want, err)
			}
		})
	}
}

func TestValidate_WeightsMustSumToOne(t *testing.T) {
	body := strings.Replace(validYAML, "weight: 1.0", "weight: 0.5", 1)
	p := writeTemp(t, body)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "sum") {
		t.Errorf("expected weight-sum error, got: %v", err)
	}
}
