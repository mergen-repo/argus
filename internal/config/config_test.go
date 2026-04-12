package config

import (
	"os"
	"testing"
	"time"

	"github.com/kelseyhightower/envconfig"
)

func processEnvconfig(cfg *Config) error {
	return envconfig.Process("", cfg)
}

func validConfig() Config {
	return Config{
		AppEnv:           "development",
		AppPort:          8080,
		WSPort:           8081,
		LogLevel:         "info",
		DeploymentMode:   "single",
		DatabaseURL:      "postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable",
		DatabaseMaxConns: 50,
		RedisURL:         "redis://localhost:6379/0",
		RedisMaxConns:    100,
		NATSURL:          "nats://localhost:4222",
		JWTSecret:        "this-is-a-very-long-secret-key-for-testing-purposes",
		BcryptCost:       12,

		ShutdownTimeoutSec:        30,
		ShutdownHTTPSec:           20,
		ShutdownWSSec:             10,
		ShutdownRADIUSSec:         5,
		ShutdownDiameterSec:       5,
		ShutdownSBASec:            5,
		ShutdownJobSec:            30,
		ShutdownNATSSec:           5,
		ShutdownDBSec:             5,
		CircuitBreakerThreshold:   5,
		CircuitBreakerRecoverySec: 30,
		RequestBodyMaxMB:          10,
		RequestBodyAuthMB:         1,
		RequestBodyBulkMB:         50,
		DiskDegradedPct:           85,
		DiskUnhealthyPct:          95,
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidate_InvalidAppEnv(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "invalid"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid APP_ENV")
	}
}

func TestValidate_InvalidDeploymentMode(t *testing.T) {
	cfg := validConfig()
	cfg.DeploymentMode = "multi"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid DEPLOYMENT_MODE")
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	cfg := validConfig()
	cfg.LogLevel = "verbose"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid LOG_LEVEL")
	}
}

func TestValidate_ShortJWTSecret(t *testing.T) {
	cfg := validConfig()
	cfg.JWTSecret = "short"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for short JWT_SECRET")
	}
}

func TestValidate_BcryptCostTooLow(t *testing.T) {
	cfg := validConfig()
	cfg.BcryptCost = 5
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for low BCRYPT_COST")
	}
}

func TestValidate_BcryptCostTooHigh(t *testing.T) {
	cfg := validConfig()
	cfg.BcryptCost = 20
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for high BCRYPT_COST")
	}
}

func TestValidate_ZeroDatabaseMaxConns(t *testing.T) {
	cfg := validConfig()
	cfg.DatabaseMaxConns = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero DATABASE_MAX_CONNS")
	}
}

func TestValidate_ZeroRedisMaxConns(t *testing.T) {
	cfg := validConfig()
	cfg.RedisMaxConns = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero REDIS_MAX_CONNS")
	}
}

func TestValidate_AllValidLogLevels(t *testing.T) {
	levels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}
	for _, lvl := range levels {
		cfg := validConfig()
		cfg.LogLevel = lvl
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected log level %q to be valid, got: %v", lvl, err)
		}
	}
}

func TestValidate_AllValidEnvs(t *testing.T) {
	envs := []string{"development", "staging", "production"}
	for _, env := range envs {
		cfg := validConfig()
		cfg.AppEnv = env
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected env %q to be valid, got: %v", env, err)
		}
	}
}

func TestValidate_BcryptCostTooLowForProduction(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "production"
	cfg.BcryptCost = 10
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for BCRYPT_COST < 12 in production")
	}
}

func TestValidate_BcryptCostTooLowForStaging(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "staging"
	cfg.BcryptCost = 11
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for BCRYPT_COST < 12 in staging")
	}
}

func TestValidate_BcryptCostLowAllowedInDevelopment(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "development"
	cfg.BcryptCost = 10
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for BCRYPT_COST=10 in development, got: %v", err)
	}
}

func TestValidate_OTELSamplerRatio_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.OTELSamplerRatio = 0.5
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for OTEL_SAMPLER_RATIO=0.5, got: %v", err)
	}
}

func TestValidate_OTELSamplerRatio_OutOfRange(t *testing.T) {
	cfg := validConfig()
	cfg.OTELSamplerRatio = 1.5
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for OTEL_SAMPLER_RATIO=1.5")
	}
}

func TestValidate_ShutdownTimeoutSec_TooLow(t *testing.T) {
	cfg := validConfig()
	cfg.ShutdownTimeoutSec = 4
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for SHUTDOWN_TIMEOUT_SECONDS < 5")
	}
}

func TestValidate_ShutdownTimeoutSec_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.ShutdownTimeoutSec = 5
	cfg.ShutdownJobSec = 5
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for SHUTDOWN_TIMEOUT_SECONDS=5, got: %v", err)
	}
}

func TestValidate_ShutdownJobSec_ExceedsTimeout(t *testing.T) {
	cfg := validConfig()
	cfg.ShutdownTimeoutSec = 30
	cfg.ShutdownJobSec = 31
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when SHUTDOWN_JOB_SECONDS > SHUTDOWN_TIMEOUT_SECONDS")
	}
}

