package diameter

import (
	"net"

	argusdiameter "github.com/btopcu/argus/internal/aaa/diameter"
	"github.com/btopcu/argus/internal/simulator/radius"
)

// avpCodeFramedIPAddress is the Diameter NAS attribute code for Framed-IP-Address.
// Defined in RFC 7155 §4.4.10.5.1 (NAS Application) — code 8, inherited from RADIUS.
const avpCodeFramedIPAddress uint32 = 8

// BuildGxCCRI builds a Gx Credit-Control-Request Initial (CCR-I) message.
// AVP order mirrors what internal/aaa/diameter/gx.go handleInitial expects:
// Session-Id, Origin-Host, Origin-Realm, Destination-Realm, Auth-Application-Id,
// CC-Request-Type, CC-Request-Number, Subscription-Id, Framed-IP-Address,
// IP-CAN-Type, RAT-Type.
func BuildGxCCRI(sc *radius.SessionContext, originHost, originRealm, destRealm string, hopID, endID uint32) *argusdiameter.Message {
	msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGx, hopID, endID)

	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeSessionID, argusdiameter.AVPFlagMandatory, 0, sc.AcctSessionID))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, originHost))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, originRealm))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeDestinationRealm, argusdiameter.AVPFlagMandatory, 0, destRealm))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGx))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestType, argusdiameter.AVPFlagMandatory, 0, argusdiameter.CCRequestTypeInitial))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestNumber, argusdiameter.AVPFlagMandatory, 0, 0))

	msisdn := derefString(sc.SIM.MSISDN)
	for _, subID := range argusdiameter.BuildSubscriptionID(sc.SIM.IMSI, msisdn) {
		msg.AddAVP(subID)
	}

	if ip4 := framedIP4(sc.FramedIP); ip4 != nil {
		msg.AddAVP(argusdiameter.NewAVPAddress(avpCodeFramedIPAddress, argusdiameter.AVPFlagMandatory, 0, *ip4))
	}

	// IP-CAN-Type = 0 (3GPP-GPRS) per RFC 7155 + docs/architecture/PROTOCOLS.md
	// (table `0=3GPP-GPRS, 5=3GPP-EPS`). Server is permissive but we match the
	// documented enumeration.
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeIPCANType, argusdiameter.AVPFlagMandatory, argusdiameter.VendorID3GPP, 0))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeRATType3GPP, argusdiameter.AVPFlagMandatory, argusdiameter.VendorID3GPP, 1004))

	return msg
}

// BuildGxCCRT builds a Gx Credit-Control-Request Termination (CCR-T) message.
// Gx CCR-T does not carry Used-Service-Unit (Gx is policy-only, not charging).
// AVPs: Session-Id, Origin-Host, Origin-Realm, Destination-Realm,
// Auth-Application-Id, CC-Request-Type, CC-Request-Number, Subscription-Id.
func BuildGxCCRT(sc *radius.SessionContext, originHost, originRealm, destRealm string, hopID, endID, reqNum uint32) *argusdiameter.Message {
	msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGx, hopID, endID)

	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeSessionID, argusdiameter.AVPFlagMandatory, 0, sc.AcctSessionID))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, originHost))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, originRealm))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeDestinationRealm, argusdiameter.AVPFlagMandatory, 0, destRealm))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGx))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestType, argusdiameter.AVPFlagMandatory, 0, argusdiameter.CCRequestTypeTermination))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestNumber, argusdiameter.AVPFlagMandatory, 0, reqNum))

	msisdn := derefString(sc.SIM.MSISDN)
	for _, subID := range argusdiameter.BuildSubscriptionID(sc.SIM.IMSI, msisdn) {
		msg.AddAVP(subID)
	}

	return msg
}

// BuildGyCCRI builds a Gy Credit-Control-Request Initial (CCR-I) message.
// Includes the same base AVPs as BuildGxCCRI plus Requested-Service-Unit
// with CC-Total-Octets = requestedOctets.
func BuildGyCCRI(sc *radius.SessionContext, originHost, originRealm, destRealm string, hopID, endID uint32, requestedOctets uint64) *argusdiameter.Message {
	msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGy, hopID, endID)

	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeSessionID, argusdiameter.AVPFlagMandatory, 0, sc.AcctSessionID))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, originHost))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, originRealm))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeDestinationRealm, argusdiameter.AVPFlagMandatory, 0, destRealm))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGy))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestType, argusdiameter.AVPFlagMandatory, 0, argusdiameter.CCRequestTypeInitial))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestNumber, argusdiameter.AVPFlagMandatory, 0, 0))

	msisdn := derefString(sc.SIM.MSISDN)
	for _, subID := range argusdiameter.BuildSubscriptionID(sc.SIM.IMSI, msisdn) {
		msg.AddAVP(subID)
	}

	if ip4 := framedIP4(sc.FramedIP); ip4 != nil {
		msg.AddAVP(argusdiameter.NewAVPAddress(avpCodeFramedIPAddress, argusdiameter.AVPFlagMandatory, 0, *ip4))
	}

	// IP-CAN-Type = 0 (3GPP-GPRS) per RFC 7155 + docs/architecture/PROTOCOLS.md.
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeIPCANType, argusdiameter.AVPFlagMandatory, argusdiameter.VendorID3GPP, 0))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeRATType3GPP, argusdiameter.AVPFlagMandatory, argusdiameter.VendorID3GPP, 1004))

	rsu := argusdiameter.NewAVPGrouped(argusdiameter.AVPCodeRequestedServiceUnit, argusdiameter.AVPFlagMandatory, 0, []*argusdiameter.AVP{
		argusdiameter.NewAVPUint64(argusdiameter.AVPCodeCCTotalOctets, argusdiameter.AVPFlagMandatory, 0, requestedOctets),
	})
	msg.AddAVP(rsu)

	return msg
}

