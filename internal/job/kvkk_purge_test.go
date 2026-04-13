package job

import (
	"encoding/json"
	"testing"

	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

// ----------------------------------------------------------------------------
// Unit tests — no DB required
// ----------------------------------------------------------------------------

func TestKVKKPurgeProcessor_Type(t *testing.T) {
	p := &KVKKPurgeProcessor{}
	if got := p.Type(); got != JobTypeKVKKPurgeDaily {
		t.Fatalf("Type() = %q, want %q", got, JobTypeKVKKPurgeDaily)
	}
}

func TestKVKKPurgeDailyRegisteredInAllJobTypes(t *testing.T) {
	found := false
	for _, jt := range AllJobTypes {
		if jt == JobTypeKVKKPurgeDaily {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("kvkk_purge_daily not registered in AllJobTypes")
	}
}

func TestKVKKPurgeJobPayload_Parse(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantDry  bool
		wantTID  bool
	}{
		{
			name:    "empty payload defaults dryRun=false, tenantID=nil",
			raw:     `{}`,
			wantDry: false,
			wantTID: false,
		},
		{
			name:    "dry_run=true",
			raw:     `{"dry_run":true}`,
			wantDry: true,
			wantTID: false,
		},
		{
			name:    "dry_run=false, tenant_id set",
			raw:     `{"dry_run":false,"tenant_id":"10000000-0000-0000-0000-000000000001"}`,
			wantDry: false,
			wantTID: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p KVKKPurgeJobPayload
			if err := json.Unmarshal([]byte(tc.raw), &p); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if p.DryRun != tc.wantDry {
				t.Errorf("DryRun = %v, want %v", p.DryRun, tc.wantDry)
			}
			if (p.TenantID != nil) != tc.wantTID {
				t.Errorf("TenantID presence = %v, want %v", p.TenantID != nil, tc.wantTID)
			}
		})
	}
}

func TestRetentionForTable_PerTableConfig(t *testing.T) {
	cfg := &store.TenantRetentionConfig{
		CDRRetentionDays:     90,
		SessionRetentionDays: 180,
		AuditRetentionDays:   730,
	}

	if got := retentionForTable(cfg, "cdr", 0); got != 90 {
		t.Errorf("cdr: got %d, want 90", got)
	}
	if got := retentionForTable(cfg, "session", 0); got != 180 {
		t.Errorf("session: got %d, want 180", got)
	}
	if got := retentionForTable(cfg, "audit", 0); got != 730 {
		t.Errorf("audit: got %d, want 730", got)
	}
}

func TestRetentionForTable_TenantScalarFallback(t *testing.T) {
	// No per-table config → falls back to tenant purge_retention_days
	got := retentionForTable(nil, "session", 60)
	if got != 60 {
		t.Errorf("tenant fallback: got %d, want 60", got)
	}
}

func TestRetentionForTable_DefaultFallback(t *testing.T) {
	// Neither per-table config nor tenant scalar → defaults to 365
	got := retentionForTable(nil, "audit", 0)
	if got != kvkkDefaultRetentionDays {
		t.Errorf("default fallback: got %d, want %d", got, kvkkDefaultRetentionDays)
	}
}

func TestRetentionForTable_ZeroConfigFieldFallsThrough(t *testing.T) {
	// CDRRetentionDays=0 in config → should fall through to tenant scalar
	cfg := &store.TenantRetentionConfig{
		CDRRetentionDays: 0,
	}
	got := retentionForTable(cfg, "cdr", 120)
	if got != 120 {
		t.Errorf("zero-field fallback: got %d, want 120", got)
	}
}

func TestKVKKPurgeResult_Structure(t *testing.T) {
	result := KVKKPurgeResult{
		DryRun: true,
		PerTenant: []kvkkPerTenantResult{
			{
				TenantID: "abc",
				Purged: map[string]int{
					"sessions":      5,
					"user_sessions": 2,
					"audit_logs":    3,
				},
				RetentionDays: map[string]int{
					"sessions":      180,
					"user_sessions": 180,
					"audit_logs":    730,
				},
			},
		},
	}

	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded KVKKPurgeResult
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !decoded.DryRun {
		t.Error("DryRun should be true after round-trip")
	}
	if len(decoded.PerTenant) != 1 {
		t.Fatalf("PerTenant len = %d, want 1", len(decoded.PerTenant))
	}
	if decoded.PerTenant[0].Purged["sessions"] != 5 {
		t.Errorf("sessions purged = %d, want 5", decoded.PerTenant[0].Purged["sessions"])
	}
}

func TestKVKKPurgeProcessor_New(t *testing.T) {
	p := NewKVKKPurgeProcessor(nil, nil, nil, nil, nil, nil, nil, zerolog.Nop())
	if p == nil {
		t.Fatal("NewKVKKPurgeProcessor returned nil")
	}
	if p.Type() != JobTypeKVKKPurgeDaily {
		t.Errorf("Type() = %q, want %q", p.Type(), JobTypeKVKKPurgeDaily)
	}
}
