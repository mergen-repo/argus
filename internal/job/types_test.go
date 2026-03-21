package job

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestAllJobTypes_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for _, jt := range AllJobTypes {
		if seen[jt] {
			t.Errorf("duplicate job type: %s", jt)
		}
		seen[jt] = true
	}
}

func TestAllJobTypes_NotEmpty(t *testing.T) {
	if len(AllJobTypes) == 0 {
		t.Fatal("AllJobTypes is empty")
	}
}

func TestJobTypeConstants(t *testing.T) {
	expected := map[string]string{
		"bulk_sim_import":         JobTypeBulkImport,
		"bulk_session_disconnect": JobTypeBulkDisconnect,
		"bulk_state_change":       JobTypeBulkStateChange,
		"bulk_policy_assign":      JobTypeBulkPolicyAssign,
		"bulk_esim_switch":        JobTypeBulkEsimSwitch,
		"ota_command":             JobTypeOTACommand,
		"purge_sweep":             JobTypePurgeSweep,
		"ip_reclaim":              JobTypeIPReclaim,
		"sla_report":              JobTypeSLAReport,
		"policy_dry_run":          JobTypePolicyDryRun,
		"policy_rollout_stage":    JobTypeRolloutStage,
	}

	for val, constant := range expected {
		if constant != val {
			t.Errorf("JobType constant for %q = %q, want %q", val, constant, val)
		}
	}
}

func TestStubProcessor_Type(t *testing.T) {
	stub := NewStubProcessor("test_type", nil, nil, zerolog.Nop())
	if stub.Type() != "test_type" {
		t.Errorf("Type() = %s, want test_type", stub.Type())
	}
}
