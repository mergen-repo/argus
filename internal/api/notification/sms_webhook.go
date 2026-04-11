package notification

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

type SMSStatusStore interface {
	UpdateDeliveryBySID(ctx context.Context, sid, status, errorCode string) error
}

type TwilioVerifier interface {
	VerifyStatusSignature(fullURL string, form url.Values, headerSig string) bool
}

type SMSWebhookHandler struct {
	verifier TwilioVerifier
	store    SMSStatusStore
	logger   zerolog.Logger
}

func NewSMSWebhookHandler(verifier TwilioVerifier, store SMSStatusStore, logger zerolog.Logger) *SMSWebhookHandler {
	return &SMSWebhookHandler{
		verifier: verifier,
		store:    store,
		logger:   logger.With().Str("component", "sms_webhook").Logger(),
	}
}

func (h *SMSWebhookHandler) HandleStatusCallback(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)

	if err := r.ParseForm(); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Failed to parse form body")
		return
	}

	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	fullURL := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.RequestURI())

	headerSig := r.Header.Get("X-Twilio-Signature")

	if h.verifier != nil {
		if !h.verifier.VerifyStatusSignature(fullURL, r.PostForm, headerSig) {
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Invalid Twilio signature")
			return
		}
	}

	sid := r.FormValue("MessageSid")
	status := r.FormValue("MessageStatus")

	if sid == "" || status == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "MessageSid and MessageStatus are required")
		return
	}

	errorCode := r.FormValue("ErrorCode")

	if h.store != nil {
		if err := h.store.UpdateDeliveryBySID(r.Context(), sid, status, errorCode); err != nil {
			h.logger.Error().Err(err).Str("sid", sid).Str("status", status).Msg("update SMS delivery status")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
	}

	h.logger.Info().Str("sid", sid).Str("status", status).Str("error_code", errorCode).Msg("SMS status callback processed")

	w.WriteHeader(http.StatusNoContent)
}
