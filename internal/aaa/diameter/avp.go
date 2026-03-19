package diameter

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	AVPFlagVendor    uint8 = 0x80
	AVPFlagMandatory uint8 = 0x40
	AVPFlagProtected uint8 = 0x20

	VendorID3GPP uint32 = 10415
)

const (
	AVPCodeSessionID          uint32 = 263
	AVPCodeOriginHost         uint32 = 264
	AVPCodeOriginRealm        uint32 = 296
	AVPCodeHostIPAddress      uint32 = 257
	AVPCodeVendorID           uint32 = 266
	AVPCodeProductName        uint32 = 269
	AVPCodeResultCode         uint32 = 268
	AVPCodeAuthApplicationID  uint32 = 258
	AVPCodeAcctApplicationID  uint32 = 259
	AVPCodeFirmwareRevision   uint32 = 267
	AVPCodeDestinationHost    uint32 = 293
	AVPCodeDestinationRealm   uint32 = 283
	AVPCodeDisconnectCause    uint32 = 273
	AVPCodeOriginStateID      uint32 = 278
	AVPCodeSupportedVendorID  uint32 = 265

	AVPCodeCCRequestType      uint32 = 416
	AVPCodeCCRequestNumber    uint32 = 415
	AVPCodeSubscriptionID     uint32 = 443
	AVPCodeSubscriptionIDType uint32 = 450
	AVPCodeSubscriptionIDData uint32 = 444

	AVPCodeUsedServiceUnit    uint32 = 446
	AVPCodeGrantedServiceUnit uint32 = 431
	AVPCodeRequestedServiceUnit uint32 = 437
	AVPCodeCCTotalOctets      uint32 = 421
	AVPCodeCCInputOctets      uint32 = 412
	AVPCodeCCOutputOctets     uint32 = 414
	AVPCodeCCTime             uint32 = 420
	AVPCodeValidityTime       uint32 = 448
	AVPCodeFinalUnitIndication uint32 = 430
	AVPCodeFinalUnitAction    uint32 = 449
	AVPCodeRatingGroup        uint32 = 432
	AVPCodeServiceIdentifier  uint32 = 439
	AVPCodeSessionTimeout     uint32 = 27

	AVPCodeChargingRuleInstall    uint32 = 1001
	AVPCodeChargingRuleRemove     uint32 = 1002
	AVPCodeChargingRuleDefinition uint32 = 1003
	AVPCodeChargingRuleName       uint32 = 1005
	AVPCodeEventTrigger           uint32 = 1006
	AVPCodeQoSInformation         uint32 = 1016
	AVPCodeBearerIdentifier       uint32 = 1020
	AVPCodeIPCANType              uint32 = 1027
	AVPCodeQoSClassIdentifier     uint32 = 1028
	AVPCodeRATType3GPP            uint32 = 1032

	AVPCodeMaxRequestedBandwidthUL uint32 = 516
	AVPCodeMaxRequestedBandwidthDL uint32 = 515

	AVPCode3GPPUserLocationInfo uint32 = 22
)

const (
	ResultCodeSuccess                    uint32 = 2001
	ResultCodeUnableToDeliver            uint32 = 3002
	ResultCodeAuthenticationRejected     uint32 = 4001
	ResultCodeAVPUnsupported             uint32 = 5001
	ResultCodeUnknownSessionID           uint32 = 5002
	ResultCodeUnableToComply             uint32 = 5012
	ResultCodeApplicationUnsupported     uint32 = 3007
	ResultCodeInvalidAVPValue            uint32 = 5004
	ResultCodeMissingAVP                 uint32 = 5005
)

const (
	CCRequestTypeInitial     uint32 = 1
	CCRequestTypeUpdate      uint32 = 2
	CCRequestTypeTermination uint32 = 3
	CCRequestTypeEvent       uint32 = 4
)

const (
	SubscriptionIDTypeIMSI   uint32 = 1
	SubscriptionIDTypeMSISDN uint32 = 0
)

