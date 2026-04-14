// Package config loads and validates the simulator's YAML configuration.
// Env vars override a subset of fields (DB URL, log level, config path,
// SIMULATOR_ENABLED guard) so operators can point a single container at
// different environments without rebaking images.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the root YAML shape. Fields without yaml tags use field names.
type Config struct {
	Argus     ArgusConfig      `yaml:"argus"`
	Operators []OperatorConfig `yaml:"operators"`
	Scenarios []ScenarioConfig `yaml:"scenarios"`
	Rate      RateConfig       `yaml:"rate"`
	Metrics   MetricsConfig    `yaml:"metrics"`
	Log       LogConfig        `yaml:"log"`
}

type ArgusConfig struct {
	RadiusHost           string        `yaml:"radius_host"`
	RadiusAuthPort       int           `yaml:"radius_auth_port"`
	RadiusAccountingPort int           `yaml:"radius_accounting_port"`
	RadiusSharedSecret   string        `yaml:"radius_shared_secret"`
	DBURL                string        `yaml:"db_url"`
	DBRefreshInterval    time.Duration `yaml:"db_refresh_interval"`
}

type OperatorConfig struct {
	Code          string `yaml:"code"`
	NASIdentifier string `yaml:"nas_identifier"`
	NASIP         string `yaml:"nas_ip"`
}

type ScenarioConfig struct {
	Name                    string  `yaml:"name"`
	Weight                  float64 `yaml:"weight"`
	SessionDurationSeconds  [2]int  `yaml:"session_duration_seconds"`
	InterimIntervalSeconds  int     `yaml:"interim_interval_seconds"`
	BytesPerInterimInRange  [2]int  `yaml:"bytes_per_interim_in"`
	BytesPerInterimOutRange [2]int  `yaml:"bytes_per_interim_out"`
}

type RateConfig struct {
	MaxRadiusRequestsPerSecond int    `yaml:"max_radius_requests_per_second"`
	InitialJitterSeconds       [2]int `yaml:"initial_jitter_seconds"`
}

type MetricsConfig struct {
	Listen string `yaml:"listen"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads, parses, applies env overrides, and validates.
func Load(path string) (*Config, error) {
	if v := os.Getenv("ARGUS_SIM_CONFIG"); v != "" {
		path = v
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	cfg.applyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("ARGUS_SIM_DB_URL"); v != "" {
		c.Argus.DBURL = v
	}
	if v := os.Getenv("ARGUS_SIM_RADIUS_SECRET"); v != "" {
		c.Argus.RadiusSharedSecret = v
	}
	if v := os.Getenv("ARGUS_SIM_RADIUS_HOST"); v != "" {
		c.Argus.RadiusHost = v
	}
	if v := os.Getenv("ARGUS_SIM_LOG_LEVEL"); v != "" {
		c.Log.Level = strings.ToLower(v)
	}
}

// Validate enforces the invariants the engine assumes.
func (c *Config) Validate() error {
	if c.Argus.RadiusHost == "" {
		return fmt.Errorf("argus.radius_host required")
	}
	if c.Argus.RadiusAuthPort <= 0 || c.Argus.RadiusAuthPort > 65535 {
		return fmt.Errorf("argus.radius_auth_port out of range: %d", c.Argus.RadiusAuthPort)
	}
	if c.Argus.RadiusAccountingPort <= 0 || c.Argus.RadiusAccountingPort > 65535 {
		return fmt.Errorf("argus.radius_accounting_port out of range: %d", c.Argus.RadiusAccountingPort)
	}
	if c.Argus.RadiusSharedSecret == "" {
		return fmt.Errorf("argus.radius_shared_secret required (matches Argus RADIUS_SECRET env)")
	}
	if c.Argus.DBURL == "" {
		return fmt.Errorf("argus.db_url required")
	}
	if c.Argus.DBRefreshInterval == 0 {
		c.Argus.DBRefreshInterval = 5 * time.Minute
	}
	if len(c.Operators) == 0 {
		return fmt.Errorf("at least one operator required")
	}
	for i, op := range c.Operators {
		if op.Code == "" {
			return fmt.Errorf("operators[%d].code required", i)
		}
	}
	if len(c.Scenarios) == 0 {
		return fmt.Errorf("at least one scenario required")
	}
	weightSum := 0.0
	for i, s := range c.Scenarios {
		if s.Name == "" {
			return fmt.Errorf("scenarios[%d].name required", i)
		}
		if s.Weight <= 0 {
			return fmt.Errorf("scenarios[%d].weight must be > 0", i)
		}
		if s.SessionDurationSeconds[0] <= 0 || s.SessionDurationSeconds[1] < s.SessionDurationSeconds[0] {
			return fmt.Errorf("scenarios[%d].session_duration_seconds invalid: %v", i, s.SessionDurationSeconds)
		}
		if s.InterimIntervalSeconds <= 0 {
			return fmt.Errorf("scenarios[%d].interim_interval_seconds must be > 0", i)
		}
		weightSum += s.Weight
	}
	if weightSum < 0.99 || weightSum > 1.01 {
		return fmt.Errorf("scenario weights must sum to ~1.0, got %.3f", weightSum)
	}
	if c.Rate.MaxRadiusRequestsPerSecond <= 0 {
		c.Rate.MaxRadiusRequestsPerSecond = 5
	}
	if c.Metrics.Listen == "" {
		c.Metrics.Listen = ":9099"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "console"
	}
	return nil
}

// OperatorByCode is a convenience lookup used by the engine when binding
// a discovered SIM to its operator config (NAS-IP, NAS-Identifier).
func (c *Config) OperatorByCode(code string) *OperatorConfig {
	for i := range c.Operators {
		if c.Operators[i].Code == code {
			return &c.Operators[i]
		}
	}
	return nil
}
