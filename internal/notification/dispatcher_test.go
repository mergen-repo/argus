package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

func makeConfig(tenantID uuid.UUID, url, secret string) *store.WebhookConfig {
	return &store.WebhookConfig{
		ID:         uuid.New(),
		TenantID:   tenantID,
		URL:        url,
		Secret:     secret,
		EventTypes: []string{"test.event"},
		Enabled:    true,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestSendWebhook_CorrectHMAC(t *testing.T) {
	secret := "test-secret-xyz"
	payload := []byte(`{"event":"test.event","data":"hello"}`)

	var receivedSig string
	var receivedEvent string
	var receivedTimestamp string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Argus-Signature")
		receivedEvent = r.Header.Get("X-Argus-Event")
		receivedTimestamp = r.Header.Get("X-Argus-Timestamp")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := makeConfig(uuid.New(), srv.URL, secret)
	client := &http.Client{Timeout: 5 * time.Second}
	outcome := SendWebhook(context.Background(), client, cfg, "test.event", payload)

	if !outcome.Success {
		t.Fatalf("expected success, got err=%v statusCode=%d", outcome.Err, outcome.StatusCode)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if receivedSig != expectedSig {
		t.Errorf("signature = %q, want %q", receivedSig, expectedSig)
	}
	if receivedEvent != "test.event" {
		t.Errorf("X-Argus-Event = %q, want %q", receivedEvent, "test.event")
	}
	if receivedTimestamp == "" {
		t.Error("X-Argus-Timestamp header should not be empty")
	}

	if outcome.Signature == "" {
		t.Error("outcome.Signature should not be empty")
	}
	if outcome.PayloadHash == "" {
		t.Error("outcome.PayloadHash should not be empty")
	}
	if outcome.DurationMs < 0 {
		t.Error("DurationMs should be >= 0")
	}
}

func TestSendWebhook_FailurePath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := makeConfig(uuid.New(), srv.URL, "secret")
	client := &http.Client{Timeout: 5 * time.Second}
	outcome := SendWebhook(context.Background(), client, cfg, "test.event", []byte(`{}`))

	if outcome.Success {
		t.Error("expected failure for 500 response")
	}
	if outcome.StatusCode != 500 {
		t.Errorf("status = %d, want 500", outcome.StatusCode)
	}
}

func TestSendWebhook_TimeoutPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := makeConfig(uuid.New(), srv.URL, "secret")
	client := &http.Client{Timeout: 10 * time.Millisecond}
	outcome := SendWebhook(context.Background(), client, cfg, "test.event", []byte(`{}`))

	if outcome.Success {
		t.Error("expected failure for timed-out request")
	}
	if outcome.Err == nil {
		t.Error("expected non-nil error for timed-out request")
	}
}

type inMemConfigStore struct {
	configs   []*store.WebhookConfig
	successes []uuid.UUID
	failures  []uuid.UUID
}

func (s *inMemConfigStore) ListEnabledByEventType(_ context.Context, tenantID uuid.UUID, eventType string) ([]*store.WebhookConfig, error) {
	var out []*store.WebhookConfig
	for _, c := range s.configs {
		if c.TenantID == tenantID && c.Enabled {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *inMemConfigStore) Get(_ context.Context, id uuid.UUID) (*store.WebhookConfig, error) {
	for _, c := range s.configs {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, store.ErrWebhookConfigNotFound
}

func (s *inMemConfigStore) BumpSuccess(_ context.Context, id uuid.UUID, _ time.Time) error {
	s.successes = append(s.successes, id)
	return nil
}

func (s *inMemConfigStore) BumpFailure(_ context.Context, id uuid.UUID, _ time.Time) error {
	s.failures = append(s.failures, id)
	return nil
}

type inMemDeliveryStore struct {
	inserted []*store.WebhookDelivery
}

func (s *inMemDeliveryStore) Insert(_ context.Context, d *store.WebhookDelivery) (*store.WebhookDelivery, error) {
	cp := *d
	cp.ID = uuid.New()
	cp.CreatedAt = time.Now()
	cp.UpdatedAt = time.Now()
	s.inserted = append(s.inserted, &cp)
	return &cp, nil
}

func (s *inMemDeliveryStore) GetByID(_ context.Context, id uuid.UUID) (*store.WebhookDelivery, error) {
	for _, d := range s.inserted {
		if d.ID == id {
			return d, nil
		}
	}
	return nil, store.ErrWebhookDeliveryNotFound
}

func TestDispatcher_DispatchToConfigs_TwoConfigs(t *testing.T) {
	tenantID := uuid.New()

	var successURL, failURL string
	successSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer successSrv.Close()
	successURL = successSrv.URL

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failSrv.Close()
	failURL = failSrv.URL

	cfg1 := makeConfig(tenantID, successURL, "secret1")
	cfg2 := makeConfig(tenantID, failURL, "secret2")

	cs := &inMemConfigStore{configs: []*store.WebhookConfig{cfg1, cfg2}}
	ds := &inMemDeliveryStore{}

	dispatcher := &Dispatcher{
		configStore:   nil,
		deliveryStore: nil,
		client:        &http.Client{Timeout: 5 * time.Second},
	}
	_ = dispatcher

	client := &http.Client{Timeout: 5 * time.Second}

	payload := []byte(`{"event":"test"}`)

	configs, _ := cs.ListEnabledByEventType(context.Background(), tenantID, "test.event")
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(configs))
	}

	for _, cfg := range configs {
		outcome := SendWebhook(context.Background(), client, cfg, "test.event", payload)
		ds.Insert(context.Background(), &store.WebhookDelivery{
			TenantID:   cfg.TenantID,
			ConfigID:   cfg.ID,
			EventType:  "test.event",
			FinalState: func() string {
				if outcome.Success {
					return "succeeded"
				}
				return "retrying"
			}(),
			AttemptCount: 1,
		})
	}

	if len(ds.inserted) != 2 {
		t.Errorf("expected 2 delivery rows, got %d", len(ds.inserted))
	}

	succeededCount := 0
	retryingCount := 0
	for _, d := range ds.inserted {
		if d.FinalState == "succeeded" {
			succeededCount++
		} else if d.FinalState == "retrying" {
			retryingCount++
		}
	}

	if succeededCount != 1 {
		t.Errorf("expected 1 succeeded delivery, got %d", succeededCount)
	}
	if retryingCount != 1 {
		t.Errorf("expected 1 retrying delivery, got %d", retryingCount)
	}
}

func TestSendWebhook_HeadersSet(t *testing.T) {
	var ua string
	var ct string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		ct = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := makeConfig(uuid.New(), srv.URL, "sec")
	client := &http.Client{Timeout: 5 * time.Second}
	outcome := SendWebhook(context.Background(), client, cfg, "evt", []byte(`{}`))

	if !outcome.Success {
		t.Fatalf("unexpected failure: %v", outcome.Err)
	}
	if ua != "Argus-Webhook/1.0" {
		t.Errorf("User-Agent = %q, want %q", ua, "Argus-Webhook/1.0")
	}
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}