const (
	DisconnectCauseRebooting     uint32 = 0
	DisconnectCauseBusy          uint32 = 1
	DisconnectCauseDoNotWant     uint32 = 2
)

const (
	ApplicationIDGx            uint32 = 16777238
	ApplicationIDGy            uint32 = 4
	ApplicationIDDiameterBase  uint32 = 0
)

type AVP struct {
	Code     uint32
	Flags    uint8
	VendorID uint32
	Data     []byte
}

func (a *AVP) IsVendor() bool {
	return a.Flags&AVPFlagVendor != 0
}

func (a *AVP) IsMandatory() bool {
	return a.Flags&AVPFlagMandatory != 0
}

func (a *AVP) GetUint32() (uint32, error) {
	if len(a.Data) < 4 {
		return 0, fmt.Errorf("avp data too short for uint32: %d", len(a.Data))
	}
	return binary.BigEndian.Uint32(a.Data[:4]), nil
}

func (a *AVP) GetUint64() (uint64, error) {
	if len(a.Data) < 8 {
		return 0, fmt.Errorf("avp data too short for uint64: %d", len(a.Data))
	}
	return binary.BigEndian.Uint64(a.Data[:8]), nil
}

func (a *AVP) GetString() string {
	return string(a.Data)
}

func (a *AVP) GetGrouped() ([]*AVP, error) {
	return DecodeAVPs(a.Data)
}

func (a *AVP) headerLen() int {
	if a.IsVendor() {
		return 12
	}
	return 8
}

func (a *AVP) Len() int {
	return a.headerLen() + len(a.Data)
}

func (a *AVP) PaddedLen() int {
	l := a.Len()
	return (l + 3) & ^3
}

func (a *AVP) Encode() []byte {
	avpLen := a.Len()
	paddedLen := a.PaddedLen()
	buf := make([]byte, paddedLen)

	binary.BigEndian.PutUint32(buf[0:4], a.Code)

	flags := a.Flags
	if a.VendorID != 0 {
		flags |= AVPFlagVendor
	}
	buf[4] = flags
	buf[5] = byte(avpLen >> 16)
	buf[6] = byte(avpLen >> 8)
	buf[7] = byte(avpLen)

	offset := 8
	if flags&AVPFlagVendor != 0 {
		binary.BigEndian.PutUint32(buf[offset:offset+4], a.VendorID)
		offset += 4
	}

	copy(buf[offset:], a.Data)
	return buf
}

func DecodeAVP(data []byte) (*AVP, int, error) {
	if len(data) < 8 {
		return nil, 0, fmt.Errorf("avp data too short: %d", len(data))
	}

	avp := &AVP{}
	avp.Code = binary.BigEndian.Uint32(data[0:4])
	avp.Flags = data[4]

	avpLen := int(data[5])<<16 | int(data[6])<<8 | int(data[7])
	if avpLen < 8 {
		return nil, 0, fmt.Errorf("invalid avp length: %d", avpLen)
	}

	paddedLen := (avpLen + 3) & ^3
	if paddedLen > len(data) {
		return nil, 0, fmt.Errorf("avp length %d exceeds data length %d", paddedLen, len(data))
	}

	headerLen := 8
	if avp.Flags&AVPFlagVendor != 0 {
		if avpLen < 12 {
			return nil, 0, fmt.Errorf("vendor avp length too short: %d", avpLen)
		}
		avp.VendorID = binary.BigEndian.Uint32(data[8:12])
		headerLen = 12
	}

	dataLen := avpLen - headerLen
	if dataLen > 0 {
		avp.Data = make([]byte, dataLen)
		copy(avp.Data, data[headerLen:headerLen+dataLen])
	}

	return avp, paddedLen, nil
}

func DecodeAVPs(data []byte) ([]*AVP, error) {
	var avps []*AVP
	offset := 0
	for offset < len(data) {
		if offset+8 > len(data) {
			break
		}
		avp, consumed, err := DecodeAVP(data[offset:])
		if err != nil {
			return avps, fmt.Errorf("decode avp at offset %d: %w", offset, err)
		}
		avps = append(avps, avp)
		offset += consumed
	}
	return avps, nil
}

