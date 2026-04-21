package events

import (
	"testing"

	"github.com/btopcu/argus/internal/severity"
)

func TestCatalog_HasMinimumEntries(t *testing.T) {
	const minEntries = 14
	if len(Catalog) < minEntries {
		t.Fatalf("Catalog has %d entries, want >= %d", len(Catalog), minEntries)
	}
}

func TestCatalog_EveryEntry_HasCanonicalSeverity(t *testing.T) {
	for _, e := range Catalog {
		if e.Type == "" {
			t.Errorf("empty Type in entry: %+v", e)
		}
		if e.Source == "" {
			t.Errorf("empty Source in entry: type=%q", e.Type)
		}
		if err := severity.Validate(e.DefaultSeverity); err != nil {
			t.Errorf("entry type=%q has invalid default_severity=%q: %v", e.Type, e.DefaultSeverity, err)
		}
		if e.MetaSchema == nil {
			t.Errorf("entry type=%q has nil MetaSchema (use empty map)", e.Type)
		}
	}
}

func TestCatalog_TypesUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, e := range Catalog {
		if seen[e.Type] {
			t.Errorf("duplicate type in catalog: %q", e.Type)
		}
		seen[e.Type] = true
	}
}

func TestCatalog_ContainsCoreSubjects(t *testing.T) {
	required := []string{
		"session.started",
		"session.updated",
		"session.ended",
		"sim.state_changed",
		"operator.health_changed",
		"operator_down",
		"anomaly.detected",
		"policy.updated",
		"policy.rollout_progress",
		"ip.reclaimed",
		"ip.released",
		"sla.report.generated",
		"notification.dispatch",
		"nats_consumer_lag",
	}
	index := make(map[string]bool)
	for _, e := range Catalog {
		index[e.Type] = true
	}
	for _, typ := range required {
		if !index[typ] {
			t.Errorf("required catalog type missing: %q", typ)
		}
	}
}
