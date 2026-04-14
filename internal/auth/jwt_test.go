package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecretB = "test-secret-b-key-must-be-at-least-32-chars!"

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

func TestValidateTokenMulti_CurrentKeySucceeds(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, err := GenerateToken(testSecret, userID, tenantID, "analyst", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateTokenMulti(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateTokenMulti failed: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", claims.UserID, userID)
	}
}

func TestValidateTokenMulti_CurrentKeyInSlice(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, err := GenerateToken(testSecret, userID, tenantID, "analyst", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateTokenMulti(token, testSecret, testSecretB)
	if err != nil {
		t.Fatalf("ValidateTokenMulti failed: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", claims.UserID, userID)
	}
}

func TestValidateTokenMulti_PreviousKeySucceeds(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, err := GenerateToken(testSecretB, userID, tenantID, "analyst", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateTokenMulti(token, testSecret, testSecretB)
	if err != nil {
		t.Fatalf("ValidateTokenMulti with previous key failed: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", claims.UserID, userID)
	}
}

func TestValidateTokenMulti_PreviousKeyFailsWithoutIt(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, err := GenerateToken(testSecretB, userID, tenantID, "analyst", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = ValidateTokenMulti(token, testSecret)
	if err == nil {
		t.Fatal("Expected error when only current key provided but token signed with previous")
	}
	if err != ErrTokenInvalid {
		t.Errorf("Expected ErrTokenInvalid, got: %v", err)
	}
}

func TestValidateTokenMulti_ExpiredToken(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, err := GenerateToken(testSecret, userID, tenantID, "analyst", -1*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = ValidateTokenMulti(token, testSecret, testSecretB)
	if err == nil {
		t.Fatal("Expected error for expired token")
	}
	if err != ErrTokenExpired {
		t.Errorf("Expected ErrTokenExpired, got: %v", err)
	}
}

func TestValidateTokenMulti_MetricHookCalledCurrentSlot(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, _ := GenerateToken(testSecret, userID, tenantID, "analyst", 15*time.Minute, false)

	var hookSlot string
	var hookCalls int
	orig := JWTVerifyHook
	JWTVerifyHook = func(slot string) {
		hookSlot = slot
		hookCalls++
	}
	defer func() { JWTVerifyHook = orig }()

	ValidateTokenMulti(token, testSecret, testSecretB)

	if hookCalls != 1 {
		t.Errorf("Expected hook called once, got %d", hookCalls)
	}
	if hookSlot != "current" {
		t.Errorf("Expected slot 'current', got %q", hookSlot)
	}
}

func TestValidateTokenMulti_MetricHookCalledPreviousSlot(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, _ := GenerateToken(testSecretB, userID, tenantID, "analyst", 15*time.Minute, false)

	var hookSlot string
	orig := JWTVerifyHook
	JWTVerifyHook = func(slot string) { hookSlot = slot }
	defer func() { JWTVerifyHook = orig }()

	ValidateTokenMulti(token, testSecret, testSecretB)

	if hookSlot != "previous" {
		t.Errorf("Expected slot 'previous', got %q", hookSlot)
	}
}

func TestValidateTokenMulti_MetricHookCalledFailedSlot(t *testing.T) {
	var hookSlot string
	orig := JWTVerifyHook
	JWTVerifyHook = func(slot string) { hookSlot = slot }
	defer func() { JWTVerifyHook = orig }()

	ValidateTokenMulti("not-a-token", testSecret, testSecretB)

	if hookSlot != "failed" {
		t.Errorf("Expected slot 'failed', got %q", hookSlot)
	}
}

func TestGenerateSwitchedToken_WithActiveTenant(t *testing.T) {
	userID := uuid.New()
	homeTenantID := uuid.New()
	activeTenantID := uuid.New()

	token, err := GenerateSwitchedToken(testSecret, userID, homeTenantID, &activeTenantID, "super_admin", time.Hour)
	if err != nil {
		t.Fatalf("GenerateSwitchedToken failed: %v", err)
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", claims.UserID, userID)
	}
	if claims.TenantID != homeTenantID {
		t.Errorf("TenantID (home) should be preserved: got %v, want %v", claims.TenantID, homeTenantID)
	}
	if claims.ActiveTenantID == nil {
		t.Fatal("ActiveTenantID should be set")
	}
	if *claims.ActiveTenantID != activeTenantID {
		t.Errorf("ActiveTenantID mismatch: got %v, want %v", *claims.ActiveTenantID, activeTenantID)
	}
	if claims.Role != "super_admin" {
		t.Errorf("Role mismatch: got %v", claims.Role)
	}
	if claims.Impersonated {
		t.Error("Impersonated should not be set for tenant switch")
	}
}

func TestGenerateSwitchedToken_NilClearsActiveTenant(t *testing.T) {
	userID := uuid.New()
	homeTenantID := uuid.New()

	token, err := GenerateSwitchedToken(testSecret, userID, homeTenantID, nil, "super_admin", time.Hour)
	if err != nil {
		t.Fatalf("GenerateSwitchedToken failed: %v", err)
	}

	claims, err := ValidateToken(token, testSecret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.ActiveTenantID != nil {
		t.Errorf("ActiveTenantID should be nil after exit, got %v", *claims.ActiveTenantID)
	}
	if claims.TenantID != homeTenantID {
		t.Errorf("TenantID (home) should be preserved on exit: got %v, want %v", claims.TenantID, homeTenantID)
	}
}

func TestValidateTokenMulti_EmptySecretsSkipped(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	token, _ := GenerateToken(testSecret, userID, tenantID, "analyst", 15*time.Minute, false)

	claims, err := ValidateTokenMulti(token, testSecret, "")
	if err != nil {
		t.Fatalf("Expected success with empty previous secret skipped, got: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID mismatch: got %v, want %v", claims.UserID, userID)
	}
}
