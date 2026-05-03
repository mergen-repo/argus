package store

// ValidBindingModes mirrors the SQL CHECK constraint on sims.binding_mode.
// MUST match migrations/20260507000001_sim_device_binding_columns.up.sql exactly
// (validated by TestBindingModeConstSetMatchesCheckConstraint — PAT-022).
var ValidBindingModes = []string{
	"strict",
	"allowlist",
	"first-use",
	"tac-lock",
	"grace-period",
	"soft",
}

// ValidBindingStatuses mirrors the SQL CHECK constraint on sims.binding_status.
// MUST match migrations/20260507000001_sim_device_binding_columns.up.sql exactly
// (validated by TestBindingStatusConstSetMatchesCheckConstraint — PAT-022).
var ValidBindingStatuses = []string{
	"verified",
	"pending",
	"mismatch",
	"unbound",
	"disabled",
}

// IsValidBindingMode reports whether s is a member of ValidBindingModes.
func IsValidBindingMode(s string) bool {
	for _, v := range ValidBindingModes {
		if v == s {
			return true
		}
	}
	return false
}

// IsValidBindingStatus reports whether s is a member of ValidBindingStatuses.
func IsValidBindingStatus(s string) bool {
	for _, v := range ValidBindingStatuses {
		if v == s {
			return true
		}
	}
	return false
}
