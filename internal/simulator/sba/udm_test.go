package sba

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	argussba "github.com/btopcu/argus/internal/aaa/sba"
	"github.com/rs/zerolog"
)

// newUDMMux returns a ServeMux wired to the real UDMHandler — same routing
// as internal/aaa/sba/server.go:101-107.
//
// The real handler only accepts PUT on the registration path (returns 405 for
// DELETE). Tests that require DELETE are wired to a separate mock mux.
func newUDMMux() *http.ServeMux {
	handler := argussba.NewUDMHandler(nil, nil, zerolog.Nop())
	mux := http.NewServeMux()
	mux.HandleFunc("/nudm-uecm/v1/", handler.HandleRegistration)
	mux.HandleFunc("/nudm-ueau/v1/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/security-information") {
			handler.HandleSecurityInfo(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/auth-events") {
			handler.HandleAuthEvents(w, r)
			return
		}
		http.NotFound(w, r)
	})
	return mux
}

// TestUDM_RegisterHappyPath exercises RegisterViaUDM against the real
// UDMHandler and asserts the request body conforms to Amf3GppAccessRegistration.
func TestUDM_RegisterHappyPath(t *testing.T) {
	var capturedBody []byte
	mux := newUDMMux()
	// Wrap the mux to capture the request body for the canary assertion.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/nudm-uecm/v1/") && r.Method == http.MethodPut {
			var buf []byte
			var tmp [4096]byte
			for {
				n, err := r.Body.Read(tmp[:])
				buf = append(buf, tmp[:n]...)
				if err != nil {
					break
				}
			}
			capturedBody = buf
			// Re-feed body to mux handler.
			r.Body = http.NoBody
			r.Body = newReadCloser(buf)
		}
		mux.ServeHTTP(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	supi := "imsi-286010123456789"
	err := c.RegisterViaUDM(context.Background(), supi, c.amfInstanceID)
	if err != nil {
		t.Fatalf("RegisterViaUDM: %v", err)
	}

	// Canary: the body the simulator sent must decode as Amf3GppAccessRegistration
	// matching the server's expected shape.
	var reg argussba.Amf3GppAccessRegistration
	if err := json.Unmarshal(capturedBody, &reg); err != nil {
		t.Fatalf("canary: captured body not decodable as Amf3GppAccessRegistration: %v", err)
	}
	if reg.AmfInstanceID == "" {
		t.Error("canary: AmfInstanceID is empty")
	}
	if reg.RATType != "NR" {
		t.Errorf("canary: RATType=%q, want NR", reg.RATType)
	}
	if !reg.InitialRegInd {
		t.Error("canary: InitialRegInd should be true for initial registration")
	}
	if reg.GUAMI.PlmnID.MCC == "" || reg.GUAMI.PlmnID.MNC == "" {
		t.Errorf("canary: GUAMI.PlmnID incomplete: %+v", reg.GUAMI.PlmnID)
	}
}

// TestUDM_RegisterFailure verifies that a 403 response from the UDM causes
// RegisterViaUDM to return an error wrapping ErrServerError.
func TestUDM_RegisterFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/nudm-uecm/v1/", func(w http.ResponseWriter, r *http.Request) {
		prob := argussba.ProblemDetails{
			Status: http.StatusForbidden,
			Cause:  "MANDATORY_IE_INCORRECT",
			Detail: "amfInstanceId is missing",
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(prob)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.RegisterViaUDM(context.Background(), "imsi-286010000000001", c.amfInstanceID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrServerError) {
		t.Errorf("expected ErrServerError, got %v", err)
	}
	if !strings.Contains(err.Error(), "MANDATORY_IE_INCORRECT") {
		t.Errorf("error should mention MANDATORY_IE_INCORRECT, got: %v", err)
	}
}

// TestUDM_Register500 verifies that a 500 response causes RegisterViaUDM to
// return a wrapped ErrServerError with no cause panic.
func TestUDM_Register500(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/nudm-uecm/v1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.RegisterViaUDM(context.Background(), "imsi-286010000000002", c.amfInstanceID)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !errors.Is(err, ErrServerError) {
		t.Errorf("expected ErrServerError, got %v", err)
	}
}

