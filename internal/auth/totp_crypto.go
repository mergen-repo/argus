package auth

import (
	"encoding/hex"
	"fmt"

	"github.com/btopcu/argus/internal/crypto"
)

func EncryptTOTPSecret(plainSecret, hexKey string) (string, error) {
	if hexKey == "" {
		return plainSecret, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("auth: invalid encryption key: %w", err)
	}
	encrypted, err := crypto.Encrypt([]byte(plainSecret), key)
	if err != nil {
		return "", fmt.Errorf("auth: encrypt totp secret: %w", err)
	}
	return string(encrypted), nil
}

func DecryptTOTPSecret(encryptedSecret, hexKey string) (string, error) {
	if hexKey == "" {
		return encryptedSecret, nil
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("auth: invalid encryption key: %w", err)
	}
	plaintext, err := crypto.Decrypt([]byte(encryptedSecret), key)
	if err != nil {
		return "", fmt.Errorf("auth: decrypt totp secret: %w", err)
	}
	return string(plaintext), nil
}
