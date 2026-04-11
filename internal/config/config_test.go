package config

import (
	"testing"
)

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
