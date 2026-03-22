package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestComputeHMAC(t *testing.T) {
	payload := `{"title":"test","body":"hello"}`
	secret := "my-secret-key"

	sig := ComputeHMAC(payload, secret)

	if sig == "" {
		t.Fatal("HMAC signature is empty")
	}
	if len(sig) != 64 {
		t.Errorf("HMAC signature length = %d, want 64 (SHA256 hex)", len(sig))
	}
}

func TestVerifyHMAC(t *testing.T) {
	payload := `{"event":"operator.down"}`
	secret := "webhook-secret"

	sig := ComputeHMAC(payload, secret)

	if !VerifyHMAC(payload, secret, sig) {
		t.Error("valid HMAC signature failed verification")
	}

	if VerifyHMAC(payload, "wrong-secret", sig) {
		t.Error("invalid HMAC should not verify")
	}

	if VerifyHMAC("tampered-payload", secret, sig) {
		t.Error("tampered payload should not verify")
	}
}

func TestHTTPWebhookSender_Success(t *testing.T) {
	var receivedSig string
	var receivedBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Argus-Signature")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewHTTPWebhookSender(WebhookConfig{Timeout: 5 * time.Second})
	payload := `{"event":"test"}`
	secret := "test-secret"

	err := sender.SendWebhook(context.Background(), srv.URL, secret, payload)
	if err != nil {
		t.Fatalf("SendWebhook: %v", err)
	}

	if receivedBody != payload {
		t.Errorf("body = %q, want %q", receivedBody, payload)
	}

	expectedSig := "sha256=" + ComputeHMAC(payload, secret)
	if receivedSig != expectedSig {
		t.Errorf("signature = %q, want %q", receivedSig, expectedSig)
	}
}

func TestHTTPWebhookSender_NoSecret(t *testing.T) {
	var receivedSig string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Argus-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sender := NewHTTPWebhookSender(WebhookConfig{})
	err := sender.SendWebhook(context.Background(), srv.URL, "", `{"test":true}`)
	if err != nil {
		t.Fatalf("SendWebhook: %v", err)
	}

	if receivedSig != "" {
		t.Errorf("signature should be empty when no secret, got %q", receivedSig)
	}
}

func TestHTTPWebhookSender_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sender := NewHTTPWebhookSender(WebhookConfig{})
	err := sender.SendWebhook(context.Background(), srv.URL, "", `{}`)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}
