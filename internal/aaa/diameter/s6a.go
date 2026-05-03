package diameter

import (
	"context"
	"errors"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	obsmetrics "github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/policy/binding"
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
//
// STORY-096 Task 4: bindingGate and simResolver are optional additions.
// When bindingGate is nil (PAT-017 / AC-17), behaviour is identical to
// STORY-094 T7. Task 7 supplies the concrete gate via WithBindingGate.
type S6aHandler struct {
	sessionMgr  *session.Manager
	eventBus    *bus.EventBus
	reg         *obsmetrics.Registry
	simResolver SIMResolver
	bindingGate BindingGate
	logger      zerolog.Logger
}

// S6aOption configures an S6aHandler after construction.
type S6aOption func(*S6aHandler)

// WithBindingGate attaches the IMEI/SIM binding pre-check gate
// (STORY-096 AC-1). When gate is nil the option is a no-op.
func WithBindingGate(gate BindingGate) S6aOption {
	return func(h *S6aHandler) {
		if gate != nil {
			h.bindingGate = gate
		}
	}
}

// WithS6aSIMResolver attaches the SIM resolver used by the binding
// pre-check to fetch the SIM view for a given IMSI. When resolver is nil
// the binding pre-check is skipped (AC-17 zero regression).
func WithS6aSIMResolver(r SIMResolver) S6aOption {
	return func(h *S6aHandler) {
		if r != nil {
			h.simResolver = r
		}
	}
}

func NewS6aHandler(sessionMgr *session.Manager, eventBus *bus.EventBus, reg *obsmetrics.Registry, logger zerolog.Logger, opts ...S6aOption) *S6aHandler {
	h := &S6aHandler{
		sessionMgr: sessionMgr,
		eventBus:   eventBus,
		reg:        reg,
		logger:     logger.With().Str("handler", "s6a").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
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

	// Binding pre-check (STORY-096 AC-1 / AC-10 / AC-17).
	// Runs after IMEI capture, before response write. Requires a sim view —
	// look up the SIM by IMSI. Skip silently when gate or resolver is absent
	// so STORY-094 T7 behaviour is unchanged (AC-17).
	if h.bindingGate != nil && h.simResolver != nil && imsi != "" {
		sim, simErr := h.simResolver.GetByIMSI(ctx, imsi)
		if simErr != nil {
			h.logger.Warn().Err(simErr).Str("imsi", imsi).Msg("binding precheck: SIM lookup failed (s6a/ulr), skipping")
		} else if sim != nil {
			bSession := binding.SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            imei,
				SoftwareVersion: sv,
			}
			bSIM := binding.SIMView{
				ID:                    sim.ID,
				TenantID:              sim.TenantID,
				BindingMode:           sim.BindingMode,
				BoundIMEI:             sim.BoundIMEI,
				BindingGraceExpiresAt: sim.BindingGraceExpiresAt,
			}
			verdict, evalErr := h.bindingGate.Evaluate(ctx, bSession, bSIM)
			if evalErr != nil {
				h.logger.Warn().Err(evalErr).Msg("binding precheck evaluate failed (s6a/ulr)")
			} else {
				if applyErr := h.bindingGate.Apply(ctx, verdict, bSession, bSIM, "diameter_s6a"); applyErr != nil {
					h.logger.Warn().Err(applyErr).Msg("binding precheck apply failed (s6a/ulr)")
				}
				if verdict.Kind == binding.VerdictReject {
					return buildULAReject(msg, verdict.Reason)
				}
			}
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

	// Binding pre-check (STORY-096 AC-1 / D-NTR signaling-only disposition).
	// NTR is a signaling notification — enforce + audit but do NOT tear down the
	// session: NTA result remains success regardless of verdict. Sinks still fire.
	if h.bindingGate != nil && h.simResolver != nil && imsi != "" {
		sim, simErr := h.simResolver.GetByIMSI(ctx, imsi)
		if simErr != nil {
			h.logger.Warn().Err(simErr).Str("imsi", imsi).Msg("binding precheck: SIM lookup failed (s6a/ntr), skipping")
		} else if sim != nil {
			bSession := binding.SessionContext{
				TenantID:        sim.TenantID,
				SIMID:           sim.ID,
				IMEI:            imei,
				SoftwareVersion: sv,
			}
			bSIM := binding.SIMView{
				ID:                    sim.ID,
				TenantID:              sim.TenantID,
				BindingMode:           sim.BindingMode,
				BoundIMEI:             sim.BoundIMEI,
				BindingGraceExpiresAt: sim.BindingGraceExpiresAt,
			}
			verdict, evalErr := h.bindingGate.Evaluate(ctx, bSession, bSIM)
			if evalErr == nil {
				if applyErr := h.bindingGate.Apply(ctx, verdict, bSession, bSIM, "diameter_s6a"); applyErr != nil {
					h.logger.Warn().Err(applyErr).Msg("binding precheck apply failed (s6a/ntr)")
				}
			} else {
				h.logger.Warn().Err(evalErr).Msg("binding precheck evaluate failed (s6a/ntr)")
			}
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

// buildULAReject constructs a ULA answer with Result-Code 5012
// (DIAMETER_UNABLE_TO_COMPLY) and an Error-Message AVP (281) carrying
// the binding rejection reason code (STORY-096 AC-10).
//
// Result-Code 5012 is chosen for binding mismatch: it signals a
// permanent refusal without implying AVP-level invalidity (5004) or a
// missing AVP (5005). The HSS may retry on a different MME but the SIM
// will always be rejected until the binding is resolved.
func buildULAReject(req *Message, reasonCode string) *Message {
	ula := NewAnswer(req)
	ula.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeUnableToComply))
	if reasonCode != "" {
		ula.AddAVP(NewAVPErrorMessage(reasonCode))
	}
	sessionID := req.GetSessionID()
	if sessionID != "" {
		ula.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	}
	return ula
}
