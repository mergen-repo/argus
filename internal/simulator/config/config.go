// Package config loads and validates the simulator's YAML configuration.
// Env vars override a subset of fields (DB URL, log level, config path,
// SIMULATOR_ENABLED guard) so operators can point a single container at
// different environments without rebaking images.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
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
	Diameter  DiameterDefaults `yaml:"diameter"`
	SBA       SBADefaults      `yaml:"sba"`
	Reactive  ReactiveDefaults `yaml:"reactive"`
}

// ReactiveDefaults holds global reactive-behavior defaults for the simulator.
// All fields are applied only when Enabled is true; a disabled reactive block
// is a zero-value no-op (backwards compatible with STORY-082/083 configs).
//
// SessionTimeoutRespect is a bare bool that defaults to true when Enabled is
// true. YAML cannot distinguish "omitted" from "explicit false" for bare bool,
// so set session_timeout_respect: false explicitly to disable it.
type ReactiveDefaults struct {
	Enabled                 bool              `yaml:"enabled"`
	SessionTimeoutRespect   bool              `yaml:"session_timeout_respect"`
	EarlyTerminationMargin  time.Duration     `yaml:"early_termination_margin"`
	RejectBackoffBase       time.Duration     `yaml:"reject_backoff_base"`
	RejectBackoffMax        time.Duration     `yaml:"reject_backoff_max"`
	RejectMaxRetriesPerHour int               `yaml:"reject_max_retries_per_hour"`
	CoAListener             CoAListenerConfig `yaml:"coa_listener"`
}

// CoAListenerConfig configures the CoA/DM listener on the simulator.
// SharedSecret defaults to inheriting argus.radius_shared_secret when empty.
type CoAListenerConfig struct {
	Enabled      bool   `yaml:"enabled"`
	ListenAddr   string `yaml:"listen_addr"`   // default "0.0.0.0:3799"
	SharedSecret string `yaml:"shared_secret"` // empty → inherit argus.radius_shared_secret
}

// DiameterDefaults holds global Diameter peer defaults applied to all
// operators that opt in. Per-operator overrides sit on OperatorDiameterConfig.
type DiameterDefaults struct {
	Host                string        `yaml:"host"`
	Port                int           `yaml:"port"`
	OriginRealm         string        `yaml:"origin_realm"`
	DestinationRealm    string        `yaml:"destination_realm"`
	WatchdogInterval    time.Duration `yaml:"watchdog_interval"`
	ConnectTimeout      time.Duration `yaml:"connect_timeout"`
	RequestTimeout      time.Duration `yaml:"request_timeout"`
	ReconnectBackoffMin time.Duration `yaml:"reconnect_backoff_min"`
	ReconnectBackoffMax time.Duration `yaml:"reconnect_backoff_max"`
}

// SBADefaults holds global 5G SBA client defaults applied to all
// operators that opt in. Per-operator overrides sit on OperatorSBAConfig.
//
// ProdGuard default is true (guard ON). When ARGUS_SIM_ENV=prod AND
// ProdGuard is true AND TLSSkipVerify is true, Validate rejects the config.
// Operators who need to disable the guard in exceptional cases (e.g.
// hand-crafted prod-like fixtures) must set `prod_guard: false` explicitly.
type SBADefaults struct {
	Host                 string        `yaml:"host"`
	Port                 int           `yaml:"port"`
	TLSEnabled           bool          `yaml:"tls_enabled"`
	TLSSkipVerify        bool          `yaml:"tls_skip_verify"`
	ServingNetworkName   string        `yaml:"serving_network_name"`
	RequestTimeout       time.Duration `yaml:"request_timeout"`
	AMFInstanceID        string        `yaml:"amf_instance_id"`
	DeregCallbackURI     string        `yaml:"dereg_callback_uri"`
	IncludeOptionalCalls bool          `yaml:"include_optional_calls"`
	ProdGuard            *bool         `yaml:"prod_guard"` // default true; pointer so unset != explicit false
}

