package diameter

import (
	"context"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type GxHandler struct {
	sessionMgr  *session.Manager
	eventBus    *bus.EventBus
	simResolver SIMResolver
	stateMap    *SessionStateMap
	logger      zerolog.Logger
}

func NewGxHandler(sessMgr *session.Manager, eventBus *bus.EventBus, simResolver SIMResolver, stateMap *SessionStateMap, logger zerolog.Logger) *GxHandler {
	return &GxHandler{
		sessionMgr:  sessMgr,
		eventBus:    eventBus,
		simResolver: simResolver,
		stateMap:    stateMap,
		logger:      logger.With().Str("handler", "gx").Logger(),
	}
}

func (h *GxHandler) HandleCCR(msg *Message) *Message {
	sessionID := msg.GetSessionID()
	if sessionID == "" {
		h.logger.Warn().Msg("Gx CCR missing Session-ID")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	ccRequestType := msg.GetCCRequestType()
	ccRequestNumber := msg.GetCCRequestNumber()
	imsi, msisdn := ExtractSubscriptionID(msg.AVPs)

	if ccRequestType == 0 {
		h.logger.Warn().Msg("Gx CCR missing CC-Request-Type")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	h.logger.Debug().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Uint32("cc_request_type", ccRequestType).
		Uint32("cc_request_number", ccRequestNumber).
		Msg("Gx CCR received")

	switch ccRequestType {
	case CCRequestTypeInitial:
		return h.handleInitial(msg, sessionID, imsi, msisdn, ccRequestNumber)
	case CCRequestTypeUpdate:
		return h.handleUpdate(msg, sessionID, imsi, ccRequestNumber)
	case CCRequestTypeTermination:
		return h.handleTermination(msg, sessionID, imsi, ccRequestNumber)
	default:
		h.logger.Warn().Uint32("type", ccRequestType).Msg("unknown CC-Request-Type")
		return NewErrorAnswer(msg, ResultCodeInvalidAVPValue)
	}
}

func (h *GxHandler) handleInitial(msg *Message, sessionID, imsi, msisdn string, ccReqNum uint32) *Message {
	if imsi == "" {
		h.logger.Warn().Msg("Gx CCR-I missing IMSI")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess := &session.Session{
		IMSI:          imsi,
		MSISDN:        msisdn,
		AcctSessionID: sessionID,
		SessionState:  "active",
		AuthMethod:    "diameter_gx",
		StartedAt:     time.Now().UTC(),
	}

	if h.simResolver != nil {
		sim, err := h.simResolver.GetByIMSI(ctx, imsi)
		if err != nil {
			if err == store.ErrSIMNotFound {
				h.logger.Info().Str("imsi", imsi).Msg("Gx CCR-I: SIM not found")
				return NewErrorAnswer(msg, ResultCodeAuthenticationRejected)
			}
			h.logger.Error().Err(err).Str("imsi", imsi).Msg("SIM lookup failed")
			return NewErrorAnswer(msg, ResultCodeUnableToComply)
		}
		if sim.State != "active" {
			h.logger.Info().Str("imsi", imsi).Str("sim_state", sim.State).Msg("Gx CCR-I: SIM not active")
			return NewErrorAnswer(msg, ResultCodeAuthenticationRejected)
		}
		sess.SimID = sim.ID.String()
		sess.TenantID = sim.TenantID.String()
		sess.OperatorID = sim.OperatorID.String()
		if sim.APNID != nil {
			sess.APNID = sim.APNID.String()
		}
		if sim.MSISDN != nil && msisdn == "" {
			sess.MSISDN = *sim.MSISDN
		}
	}

	ratAVP := msg.FindAVPVendor(AVPCodeRATType3GPP, VendorID3GPP)
	if ratAVP != nil {
		ratVal, err := ratAVP.GetUint32()
		if err == nil {
			sess.RATType = mapDiameterRATType(ratVal)
		}
	}

	ds := h.stateMap.Create(sessionID, "", msg.ApplicationID, imsi)
	_ = ds.Transition(SessionStateOpen)

	if h.sessionMgr != nil {
		if err := h.sessionMgr.Create(ctx, sess); err != nil {
			h.logger.Error().Err(err).Str("imsi", imsi).Msg("failed to create session")
			h.stateMap.Delete(sessionID)
			return NewErrorAnswer(msg, ResultCodeUnableToComply)
		}
	}

	ds.InternalID = sess.ID

	if h.eventBus != nil {
		h.eventBus.Publish(ctx, bus.SubjectSessionStarted, map[string]interface{}{
			"session_id":  sess.ID,
			"sim_id":      sess.SimID,
			"tenant_id":   sess.TenantID,
			"operator_id": sess.OperatorID,
			"imsi":        imsi,
			"msisdn":      msisdn,
			"protocol":    "diameter_gx",
			"timestamp":   time.Now().UTC(),
		})
	}

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
	cca.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))

	ruleName := fmt.Sprintf("argus-default-%s", imsi)
	cca.AddAVP(BuildChargingRuleInstall(ruleName, DefaultQCI, DefaultMaxBwUL, DefaultMaxBwDL))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Str("internal_id", sess.ID).
		Msg("Gx CCR-I processed, session created")

	return cca
}

func (h *GxHandler) handleUpdate(msg *Message, sessionID, imsi string, ccReqNum uint32) *Message {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ds := h.stateMap.Get(sessionID)
	if ds != nil {
		_ = ds.Transition(SessionStatePending)
		defer func() { _ = ds.Transition(SessionStateOpen) }()
	}

	if h.sessionMgr != nil {
		sess, err := h.sessionMgr.GetByAcctSessionID(ctx, sessionID)
		if err != nil || sess == nil {
			h.logger.Warn().Str("session_id", sessionID).Msg("Gx CCR-U session not found")
			return NewErrorAnswer(msg, ResultCodeUnknownSessionID)
		}

		if h.eventBus != nil {
			h.eventBus.Publish(ctx, bus.SubjectSessionUpdated, map[string]interface{}{
				"session_id":  sess.ID,
				"sim_id":      sess.SimID,
				"tenant_id":   sess.TenantID,
				"operator_id": sess.OperatorID,
				"imsi":        imsi,
				"protocol":    "diameter_gx",
				"type":        "policy_update",
				"timestamp":   time.Now().UTC(),
			})
		}
	}

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeUpdate))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
	cca.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("Gx CCR-U processed")

	return cca
}

