package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// bindingMigrationFilename is the migration that adds the device-binding
// columns + CHECK constraints. PAT-022 structural tests below re-parse it at
// runtime to detect Go-vs-SQL enum drift.
const bindingMigrationFilename = "20260507000001_sim_device_binding_columns.up.sql"

// extractEnumValues parses the first `<column> IN ('a','b',...)` tuple in the
// supplied SQL where the column matches the given name (case-sensitive). Returns
// the de-quoted enum members in source order, or nil if no match is found.
func extractEnumValues(sql, column string) []string {
	pattern := column + `\s+IN\s*\(([^)]*)\)`
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(sql)
	if len(m) < 2 {
		return nil
	}
	parts := strings.Split(m[1], ",")
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		v = strings.Trim(v, "'\"")
		if v != "" {
			values = append(values, v)
		}
	}
	return values
}

// loadBindingMigration reads the binding migration file and returns its
// contents. Fails the test if the file cannot be read.
func loadBindingMigration(t *testing.T) string {
	t.Helper()
	path := filepath.Join(migrationsDir(t), bindingMigrationFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// assertSetEqual asserts that two string slices have identical members
// (order-independent). On mismatch, reports both sides for easy diffing.
func assertSetEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s: set mismatch\n  got:  %v\n  want: %v", label, g, w)
	}
}

// TestBindingModeConstSetMatchesCheckConstraint enforces PAT-022: the Go-side
// ValidBindingModes slice MUST be a set-equal mirror of the SQL CHECK
// constraint on sims.binding_mode. Any drift between the two is a hard
// failure — this is the only guard against an enum value diverging silently.
func TestBindingModeConstSetMatchesCheckConstraint(t *testing.T) {
	sql := loadBindingMigration(t)
	sqlValues := extractEnumValues(sql, "binding_mode")
	if len(sqlValues) == 0 {
		t.Fatalf("could not extract binding_mode IN (...) tuple from migration")
	}
	assertSetEqual(t, "binding_mode", ValidBindingModes, sqlValues)
}

// TestBindingStatusConstSetMatchesCheckConstraint enforces PAT-022 for
// binding_status (mirror of TestBindingModeConstSetMatchesCheckConstraint).
func TestBindingStatusConstSetMatchesCheckConstraint(t *testing.T) {
	sql := loadBindingMigration(t)
	sqlValues := extractEnumValues(sql, "binding_status")
	if len(sqlValues) == 0 {
		t.Fatalf("could not extract binding_status IN (...) tuple from migration")
	}
	assertSetEqual(t, "binding_status", ValidBindingStatuses, sqlValues)
}

// TestIsValidBindingMode_TableDriven exercises the membership helper for all
// canonical values plus a sentinel non-member.
func TestIsValidBindingMode_TableDriven(t *testing.T) {
	for _, m := range ValidBindingModes {
		if !IsValidBindingMode(m) {
			t.Errorf("IsValidBindingMode(%q) = false, want true", m)
		}
	}
	if IsValidBindingMode("not-a-mode") {
		t.Errorf("IsValidBindingMode(\"not-a-mode\") = true, want false")
	}
	if IsValidBindingMode("") {
		t.Errorf("IsValidBindingMode(\"\") = true, want false")
	}
}

// TestIsValidBindingStatus_TableDriven mirrors TestIsValidBindingMode_TableDriven.
func TestIsValidBindingStatus_TableDriven(t *testing.T) {
	for _, s := range ValidBindingStatuses {
		if !IsValidBindingStatus(s) {
			t.Errorf("IsValidBindingStatus(%q) = false, want true", s)
		}
	}
	if IsValidBindingStatus("not-a-status") {
		t.Errorf("IsValidBindingStatus(\"not-a-status\") = true, want false")
	}
	if IsValidBindingStatus("") {
		t.Errorf("IsValidBindingStatus(\"\") = true, want false")
	}
}