func TestValidate_ShutdownJobSec_EqualTimeout(t *testing.T) {
	cfg := validConfig()
	cfg.ShutdownTimeoutSec = 30
	cfg.ShutdownJobSec = 30
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error when SHUTDOWN_JOB_SECONDS == SHUTDOWN_TIMEOUT_SECONDS, got: %v", err)
	}
}

func TestValidate_PprofToken_RequiredInNonDev(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "production"
	cfg.PprofEnabled = true
	cfg.PprofToken = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for short PPROF_TOKEN with PPROF_ENABLED in production")
	}
}

func TestValidate_PprofToken_ValidInNonDev(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "production"
	cfg.PprofEnabled = true
	cfg.PprofToken = "this-is-a-long-enough-pprof-token-for-production-use"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for valid PPROF_TOKEN in production, got: %v", err)
	}
}

func TestValidate_PprofToken_NotRequiredWhenDisabled(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "production"
	cfg.PprofEnabled = false
	cfg.PprofToken = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error when PPROF_ENABLED=false, got: %v", err)
	}
}

func TestValidate_PprofToken_NotRequiredInDev(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "development"
	cfg.PprofEnabled = true
	cfg.PprofToken = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for missing PPROF_TOKEN in development, got: %v", err)
	}
}

func TestValidate_JWTSecretPrevious_TooShort(t *testing.T) {
	cfg := validConfig()
	cfg.JWTSecretPrevious = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for short JWT_SECRET_PREVIOUS")
	}
}

func TestValidate_JWTSecretPrevious_ValidLength(t *testing.T) {
	cfg := validConfig()
	cfg.JWTSecretPrevious = "this-is-a-valid-previous-jwt-secret-key-value"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for valid JWT_SECRET_PREVIOUS, got: %v", err)
	}
}

func TestValidate_JWTSecretPrevious_Empty(t *testing.T) {
	cfg := validConfig()
	cfg.JWTSecretPrevious = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for empty JWT_SECRET_PREVIOUS, got: %v", err)
	}
}

func TestValidate_RequestBodyMaxMB_Zero(t *testing.T) {
	cfg := validConfig()
	cfg.RequestBodyMaxMB = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for REQUEST_BODY_MAX_MB=0")
	}
}

func TestValidate_RequestBodyAuthMB_Zero(t *testing.T) {
	cfg := validConfig()
	cfg.RequestBodyAuthMB = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for REQUEST_BODY_AUTH_MB=0")
	}
}

func TestValidate_RequestBodyBulkMB_Zero(t *testing.T) {
	cfg := validConfig()
	cfg.RequestBodyBulkMB = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for REQUEST_BODY_BULK_MB=0")
	}
}

func TestValidate_RequestBodyMBs_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.RequestBodyMaxMB = 10
	cfg.RequestBodyAuthMB = 1
	cfg.RequestBodyBulkMB = 50
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for valid request body MB values, got: %v", err)
	}
}

func TestValidate_CircuitBreakerThreshold_Zero(t *testing.T) {
	cfg := validConfig()
	cfg.CircuitBreakerThreshold = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for CIRCUIT_BREAKER_THRESHOLD=0")
	}
}

func TestValidate_CircuitBreakerThreshold_One(t *testing.T) {
	cfg := validConfig()
	cfg.CircuitBreakerThreshold = 1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for CIRCUIT_BREAKER_THRESHOLD=1, got: %v", err)
	}
}

func TestValidate_DiskPct_DegradedEqualUnhealthy(t *testing.T) {
	cfg := validConfig()
	cfg.DiskDegradedPct = 90
	cfg.DiskUnhealthyPct = 90
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when DISK_DEGRADED_PCT == DISK_UNHEALTHY_PCT")
	}
}

func TestValidate_DiskPct_DegradedGreaterThanUnhealthy(t *testing.T) {
	cfg := validConfig()
	cfg.DiskDegradedPct = 95
	cfg.DiskUnhealthyPct = 90
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when DISK_DEGRADED_PCT > DISK_UNHEALTHY_PCT")
	}
}

func TestValidate_DiskPct_UnhealthyAbove100(t *testing.T) {
	cfg := validConfig()
	cfg.DiskDegradedPct = 85
	cfg.DiskUnhealthyPct = 101
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when DISK_UNHEALTHY_PCT > 100")
	}
}

func TestValidate_DiskPct_Valid(t *testing.T) {
	cfg := validConfig()
	cfg.DiskDegradedPct = 85
	cfg.DiskUnhealthyPct = 95
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for valid disk pct values, got: %v", err)
	}
}