func NewAVPUint32(code uint32, flags uint8, vendorID uint32, value uint32) *AVP {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, value)
	f := flags
	if vendorID != 0 {
		f |= AVPFlagVendor
	}
	return &AVP{Code: code, Flags: f, VendorID: vendorID, Data: data}
}

func NewAVPUint64(code uint32, flags uint8, vendorID uint32, value uint64) *AVP {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, value)
	f := flags
	if vendorID != 0 {
		f |= AVPFlagVendor
	}
	return &AVP{Code: code, Flags: f, VendorID: vendorID, Data: data}
}

func NewAVPString(code uint32, flags uint8, vendorID uint32, value string) *AVP {
	f := flags
	if vendorID != 0 {
		f |= AVPFlagVendor
	}
	return &AVP{Code: code, Flags: f, VendorID: vendorID, Data: []byte(value)}
}

func NewAVPBytes(code uint32, flags uint8, vendorID uint32, value []byte) *AVP {
	f := flags
	if vendorID != 0 {
		f |= AVPFlagVendor
	}
	return &AVP{Code: code, Flags: f, VendorID: vendorID, Data: value}
}

func NewAVPGrouped(code uint32, flags uint8, vendorID uint32, avps []*AVP) *AVP {
	var data []byte
	for _, a := range avps {
		data = append(data, a.Encode()...)
	}
	f := flags
	if vendorID != 0 {
		f |= AVPFlagVendor
	}
	return &AVP{Code: code, Flags: f, VendorID: vendorID, Data: data}
}

func NewAVPAddress(code uint32, flags uint8, vendorID uint32, ip [4]byte) *AVP {
	data := make([]byte, 6)
	binary.BigEndian.PutUint16(data[0:2], 1)
	copy(data[2:], ip[:])
	f := flags
	if vendorID != 0 {
		f |= AVPFlagVendor
	}
	return &AVP{Code: code, Flags: f, VendorID: vendorID, Data: data}
}

func FindAVP(avps []*AVP, code uint32) *AVP {
	for _, a := range avps {
		if a.Code == code {
			return a
		}
	}
	return nil
}

func FindAVPVendor(avps []*AVP, code uint32, vendorID uint32) *AVP {
	for _, a := range avps {
		if a.Code == code && a.VendorID == vendorID {
			return a
		}
	}
	return nil
}

func ExtractSubscriptionID(avps []*AVP) (imsi, msisdn string) {
	for _, a := range avps {
		if a.Code != AVPCodeSubscriptionID {
			continue
		}
		grouped, err := a.GetGrouped()
		if err != nil {
			continue
		}
		typeAVP := FindAVP(grouped, AVPCodeSubscriptionIDType)
		dataAVP := FindAVP(grouped, AVPCodeSubscriptionIDData)
		if typeAVP == nil || dataAVP == nil {
			continue
		}
		subType, err := typeAVP.GetUint32()
		if err != nil {
			continue
		}
		switch subType {
		case SubscriptionIDTypeIMSI:
			imsi = dataAVP.GetString()
		case SubscriptionIDTypeMSISDN:
			msisdn = dataAVP.GetString()
		}
	}
	return
}

func ExtractUsedServiceUnit(avps []*AVP) (totalOctets, inputOctets, outputOctets uint64, timeSec uint32) {
	usu := FindAVP(avps, AVPCodeUsedServiceUnit)
	if usu == nil {
		return
	}
	grouped, err := usu.GetGrouped()
	if err != nil {
		return
	}
	if a := FindAVP(grouped, AVPCodeCCTotalOctets); a != nil {
		totalOctets, _ = a.GetUint64()
	}
	if a := FindAVP(grouped, AVPCodeCCInputOctets); a != nil {
		inputOctets, _ = a.GetUint64()
	}
	if a := FindAVP(grouped, AVPCodeCCOutputOctets); a != nil {
		outputOctets, _ = a.GetUint64()
	}
	if a := FindAVP(grouped, AVPCodeCCTime); a != nil {
		t, _ := a.GetUint32()
		timeSec = t
	}
	if totalOctets == 0 && (inputOctets > 0 || outputOctets > 0) {
		totalOctets = inputOctets + outputOctets
	}
	return
}

