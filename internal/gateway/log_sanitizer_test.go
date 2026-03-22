package gateway

import (
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"Password", true},
		{"PASSWORD", true},
		{"user_password", true},
		{"secret", true},
		{"jwt_secret", true},
		{"token", true},
		{"access_token", true},
		{"refresh_token", true},
		{"api_key", true},
		{"apikey", true},
		{"x-api-key", true},
		{"authorization", true},
		{"Authorization", true},
		{"cookie", true},
		{"totp_secret", true},
		{"encryption_key", true},
		{"private_key", true},
		{"radius_secret", true},
		{"smtp_password", true},
		{"credentials", true},

		{"username", false},
		{"email", false},
		{"name", false},
		{"id", false},
		{"tenant_id", false},
		{"status", false},
	}

	for _, tt := range tests {
		got := isSensitiveKey(tt.key)
		if got != tt.want {
			t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestSanitizeLogField(t *testing.T) {
	tests := []struct {
		key   string
		value string
		want  string
	}{
		{"password", "my-secret-pass", maskedValue},
		{"api_key", "sk-123456789", maskedValue},
		{"username", "john@example.com", "john@example.com"},
		{"Authorization", "Bearer eyJ...", maskedValue},
		{"email", "user@test.com", "user@test.com"},
	}

	for _, tt := range tests {
		got := SanitizeLogField(tt.key, tt.value)
		if got != tt.want {
			t.Errorf("SanitizeLogField(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
		}
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ab", maskedValue},
		{"abcd", maskedValue},
		{"abcdef", "ab***ef"},
		{"super-long-secret-key-12345", "su***45"},
	}

	for _, tt := range tests {
		got := MaskSecret(tt.input)
		if got != tt.want {
			t.Errorf("MaskSecret(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLogSanitizerHookInterface(t *testing.T) {
	h := NewLogSanitizer()
	h.Run(nil, 0, "test")
}
