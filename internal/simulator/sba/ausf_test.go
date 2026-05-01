package sba

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/rs/zerolog"
)

// newTestClient builds an sba.Client pointed at the given httptest server URL.
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	defaults := config.SBADefaults{
		Host:               "127.0.0.1",
		Port:               0,
		TLSEnabled:         false,
		ServingNetworkName: "5G:mnc001.mcc286.3gppnetwork.org",
		RequestTimeout:     5 * time.Second,
		AMFInstanceID:      "sim-amf-01",
		DeregCallbackURI:   "http://sim-amf.invalid/dereg",
	}
	op := config.OperatorConfig{
		Code: "test-op",
	}
	c := New(op, defaults, zerolog.Nop())
	c.baseURL = serverURL
	return c
}

// newAUSFMux returns a ServeMux wired to the real AUSFHandler — same routing
// as internal/aaa/sba/server.go:75-84.
func newAUSFMux() *http.ServeMux {
	handler := argussba.NewAUSFHandler(nil, nil, nil, zerolog.Nop())
	mux := http.NewServeMux()
	mux.HandleFunc("/nausf-auth/v1/ue-authentications", handler.HandleAuthentication)
	mux.HandleFunc("/nausf-auth/v1/ue-authentications/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/5g-aka-confirmation") {
			handler.HandleConfirmation(w, r)
			return
		}
		http.NotFound(w, r)
	})
	return mux
}