// OperatorSBAConfig is the per-operator 5G SBA opt-in block.
// A nil pointer on OperatorConfig means SBA is disabled for that operator.
type OperatorSBAConfig struct {
	Enabled    bool          `yaml:"enabled"`
	AuthMethod string        `yaml:"auth_method"`
	Rate       float64       `yaml:"rate"`
	Slices     []SliceConfig `yaml:"slices,omitempty"`
}

// SliceConfig describes one S-NSSAI entry advertised in RequestedNSSAI on
// 5G-AKA authentication requests. When OperatorSBAConfig.Slices is empty and
// the operator opts in, validateSBA applies a default of [{SST:1, SD:"000001"}].
type SliceConfig struct {
	SST int    `yaml:"sst"`
	SD  string `yaml:"sd,omitempty"`
}

// OperatorDiameterConfig is the per-operator Diameter opt-in block.
// A nil pointer on OperatorConfig means Diameter is disabled for that operator.
type OperatorDiameterConfig struct {
	Enabled      bool     `yaml:"enabled"`
	OriginHost   string   `yaml:"origin_host"`
	Applications []string `yaml:"applications"`
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
	Code          string                  `yaml:"code"`
	NASIdentifier string                  `yaml:"nas_identifier"`
	NASIP         string                  `yaml:"nas_ip"`
	Diameter      *OperatorDiameterConfig `yaml:"diameter,omitempty"`
	SBA           *OperatorSBAConfig      `yaml:"sba,omitempty"`
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
	if v := os.Getenv("ARGUS_SIM_COA_SECRET"); v != "" {
		c.Reactive.CoAListener.SharedSecret = v
	}

	// AC-9 env knobs — applied after YAML is parsed so YAML defaults remain
	// the fallback when env is unset. Validate() runs after this function and
	// enforces range constraints on every mutated field.

	if v := os.Getenv("ARGUS_SIM_SESSION_RATE_PER_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Rate.MaxRadiusRequestsPerSecond = n
		}
	}

	// ARGUS_SIM_DIAMETER_ENABLED=false disables Diameter for all operators.
	// Per-operator enabled:true is respected only when this global toggle is true.
	if v := os.Getenv("ARGUS_SIM_DIAMETER_ENABLED"); strings.EqualFold(v, "false") {
		for i := range c.Operators {
			c.Operators[i].Diameter = nil
		}
	}

	// ARGUS_SIM_SBA_ENABLED=false disables 5G SBA for all operators globally.
	if v := os.Getenv("ARGUS_SIM_SBA_ENABLED"); strings.EqualFold(v, "false") {
		for i := range c.Operators {
			c.Operators[i].SBA = nil
		}
	}

	// ARGUS_SIM_INTERIM_INTERVAL_SEC, when >0, overrides interim_interval_seconds
	// for every scenario. Enables 30s cadence demo without editing YAML.
	if v := os.Getenv("ARGUS_SIM_INTERIM_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			for i := range c.Scenarios {
				c.Scenarios[i].InterimIntervalSeconds = n
			}
		}
	}

	// ARGUS_SIM_VIOLATION_RATE_PCT (0-100 float) rescales aggressive_m2m scenario
	// weight proportionally by reducing normal_browsing weight to compensate.
	// When aggressive_m2m is absent, the env has no effect (safe no-op).
	if v := os.Getenv("ARGUS_SIM_VIOLATION_RATE_PCT"); v != "" {
		if pct, err := strconv.ParseFloat(v, 64); err == nil {
			c.applyViolationRatePct(pct)
		}
	}
}

