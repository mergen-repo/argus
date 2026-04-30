package sba

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

func newTestServer() *Server {
	return NewServer(ServerConfig{
		Port: 0,
	}, ServerDeps{
		SessionMgr:      nil,
		EventBus:        nil,
		EAPStateMachine: nil,
		Logger:          testLogger(),
	})
}

func testLogger() zerolog.Logger {
	return zerolog.Nop()
}

func TestAUSFAuthenticationInitiation(t *testing.T) {
	srv := newTestServer()

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ausfHandler.HandleAuthentication(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp AuthenticationResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.AuthType != AuthType5GAKA {
		t.Errorf("expected auth type %s, got %s", AuthType5GAKA, resp.AuthType)
	}

	if resp.AuthData5G == nil {
		t.Fatal("expected 5gAuthData to be present")
	}

	if resp.AuthData5G.RAND == "" || resp.AuthData5G.AUTN == "" || resp.AuthData5G.HxresStar == "" {
		t.Error("expected RAND, AUTN, and HxresStar to be non-empty")
	}

	link, ok := resp.Links["5g-aka"]
	if !ok {
		t.Fatal("expected 5g-aka link")
	}
	if !strings.Contains(link.Href, "/5g-aka-confirmation") {
		t.Errorf("unexpected link href: %s", link.Href)
	}
}

func TestAUSFAuthenticationConfirmationSuccess(t *testing.T) {
	srv := newTestServer()

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org"}`
	initReq := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	initReq.Header.Set("Content-Type", "application/json")
	initW := httptest.NewRecorder()

	srv.ausfHandler.HandleAuthentication(initW, initReq)

	if initW.Code != http.StatusCreated {
		t.Fatalf("init expected 201, got %d", initW.Code)
	}

	var initResp AuthenticationResponse
	json.NewDecoder(initW.Body).Decode(&initResp)

	_, autn, xresStar, _ := generate5GAV("imsi-286010123456789", "5G:mnc001.mcc286.3gppnetwork.org")
	_ = autn

	confirmBody, _ := json.Marshal(ConfirmationRequest{
		ResStar: base64.StdEncoding.EncodeToString(xresStar),
	})

	confirmPath := initResp.Links["5g-aka"].Href
	confirmReq := httptest.NewRequest(http.MethodPut, confirmPath, bytes.NewReader(confirmBody))
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmW := httptest.NewRecorder()

	srv.ausfHandler.HandleConfirmation(confirmW, confirmReq)

	if confirmW.Code != http.StatusOK {
		t.Fatalf("confirm expected 200, got %d: %s", confirmW.Code, confirmW.Body.String())
	}

	var confirmResp ConfirmationResponse
	json.NewDecoder(confirmW.Body).Decode(&confirmResp)

	if confirmResp.AuthResult != "SUCCESS" {
		t.Errorf("expected SUCCESS, got %s", confirmResp.AuthResult)
	}

	if confirmResp.SUPI != "imsi-286010123456789" {
		t.Errorf("expected SUPI imsi-286010123456789, got %s", confirmResp.SUPI)
	}

	if confirmResp.Kseaf == "" {
		t.Error("expected Kseaf to be non-empty")
	}
}

func TestAUSFAuthenticationConfirmationFailure(t *testing.T) {
	srv := newTestServer()

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org"}`
	initReq := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	initReq.Header.Set("Content-Type", "application/json")
	initW := httptest.NewRecorder()
	srv.ausfHandler.HandleAuthentication(initW, initReq)

	var initResp AuthenticationResponse
	json.NewDecoder(initW.Body).Decode(&initResp)

	invalidRes := make([]byte, 16)
	confirmBody, _ := json.Marshal(ConfirmationRequest{
		ResStar: base64.StdEncoding.EncodeToString(invalidRes),
	})

	confirmPath := initResp.Links["5g-aka"].Href
	confirmReq := httptest.NewRequest(http.MethodPut, confirmPath, bytes.NewReader(confirmBody))
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmW := httptest.NewRecorder()

	srv.ausfHandler.HandleConfirmation(confirmW, confirmReq)

	if confirmW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", confirmW.Code, confirmW.Body.String())
	}
}

