package diameter

import (
	"encoding/binary"
	"fmt"
)

const (
	DiameterHeaderLen = 20

	MsgFlagRequest    uint8 = 0x80
	MsgFlagProxiable  uint8 = 0x40
	MsgFlagError      uint8 = 0x20
	MsgFlagRetransmit uint8 = 0x10
)

const (
	CommandCER uint32 = 257
	CommandCEA uint32 = 257
	CommandDWR uint32 = 280
	CommandDWA uint32 = 280
	CommandDPR uint32 = 282
	CommandDPA uint32 = 282
	CommandCCR uint32 = 272
	CommandCCA uint32 = 272
	CommandRAR uint32 = 258
	CommandRAA uint32 = 258
)

type Message struct {
	Version       uint8
	Flags         uint8
	CommandCode   uint32
	ApplicationID uint32
	HopByHopID    uint32
	EndToEndID    uint32
	AVPs          []*AVP
}

func (m *Message) IsRequest() bool {
	return m.Flags&MsgFlagRequest != 0
}

func (m *Message) IsError() bool {
	return m.Flags&MsgFlagError != 0
}

func (m *Message) FindAVP(code uint32) *AVP {
	return FindAVP(m.AVPs, code)
}

func (m *Message) FindAVPVendor(code uint32, vendorID uint32) *AVP {
	return FindAVPVendor(m.AVPs, code, vendorID)
}

func (m *Message) GetSessionID() string {
	if a := m.FindAVP(AVPCodeSessionID); a != nil {
		return a.GetString()
	}
	return ""
}

func (m *Message) GetOriginHost() string {
	if a := m.FindAVP(AVPCodeOriginHost); a != nil {
		return a.GetString()
	}
	return ""
}

func (m *Message) GetOriginRealm() string {
	if a := m.FindAVP(AVPCodeOriginRealm); a != nil {
		return a.GetString()
	}
	return ""
}

func (m *Message) GetResultCode() uint32 {
	if a := m.FindAVP(AVPCodeResultCode); a != nil {
		v, err := a.GetUint32()
		if err == nil {
			return v
		}
	}
	return 0
}

func (m *Message) GetCCRequestType() uint32 {
	if a := m.FindAVP(AVPCodeCCRequestType); a != nil {
		v, err := a.GetUint32()
		if err == nil {
			return v
		}
	}
	return 0
}

func (m *Message) GetCCRequestNumber() uint32 {
	if a := m.FindAVP(AVPCodeCCRequestNumber); a != nil {
		v, err := a.GetUint32()
		if err == nil {
			return v
		}
	}
	return 0
}

func (m *Message) AddAVP(avp *AVP) {
	m.AVPs = append(m.AVPs, avp)
}

func (m *Message) Encode() ([]byte, error) {
	var avpData []byte
	for _, a := range m.AVPs {
		avpData = append(avpData, a.Encode()...)
	}

	msgLen := DiameterHeaderLen + len(avpData)
	if msgLen > 0xFFFFFF {
		return nil, fmt.Errorf("diameter message too large: %d", msgLen)
	}

	buf := make([]byte, msgLen)
	buf[0] = m.Version
	buf[1] = byte(msgLen >> 16)
	buf[2] = byte(msgLen >> 8)
	buf[3] = byte(msgLen)
	buf[4] = m.Flags
	buf[5] = byte(m.CommandCode >> 16)
	buf[6] = byte(m.CommandCode >> 8)
	buf[7] = byte(m.CommandCode)
	binary.BigEndian.PutUint32(buf[8:12], m.ApplicationID)
	binary.BigEndian.PutUint32(buf[12:16], m.HopByHopID)
	binary.BigEndian.PutUint32(buf[16:20], m.EndToEndID)

	copy(buf[DiameterHeaderLen:], avpData)
	return buf, nil
}

func DecodeMessage(data []byte) (*Message, error) {
	if len(data) < DiameterHeaderLen {
		return nil, fmt.Errorf("data too short for diameter header: %d", len(data))
	}

	m := &Message{}
	m.Version = data[0]
	if m.Version != 1 {
		return nil, fmt.Errorf("unsupported diameter version: %d", m.Version)
	}

	msgLen := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if msgLen < DiameterHeaderLen {
		return nil, fmt.Errorf("invalid message length: %d", msgLen)
	}
	if msgLen > len(data) {
		return nil, fmt.Errorf("message length %d exceeds data length %d", msgLen, len(data))
	}

	m.Flags = data[4]
	m.CommandCode = uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
	m.ApplicationID = binary.BigEndian.Uint32(data[8:12])
	m.HopByHopID = binary.BigEndian.Uint32(data[12:16])
	m.EndToEndID = binary.BigEndian.Uint32(data[16:20])

	if msgLen > DiameterHeaderLen {
		avps, err := DecodeAVPs(data[DiameterHeaderLen:msgLen])
		if err != nil {
			return nil, fmt.Errorf("decode avps: %w", err)
		}
		m.AVPs = avps
	}

	return m, nil
}

func NewRequest(commandCode, appID, hopByHop, endToEnd uint32) *Message {
	return &Message{
		Version:       1,
		Flags:         MsgFlagRequest,
		CommandCode:   commandCode,
		ApplicationID: appID,
		HopByHopID:    hopByHop,
		EndToEndID:    endToEnd,
	}
}

func NewAnswer(req *Message) *Message {
	return &Message{
		Version:       1,
		Flags:         0,
		CommandCode:   req.CommandCode,
		ApplicationID: req.ApplicationID,
		HopByHopID:    req.HopByHopID,
		EndToEndID:    req.EndToEndID,
	}
}

func NewErrorAnswer(req *Message, resultCode uint32) *Message {
	ans := NewAnswer(req)
	ans.Flags = MsgFlagError
	ans.AddAVP(NewAVPUint32(AVPCodeResultCode, AVPFlagMandatory, 0, resultCode))
	return ans
}

func ReadMessageLength(header []byte) (int, error) {
	if len(header) < 4 {
		return 0, fmt.Errorf("header too short: %d", len(header))
	}
	if header[0] != 1 {
		return 0, fmt.Errorf("unsupported diameter version: %d", header[0])
	}
	msgLen := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	if msgLen < DiameterHeaderLen {
		return 0, fmt.Errorf("invalid message length: %d", msgLen)
	}
	return msgLen, nil
}