func BuildGrantedServiceUnit(totalOctets uint64, timeSec uint32, validityTime uint32) *AVP {
	var inner []*AVP
	if totalOctets > 0 {
		inner = append(inner, NewAVPUint64(AVPCodeCCTotalOctets, AVPFlagMandatory, 0, totalOctets))
	}
	if timeSec > 0 {
		inner = append(inner, NewAVPUint32(AVPCodeCCTime, AVPFlagMandatory, 0, timeSec))
	}
	gsu := NewAVPGrouped(AVPCodeGrantedServiceUnit, AVPFlagMandatory, 0, inner)
	if validityTime > 0 {
		return NewAVPGrouped(AVPCodeGrantedServiceUnit, AVPFlagMandatory, 0, append(inner,
			NewAVPUint32(AVPCodeValidityTime, AVPFlagMandatory, 0, validityTime),
		))
	}
	return gsu
}

func BuildSubscriptionID(imsi, msisdn string) []*AVP {
	var avps []*AVP
	if imsi != "" {
		avps = append(avps, NewAVPGrouped(AVPCodeSubscriptionID, AVPFlagMandatory, 0, []*AVP{
			NewAVPUint32(AVPCodeSubscriptionIDType, AVPFlagMandatory, 0, SubscriptionIDTypeIMSI),
			NewAVPString(AVPCodeSubscriptionIDData, AVPFlagMandatory, 0, imsi),
		}))
	}
	if msisdn != "" {
		avps = append(avps, NewAVPGrouped(AVPCodeSubscriptionID, AVPFlagMandatory, 0, []*AVP{
			NewAVPUint32(AVPCodeSubscriptionIDType, AVPFlagMandatory, 0, SubscriptionIDTypeMSISDN),
			NewAVPString(AVPCodeSubscriptionIDData, AVPFlagMandatory, 0, msisdn),
		}))
	}
	return avps
}

func BuildChargingRuleInstall(ruleName string, qci uint32, maxBwUL, maxBwDL uint32) *AVP {
	qos := NewAVPGrouped(AVPCodeQoSInformation, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, []*AVP{
		NewAVPUint32(AVPCodeQoSClassIdentifier, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, qci),
		NewAVPUint32(AVPCodeMaxRequestedBandwidthUL, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, maxBwUL),
		NewAVPUint32(AVPCodeMaxRequestedBandwidthDL, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, maxBwDL),
	})

	ruleDef := NewAVPGrouped(AVPCodeChargingRuleDefinition, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, []*AVP{
		NewAVPString(AVPCodeChargingRuleName, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, ruleName),
		qos,
	})

	return NewAVPGrouped(AVPCodeChargingRuleInstall, AVPFlagMandatory|AVPFlagVendor, VendorID3GPP, []*AVP{ruleDef})
}

func BuildFinalUnitIndication(action uint32) *AVP {
	return NewAVPGrouped(AVPCodeFinalUnitIndication, AVPFlagMandatory, 0, []*AVP{
		NewAVPUint32(AVPCodeFinalUnitAction, AVPFlagMandatory, 0, action),
	})
}

const (
	FinalUnitActionTerminate     uint32 = 0
	FinalUnitActionRedirect      uint32 = 1
	FinalUnitActionRestrictAccess uint32 = 2

	DefaultGrantedOctets  uint64 = 100 * 1024 * 1024
	DefaultGrantedTimeSec uint32 = 3600
	DefaultValidityTime   uint32 = 600
	DefaultQCI            uint32 = 9
	DefaultMaxBwUL        uint32 = 10_000_000
	DefaultMaxBwDL        uint32 = 50_000_000

	MaxUint32 = math.MaxUint32
)
