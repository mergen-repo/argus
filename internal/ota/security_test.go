package ota

import (
	"crypto/rand"
	"testing"
)

func TestSecureAPDU_SecurityNone(t *testing.T) {
	data := []byte{0x00, 0xA4, 0x00, 0x04, 0x02, 0x3F, 0x00}
	pkt, err := SecureAPDU(data, nil, SecurityNone)
	if err != nil {
		t.Fatalf("SecureAPDU: %v", err)
	}
	if len(pkt.MAC) != 0 {
		t.Errorf("MAC len = %d, want 0", len(pkt.MAC))
	}
	if len(pkt.Data) != len(data) {
		t.Errorf("data len = %d, want %d", len(pkt.Data), len(data))
	}
	for i, b := range pkt.Data {
		if b != data[i] {
			t.Errorf("data[%d] = %02x, want %02x", i, b, data[i])
		}
	}
}

func TestSecureAPDU_SecurityNone_NilKeys(t *testing.T) {
	data := []byte{0x01, 0x02}
	pkt, err := SecureAPDU(data, nil, SecurityNone)
	if err != nil {
		t.Fatalf("SecureAPDU: %v", err)
	}
	if len(pkt.Data) != 2 {
		t.Errorf("data len = %d, want 2", len(pkt.Data))
	}
}

func TestSecureAPDU_SecurityKIC(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	data := []byte{0x00, 0xA4, 0x00, 0x04, 0x02, 0x3F, 0x00}
	pkt, err := SecureAPDU(data, &OTAKeys{KIC: key}, SecurityKIC)
	if err != nil {
		t.Fatalf("SecureAPDU: %v", err)
	}

	if len(pkt.Data) == len(data) {
		equal := true
		for i := range data {
			if pkt.Data[i] != data[i] {
				equal = false
				break
			}
		}
		if equal {
			t.Error("encrypted data should differ from original")
		}
	}

	if len(pkt.MAC) != 0 {
		t.Errorf("MAC len = %d, want 0 for KIC-only", len(pkt.MAC))
	}
}

func TestSecureAPDU_SecurityKID(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	data := []byte{0x00, 0xA4, 0x00, 0x04, 0x02, 0x3F, 0x00}
	pkt, err := SecureAPDU(data, &OTAKeys{KID: key}, SecurityKID)
	if err != nil {
		t.Fatalf("SecureAPDU: %v", err)
	}

	for i, b := range pkt.Data {
		if b != data[i] {
			t.Errorf("data should be unencrypted: data[%d] = %02x, want %02x", i, b, data[i])
		}
	}

	if len(pkt.MAC) != 8 {
		t.Errorf("MAC len = %d, want 8", len(pkt.MAC))
	}
}

func TestSecureAPDU_SecurityKICKID(t *testing.T) {
	kic := make([]byte, 16)
	kid := make([]byte, 16)
	if _, err := rand.Read(kic); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(kid); err != nil {
		t.Fatal(err)
	}

	data := []byte{0x00, 0xA4, 0x00, 0x04, 0x02, 0x3F, 0x00}
	pkt, err := SecureAPDU(data, &OTAKeys{KIC: kic, KID: kid}, SecurityKICKID)
	if err != nil {
		t.Fatalf("SecureAPDU: %v", err)
	}

	if len(pkt.Data) == len(data) {
		equal := true
		for i := range data {
			if pkt.Data[i] != data[i] {
				equal = false
				break
			}
		}
		if equal {
			t.Error("encrypted data should differ from original")
		}
	}

	if len(pkt.MAC) != 8 {
		t.Errorf("MAC len = %d, want 8", len(pkt.MAC))
	}
}

func TestVerifyMAC_Valid(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	data := []byte{0x01, 0x02, 0x03}
	mac := computeMAC(data, key)

	if !VerifyMAC(data, mac, key) {
		t.Error("valid MAC should verify")
	}
}

func TestVerifyMAC_Invalid(t *testing.T) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}

	data := []byte{0x01, 0x02, 0x03}
	badMAC := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	if VerifyMAC(data, badMAC, key) {
		t.Error("invalid MAC should not verify")
	}
}

func TestVerifyMAC_DifferentKey(t *testing.T) {
	key1 := make([]byte, 16)
	key2 := make([]byte, 16)
	if _, err := rand.Read(key1); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key2); err != nil {
		t.Fatal(err)
	}

	data := []byte{0x01, 0x02, 0x03}
	mac := computeMAC(data, key1)

	if VerifyMAC(data, mac, key2) {
		t.Error("MAC from different key should not verify")
	}
}

func TestEncryptAES_ValidKeys(t *testing.T) {
	keySizes := []int{16, 24, 32}
	data := []byte("test data for encryption")

	for _, size := range keySizes {
		key := make([]byte, size)
		if _, err := rand.Read(key); err != nil {
			t.Fatal(err)
		}

		encrypted, err := encryptAES(data, key)
		if err != nil {
			t.Errorf("encryptAES with %d-byte key: %v", size, err)
			continue
		}

		if len(encrypted) <= len(data) {
			t.Errorf("encrypted len (%d) should be > data len (%d)", len(encrypted), len(data))
		}
	}
}

func TestEncryptAES_InvalidKeyLength(t *testing.T) {
	invalidSizes := []int{0, 8, 15, 17, 33}
	data := []byte("test data")

	for _, size := range invalidSizes {
		key := make([]byte, size)
		_, err := encryptAES(data, key)
		if err == nil {
			t.Errorf("expected error for %d-byte key", size)
		}
	}
}

func TestPkcs7Pad(t *testing.T) {
	tests := []struct {
		name      string
		dataLen   int
		blockSize int
		wantLen   int
	}{
		{"exact block", 16, 16, 32},
		{"one byte short", 15, 16, 16},
		{"half block", 8, 16, 16},
		{"one byte", 1, 16, 16},
		{"empty", 0, 16, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := make([]byte, tt.dataLen)
			padded := pkcs7Pad(data, tt.blockSize)
			if len(padded) != tt.wantLen {
				t.Errorf("padded len = %d, want %d", len(padded), tt.wantLen)
			}
			if len(padded)%tt.blockSize != 0 {
				t.Errorf("padded len %d not multiple of block size %d", len(padded), tt.blockSize)
			}
			padVal := padded[len(padded)-1]
			if int(padVal) < 1 || int(padVal) > tt.blockSize {
				t.Errorf("pad value = %d, should be 1-%d", padVal, tt.blockSize)
			}
		})
	}
}

func TestComputeMAC_Deterministic(t *testing.T) {
	key := []byte("1234567890123456")
	data := []byte("hello world")

	mac1 := computeMAC(data, key)
	mac2 := computeMAC(data, key)

	if len(mac1) != 8 {
		t.Errorf("MAC len = %d, want 8", len(mac1))
	}

	for i := range mac1 {
		if mac1[i] != mac2[i] {
			t.Error("MAC should be deterministic")
			break
		}
	}
}
