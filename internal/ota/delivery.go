package ota

import (
	"encoding/binary"
	"fmt"
)

const (
	smsppUDHI        byte = 0x70
	smsppSecHeader   byte = 0x02
	smsppResponseTag byte = 0x23

	spiEncrypt    byte = 0x01
	spiMAC        byte = 0x02
	spiEncryptMAC byte = 0x03

	bipChannelID    byte = 0x01
	bipTransportTCP byte = 0x02
)

type SMSPPEnvelope struct {
	SPI         [2]byte
	KIC         byte
	KID         byte
	TAR         [3]byte
	CNTR        [5]byte
	SecuredData []byte
	MAC         []byte
}

func EncodeSMSPP(packet *SecuredPacket, tar [3]byte, counter uint64, mode SecurityMode) ([]byte, error) {
	env := SMSPPEnvelope{
		TAR:         tar,
		SecuredData: packet.Data,
		MAC:         packet.MAC,
	}

	switch mode {
	case SecurityNone:
		env.SPI = [2]byte{0x00, 0x00}
	case SecurityKIC:
		env.SPI = [2]byte{spiEncrypt, 0x00}
		env.KIC = 0x01
	case SecurityKID:
		env.SPI = [2]byte{spiMAC, 0x00}
		env.KID = 0x01
	case SecurityKICKID:
		env.SPI = [2]byte{spiEncryptMAC, 0x00}
		env.KIC = 0x01
		env.KID = 0x01
	}

	binary.BigEndian.PutUint32(env.CNTR[1:], uint32(counter))

	headerLen := 2 + 1 + 1 + 3 + 5
	macLen := len(env.MAC)
	totalLen := 1 + 1 + headerLen + macLen + len(env.SecuredData)

	if totalLen > 140 {
		return nil, fmt.Errorf("SMS-PP envelope exceeds 140 bytes: %d", totalLen)
	}

	buf := make([]byte, 0, totalLen+2)

	buf = append(buf, smsppUDHI)
	buf = append(buf, byte(totalLen))

	buf = append(buf, smsppSecHeader)
	buf = append(buf, byte(headerLen+macLen))
	buf = append(buf, env.SPI[0], env.SPI[1])
	buf = append(buf, env.KIC)
	buf = append(buf, env.KID)
	buf = append(buf, env.TAR[0], env.TAR[1], env.TAR[2])
	buf = append(buf, env.CNTR[:]...)

	if len(env.MAC) > 0 {
		buf = append(buf, env.MAC...)
	}

	buf = append(buf, env.SecuredData...)

	return buf, nil
}

type BIPPacket struct {
	ChannelID  byte
	Transport  byte
	Port       uint16
	DataLength uint16
	Data       []byte
}

func EncodeBIP(packet *SecuredPacket, port uint16) ([]byte, error) {
	data := packet.Data
	if len(packet.MAC) > 0 {
		data = append(data, packet.MAC...)
	}

	bip := BIPPacket{
		ChannelID:  bipChannelID,
		Transport:  bipTransportTCP,
		Port:       port,
		DataLength: uint16(len(data)),
		Data:       data,
	}

	buf := make([]byte, 0, 6+len(bip.Data))

	buf = append(buf, bip.ChannelID)
	buf = append(buf, bip.Transport)

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, bip.Port)
	buf = append(buf, portBytes...)

	lenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(lenBytes, bip.DataLength)
	buf = append(buf, lenBytes...)

	buf = append(buf, bip.Data...)

	return buf, nil
}
