package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

type WebhookConfig struct {
	Timeout time.Duration
}

type HTTPWebhookSender struct {
	client *http.Client
}

func NewHTTPWebhookSender(cfg WebhookConfig) *HTTPWebhookSender {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &HTTPWebhookSender{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (w *HTTPWebhookSender) SendWebhook(ctx context.Context, url, secret, payload string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("notification: webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Argus-Webhook/1.0")

	if secret != "" {
		sig := ComputeHMAC(payload, secret)
		req.Header.Set("X-Argus-Signature", "sha256="+sig)
	}

	req.Header.Set("X-Argus-Timestamp", time.Now().UTC().Format(time.RFC3339))

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("notification: webhook send: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification: webhook status %d", resp.StatusCode)
	}

	return nil
}

func ValidateWebhookConfig(rawURL, secret string) error {
	if rawURL == "" {
		return fmt.Errorf("notification: webhook url is empty")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("notification: webhook url invalid: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("notification: webhook url must use https scheme, got %q", parsed.Scheme)
	}
	if secret == "" {
		return fmt.Errorf("notification: webhook secret is empty")
	}
	return nil
}

func ComputeHMAC(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func VerifyHMAC(payload, secret, signature string) bool {
	expected := ComputeHMAC(payload, secret)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}

type DeliveryOutcome struct {
	StatusCode   int
	ResponseBody string
	Signature    string
	PayloadHash  string
	Success      bool
	Err          error
	DurationMs   int64
}

func SendWebhook(ctx context.Context, client *http.Client, cfg *store.WebhookConfig, event string, payload []byte) DeliveryOutcome {
	start := time.Now()

	mac := hmac.New(sha256.New, []byte(cfg.Secret))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	hashBytes := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(hashBytes[:])

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return DeliveryOutcome{
			Signature:   sig,
			PayloadHash: payloadHash,
			Err:         fmt.Errorf("notification: webhook request: %w", err),
			DurationMs:  time.Since(start).Milliseconds(),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Argus-Webhook/1.0")
	req.Header.Set("X-Argus-Signature", "sha256="+sig)
	req.Header.Set("X-Argus-Event", event)
	req.Header.Set("X-Argus-Timestamp", time.Now().UTC().Format(time.RFC3339))

	resp, err := client.Do(req)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return DeliveryOutcome{
			Signature:   sig,
			PayloadHash: payloadHash,
			Err:         fmt.Errorf("notification: webhook send: %w", err),
			DurationMs:  durationMs,
		}
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	respBody := string(bodyBytes)

	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	return DeliveryOutcome{
		StatusCode:   resp.StatusCode,
		ResponseBody: respBody,
		Signature:    sig,
		PayloadHash:  payloadHash,
		Success:      success,
		DurationMs:   durationMs,
	}
}

type Dispatcher struct {
	configStore   *store.WebhookConfigStore
	deliveryStore *store.WebhookDeliveryStore
	client        *http.Client
}

func NewDispatcher(configStore *store.WebhookConfigStore, deliveryStore *store.WebhookDeliveryStore, client *http.Client) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Dispatcher{
		configStore:   configStore,
		deliveryStore: deliveryStore,
		client:        client,
	}
}

func (d *Dispatcher) DispatchToConfigs(ctx context.Context, tenantID uuid.UUID, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notification: dispatcher marshal payload: %w", err)
	}

	configs, err := d.configStore.ListEnabledByEventType(ctx, tenantID, event)
	if err != nil {
		return fmt.Errorf("notification: dispatcher list configs: %w", err)
	}

	for _, cfg := range configs {
		outcome := SendWebhook(ctx, d.client, cfg, event, data)
		d.persistDelivery(ctx, cfg, event, data, outcome)
	}
	return nil
}

func (d *Dispatcher) ResendDelivery(ctx context.Context, deliveryID uuid.UUID) (*store.WebhookDelivery, error) {
	original, err := d.deliveryStore.GetByID(ctx, deliveryID)
	if err != nil {
		return nil, fmt.Errorf("notification: resend delivery: get original: %w", err)
	}

	cfg, err := d.configStore.Get(ctx, original.ConfigID)
	if err != nil {
		return nil, fmt.Errorf("notification: resend delivery: get config: %w", err)
	}

	preview := original.PayloadPreview
	var payload []byte
	if len(preview) > 0 {
		payload = []byte(preview)
	} else {
		payload = []byte("{}")
	}

	outcome := SendWebhook(ctx, d.client, cfg, original.EventType, payload)
	saved := d.persistDelivery(ctx, cfg, original.EventType, payload, outcome)
	return saved, nil
}

func (d *Dispatcher) persistDelivery(ctx context.Context, cfg *store.WebhookConfig, event string, payload []byte, outcome DeliveryOutcome) *store.WebhookDelivery {
	finalState := "retrying"
	var nextRetryAt *time.Time
	if outcome.Success {
		finalState = "succeeded"
	} else {
		t := time.Now().UTC().Add(30 * time.Second)
		nextRetryAt = &t
	}

	preview := string(payload)
	if len(preview) > 500 {
		preview = preview[:500]
	}

	var respStatus *int
	var respBody *string
	if outcome.StatusCode != 0 {
		sc := outcome.StatusCode
		respStatus = &sc
	}
	if outcome.ResponseBody != "" {
		rb := outcome.ResponseBody
		respBody = &rb
	}

	delivery := &store.WebhookDelivery{
		TenantID:       cfg.TenantID,
		ConfigID:       cfg.ID,
		EventType:      event,
		PayloadHash:    outcome.PayloadHash,
		PayloadPreview: preview,
		Signature:      outcome.Signature,
		ResponseStatus: respStatus,
		ResponseBody:   respBody,
		AttemptCount:   1,
		NextRetryAt:    nextRetryAt,
		FinalState:     finalState,
	}

	saved, err := d.deliveryStore.Insert(ctx, delivery)
	if err != nil {
		return delivery
	}

	if outcome.Success {
		_ = d.configStore.BumpSuccess(ctx, cfg.ID, time.Now().UTC())
	} else {
		_ = d.configStore.BumpFailure(ctx, cfg.ID, time.Now().UTC())
	}

	return saved
}
