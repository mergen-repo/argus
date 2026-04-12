package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