// applyViolationRatePct rescales the aggressive_m2m scenario weight from its
// YAML default to the requested pct (0-100). It proportionally reduces the
// normal_browsing scenario weight to keep the total at ~1.0. When either
// scenario is absent from the config, the function is a safe no-op so custom
// YAML configs without the aggressive_m2m scenario are unaffected.
func (c *Config) applyViolationRatePct(pct float64) {
	if pct < 0 || pct > 100 {
		return
	}
	newWeight := pct / 100.0

	aggressiveIdx := -1
	normalIdx := -1
	for i, s := range c.Scenarios {
		if s.Name == "aggressive_m2m" {
			aggressiveIdx = i
		}
		if s.Name == "normal_browsing" {
			normalIdx = i
		}
	}
	if aggressiveIdx < 0 || normalIdx < 0 {
		return
	}

	oldWeight := c.Scenarios[aggressiveIdx].Weight
	delta := newWeight - oldWeight
	c.Scenarios[aggressiveIdx].Weight = newWeight
	c.Scenarios[normalIdx].Weight -= delta
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
		return fmt.Errorf("rate.max_radius_requests_per_second must be > 0 (got %d; set ARGUS_SIM_SESSION_RATE_PER_SEC to override)", c.Rate.MaxRadiusRequestsPerSecond)
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

	if err := c.validateDiameter(); err != nil {
		return err
	}
	if err := c.validateSBA(); err != nil {
		return err
	}
	if err := c.validateReactive(); err != nil {
		return err
	}
	return nil
}

// validateDiameter applies Diameter defaults and enforces Diameter invariants.
// Called after all structural RADIUS/scenario validation.
func (c *Config) validateDiameter() error {
	d := &c.Diameter

	if d.Host == "" {
		d.Host = "argus-app"
	}
	if d.Port == 0 {
		d.Port = 3868
	}
	if d.OriginRealm == "" {
		d.OriginRealm = "sim.argus.test"
	}
	if d.WatchdogInterval == 0 {
		d.WatchdogInterval = 30 * time.Second
	}
	if d.ConnectTimeout == 0 {
		d.ConnectTimeout = 5 * time.Second
	}
	if d.RequestTimeout == 0 {
		d.RequestTimeout = 5 * time.Second
	}
	if d.ReconnectBackoffMin == 0 {
		d.ReconnectBackoffMin = 1 * time.Second
	}
	if d.ReconnectBackoffMax == 0 {
		d.ReconnectBackoffMax = 30 * time.Second
	}

	validApps := map[string]bool{"gx": true, "gy": true}
	anyEnabled := false

	for i := range c.Operators {
		op := &c.Operators[i]
		if op.Diameter == nil || !op.Diameter.Enabled {
			continue
		}
		anyEnabled = true

		if len(op.Diameter.Applications) == 0 {
			op.Diameter.Applications = []string{"gx", "gy"}
		}
		for _, app := range op.Diameter.Applications {
			if !validApps[strings.ToLower(app)] {
				return fmt.Errorf("operators[%s].diameter.applications: unknown app %q (must be gx or gy)", op.Code, app)
			}
		}

		if op.Diameter.OriginHost == "" {
			op.Diameter.OriginHost = "sim-" + toKebab(op.Code) + "." + d.OriginRealm
		}
	}

	if anyEnabled && d.DestinationRealm == "" {
		return fmt.Errorf("diameter.destination_realm required when any operator has diameter.enabled: true")
	}
	return nil
}

