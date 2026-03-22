package notification

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

type SMSConfig struct {
	Provider  string
	AccountID string
	AuthToken string
	FromPhone string
}

type SMSGatewaySender struct {
	cfg    SMSConfig
	logger zerolog.Logger
}

func NewSMSGatewaySender(cfg SMSConfig, logger zerolog.Logger) *SMSGatewaySender {
	return &SMSGatewaySender{
		cfg:    cfg,
		logger: logger.With().Str("component", "sms_sender").Logger(),
	}
}

func (s *SMSGatewaySender) SendSMS(ctx context.Context, phoneNumber, message string) error {
	switch s.cfg.Provider {
	case "twilio":
		return s.sendViaTwilio(ctx, phoneNumber, message)
	case "vonage":
		return s.sendViaVonage(ctx, phoneNumber, message)
	default:
		s.logger.Info().
			Str("phone", phoneNumber).
			Str("provider", s.cfg.Provider).
			Int("message_len", len(message)).
			Msg("SMS send (placeholder - no provider configured)")
		return nil
	}
}

func (s *SMSGatewaySender) sendViaTwilio(_ context.Context, phoneNumber, message string) error {
	s.logger.Info().
		Str("phone", phoneNumber).
		Str("from", s.cfg.FromPhone).
		Int("message_len", len(message)).
		Msg("twilio SMS send (placeholder)")
	return fmt.Errorf("notification: twilio integration not yet implemented")
}

func (s *SMSGatewaySender) sendViaVonage(_ context.Context, phoneNumber, message string) error {
	s.logger.Info().
		Str("phone", phoneNumber).
		Str("from", s.cfg.FromPhone).
		Int("message_len", len(message)).
		Msg("vonage SMS send (placeholder)")
	return fmt.Errorf("notification: vonage integration not yet implemented")
}
