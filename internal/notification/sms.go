package notification

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/rs/zerolog"
)

var ErrSMSProviderNotSupported = errors.New("sms: provider not supported (only twilio implemented in v1)")

type SMSConfig struct {
	Provider          string
	AccountID         string
	AuthToken         string
	FromPhone         string
	StatusCallbackURL string
	Timeout           time.Duration
}

type TwilioVerifier interface {
	VerifyStatusSignature(fullURL string, form url.Values, headerSig string) bool
}

type SMSGatewaySender struct {
	cfg    SMSConfig
	twilio *twilioClient
	logger zerolog.Logger
}

func NewSMSGatewaySender(cfg SMSConfig, logger zerolog.Logger) *SMSGatewaySender {
	s := &SMSGatewaySender{
		cfg:    cfg,
		logger: logger.With().Str("component", "sms_sender").Logger(),
	}
	if cfg.Provider == "twilio" {
		s.twilio = newTwilioClient(cfg, s.logger)
	}
	return s
}

func (s *SMSGatewaySender) SendSMS(ctx context.Context, phoneNumber, message string) error {
	switch s.cfg.Provider {
	case "twilio":
		return s.sendViaTwilio(ctx, phoneNumber, message)
	case "vonage":
		return s.sendViaVonage()
	default:
		s.logger.Info().
			Str("phone", phoneNumber).
			Str("provider", s.cfg.Provider).
			Int("message_len", len(message)).
			Msg("SMS send skipped; no provider configured")
		return nil
	}
}

func (s *SMSGatewaySender) Verifier() TwilioVerifier {
	if s.twilio == nil {
		return nil
	}
	return s.twilio
}

func (s *SMSGatewaySender) sendViaTwilio(ctx context.Context, phoneNumber, message string) error {
	return s.twilio.Send(ctx, phoneNumber, message)
}

func (s *SMSGatewaySender) sendViaVonage() error {
	return ErrSMSProviderNotSupported
}

// SendSMSWithResult sends an SMS via the configured provider and returns
// the provider message ID (e.g. Twilio SID) on success.
func (s *SMSGatewaySender) SendSMSWithResult(ctx context.Context, phoneNumber, message string) (string, error) {
	switch s.cfg.Provider {
	case "twilio":
		return s.twilio.SendWithResult(ctx, phoneNumber, message)
	case "vonage":
		return "", ErrSMSProviderNotSupported
	default:
		s.logger.Info().
			Str("phone", phoneNumber).
			Str("provider", s.cfg.Provider).
			Int("message_len", len(message)).
			Msg("SMS send skipped; no provider configured")
		return "", nil
	}
}
