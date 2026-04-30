package diameter

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/aaa/rattype"
	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type GxHandler struct {
	sessionMgr  *session.Manager
	eventBus    *bus.EventBus
	simResolver SIMResolver
	// ipPoolStore + simStore are optional — when both are set, handleInitial
	// allocates a dynamic IP (if the SIM has no ip_address_id and has an APN)
	// and handleTermination releases it on CCR-T. When either is nil, the
	// handler skips IP handling (pre-STORY-092 behaviour).
	ipPoolStore *store.IPPoolStore
	simStore    *store.SIMStore
	stateMap    *SessionStateMap
	logger      zerolog.Logger
}

func NewGxHandler(sessMgr *session.Manager, eventBus *bus.EventBus, simResolver SIMResolver, ipPoolStore *store.IPPoolStore, simStore *store.SIMStore, stateMap *SessionStateMap, logger zerolog.Logger) *GxHandler {
	return &GxHandler{
		sessionMgr:  sessMgr,
		eventBus:    eventBus,
		simResolver: simResolver,
		ipPoolStore: ipPoolStore,
		simStore:    simStore,
		stateMap:    stateMap,
		logger:      logger.With().Str("handler", "gx").Logger(),
	}
}

// parseV4AddressForAVP strips an optional CIDR suffix (e.g. "/32") from a
// PostgreSQL INET-style address string and returns the 4-byte IPv4 form
// expected by NewAVPAddress. Returns [4]byte{0,0,0,0} on parse failure —
// callers must handle the zero-IP case before emitting the AVP. Kept local
// to this file per STORY-092 plan (YAGNI — do not shared-package-ify).
func parseV4AddressForAVP(s string) [4]byte {
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return [4]byte{0, 0, 0, 0}
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return [4]byte{0, 0, 0, 0}
	}
	var out [4]byte
	copy(out[:], ip4)
	return out
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

	var sim *store.SIM
	if h.simResolver != nil {
		var err error
		sim, err = h.simResolver.GetByIMSI(ctx, imsi)
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

		// STORY-092 Wave 2: dynamic IP allocation before CCA-I build, after
		// SIM confirmed active (advisor flag #6). No-op when deps nil or
		// sim already has an IP or sim has no APN (can't pick a pool).
		h.allocateDynamicIPIfNeeded(ctx, sim, imsi)
	}

	ratAVP := msg.FindAVPVendor(AVPCodeRATType3GPP, VendorID3GPP)
	if ratAVP != nil {
		ratVal, err := ratAVP.GetUint32()
		if err == nil {
			sess.RATType = rattype.FromDiameter(ratVal)
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
		iccid := ""
		if sim != nil {
			iccid = sim.ICCID
		}
		env := bus.NewSessionEnvelope("session.started", sess.TenantID, sess.SimID, iccid, "Session started (Gx)").
			WithMeta("session_id", sess.ID).
			WithMeta("operator_id", sess.OperatorID).
			WithMeta("imsi", imsi).
			WithMeta("msisdn", msisdn).
			WithMeta("protocol", "diameter_gx")
		h.eventBus.Publish(ctx, bus.SubjectSessionStarted, env)
	}

	cca := NewAnswer(msg)
	cca.AddAVP(NewAVPString(AVPCodeSessionID, AVPFlagMandatory, 0, sessionID))
	cca.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, ResultCodeSuccess))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestType, AVPFlagMandatory, 0, CCRequestTypeInitial))
	cca.AddAVP(NewAVPUint32(AVPCodeCCRequestNumber, AVPFlagMandatory, 0, ccReqNum))
	cca.AddAVP(NewAVPUint32(AVPCodeAuthApplicationID, AVPFlagMandatory, 0, ApplicationIDGx))

	ruleName := fmt.Sprintf("argus-default-%s", imsi)
	cca.AddAVP(BuildChargingRuleInstall(ruleName, DefaultQCI, DefaultMaxBwUL, DefaultMaxBwDL))

	// STORY-092 Wave 2: attach Framed-IP-Address AVP when the SIM has an
	// allocated IP (either preallocated in the DB or just allocated above by
	// allocateDynamicIPIfNeeded, which mutates sim.IPAddressID in place).
	// RFC 7155 NASREQ §4.4.10.5.1: Framed-IP-Address, code 8, vendorID=0,
	// flags M=1 V=0 P=0, 4-byte IPv4 payload.
	if sim != nil && sim.IPAddressID != nil && h.ipPoolStore != nil {
		if ipAddr, err := h.ipPoolStore.GetIPAddressByID(ctx, *sim.IPAddressID); err == nil && ipAddr.AddressV4 != nil {
			ip4 := parseV4AddressForAVP(*ipAddr.AddressV4)
			if ip4 != ([4]byte{}) {
				cca.AddAVP(NewAVPAddress(AVPCodeFramedIPAddress, AVPFlagMandatory, 0, ip4))
			}
		}
	}

	h.logger.Info().
		Str("session_id", sessionID).
		Str("imsi", imsi).
		Str("internal_id", sess.ID).
		Msg("Gx CCR-I processed, session created")

	return cca
}

