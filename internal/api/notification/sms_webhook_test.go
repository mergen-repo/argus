package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

type fakeSMSStatusStore struct {
	calledSID     string
	calledStatus  string
	calledErrCode string
	returnErr     error
}

func (f *fakeSMSStatusStore) UpdateDeliveryBySID(_ context.Context, sid, status, errorCode string) error {
	f.calledSID = sid
	f.calledStatus = status
	f.calledErrCode = errorCode
	return f.returnErr
}

type alwaysValidVerifier struct{}

func (a *alwaysValidVerifier) VerifyStatusSignature(_ string, _ url.Values, _ string) bool {
	return true
}

type alwaysInvalidVerifier struct{}

func (a *alwaysInvalidVerifier) VerifyStatusSignature(_ string, _ url.Values, _ string) bool {
	return false
}

func TestSMSWebhook_ValidSignatureAndBody(t *testing.T) {
	store := &fakeSMSStatusStore{}
	h := NewSMSWebhookHandler(&alwaysValidVerifier{}, store, zerolog.Nop())

	form := url.Values{}
	form.Set("MessageSid", "SMtest123")
	form.Set("MessageStatus", "delivered")
	form.Set("ErrorCode", "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/sms/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", "validsig")
	req.Host = "example.com"
	w := httptest.NewRecorder()

	h.HandleStatusCallback(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if store.calledSID != "SMtest123" {
		t.Errorf("store SID = %q, want %q", store.calledSID, "SMtest123")
	}
	if store.calledStatus != "delivered" {
		t.Errorf("store status = %q, want %q", store.calledStatus, "delivered")
	}
}

func TestSMSWebhook_InvalidSignature(t *testing.T) {
	store := &fakeSMSStatusStore{}
	h := NewSMSWebhookHandler(&alwaysInvalidVerifier{}, store, zerolog.Nop())

	form := url.Values{}
	form.Set("MessageSid", "SMtest123")
	form.Set("MessageStatus", "delivered")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/sms/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", "badsig")
	w := httptest.NewRecorder()

	h.HandleStatusCallback(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if store.calledSID != "" {
		t.Error("store should not be called on invalid signature")
	}
}

func TestSMSWebhook_MissingMessageSid(t *testing.T) {
	store := &fakeSMSStatusStore{}
	h := NewSMSWebhookHandler(&alwaysValidVerifier{}, store, zerolog.Nop())

	form := url.Values{}
	form.Set("MessageStatus", "delivered")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/sms/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	h.HandleStatusCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSMSWebhook_MissingMessageStatus(t *testing.T) {
	store := &fakeSMSStatusStore{}
	h := NewSMSWebhookHandler(&alwaysValidVerifier{}, store, zerolog.Nop())

	form := url.Values{}
	form.Set("MessageSid", "SMtest123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/sms/status", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	h.HandleStatusCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
