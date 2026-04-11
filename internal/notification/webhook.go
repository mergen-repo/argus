package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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
	return hmac.Equal([]byte(expected), []byte(signature))
}
