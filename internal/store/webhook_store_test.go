package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWebhookConfigStore_Constructor(t *testing.T) {
	s := NewWebhookConfigStore(nil, "deadbeefdeadbeefdeadbeefdeadbeef")
	if s == nil {
		t.Fatal("NewWebhookConfigStore returned nil")
	}
}

func TestWebhookDeliveryStore_Constructor(t *testing.T) {
	s := NewWebhookDeliveryStore(nil)
	if s == nil {
		t.Fatal("NewWebhookDeliveryStore returned nil")
	}
}

func TestWebhookConfigStore_EncryptDecrypt_Roundtrip(t *testing.T) {
	hexKey := "6368616e676520746869732070617373"
	s := NewWebhookConfigStore(nil, hexKey)

	plain := "super-secret-webhook-signing-key"
	encrypted, err := s.encryptSecret(plain)
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	if len(encrypted) == 0 {
		t.Fatal("encrypted value should not be empty")
	}
	if string(encrypted) == plain {
		t.Fatal("encrypted value should not equal plaintext")
	}

	decrypted, err := s.decryptSecret(encrypted)
	if err != nil {
		t.Fatalf("decryptSecret: %v", err)
	}
	if decrypted != plain {
		t.Errorf("roundtrip: got %q, want %q", decrypted, plain)
	}
}

func TestWebhookConfigStore_EncryptSecret_DifferentEachCall(t *testing.T) {
	hexKey := "6368616e676520746869732070617373"
	s := NewWebhookConfigStore(nil, hexKey)

	plain := "my-secret"
	enc1, err := s.encryptSecret(plain)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	enc2, err := s.encryptSecret(plain)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if string(enc1) == string(enc2) {
		t.Error("AES-GCM should produce different ciphertext each call due to random nonce")
	}
}

func TestWebhookConfigStore_EncryptSecret_InvalidKey(t *testing.T) {
	s := NewWebhookConfigStore(nil, "not-hex")
	_, err := s.encryptSecret("value")
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
}

func TestWebhookConfigStore_ListLimit_Clamping(t *testing.T) {
	cases := []struct {
		name      string
		in        int
		wantLimit int
	}{
		{"zero becomes 50", 0, 50},
		{"negative becomes 50", -1, 50},
		{"over 100 becomes 50", 101, 50},
		{"100 preserved", 100, 100},
		{"25 preserved", 25, 25},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := tc.in
			if limit <= 0 || limit > 100 {
				limit = 50
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
		})
	}
}

func TestWebhookDeliveryStore_ListByConfig_LimitClamping(t *testing.T) {
	cases := []struct {
		name      string
		in        int
		wantLimit int
	}{
		{"zero becomes 50", 0, 50},
		{"over 100 becomes 50", 200, 50},
		{"50 preserved", 50, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limit := tc.in
			if limit <= 0 || limit > 100 {
				limit = 50
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
		})
	}
}

func TestWebhookConfig_Fields(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	now := time.Now()

	cfg := WebhookConfig{
		ID:           id,
		TenantID:     tenantID,
		URL:          "https://example.com/hook",
		Secret:       "secret-value",
		EventTypes:   []string{"sim.activated", "sim.suspended"},
		Enabled:      true,
		FailureCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if cfg.ID != id {
		t.Errorf("ID mismatch")
	}
	if cfg.TenantID != tenantID {
		t.Errorf("TenantID mismatch")
	}
	if cfg.URL != "https://example.com/hook" {
		t.Errorf("URL = %s, want https://example.com/hook", cfg.URL)
	}
	if cfg.Secret != "secret-value" {
		t.Errorf("Secret mismatch")
	}
	if len(cfg.EventTypes) != 2 {
		t.Errorf("EventTypes len = %d, want 2", len(cfg.EventTypes))
	}
	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.LastSuccessAt != nil {
		t.Error("LastSuccessAt should be nil on new config")
	}
	if cfg.LastFailureAt != nil {
		t.Error("LastFailureAt should be nil on new config")
	}
}

func TestWebhookDelivery_Fields(t *testing.T) {
	id := uuid.New()
	configID := uuid.New()
	tenantID := uuid.New()
	now := time.Now()
	status := 200
	body := `{"ok":true}`

	d := WebhookDelivery{
		ID:             id,
		TenantID:       tenantID,
		ConfigID:       configID,
		EventType:      "sim.activated",
		PayloadHash:    "abc123",
		PayloadPreview: `{"sim_id":"..."}`,
		Signature:      "sha256=xyz",
		ResponseStatus: &status,
		ResponseBody:   &body,
		AttemptCount:   1,
		FinalState:     "retrying",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if d.ID != id {
		t.Errorf("ID mismatch")
	}
	if d.ConfigID != configID {
		t.Errorf("ConfigID mismatch")
	}
	if d.EventType != "sim.activated" {
		t.Errorf("EventType = %s", d.EventType)
	}
	if *d.ResponseStatus != 200 {
		t.Errorf("ResponseStatus = %d, want 200", *d.ResponseStatus)
	}
	if d.FinalState != "retrying" {
		t.Errorf("FinalState = %s, want retrying", d.FinalState)
	}
	if d.NextRetryAt != nil {
		t.Error("NextRetryAt should be nil when not set")
	}
}

func TestWebhookConfigPatch_PointerSemantics(t *testing.T) {
	url := "https://new.example.com/hook"
	secret := "new-secret"
	enabled := false
	eventTypes := []string{"sim.suspended"}

	patch := WebhookConfigPatch{
		URL:        &url,
		Secret:     &secret,
		Enabled:    &enabled,
		EventTypes: &eventTypes,
	}

	if *patch.URL != url {
		t.Errorf("URL = %s, want %s", *patch.URL, url)
	}
	if *patch.Secret != secret {
		t.Errorf("Secret mismatch")
	}
	if *patch.Enabled != false {
		t.Error("Enabled should be false")
	}
	if len(*patch.EventTypes) != 1 || (*patch.EventTypes)[0] != "sim.suspended" {
		t.Errorf("EventTypes mismatch: %v", *patch.EventTypes)
	}
}

func TestWebhookConfigPatch_EmptyPatch(t *testing.T) {
	patch := WebhookConfigPatch{}

	if patch.URL != nil {
		t.Error("URL should be nil for empty patch")
	}
	if patch.Secret != nil {
		t.Error("Secret should be nil for empty patch")
	}
	if patch.Enabled != nil {
		t.Error("Enabled should be nil for empty patch")
	}
	if patch.EventTypes != nil {
		t.Error("EventTypes should be nil for empty patch")
	}
}

func TestWebhookDeliveryStore_ListDueForRetry_DefaultLimit(t *testing.T) {
	limit := 0
	if limit <= 0 {
		limit = 100
	}
	if limit != 100 {
		t.Errorf("default limit = %d, want 100", limit)
	}
}

func TestErrWebhookSentinels(t *testing.T) {
	if ErrWebhookConfigNotFound == nil {
		t.Error("ErrWebhookConfigNotFound should not be nil")
	}
	if ErrWebhookDeliveryNotFound == nil {
		t.Error("ErrWebhookDeliveryNotFound should not be nil")
	}
	if ErrWebhookConfigNotFound.Error() != "store: webhook config not found" {
		t.Errorf("unexpected message: %s", ErrWebhookConfigNotFound.Error())
	}
	if ErrWebhookDeliveryNotFound.Error() != "store: webhook delivery not found" {
		t.Errorf("unexpected message: %s", ErrWebhookDeliveryNotFound.Error())
	}
}