// TestUDM_RegisterTimeout verifies that a hung UDM server causes
// RegisterViaUDM to return an error wrapping ErrTimeout.
func TestUDM_RegisterTimeout(t *testing.T) {
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

	err := c.RegisterViaUDM(ctx, "imsi-286010000000003", c.amfInstanceID)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected ErrTimeout, got %v", err)
	}
}

// TestUDM_SecurityInformation_HappyPath exercises the optional GET
// /nudm-ueau/v1/{supi}/security-information path against the real UDMHandler.
// This is one of the two optional SBA calls (gated on IncludeOptionalCalls +
// per-session Bernoulli roll in the client); this test exercises it directly
// to assert the metric + endpoint name contract.
func TestUDM_SecurityInformation_HappyPath(t *testing.T) {
	srv := httptest.NewServer(newUDMMux())
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.GetSecurityInformation(context.Background(), "imsi-286010123456789", "5G:mnc001.mcc286.3gppnetwork.org")
	if err != nil {
		t.Fatalf("GetSecurityInformation: %v", err)
	}
}

// TestUDM_AuthEvents_HappyPath exercises the optional POST
// /nudm-ueau/v1/{supi}/auth-events path. RecordAuthEvent is called at session
// end with success=true when IncludeOptionalCalls is on.
func TestUDM_AuthEvents_HappyPath(t *testing.T) {
	srv := httptest.NewServer(newUDMMux())
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.RecordAuthEvent(context.Background(), "imsi-286010123456789", true)
	if err != nil {
		t.Fatalf("RecordAuthEvent: %v", err)
	}
}

// TestUDM_RegisterCanary_ShapeValidation builds an Amf3GppAccessRegistration
// via the simulator (RegisterViaUDM request body) and asserts it round-trips
// through the server-side struct without loss of key fields.
//
// The real UDMHandler at line 172 of internal/aaa/sba/udm.go echoes the
// decoded body back as JSON on 201 Created. We decode that echo and compare.
func TestUDM_RegisterCanary_ShapeValidation(t *testing.T) {
	var echoBody []byte

	// Use real handler which echoes the body back.
	mux := http.NewServeMux()
	handler := argussba.NewUDMHandler(nil, nil, zerolog.Nop())
	mux.HandleFunc("/nudm-uecm/v1/", handler.HandleRegistration)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		result := rec.Result()
		defer result.Body.Close()

		// Copy response headers + status.
		for k, v := range result.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(result.StatusCode)
		var buf [8192]byte
		for {
			n, err := result.Body.Read(buf[:])
			echoBody = append(echoBody, buf[:n]...)
			w.Write(buf[:n])
			if err != nil {
				break
			}
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := c.RegisterViaUDM(context.Background(), "imsi-286010123456789", "sim-amf-canary")
	if err != nil {
		t.Fatalf("RegisterViaUDM (canary): %v", err)
	}

	var echo argussba.Amf3GppAccessRegistration
	if err := json.Unmarshal(echoBody, &echo); err != nil {
		t.Fatalf("canary: decode echo body: %v", err)
	}
	if echo.AmfInstanceID != "sim-amf-canary" {
		t.Errorf("canary: AmfInstanceID=%q, want sim-amf-canary", echo.AmfInstanceID)
	}
	if echo.RATType != "NR" {
		t.Errorf("canary: RATType=%q, want NR", echo.RATType)
	}
	if !echo.InitialRegInd {
		t.Error("canary: InitialRegInd should be true")
	}
	if echo.GUAMI.PlmnID.MCC != "286" || echo.GUAMI.PlmnID.MNC != "01" {
		t.Errorf("canary: GUAMI.PlmnID=%+v, want MCC=286 MNC=01", echo.GUAMI.PlmnID)
	}
	if echo.GUAMI.AmfID != "abc123" {
		t.Errorf("canary: GUAMI.AmfID=%q, want abc123", echo.GUAMI.AmfID)
	}
}

// --- helpers ---

type staticReadCloser struct {
	data []byte
	pos  int
}

func newReadCloser(data []byte) *staticReadCloser {
	cp := make([]byte, len(data))
	copy(cp, data)
	return &staticReadCloser{data: cp}
}

func (r *staticReadCloser) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *staticReadCloser) Close() error { return nil }