func TestDefaults_Story066Fields(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://argus:argus_dev@localhost:5432/argus_dev?sslmode=disable")
	os.Setenv("REDIS_URL", "redis://localhost:6379/0")
	os.Setenv("NATS_URL", "nats://localhost:4222")
	os.Setenv("JWT_SECRET", "this-is-a-very-long-secret-key-for-testing-purposes")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("REDIS_URL")
		os.Unsetenv("NATS_URL")
		os.Unsetenv("JWT_SECRET")
	}()

	var cfg Config
	if err := processEnvconfig(&cfg); err != nil {
		t.Fatalf("envconfig.Process failed: %v", err)
	}

	checks := []struct {
		name string
		got  int
		want int
	}{
		{"ShutdownTimeoutSec", cfg.ShutdownTimeoutSec, 30},
		{"ShutdownHTTPSec", cfg.ShutdownHTTPSec, 20},
		{"ShutdownWSSec", cfg.ShutdownWSSec, 10},
		{"ShutdownRADIUSSec", cfg.ShutdownRADIUSSec, 5},
		{"ShutdownDiameterSec", cfg.ShutdownDiameterSec, 5},
		{"ShutdownSBASec", cfg.ShutdownSBASec, 5},
		{"ShutdownJobSec", cfg.ShutdownJobSec, 30},
		{"ShutdownNATSSec", cfg.ShutdownNATSSec, 5},
		{"ShutdownDBSec", cfg.ShutdownDBSec, 5},
		{"CircuitBreakerThreshold", cfg.CircuitBreakerThreshold, 5},
		{"CircuitBreakerRecoverySec", cfg.CircuitBreakerRecoverySec, 30},
		{"RequestBodyMaxMB", cfg.RequestBodyMaxMB, 10},
		{"RequestBodyAuthMB", cfg.RequestBodyAuthMB, 1},
		{"RequestBodyBulkMB", cfg.RequestBodyBulkMB, 50},
		{"DiskDegradedPct", cfg.DiskDegradedPct, 85},
		{"DiskUnhealthyPct", cfg.DiskUnhealthyPct, 95},
		{"BackupTimeoutSec", cfg.BackupTimeoutSec, 1800},
		{"BackupRetentionDaily", cfg.BackupRetentionDaily, 14},
		{"BackupRetentionWeekly", cfg.BackupRetentionWeekly, 8},
		{"BackupRetentionMonthly", cfg.BackupRetentionMonthly, 12},
		{"NATSConsumerLagAlertThreshold", cfg.NATSConsumerLagAlertThreshold, 10000},
		{"NATSConsumerLagPollSec", cfg.NATSConsumerLagPollSec, 30},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("default %s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	stringChecks := []struct {
		name string
		got  string
		want string
	}{
		{"BackupBucket", cfg.BackupBucket, "argus-backup"},
		{"BackupDailyCron", cfg.BackupDailyCron, "0 2 * * *"},
		{"BackupVerifyCron", cfg.BackupVerifyCron, "0 3 * * 0"},
		{"BackupCleanupCron", cfg.BackupCleanupCron, "0 4 * * *"},
		{"DiskProbeMount", cfg.DiskProbeMount, "/var/lib/postgresql/data,/app/logs,/data"},
	}
	for _, tc := range stringChecks {
		if tc.got != tc.want {
			t.Errorf("default %s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}

	if cfg.BackupEnabled {
		t.Error("default BackupEnabled should be false")
	}
	if cfg.TLSEnabled {
		t.Error("default TLSEnabled should be false")
	}
	if !cfg.TrustForwardedProto {
		t.Error("default TrustForwardedProto should be true")
	}
}

func TestTotalShutdownBudget_TimeoutDominates(t *testing.T) {
	cfg := validConfig()
	cfg.ShutdownTimeoutSec = 300
	cfg.ShutdownHTTPSec = 20
	cfg.ShutdownWSSec = 10
	cfg.ShutdownRADIUSSec = 5
	cfg.ShutdownDiameterSec = 5
	cfg.ShutdownSBASec = 5
	cfg.ShutdownJobSec = 30
	cfg.ShutdownNATSSec = 5
	cfg.ShutdownDBSec = 5
	got := cfg.TotalShutdownBudget()
	if got != 300*time.Second {
		t.Errorf("TotalShutdownBudget() = %v, want 300s (timeout dominates)", got)
	}
}

func TestTotalShutdownBudget_SumDominates(t *testing.T) {
	cfg := validConfig()
	cfg.ShutdownTimeoutSec = 30
	cfg.ShutdownHTTPSec = 20
	cfg.ShutdownWSSec = 15
	cfg.ShutdownRADIUSSec = 10
	cfg.ShutdownDiameterSec = 10
	cfg.ShutdownSBASec = 10
	cfg.ShutdownJobSec = 30
	cfg.ShutdownNATSSec = 10
	cfg.ShutdownDBSec = 10
	sum := 20 + 15 + 10 + 10 + 10 + 30 + 10 + 10
	got := cfg.TotalShutdownBudget()
	if got != time.Duration(sum)*time.Second {
		t.Errorf("TotalShutdownBudget() = %v, want %ds (sum dominates)", got, sum)
	}
}
