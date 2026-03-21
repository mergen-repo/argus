package ota

import (
	"testing"
)

func TestEncodeSMSPP_SecurityNone(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x00, 0xA4, 0x00, 0x04}}
	tar := [3]byte{0xB0, 0x00, 0x10}

	buf, err := EncodeSMSPP(pkt, tar, 1, SecurityNone)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	if len(buf) == 0 {
		t.Fatal("expected non-empty buffer")
	}

	if buf[0] != smsppUDHI {
		t.Errorf("first byte = %02x, want %02x (UDHI)", buf[0], smsppUDHI)
	}

	if buf[2] != smsppSecHeader {
		t.Errorf("security header tag = %02x, want %02x", buf[2], smsppSecHeader)
	}

	if buf[4] != 0x00 || buf[5] != 0x00 {
		t.Errorf("SPI = %02x%02x, want 0000 for SecurityNone", buf[4], buf[5])
	}
}

func TestEncodeSMSPP_SecurityKIC(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01, 0x02}}
	tar := [3]byte{0xB0, 0x00, 0x10}

	buf, err := EncodeSMSPP(pkt, tar, 42, SecurityKIC)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	if buf[4] != spiEncrypt {
		t.Errorf("SPI[0] = %02x, want %02x (encrypt)", buf[4], spiEncrypt)
	}
	if buf[6] != 0x01 {
		t.Errorf("KIC = %02x, want 01", buf[6])
	}
}

func TestEncodeSMSPP_SecurityKID(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01, 0x02}}
	tar := [3]byte{0xB0, 0x00, 0x10}

	buf, err := EncodeSMSPP(pkt, tar, 1, SecurityKID)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	if buf[4] != spiMAC {
		t.Errorf("SPI[0] = %02x, want %02x (MAC)", buf[4], spiMAC)
	}
	if buf[7] != 0x01 {
		t.Errorf("KID = %02x, want 01", buf[7])
	}
}

func TestEncodeSMSPP_SecurityKICKID(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01, 0x02}}
	tar := [3]byte{0xB0, 0x00, 0x10}

	buf, err := EncodeSMSPP(pkt, tar, 1, SecurityKICKID)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	if buf[4] != spiEncryptMAC {
		t.Errorf("SPI[0] = %02x, want %02x (encrypt+MAC)", buf[4], spiEncryptMAC)
	}
	if buf[6] != 0x01 {
		t.Errorf("KIC = %02x, want 01", buf[6])
	}
	if buf[7] != 0x01 {
		t.Errorf("KID = %02x, want 01", buf[7])
	}
}

func TestEncodeSMSPP_TAR(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01}}
	tar := [3]byte{0xAA, 0xBB, 0xCC}

	buf, err := EncodeSMSPP(pkt, tar, 0, SecurityNone)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	if buf[8] != 0xAA || buf[9] != 0xBB || buf[10] != 0xCC {
		t.Errorf("TAR = %02x%02x%02x, want AABBCC", buf[8], buf[9], buf[10])
	}
}

func TestEncodeSMSPP_Counter(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01}}
	tar := [3]byte{0x00, 0x00, 0x00}

	buf, err := EncodeSMSPP(pkt, tar, 256, SecurityNone)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	cntr := buf[11:16]
	if cntr[3] != 0x01 || cntr[4] != 0x00 {
		t.Errorf("CNTR last 2 bytes = %02x%02x, want 0100 for counter=256", cntr[3], cntr[4])
	}
}

func TestEncodeSMSPP_TooLarge(t *testing.T) {
	bigData := make([]byte, 200)
	pkt := &SecuredPacket{Data: bigData}
	tar := [3]byte{0x00, 0x00, 0x00}

	_, err := EncodeSMSPP(pkt, tar, 0, SecurityNone)
	if err == nil {
		t.Error("expected error for oversized SMS-PP")
	}
}

func TestEncodeSMSPP_WithMAC(t *testing.T) {
	pkt := &SecuredPacket{
		Data: []byte{0x01, 0x02},
		MAC:  []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22},
	}
	tar := [3]byte{0x00, 0x00, 0x00}

	buf, err := EncodeSMSPP(pkt, tar, 0, SecurityKID)
	if err != nil {
		t.Fatalf("EncodeSMSPP: %v", err)
	}

	if len(buf) == 0 {
		t.Error("expected non-empty buffer with MAC")
	}
}

func TestEncodeBIP(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01, 0x02, 0x03}}

	buf, err := EncodeBIP(pkt, 8080)
	if err != nil {
		t.Fatalf("EncodeBIP: %v", err)
	}

	if len(buf) < 6 {
		t.Fatalf("buf too short: %d", len(buf))
	}

	if buf[0] != bipChannelID {
		t.Errorf("channel ID = %02x, want %02x", buf[0], bipChannelID)
	}
	if buf[1] != bipTransportTCP {
		t.Errorf("transport = %02x, want %02x (TCP)", buf[1], bipTransportTCP)
	}

	port := uint16(buf[2])<<8 | uint16(buf[3])
	if port != 8080 {
		t.Errorf("port = %d, want 8080", port)
	}

	dataLen := uint16(buf[4])<<8 | uint16(buf[5])
	if dataLen != 3 {
		t.Errorf("data length = %d, want 3", dataLen)
	}

	if buf[6] != 0x01 || buf[7] != 0x02 || buf[8] != 0x03 {
		t.Errorf("data = %x, want 010203", buf[6:9])
	}
}

func TestEncodeBIP_WithMAC(t *testing.T) {
	pkt := &SecuredPacket{
		Data: []byte{0x01},
		MAC:  []byte{0xAA, 0xBB},
	}

	buf, err := EncodeBIP(pkt, 443)
	if err != nil {
		t.Fatalf("EncodeBIP: %v", err)
	}

	dataLen := uint16(buf[4])<<8 | uint16(buf[5])
	if dataLen != 3 {
		t.Errorf("data length = %d, want 3 (1 data + 2 MAC)", dataLen)
	}
}

func TestEncodeBIP_ZeroPort(t *testing.T) {
	pkt := &SecuredPacket{Data: []byte{0x01}}

	buf, err := EncodeBIP(pkt, 0)
	if err != nil {
		t.Fatalf("EncodeBIP: %v", err)
	}

	port := uint16(buf[2])<<8 | uint16(buf[3])
	if port != 0 {
		t.Errorf("port = %d, want 0", port)
	}
}
