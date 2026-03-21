package diameter

import (
	"context"
	"time"

	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

type GyHandler struct {
	sessionMgr  *session.Manager
	eventBus    *bus.EventBus
	simResolver SIMResolver
	stateMap    *SessionStateMap
	logger      zerolog.Logger
}

func NewGyHandler(sessMgr *session.Manager, eventBus *bus.EventBus, simResolver SIMResolver, stateMap *SessionStateMap, logger zerolog.Logger) *GyHandler {
	return &GyHandler{
		sessionMgr:  sessMgr,
		eventBus:    eventBus,
		simResolver: simResolver,
		stateMap:    stateMap,
		logger:      logger.With().Str("handler", "gy").Logger(),
	}
}

func (h *GyHandler) HandleCCR(msg *Message) *Message {
	sessionID := msg.GetSessionID()
	if sessionID == "" {
		h.logger.Warn().Msg("Gy CCR missing Session-ID")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	ccRequestType := msg.GetCCRequestType()
	ccRequestNumber := msg.GetCCRequestNumber()
	imsi, msisdn := ExtractSubscriptionID(msg.AVPs)

	if ccRequestType == 0 {
		h.logger.Warn().Msg("Gy CCR missing CC-Request-Type")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	h.logger.Debug().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Uint32("cc_request_type", ccRequestType).
		Uint32("cc_request_number", ccRequestNumber).
		Msg("Gy CCR received")

	switch ccRequestType {
	case CCRequestTypeInitial:
		return h.handleInitial(msg, sessionID, imsi, msisdn, ccRequestNumber)
	case CCRequestTypeUpdate:
		return h.handleUpdate(msg, sessionID, imsi, ccRequestNumber)
	case CCRequestTypeTermination:
		return h.handleTermination(msg, sessionID, imsi, ccRequestNumber)
	case CCRequestTypeEvent:
		return h.handleEvent(msg, sessionID, imsi, ccRequestNumber)
	default:
		h.logger.Warn().Uint32("type", ccRequestType).Msg("unknown CC-Request-Type")
		return NewErrorAnswer(msg, ResultCodeInvalidAVPValue)
	}
}

func (h *GyHandler) handleInitial(msg *Message, sessionID, imsi, msisdn string, ccReqNum uint32) *Message {
	if imsi == "" {
		h.logger.Warn().Msg("Gy CCR-I missing IMSI")
		return NewErrorAnswer(msg, ResultCodeMissingAVP)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ds := h.stateMap.Create(sessionID, "", msg.ApplicationID, imsi)
	_ = ds.Transition(SessionStateOpen)

	if h.sessionMgr != nil {
		existing, _ := h.sessionMgr.GetByAcctSessionID(ctx, sessionID)
		if existing == nil {
			sess := &session.Session{
				IMSI:          imsi,
				MSISDN:        msisdn,
				AcctSessionID: sessionID,
				SessionState:  "active",
				AuthMethod:    "diameter_gy",
				StartedAt:     time.Now().UTC(),
			}

			if h.simResolver != nil {
				sim, err := h.simResolver.GetByIMSI(ctx, imsi)
				if err != nil {
					if err == store.ErrSIMNotFound {
						h.logger.Info().Str("imsi", imsi).Msg("Gy CCR-I: SIM not found")
						h.stateMap.Delete(sessionID)
						return NewErrorAnswer(msg, ResultCodeAuthenticationRejected)
					}
					h.logger.Error().Err(err).Str("imsi", imsi).Msg("SIM lookup failed")
					h.stateMap.Delete(sessionID)
					return NewErrorAnswer(msg, ResultCodeUnableToComply)
				}
				if sim.State != "active" {
					h.logger.Info().Str("imsi", imsi).Str("sim_state", sim.State).Msg("Gy CCR-I: SIM not active")
					h.stateMap.Delete(sessionID)
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
					sess.RATType = rattype.FromDiameter(ratVal)
				}
			}

			if err := h.sessionMgr.Create(ctx, sess); err != nil {
				h.logger.Error().Err(err).Str("imsi", imsi).Msg("failed to create session for Gy")
				h.stateMap.Delete(sessionID)
			} else {
				ds.InternalID = sess.ID
			}
		}
	}

	if h.eventBus != nil {
		h.eventBus.Publish(ctx, bus.SubjectSessionStarted, map[string]interface{}{
			"session_id": sessionID,
			"imsi":       imsi,
			"msisdn":     msisdn,
			"protocol":   "diameter_gy",
			"timestamp":  time.Now().UTC(),
		})
	}

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
	cca.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	cca.AddAVP(BuildGrantedServiceUnit(DefaultGrantedOctets, DefaultGrantedTimeSec, DefaultValidityTime))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Uint64("granted_octets", DefaultGrantedOctets).
		Msg("Gy CCR-I processed, initial credit granted")

	return cca
}

func (h *GyHandler) handleUpdate(msg *Message, sessionID, imsi string, ccReqNum uint32) *Message {
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
			h.logger.Warn().Str("session_id", sessionID).Msg("Gy CCR-U session not found")
			return NewErrorAnswer(msg, ResultCodeUnknownSessionID)
		}

		totalOctets, inputOctets, outputOctets, _ := ExtractUsedServiceUnit(msg.AVPs)

		if totalOctets > 0 || inputOctets > 0 || outputOctets > 0 {
			bytesIn := inputOctets
			bytesOut := outputOctets
			if bytesIn == 0 && bytesOut == 0 && totalOctets > 0 {
				bytesIn = totalOctets / 2
				bytesOut = totalOctets - bytesIn
			}
			if err := h.sessionMgr.UpdateCounters(ctx, sess.ID, sess.BytesIn+bytesIn, sess.BytesOut+bytesOut); err != nil {
				h.logger.Error().Err(err).Msg("failed to update counters")
			}
		}

		if h.eventBus != nil {
			h.eventBus.Publish(ctx, bus.SubjectSessionUpdated, map[string]interface{}{
				"session_id":  sess.ID,
				"sim_id":      sess.SimID,
				"tenant_id":   sess.TenantID,
				"operator_id": sess.OperatorID,
				"imsi":        imsi,
				"protocol":    "diameter_gy",
				"type":        "credit_update",
				"used_octets": totalOctets,
				"timestamp":   time.Now().UTC(),
			})
		}
	}

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeUpdate))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
	cca.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))
	cca.AddAVP(BuildGrantedServiceUnit(DefaultGrantedOctets, DefaultGrantedTimeSec, DefaultValidityTime))

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("Gy CCR-U processed, credit updated")

	return cca
}

