package ota

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

type OTAKeys struct {
	KIC []byte
	KID []byte
}

type SecuredPacket struct {
	Data []byte
	MAC  []byte
}

func SecureAPDU(apduData []byte, keys *OTAKeys, mode SecurityMode) (*SecuredPacket, error) {
	if mode == SecurityNone || keys == nil {
		return &SecuredPacket{Data: apduData}, nil
	}

	var encrypted []byte
	var err error

	switch mode {
	case SecurityKIC:
		encrypted, err = encryptAES(apduData, keys.KIC)
		if err != nil {
			return nil, fmt.Errorf("encrypt with KIC: %w", err)
		}
		return &SecuredPacket{Data: encrypted}, nil

	case SecurityKID:
		mac := computeMAC(apduData, keys.KID)
		return &SecuredPacket{Data: apduData, MAC: mac}, nil

	case SecurityKICKID:
		encrypted, err = encryptAES(apduData, keys.KIC)
		if err != nil {
			return nil, fmt.Errorf("encrypt with KIC: %w", err)
		}
		mac := computeMAC(encrypted, keys.KID)
		return &SecuredPacket{Data: encrypted, MAC: mac}, nil

	default:
		return nil, fmt.Errorf("unsupported security mode: %s", mode)
	}
}

func VerifyMAC(data, expectedMAC []byte, kid []byte) bool {
	computed := computeMAC(data, kid)
	return hmac.Equal(computed, expectedMAC)
}

func encryptAES(data, key []byte) ([]byte, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	padded := pkcs7Pad(data, aes.BlockSize)

	ciphertext := make([]byte, aes.BlockSize+len(padded))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext[aes.BlockSize:], padded)

	return ciphertext, nil
}

func computeMAC(data, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)[:8]
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}
