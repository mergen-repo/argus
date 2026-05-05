package syslog

// PAT-026 RECURRENCE prevention paired test for STORY-098 Task 5.
//
// The forwarder is a singleton background subscriber that lives or dies with
// cmd/argus/main.go. Earlier inverse-orphan recurrences (STORY-095 Gate F-A1,
// STORY-097 F-A2) caught NEW background workers that were defined in their
// package but never wired into main.go — silently inert in production.
//
// This test asserts the wiring text is present in cmd/argus/main.go. It is
// intentionally a string grep (not a build/run check): the unit test must
// fail FAST without spinning up the whole binary, and the failure mode we
// guard against is "the wiring code was removed" — exact symbol names
// matter more than runtime semantics here.

import (
	"os"
	"strings"
	"testing"
)

// TestSyslogForwarder_RegisteredAtBoot enforces that cmd/argus/main.go
// instantiates the forwarder, calls Start, and registers Stop in the
// graceful-shutdown sequence. PAT-026 RECURRENCE — see comment above.
func TestSyslogForwarder_RegisteredAtBoot(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("../../../cmd/argus/main.go")
	if err != nil {
		t.Skipf("cannot read cmd/argus/main.go (build dir layout?): %v", err)
		return
	}
	src := string(data)

	for _, want := range []string{
		"syslogForwarder",       // local variable name
		"syslog.NewForwarder",   // constructor invocation
		"syslogForwarder.Start", // start invocation
		"syslogForwarder.Stop",  // shutdown invocation (inside gracefulShutdown)
		"syslog forwarder enabled (per VAL-098 default-on gate)", // boot log line
	} {
		if !strings.Contains(src, want) {
			t.Errorf("cmd/argus/main.go missing %q — Forwarder must be wired at boot per PAT-026 RECURRENCE prevention", want)
		}
	}
}
