package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "operator-sim-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestLoad(t *testing.T) {
	validYAML := `
server:
  listen: ":9595"
  metrics_listen: ":9596"
operators:
  - code: "turkcell"
    display_name: "Turkcell"
  - code: "vodafone_tr"
    display_name: "Vodafone TR"
log:
  level: "debug"
  format: "json"
stubs:
  subscriber_status: "active"
  subscriber_plan: "premium"
  cdr_echo: true
`

	tests := []struct {
		name        string
		yaml        string
		envKey      string
		envVal      string
		wantErr     bool
		errContains string
		check       func(t *testing.T, cfg *Config)
	}{
		{
			name: "happy_path_valid_config",
			yaml: validYAML,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Server.Listen != ":9595" {
					t.Errorf("listen = %q, want :9595", cfg.Server.Listen)
				}
				if len(cfg.Operators) != 2 {
					t.Errorf("operators = %d, want 2", len(cfg.Operators))
				}
				if cfg.Operators[0].Code != "turkcell" {
					t.Errorf("operator[0].code = %q, want turkcell", cfg.Operators[0].Code)
				}
				if cfg.Log.Level != "debug" {
					t.Errorf("log.level = %q, want debug", cfg.Log.Level)
				}
				if cfg.Stubs.SubscriberPlan != "premium" {
					t.Errorf("stubs.subscriber_plan = %q, want premium", cfg.Stubs.SubscriberPlan)
				}
				if !cfg.Stubs.CDREcho {
					t.Error("stubs.cdr_echo should be true")
				}
			},
		},
		{
			name: "defaults_applied_when_fields_omitted",
			yaml: `
server:
  listen: ":9595"
  metrics_listen: ":9596"
operators:
  - code: "op1"
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Server.ReadTimeout != DefaultReadTimeout {
					t.Errorf("read_timeout = %v, want %v", cfg.Server.ReadTimeout, DefaultReadTimeout)
				}
				if cfg.Server.WriteTimeout != DefaultWriteTimeout {
					t.Errorf("write_timeout = %v, want %v", cfg.Server.WriteTimeout, DefaultWriteTimeout)
				}
				if cfg.Log.Level != "info" {
					t.Errorf("log.level = %q, want info", cfg.Log.Level)
				}
				if cfg.Log.Format != "console" {
					t.Errorf("log.format = %q, want console", cfg.Log.Format)
				}
				if cfg.Stubs.SubscriberStatus != "active" {
					t.Errorf("stubs.subscriber_status = %q, want active", cfg.Stubs.SubscriberStatus)
				}
				if cfg.Stubs.SubscriberPlan != "default" {
					t.Errorf("stubs.subscriber_plan = %q, want default", cfg.Stubs.SubscriberPlan)
				}
			},
		},
		{
			name: "missing_operators_array_returns_error",
			yaml: `
server:
  listen: ":9595"
  metrics_listen: ":9596"
`,
			wantErr:     true,
			errContains: "at least one operator required",
		},
		{
			name: "empty_operators_array_returns_error",
			yaml: `
server:
  listen: ":9595"
  metrics_listen: ":9596"
operators: []
`,
			wantErr:     true,
			errContains: "at least one operator required",
		},
		{
			name: "duplicate_operator_codes_returns_error",
			yaml: `
server:
  listen: ":9595"
  metrics_listen: ":9596"
operators:
  - code: "turkcell"
  - code: "turkcell"
`,
			wantErr:     true,
			errContains: "duplicate operator code: turkcell",
		},
		{
			name: "empty_operator_code_returns_error",
			yaml: `
server:
  listen: ":9595"
  metrics_listen: ":9596"
operators:
  - code: ""
    display_name: "No Code"
`,
			wantErr:     true,
			errContains: "operators[0].code required",
		},
		{
			name: "missing_server_listen_returns_error",
			yaml: `
server:
  metrics_listen: ":9596"
operators:
  - code: "op1"
`,
			wantErr:     true,
			errContains: "server.listen required",
		},
		{
			name: "missing_metrics_listen_returns_error",
			yaml: `
server:
  listen: ":9595"
operators:
  - code: "op1"
`,
			wantErr:     true,
			errContains: "server.metrics_listen required",
		},
		{
			name:   "env_override_log_level",
			yaml:   validYAML,
			envKey: "ARGUS_OPERATOR_SIM_LOG_LEVEL",
			envVal: "WARN",
			check: func(t *testing.T, cfg *Config) {
				if cfg.Log.Level != "warn" {
					t.Errorf("log.level = %q, want warn (from env, lowercased)", cfg.Log.Level)
				}
			},
		},
		{
			name: "explicit_timeouts_preserved",
			yaml: `
server:
  listen: ":9595"
  metrics_listen: ":9596"
  read_timeout: 30s
  write_timeout: 60s
operators:
  - code: "op1"
`,
			check: func(t *testing.T, cfg *Config) {
				if cfg.Server.ReadTimeout != 30*time.Second {
					t.Errorf("read_timeout = %v, want 30s", cfg.Server.ReadTimeout)
				}
				if cfg.Server.WriteTimeout != 60*time.Second {
					t.Errorf("write_timeout = %v, want 60s", cfg.Server.WriteTimeout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}
			path := writeYAML(t, tt.yaml)
			cfg, err := Load(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if tt.errContains != "" && !containsStr(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_EnvPathOverride(t *testing.T) {
	yaml := `
server:
  listen: ":9595"
  metrics_listen: ":9596"
operators:
  - code: "op1"
`
	path := writeYAML(t, yaml)
	t.Setenv("ARGUS_OPERATOR_SIM_CONFIG", path)
	cfg, err := Load("/nonexistent/should/be/overridden.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Listen != ":9595" {
		t.Errorf("listen = %q, want :9595", cfg.Server.Listen)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
