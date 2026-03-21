package job

import (
	"testing"
	"time"
)

func TestShouldFire_Hourly(t *testing.T) {
	cases := []struct {
		name   string
		t      time.Time
		expect bool
	}{
		{"midnight", time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC), true},
		{"1am", time.Date(2026, 3, 21, 1, 0, 0, 0, time.UTC), true},
		{"1:30", time.Date(2026, 3, 21, 1, 30, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFire("@hourly", tc.t); got != tc.expect {
				t.Errorf("shouldFire(@hourly, %s) = %v, want %v", tc.t, got, tc.expect)
			}
		})
	}
}

func TestShouldFire_Daily(t *testing.T) {
	cases := []struct {
		name   string
		t      time.Time
		expect bool
	}{
		{"2am", time.Date(2026, 3, 21, 2, 0, 0, 0, time.UTC), true},
		{"3am", time.Date(2026, 3, 21, 3, 0, 0, 0, time.UTC), false},
		{"2:01am", time.Date(2026, 3, 21, 2, 1, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFire("@daily", tc.t); got != tc.expect {
				t.Errorf("shouldFire(@daily, %s) = %v, want %v", tc.t, got, tc.expect)
			}
		})
	}
}

func TestShouldFire_CronExpr(t *testing.T) {
	cases := []struct {
		name   string
		expr   string
		t      time.Time
		expect bool
	}{
		{"every minute", "* * * * *", time.Date(2026, 3, 21, 15, 30, 0, 0, time.UTC), true},
		{"every 5 min at 0", "*/5 * * * *", time.Date(2026, 3, 21, 15, 0, 0, 0, time.UTC), true},
		{"every 5 min at 5", "*/5 * * * *", time.Date(2026, 3, 21, 15, 5, 0, 0, time.UTC), true},
		{"every 5 min at 3", "*/5 * * * *", time.Date(2026, 3, 21, 15, 3, 0, 0, time.UTC), false},
		{"specific hour min", "30 14 * * *", time.Date(2026, 3, 21, 14, 30, 0, 0, time.UTC), true},
		{"specific hour min miss", "30 14 * * *", time.Date(2026, 3, 21, 14, 31, 0, 0, time.UTC), false},
		{"range match", "0 9-17 * * *", time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC), true},
		{"range miss", "0 9-17 * * *", time.Date(2026, 3, 21, 18, 0, 0, 0, time.UTC), false},
		{"list match", "0 8,12,18 * * *", time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC), true},
		{"list miss", "0 8,12,18 * * *", time.Date(2026, 3, 21, 13, 0, 0, 0, time.UTC), false},
		{"invalid expr", "invalid", time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldFire(tc.expr, tc.t); got != tc.expect {
				t.Errorf("shouldFire(%q, %s) = %v, want %v", tc.expr, tc.t, got, tc.expect)
			}
		})
	}
}

func TestFieldMatches(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		value  int
		min    int
		max    int
		expect bool
	}{
		{"wildcard", "*", 5, 0, 59, true},
		{"exact match", "5", 5, 0, 59, true},
		{"exact miss", "5", 6, 0, 59, false},
		{"step 0", "*/10", 0, 0, 59, true},
		{"step 10", "*/10", 10, 0, 59, true},
		{"step miss", "*/10", 7, 0, 59, false},
		{"range in", "5-10", 7, 0, 59, true},
		{"range out", "5-10", 11, 0, 59, false},
		{"list in", "1,5,10", 5, 0, 59, true},
		{"list out", "1,5,10", 6, 0, 59, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fieldMatches(tc.field, tc.value, tc.min, tc.max); got != tc.expect {
				t.Errorf("fieldMatches(%q, %d) = %v, want %v", tc.field, tc.value, got, tc.expect)
			}
		})
	}
}

func TestLockKeyFormat(t *testing.T) {
	dl := &DistributedLock{}

	key := dl.SIMKey("abc-123")
	if key != "sim:abc-123" {
		t.Errorf("SIMKey = %s, want sim:abc-123", key)
	}

	full := dl.keyFor(key)
	if full != "argus:lock:sim:abc-123" {
		t.Errorf("keyFor = %s, want argus:lock:sim:abc-123", full)
	}
}