func (h *GyHandler) handleTermination(msg *Message, sessionID, imsi string, ccReqNum uint32) *Message {
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
			h.logger.Warn().Str("session_id", sessionID).Msg("Gy CCR-T session not found")
			cca := NewAnswer(msg)
			cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
			cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
			cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeTermination))
			cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
			return cca
		}

		totalOctets, inputOctets, outputOctets, _ := ExtractUsedServiceUnit(msg.AVPs)
		if totalOctets > 0 || inputOctets > 0 || outputOctets > 0 {
			bytesIn := inputOctets
			bytesOut := outputOctets
			if bytesIn == 0 && bytesOut == 0 && totalOctets > 0 {
				bytesIn = totalOctets / 2
				bytesOut = totalOctets - bytesIn
			}
			_ = h.sessionMgr.UpdateCounters(ctx, sess.ID, sess.BytesIn+bytesIn, sess.BytesOut+bytesOut)
		}

		if err := h.sessionMgr.Terminate(ctx, sess.ID, "diameter_gy_termination"); err != nil {
			h.logger.Error().Err(err).Str("session_id", sessionID).Msg("failed to terminate session")
		}

		if h.eventBus != nil {
			h.eventBus.Publish(ctx, bus.SubjectSessionEnded, map[string]interface{}{
				"session_id":      sess.ID,
				"sim_id":          sess.SimID,
				"tenant_id":       sess.TenantID,
				"operator_id":     sess.OperatorID,
				"imsi":            imsi,
				"protocol":        "diameter_gy",
				"terminate_cause": "normal",
				"bytes_in":        sess.BytesIn + inputOctets,
				"bytes_out":       sess.BytesOut + outputOctets,
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
		Msg("Gy CCR-T processed, session terminated")

	return cca
}

func (h *GyHandler) handleEvent(msg *Message, sessionID, imsi string, ccReqNum uint32) *Message {
	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Msg("Gy CCR-E (event) processed")

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeEvent))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
	cca.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGy))

	return cca
}
