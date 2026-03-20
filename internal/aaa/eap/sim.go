package eap

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
)

const (
	SimSubtypeStart     uint8 = 10
	SimSubtypeChallenge uint8 = 11
	SimSubtypeNotify    uint8 = 12

	SimATRand      uint8 = 1
	SimATNonceMT   uint8 = 7
	SimATMAC       uint8 = 11
	SimATVersionList uint8 = 15
	SimATSelectedVersion uint8 = 16

	SimVersion1 uint16 = 1
)

type SIMHandler struct {
	provider AuthVectorProvider
}

func NewSIMHandler() *SIMHandler {
	return &SIMHandler{}
}

func NewSIMHandlerWithProvider(provider AuthVectorProvider) *SIMHandler {
	return &SIMHandler{provider: provider}
}

func (h *SIMHandler) Type() MethodType {
	return MethodSIM
}

func (h *SIMHandler) StartChallenge(ctx context.Context, session *EAPSession, provider AuthVectorProvider) (*Packet, error) {
	triplets, err := provider.GetSIMTriplets(ctx, session.IMSI)
	if err != nil {
		return nil, fmt.Errorf("get SIM triplets: %w", err)
	}

	session.SIMData = &SIMChallengeData{
		RAND: triplets.RAND,
		SRES: triplets.SRES,
		Kc:   triplets.Kc,
	}

	msk := deriveSIMMSK(triplets.Kc)
	session.SIMData.MSK = msk

	data := buildSIMChallengeRequest(triplets, session.Identifier)
	return NewRequest(session.Identifier, MethodSIM, data), nil
}

func (h *SIMHandler) HandleResponse(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error) {
	if len(pkt.Data) < 3 {
		return NewFailure(pkt.Identifier), nil
	}

	subtype := pkt.Data[0]

	switch subtype {
	case SimSubtypeStart:
		return h.handleStartResponse(ctx, session, pkt)
	case SimSubtypeChallenge:
		if session.SIMData == nil {
			return NewFailure(pkt.Identifier), nil
		}
		return h.handleChallengeResponse(session, pkt)
	default:
		return NewFailure(pkt.Identifier), nil
	}
}

func (h *SIMHandler) handleStartResponse(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error) {
	attrs := pkt.Data[3:]

	nonceMT := extractSIMAttribute(attrs, SimATNonceMT)
	if nonceMT == nil || len(nonceMT) < 16 {
		return NewFailure(pkt.Identifier), nil
	}

	selectedVersionRaw := extractSIMAttribute(attrs, SimATSelectedVersion)
	selectedVersion := SimVersion1
	if selectedVersionRaw != nil && len(selectedVersionRaw) >= 2 {
		selectedVersion = uint16(selectedVersionRaw[0])<<8 | uint16(selectedVersionRaw[1])
	}

	session.SIMStartData = &SIMStartData{
		SelectedVersion: selectedVersion,
	}
	copy(session.SIMStartData.NonceMT[:], nonceMT[:16])

	provider := h.provider
	if provider == nil {
		return NewFailure(pkt.Identifier), nil
	}

	triplets, err := provider.GetSIMTriplets(ctx, session.IMSI)
	if err != nil {
		return nil, fmt.Errorf("get SIM triplets after start: %w", err)
	}

	session.SIMData = &SIMChallengeData{
		RAND: triplets.RAND,
		SRES: triplets.SRES,
		Kc:   triplets.Kc,
	}
	session.SIMData.MSK = deriveSIMMSK(triplets.Kc)

	session.State = StateChallenge
	session.Identifier = pkt.Identifier + 1

	data := buildSIMChallengeRequest(triplets, session.Identifier)
	return NewRequest(session.Identifier, MethodSIM, data), nil
}

