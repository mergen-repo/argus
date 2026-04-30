package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultReadTimeout  = 5 * time.Second
	DefaultWriteTimeout = 10 * time.Second
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Operators []OperatorEntry `yaml:"operators"`
	Log       LogConfig       `yaml:"log"`
	Stubs     StubsConfig     `yaml:"stubs"`
}

type ServerConfig struct {
	Listen        string        `yaml:"listen"`
	MetricsListen string        `yaml:"metrics_listen"`
	ReadTimeout   time.Duration `yaml:"read_timeout"`
	WriteTimeout  time.Duration `yaml:"write_timeout"`
}

type OperatorEntry struct {
	Code        string `yaml:"code"`
	DisplayName string `yaml:"display_name"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type StubsConfig struct {
	SubscriberStatus string `yaml:"subscriber_status"`
	SubscriberPlan   string `yaml:"subscriber_plan"`
	CDREcho          bool   `yaml:"cdr_echo"`
}

func Load(path string) (*Config, error) {
	if v := os.Getenv("ARGUS_OPERATOR_SIM_CONFIG"); v != "" {
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
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("ARGUS_OPERATOR_SIM_LOG_LEVEL"); v != "" {
		c.Log.Level = strings.ToLower(v)
	}
}

func (c *Config) applyDefaults() {
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = DefaultReadTimeout
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = DefaultWriteTimeout
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "console"
	}
	if c.Stubs.SubscriberStatus == "" {
		c.Stubs.SubscriberStatus = "active"
	}
	if c.Stubs.SubscriberPlan == "" {
		c.Stubs.SubscriberPlan = "default"
	}
}

func (c *Config) Validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen required")
	}
	if c.Server.MetricsListen == "" {
		return fmt.Errorf("server.metrics_listen required")
	}
	if len(c.Operators) == 0 {
		return fmt.Errorf("at least one operator required")
	}
	seen := make(map[string]struct{}, len(c.Operators))
	for i, op := range c.Operators {
		if op.Code == "" {
			return fmt.Errorf("operators[%d].code required", i)
		}
		if _, exists := seen[op.Code]; exists {
			return fmt.Errorf("duplicate operator code: %s", op.Code)
		}
		seen[op.Code] = struct{}{}
	}
	return nil
}
