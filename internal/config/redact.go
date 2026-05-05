package config

type RedactedFeatureFlags struct {
	SBAEnabled             bool `json:"sba_enabled"`
	TLSEnabled             bool `json:"tls_enabled"`
	BackupEnabled          bool `json:"backup_enabled"`
	MetricsEnabled         bool `json:"metrics_enabled"`
	RateLimitEnabled       bool `json:"rate_limit_enabled"`
	SecurityHeadersEnabled bool `json:"security_headers_enabled"`
	CronEnabled            bool `json:"cron_enabled"`
	PprofEnabled           bool `json:"pprof_enabled"`
}

type RedactedProtocols struct {
	RadiusAuthPort     int  `json:"radius_auth_port"`
	RadiusAcctPort     int  `json:"radius_acct_port"`
	RadiusCoAPort      int  `json:"radius_coa_port"`
	DiameterPort       int  `json:"diameter_port"`
	DiameterTLSEnabled bool `json:"diameter_tls_enabled"`
	SBAPort            int  `json:"sba_port"`
	SBAEnabled         bool `json:"sba_enabled"`
	RadSecPort         int  `json:"radsec_port"`
}

type RedactedLimits struct {
	RateLimitPerMinute        int `json:"rate_limit_per_minute"`
	RequestBodyMaxMB          int `json:"request_body_max_mb"`
	DefaultMaxSIMs            int `json:"default_max_sims"`
	JobMaxConcurrentPerTenant int `json:"job_max_concurrent_per_tenant"`
}

type RedactedRetention struct {
	PurgeDays int `json:"purge_days"`
	AuditDays int `json:"audit_days"`
	CDRDays   int `json:"cdr_days"`
}

type RedactedConfig struct {
	AppEnv         string `json:"app_env"`
	DeploymentMode string `json:"deployment_mode"`

	FeatureFlags RedactedFeatureFlags `json:"feature_flags"`
	Protocols    RedactedProtocols    `json:"protocols"`
	Limits       RedactedLimits       `json:"limits"`
	Retention    RedactedRetention    `json:"retention"`

	SecretsRedacted []string `json:"secrets_redacted"`
}

func (c *Config) Redact() RedactedConfig {
	return RedactedConfig{
		AppEnv:         c.AppEnv,
		DeploymentMode: c.DeploymentMode,
		FeatureFlags: RedactedFeatureFlags{
			SBAEnabled:             c.SBAEnabled,
			TLSEnabled:             c.TLSEnabled,
			BackupEnabled:          c.BackupEnabled,
			MetricsEnabled:         c.MetricsEnabled,
			RateLimitEnabled:       c.RateLimitEnabled,
			SecurityHeadersEnabled: c.SecurityHeaders,
			CronEnabled:            c.CronEnabled,
			PprofEnabled:           c.PprofEnabled,
		},
		Protocols: RedactedProtocols{
			RadiusAuthPort:     c.RadiusAuthPort,
			RadiusAcctPort:     c.RadiusAcctPort,
			RadiusCoAPort:      c.RadiusCoAPort,
			DiameterPort:       c.DiameterPort,
			DiameterTLSEnabled: c.DiameterTLSEnabled,
			SBAPort:            c.SBAPort,
			SBAEnabled:         c.SBAEnabled,
			RadSecPort:         c.RadSecPort,
		},
		Limits: RedactedLimits{
			RateLimitPerMinute:        c.RateLimitPerMinute,
			RequestBodyMaxMB:          c.RequestBodyMaxMB,
			DefaultMaxSIMs:            c.DefaultMaxSIMs,
			JobMaxConcurrentPerTenant: c.JobMaxConcurrentPerTenant,
		},
		Retention: RedactedRetention{
			PurgeDays: c.DefaultPurgeRetentionDays,
			AuditDays: c.DefaultAuditRetentionDays,
			CDRDays:   c.DefaultCDRRetentionDays,
		},
		SecretsRedacted: []string{
			"JWT_SECRET",
			"JWT_SECRET_PREVIOUS",
			"ENCRYPTION_KEY",
			"DATABASE_URL",
			"DATABASE_READ_REPLICA_URL",
			"REDIS_URL",
			"NATS_URL",
			"SMTP_PASSWORD",
			"TELEGRAM_BOT_TOKEN",
			"S3_ACCESS_KEY",
			"S3_SECRET_KEY",
			"ESIM_SMDP_API_KEY",
			"ESIM_SMDP_CLIENT_CERT_PATH",
			"ESIM_SMDP_CLIENT_KEY_PATH",
			"SMS_AUTH_TOKEN",
			"RADIUS_SECRET",
			"PPROF_TOKEN",
			"TLS_CERT_PATH",
			"TLS_KEY_PATH",
			"RADSEC_CERT_PATH",
			"RADSEC_KEY_PATH",
			"RADSEC_CA_PATH",
			"DIAMETER_TLS_CERT_PATH",
			"DIAMETER_TLS_KEY_PATH",
			"DIAMETER_TLS_CA_PATH",
		},
	}
}
