package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// imeiPoolMigrationFilename is the migration that creates the three IMEI pool
// tables and their CHECK constraints. PAT-022 structural tests below re-parse
// it at runtime to detect Go-vs-SQL enum drift.
const imeiPoolMigrationFilename = "20260508000001_imei_pools.up.sql"

// loadIMEIPoolMigration reads the imei pool migration file and returns its
// contents. Fails the test if the file cannot be read.
func loadIMEIPoolMigration(t *testing.T) string {
	t.Helper()
	path := filepath.Join(migrationsDir(t), imeiPoolMigrationFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// extractAllEnumValues parses ALL `<column> IN ('a','b',...)` tuples in the
// supplied SQL where the column matches the given name. Returns the union
// of all such tuples (set semantics). Use this for columns that appear in
// multiple CREATE TABLE statements (e.g., `kind` across the three pool tables).
func extractAllEnumValues(sql, column string) []string {
	pattern := column + `\s+IN\s*\(([^)]*)\)`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(sql, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var values []string
	for _, m := range matches {
		for _, p := range strings.Split(m[1], ",") {
			v := strings.TrimSpace(p)
			v = strings.Trim(v, "'\"")
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			values = append(values, v)
		}
	}
	return values
}

// TestEntryKindConstSetMatchesCheckConstraint enforces PAT-022: the Go-side
// ValidEntryKinds slice MUST be a set-equal mirror of the SQL CHECK constraint
// on imei_*.kind. Drift is a hard failure.
func TestEntryKindConstSetMatchesCheckConstraint(t *testing.T) {
	sql := loadIMEIPoolMigration(t)
	sqlValues := extractAllEnumValues(sql, "kind")
	if len(sqlValues) == 0 {
		t.Fatalf("could not extract kind IN (...) tuple from migration")
	}
	assertSetEqual(t, "imei_pool.kind", ValidEntryKinds, sqlValues)
}

// TestImportedFromConstSetMatchesCheckConstraint enforces PAT-022: the Go-side
// ValidImportedFromValues slice MUST be a set-equal mirror of the SQL CHECK
// constraint on imei_blacklist.imported_from.
func TestImportedFromConstSetMatchesCheckConstraint(t *testing.T) {
	sql := loadIMEIPoolMigration(t)
	sqlValues := extractAllEnumValues(sql, "imported_from")
	if len(sqlValues) == 0 {
		t.Fatalf("could not extract imported_from IN (...) tuple from migration")
	}
	assertSetEqual(t, "imei_blacklist.imported_from", ValidImportedFromValues, sqlValues)
}

// TestPoolKindEnumeration table-tests IsValidPoolKind for all canonical values
// plus a sentinel non-member. Also verifies tableNameForKind mapping.
func TestPoolKindEnumeration(t *testing.T) {
	cases := []struct {
		k       PoolKind
		valid   bool
		tableNm string
	}{
		{PoolWhitelist, true, "imei_whitelist"},
		{PoolGreylist, true, "imei_greylist"},
		{PoolBlacklist, true, "imei_blacklist"},
		{PoolKind("not-a-pool"), false, ""},
		{PoolKind(""), false, ""},
	}
	for _, tc := range cases {
		if got := IsValidPoolKind(tc.k); got != tc.valid {
			t.Errorf("IsValidPoolKind(%q) = %v, want %v", tc.k, got, tc.valid)
		}
		if got := tableNameForKind(tc.k); got != tc.tableNm {
			t.Errorf("tableNameForKind(%q) = %q, want %q", tc.k, got, tc.tableNm)
		}
	}
}

// TestIsValidEntryKind_TableDriven exercises membership for all canonical
// values plus a sentinel non-member.
func TestIsValidEntryKind_TableDriven(t *testing.T) {
	for _, k := range ValidEntryKinds {
		if !IsValidEntryKind(k) {
			t.Errorf("IsValidEntryKind(%q) = false, want true", k)
		}
	}
	if IsValidEntryKind("not-a-kind") {
		t.Errorf("IsValidEntryKind(\"not-a-kind\") = true, want false")
	}
	if IsValidEntryKind("") {
		t.Errorf("IsValidEntryKind(\"\") = true, want false")
	}
}

// TestIsValidImportedFrom_TableDriven mirrors TestIsValidEntryKind_TableDriven.
func TestIsValidImportedFrom_TableDriven(t *testing.T) {
	for _, s := range ValidImportedFromValues {
		if !IsValidImportedFrom(s) {
			t.Errorf("IsValidImportedFrom(%q) = false, want true", s)
		}
	}
	if IsValidImportedFrom("invalid") {
		t.Errorf("IsValidImportedFrom(\"invalid\") = true, want false")
	}
	if IsValidImportedFrom("") {
		t.Errorf("IsValidImportedFrom(\"\") = true, want false")
	}
}
