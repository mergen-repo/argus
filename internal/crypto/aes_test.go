package crypto

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("hello world secret data")
	encrypted, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	if bytes.Equal(plaintext, encrypted) {
		t.Error("Encrypted data should differ from plaintext")
	}

	decrypted, err := Decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("Decrypted = %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestEncryptProducesDifferentOutputs(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("test data")
	enc1, _ := Encrypt(plaintext, key)
	enc2, _ := Encrypt(plaintext, key)

	if bytes.Equal(enc1, enc2) {
		t.Error("Two encryptions of same data should produce different ciphertexts (random nonce)")
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := []byte("secret data")
	encrypted, _ := Encrypt(plaintext, key)

	if len(encrypted) > 5 {
		encrypted[5] ^= 0xff
	}

	_, err := Decrypt(encrypted, key)
	if err == nil {
		t.Error("Decrypt should fail on tampered ciphertext")
	}
}

func TestEncryptJSONEmptyKeyPassthrough(t *testing.T) {
	data := json.RawMessage(`{"secret": "value"}`)
	result, err := EncryptJSON(data, "")
	if err != nil {
		t.Fatalf("EncryptJSON error: %v", err)
	}

	if !bytes.Equal(data, result) {
		t.Error("Empty key should pass through data unchanged")
	}
}

func TestDecryptJSONEmptyKeyPassthrough(t *testing.T) {
	data := json.RawMessage(`{"secret": "value"}`)
	result, err := DecryptJSON(data, "")
	if err != nil {
		t.Fatalf("DecryptJSON error: %v", err)
	}

	if !bytes.Equal(data, result) {
		t.Error("Empty key should pass through data unchanged")
	}
}

func TestEncryptDecryptJSONRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	hexKey := hex.EncodeToString(key)

	data := json.RawMessage(`{"host":"10.0.0.1","port":1812,"secret":"radius_secret"}`)

	encrypted, err := EncryptJSON(data, hexKey)
	if err != nil {
		t.Fatalf("EncryptJSON error: %v", err)
	}

	if bytes.Equal(data, encrypted) {
		t.Error("Encrypted JSON should differ from original")
	}

	decrypted, err := DecryptJSON(encrypted, hexKey)
	if err != nil {
		t.Fatalf("DecryptJSON error: %v", err)
	}

	if !bytes.Equal(data, decrypted) {
		t.Errorf("DecryptJSON = %s, want %s", string(decrypted), string(data))
	}
}

func TestInvalidKeyLength(t *testing.T) {
	key := []byte("tooshort")
	_, err := Encrypt([]byte("data"), key)
	if err == nil {
		t.Error("Encrypt should fail with invalid key length")
	}
}
