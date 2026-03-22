package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestNotificationStoreConstructor(t *testing.T) {
	s := NewNotificationStore(nil)
	if s == nil {
		t.Fatal("NewNotificationStore returned nil")
	}
}

func TestNotificationConfigStoreConstructor(t *testing.T) {
	s := NewNotificationConfigStore(nil)
	if s == nil {
		t.Fatal("NewNotificationConfigStore returned nil")
	}
}

func TestCreateNotificationParams_Validation(t *testing.T) {
	p := CreateNotificationParams{
		TenantID:  uuid.New(),
		EventType: "operator.down",
		ScopeType: "system",
		Title:     "Test",
		Body:      "Body",
		Severity:  "critical",
	}

	if p.TenantID == uuid.Nil {
		t.Error("tenant_id should not be nil")
	}
	if p.EventType != "operator.down" {
		t.Errorf("event_type = %s, want operator.down", p.EventType)
	}
}

func TestListNotificationParams_Defaults(t *testing.T) {
	p := ListNotificationParams{}

	if p.Limit != 0 {
		t.Errorf("default limit = %d, want 0", p.Limit)
	}
	if p.UnreadOnly != false {
		t.Error("default unread_only should be false")
	}
	if p.Cursor != "" {
		t.Error("default cursor should be empty")
	}
}

func TestUpsertNotificationConfigParams_Fields(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()
	thresholdType := "percentage"
	thresholdValue := 80.0

	p := UpsertNotificationConfigParams{
		TenantID:       tenantID,
		UserID:         &userID,
		EventType:      "quota.warning",
		ScopeType:      "sim",
		ThresholdType:  &thresholdType,
		ThresholdValue: &thresholdValue,
		Enabled:        true,
	}

	if p.TenantID != tenantID {
		t.Errorf("tenant_id mismatch")
	}
	if *p.UserID != userID {
		t.Errorf("user_id mismatch")
	}
	if *p.ThresholdType != "percentage" {
		t.Errorf("threshold_type = %s, want percentage", *p.ThresholdType)
	}
	if *p.ThresholdValue != 80.0 {
		t.Errorf("threshold_value = %f, want 80.0", *p.ThresholdValue)
	}
}
