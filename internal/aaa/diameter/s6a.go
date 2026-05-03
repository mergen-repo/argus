package diameter

import (
	"context"
	"errors"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/rs/zerolog"
)

// S6aHandler handles Diameter S6a interface messages (3GPP TS 29.272):
//   - Update-Location-Request  (ULR, command 316)
//   - Notify-Request           (NTR, command 323)
//
// STORY-094 D-182 disposition (A): capture-only. IMEI is written to the
// in-memory + Redis-cached session blob. No DB writes, no audit rows,
// no enforcement (enforcement is STORY-096). Parity with RADIUS +
// 5G SBA wiring shipped in STORY-093.
type S6aHandler struct {
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	reg        *obsmetrics.Registry
	logger     zerolog.Logger
}

func NewS6aHandler(sessionMgr *session.Manager, eventBus *bus.EventBus, reg *obsmetrics.Registry, logger zerolog.Logger) *S6aHandler {
	return &S6aHandler{
		sessionMgr: sessionMgr,
		eventBus:   eventBus,
		reg:        reg,
		logger:     logger.With().Str("handler", "s6a").Logger(),
	}
}

// HandleULR processes an Update-Location-Request (CommandULR = 316).
// After extracting the IMSI from Subscription-ID AVPs and validating the
// session ID, it calls ExtractTerminalInformation to capture IMEI/SV from
// Terminal-Information AVP (350) into the session. The response is always
// successful — capture failures never block the ULR flow.
func (h *S6aHandler) HandleULR(msg *Message) *Message {
	sessionID := msg.GetSessionID()
	if sessionID == "" {
		h.logger.Warn().Msg("S6a ULR missing Session-ID")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	imsi, _ := ExtractSubscriptionID(msg.AVPs)

	h.logger.Debug().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("S6a ULR received")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Capture IMEI immediately after request validation, before any response
	// writing — mirrors STORY-093 RADIUS + SBA wire sites.
	imei, sv, imeiErr := ExtractTerminalInformation(msg.AVPs)
	if imeiErr != nil {
		if errors.Is(imeiErr, ErrIMEICaptureMalformed) {
			if h.reg != nil {
				h.reg.IncIMEICaptureParseErrors("diameter_s6a")
			}
			h.logger.Warn().Err(imeiErr).
				Str("protocol", "diameter_s6a").
				Str("session_id", sessionID).
				Msg("imei capture: malformed Terminal-Information AVP")
		}
	}

	if h.sessionMgr != nil && imsi != "" {
		sess := &session.Session{
			IMSI:          imsi,
			AcctSessionID: sessionID,
			SessionState:  "active",
			AuthMethod:    "diameter_s6a",
			ProtocolType:  session.ProtocolTypeDiameter,
			StartedAt:     time.Now().UTC(),
		}
		if imeiErr == nil && imei != "" {
			sess.IMEI = imei
			sess.SoftwareVersion = sv
		}
		if err := h.sessionMgr.Create(ctx, sess); err != nil {
			h.logger.Warn().Err(err).Str("imsi", imsi).Msg("S6a ULR: session create failed (non-fatal)")
		}
	}

	if h.eventBus != nil {
		env := bus.NewSessionEnvelope("session.started", "", "", "", "S6a ULR processed").
			WithMeta("session_id", sessionID).
			WithMeta("imsi", imsi).
			WithMeta("protocol", "diameter_s6a")
		h.eventBus.Publish(ctx, bus.SubjectSessionStarted, env)
	}

	ula := NewAnswer(msg)
	ula.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	ula.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("S6a ULR processed")

	return ula
}

// HandleNTR processes a Notify-Request (CommandNTR = 323).
// It looks up the existing session by AcctSessionID and updates IMEI/SV
// if Terminal-Information AVP is present. The response is always
// successful — capture failures never block the NTR flow.
func (h *S6aHandler) HandleNTR(msg *Message) *Message {
	sessionID := msg.GetSessionID()
	if sessionID == "" {
		h.logger.Warn().Msg("S6a NTR missing Session-ID")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	imsi, _ := ExtractSubscriptionID(msg.AVPs)

	h.logger.Debug().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("S6a NTR received")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Capture IMEI immediately after request validation, before any response
	// writing — mirrors STORY-093 RADIUS + SBA wire sites.
	imei, sv, imeiErr := ExtractTerminalInformation(msg.AVPs)
	if imeiErr != nil {
		if errors.Is(imeiErr, ErrIMEICaptureMalformed) {
			if h.reg != nil {
				h.reg.IncIMEICaptureParseErrors("diameter_s6a")
			}
			h.logger.Warn().Err(imeiErr).
				Str("protocol", "diameter_s6a").
				Str("session_id", sessionID).
				Msg("imei capture: malformed Terminal-Information AVP")
		}
	} else if imei != "" {
		if h.sessionMgr != nil {
			_ = h.sessionMgr.UpdateDeviceInfo(ctx, sessionID, imei, sv)
		}
	}

	nta := NewAnswer(msg)
	nta.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	nta.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("S6a NTR processed")

	return nta
}
