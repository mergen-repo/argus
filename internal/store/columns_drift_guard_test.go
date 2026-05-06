package store

import (
	"strings"
	"testing"
)

// PAT-006 (column list vs Scan destination drift) Recurrence Guard
// =====================================================================
//
// Background: A recurring class of latent production-blockers in this
// codebase has been a SELECT/RETURNING column list (a `xxxColumns`
// constant or string literal) drifting out-of-sync with its companion
// scan destination list (either an inline `rows.Scan(&a, &b, ...)` or a
// `scanXxx(row)` helper). The build is clean (Go can't enforce
// column-vs-Scan arity) and most unit tests pass (most don't exercise
// the drifted paths against a real DB). Then a customer demo hits an
// HTTP 500: "number of field descriptions must equal number of
// destinations, got N and M".
//
// Recurrences:
//   #1 FIX-215 (2026-04-19) — operatorColumns vs Operator.List
//   #2 FIX-242 (2026-04-26) — sessions DTO field omission
//   #3 FIX-251 (2026-04-30) — PAT-006 #3 systemic audit
//   #4 E1 Fix Loop 1 (2026-05-06) — sim.go SIMStore.List + FetchSample
//      drifted to 23 dests vs 29-col simColumns after STORY-094 added
//      6 binding columns
//
// E2 Test Hardener (D-181) sweep: this file's tests assert structural
// alignment between every `xxxColumns` constant and its companion
// `scanXxx` helper's destination count. They are pure-Go (no DB) and
// run in <1ms — safe to keep enabled in every CI run.
//
// How to maintain:
//   - When you ADD a column to `xxxColumns`, you MUST add a matching
//     destination to `scanXxx` AND bump the constant in this file.
//   - When you ADD a Scan destination to `scanXxx`, same drill in
//     reverse + bump the constant here.
//   - When the test fails, that is the message: the test caught a
//     drift before it reached production. Fix both sides + the
//     constant; do NOT silently bump the constant alone.
//
// Caveat: the comma-counting trick assumes column constants do NOT
// embed expressions with commas (e.g. `concat(a,b)`). All audited
// constants are clean as of E2. If anyone introduces such an
// expression later, this counting needs to switch to a structured
// parser — but the test will fail loudly before merging silently.

// countColumns counts comma-separated identifiers in a column list
// constant. Whitespace (including newlines) is collapsed first.
func countColumns(t *testing.T, src string) int {
	t.Helper()
	cleaned := strings.Join(strings.Fields(src), "")
	if cleaned == "" {
		t.Fatal("countColumns: empty input")
	}
	return strings.Count(cleaned, ",") + 1
}

func TestPolicyColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, policyColumns)
	const wantPolicy = 11 // scanPolicy destinations
	if got != wantPolicy {
		t.Fatalf("policyColumns/scanPolicy drift: policyColumns=%d cols, scanPolicy=%d dests.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantPolicy.",
			got, wantPolicy)
	}
}

func TestPolicyVersionColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, policyVersionColumns)
	const wantPolicyVersion = 12 // scanPolicyVersion destinations
	if got != wantPolicyVersion {
		t.Fatalf("policyVersionColumns/scanPolicyVersion drift: policyVersionColumns=%d cols, scanPolicyVersion=%d dests.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantPolicyVersion.",
			got, wantPolicyVersion)
	}
}

func TestRolloutColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, rolloutColumns)
	const wantRollout = 16 // scanRollout destinations
	if got != wantRollout {
		t.Fatalf("rolloutColumns/scanRollout drift: rolloutColumns=%d cols, scanRollout=%d dests.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantRollout.",
			got, wantRollout)
	}
}

func TestCDRColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, cdrColumns)
	const wantCDR = 16 // scanCDR destinations
	if got != wantCDR {
		t.Fatalf("cdrColumns/scanCDR drift: cdrColumns=%d cols, scanCDR=%d dests.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantCDR.",
			got, wantCDR)
	}
}

func TestIPPoolColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, ippoolColumns)
	const wantIPPool = 13 // scanIPPool destinations
	if got != wantIPPool {
		t.Fatalf("ippoolColumns/scanIPPool drift: ippoolColumns=%d cols, scanIPPool=%d dests.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantIPPool.",
			got, wantIPPool)
	}
}

func TestIPAddressColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, ipAddressColumns)
	const wantIPAddress = 10 // scanIPAddress destinations
	if got != wantIPAddress {
		t.Fatalf("ipAddressColumns/scanIPAddress drift: ipAddressColumns=%d cols, scanIPAddress=%d dests.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantIPAddress.",
			got, wantIPAddress)
	}
}

func TestIPAddressColumnsJoinedAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, ipAddressColumnsJoined)
	const wantIPAddressJoined = 13 // scanIPAddressJoined destinations
	if got != wantIPAddressJoined {
		t.Fatalf("ipAddressColumnsJoined/scanIPAddressJoined drift: cols=%d, dests=%d.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantIPAddressJoined.",
			got, wantIPAddressJoined)
	}
}

func TestRadiusSessionColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, radiusSessionColumns)
	const wantRadiusSession = 25 // scanRadiusSession destinations
	if got != wantRadiusSession {
		t.Fatalf("radiusSessionColumns/scanRadiusSession drift: cols=%d, dests=%d.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantRadiusSession.",
			got, wantRadiusSession)
	}
}

func TestNotificationColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, notificationColumns)
	const wantNotification = 18 // scanNotification destinations
	if got != wantNotification {
		t.Fatalf("notificationColumns/scanNotification drift: cols=%d, dests=%d.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantNotification.",
			got, wantNotification)
	}
}

func TestNotificationConfigColumnsAndScanCountConsistency(t *testing.T) {
	got := countColumns(t, notificationConfigColumns)
	const wantNotificationConfig = 12 // scanNotificationConfig destinations
	if got != wantNotificationConfig {
		t.Fatalf("notificationConfigColumns/scanNotificationConfig drift: cols=%d, dests=%d.\n"+
			"PAT-006 RECURRENCE class. Update both sides + bump wantNotificationConfig.",
			got, wantNotificationConfig)
	}
}
