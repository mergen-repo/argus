package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const testHexKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func contextBackground() context.Context {
	return context.Background()
}

func newTestServiceWithEncryption(hexKey string) (*Service, *mockUserRepo, *mockSessionRepo, *mockAuditLogger) {
	users := newMockUserRepo()
	sessions := newMockSessionRepo()
	audit := &mockAuditLogger{}
	svc := NewService(users, sessions, audit, Config{
		JWTSecret:        testSecret,
		JWTExpiry:        15 * time.Minute,
		JWTRefreshExpiry: 168 * time.Hour,
		JWTIssuer:        "argus",
		BcryptCost:       bcrypt.MinCost,
		MaxLoginAttempts: 5,
		LockoutDuration:  15 * time.Minute,
		EncryptionKey:    hexKey,
	})
	return svc, users, sessions, audit
}

func generateCurrentTOTPCode(secret string) (string, error) {
	return totp.GenerateCode(secret, time.Now())
}

func TestTOTPEncryptRoundTrip(t *testing.T) {
	plain := "JBSWY3DPEHPK3PXP"

	encrypted, err := EncryptTOTPSecret(plain, testHexKey)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret failed: %v", err)
	}
	if encrypted == plain {
		t.Fatal("expected ciphertext to differ from plaintext")
	}
	if encrypted == "" {
		t.Fatal("expected non-empty ciphertext")
	}

	decrypted, err := DecryptTOTPSecret(encrypted, testHexKey)
	if err != nil {
		t.Fatalf("DecryptTOTPSecret failed: %v", err)
	}
	if decrypted != plain {
		t.Errorf("expected %q, got %q", plain, decrypted)
	}
}

func TestTOTPEncryptEmptyKeyPassthrough(t *testing.T) {
	plain := "JBSWY3DPEHPK3PXP"

	encrypted, err := EncryptTOTPSecret(plain, "")
	if err != nil {
		t.Fatalf("EncryptTOTPSecret with empty key failed: %v", err)
	}
	if encrypted != plain {
		t.Errorf("expected passthrough %q, got %q", plain, encrypted)
	}

	decrypted, err := DecryptTOTPSecret(plain, "")
	if err != nil {
		t.Fatalf("DecryptTOTPSecret with empty key failed: %v", err)
	}
	if decrypted != plain {
		t.Errorf("expected passthrough %q, got %q", plain, decrypted)
	}
}

func TestTOTPEncryptDistinctNonces(t *testing.T) {
	plain := "JBSWY3DPEHPK3PXP"

	first, err := EncryptTOTPSecret(plain, testHexKey)
	if err != nil {
		t.Fatalf("first encrypt failed: %v", err)
	}
	second, err := EncryptTOTPSecret(plain, testHexKey)
	if err != nil {
		t.Fatalf("second encrypt failed: %v", err)
	}
	if first == second {
		t.Error("expected distinct ciphertexts due to random nonces")
	}
}

func TestTOTPEncryptInvalidHexKey(t *testing.T) {
	_, err := EncryptTOTPSecret("secret", "not-hex!")
	if err == nil {
		t.Fatal("expected error for invalid hex key")
	}
	if !strings.Contains(err.Error(), "invalid encryption key") {
		t.Errorf("expected invalid key error, got: %v", err)
	}
}

func TestTOTPDecryptCorruptCiphertext(t *testing.T) {
	_, err := DecryptTOTPSecret("not-real-ciphertext", testHexKey)
	if err == nil {
		t.Fatal("expected error decrypting garbage")
	}
}

func TestTOTPGenerateAndEncryptValidates(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("user@example.com")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}

	encrypted, err := EncryptTOTPSecret(secret, testHexKey)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret failed: %v", err)
	}

	decrypted, err := DecryptTOTPSecret(encrypted, testHexKey)
	if err != nil {
		t.Fatalf("DecryptTOTPSecret failed: %v", err)
	}
	if decrypted != secret {
		t.Errorf("round-trip mismatch: want %q, got %q", secret, decrypted)
	}
}

func TestSetup2FA_StoresEncryptedSecret(t *testing.T) {
	svc, users, _, _ := newTestServiceWithEncryption(testHexKey)
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	result, err := svc.Setup2FA(contextBackground(), user.ID)
	if err != nil {
		t.Fatalf("Setup2FA failed: %v", err)
	}

	if result.Secret == "" {
		t.Fatal("expected plaintext secret in result")
	}

	stored := users.users[user.ID.String()].TOTPSecret
	if stored == nil {
		t.Fatal("expected stored totp secret")
	}
	if *stored == result.Secret {
		t.Error("expected stored secret to be encrypted, not plaintext")
	}

	decrypted, err := DecryptTOTPSecret(*stored, testHexKey)
	if err != nil {
		t.Fatalf("decrypt stored secret failed: %v", err)
	}
	if decrypted != result.Secret {
		t.Errorf("decrypted stored secret %q does not match returned plaintext %q", decrypted, result.Secret)
	}
}

func TestVerify2FA_DecryptsBeforeValidating(t *testing.T) {
	svc, users, _, _ := newTestServiceWithEncryption(testHexKey)
	user := createTestUser("test@example.com", "password123", false)
	users.addUser(user)

	setup, err := svc.Setup2FA(contextBackground(), user.ID)
	if err != nil {
		t.Fatalf("Setup2FA failed: %v", err)
	}

	code, err := generateCurrentTOTPCode(setup.Secret)
	if err != nil {
		t.Fatalf("generate current code: %v", err)
	}

	verifyResult, err := svc.Verify2FA(contextBackground(), user.ID, code, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("Verify2FA failed: %v", err)
	}
	if verifyResult.Token == "" {
		t.Error("expected non-empty token after 2fa verify")
	}
}