// TestCrypto_Canary verifies that the simulator's generate5GAVSim produces
// xresStar values that, when sha256-hashed (first 16 bytes), match what the
// server returns in HxresStar for the same (supi, servingNetwork) inputs.
//
// Layered defence (plan Task 4 §What): hardcoded expected hex bytes are
// compared against the simulator's output first — catches copy-paste drift
// where both trees mutate together (the httptest path would silently pass).
// Then the simulator output is compared to the live server response.
//
// If internal/aaa/sba/ausf.go's generate5GAV ever changes, this test fails
// loudly on both checks; re-copy the helpers AND regenerate the expected hex.
func TestCrypto_Canary(t *testing.T) {
	const imsi = "286010123456789"
	const sn = "5G:mnc001.mcc286.3gppnetwork.org"
	supi := "imsi-" + imsi

	// Hardcoded expected values computed from the reference generate5GAVSim
	// for supi="imsi-286010123456789", sn="5G:mnc001.mcc286.3gppnetwork.org".
	// Regenerate via scripts/tools if crypto helpers are intentionally updated.
	const wantXresStarHex = "82679e3d5d493cde266595561edcb62d"
	const wantHxresStarHex = "6114eef9293b7c60dafd0e84c0af9b95"

	_, _, simXresStar, _ := generate5GAVSim(supi, sn)
	gotXresStarHex := hex.EncodeToString(simXresStar)
	if gotXresStarHex != wantXresStarHex {
		t.Fatalf("simulator generate5GAVSim xresStar drift: got %s, want %s (re-copy crypto.go from internal/aaa/sba/ausf.go)", gotXresStarHex, wantXresStarHex)
	}

	h := sha256.Sum256(simXresStar)
	gotHxresStarHex := hex.EncodeToString(h[:16])
	if gotHxresStarHex != wantHxresStarHex {
		t.Fatalf("simulator hxresStar drift: got %s, want %s", gotHxresStarHex, wantHxresStarHex)
	}

	// Second layer: compare against the live server. If server mutates in
	// isolation, this check fires. Both layers together catch both drift modes.
	srv := httptest.NewServer(newAUSFMux())
	defer srv.Close()

	body := `{"supiOrSuci":"` + supi + `","servingNetworkName":"` + sn + `"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/nausf-auth/v1/ue-authentications", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST authenticate: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var authResp argussba.AuthenticationResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if authResp.AuthData5G == nil {
		t.Fatal("AuthData5G is nil")
	}

	serverHxresStar, err := base64.StdEncoding.DecodeString(authResp.AuthData5G.HxresStar)
	if err != nil {
		t.Fatalf("decode hxresStar: %v", err)
	}

	simHxresStar := h[:16]
	if len(simHxresStar) != len(serverHxresStar) {
		t.Fatalf("hxresStar length mismatch: sim=%d server=%d", len(simHxresStar), len(serverHxresStar))
	}
	for i := range simHxresStar {
		if simHxresStar[i] != serverHxresStar[i] {
			t.Errorf("hxresStar byte[%d] mismatch: sim=0x%02x server=0x%02x (server diverged from simulator)", i, simHxresStar[i], serverHxresStar[i])
		}
	}
}

// TestAUSF_HappyPath exercises the full POST authenticate → PUT confirm flow
// against a real in-process AUSF handler and verifies AuthenticateViaAUSF +
// ConfirmAUSF both succeed.
func TestAUSF_HappyPath(t *testing.T) {
	srv := httptest.NewServer(newAUSFMux())
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	const imsi = "286010123456789"
	const sn = "5G:mnc001.mcc286.3gppnetwork.org"

	href, err := c.AuthenticateViaAUSF(context.Background(), imsi, sn)
	if err != nil {
		t.Fatalf("AuthenticateViaAUSF: %v", err)
	}
	if !strings.Contains(href, "/5g-aka-confirmation") {
		t.Errorf("href %q does not end in /5g-aka-confirmation", href)
	}

	supi, kseaf, err := c.ConfirmAUSF(context.Background(), href, imsi, sn)
	if err != nil {
		t.Fatalf("ConfirmAUSF: %v", err)
	}
	if supi != "imsi-"+imsi {
		t.Errorf("supi=%q, want %q", supi, "imsi-"+imsi)
	}
	if len(kseaf) == 0 {
		t.Error("kseaf is empty")
	}
}

// TestAUSF_AuthFailure verifies that a 401 AUTH_REJECTED response from the
// AUSF causes ConfirmAUSF to return an error wrapping ErrConfirmFailed.
//
// We use a stub server that accepts the POST but returns 401 on the PUT.
func TestAUSF_AuthFailure(t *testing.T) {
	authCtxHref := "/nausf-auth/v1/ue-authentications/test-ctx/5g-aka-confirmation"

	mux := http.NewServeMux()
	mux.HandleFunc("/nausf-auth/v1/ue-authentications", func(w http.ResponseWriter, r *http.Request) {
		resp := argussba.AuthenticationResponse{
			AuthType: argussba.AuthType5GAKA,
			AuthData5G: &argussba.AKA5GAuthData{
				RAND:      base64.StdEncoding.EncodeToString([]byte("rand1234rand1234")),
				AUTN:      base64.StdEncoding.EncodeToString([]byte("autn1234autn1234")),
				HxresStar: base64.StdEncoding.EncodeToString([]byte("hxres1234hxres12")),
			},
			Links: map[string]argussba.AuthLink{
				"5g-aka": {Href: authCtxHref},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/nausf-auth/v1/ue-authentications/", func(w http.ResponseWriter, r *http.Request) {
		prob := argussba.ProblemDetails{
			Status: http.StatusUnauthorized,
			Cause:  "AUTH_REJECTED",
			Detail: "Authentication verification failed",
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(prob)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	const imsi = "286010123456789"
	const sn = "5G:mnc001.mcc286.3gppnetwork.org"

	href, err := c.AuthenticateViaAUSF(context.Background(), imsi, sn)
	if err != nil {
		t.Fatalf("AuthenticateViaAUSF: %v", err)
	}

	_, _, err = c.ConfirmAUSF(context.Background(), href, imsi, sn)
	if err == nil {
		t.Fatal("ConfirmAUSF: expected error, got nil")
	}
	if !errors.Is(err, ErrConfirmFailed) {
		t.Errorf("expected ErrConfirmFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "AUTH_REJECTED") {
		t.Errorf("error should mention AUTH_REJECTED, got: %v", err)
	}
}

// TestAUSF_Timeout verifies that a hung server causes AuthenticateViaAUSF to
// return an error wrapping ErrTimeout when the context deadline fires.
func TestAUSF_Timeout(t *testing.T) {
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
	}))
	defer func() {
		close(unblock)
		srv.Close()
	}()

	c := newTestClient(t, srv.URL)
	c.httpClient.Timeout = 0

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.AuthenticateViaAUSF(ctx, "286010123456789", "5G:mnc001.mcc286.3gppnetwork.org")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got %v", err)
	}
}
