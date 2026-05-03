package store

// PoolKind identifies one of the three IMEI pool tables.
// MUST equal the suffix of the underlying tables: imei_whitelist / imei_greylist / imei_blacklist.
type PoolKind string

const (
	PoolWhitelist PoolKind = "whitelist"
	PoolGreylist  PoolKind = "greylist"
	PoolBlacklist PoolKind = "blacklist"
)

// EntryKind mirrors the SQL CHECK constraint on imei_*.kind.
type EntryKind = string

const (
	EntryKindFullIMEI EntryKind = "full_imei"
	EntryKindTACRange EntryKind = "tac_range"
)

// ImportedFrom mirrors the SQL CHECK constraint on imei_blacklist.imported_from.
type ImportedFrom = string

const (
	ImportedManual      ImportedFrom = "manual"
	ImportedGSMACEIR    ImportedFrom = "gsma_ceir"
	ImportedOperatorEIR ImportedFrom = "operator_eir"
)

// ValidEntryKinds mirrors the SQL CHECK constraint on imei_*.kind.
// MUST match migrations/20260508000001_imei_pools.up.sql exactly
// (validated by TestEntryKindConstSetMatchesCheckConstraint — PAT-022).
var ValidEntryKinds = []string{
	"full_imei",
	"tac_range",
}

// ValidImportedFromValues mirrors the SQL CHECK constraint on imei_blacklist.imported_from.
// MUST match migrations/20260508000001_imei_pools.up.sql exactly
// (validated by TestImportedFromConstSetMatchesCheckConstraint — PAT-022).
var ValidImportedFromValues = []string{
	"manual",
	"gsma_ceir",
	"operator_eir",
}

// ValidPoolKinds enumerates the three semantic pool buckets.
var ValidPoolKinds = []PoolKind{
	PoolWhitelist,
	PoolGreylist,
	PoolBlacklist,
}

// IsValidEntryKind reports whether s is a member of ValidEntryKinds.
func IsValidEntryKind(s string) bool {
	for _, v := range ValidEntryKinds {
		if v == s {
			return true
		}
	}
	return false
}

// IsValidImportedFrom reports whether s is a member of ValidImportedFromValues.
func IsValidImportedFrom(s string) bool {
	for _, v := range ValidImportedFromValues {
		if v == s {
			return true
		}
	}
	return false
}

// IsValidPoolKind reports whether k is a member of ValidPoolKinds.
func IsValidPoolKind(k PoolKind) bool {
	for _, v := range ValidPoolKinds {
		if v == k {
			return true
		}
	}
	return false
}

// tableNameForKind maps a PoolKind to its physical table name. The switch is
// intentional — never format-string concatenate user input into SQL identifiers.
// Returns "" for unknown kinds; callers MUST treat that as a programming error.
func tableNameForKind(k PoolKind) string {
	switch k {
	case PoolWhitelist:
		return "imei_whitelist"
	case PoolGreylist:
		return "imei_greylist"
	case PoolBlacklist:
		return "imei_blacklist"
	default:
		return ""
	}
}
