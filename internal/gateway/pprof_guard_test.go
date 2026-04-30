package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestPprofGuard_NoToken_Passthrough(t *testing.T) {
	handler := PprofGuard("")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("empty token: got %d, want 200", rr.Code)
	}
}

func TestPprofGuard_CorrectToken_QueryParam(t *testing.T) {
	handler := PprofGuard("secret-token-value")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/?token=secret-token-value", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("correct query param token: got %d, want 200", rr.Code)
	}
}

func TestPprofGuard_CorrectToken_BearerHeader(t *testing.T) {
	handler := PprofGuard("secret-token-value")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer secret-token-value")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("correct bearer token: got %d, want 200", rr.Code)
	}
}

func TestPprofGuard_MissingToken_401(t *testing.T) {
	handler := PprofGuard("secret-token-value")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("missing token: got %d, want 401", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("response body not valid JSON: %v", err)
	}
	if body["status"] != "error" {
		t.Errorf("status = %q, want %q", body["status"], "error")
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatal("error field missing or wrong type")
	}
	if errObj["code"] != "PPROF_UNAUTHORIZED" {
		t.Errorf("code = %q, want PPROF_UNAUTHORIZED", errObj["code"])
	}
}

func TestPprofGuard_WrongToken_401(t *testing.T) {
	handler := PprofGuard("secret-token-value")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/?token=wrong-token", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", rr.Code)
	}
}

func TestPprofGuard_WrongBearerToken_401(t *testing.T) {
	handler := PprofGuard("secret-token-value")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("wrong bearer token: got %d, want 401", rr.Code)
	}
}

func TestPprofGuard_ErrorBody_ContentType(t *testing.T) {
	handler := PprofGuard("secret-token-value")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
