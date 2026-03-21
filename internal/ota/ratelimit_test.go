package ota

import (
	"testing"
)

func TestNewRateLimiter_DefaultMaxPerHour(t *testing.T) {
	rl := NewRateLimiter(nil, 0)
	if rl.MaxPerHour() != DefaultMaxOTAPerSimPerHour {
		t.Errorf("MaxPerHour() = %d, want %d", rl.MaxPerHour(), DefaultMaxOTAPerSimPerHour)
	}
}

func TestNewRateLimiter_NegativeMaxPerHour(t *testing.T) {
	rl := NewRateLimiter(nil, -5)
	if rl.MaxPerHour() != DefaultMaxOTAPerSimPerHour {
		t.Errorf("MaxPerHour() = %d, want %d", rl.MaxPerHour(), DefaultMaxOTAPerSimPerHour)
	}
}

func TestNewRateLimiter_CustomMaxPerHour(t *testing.T) {
	rl := NewRateLimiter(nil, 50)
	if rl.MaxPerHour() != 50 {
		t.Errorf("MaxPerHour() = %d, want 50", rl.MaxPerHour())
	}
}

func TestDefaultMaxOTAPerSimPerHour(t *testing.T) {
	if DefaultMaxOTAPerSimPerHour != 10 {
		t.Errorf("DefaultMaxOTAPerSimPerHour = %d, want 10", DefaultMaxOTAPerSimPerHour)
	}
}

func TestRateLimitKeyPrefix(t *testing.T) {
	if otaRateLimitKeyPrefix != "ota:ratelimit:" {
		t.Errorf("key prefix = %q, want %q", otaRateLimitKeyPrefix, "ota:ratelimit:")
	}
}
