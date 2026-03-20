package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "test-secret-key-must-be-at-least-32-chars-long"

func TestGenerateToken(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	role := "tenant_admin"

	token, err := GenerateToken(testSecret, userID, tenantID, role, 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned empty token")
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", claims.UserID, userID)
	}
	if claims.TenantID != tenantID {
		t.Errorf("TenantID mismatch: got %v, want %v", claims.TenantID, tenantID)
	}
	if claims.Role != role {
		t.Errorf("Role mismatch: got %v, want %v", claims.Role, role)
	}
	if claims.Partial {
		t.Error("Expected Partial=false")
	}
}

func TestGeneratePartialToken(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()

	token, err := GenerateToken(testSecret, userID, tenantID, "sim_manager", 5*time.Minute, true)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if !claims.Partial {
		t.Error("Expected Partial=true for 2FA pending token")
	}
}

func TestValidateToken_ExpiredToken(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()

	token, err := GenerateToken(testSecret, userID, tenantID, "analyst", -1*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = ValidateToken(token, testSecret)
	if err == nil {
		t.Fatal("Expected error for expired token")
	}
	if err != ErrTokenExpired {
		t.Errorf("Expected ErrTokenExpired, got: %v", err)
	}
}

func TestValidateToken_BadSignature(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()

	token, err := GenerateToken(testSecret, userID, tenantID, "analyst", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = ValidateToken(token, "wrong-secret-key-that-is-also-32-chars")
	if err == nil {
		t.Fatal("Expected error for wrong secret")
	}
	if err != ErrTokenInvalid {
		t.Errorf("Expected ErrTokenInvalid, got: %v", err)
	}
}

func TestValidateToken_InvalidString(t *testing.T) {
	_, err := ValidateToken("not-a-jwt-token", testSecret)
	if err == nil {
		t.Fatal("Expected error for invalid token string")
	}
}