// BuildGyCCRU builds a Gy Credit-Control-Request Update (CCR-U) message.
// Includes Used-Service-Unit (deltaIn/deltaOut bytes + deltaSec seconds
// since last update) and Requested-Service-Unit for the next chunk
// (DefaultGrantedOctets).
func BuildGyCCRU(sc *radius.SessionContext, originHost, originRealm, destRealm string, hopID, endID, reqNum uint32, deltaIn, deltaOut uint64, deltaSec uint32) *argusdiameter.Message {
	msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGy, hopID, endID)

	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeSessionID, argusdiameter.AVPFlagMandatory, 0, sc.AcctSessionID))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, originHost))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, originRealm))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeDestinationRealm, argusdiameter.AVPFlagMandatory, 0, destRealm))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGy))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestType, argusdiameter.AVPFlagMandatory, 0, argusdiameter.CCRequestTypeUpdate))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestNumber, argusdiameter.AVPFlagMandatory, 0, reqNum))

	msisdn := derefString(sc.SIM.MSISDN)
	for _, subID := range argusdiameter.BuildSubscriptionID(sc.SIM.IMSI, msisdn) {
		msg.AddAVP(subID)
	}

	usu := buildUsedServiceUnit(deltaIn, deltaOut, deltaSec)
	msg.AddAVP(usu)

	rsu := argusdiameter.NewAVPGrouped(argusdiameter.AVPCodeRequestedServiceUnit, argusdiameter.AVPFlagMandatory, 0, []*argusdiameter.AVP{
		argusdiameter.NewAVPUint64(argusdiameter.AVPCodeCCTotalOctets, argusdiameter.AVPFlagMandatory, 0, argusdiameter.DefaultGrantedOctets),
	})
	msg.AddAVP(rsu)

	return msg
}

// BuildGyCCRT builds a Gy Credit-Control-Request Termination (CCR-T) message.
// Includes final Used-Service-Unit and does NOT request further credit.
func BuildGyCCRT(sc *radius.SessionContext, originHost, originRealm, destRealm string, hopID, endID, reqNum uint32, finalIn, finalOut uint64, finalSec uint32) *argusdiameter.Message {
	msg := argusdiameter.NewRequest(argusdiameter.CommandCCR, argusdiameter.ApplicationIDGy, hopID, endID)

	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeSessionID, argusdiameter.AVPFlagMandatory, 0, sc.AcctSessionID))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginHost, argusdiameter.AVPFlagMandatory, 0, originHost))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeOriginRealm, argusdiameter.AVPFlagMandatory, 0, originRealm))
	msg.AddAVP(argusdiameter.NewAVPString(argusdiameter.AVPCodeDestinationRealm, argusdiameter.AVPFlagMandatory, 0, destRealm))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeAuthApplicationID, argusdiameter.AVPFlagMandatory, 0, argusdiameter.ApplicationIDGy))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestType, argusdiameter.AVPFlagMandatory, 0, argusdiameter.CCRequestTypeTermination))
	msg.AddAVP(argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCRequestNumber, argusdiameter.AVPFlagMandatory, 0, reqNum))

	msisdn := derefString(sc.SIM.MSISDN)
	for _, subID := range argusdiameter.BuildSubscriptionID(sc.SIM.IMSI, msisdn) {
		msg.AddAVP(subID)
	}

	usu := buildUsedServiceUnit(finalIn, finalOut, finalSec)
	msg.AddAVP(usu)

	return msg
}

// buildUsedServiceUnit assembles a Used-Service-Unit grouped AVP with
// CC-Input-Octets, CC-Output-Octets, and CC-Time.
func buildUsedServiceUnit(inputOctets, outputOctets uint64, timeSec uint32) *argusdiameter.AVP {
	inner := []*argusdiameter.AVP{
		argusdiameter.NewAVPUint64(argusdiameter.AVPCodeCCInputOctets, argusdiameter.AVPFlagMandatory, 0, inputOctets),
		argusdiameter.NewAVPUint64(argusdiameter.AVPCodeCCOutputOctets, argusdiameter.AVPFlagMandatory, 0, outputOctets),
		argusdiameter.NewAVPUint32(argusdiameter.AVPCodeCCTime, argusdiameter.AVPFlagMandatory, 0, timeSec),
	}
	return argusdiameter.NewAVPGrouped(argusdiameter.AVPCodeUsedServiceUnit, argusdiameter.AVPFlagMandatory, 0, inner)
}

// framedIP4 converts net.IP to a 4-byte array suitable for NewAVPAddress.
// Returns nil when the IP is nil or not an IPv4 address.
func framedIP4(ip net.IP) *[4]byte {
	if ip == nil {
		return nil
	}
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	var arr [4]byte
	copy(arr[:], v4)
	return &arr
}

// derefString dereferences a *string, returning "" for nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
