package admin

import (
	"testing"
)

func TestHealthFromRate(t *testing.T) {
	tests := []struct {
		rate float64
		want string
	}{
		{1.0, "green"},
		{0.99, "green"},
		{0.98, "green"},
		{0.97, "yellow"},
		{0.85, "yellow"},
		{0.84, "red"},
		{0.0, "red"},
	}
	for _, tc := range tests {
		got := healthFromRate(tc.rate)
		if got != tc.want {
			t.Errorf("healthFromRate(%v) = %q, want %q", tc.rate, got, tc.want)
		}
	}
}

func TestQuotaStatus(t *testing.T) {
	tests := []struct {
		pct  float64
		want string
	}{
		{99.0, "danger"},
		{95.0, "danger"},
		{94.9, "warning"},
		{80.0, "warning"},
		{79.9, "ok"},
		{0.0, "ok"},
	}
	for _, tc := range tests {
		got := quotaStatus(tc.pct)
		if got != tc.want {
			t.Errorf("quotaStatus(%v) = %q, want %q", tc.pct, got, tc.want)
		}
	}
}

func TestJobStateToDSARStatus(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"queued", "received"},
		{"pending", "received"},
		{"running", "processing"},
		{"succeeded", "completed"},
		{"failed", "failed"},
	}
	for _, tc := range tests {
		got := jobStateToDSARStatus(tc.state)
		if got != tc.want {
			t.Errorf("jobStateToDSARStatus(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestParseUA(t *testing.T) {
	ua := parseUA("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
	if ua.Browser != "Chrome" {
		t.Errorf("expected Chrome, got %s", ua.Browser)
	}
	if ua.OS != "Windows" {
		t.Errorf("expected Windows, got %s", ua.OS)
	}
}

func TestParseUA_Empty(t *testing.T) {
	ua := parseUA("")
	if ua.Browser != "Unknown" {
		t.Errorf("expected Unknown, got %s", ua.Browser)
	}
}

func TestWindowDuration(t *testing.T) {
	tests := []struct {
		window string
		hours  float64
	}{
		{"1h", 1},
		{"7d", 168},
		{"24h", 24},
		{"", 24},
	}
	for _, tc := range tests {
		d := windowDuration(tc.window)
		if d.Hours() != tc.hours {
			t.Errorf("windowDuration(%q) = %v, want %v hours", tc.window, d.Hours(), tc.hours)
		}
	}
}
