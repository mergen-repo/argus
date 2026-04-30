package admin

import "testing"

// TestUsageQuotaStatus verifies the FIX-211-aligned tier mapping used by
// the unified Tenant Usage endpoint (FIX-246). Boundary values use >= per
// the contract (and matching internal/job/quota_breach_checker.go pct >= 0.95).
func TestUsageQuotaStatus(t *testing.T) {
	tests := []struct {
		name string
		pct  float64
		want string
	}{
		{"0pct ok", 0, "ok"},
		{"49pct ok", 49.9, "ok"},
		{"50pct ok", 50.0, "ok"},
		{"79.9pct ok", 79.9, "ok"},
		{"80pct warning", 80.0, "warning"},
		{"90pct warning", 90.0, "warning"},
		{"94.9pct warning", 94.9, "warning"},
		{"95pct critical", 95.0, "critical"},
		{"99pct critical", 99.0, "critical"},
		{"100pct critical", 100.0, "critical"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := usageQuotaStatus(tc.pct)
			if got != tc.want {
				t.Errorf("usageQuotaStatus(%v) = %q, want %q", tc.pct, got, tc.want)
			}
		})
	}
}

// TestCalcUsagePct verifies clamping and zero-max guard for the int variant
// used by SIMs / sessions / api_rps quota metrics.
func TestCalcUsagePct(t *testing.T) {
	tests := []struct {
		name             string
		current, max     int
		want             float64
	}{
		{"zero max returns 0", 100, 0, 0},
		{"50pct", 500, 1000, 50},
		{"95pct boundary", 950, 1000, 95},
		{"100pct exact", 1000, 1000, 100},
		{"clamped over 100", 1500, 1000, 100},
		{"zero current", 0, 1000, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calcUsagePct(tc.current, tc.max)
			if got != tc.want {
				t.Errorf("calcUsagePct(%d, %d) = %v, want %v", tc.current, tc.max, got, tc.want)
			}
		})
	}
}

// TestCalcUsagePct64 mirrors TestCalcUsagePct for the int64 (storage_bytes)
// variant. Verifies the int64 path does not lose precision at large values
// and clamps identically.
func TestCalcUsagePct64(t *testing.T) {
	const tenGB int64 = 10 * 1024 * 1024 * 1024
	const oneTB int64 = 1024 * tenGB
	tests := []struct {
		name             string
		current, max     int64
		want             float64
	}{
		{"zero max returns 0", tenGB, 0, 0},
		{"50pct of 1TB", oneTB / 2, oneTB, 50},
		{"95pct boundary", 95 * tenGB, 100 * tenGB, 95},
		{"clamped over 100", 200 * tenGB, 100 * tenGB, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calcUsagePct64(tc.current, tc.max)
			if got != tc.want {
				t.Errorf("calcUsagePct64(%d, %d) = %v, want %v", tc.current, tc.max, got, tc.want)
			}
		})
	}
}

// TestTenantUsageItemShape asserts the struct compiles with all required
// fields present. The wire contract is enforced by FE TS compilation; any
// field rename here will surface as a TS error in web/src/types/admin.ts.
// Storage metric uses int64 (BIGINT) — see F-A9.
func TestTenantUsageItemShape(t *testing.T) {
	item := tenantUsageItem{}
	_ = item.SIMs.Status
	_ = item.Sessions.Status
	_ = item.APIRPS.Status
	_ = item.StorageBytes.Status
	// Compile-time guard: storage Max is int64.
	var _ int64 = item.StorageBytes.Max
	var _ int64 = item.StorageBytes.Current
	_ = item.OpenBreachCount
	_ = item.CDRBytes30d
	_ = item.UserCount
}