func (h *GxHandler) handleTermination(msg *Message, sessionID, imsi string, ccReqNum uint32) *Message {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ds := h.stateMap.Get(sessionID)
	if ds != nil {
		_ = ds.Transition(SessionStateClosed)
		defer h.stateMap.Delete(sessionID)
	}

	if h.sessionMgr != nil {
		sess, err := h.sessionMgr.GetByAcctSessionID(ctx, sessionID)
		if err != nil || sess == nil {
			h.logger.Warn().Str("session_id", sessionID).Msg("Gx CCR-T session not found")
			cca := NewAnswer(msg)
			cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
			cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
			cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
			cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
			return cca
		}

		if err := h.sessionMgr.Terminate(ctx, sess.ID, "diameter_gx_termination"); err != nil {
			h.logger.Error().Err(err).Str("session_id", sessionID).Msg("failed to terminate session")
		}

		if h.eventBus != nil {
			h.eventBus.Publish(ctx, bus.SubjectSessionEnded, map[string]interface{}{
				"session_id":      sess.ID,
				"sim_id":          sess.SimID,
				"tenant_id":       sess.TenantID,
				"operator_id":     sess.OperatorID,
				"imsi":            imsi,
				"protocol":        "diameter_gx",
				"terminate_cause": "normal",
				"bytes_in":        sess.BytesIn,
				"bytes_out":       sess.BytesOut,
				"timestamp":       time.Now().UTC(),
			})
		}
	}

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("Gx CCR-T processed, session terminated")

	return cca
}

func mapDiameterRATType(rat uint32) string {
	switch rat {
	case 1000:
		return "utran"
	case 1001:
		return "geran"
	case 1004:
		return "lte"
	case 1005:
		return "nb_iot"
	case 1009:
		return "nr_5g"
	default:
		return fmt.Sprintf("unknown_%d", rat)
	}
}
