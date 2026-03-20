package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAPIKeyStruct(t *testing.T) {
	k := APIKey{
		ID:                 uuid.New(),
		TenantID:           uuid.New(),
		Name:               "test-key",
		KeyPrefix:          "ab",
		KeyHash:            "sha256hash",
		Scopes:             []string{"sims:read", "cdrs:*"},
		RateLimitPerMinute: 500,
		RateLimitPerHour:   15000,
		UsageCount:         42,
		CreatedAt:          time.Now(),
	}

	if k.Name != "test-key" {
		t.Errorf("Name = %q, want %q", k.Name, "test-key")
	}
	if k.KeyPrefix != "ab" {
		t.Errorf("KeyPrefix = %q, want %q", k.KeyPrefix, "ab")
	}
	if len(k.Scopes) != 2 {
		t.Errorf("Scopes length = %d, want 2", len(k.Scopes))
	}
	if k.RateLimitPerMinute != 500 {
		t.Errorf("RateLimitPerMinute = %d, want 500", k.RateLimitPerMinute)
	}
	if k.RateLimitPerHour != 15000 {
		t.Errorf("RateLimitPerHour = %d, want 15000", k.RateLimitPerHour)
	}
	if k.UsageCount != 42 {
		t.Errorf("UsageCount = %d, want 42", k.UsageCount)
	}
	if k.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil")
	}
	if k.RevokedAt != nil {
		t.Error("RevokedAt should be nil")
	}
	if k.PreviousKeyHash != nil {
		t.Error("PreviousKeyHash should be nil")
	}
	if k.KeyRotatedAt != nil {
		t.Error("KeyRotatedAt should be nil")
	}
}

func TestCreateAPIKeyParams(t *testing.T) {
	p := CreateAPIKeyParams{
		Name:               "integration-key",
		KeyPrefix:          "cd",
		KeyHash:            "hash123",
		Scopes:             []string{"*"},
		RateLimitPerMinute: 1000,
		RateLimitPerHour:   30000,
	}

	if p.Name != "integration-key" {
		t.Errorf("Name = %q, want %q", p.Name, "integration-key")
	}
	if p.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil")
	}
	if p.CreatedBy != nil {
		t.Error("CreatedBy should be nil")
	}
}

func TestUpdateAPIKeyParams(t *testing.T) {
	name := "updated-name"
	scopes := []string{"sims:read"}
	perMin := 2000
	perHour := 60000

	p := UpdateAPIKeyParams{
		Name:               &name,
		Scopes:             &scopes,
		RateLimitPerMinute: &perMin,
		RateLimitPerHour:   &perHour,
	}

	if *p.Name != "updated-name" {
		t.Errorf("Name = %q, want %q", *p.Name, "updated-name")
	}
	if len(*p.Scopes) != 1 {
		t.Errorf("Scopes length = %d, want 1", len(*p.Scopes))
	}
	if *p.RateLimitPerMinute != 2000 {
		t.Errorf("RateLimitPerMinute = %d, want 2000", *p.RateLimitPerMinute)
	}
}

func TestAPIKeyRotationFields(t *testing.T) {
	now := time.Now()
	prevHash := "prev_hash_value"
	k := APIKey{
		ID:              uuid.New(),
		TenantID:        uuid.New(),
		Name:            "rotated-key",
		KeyPrefix:       "ef",
		KeyHash:         "new_hash",
		PreviousKeyHash: &prevHash,
		KeyRotatedAt:    &now,
		Scopes:          []string{"*"},
		CreatedAt:       now,
	}

	if k.PreviousKeyHash == nil {
		t.Fatal("PreviousKeyHash should not be nil")
	}
	if *k.PreviousKeyHash != "prev_hash_value" {
		t.Errorf("PreviousKeyHash = %q, want %q", *k.PreviousKeyHash, "prev_hash_value")
	}
	if k.KeyRotatedAt == nil {
		t.Fatal("KeyRotatedAt should not be nil")
	}
}
