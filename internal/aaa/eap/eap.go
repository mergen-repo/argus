package eap

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	CodeRequest  Code = 1
	CodeResponse Code = 2
	CodeSuccess  Code = 3
	CodeFailure  Code = 4

	MethodIdentity     MethodType = 1
	MethodNotification MethodType = 2
	MethodNAK          MethodType = 3
	MethodSIM          MethodType = 18
	MethodAKA          MethodType = 23
	MethodAKAPrime     MethodType = 50

	HeaderLen = 4
	MinLen    = 4
)

type Code uint8
type MethodType uint8

var (
	ErrPacketTooShort = errors.New("eap: packet too short")
	ErrLengthMismatch = errors.New("eap: length field mismatch")
	ErrUnknownMethod  = errors.New("eap: unknown method")
)

func (c Code) String() string {
	switch c {
	case CodeRequest:
		return "Request"
	case CodeResponse:
		return "Response"
	case CodeSuccess:
		return "Success"
	case CodeFailure:
		return "Failure"
	default:
		return fmt.Sprintf("Code(%d)", c)
	}
}

func (m MethodType) String() string {
	switch m {
	case MethodIdentity:
		return "Identity"
	case MethodNotification:
		return "Notification"
	case MethodNAK:
		return "NAK"
	case MethodSIM:
		return "EAP-SIM"
	case MethodAKA:
		return "EAP-AKA"
	case MethodAKAPrime:
		return "EAP-AKA'"
	default:
		return fmt.Sprintf("Method(%d)", m)
	}
}

type Packet struct {
	Code       Code
	Identifier uint8
	Length     uint16
	Type       MethodType
	Data       []byte
}

func Decode(raw []byte) (*Packet, error) {
	if len(raw) < MinLen {
		return nil, ErrPacketTooShort
	}

	pkt := &Packet{
		Code:       Code(raw[0]),
		Identifier: raw[1],
		Length:     binary.BigEndian.Uint16(raw[2:4]),
	}

	if int(pkt.Length) > len(raw) {
		return nil, ErrLengthMismatch
	}

	if pkt.Code == CodeSuccess || pkt.Code == CodeFailure {
		return pkt, nil
	}

	if pkt.Length < 5 {
		return nil, ErrPacketTooShort
	}

	pkt.Type = MethodType(raw[4])

	if pkt.Length > 5 {
		pkt.Data = make([]byte, pkt.Length-5)
		copy(pkt.Data, raw[5:pkt.Length])
	}

	return pkt, nil
}

func Encode(pkt *Packet) []byte {
	if pkt.Code == CodeSuccess || pkt.Code == CodeFailure {
		buf := make([]byte, 4)
		buf[0] = byte(pkt.Code)
		buf[1] = pkt.Identifier
		binary.BigEndian.PutUint16(buf[2:4], 4)
		return buf
	}

	length := uint16(5 + len(pkt.Data))
	buf := make([]byte, length)
	buf[0] = byte(pkt.Code)
	buf[1] = pkt.Identifier
	binary.BigEndian.PutUint16(buf[2:4], length)
	buf[4] = byte(pkt.Type)
	if len(pkt.Data) > 0 {
		copy(buf[5:], pkt.Data)
	}
	return buf
}

func NewRequest(id uint8, method MethodType, data []byte) *Packet {
	return &Packet{
		Code:       CodeRequest,
		Identifier: id,
		Type:       method,
		Data:       data,
	}
}

func NewResponse(id uint8, method MethodType, data []byte) *Packet {
	return &Packet{
		Code:       CodeResponse,
		Identifier: id,
		Type:       method,
		Data:       data,
	}
}

func NewSuccess(id uint8) *Packet {
	return &Packet{
		Code:       CodeSuccess,
		Identifier: id,
	}
}

func NewFailure(id uint8) *Packet {
	return &Packet{
		Code:       CodeFailure,
		Identifier: id,
	}
}

func NewNAK(id uint8, supportedMethods []MethodType) *Packet {
	data := make([]byte, len(supportedMethods))
	for i, m := range supportedMethods {
		data[i] = byte(m)
	}
	return &Packet{
		Code:       CodeResponse,
		Identifier: id,
		Type:       MethodNAK,
		Data:       data,
	}
}

func NewIdentityRequest(id uint8) *Packet {
	return NewRequest(id, MethodIdentity, nil)
}

func NewIdentityResponse(id uint8, identity string) *Packet {
	return NewResponse(id, MethodIdentity, []byte(identity))
}
