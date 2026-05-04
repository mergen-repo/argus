package store

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestResolvePeriod_Presets(t *testing.T) {
	tests := []struct {
		period     string
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

// TestBuildTimeSeriesQuery_COALESCESentinel verifies that the dynamic SQL string emitted for a
// group_by request wraps the grouping column in `COALESCE(..., '__unassigned__')`. This is the
// AC-7 regression safeguard for FIX-204 — without COALESCE, NULL group values crash the pgx
// scan into `tp.GroupKey string`. AC-8 perf note: COALESCE is a planner-cheap pass-through for
// non-null values; no benchmark harness built per plan (pure code-inspectable guarantee).
func TestBuildTimeSeriesQuery_COALESCESentinel(t *testing.T) {
	base := UsageQueryParams{
		TenantID: uuid.New(),
		Period:   "24h",
		From:     time.Now().UTC().Add(-24 * time.Hour),
		To:       time.Now().UTC(),
	}

	cases := []struct {
		name       string
		groupBy    string
		wantSubstr string
	}{
		{"apn", "apn", `COALESCE(apn_id::text, '__unassigned__')`},
		{"operator", "operator", `COALESCE(operator_id::text, '__unassigned__')`},
		{"rat_type", "rat_type", `COALESCE(rat_type::text, '__unassigned__')`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.GroupBy = tc.groupBy
			query, _ := buildTimeSeriesQuery(p)
			if !strings.Contains(query, tc.wantSubstr) {
				t.Errorf("buildTimeSeriesQuery(groupBy=%q) missing %q\nfull query: %s",
					tc.groupBy, tc.wantSubstr, query)
			}
			if strings.Contains(query, `'unknown'`) {
				t.Errorf("buildTimeSeriesQuery(groupBy=%q) still contains legacy 'unknown' sentinel: %s",
					tc.groupBy, query)
			}
		})
	}
}

// TestBuildTimeSeriesQuery_NoGroupByOmitsCOALESCE verifies the sentinel is only applied when a
// group_by column is requested; the default (no-grouping) path does not project group_key.
func TestBuildTimeSeriesQuery_NoGroupByOmitsCOALESCE(t *testing.T) {
	p := UsageQueryParams{
		TenantID: uuid.New(),
		Period:   "24h",
		From:     time.Now().UTC().Add(-24 * time.Hour),
		To:       time.Now().UTC(),
	}
	query, _ := buildTimeSeriesQuery(p)
	if strings.Contains(query, "__unassigned__") {
		t.Errorf("buildTimeSeriesQuery(no group_by) unexpectedly contains sentinel: %s", query)
	}
	if strings.Contains(query, "AS group_key") {
		t.Errorf("buildTimeSeriesQuery(no group_by) unexpectedly projects group_key: %s", query)
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
