package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/auth"
	"github.com/google/uuid"
)

const (
	testCurrentSecret  = "test-current-secret-key-must-be-at-least-32chars"
	testPreviousSecret = "test-previous-secret-key-must-be-at-least-32ch!"
)

func buildBearer(t *testing.T, secret string) string {
	t.Helper()
	userID := uuid.New()
	tenantID := uuid.New()
	tok, err := auth.GenerateToken(secret, userID, tenantID, "analyst", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	return "Bearer " + tok
}

func TestJWTAuth_CurrentSecretAccepted(t *testing.T) {
	handler := JWTAuth(testCurrentSecret, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", buildBearer(t, testCurrentSecret))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestJWTAuth_PreviousSecretAccepted(t *testing.T) {
	handler := JWTAuth(testCurrentSecret, testPreviousSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", buildBearer(t, testPreviousSecret))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with previous secret, got %d", rec.Code)
	}
}

func TestJWTAuth_InvalidTokenRejected(t *testing.T) {
	handler := JWTAuth(testCurrentSecret, testPreviousSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_MissingHeaderRejected(t *testing.T) {
	handler := JWTAuth(testCurrentSecret, testPreviousSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_ExpiredTokenRejected(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	tok, err := auth.GenerateToken(testCurrentSecret, userID, tenantID, "analyst", -1*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	handler := JWTAuth(testCurrentSecret, testPreviousSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_SuperAdminWithActiveTenant_EffectiveTenantIsActive(t *testing.T) {
	userID := uuid.New()
	homeTenant := uuid.New()
	activeTenant := uuid.New()
	tok, err := auth.GenerateSwitchedToken(testCurrentSecret, userID, homeTenant, &activeTenant, "super_admin", 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateSwitchedToken: %v", err)
	}

	var observedTenant, observedHome uuid.UUID
	var observedActive uuid.UUID
	var observedActiveSet bool
	handler := JWTAuth(testCurrentSecret, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedTenant, _ = r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		observedHome, _ = r.Context().Value(apierr.HomeTenantIDKey).(uuid.UUID)
		observedActive, observedActiveSet = r.Context().Value(apierr.ActiveTenantIDKey).(uuid.UUID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if observedTenant != activeTenant {
		t.Errorf("effective tenant = %v, want active %v", observedTenant, activeTenant)
	}
	if observedHome != homeTenant {
		t.Errorf("home tenant = %v, want %v", observedHome, homeTenant)
	}
	if !observedActiveSet || observedActive != activeTenant {
		t.Errorf("active tenant key missing or wrong: set=%v val=%v", observedActiveSet, observedActive)
	}
}

func TestJWTAuth_NonSuperAdminWithActiveTenant_IgnoresOverride(t *testing.T) {
	// A maliciously-crafted or stale token with ActiveTenantID but role
	// other than super_admin MUST NOT be honored — the effective tenant
	// stays at the user's home tenant.
	userID := uuid.New()
	homeTenant := uuid.New()
	activeTenant := uuid.New()
	tok, err := auth.GenerateSwitchedToken(testCurrentSecret, userID, homeTenant, &activeTenant, "tenant_admin", 15*time.Minute)
	if err != nil {
		t.Fatalf("GenerateSwitchedToken: %v", err)
	}

	var observedTenant uuid.UUID
	var observedActiveSet bool
	handler := JWTAuth(testCurrentSecret, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedTenant, _ = r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		_, observedActiveSet = r.Context().Value(apierr.ActiveTenantIDKey).(uuid.UUID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if observedTenant != homeTenant {
		t.Errorf("non-super_admin effective tenant = %v, want home %v (override ignored)", observedTenant, homeTenant)
	}
	if observedActiveSet {
		t.Error("ActiveTenantIDKey must not be set for non-super_admin")
	}
}

func TestJWTAuth_StandardTokenNoActiveTenant_UsesHomeTenant(t *testing.T) {
	userID := uuid.New()
	homeTenant := uuid.New()
	tok, err := auth.GenerateToken(testCurrentSecret, userID, homeTenant, "tenant_admin", 15*time.Minute, false)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	var observedTenant, observedHome uuid.UUID
	var observedActiveSet bool
	handler := JWTAuth(testCurrentSecret, "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedTenant, _ = r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		observedHome, _ = r.Context().Value(apierr.HomeTenantIDKey).(uuid.UUID)
		_, observedActiveSet = r.Context().Value(apierr.ActiveTenantIDKey).(uuid.UUID)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if observedTenant != homeTenant || observedHome != homeTenant {
		t.Errorf("expected both effective and home = %v, got eff=%v home=%v", homeTenant, observedTenant, observedHome)
	}
	if observedActiveSet {
		t.Error("ActiveTenantIDKey must not be set without switch")
	}
}

func TestJWTAuthAllowPartial_PreviousSecretAccepted(t *testing.T) {
	handler := JWTAuthAllowPartial(testCurrentSecret, testPreviousSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req.Header.Set("Authorization", buildBearer(t, testPreviousSecret))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with previous secret on AllowPartial, got %d", rec.Code)
	}
}
