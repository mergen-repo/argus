package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btopcu/argus/internal/config"
)

func newTestConfig() *config.Config {
	return &config.Config{
		AppEnv:                    "development",
		DeploymentMode:            "single",
		SBAEnabled:                false,
		TLSEnabled:                false,
		BackupEnabled:             false,
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
}

func TestConfigHandler_Serve_200(t *testing.T) {
	h := NewConfigHandler(newTestConfig(), "1.0.0", "abc1234", "2026-04-13T08:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/config", nil)
	rec := httptest.NewRecorder()
	h.Serve(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200", rec.Code)
	}

	var resp struct {
		Status string `json:"status"`
		Data   struct {
			Version   string `json:"version"`
			GitSHA    string `json:"git_sha"`
			BuildTime string `json:"build_time"`
			StartedAt string `json:"started_at"`
			AppEnv    string `json:"app_env"`
			FeatureFlags struct {
				RateLimitEnabled bool `json:"rate_limit_enabled"`
				MetricsEnabled   bool `json:"metrics_enabled"`
			} `json:"feature_flags"`
			Protocols struct {
				RadiusAuthPort int `json:"radius_auth_port"`
				DiameterPort   int `json:"diameter_port"`
			} `json:"protocols"`
			Limits struct {
				RateLimitPerMinute int `json:"rate_limit_per_minute"`
				DefaultMaxSIMs     int `json:"default_max_sims"`
			} `json:"limits"`
			Retention struct {
				PurgeDays int `json:"purge_days"`
				AuditDays int `json:"audit_days"`
			} `json:"retention"`
			SecretsRedacted []string `json:"secrets_redacted"`
		} `json:"data"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Status != "success" {
		t.Errorf("status = %q, want success", resp.Status)
	}
	if resp.Data.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", resp.Data.Version)
	}
	if resp.Data.GitSHA != "abc1234" {
		t.Errorf("git_sha = %q, want abc1234", resp.Data.GitSHA)
	}
	if resp.Data.BuildTime != "2026-04-13T08:00:00Z" {
		t.Errorf("build_time = %q, want 2026-04-13T08:00:00Z", resp.Data.BuildTime)
	}
	if resp.Data.StartedAt == "" {
		t.Error("started_at should be non-empty")
	}
	if resp.Data.AppEnv != "development" {
		t.Errorf("app_env = %q, want development", resp.Data.AppEnv)
	}
	if !resp.Data.FeatureFlags.RateLimitEnabled {
		t.Error("feature_flags.rate_limit_enabled should be true")
	}
	if !resp.Data.FeatureFlags.MetricsEnabled {
		t.Error("feature_flags.metrics_enabled should be true")
	}
	if resp.Data.Protocols.RadiusAuthPort != 1812 {
		t.Errorf("protocols.radius_auth_port = %d, want 1812", resp.Data.Protocols.RadiusAuthPort)
	}
	if resp.Data.Protocols.DiameterPort != 3868 {
		t.Errorf("protocols.diameter_port = %d, want 3868", resp.Data.Protocols.DiameterPort)
	}
	if resp.Data.Limits.RateLimitPerMinute != 1000 {
		t.Errorf("limits.rate_limit_per_minute = %d, want 1000", resp.Data.Limits.RateLimitPerMinute)
	}
	if resp.Data.Limits.DefaultMaxSIMs != 1000000 {
		t.Errorf("limits.default_max_sims = %d, want 1000000", resp.Data.Limits.DefaultMaxSIMs)
	}
	if resp.Data.Retention.PurgeDays != 90 {
		t.Errorf("retention.purge_days = %d, want 90", resp.Data.Retention.PurgeDays)
	}
	if resp.Data.Retention.AuditDays != 365 {
		t.Errorf("retention.audit_days = %d, want 365", resp.Data.Retention.AuditDays)
	}
	if len(resp.Data.SecretsRedacted) < 14 {
		t.Errorf("secrets_redacted len = %d, want >= 14", len(resp.Data.SecretsRedacted))
	}
}

func TestConfigHandler_NoSecretsInResponse(t *testing.T) {
	cfg := newTestConfig()
	cfg.JWTSecret = "super-secret-jwt-value-xxxx"
	cfg.DatabaseURL = "postgres://user:password@host/db"
	cfg.EncryptionKey = "enc-key-should-not-leak"
	cfg.SMTPPassword = "smtp-pass-should-not-leak"
	cfg.TelegramBotToken = "telegram-token-should-not-leak"

	h := NewConfigHandler(cfg, "1.0.0", "abc1234", "2026-04-13T08:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/config", nil)
	rec := httptest.NewRecorder()
	h.Serve(rec, req)

	body := rec.Body.String()

	secrets := []string{
		"super-secret-jwt-value-xxxx",
		"postgres://user:password@host/db",
		"enc-key-should-not-leak",
		"smtp-pass-should-not-leak",
		"telegram-token-should-not-leak",
	}

	for _, s := range secrets {
		if containsStr(body, s) {
			t.Errorf("secret value %q leaked into response body", s)
		}
	}
}

func TestConfigHandler_BuildMetadataPresent(t *testing.T) {
	h := NewConfigHandler(newTestConfig(), "2.3.4", "deadbeef", "2026-01-01T00:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/config", nil)
	rec := httptest.NewRecorder()
	h.Serve(rec, req)

	var resp struct {
		Data struct {
			Version   string `json:"version"`
			GitSHA    string `json:"git_sha"`
			BuildTime string `json:"build_time"`
			StartedAt string `json:"started_at"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data.Version != "2.3.4" {
		t.Errorf("version = %q, want 2.3.4", resp.Data.Version)
	}
	if resp.Data.GitSHA != "deadbeef" {
		t.Errorf("git_sha = %q, want deadbeef", resp.Data.GitSHA)
	}
	if resp.Data.BuildTime != "2026-01-01T00:00:00Z" {
		t.Errorf("build_time = %q, want 2026-01-01T00:00:00Z", resp.Data.BuildTime)
	}
	if resp.Data.StartedAt == "" {
		t.Error("started_at must not be empty")
	}
}

func containsStr(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
