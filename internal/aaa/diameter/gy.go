package diameter

import (
	"context"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/rs/zerolog"
)

type GyHandler struct {
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	logger     zerolog.Logger
}

func NewGyHandler(sessMgr *session.Manager, eventBus *bus.EventBus, logger zerolog.Logger) *GyHandler {
	return &GyHandler{
		sessionMgr: sessMgr,
		eventBus:   eventBus,
		logger:     logger.With().Str("handler", "gy").Logger(),
	}
}

func (h *GyHandler) HandleCCR(msg *Message) *Message {
	ccRequestType := msg.GetCCRequestType()
	ccRequestNumber := msg.GetCCRequestNumber()
	sessionID := msg.GetSessionID()
	imsi, msisdn := ExtractSubscriptionID(msg.AVPs)

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

	existing, _ := h.sessionMgr.GetByAcctSessionID(ctx, sessionID)
	if existing == nil {
		sess := &session.Session{
			IMSI:          imsi,
			MSISDN:        msisdn,
			AcctSessionID: sessionID,
			SessionState:  "active",
			StartedAt:     time.Now().UTC(),
		}
		if err := h.sessionMgr.Create(ctx, sess); err != nil {
			h.logger.Error().Err(err).Str("imsi", imsi).Msg("failed to create session for Gy")
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
			"imsi":        imsi,
			"protocol":    "diameter_gy",
			"type":        "credit_update",
			"used_octets": totalOctets,
			"timestamp":   time.Now().UTC(),
		})
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
		Uint64("used_total", totalOctets).
		Msg("Gy CCR-U processed, credit updated")

	return cca
}

func (h *GyHandler) handleTermination(msg *Message, sessionID, imsi string, ccReqNum uint32) *Message {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

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
			"imsi":            imsi,
			"protocol":        "diameter_gy",
			"terminate_cause": "normal",
			"bytes_in":        sess.BytesIn + inputOctets,
			"bytes_out":       sess.BytesOut + outputOctets,
			"timestamp":       time.Now().UTC(),
		})
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

	_ = fmt.Sprintf("event_%s", sessionID)

	return cca
}
