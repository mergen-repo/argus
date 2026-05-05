package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/notification/syslog"
	"github.com/btopcu/argus/internal/notification/syslog/syslogtest"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestHandler() *LogForwardingHandler {
	return NewLogForwardingHandler(nil, nil, zerolog.Nop())
}

func makeReqWithTenant(t *testing.T, method, path string, body interface{}, tenantID uuid.UUID) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	ctx := context.WithValue(req.Context(), apierr.TenantIDKey, tenantID)
	return req.WithContext(ctx)
}

func reqWithChiParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	h, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return h, p
}

func decodeTestResponse(t *testing.T, w *httptest.ResponseRecorder) testResponse {
	t.Helper()
	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, _ := json.Marshal(resp.Data)
	var tr testResponse
	json.Unmarshal(data, &tr)
	return tr
}

// ── validation tests (no store or DB required) ────────────────────────────────

// TestList_NoTenantContext returns 403 when TenantIDKey is absent.
func TestList_NoTenantContext(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/log-forwarding", nil)
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// TestUpsert_InvalidTransport returns 422 on unknown transport value.
func TestUpsert_InvalidTransport(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "dest",
		Host:      "127.0.0.1",
		Port:      514,
		Transport: "grpc",
		Format:    syslog.FormatRFC3164,
		Facility:  1,
		Enabled:   true,
	}
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_TRANSPORT" {
		t.Errorf("error code = %q, want INVALID_TRANSPORT", resp.Error.Code)
	}
}

// TestUpsert_InvalidFormat returns 422 on unknown format value.
func TestUpsert_InvalidFormat(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "dest",
		Host:      "127.0.0.1",
		Port:      514,
		Transport: syslog.TransportUDP,
		Format:    "syslog-ng",
		Facility:  1,
		Enabled:   true,
	}
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_FORMAT" {
		t.Errorf("error code = %q, want INVALID_FORMAT", resp.Error.Code)
	}
}

// TestUpsert_InvalidFacility returns 422 when facility is out of 0..23 range.
func TestUpsert_InvalidFacility(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "dest",
		Host:      "127.0.0.1",
		Port:      514,
		Transport: syslog.TransportUDP,
		Format:    syslog.FormatRFC3164,
		Facility:  99,
		Enabled:   true,
	}
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_FACILITY" {
		t.Errorf("error code = %q, want INVALID_FACILITY", resp.Error.Code)
	}
}

// TestUpsert_InvalidCategory returns 422 when filter_categories contains an
// unknown category string.
func TestUpsert_InvalidCategory(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	body := upsertRequest{
		Name:             "dest",
		Host:             "127.0.0.1",
		Port:             514,
		Transport:        syslog.TransportUDP,
		Format:           syslog.FormatRFC3164,
		Facility:         1,
		FilterCategories: []string{"unknown_category"},
		Enabled:          true,
	}
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_CATEGORY" {
		t.Errorf("error code = %q, want INVALID_CATEGORY", resp.Error.Code)
	}
}

