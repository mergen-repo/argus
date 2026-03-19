package eap

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

const (
	AKASubtypeChallenge  uint8 = 1
	AKASubtypeAuthReject uint8 = 2
	AKASubtypeSyncFail   uint8 = 4
	AKASubtypeIdentity   uint8 = 5

	AKAATRand     uint8 = 1
	AKAATAutn     uint8 = 2
	AKAATRes      uint8 = 3
	AKAATAuts     uint8 = 4
	AKAATMAC      uint8 = 11
	AKAATCheckcode uint8 = 134

	AKAPrimeATKDF      uint8 = 23
	AKAPrimeATKDFInput uint8 = 24

	AKADefaultKDF uint16 = 1
)

type AKAHandler struct {
	prime bool
}

func NewAKAHandler() *AKAHandler {
	return &AKAHandler{prime: false}
}

func NewAKAPrimeHandler() *AKAHandler {
	return &AKAHandler{prime: true}
}

func (h *AKAHandler) Type() MethodType {
	if h.prime {
		return MethodAKAPrime
	}
	return MethodAKA
}

func (h *AKAHandler) StartChallenge(ctx context.Context, session *EAPSession, provider AuthVectorProvider) (*Packet, error) {
	quintets, err := provider.GetAKAQuintets(ctx, session.IMSI)
	if err != nil {
		return nil, fmt.Errorf("get AKA quintets: %w", err)
	}

	session.AKAData = &AKAChallengeData{
		RAND: quintets.RAND,
		AUTN: quintets.AUTN,
		XRES: quintets.XRES,
		CK:   quintets.CK,
		IK:   quintets.IK,
	}

	msk := deriveAKAMSK(quintets.CK, quintets.IK, h.prime)
	session.AKAData.MSK = msk

	data := h.buildChallengeRequest(quintets, session.Identifier)

	method := MethodAKA
	if h.prime {
		method = MethodAKAPrime
	}
	return NewRequest(session.Identifier, method, data), nil
}

func (h *AKAHandler) HandleResponse(ctx context.Context, session *EAPSession, pkt *Packet) (*Packet, error) {
	if session.AKAData == nil {
		return NewFailure(pkt.Identifier), nil
	}

	if len(pkt.Data) < 3 {
		return NewFailure(pkt.Identifier), nil
	}

	subtype := pkt.Data[0]

	switch subtype {
	case AKASubtypeChallenge:
		return h.handleChallengeResponse(session.AKAData, pkt)
	case AKASubtypeSyncFail:
		return h.handleSyncFailure(session.AKAData, pkt)
	case AKASubtypeAuthReject:
		return NewFailure(pkt.Identifier), nil
	default:
		return NewFailure(pkt.Identifier), nil
	}
}

func (h *AKAHandler) handleChallengeResponse(session *AKAChallengeData, pkt *Packet) (*Packet, error) {
	res := extractAKAAttribute(pkt.Data[3:], AKAATRes)
	if res == nil {
		return NewFailure(pkt.Identifier), nil
	}

	if len(res) >= 2 {
		resLenBits := int(res[0])<<8 | int(res[1])
		resLenBytes := resLenBits / 8
		if len(res) >= 2+resLenBytes {
			res = res[2 : 2+resLenBytes]
		}
	}

	if bytes.Equal(res, session.XRES) {
		return NewSuccess(pkt.Identifier), nil
	}

	return NewFailure(pkt.Identifier), nil
}

func (h *AKAHandler) handleSyncFailure(_ *AKAChallengeData, pkt *Packet) (*Packet, error) {
	return NewFailure(pkt.Identifier), nil
}

func (h *AKAHandler) buildChallengeRequest(quintets *AKAQuintets, identifier uint8) []byte {
	var buf bytes.Buffer

	buf.WriteByte(AKASubtypeChallenge)
	buf.WriteByte(0)
	buf.WriteByte(0)

	buf.WriteByte(AKAATRand)
	buf.WriteByte(5)
	buf.WriteByte(0)
	buf.WriteByte(0)
	buf.Write(quintets.RAND[:])

	buf.WriteByte(AKAATAutn)
	buf.WriteByte(5)
	buf.WriteByte(0)
	buf.WriteByte(0)
	buf.Write(quintets.AUTN[:])

	if h.prime {
		buf.WriteByte(AKAPrimeATKDF)
		buf.WriteByte(1)
		buf.WriteByte(0)
		buf.WriteByte(byte(AKADefaultKDF))

		networkName := []byte("argus.eap.5g")
		attrLen := uint8((4 + 2 + len(networkName) + 3) / 4)
		buf.WriteByte(AKAPrimeATKDFInput)
		buf.WriteByte(attrLen)
		lenBytes := make([]byte, 2)
		lenBytes[0] = byte(len(networkName) >> 8)
		lenBytes[1] = byte(len(networkName))
		buf.Write(lenBytes)
		buf.Write(networkName)
		padding := (4 - (2+len(networkName))%4) % 4
		for i := 0; i < padding; i++ {
			buf.WriteByte(0)
		}
	}

	mac := computeAKAMAC(quintets.CK, quintets.IK, buf.Bytes(), identifier)
	buf.WriteByte(AKAATMAC)
	buf.WriteByte(5)
	buf.WriteByte(0)
	buf.WriteByte(0)
	buf.Write(mac)

	return buf.Bytes()
}

func extractAKAAttribute(data []byte, attrType uint8) []byte {
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

func computeAKAMAC(ck, ik [16]byte, eapData []byte, identifier uint8) []byte {
	var key []byte
	key = append(key, ck[:]...)
	key = append(key, ik[:]...)

	mac := hmac.New(sha256.New, key)
	mac.Write(eapData)
	mac.Write([]byte{identifier})
	sum := mac.Sum(nil)
	return sum[:16]
}

func deriveAKAMSK(ck, ik [16]byte, prime bool) []byte {
	var key []byte
	key = append(key, ck[:]...)
	key = append(key, ik[:]...)

	label := "EAP-AKA MSK"
	if prime {
		label = "EAP-AKA' MSK"
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(label))
	msk := mac.Sum(nil)

	result := make([]byte, 64)
	copy(result, msk)

	mac.Reset()
	mac.Write([]byte(label + " ext"))
	ext := mac.Sum(nil)
	copy(result[32:], ext)

	return result[:64]
}
