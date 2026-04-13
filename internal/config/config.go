package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

type Config struct {
	AppEnv         string `envconfig:"APP_ENV" default:"development"`
	AppPort        int    `envconfig:"APP_PORT" default:"8080"`
	WSPort         int    `envconfig:"WS_PORT" default:"8081"`
	LogLevel       string `envconfig:"LOG_LEVEL" default:"info"`
	DeploymentMode string `envconfig:"DEPLOYMENT_MODE" default:"single"`

	DatabaseURL            string        `envconfig:"DATABASE_URL" required:"true"`
	DatabaseMaxConns       int32         `envconfig:"DATABASE_MAX_CONNS" default:"50"`
	DatabaseMaxIdleConns   int32         `envconfig:"DATABASE_MAX_IDLE_CONNS" default:"25"`
	DatabaseConnMaxLife    time.Duration `envconfig:"DATABASE_CONN_MAX_LIFETIME" default:"30m"`
	DatabaseReadReplicaURL string        `envconfig:"DATABASE_READ_REPLICA_URL"`

	RedisURL          string `envconfig:"REDIS_URL" required:"true"`
	RedisMaxConns     int    `envconfig:"REDIS_MAX_CONNS" default:"100"`
	RedisReadTimeout  time.Duration `envconfig:"REDIS_READ_TIMEOUT" default:"3s"`
	RedisWriteTimeout time.Duration `envconfig:"REDIS_WRITE_TIMEOUT" default:"3s"`

	NATSURL          string        `envconfig:"NATS_URL" required:"true"`
	NATSClusterID    string        `envconfig:"NATS_CLUSTER_ID" default:"argus-cluster"`
	NATSMaxReconnect int           `envconfig:"NATS_MAX_RECONNECT" default:"60"`
	NATSReconnectWait time.Duration `envconfig:"NATS_RECONNECT_WAIT" default:"2s"`

	JWTSecret              string        `envconfig:"JWT_SECRET" required:"true"`
	JWTExpiry              time.Duration `envconfig:"JWT_EXPIRY" default:"15m"`
	JWTRefreshExpiry       time.Duration `envconfig:"JWT_REFRESH_EXPIRY" default:"168h"`
	JWTRememberMeExpiry    time.Duration `envconfig:"AUTH_JWT_REMEMBER_ME_TTL" default:"168h"`
	JWTIssuer              string        `envconfig:"JWT_ISSUER" default:"argus"`
	BcryptCost       int           `envconfig:"BCRYPT_COST" default:"12"`
	LoginMaxAttempts int           `envconfig:"LOGIN_MAX_ATTEMPTS" default:"5"`
	LoginLockoutDur  time.Duration `envconfig:"LOGIN_LOCKOUT_DURATION" default:"15m"`

	// Password policy (STORY-068)
	PasswordMinLength     int `envconfig:"PASSWORD_MIN_LENGTH"      default:"12"`
	PasswordRequireUpper  bool `envconfig:"PASSWORD_REQUIRE_UPPER"   default:"true"`
	PasswordRequireLower  bool `envconfig:"PASSWORD_REQUIRE_LOWER"   default:"true"`
	PasswordRequireDigit  bool `envconfig:"PASSWORD_REQUIRE_DIGIT"   default:"true"`
	PasswordRequireSymbol bool `envconfig:"PASSWORD_REQUIRE_SYMBOL"  default:"true"`
	PasswordMaxRepeating  int  `envconfig:"PASSWORD_MAX_REPEATING"   default:"3"`
	PasswordHistoryCount  int  `envconfig:"PASSWORD_HISTORY_COUNT"   default:"5"`
	PasswordMaxAgeDays    int  `envconfig:"PASSWORD_MAX_AGE_DAYS"    default:"0"`

	RadiusAuthPort       int    `envconfig:"RADIUS_AUTH_PORT" default:"1812"`
	RadiusAcctPort       int    `envconfig:"RADIUS_ACCT_PORT" default:"1813"`
	RadiusSecret         string `envconfig:"RADIUS_SECRET"`
	RadiusWorkerPoolSize int    `envconfig:"RADIUS_WORKER_POOL_SIZE" default:"256"`
	RadiusCoAPort        int    `envconfig:"RADIUS_COA_PORT" default:"3799"`
	DiameterPort         int    `envconfig:"DIAMETER_PORT" default:"3868"`
	DiameterOriginHost   string `envconfig:"DIAMETER_ORIGIN_HOST"`
	DiameterOriginRealm  string `envconfig:"DIAMETER_ORIGIN_REALM"`
	SBAPort              int    `envconfig:"SBA_PORT" default:"8443"`
	SBAEnabled           bool   `envconfig:"SBA_ENABLED" default:"false"`
	SBAEnableMTLS        bool   `envconfig:"SBA_ENABLE_MTLS" default:"false"`

	RateLimitPerMinute          int    `envconfig:"RATE_LIMIT_DEFAULT_PER_MINUTE" default:"1000"`
	RateLimitPerHour            int    `envconfig:"RATE_LIMIT_DEFAULT_PER_HOUR" default:"30000"`
	RateLimitAlgorithm          string `envconfig:"RATE_LIMIT_ALGORITHM" default:"sliding_window"`
	RateLimitAuthPerMin         int    `envconfig:"RATE_LIMIT_AUTH_PER_MINUTE" default:"10"`
	RateLimitEnabled            bool   `envconfig:"RATE_LIMIT_ENABLED" default:"true"`
	NotificationRateLimitPerMin int    `envconfig:"NOTIFICATION_RATE_LIMIT_PER_MINUTE" default:"60"`

	SMTPHost     string `envconfig:"SMTP_HOST"`
	SMTPPort     int    `envconfig:"SMTP_PORT" default:"587"`
	SMTPUser     string `envconfig:"SMTP_USER"`
	SMTPPassword string `envconfig:"SMTP_PASSWORD"`
	SMTPFrom     string `envconfig:"SMTP_FROM" default:"noreply@argus.io"`
	SMTPTLS      bool   `envconfig:"SMTP_TLS" default:"true"`

	TelegramBotToken     string `envconfig:"TELEGRAM_BOT_TOKEN"`
	TelegramDefaultChat  string `envconfig:"TELEGRAM_DEFAULT_CHAT_ID"`

	S3Endpoint  string `envconfig:"S3_ENDPOINT"`
	S3AccessKey string `envconfig:"S3_ACCESS_KEY"`
	S3SecretKey string `envconfig:"S3_SECRET_KEY"`
	S3Bucket    string `envconfig:"S3_BUCKET" default:"argus-storage"`
	S3Region    string `envconfig:"S3_REGION" default:"eu-west-1"`
	S3PathStyle bool   `envconfig:"S3_PATH_STYLE" default:"false"`

	EncryptionKey  string `envconfig:"ENCRYPTION_KEY"`

	TLSCertPath    string `envconfig:"TLS_CERT_PATH"`
	TLSKeyPath     string `envconfig:"TLS_KEY_PATH"`
	RadSecCertPath string `envconfig:"RADSEC_CERT_PATH"`
	RadSecKeyPath  string `envconfig:"RADSEC_KEY_PATH"`
	RadSecCAPath   string `envconfig:"RADSEC_CA_PATH"`

	DefaultMaxSIMs             int `envconfig:"DEFAULT_MAX_SIMS" default:"1000000"`
	DefaultMaxAPNs             int `envconfig:"DEFAULT_MAX_APNS" default:"100"`
	DefaultMaxUsers            int `envconfig:"DEFAULT_MAX_USERS" default:"50"`
	DefaultMaxAPIKeys          int `envconfig:"DEFAULT_MAX_API_KEYS" default:"20"`
	DefaultPurgeRetentionDays  int `envconfig:"DEFAULT_PURGE_RETENTION_DAYS" default:"90"`
	DefaultAuditRetentionDays  int `envconfig:"DEFAULT_AUDIT_RETENTION_DAYS" default:"365"`
	DefaultCDRRetentionDays    int `envconfig:"DEFAULT_CDR_RETENTION_DAYS" default:"180"`

	JobMaxConcurrentPerTenant int           `envconfig:"JOB_MAX_CONCURRENT_PER_TENANT" default:"5"`
	JobTimeoutMinutes         int           `envconfig:"JOB_TIMEOUT_MINUTES" default:"30"`
	JobTimeoutCheckInterval   time.Duration `envconfig:"JOB_TIMEOUT_CHECK_INTERVAL" default:"5m"`
	JobLockTTL                time.Duration `envconfig:"JOB_LOCK_TTL" default:"60s"`
	JobLockRenewInterval      time.Duration `envconfig:"JOB_LOCK_RENEW_INTERVAL" default:"30s"`
	CronPurgeSweep            string        `envconfig:"CRON_PURGE_SWEEP" default:"@daily"`
	CronIPReclaim             string        `envconfig:"CRON_IP_RECLAIM" default:"@hourly"`
	CronSLAReport             string        `envconfig:"CRON_SLA_REPORT" default:"@daily"`
	CronS3Archival            string        `envconfig:"CRON_S3_ARCHIVAL" default:"0 3 * * 0"`
	CronDataRetention         string        `envconfig:"CRON_DATA_RETENTION" default:"@daily"`
	CronStorageMonitor        string        `envconfig:"CRON_STORAGE_MONITOR" default:"@hourly"`
	CronEnabled               bool          `envconfig:"CRON_ENABLED" default:"true"`
	RoamingRenewalAlertDays   int           `envconfig:"ROAMING_RENEWAL_ALERT_DAYS" default:"30"`
	RoamingRenewalCron        string        `envconfig:"ROAMING_RENEWAL_CRON" default:"0 6 * * *"`

	StorageAlertPct           float64       `envconfig:"STORAGE_ALERT_PCT" default:"80"`

	PprofEnabled bool   `envconfig:"PPROF_ENABLED" default:"false"`
	PprofAddr    string `envconfig:"PPROF_ADDR" default:":6060"`
	GOGC         int    `envconfig:"GOGC" default:"100"`

	WSMaxConnsPerTenant int           `envconfig:"WS_MAX_CONNS_PER_TENANT" default:"100"`
	WSMaxConnsPerUser   int           `envconfig:"WS_MAX_CONNS_PER_USER" default:"5"`
	WSPongTimeout       time.Duration `envconfig:"WS_PONG_TIMEOUT" default:"90s"`

	RadSecPort         int    `envconfig:"RADSEC_PORT" default:"2083"`
	DiameterTLSEnabled bool   `envconfig:"DIAMETER_TLS_ENABLED" default:"false"`
	DiameterTLSCert    string `envconfig:"DIAMETER_TLS_CERT_PATH"`
	DiameterTLSKey     string `envconfig:"DIAMETER_TLS_KEY_PATH"`
	DiameterTLSCA      string `envconfig:"DIAMETER_TLS_CA_PATH"`

	CORSAllowedOrigins string `envconfig:"CORS_ALLOWED_ORIGINS" default:""`
	CSPDirectives      string `envconfig:"CSP_DIRECTIVES" default:""`
	SecurityHeaders    bool   `envconfig:"SECURITY_HEADERS_ENABLED" default:"true"`

	BruteForceMaxAttempts   int `envconfig:"BRUTE_FORCE_MAX_ATTEMPTS" default:"10"`
	BruteForceWindowSeconds int `envconfig:"BRUTE_FORCE_WINDOW_SECONDS" default:"900"`

	DevCORSAllowAll  bool `envconfig:"DEV_CORS_ALLOW_ALL" default:"true"`

	ESIMProvider       string `envconfig:"ESIM_SMDP_PROVIDER"       default:"mock"`
	ESIMSMDPBaseURL    string `envconfig:"ESIM_SMDP_BASE_URL"`
	ESIMSMDPAPIKey     string `envconfig:"ESIM_SMDP_API_KEY"`
	ESIMSMDPClientCert string `envconfig:"ESIM_SMDP_CLIENT_CERT_PATH"`
	ESIMSMDPClientKey  string `envconfig:"ESIM_SMDP_CLIENT_KEY_PATH"`

	SMSProvider          string `envconfig:"SMS_PROVIDER"              default:""`
	SMSAccountID         string `envconfig:"SMS_ACCOUNT_ID"`
	SMSAuthToken         string `envconfig:"SMS_AUTH_TOKEN"`
	SMSFromNumber        string `envconfig:"SMS_FROM_NUMBER"`
	SMSStatusCallbackURL string `envconfig:"SMS_STATUS_CALLBACK_URL"`

	SBANRFURL          string `envconfig:"SBA_NRF_URL"`
	SBANFInstanceID    string `envconfig:"SBA_NF_INSTANCE_ID"       default:"argus-sba-01"`
	SBANRFHeartbeatSec int    `envconfig:"SBA_NRF_HEARTBEAT_SEC"    default:"30"`

	OTELExporterOTLPEndpoint   string  `envconfig:"OTEL_EXPORTER_OTLP_ENDPOINT"    default:""`
	OTELSamplerRatio           float64 `envconfig:"OTEL_SAMPLER_RATIO"             default:"1.0"`
	OTELServiceName            string  `envconfig:"OTEL_SERVICE_NAME"              default:"argus"`
	OTELServiceVersion         string  `envconfig:"OTEL_SERVICE_VERSION"           default:"dev"`
	OTELDeploymentEnvironment  string  `envconfig:"OTEL_DEPLOYMENT_ENVIRONMENT"    default:"development"`
	MetricsTenantLabelEnabled  bool    `envconfig:"METRICS_TENANT_LABEL_ENABLED"   default:"true"`
	MetricsEnabled             bool    `envconfig:"METRICS_ENABLED"                default:"true"`
	MetricsNamespace           string  `envconfig:"METRICS_NAMESPACE"              default:"argus"`
	OTELBSPExportTimeoutSec    int     `envconfig:"OTEL_BSP_EXPORT_TIMEOUT_SEC"    default:"5"`

	ShutdownTimeoutSec  int `envconfig:"SHUTDOWN_TIMEOUT_SECONDS"  default:"30"`
	ShutdownHTTPSec     int `envconfig:"SHUTDOWN_HTTP_SECONDS"     default:"20"`
	ShutdownWSSec       int `envconfig:"SHUTDOWN_WS_SECONDS"       default:"10"`
	ShutdownRADIUSSec   int `envconfig:"SHUTDOWN_RADIUS_SECONDS"   default:"5"`
	ShutdownDiameterSec int `envconfig:"SHUTDOWN_DIAMETER_SECONDS" default:"5"`
	ShutdownSBASec      int `envconfig:"SHUTDOWN_SBA_SECONDS"      default:"5"`
	ShutdownJobSec      int `envconfig:"SHUTDOWN_JOB_SECONDS"      default:"30"`
	ShutdownNATSSec     int `envconfig:"SHUTDOWN_NATS_SECONDS"     default:"5"`
	ShutdownDBSec       int `envconfig:"SHUTDOWN_DB_SECONDS"       default:"5"`

	CircuitBreakerThreshold   int `envconfig:"CIRCUIT_BREAKER_THRESHOLD"    default:"5"`
	CircuitBreakerRecoverySec int `envconfig:"CIRCUIT_BREAKER_RECOVERY_SEC" default:"30"`

	JWTSecretPrevious string `envconfig:"JWT_SECRET_PREVIOUS"`

	TLSEnabled            bool `envconfig:"TLS_ENABLED"             default:"false"`
	TrustForwardedProto   bool `envconfig:"TRUST_FORWARDED_PROTO"   default:"true"`

	PprofToken string `envconfig:"PPROF_TOKEN"`

	RequestBodyMaxMB  int `envconfig:"REQUEST_BODY_MAX_MB"  default:"10"`
	RequestBodyAuthMB int `envconfig:"REQUEST_BODY_AUTH_MB" default:"1"`
	RequestBodyBulkMB int `envconfig:"REQUEST_BODY_BULK_MB" default:"50"`

	DiskProbeMount  string `envconfig:"DISK_PROBE_MOUNTS"    default:"/var/lib/postgresql/data,/app/logs,/data"`
	DiskDegradedPct int    `envconfig:"DISK_DEGRADED_PCT"    default:"85"`
	DiskUnhealthyPct int   `envconfig:"DISK_UNHEALTHY_PCT"   default:"95"`

	BackupEnabled          bool   `envconfig:"BACKUP_ENABLED"           default:"false"`
	BackupDailyCron        string `envconfig:"BACKUP_DAILY_CRON"        default:"0 2 * * *"`
	BackupVerifyCron       string `envconfig:"BACKUP_VERIFY_CRON"       default:"0 3 * * 0"`
	BackupCleanupCron      string `envconfig:"BACKUP_CLEANUP_CRON"      default:"0 4 * * *"`
	BackupBucket           string `envconfig:"BACKUP_BUCKET"            default:"argus-backup"`
	BackupTimeoutSec       int    `envconfig:"BACKUP_TIMEOUT_SECONDS"   default:"1800"`
	BackupRetentionDaily   int    `envconfig:"BACKUP_RETENTION_DAILY"   default:"14"`
	BackupRetentionWeekly  int    `envconfig:"BACKUP_RETENTION_WEEKLY"  default:"8"`
	BackupRetentionMonthly int    `envconfig:"BACKUP_RETENTION_MONTHLY" default:"12"`

	NATSConsumerLagAlertThreshold int `envconfig:"NATS_CONSUMER_LAG_ALERT_THRESHOLD" default:"10000"`
	NATSConsumerLagPollSec        int `envconfig:"NATS_CONSUMER_LAG_POLL_SECONDS"    default:"30"`

	CapacitySIMs          int `envconfig:"ARGUS_CAPACITY_SIM"                  default:"15000000"`
	CapacitySessions      int `envconfig:"ARGUS_CAPACITY_SESSION"              default:"2000000"`
	CapacityAuthPerSec    int `envconfig:"ARGUS_CAPACITY_AUTH"                 default:"5000"`
	CapacityMonthlyGrowth int `envconfig:"ARGUS_CAPACITY_GROWTH_SIMS_MONTHLY"  default:"72000"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &cfg, nil
}

var validEnvs = map[string]bool{
	"development": true,
	"staging":     true,
	"production":  true,
}

var validDeploymentModes = map[string]bool{
	"single":  true,
	"cluster": true,
}

func (c *Config) Validate() error {
	if !validEnvs[c.AppEnv] {
		return fmt.Errorf("invalid APP_ENV %q: must be development, staging, or production", c.AppEnv)
	}

	if !validDeploymentModes[c.DeploymentMode] {
		return fmt.Errorf("invalid DEPLOYMENT_MODE %q: must be single or cluster", c.DeploymentMode)
	}

	if _, err := zerolog.ParseLevel(strings.ToLower(c.LogLevel)); err != nil {
		return fmt.Errorf("invalid LOG_LEVEL %q: %w", c.LogLevel, err)
	}

	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters (got %d)", len(c.JWTSecret))
	}

	if c.BcryptCost < 10 || c.BcryptCost > 14 {
		return fmt.Errorf("BCRYPT_COST must be between 10 and 14 (got %d)", c.BcryptCost)
	}

	if !c.IsDev() && c.BcryptCost < 12 {
		return fmt.Errorf("BCRYPT_COST must be at least 12 in non-development environments (got %d)", c.BcryptCost)
	}

	if c.DatabaseMaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be > 0 (got %d)", c.DatabaseMaxConns)
	}

	if c.RedisMaxConns <= 0 {
		return fmt.Errorf("REDIS_MAX_CONNS must be > 0 (got %d)", c.RedisMaxConns)
	}

	if c.OTELSamplerRatio < 0.0 || c.OTELSamplerRatio > 1.0 {
		return fmt.Errorf("OTEL_SAMPLER_RATIO must be in [0.0, 1.0] (got %g)", c.OTELSamplerRatio)
	}

	if c.ShutdownTimeoutSec < 5 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT_SECONDS must be >= 5 (got %d)", c.ShutdownTimeoutSec)
	}

	if c.ShutdownJobSec > c.ShutdownTimeoutSec {
		return fmt.Errorf("SHUTDOWN_JOB_SECONDS (%d) must not exceed SHUTDOWN_TIMEOUT_SECONDS (%d)", c.ShutdownJobSec, c.ShutdownTimeoutSec)
	}

	if c.PprofEnabled && !c.IsDev() && len(c.PprofToken) < 32 {
		return fmt.Errorf("PPROF_TOKEN must be at least 32 characters when PPROF_ENABLED=true in non-development environments (got %d)", len(c.PprofToken))
	}

	if c.JWTSecretPrevious != "" && len(c.JWTSecretPrevious) < 32 {
		return fmt.Errorf("JWT_SECRET_PREVIOUS must be at least 32 characters if set (got %d)", len(c.JWTSecretPrevious))
	}

	if c.RequestBodyMaxMB <= 0 {
		return fmt.Errorf("REQUEST_BODY_MAX_MB must be > 0 (got %d)", c.RequestBodyMaxMB)
	}

	if c.RequestBodyAuthMB <= 0 {
		return fmt.Errorf("REQUEST_BODY_AUTH_MB must be > 0 (got %d)", c.RequestBodyAuthMB)
	}

	if c.RequestBodyBulkMB <= 0 {
		return fmt.Errorf("REQUEST_BODY_BULK_MB must be > 0 (got %d)", c.RequestBodyBulkMB)
	}

	if c.CircuitBreakerThreshold < 1 {
		return fmt.Errorf("CIRCUIT_BREAKER_THRESHOLD must be >= 1 (got %d)", c.CircuitBreakerThreshold)
	}

	if c.DiskDegradedPct >= c.DiskUnhealthyPct || c.DiskUnhealthyPct > 100 {
		return fmt.Errorf("DISK_DEGRADED_PCT (%d) must be < DISK_UNHEALTHY_PCT (%d) and DISK_UNHEALTHY_PCT must be <= 100", c.DiskDegradedPct, c.DiskUnhealthyPct)
	}

	if c.PasswordMinLength < 8 {
		return fmt.Errorf("PASSWORD_MIN_LENGTH must be >= 8 (got %d)", c.PasswordMinLength)
	}

	if c.PasswordHistoryCount < 0 {
		return fmt.Errorf("PASSWORD_HISTORY_COUNT must be >= 0 (got %d)", c.PasswordHistoryCount)
	}

	if c.PasswordMaxRepeating < 2 {
		return fmt.Errorf("PASSWORD_MAX_REPEATING must be >= 2 (got %d)", c.PasswordMaxRepeating)
	}

	if c.LoginMaxAttempts < 1 {
		return fmt.Errorf("LOGIN_MAX_ATTEMPTS must be >= 1 (got %d)", c.LoginMaxAttempts)
	}

	if c.LoginLockoutDur <= 0 {
		return fmt.Errorf("LOGIN_LOCKOUT_DURATION must be > 0 (got %v)", c.LoginLockoutDur)
	}

	return nil
}

func (c *Config) IsDev() bool {
	return c.AppEnv == "development"
}

func (c *Config) IsProd() bool {
	return c.AppEnv == "production"
}

func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.AppPort)
}

func (c *Config) TotalShutdownBudget() time.Duration {
	sum := c.ShutdownHTTPSec + c.ShutdownWSSec + c.ShutdownRADIUSSec + c.ShutdownDiameterSec +
		c.ShutdownSBASec + c.ShutdownJobSec + c.ShutdownNATSSec + c.ShutdownDBSec
	return time.Duration(max(c.ShutdownTimeoutSec, sum)) * time.Second
}
