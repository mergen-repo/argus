package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedact_SecretsAbsentFromJSON(t *testing.T) {
	tests := []struct {
		field    string
		setValue func(c *Config, v string)
	}{
		{"JWT_SECRET", func(c *Config, v string) { c.JWTSecret = v }},
		{"JWT_SECRET_PREVIOUS", func(c *Config, v string) { c.JWTSecretPrevious = v }},
		{"ENCRYPTION_KEY", func(c *Config, v string) { c.EncryptionKey = v }},
		{"DATABASE_URL", func(c *Config, v string) { c.DatabaseURL = v }},
		{"DATABASE_READ_REPLICA_URL", func(c *Config, v string) { c.DatabaseReadReplicaURL = v }},
		{"REDIS_URL", func(c *Config, v string) { c.RedisURL = v }},
		{"NATS_URL", func(c *Config, v string) { c.NATSURL = v }},
		{"SMTP_PASSWORD", func(c *Config, v string) { c.SMTPPassword = v }},
		{"TELEGRAM_BOT_TOKEN", func(c *Config, v string) { c.TelegramBotToken = v }},
		{"S3_ACCESS_KEY", func(c *Config, v string) { c.S3AccessKey = v }},
		{"S3_SECRET_KEY", func(c *Config, v string) { c.S3SecretKey = v }},
		{"ESIM_SMDP_API_KEY", func(c *Config, v string) { c.ESIMSMDPAPIKey = v }},
		{"ESIM_SMDP_CLIENT_CERT_PATH", func(c *Config, v string) { c.ESIMSMDPClientCert = v }},
		{"ESIM_SMDP_CLIENT_KEY_PATH", func(c *Config, v string) { c.ESIMSMDPClientKey = v }},
		{"SMS_AUTH_TOKEN", func(c *Config, v string) { c.SMSAuthToken = v }},
		{"RADIUS_SECRET", func(c *Config, v string) { c.RadiusSecret = v }},
		{"PPROF_TOKEN", func(c *Config, v string) { c.PprofToken = v }},
		{"TLS_CERT_PATH", func(c *Config, v string) { c.TLSCertPath = v }},
		{"TLS_KEY_PATH", func(c *Config, v string) { c.TLSKeyPath = v }},
		{"RADSEC_CERT_PATH", func(c *Config, v string) { c.RadSecCertPath = v }},
		{"RADSEC_KEY_PATH", func(c *Config, v string) { c.RadSecKeyPath = v }},
		{"RADSEC_CA_PATH", func(c *Config, v string) { c.RadSecCAPath = v }},
		{"DIAMETER_TLS_CERT_PATH", func(c *Config, v string) { c.DiameterTLSCert = v }},
		{"DIAMETER_TLS_KEY_PATH", func(c *Config, v string) { c.DiameterTLSKey = v }},
		{"DIAMETER_TLS_CA_PATH", func(c *Config, v string) { c.DiameterTLSCA = v }},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			sentinel := "DETECT_LEAK_" + tt.field

			cfg := &Config{AppEnv: "development"}
			tt.setValue(cfg, sentinel)

			redacted := cfg.Redact()

			out, err := json.Marshal(redacted)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if strings.Contains(string(out), sentinel) {
				t.Errorf("secret field %s leaked into redacted JSON: %s", tt.field, string(out))
			}
		})
	}
}

func TestRedact_SafeFieldsPresent(t *testing.T) {
	cfg := &Config{
		AppEnv:                    "production",
		DeploymentMode:            "cluster",
		SBAEnabled:                true,
		TLSEnabled:                true,
		BackupEnabled:             true,
		MetricsEnabled:            true,
		RateLimitEnabled:          true,
		SecurityHeaders:           true,
		CronEnabled:               true,
		PprofEnabled:              false,
		RadiusAuthPort:            1812,
		RadiusAcctPort:            1813,
		RadiusCoAPort:             3799,
		DiameterPort:              3868,
		DiameterTLSEnabled:        false,
		SBAPort:                   8443,
		RadSecPort:                2083,
		RateLimitPerMinute:        1000,
		RequestBodyMaxMB:          10,
		DefaultMaxSIMs:            1000000,
		JobMaxConcurrentPerTenant: 5,
		DefaultPurgeRetentionDays: 90,
		DefaultAuditRetentionDays: 365,
		DefaultCDRRetentionDays:   180,
	}

	r := cfg.Redact()

	if r.AppEnv != "production" {
		t.Errorf("app_env = %q, want production", r.AppEnv)
	}
	if r.DeploymentMode != "cluster" {
		t.Errorf("deployment_mode = %q, want cluster", r.DeploymentMode)
	}
	if !r.FeatureFlags.SBAEnabled {
		t.Error("feature_flags.sba_enabled should be true")
	}
	if !r.FeatureFlags.TLSEnabled {
		t.Error("feature_flags.tls_enabled should be true")
	}
	if r.Protocols.RadiusAuthPort != 1812 {
		t.Errorf("protocols.radius_auth_port = %d, want 1812", r.Protocols.RadiusAuthPort)
	}
	if r.Protocols.DiameterPort != 3868 {
		t.Errorf("protocols.diameter_port = %d, want 3868", r.Protocols.DiameterPort)
	}
	if r.Limits.RateLimitPerMinute != 1000 {
		t.Errorf("limits.rate_limit_per_minute = %d, want 1000", r.Limits.RateLimitPerMinute)
	}
	if r.Limits.DefaultMaxSIMs != 1000000 {
		t.Errorf("limits.default_max_sims = %d, want 1000000", r.Limits.DefaultMaxSIMs)
	}
	if r.Retention.PurgeDays != 90 {
		t.Errorf("retention.purge_days = %d, want 90", r.Retention.PurgeDays)
	}
	if r.Retention.AuditDays != 365 {
		t.Errorf("retention.audit_days = %d, want 365", r.Retention.AuditDays)
	}
	if len(r.SecretsRedacted) < 14 {
		t.Errorf("secrets_redacted len = %d, want >= 14", len(r.SecretsRedacted))
	}
}