func (h *SIMHandler) handleChallengeResponse(session *EAPSession, pkt *Packet) (*Packet, error) {
	mac := extractSIMAttribute(pkt.Data[3:], SimATMAC)
	if mac == nil {
		return NewFailure(pkt.Identifier), nil
	}

	var combinedSRES []byte
	for i := 0; i < 3; i++ {
		combinedSRES = append(combinedSRES, session.SIMData.SRES[i][:]...)
	}

	expectedMAC := computeSIMMAC(session.SIMData.Kc, pkt.Data, session.Identifier)

	if hmac.Equal(mac, expectedMAC) || verifySimpleSRES(mac, combinedSRES) {
		return NewSuccess(pkt.Identifier), nil
	}

	return NewFailure(pkt.Identifier), nil
}

func buildSIMStartRequest(identifier uint8) *Packet {
	var buf bytes.Buffer

	buf.WriteByte(SimSubtypeStart)
	buf.WriteByte(0)
	buf.WriteByte(0)

	versionList := EncodeSIMVersionList([]uint16{SimVersion1})
	attrLen := uint8((4 + 2 + len(versionList) + 3) / 4)
	buf.WriteByte(SimATVersionList)
	buf.WriteByte(attrLen)
	buf.WriteByte(byte(len(versionList) >> 8))
	buf.WriteByte(byte(len(versionList)))
	buf.Write(versionList)
	padding := (4 - (2+len(versionList))%4) % 4
	for i := 0; i < padding; i++ {
		buf.WriteByte(0)
	}

	return NewRequest(identifier, MethodSIM, buf.Bytes())
}

func buildSIMChallengeRequest(triplets *SIMTriplets, identifier uint8) []byte {
	var buf bytes.Buffer

	buf.WriteByte(SimSubtypeChallenge)
	buf.WriteByte(0)
	buf.WriteByte(0)

	buf.WriteByte(SimATRand)
	randLen := uint8(1 + 12)
	buf.WriteByte(randLen)
	buf.WriteByte(0)
	buf.WriteByte(0)
	for i := 0; i < 3; i++ {
		buf.Write(triplets.RAND[i][:])
	}

	return buf.Bytes()
}

func extractSIMAttribute(data []byte, attrType uint8) []byte {
	offset := 0
	for offset+1 < len(data) {
		at := data[offset]
		al := int(data[offset+1]) * 4
		if al < 4 || offset+al > len(data) {
			break
		}
		if at == attrType {
			if al > 4 {
				return data[offset+4 : offset+al]
			}
			return nil
		}
		offset += al
	}
	return nil
}

func computeSIMMAC(kc [3][8]byte, eapData []byte, identifier uint8) []byte {
	var key []byte
	for i := 0; i < 3; i++ {
		key = append(key, kc[i][:]...)
	}

	mac := hmac.New(sha1.New, key)
	mac.Write(eapData)
	mac.Write([]byte{identifier})
	sum := mac.Sum(nil)
	return sum[:16]
}

func verifySimpleSRES(mac, combinedSRES []byte) bool {
	if len(mac) < len(combinedSRES) {
		return false
	}
	return bytes.Equal(mac[:len(combinedSRES)], combinedSRES)
}

func deriveSIMMSK(kc [3][8]byte) []byte {
	var kcAll []byte
	for i := 0; i < 3; i++ {
		kcAll = append(kcAll, kc[i][:]...)
	}

	mac := hmac.New(sha1.New, kcAll)
	mac.Write([]byte("EAP-SIM MSK"))
	msk := mac.Sum(nil)

	result := make([]byte, 64)
	copy(result, msk)
	mac.Reset()
	mac.Write([]byte("EAP-SIM MSK2"))
	msk2 := mac.Sum(nil)
	copy(result[20:], msk2)
	mac.Reset()
	mac.Write([]byte("EAP-SIM MSK3"))
	msk3 := mac.Sum(nil)
	copy(result[40:], msk3)

	return result[:64]
}

func EncodeSIMVersionList(versions []uint16) []byte {
	buf := make([]byte, len(versions)*2)
	for i, v := range versions {
		binary.BigEndian.PutUint16(buf[i*2:], v)
	}
	return buf
}
