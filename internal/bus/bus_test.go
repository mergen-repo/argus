package bus

import (
	"encoding/json"
	"testing"
)

type testEvent struct {
	Type     string `json:"type"`
	EntityID string `json:"entity_id"`
	Action   string `json:"action"`
}

func TestSubjectConstants(t *testing.T) {
	subjects := map[string]string{
		"SubjectSessionStarted":        SubjectSessionStarted,
		"SubjectSessionUpdated":        SubjectSessionUpdated,
		"SubjectSessionEnded":          SubjectSessionEnded,
		"SubjectSIMUpdated":            SubjectSIMUpdated,
		"SubjectPolicyChanged":         SubjectPolicyChanged,
		"SubjectOperatorHealthChanged": SubjectOperatorHealthChanged,
		"SubjectNotification":          SubjectNotification,
		"SubjectAlertTriggered":        SubjectAlertTriggered,
		"SubjectJobQueue":              SubjectJobQueue,
		"SubjectJobCompleted":          SubjectJobCompleted,
		"SubjectJobProgress":           SubjectJobProgress,
		"SubjectCacheInvalidate":       SubjectCacheInvalidate,
	}

	for name, subject := range subjects {
		if subject == "" {
			t.Errorf("subject %s is empty", name)
		}
	}
}

func TestStreamConstants(t *testing.T) {
	if StreamEvents != "EVENTS" {
		t.Errorf("expected StreamEvents=EVENTS, got %s", StreamEvents)
	}
	if StreamJobs != "JOBS" {
		t.Errorf("expected StreamJobs=JOBS, got %s", StreamJobs)
	}
}

func TestEventSerialization(t *testing.T) {
	event := testEvent{
		Type:     "sim.updated",
		EntityID: "550e8400-e29b-41d4-a716-446655440000",
		Action:   "state_change",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded testEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != event.Type {
		t.Errorf("type mismatch: %s != %s", decoded.Type, event.Type)
	}
	if decoded.EntityID != event.EntityID {
		t.Errorf("entity_id mismatch: %s != %s", decoded.EntityID, event.EntityID)
	}
	if decoded.Action != event.Action {
		t.Errorf("action mismatch: %s != %s", decoded.Action, event.Action)
	}
}

func TestSubjectPrefixes(t *testing.T) {
	eventSubjects := []string{
		SubjectSessionStarted,
		SubjectSessionUpdated,
		SubjectSessionEnded,
		SubjectSIMUpdated,
		SubjectPolicyChanged,
		SubjectOperatorHealthChanged,
		SubjectNotification,
		SubjectAlertTriggered,
	}

	for _, s := range eventSubjects {
		if len(s) < 14 || s[:13] != "argus.events." {
			t.Errorf("event subject %q should start with argus.events.", s)
		}
	}

	jobSubjects := []string{
		SubjectJobQueue,
		SubjectJobCompleted,
		SubjectJobProgress,
	}

	for _, s := range jobSubjects {
		if len(s) < 12 || s[:11] != "argus.jobs." {
			t.Errorf("job subject %q should start with argus.jobs.", s)
		}
	}
}
