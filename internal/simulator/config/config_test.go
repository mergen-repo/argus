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

const sbaEnabledYAML = `
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
    sba:
      enabled: true
      rate: 0.2
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

func TestSBA_DefaultsApplied(t *testing.T) {
	p := writeTemp(t, sbaEnabledYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s := cfg.SBA
	if s.Host != "argus-app" {
		t.Errorf("Host: want %q got %q", "argus-app", s.Host)
	}
	if s.Port != 8443 {
		t.Errorf("Port: want 8443 got %d", s.Port)
	}
	if s.ServingNetworkName != "5G:mnc001.mcc286.3gppnetwork.org" {
		t.Errorf("ServingNetworkName: want default got %q", s.ServingNetworkName)
	}
	if s.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout: want 5s got %v", s.RequestTimeout)
	}
	if s.AMFInstanceID != "sim-amf-01" {
		t.Errorf("AMFInstanceID: want %q got %q", "sim-amf-01", s.AMFInstanceID)
	}
	if s.DeregCallbackURI != "http://sim-amf.invalid/dereg" {
		t.Errorf("DeregCallbackURI: want default got %q", s.DeregCallbackURI)
	}

	op := cfg.Operators[0]
	if op.SBA == nil {
		t.Fatal("SBA config should not be nil for enabled operator")
	}
	if op.SBA.AuthMethod != "5G_AKA" {
		t.Errorf("AuthMethod: want %q got %q", "5G_AKA", op.SBA.AuthMethod)
	}
	if op.SBA.Rate != 0.2 {
		t.Errorf("Rate: want 0.2 got %v", op.SBA.Rate)
	}
}

func TestSBA_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown auth method rejected",
			body: strings.Replace(sbaEnabledYAML,
				"      enabled: true\n      rate: 0.2",
				"      enabled: true\n      rate: 0.2\n      auth_method: INVALID", 1),
			want: "unknown method",
		},
		{
			name: "rate above 1 rejected",
			body: strings.Replace(sbaEnabledYAML,
				"      rate: 0.2", "      rate: 1.2", 1),
			want: "rate out of range",
		},
		{
			name: "rate below 0 rejected",
			body: strings.Replace(sbaEnabledYAML,
				"      rate: 0.2", "      rate: -0.1", 1),
			want: "rate out of range",
		},
		{
			name: "EAP_AKA_PRIME reserved for future story",
			body: strings.Replace(sbaEnabledYAML,
				"      enabled: true\n      rate: 0.2",
				"      enabled: true\n      rate: 0.2\n      auth_method: EAP_AKA_PRIME", 1),
			want: "only 5G_AKA implemented",
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

func TestSBA_ProdGuardTriggers(t *testing.T) {
	body := sbaEnabledYAML + `sba:
  tls_skip_verify: true
`
	t.Setenv("ARGUS_SIM_ENV", "prod")
	p := writeTemp(t, body)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "tls_skip_verify") {
		t.Errorf("expected prod-guard error containing %q, got: %v", "tls_skip_verify", err)
	}
}

// TestSBA_ProdGuardDefaultIsOn verifies that omitting the prod_guard field
// defaults to guard=ON (the plan default). A config without prod_guard but
// with tls_skip_verify=true must still be rejected under ARGUS_SIM_ENV=prod.
func TestSBA_ProdGuardDefaultIsOn(t *testing.T) {
	body := sbaEnabledYAML + `sba:
  tls_skip_verify: true
`
	t.Setenv("ARGUS_SIM_ENV", "prod")
	p := writeTemp(t, body)
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected prod-guard error when prod_guard is omitted (default should be ON)")
	}
}

// TestSBA_ProdGuardDisabled verifies that explicitly setting prod_guard: false
// allows tls_skip_verify: true under ARGUS_SIM_ENV=prod (exceptional hand-crafted
// scenarios, documented in simulator.md).
func TestSBA_ProdGuardDisabled(t *testing.T) {
	body := sbaEnabledYAML + `sba:
  tls_skip_verify: true
  prod_guard: false