// allocateDynamicIPIfNeeded mirrors the RADIUS Wave-1 helper
// (radius/server.go allocateDynamicIPIfNeeded). No-op when:
//   - ipPoolStore or simStore is nil (Diameter server built without deps),
//   - the SIM already has a preallocated ip_address_id,
//   - the SIM has no APN,
//   - no active pool exists for (tenant, apn), or
//   - the pool is exhausted (returns without AVP — RFC-compliant).
//
// On success, sim.IPAddressID is mutated so the caller's CCA-I build can
// emit the Framed-IP AVP without a re-fetch. The shared radius SIMCache is
// invalidated (via anonymous interface assertion — keeps the SIMResolver
// public interface untouched per advisor note).
func (h *GxHandler) allocateDynamicIPIfNeeded(ctx context.Context, sim *store.SIM, imsi string) {
	if h == nil || h.ipPoolStore == nil || h.simStore == nil || sim == nil {
		return
	}
	if sim.IPAddressID != nil {
		return
	}
	if sim.APNID == nil {
		return
	}

	pools, _, err := h.ipPoolStore.List(ctx, sim.TenantID, "", 1, sim.APNID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("gx: list pools for dynamic alloc failed")
		return
	}
	if len(pools) == 0 {
		h.logger.Debug().Str("sim_id", sim.ID.String()).Str("apn_id", sim.APNID.String()).Msg("gx: no pool for APN, skipping dynamic alloc")
		return
	}

	allocated, err := h.ipPoolStore.AllocateIP(ctx, pools[0].ID, sim.ID)
	if err != nil {
		if err == store.ErrPoolExhausted {
			h.logger.Warn().Str("sim_id", sim.ID.String()).Str("pool_id", pools[0].ID.String()).Msg("gx: pool exhausted, CCA-I without Framed-IP")
			return
		}
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Str("pool_id", pools[0].ID.String()).Msg("gx: dynamic IP allocation failed")
		return
	}

	if err := h.simStore.SetIPAndPolicy(ctx, sim.ID, &allocated.ID, nil); err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Str("ip_id", allocated.ID.String()).Msg("gx: persist dynamic ip_address_id failed")
	}

	// Anonymous interface assertion — the public SIMResolver interface does
	// not declare InvalidateIMSI (only GetByIMSI). The concrete type in
	// production is *radius.SIMCache, which implements both. Keeping the
	// assertion local avoids extending SIMResolver just for this call.
	if inv, ok := h.simResolver.(interface {
		InvalidateIMSI(context.Context, string) error
	}); ok && imsi != "" {
		_ = inv.InvalidateIMSI(ctx, imsi)
	}
	sim.IPAddressID = &allocated.ID
	h.logger.Info().Str("sim_id", sim.ID.String()).Str("ip_id", allocated.ID.String()).Str("pool_id", pools[0].ID.String()).Msg("gx: dynamic IP allocated")
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
			env := bus.NewSessionEnvelope("session.updated", sess.TenantID, sess.SimID, sess.ICCID, "Session updated (Gx)").
				WithMeta("session_id", sess.ID).
				WithMeta("operator_id", sess.OperatorID).
				WithMeta("imsi", imsi).
				WithMeta("protocol", "diameter_gx").
				WithMeta("update_type", "policy_update")
			h.eventBus.Publish(ctx, bus.SubjectSessionUpdated, env)
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

		// STORY-092 Wave 2: release dynamic IP allocation back to pool on
		// CCR-T. Static allocations are preserved. Errors logged, not
		// propagated — session termination always succeeds.
		h.releaseDynamicIPIfNeeded(ctx, sess, imsi)

		if h.eventBus != nil {
			env := bus.NewSessionEnvelope("session.ended", sess.TenantID, sess.SimID, sess.ICCID, "Session ended (Gx)").
				WithMeta("session_id", sess.ID).
				WithMeta("operator_id", sess.OperatorID).
				WithMeta("imsi", imsi).
				WithMeta("protocol", "diameter_gx").
				WithMeta("termination_cause", "normal").
				WithMeta("bytes_in", sess.BytesIn).
				WithMeta("bytes_out", sess.BytesOut)
			h.eventBus.Publish(ctx, bus.SubjectSessionEnded, env)
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

// releaseDynamicIPIfNeeded returns a dynamically allocated IP back to its
// pool on CCR-T. Mirrors the RADIUS Wave-2 helper
// (radius/server.go releaseDynamicIPIfNeededForSession). No-op when:
//   - deps nil (ipPoolStore / simStore not wired),
//   - the session has no sim_id / tenant_id,
//   - the SIM has no ip_address_id, or
//   - the ip_addresses row is allocation_type='static' (preserved).
//
// Never returns — failures are logged. CCR-T always succeeds.
func (h *GxHandler) releaseDynamicIPIfNeeded(ctx context.Context, sess *session.Session, imsi string) {
	if h == nil || h.ipPoolStore == nil || h.simStore == nil {
		return
	}
	if sess == nil || sess.SimID == "" || sess.TenantID == "" {
		return
	}
	simID, err := uuid.Parse(sess.SimID)
	if err != nil {
		h.logger.Debug().Err(err).Str("sim_id", sess.SimID).Msg("gx: parse sim_id for release, skipping")
		return
	}
	tenantID, err := uuid.Parse(sess.TenantID)
	if err != nil {
		h.logger.Debug().Err(err).Str("tenant_id", sess.TenantID).Msg("gx: parse tenant_id for release, skipping")
		return
	}

	sim, err := h.simStore.GetByID(ctx, tenantID, simID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sess.SimID).Msg("gx: SIM lookup failed during release, skipping")
		return
	}
	if sim == nil || sim.IPAddressID == nil {
		return
	}

	ipAddr, err := h.ipPoolStore.GetIPAddressByID(ctx, *sim.IPAddressID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("gx: get ip_address for release failed")
		return
	}
	if ipAddr.AllocationType != "dynamic" {
		return
	}

	if err := h.ipPoolStore.ReleaseIP(ctx, ipAddr.PoolID, sim.ID); err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Str("ip_id", ipAddr.ID.String()).Msg("gx: ReleaseIP failed")
	}
	if err := h.simStore.ClearIPAddress(ctx, sim.ID); err != nil {
		h.logger.Warn().Err(err).Str("sim_id", sim.ID.String()).Msg("gx: ClearIPAddress failed")
	}
	if inv, ok := h.simResolver.(interface {
		InvalidateIMSI(context.Context, string) error
	}); ok && imsi != "" {
		_ = inv.InvalidateIMSI(ctx, imsi)
	}
	h.logger.Info().Str("sim_id", sim.ID.String()).Str("ip_id", ipAddr.ID.String()).Str("pool_id", ipAddr.PoolID.String()).Msg("gx: dynamic IP released")
}
