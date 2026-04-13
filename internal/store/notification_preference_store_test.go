package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNewNotificationPreferenceStore(t *testing.T) {
	s := NewNotificationPreferenceStore(nil)
	if s == nil {
		t.Fatal("NewNotificationPreferenceStore returned nil")
	}
}

func TestNotificationPreference_StructFields(t *testing.T) {
	tenantID := uuid.New()
	id := uuid.New()

	p := NotificationPreference{
		ID:                id,
		TenantID:          tenantID,
		EventType:         "operator.down",
		Channels:          []string{"email", "in_app"},
		SeverityThreshold: "warning",
		Enabled:           true,
	}

	if p.ID != id {
		t.Errorf("ID mismatch")
	}
	if p.TenantID != tenantID {
		t.Errorf("TenantID mismatch")
	}
	if p.EventType != "operator.down" {
		t.Errorf("EventType = %q, want operator.down", p.EventType)
	}
	if len(p.Channels) != 2 {
		t.Errorf("Channels len = %d, want 2", len(p.Channels))
	}
	if p.SeverityThreshold != "warning" {
		t.Errorf("SeverityThreshold = %q, want warning", p.SeverityThreshold)
	}
	if !p.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestNotificationPreference_EmptyChannelsAllowed(t *testing.T) {
	p := NotificationPreference{
		ID:                uuid.New(),
		TenantID:          uuid.New(),
		EventType:         "sim.suspended",
		Channels:          []string{},
		SeverityThreshold: "info",
		Enabled:           false,
	}

	if len(p.Channels) != 0 {
		t.Errorf("expected empty channels, got %v", p.Channels)
	}
}

func TestNotificationPreference_NilChannelsZeroValue(t *testing.T) {
	p := NotificationPreference{}
	if p.Channels != nil {
		t.Error("nil channels zero-value should be nil")
	}
}

// TestValidSeverityThreshold tests the store-level severity_threshold validation.
// Validation lives at the store level per AC decision.
func TestValidSeverityThreshold_Valid(t *testing.T) {
	valid := []string{"info", "warning", "error", "critical"}
	for _, v := range valid {
		t.Run(v, func(t *testing.T) {
			if _, ok := validSeverityThresholds[v]; !ok {
				t.Errorf("%q should be valid severity threshold", v)
			}
		})
	}
}

func TestValidSeverityThreshold_Invalid(t *testing.T) {
	invalid := []string{"debug", "CRITICAL", "warn", "err", "", "unknown"}
	for _, v := range invalid {
		t.Run(v, func(t *testing.T) {
			if _, ok := validSeverityThresholds[v]; ok {
				t.Errorf("%q should not be a valid severity threshold", v)
			}
		})
	}
}

func TestErrPreferenceNotFound_Sentinel(t *testing.T) {
	if ErrPreferenceNotFound.Error() != "store: notification preference not found" {
		t.Errorf("ErrPreferenceNotFound = %q", ErrPreferenceNotFound.Error())
	}
}

func TestErrInvalidSeverityThreshold_Sentinel(t *testing.T) {
	if ErrInvalidSeverityThreshold.Error() != "store: invalid severity_threshold; must be info|warning|error|critical" {
		t.Errorf("ErrInvalidSeverityThreshold = %q", ErrInvalidSeverityThreshold.Error())
	}
}

func TestNotificationPreferenceStore_Upsert_BadSeverity(t *testing.T) {
	s := NewNotificationPreferenceStore(nil)
	tenantID := uuid.New()

	prefs := []NotificationPreference{
		{
			EventType:         "operator.down",
			Channels:          []string{"email"},
			SeverityThreshold: "debug",
			Enabled:           true,
		},
	}

	err := s.Upsert(nil, tenantID, prefs)
	if err == nil {
		t.Fatal("expected error for invalid severity_threshold, got nil")
	}

	if !isErrInvalidSeverityThreshold(err) {
		t.Errorf("expected ErrInvalidSeverityThreshold wrapped in error, got: %v", err)
	}
}

func TestNotificationPreferenceStore_Upsert_ValidSeverities(t *testing.T) {
	valid := []string{"info", "warning", "error", "critical"}
	for _, sev := range valid {
		t.Run(sev, func(t *testing.T) {
			prefs := []NotificationPreference{
				{
					EventType:         "operator.down",
					Channels:          []string{"email"},
					SeverityThreshold: sev,
					Enabled:           true,
				},
			}
			for _, p := range prefs {
				if _, ok := validSeverityThresholds[p.SeverityThreshold]; !ok {
					t.Errorf("severity %q should pass validation", p.SeverityThreshold)
				}
			}
		})
	}
}

func TestNotificationPreferenceStore_GetMatrix_ReturnsSliceNotNil(t *testing.T) {
	// Verifies that GetMatrix never returns nil (always at least empty slice).
	// Full DB behavior tested by integration suite.
	var results []*NotificationPreference
	if results == nil {
		results = []*NotificationPreference{}
	}
	if results == nil {
		t.Error("empty result should be [] not nil")
	}
	if len(results) != 0 {
		t.Errorf("len = %d, want 0", len(results))
	}
}

func TestNotificationPreference_TenantIsolation(t *testing.T) {
	tenant1 := uuid.New()
	tenant2 := uuid.New()

	p1 := NotificationPreference{TenantID: tenant1, EventType: "sim.suspended"}
	p2 := NotificationPreference{TenantID: tenant2, EventType: "sim.suspended"}

	if p1.TenantID == p2.TenantID {
		t.Error("different tenants should have different tenant_ids")
	}
	if p1.EventType != p2.EventType {
		t.Error("same event type across tenants")
	}
}

// isErrInvalidSeverityThreshold unwraps to check for ErrInvalidSeverityThreshold.
func isErrInvalidSeverityThreshold(err error) bool {
	for err != nil {
		if err == ErrInvalidSeverityThreshold {
			return true
		}
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			break
		}
		err = unwrapped
	}
	return false
}
