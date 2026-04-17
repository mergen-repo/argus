package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

const diameterEnabledYAML = `
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
    diameter:
      enabled: true
diameter:
  destination_realm: argus.local
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

func TestDiameter_DefaultsApplied(t *testing.T) {
	p := writeTemp(t, diameterEnabledYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	d := cfg.Diameter
	if d.Host != "argus-app" {
		t.Errorf("Host: want %q got %q", "argus-app", d.Host)
	}
	if d.Port != 3868 {
		t.Errorf("Port: want 3868 got %d", d.Port)
	}
	if d.OriginRealm != "sim.argus.test" {
		t.Errorf("OriginRealm: want %q got %q", "sim.argus.test", d.OriginRealm)
	}
	if d.WatchdogInterval != 30*time.Second {
		t.Errorf("WatchdogInterval: want 30s got %v", d.WatchdogInterval)
	}
	if d.ConnectTimeout != 5*time.Second {
		t.Errorf("ConnectTimeout: want 5s got %v", d.ConnectTimeout)
	}
	if d.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout: want 5s got %v", d.RequestTimeout)
	}
	if d.ReconnectBackoffMin != 1*time.Second {
		t.Errorf("ReconnectBackoffMin: want 1s got %v", d.ReconnectBackoffMin)
	}
	if d.ReconnectBackoffMax != 30*time.Second {
		t.Errorf("ReconnectBackoffMax: want 30s got %v", d.ReconnectBackoffMax)
	}

	op := cfg.Operators[0]
	if op.Diameter == nil {
		t.Fatal("Diameter config should not be nil for enabled operator")
	}
	wantApps := []string{"gx", "gy"}
	if len(op.Diameter.Applications) != len(wantApps) {
		t.Errorf("Applications: want %v got %v", wantApps, op.Diameter.Applications)
	} else {
		for i, a := range wantApps {
			if op.Diameter.Applications[i] != a {
				t.Errorf("Applications[%d]: want %q got %q", i, a, op.Diameter.Applications[i])
			}
		}
	}
	wantOriginHost := "sim-turkcell.sim.argus.test"
	if op.Diameter.OriginHost != wantOriginHost {
		t.Errorf("OriginHost: want %q got %q", wantOriginHost, op.Diameter.OriginHost)
	}
}

func TestDiameter_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown app rejected",
			body: strings.Replace(diameterEnabledYAML,
				"diameter:\n      enabled: true",
				"diameter:\n      enabled: true\n      applications: [s6a]", 1),
			want: "unknown app",
		},
		{
			name: "missing destination_realm when diameter enabled",
			body: strings.Replace(diameterEnabledYAML,
				"  destination_realm: argus.local\n", "", 1),
			want: "destination_realm",
		},
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

func TestDiameter_RadiusOnlyStillValid(t *testing.T) {
	p := writeTemp(t, validYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("RADIUS-only config should still be valid: %v", err)
	}
	if cfg.Operators[0].Diameter != nil {
		t.Error("Diameter should be nil for RADIUS-only operator")
	}
	if cfg.Diameter.Port != 3868 {
		t.Errorf("Diameter defaults still applied to global block: port %d", cfg.Diameter.Port)
	}
}

func TestToKebab(t *testing.T) {
	cases := []struct{ in, want string }{
		{"turkcell", "turkcell"},
		{"vodafone_tr", "vodafone-tr"},
		{"Turk Cell 01", "turk-cell-01"},
		{"--leading-trailing--", "leading-trailing"},
	}
	for _, c := range cases {
		got := toKebab(c.in)
		if got != c.want {
			t.Errorf("toKebab(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