func TestSUCIToSUPIResolution(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"imsi-286010123456789", "imsi-286010123456789"},
		{"suci-286-01-0123456789-extra", "imsi-286010123456789"},
		{"nai-user@example.com", "nai-user@example.com"},
		{"invalid", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := resolveSUPI(tt.input)
			if got != tt.expected {
				t.Errorf("resolveSUPI(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUDMSecurityInfo(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/nudm-ueau/v1/imsi-286010123456789/security-information?servingNetworkName=5G:test", nil)
	w := httptest.NewRecorder()

	srv.udmHandler.HandleSecurityInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp SecurityInfoResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.SUPI != "imsi-286010123456789" {
		t.Errorf("expected SUPI imsi-286010123456789, got %s", resp.SUPI)
	}

	if resp.AuthVector == nil {
		t.Fatal("expected auth vector")
	}

	if resp.AuthVector.AvType != AuthType5GAKA {
		t.Errorf("expected auth type %s, got %s", AuthType5GAKA, resp.AuthVector.AvType)
	}

	if resp.AuthVector.RAND == "" || resp.AuthVector.AUTN == "" {
		t.Error("expected RAND and AUTN to be non-empty")
	}
}

func TestUDMAuthEvents(t *testing.T) {
	srv := newTestServer()

	body := `{"nfInstanceId":"test-nf","success":true,"timeStamp":"2024-01-01T00:00:00Z","authType":"5G_AKA","servingNetworkName":"5G:test"}`
	req := httptest.NewRequest(http.MethodPost, "/nudm-ueau/v1/imsi-286010123456789/auth-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.udmHandler.HandleAuthEvents(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp AuthEventResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.AuthEventID == "" {
		t.Error("expected auth event ID to be non-empty")
	}
}

func TestSliceAuthenticationAllowed(t *testing.T) {
	srv := newTestServer()

	body := `{
		"supiOrSuci":"imsi-286010123456789",
		"servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org",
		"requestedNssai":[{"sst":1,"sd":"000001"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ausfHandler.HandleAuthentication(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSliceAuthenticationRejected(t *testing.T) {
	srv := newTestServer()

	body := `{
		"supiOrSuci":"imsi-286010123456789",
		"servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org",
		"requestedNssai":[{"sst":99,"sd":"999999"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ausfHandler.HandleAuthentication(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var prob ProblemDetails
	json.NewDecoder(w.Body).Decode(&prob)

	if prob.Cause != "SNSSAI_NOT_ALLOWED" {
		t.Errorf("expected cause SNSSAI_NOT_ALLOWED, got %s", prob.Cause)
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestConcurrentAuthentications(t *testing.T) {
	srv := newTestServer()

	const concurrency = 50
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			imsi := fmt.Sprintf("imsi-28601%010d", idx)
			body := fmt.Sprintf(`{"supiOrSuci":"%s","servingNetworkName":"5G:mnc001.mcc286.3gppnetwork.org"}`, imsi)
			req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			srv.ausfHandler.HandleAuthentication(w, req)

			if w.Code != http.StatusCreated {
				errs <- fmt.Errorf("goroutine %d: expected 201, got %d", idx, w.Code)
				return
			}

			var resp AuthenticationResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				errs <- fmt.Errorf("goroutine %d: decode: %v", idx, err)
				return
			}

			if resp.AuthData5G == nil {
				errs <- fmt.Errorf("goroutine %d: no auth data", idx)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

func TestExpiredAuthContextNotFound(t *testing.T) {
	srv := newTestServer()

	confirmBody, _ := json.Marshal(ConfirmationRequest{
		ResStar: base64.StdEncoding.EncodeToString([]byte("fake-res-star")),
	})
	confirmReq := httptest.NewRequest(http.MethodPut, "/nausf-auth/v1/ue-authentications/nonexistent-id/5g-aka-confirmation", bytes.NewReader(confirmBody))
	confirmReq.Header.Set("Content-Type", "application/json")
	confirmW := httptest.NewRecorder()

	srv.ausfHandler.HandleConfirmation(confirmW, confirmReq)

	if confirmW.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", confirmW.Code, confirmW.Body.String())
	}
}

func TestInvalidRequestBody(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ausfHandler.HandleAuthentication(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMissingSUPIOrSUCI(t *testing.T) {
	srv := newTestServer()

	body := `{"servingNetworkName":"5G:test"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.ausfHandler.HandleAuthentication(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestExtractAuthCtxID(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/nausf-auth/v1/ue-authentications/abc-123/5g-aka-confirmation", "abc-123"},
		{"/nausf-auth/v1/ue-authentications/abc-123", "abc-123"},
		{"/nausf-auth/v1/ue-authentications/", ""},
		{"/other/path", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractAuthCtxID(tt.path)
			if got != tt.expected {
				t.Errorf("extractAuthCtxID(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestUDMRegistration(t *testing.T) {
	srv := newTestServer()

	body := `{"amfInstanceId":"test-amf","deregCallbackUri":"https://amf.example.com/callback","guami":{"plmnId":{"mcc":"286","mnc":"01"},"amfId":"cafe00"},"ratType":"NR","initialRegistrationInd":true}`
	req := httptest.NewRequest(http.MethodPut, "/nudm-uecm/v1/imsi-286010123456789/registrations/amf-3gpp-access", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.udmHandler.HandleRegistration(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestNRFDiscoverEndpoint(t *testing.T) {
	srv := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/nnrf-nfm/v1/nf-instances", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		ValidityPeriod int `json:"validityPeriod"`
		NFInstances    []struct {
			NFType string `json:"nfType"`
		} `json:"nfInstances"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(result.NFInstances) == 0 {
		t.Fatal("expected at least one NF instance")
	}
	if result.NFInstances[0].NFType != "AUSF" {
		t.Errorf("expected AUSF, got %s", result.NFInstances[0].NFType)
	}
}

func TestNRFStatusNotifyEndpoint(t *testing.T) {
	srv := newTestServer()

	body := `{"event":"NF_REGISTERED","nfInstanceId":"test-nf","nfStatus":"REGISTERED"}`
	req := httptest.NewRequest(http.MethodPost, "/nnrf-nfm/v1/nf-status-notify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEAPProxyNoStateMachine(t *testing.T) {
	srv := newTestServer()

	body := `{"supiOrSuci":"imsi-286010123456789","servingNetworkName":"5G:test","authType":"EAP_AKA_PRIME"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/eap-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEAPProxyInvalidSUPI(t *testing.T) {
	srv := newTestServer()

	body := `{"supiOrSuci":"invalid","servingNetworkName":"5G:test","authType":"EAP_AKA_PRIME"}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/eap-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEAPProxyContinueNoStateMachine(t *testing.T) {
	srv := newTestServer()

	body := `{"eapPayload":"dGVzdA=="}`
	req := httptest.NewRequest(http.MethodPost, "/nausf-auth/v1/eap-sessions/test-session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExtractEAPSessionID(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/nausf-auth/v1/eap-sessions/abc-123", "abc-123"},
		{"/nausf-auth/v1/eap-sessions/", ""},
		{"/other/path", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractEAPSessionID(tt.path)
			if got != tt.expected {
				t.Errorf("extractEAPSessionID(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestNRFProfileDefaults(t *testing.T) {
	nrf := NewNRFRegistration(NRFConfig{
		NFInstanceID: "test-nf",
		NRFURL:       "",
	}, testLogger())

	profile := nrf.GetProfile()
	if profile.NFType != "AUSF" {
		t.Errorf("expected NFType=AUSF, got %s", profile.NFType)
	}
	if profile.NFStatus != "REGISTERED" {
		t.Errorf("expected NFStatus=REGISTERED, got %s", profile.NFStatus)
	}
	if len(profile.NFServices) != 2 {
		t.Errorf("expected 2 services, got %d", len(profile.NFServices))
	}

	if err := nrf.Register(); err != nil {
		t.Fatalf("Register with empty NRFURL should return nil: %v", err)
	}
	if err := nrf.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat with empty NRFURL should return nil: %v", err)
	}
	if err := nrf.Deregister(); err != nil {
		t.Fatalf("Deregister with empty NRFURL should return nil: %v", err)
	}
}

func TestProblemDetailsError(t *testing.T) {
	pd := ProblemDetails{
		Status: 403,
		Cause:  "SERVING_NETWORK_NOT_AUTHORIZED",
		Detail: "Network not authorized",
	}

	if pd.Error() != "Network not authorized" {
		t.Errorf("unexpected error: %s", pd.Error())
	}

	pd2 := ProblemDetails{
		Status: 403,
		Cause:  "SERVING_NETWORK_NOT_AUTHORIZED",
	}

	if pd2.Error() != "SERVING_NETWORK_NOT_AUTHORIZED" {
		t.Errorf("unexpected error: %s", pd2.Error())
	}
}