`
	t.Setenv("ARGUS_SIM_ENV", "prod")
	p := writeTemp(t, body)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("prod_guard: false should bypass the prod-env check, got: %v", err)
	}
	if cfg.SBA.ProdGuard == nil || *cfg.SBA.ProdGuard {
		t.Errorf("ProdGuard: want explicit false, got %v", cfg.SBA.ProdGuard)
	}
}

// TestSBA_DefaultSlicesApplied verifies that an operator that opts in without
// slices receives the default [{SST:1, SD:"000001"}].
func TestSBA_DefaultSlicesApplied(t *testing.T) {
	p := writeTemp(t, sbaEnabledYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op := cfg.Operators[0]
	if op.SBA == nil || len(op.SBA.Slices) != 1 {
		t.Fatalf("Slices: want 1 default, got %+v", op.SBA)
	}
	if op.SBA.Slices[0].SST != 1 || op.SBA.Slices[0].SD != "000001" {
		t.Errorf("default slice=%+v, want {SST:1 SD:000001}", op.SBA.Slices[0])
	}
}

// TestSBA_PerOperatorSlices verifies that a YAML-provided slice list overrides
// the default.
func TestSBA_PerOperatorSlices(t *testing.T) {
	body := strings.Replace(sbaEnabledYAML,
		"      rate: 0.2",
		"      rate: 0.2\n      slices:\n        - sst: 2\n          sd: \"000010\"\n        - sst: 3", 1)
	p := writeTemp(t, body)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op := cfg.Operators[0]
	if op.SBA == nil || len(op.SBA.Slices) != 2 {
		t.Fatalf("Slices: want 2 entries, got %+v", op.SBA)
	}
	if op.SBA.Slices[0].SST != 2 || op.SBA.Slices[0].SD != "000010" {
		t.Errorf("Slices[0]=%+v, want {SST:2 SD:000010}", op.SBA.Slices[0])
	}
	if op.SBA.Slices[1].SST != 3 {
		t.Errorf("Slices[1].SST=%d, want 3", op.SBA.Slices[1].SST)
	}
}

func TestSBA_RadiusOnlyStillValid(t *testing.T) {
	p := writeTemp(t, validYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("RADIUS-only config should still be valid: %v", err)
	}
	if cfg.Operators[0].SBA != nil {
		t.Error("SBA should be nil for RADIUS-only operator")
	}
	if cfg.SBA.Port != 8443 {
		t.Errorf("SBA defaults still applied to global block: port %d", cfg.SBA.Port)
	}
}

func TestSBA_DiameterOnlyStillValid(t *testing.T) {
	p := writeTemp(t, diameterEnabledYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Diameter-only config should still be valid: %v", err)
	}
	if cfg.Operators[0].SBA != nil {
		t.Error("SBA should be nil for Diameter-only operator")
	}
}

// TestReactive_DefaultsOff_NoChange verifies that a disabled reactive block
// is a no-op: Validate returns nil and no fields are mutated.
func TestReactive_DefaultsOff_NoChange(t *testing.T) {
	p := writeTemp(t, validYAML)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Reactive.Enabled {
		t.Error("Reactive.Enabled should be false when not set")
	}
	if cfg.Reactive.EarlyTerminationMargin != 0 {
		t.Errorf("EarlyTerminationMargin should be 0 when disabled, got %v", cfg.Reactive.EarlyTerminationMargin)
	}
	if cfg.Reactive.CoAListener.ListenAddr != "" {
		t.Errorf("CoAListener.ListenAddr should be empty when disabled, got %q", cfg.Reactive.CoAListener.ListenAddr)
	}
}

// TestReactive_EnabledAppliesDefaults verifies that enabling reactive fills in
// all zero-value fields with their documented defaults.
func TestReactive_EnabledAppliesDefaults(t *testing.T) {
	cfg := &Config{
		Argus: ArgusConfig{
			RadiusHost:           "argus-app",
			RadiusAuthPort:       1812,
			RadiusAccountingPort: 1813,
			RadiusSharedSecret:   "s",
			DBURL:                "postgres://x",
			DBRefreshInterval:    5 * time.Minute,
		},
		Operators: []OperatorConfig{{Code: "turkcell"}},
		Scenarios: []ScenarioConfig{{
			Name:                   "normal",
			Weight:                 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate:    RateConfig{MaxRadiusRequestsPerSecond: 5},
		Metrics: MetricsConfig{Listen: ":9099"},
		Log:     LogConfig{Level: "info", Format: "console"},
		Reactive: ReactiveDefaults{
			Enabled:                 true,
			RejectMaxRetriesPerHour: 5,
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	r := cfg.Reactive
	if r.EarlyTerminationMargin != 5*time.Second {
		t.Errorf("EarlyTerminationMargin: want 5s got %v", r.EarlyTerminationMargin)
	}
	if r.RejectBackoffBase != 30*time.Second {
		t.Errorf("RejectBackoffBase: want 30s got %v", r.RejectBackoffBase)
	}
	if r.RejectBackoffMax != 600*time.Second {
		t.Errorf("RejectBackoffMax: want 600s got %v", r.RejectBackoffMax)
	}
	if r.RejectMaxRetriesPerHour != 5 {
		t.Errorf("RejectMaxRetriesPerHour: want 5 got %d", r.RejectMaxRetriesPerHour)
	}
	if r.CoAListener.ListenAddr != "0.0.0.0:3799" {
		t.Errorf("CoAListener.ListenAddr: want %q got %q", "0.0.0.0:3799", r.CoAListener.ListenAddr)
	}
	if r.CoAListener.SharedSecret != "s" {
		t.Errorf("CoAListener.SharedSecret: want %q (inherited) got %q", "s", r.CoAListener.SharedSecret)
	}
	if !r.SessionTimeoutRespect {
		t.Error("SessionTimeoutRespect: want true (default-on-when-enabled)")
	}
}

// TestReactive_BackoffBaseGreaterThanMax_Error verifies that a base > max
// configuration is rejected.
func TestReactive_BackoffBaseGreaterThanMax_Error(t *testing.T) {
	cfg := &Config{
		Argus: ArgusConfig{
			RadiusHost:           "argus-app",
			RadiusAuthPort:       1812,
			RadiusAccountingPort: 1813,
			RadiusSharedSecret:   "s",
			DBURL:                "postgres://x",
			DBRefreshInterval:    5 * time.Minute,
		},
		Operators: []OperatorConfig{{Code: "turkcell"}},
		Scenarios: []ScenarioConfig{{
			Name:                   "normal",
			Weight:                 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate:    RateConfig{MaxRadiusRequestsPerSecond: 5},
		Metrics: MetricsConfig{Listen: ":9099"},
		Log:     LogConfig{Level: "info", Format: "console"},
		Reactive: ReactiveDefaults{
			Enabled:                 true,
			RejectBackoffBase:       600 * time.Second,
			RejectBackoffMax:        30 * time.Second,
			RejectMaxRetriesPerHour: 5,
			CoAListener:             CoAListenerConfig{SharedSecret: "s"},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "reject_backoff_base") {
		t.Errorf("expected error containing %q, got: %v", "reject_backoff_base", err)
	}
}

// TestReactive_MaxRetriesZero_DefaultsTo5 verifies that RejectMaxRetriesPerHour=0
// is defaulted to 5 (plan §Config schema), matching the pattern of other
// zero-value fields (margin, backoff).
func TestReactive_MaxRetriesZero_DefaultsTo5(t *testing.T) {
	cfg := &Config{
		Argus: ArgusConfig{
			RadiusHost:           "argus-app",
			RadiusAuthPort:       1812,
			RadiusAccountingPort: 1813,
			RadiusSharedSecret:   "s",
			DBURL:                "postgres://x",
			DBRefreshInterval:    5 * time.Minute,
		},
		Operators: []OperatorConfig{{Code: "turkcell"}},
		Scenarios: []ScenarioConfig{{
			Name:                   "normal",
			Weight:                 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate:    RateConfig{MaxRadiusRequestsPerSecond: 5},
		Metrics: MetricsConfig{Listen: ":9099"},
		Log:     LogConfig{Level: "info", Format: "console"},
		Reactive: ReactiveDefaults{
			Enabled:                 true,
			RejectMaxRetriesPerHour: 0, // should default to 5
			CoAListener:             CoAListenerConfig{SharedSecret: "s"},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected zero-value to default, got error: %v", err)
	}
	if cfg.Reactive.RejectMaxRetriesPerHour != 5 {
		t.Errorf("RejectMaxRetriesPerHour: want default 5, got %d", cfg.Reactive.RejectMaxRetriesPerHour)
	}
}

// TestReactive_MaxRetriesNegative_Error verifies that an explicit negative
// RejectMaxRetriesPerHour is rejected (distinguished from zero → default).
func TestReactive_MaxRetriesNegative_Error(t *testing.T) {
	cfg := &Config{
		Argus: ArgusConfig{
			RadiusHost:           "argus-app",
			RadiusAuthPort:       1812,
			RadiusAccountingPort: 1813,
			RadiusSharedSecret:   "s",
			DBURL:                "postgres://x",
			DBRefreshInterval:    5 * time.Minute,
		},
		Operators: []OperatorConfig{{Code: "turkcell"}},
		Scenarios: []ScenarioConfig{{
			Name:                   "normal",
			Weight:                 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate:    RateConfig{MaxRadiusRequestsPerSecond: 5},
		Metrics: MetricsConfig{Listen: ":9099"},
		Log:     LogConfig{Level: "info", Format: "console"},
		Reactive: ReactiveDefaults{
			Enabled:                 true,
			RejectMaxRetriesPerHour: -1,
			CoAListener:             CoAListenerConfig{SharedSecret: "s"},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "reject_max_retries_per_hour") {
		t.Errorf("expected error containing %q, got: %v", "reject_max_retries_per_hour", err)
	}
}

// TestReactive_CoASecretEmpty_ArgusSecretEmpty_Error verifies that both secrets
// being empty when reactive is enabled produces an error.
func TestReactive_CoASecretEmpty_ArgusSecretEmpty_Error(t *testing.T) {
	cfg := &Config{
		Argus: ArgusConfig{
			RadiusHost:           "argus-app",
			RadiusAuthPort:       1812,
			RadiusAccountingPort: 1813,
			RadiusSharedSecret:   "",
			DBURL:                "postgres://x",
			DBRefreshInterval:    5 * time.Minute,
		},
		Operators: []OperatorConfig{{Code: "turkcell"}},
		Scenarios: []ScenarioConfig{{
			Name:                   "normal",
			Weight:                 1.0,
			SessionDurationSeconds: [2]int{60, 120},
			InterimIntervalSeconds: 30,
		}},
		Rate:    RateConfig{MaxRadiusRequestsPerSecond: 5},
		Metrics: MetricsConfig{Listen: ":9099"},
		Log:     LogConfig{Level: "info", Format: "console"},
		Reactive: ReactiveDefaults{
			Enabled:                 true,
			RejectMaxRetriesPerHour: 5,
		},
	}

	err := cfg.validateReactive()
	if err == nil || !strings.Contains(err.Error(), "shared_secret") {
		t.Errorf("expected error containing %q, got: %v", "shared_secret", err)
	}
}

// TestReactive_EnvOverride_CoASecret verifies that ARGUS_SIM_COA_SECRET env
// overrides the YAML-provided shared_secret.
func TestReactive_EnvOverride_CoASecret(t *testing.T) {
	t.Setenv("ARGUS_SIM_COA_SECRET", "envsecret")

	body := validYAML + `reactive:
  enabled: true
  reject_max_retries_per_hour: 5
  coa_listener:
    shared_secret: yamlsecret
`
	p := writeTemp(t, body)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Reactive.CoAListener.SharedSecret != "envsecret" {
		t.Errorf("CoAListener.SharedSecret: want %q (env wins), got %q", "envsecret", cfg.Reactive.CoAListener.SharedSecret)
	}
}

// TestReactive_OperatorConfigs_ValidWithOrWithoutReactive verifies that a
// RADIUS-only config (no reactive block) is still valid after this story's
// changes, preserving backwards compatibility with STORY-082 configs.
func TestReactive_OperatorConfigs_ValidWithOrWithoutReactive(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"without reactive block", validYAML},
		{"with reactive disabled", validYAML + "reactive:\n  enabled: false\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeTemp(t, c.yaml)
			_, err := Load(p)
			if err != nil {
				t.Errorf("expected valid config, got: %v", err)
			}
		})
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