// validateSBA applies SBA defaults and enforces SBA invariants.
// Called after validateDiameter.
func (c *Config) validateSBA() error {
	s := &c.SBA

	if s.Host == "" {
		s.Host = "argus-app"
	}
	if s.Port == 0 {
		s.Port = 8443
	}
	if s.ServingNetworkName == "" {
		s.ServingNetworkName = "5G:mnc001.mcc286.3gppnetwork.org"
	}
	if s.RequestTimeout == 0 {
		s.RequestTimeout = 5 * time.Second
	}
	if s.AMFInstanceID == "" {
		s.AMFInstanceID = "sim-amf-01"
	}
	if s.DeregCallbackURI == "" {
		s.DeregCallbackURI = "http://sim-amf.invalid/dereg"
	}
	if s.ProdGuard == nil {
		// default guard ON
		t := true
		s.ProdGuard = &t
	}

	// 5G_AKA is the only implementable method in STORY-084 (plan §Config:
	// EAP_AKA_PRIME reserved, rejected for this story). EAP_AKA_PRIME remains
	// listed in the enum so future stories can flip it without a schema change.
	validAuthMethods := map[string]bool{"5G_AKA": true}
	anyEnabled := false

	for i := range c.Operators {
		op := &c.Operators[i]
		if op.SBA == nil || !op.SBA.Enabled {
			continue
		}
		anyEnabled = true

		if op.SBA.AuthMethod == "" {
			op.SBA.AuthMethod = "5G_AKA"
		}
		if !validAuthMethods[op.SBA.AuthMethod] {
			return fmt.Errorf("operators[%s].sba.auth_method: unknown method %q (only 5G_AKA implemented in STORY-084)", op.Code, op.SBA.AuthMethod)
		}
		if op.SBA.Rate < 0 || op.SBA.Rate > 1 {
			return fmt.Errorf("operators[%s].sba.rate out of range: %v (must be in [0, 1])", op.Code, op.SBA.Rate)
		}
		// Default slices when operator opts in but leaves Slices unset.
		if len(op.SBA.Slices) == 0 {
			op.SBA.Slices = []SliceConfig{{SST: 1, SD: "000001"}}
		}
	}

	if anyEnabled && s.TLSSkipVerify && *s.ProdGuard {
		if os.Getenv("ARGUS_SIM_ENV") == "prod" {
			return fmt.Errorf("sba.tls_skip_verify: true is not allowed when ARGUS_SIM_ENV=prod (disable prod guard with sba.prod_guard: false only for exceptional cases)")
		}
	}

	return nil
}

// validateReactive applies reactive-behavior defaults and enforces invariants.
// Called after validateSBA. Zero-value skip: if Reactive.Enabled is false the
// function returns nil immediately without mutating any field, preserving
// backwards compatibility with STORY-082/083 configs.
func (c *Config) validateReactive() error {
	r := &c.Reactive

	if !r.Enabled {
		return nil
	}

	// SessionTimeoutRespect defaults to true when Enabled; bare bool means
	// YAML "omitted" and "false" are indistinguishable, so default-on-when-enabled.
	r.SessionTimeoutRespect = true

	if r.EarlyTerminationMargin == 0 {
		r.EarlyTerminationMargin = 5 * time.Second
	}
	if r.RejectBackoffBase == 0 {
		r.RejectBackoffBase = 30 * time.Second
	}
	if r.RejectBackoffMax == 0 {
		r.RejectBackoffMax = 600 * time.Second
	}
	if r.CoAListener.ListenAddr == "" {
		r.CoAListener.ListenAddr = "0.0.0.0:3799"
	}

	if r.CoAListener.SharedSecret == "" {
		if c.Argus.RadiusSharedSecret != "" {
			r.CoAListener.SharedSecret = c.Argus.RadiusSharedSecret
		} else {
			return fmt.Errorf("reactive.coa_listener.shared_secret is empty and argus.radius_shared_secret is also empty")
		}
	}

	if r.RejectBackoffBase > r.RejectBackoffMax {
		return fmt.Errorf("reject_backoff_base must be <= reject_backoff_max")
	}
	// Plan §Config schema: RejectMaxRetriesPerHour defaults to 5 when unset.
	if r.RejectMaxRetriesPerHour == 0 {
		r.RejectMaxRetriesPerHour = 5
	} else if r.RejectMaxRetriesPerHour < 0 {
		return fmt.Errorf("reject_max_retries_per_hour must be >= 0 (0 → default 5)")
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

var kebabReplace = regexp.MustCompile(`[^a-z0-9]+`)

// toKebab converts an operator code to a DNS-safe kebab-case label.
// e.g. "Turk_Cell 01" → "turk-cell-01"
func toKebab(s string) string {
	s = strings.ToLower(s)
	s = kebabReplace.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
