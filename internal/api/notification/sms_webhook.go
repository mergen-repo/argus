package notification

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/rs/zerolog"
)

type SMSStatusStore interface {
	UpdateDeliveryBySID(ctx context.Context, sid, status, errorCode string) error
}

// SMSOutboundDeliveryStore is used to mark inbound Twilio delivery confirmations
// on the sms_outbound table. MarkDelivered is a no-op if no row matches the
// provider_message_id.
type SMSOutboundDeliveryStore interface {
	MarkDelivered(ctx context.Context, providerMsgID string, deliveredAt time.Time) error
}

type TwilioVerifier interface {
	VerifyStatusSignature(fullURL string, form url.Values, headerSig string) bool
}

type SMSWebhookHandler struct {
	verifier      TwilioVerifier
	store         SMSStatusStore
	outboundStore SMSOutboundDeliveryStore
	logger        zerolog.Logger
}

func NewSMSWebhookHandler(verifier TwilioVerifier, store SMSStatusStore, logger zerolog.Logger) *SMSWebhookHandler {
	return &SMSWebhookHandler{
		verifier: verifier,
		store:    store,
		logger:   logger.With().Str("component", "sms_webhook").Logger(),
	}
}

// SetOutboundStore wires the sms_outbound delivery-status store.
// When set, inbound "delivered" callbacks are also applied to sms_outbound rows.
func (h *SMSWebhookHandler) SetOutboundStore(s SMSOutboundDeliveryStore) {
	h.outboundStore = s
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

	// Also mark delivered on sms_outbound if applicable.
	// This is additive — both lookups run independently so either table can match.
	if h.outboundStore != nil && status == "delivered" {
		if err := h.outboundStore.MarkDelivered(r.Context(), sid, time.Now().UTC()); err != nil {
			h.logger.Warn().Err(err).Str("sid", sid).Msg("mark sms_outbound delivered")
		}
	}

	h.logger.Info().Str("sid", sid).Str("status", status).Str("error_code", errorCode).Msg("SMS status callback processed")

	w.WriteHeader(http.StatusNoContent)
}
