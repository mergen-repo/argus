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

	JWTSecret        string        `envconfig:"JWT_SECRET" required:"true"`
	JWTExpiry        time.Duration `envconfig:"JWT_EXPIRY" default:"15m"`
	JWTRefreshExpiry time.Duration `envconfig:"JWT_REFRESH_EXPIRY" default:"168h"`
	JWTIssuer        string        `envconfig:"JWT_ISSUER" default:"argus"`
	BcryptCost       int           `envconfig:"BCRYPT_COST" default:"12"`
	LoginMaxAttempts int           `envconfig:"LOGIN_MAX_ATTEMPTS" default:"5"`
	LoginLockoutDur  time.Duration `envconfig:"LOGIN_LOCKOUT_DURATION" default:"15m"`

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

	RateLimitPerMinute int    `envconfig:"RATE_LIMIT_DEFAULT_PER_MINUTE" default:"1000"`
	RateLimitPerHour   int    `envconfig:"RATE_LIMIT_DEFAULT_PER_HOUR" default:"30000"`
	RateLimitAlgorithm string `envconfig:"RATE_LIMIT_ALGORITHM" default:"sliding_window"`
	RateLimitAuthPerMin int   `envconfig:"RATE_LIMIT_AUTH_PER_MINUTE" default:"10"`
	RateLimitEnabled   bool   `envconfig:"RATE_LIMIT_ENABLED" default:"true"`

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

	StorageAlertPct           float64       `envconfig:"STORAGE_ALERT_PCT" default:"80"`

	PprofEnabled bool   `envconfig:"PPROF_ENABLED" default:"false"`
	PprofAddr    string `envconfig:"PPROF_ADDR" default:":6060"`
	GOGC         int    `envconfig:"GOGC" default:"100"`

	WSMaxConnsPerTenant int `envconfig:"WS_MAX_CONNS_PER_TENANT" default:"100"`

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

	DevSeedData      bool `envconfig:"DEV_SEED_DATA" default:"true"`
	DevMockOperator  bool `envconfig:"DEV_MOCK_OPERATOR" default:"true"`
	DevCORSAllowAll  bool `envconfig:"DEV_CORS_ALLOW_ALL" default:"true"`
	DevDisable2FA    bool `envconfig:"DEV_DISABLE_2FA" default:"true"`
	DevLogSQL        bool `envconfig:"DEV_LOG_SQL" default:"false"`
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

	if c.DatabaseMaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be > 0 (got %d)", c.DatabaseMaxConns)
	}

	if c.RedisMaxConns <= 0 {
		return fmt.Errorf("REDIS_MAX_CONNS must be > 0 (got %d)", c.RedisMaxConns)
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
