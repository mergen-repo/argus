package store

import (
	"testing"
	"time"
)

func TestResolvePeriod_Presets(t *testing.T) {
	tests := []struct {
		period   string
		wantBucket string
		wantView   string
	}{
		{"1h", "1 minute", "cdrs"},
		{"24h", "15 minutes", "cdrs_hourly"},
		{"7d", "1 hour", "cdrs_hourly"},
		{"30d", "6 hours", "cdrs_daily"},
		{"unknown", "1 hour", "cdrs_hourly"},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			spec := ResolvePeriod(tt.period, time.Time{}, time.Time{})
			if spec.BucketInterval != tt.wantBucket {
				t.Errorf("BucketInterval = %q, want %q", spec.BucketInterval, tt.wantBucket)
			}
			if spec.AggregateView != tt.wantView {
				t.Errorf("AggregateView = %q, want %q", spec.AggregateView, tt.wantView)
			}
		})
	}
}

func TestResolvePeriod_Custom(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name       string
		from       time.Time
		to         time.Time
		wantBucket string
		wantView   string
	}{
		{
			name:       "90min range -> 1min buckets",
			from:       now.Add(-90 * time.Minute),
			to:         now,
			wantBucket: "1 minute",
			wantView:   "cdrs",
		},
		{
			name:       "12h range -> 15min buckets",
			from:       now.Add(-12 * time.Hour),
			to:         now,
			wantBucket: "15 minutes",
			wantView:   "cdrs_hourly",
		},
		{
			name:       "3d range -> 1h buckets",
			from:       now.Add(-3 * 24 * time.Hour),
			to:         now,
			wantBucket: "1 hour",
			wantView:   "cdrs_hourly",
		},
		{
			name:       "14d range -> 6h buckets",
			from:       now.Add(-14 * 24 * time.Hour),
			to:         now,
			wantBucket: "6 hours",
			wantView:   "cdrs_daily",
		},
		{
			name:       "60d range -> 1d buckets",
			from:       now.Add(-60 * 24 * time.Hour),
			to:         now,
			wantBucket: "1 day",
			wantView:   "cdrs_daily",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := ResolvePeriod("custom", tt.from, tt.to)
			if spec.BucketInterval != tt.wantBucket {
				t.Errorf("BucketInterval = %q, want %q", spec.BucketInterval, tt.wantBucket)
			}
			if spec.AggregateView != tt.wantView {
				t.Errorf("AggregateView = %q, want %q", spec.AggregateView, tt.wantView)
			}
		})
	}
}

func TestResolveTimeRange(t *testing.T) {
	tests := []struct {
		period       string
		wantDuration time.Duration
	}{
		{"1h", 1 * time.Hour},
		{"24h", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"unknown", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			from, to := ResolveTimeRange(tt.period)
			got := to.Sub(from)
			diff := got - tt.wantDuration
			if diff < 0 {
				diff = -diff
			}
			if diff > 2*time.Second {
				t.Errorf("duration = %v, want ~%v", got, tt.wantDuration)
			}
		})
	}
}

func TestSanitizeDimension(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"operator", "operator_id"},
		{"operator_id", "operator_id"},
		{"apn", "apn_id"},
		{"apn_id", "apn_id"},
		{"rat_type", "rat_type"},
		{"evil; DROP TABLE", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeDimension(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeDimension(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBucketOrTimestamp(t *testing.T) {
	if got := bucketOrTimestamp("cdrs"); got != "timestamp" {
		t.Errorf("bucketOrTimestamp(cdrs) = %q, want timestamp", got)
	}
	if got := bucketOrTimestamp("cdrs_hourly"); got != "bucket" {
		t.Errorf("bucketOrTimestamp(cdrs_hourly) = %q, want bucket", got)
	}
	if got := bucketOrTimestamp("cdrs_daily"); got != "bucket" {
		t.Errorf("bucketOrTimestamp(cdrs_daily) = %q, want bucket", got)
	}
}
