package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type mockAuditor struct {
	calls []audit.CreateEntryParams
}

func (m *mockAuditor) CreateEntry(_ context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	m.calls = append(m.calls, p)
	return &audit.Entry{}, nil
}

func testCfg(current, previous string) *config.Config {
	return &config.Config{
		JWTSecret:         current,
		JWTSecretPrevious: previous,
	}
}

func TestCheckAndAuditRotation_NoPrevious(t *testing.T) {
	auditor := &mockAuditor{}
	cfg := testCfg("current-secret-key-must-be-at-least-32-chars", "")
	err := CheckAndAuditRotation(context.Background(), cfg, auditor, uuid.New().String(), zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auditor.calls) != 0 {
		t.Errorf("expected 0 audit entries, got %d", len(auditor.calls))
	}
}

func TestCheckAndAuditRotation_WithPrevious(t *testing.T) {
	auditor := &mockAuditor{}
	current := "current-secret-key-must-be-at-least-32-chars"
	previous := "previous-secret-key-must-be-at-least-32chars"
	bootID := uuid.New().String()
	cfg := testCfg(current, previous)

	err := CheckAndAuditRotation(context.Background(), cfg, auditor, bootID, zerolog.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(auditor.calls) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditor.calls))
	}

	entry := auditor.calls[0]
	if entry.Action != "jwt_key_rotation_detected" {
		t.Errorf("expected action jwt_key_rotation_detected, got %q", entry.Action)
	}
	if entry.EntityType != "security" {
		t.Errorf("expected entity_type security, got %q", entry.EntityType)
	}
	if entry.EntityID != "jwt_signing_key" {
		t.Errorf("expected entity_id jwt_signing_key, got %q", entry.EntityID)
	}
	if entry.CorrelationID == nil {
		t.Error("expected correlation_id to be set")
	} else if entry.CorrelationID.String() != bootID {
		t.Errorf("expected correlation_id %s, got %s", bootID, entry.CorrelationID.String())
	}
	if entry.TenantID != uuid.Nil {
		t.Errorf("expected tenant_id to be nil UUID, got %s", entry.TenantID)
	}
	if entry.AfterData == nil {
		t.Error("expected after_data to be set")
	}
}

func TestCheckAndAuditRotation_FingerprintFormat(t *testing.T) {
	current := "current-secret-key-must-be-at-least-32-chars"
	previous := "previous-secret-key-must-be-at-least-32chars"
	fp := KeyFingerprint(current)
	if !strings.HasPrefix(fp, "sha256:") {
		t.Errorf("fingerprint should start with sha256:, got %q", fp)
	}
	if len(fp) != 19 {
		t.Errorf("fingerprint length should be 19, got %d", len(fp))
	}
	if strings.Contains(fp, current) {
		t.Error("fingerprint must not contain raw secret")
	}
	if strings.Contains(fp, previous) {
		t.Error("fingerprint must not contain raw previous secret")
	}
}

func TestCheckAndAuditRotation_FingerprintsNeverLeakSecret(t *testing.T) {
	auditor := &mockAuditor{}
	current := "current-secret-key-must-be-at-least-32-chars"
	previous := "previous-secret-key-must-be-at-least-32chars"
	cfg := testCfg(current, previous)

	_ = CheckAndAuditRotation(context.Background(), cfg, auditor, uuid.New().String(), zerolog.Nop())

	if len(auditor.calls) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditor.calls))
	}

	afterDataStr := string(auditor.calls[0].AfterData)
	if strings.Contains(afterDataStr, current) {
		t.Error("after_data must not contain raw current secret")
	}
	if strings.Contains(afterDataStr, previous) {
		t.Error("after_data must not contain raw previous secret")
	}
}
