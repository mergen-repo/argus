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

type SIMHandler struct{}

func NewSIMHandler() *SIMHandler {
	return &SIMHandler{}
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
	if session.SIMData == nil {
		return NewFailure(pkt.Identifier), nil
	}

	if len(pkt.Data) < 3 {
		return NewFailure(pkt.Identifier), nil
	}

	subtype := pkt.Data[0]

	switch subtype {
	case SimSubtypeChallenge:
		return h.handleChallengeResponse(session, pkt)
	default:
		return NewFailure(pkt.Identifier), nil
	}
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