// TestUpsert_TLSClientCertXOR returns 422 when only cert but no key is supplied.
func TestUpsert_TLSClientCertXOR(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	certOnly := json.RawMessage(`"not-real-pem"`)
	body := upsertRequest{
		Name:             "dest",
		Host:             "127.0.0.1",
		Port:             514,
		Transport:        syslog.TransportTLS,
		Format:           syslog.FormatRFC5424,
		Facility:         1,
		Enabled:          true,
		TLSClientCertPEM: certOnly,
	}
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
	w := httptest.NewRecorder()
	h.Upsert(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", w.Code)
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "TLS_CONFIG_INVALID" {
		t.Errorf("error code = %q, want TLS_CONFIG_INVALID", resp.Error.Code)
	}
}

// TestSetEnabled_InvalidID returns 400 on malformed UUID path parameter.
func TestSetEnabled_InvalidID(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	body := map[string]bool{"enabled": false}
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding/bad-uuid/enabled", body, tenantID)
	req = reqWithChiParam(req, "id", "not-a-uuid")
	w := httptest.NewRecorder()
	h.SetEnabled(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestDelete_InvalidID returns 400 on malformed UUID path parameter.
func TestDelete_InvalidID(t *testing.T) {
	h := newTestHandler()
	tenantID := uuid.New()
	req := makeReqWithTenant(t, http.MethodDelete, "/api/v1/settings/log-forwarding/not-a-uuid", nil, tenantID)
	req = reqWithChiParam(req, "id", "not-a-uuid")
	w := httptest.NewRecorder()
	h.Delete(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── Test endpoint round-trip tests (no store, no DB) ─────────────────────────

// TestTest_UDPRoundTrip starts a local UDP listener and verifies Test returns
// {ok:true} for UDP transport.
func TestTest_UDPRoundTrip(t *testing.T) {
	l, addr := syslogtest.NewUDPListener(t)
	defer l.Close()

	host, port := splitHostPort(t, addr)
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "test-dest",
		Host:      host,
		Port:      port,
		Transport: syslog.TransportUDP,
		Format:    syslog.FormatRFC3164,
		Facility:  1,
		Enabled:   true,
	}
	h := newTestHandler()
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding/test", body, tenantID)
	w := httptest.NewRecorder()
	h.Test(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	tr := decodeTestResponse(t, w)
	if !tr.OK {
		t.Errorf("test response ok = false, error = %q", tr.Error)
	}
}

// TestTest_TCPRoundTrip starts a local TCP listener and verifies Test returns
// {ok:true} for TCP transport.
func TestTest_TCPRoundTrip(t *testing.T) {
	l, addr := syslogtest.NewTCPListener(t)
	defer l.Close()

	host, port := splitHostPort(t, addr)
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "test-dest",
		Host:      host,
		Port:      port,
		Transport: syslog.TransportTCP,
		Format:    syslog.FormatRFC5424,
		Facility:  1,
		Enabled:   true,
	}
	h := newTestHandler()
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding/test", body, tenantID)
	w := httptest.NewRecorder()
	h.Test(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	tr := decodeTestResponse(t, w)
	if !tr.OK {
		t.Errorf("test response ok = false, error = %q", tr.Error)
	}
}

// TestUpsert_InvalidPort returns 422 INVALID_PORT when port is out of range.
// STORY-098 Gate F-A3.
func TestUpsert_InvalidPort(t *testing.T) {
	cases := []struct {
		name string
		port int
	}{
		{"zero port", 0},
		{"negative port", -1},
		{"oversize port", 99999},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHandler()
			tenantID := uuid.New()
			body := upsertRequest{
				Name:      "dest",
				Host:      "127.0.0.1",
				Port:      c.port,
				Transport: syslog.TransportUDP,
				Format:    syslog.FormatRFC3164,
				Facility:  1,
				Enabled:   true,
			}
			req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
			w := httptest.NewRecorder()
			h.Upsert(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
			}
			var resp apierr.ErrorResponse
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Error.Code != "INVALID_PORT" {
				t.Errorf("error code = %q, want INVALID_PORT", resp.Error.Code)
			}
		})
	}
}

// TestUpsert_InvalidName returns 422 INVALID_FORMAT when name is empty or
// exceeds 255 chars. STORY-098 Gate F-A3.
func TestUpsert_InvalidName(t *testing.T) {
	cases := []struct {
		label string
		name  string
	}{
		{"empty name", ""},
		{"oversize name", string(make([]byte, 300)) + "x"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			h := newTestHandler()
			tenantID := uuid.New()
			body := upsertRequest{
				Name:      c.name,
				Host:      "127.0.0.1",
				Port:      514,
				Transport: syslog.TransportUDP,
				Format:    syslog.FormatRFC3164,
				Facility:  1,
				Enabled:   true,
			}
			req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
			w := httptest.NewRecorder()
			h.Upsert(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
			}
			var resp apierr.ErrorResponse
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Error.Code != apierr.CodeInvalidFormat {
				t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
			}
		})
	}
}

// TestUpsert_InvalidHost returns 422 INVALID_FORMAT when host is empty or
// exceeds 255 chars. STORY-098 Gate F-A3.
func TestUpsert_InvalidHost(t *testing.T) {
	cases := []struct {
		label string
		host  string
	}{
		{"empty host", ""},
		{"oversize host", string(make([]byte, 300)) + "x"},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			h := newTestHandler()
			tenantID := uuid.New()
			body := upsertRequest{
				Name:      "dest",
				Host:      c.host,
				Port:      514,
				Transport: syslog.TransportUDP,
				Format:    syslog.FormatRFC3164,
				Facility:  1,
				Enabled:   true,
			}
			req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding", body, tenantID)
			w := httptest.NewRecorder()
			h.Upsert(w, req)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
			}
			var resp apierr.ErrorResponse
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Error.Code != apierr.CodeInvalidFormat {
				t.Errorf("error code = %q, want %q", resp.Error.Code, apierr.CodeInvalidFormat)
			}
		})
	}
}

// TestTest_BlockedMetadataHost rejects probes targeted at well-known cloud
// metadata IPs to narrow the SSRF surface a tenant_admin might exploit.
// STORY-098 Gate F-A5.
func TestTest_BlockedMetadataHost(t *testing.T) {
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "metadata-probe",
		Host:      "169.254.169.254",
		Port:      80,
		Transport: syslog.TransportTCP,
		Format:    syslog.FormatRFC3164,
		Facility:  1,
		Enabled:   true,
	}
	h := newTestHandler()
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding/test", body, tenantID)
	w := httptest.NewRecorder()
	h.Test(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
	}
	var resp apierr.ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Code != "INVALID_HOST" {
		t.Errorf("error code = %q, want INVALID_HOST", resp.Error.Code)
	}
}

// TestTest_UnreachableHost returns HTTP 200 with {ok:false, error:"..."} when
// the target host is not accepting connections (VAL-098-08: test result is in
// the JSON body, not the HTTP status).
func TestTest_UnreachableHost(t *testing.T) {
	tenantID := uuid.New()
	body := upsertRequest{
		Name:      "dead-dest",
		Host:      "127.0.0.1",
		Port:      1,
		Transport: syslog.TransportTCP,
		Format:    syslog.FormatRFC3164,
		Facility:  1,
		Enabled:   true,
	}
	h := newTestHandler()
	req := makeReqWithTenant(t, http.MethodPost, "/api/v1/settings/log-forwarding/test", body, tenantID)
	w := httptest.NewRecorder()
	h.Test(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (test errors are in JSON body, not HTTP status)", w.Code)
	}
	tr := decodeTestResponse(t, w)
	if tr.OK {
		t.Error("expected ok=false for unreachable host")
	}
	if tr.Error == "" {
		t.Error("expected non-empty error string for unreachable host")
	}
}
