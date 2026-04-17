package sba

import (
	"net/http"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
	"github.com/rs/zerolog"
)

func testSBADefaults() config.SBADefaults {
	return config.SBADefaults{
		Host:               "127.0.0.1",
		Port:               8443,
		TLSEnabled:         false,
		TLSSkipVerify:      false,
		ServingNetworkName: "5G:mnc001.mcc286.3gppnetwork.org",
		RequestTimeout:     5 * time.Second,
		AMFInstanceID:      "sim-amf-01",
		DeregCallbackURI:   "http://sim-amf.invalid/dereg",
	}
}

func testOperatorCfg() config.OperatorConfig {
	return config.OperatorConfig{
		Code:          "test-op",
		NASIdentifier: "test-nas",
		NASIP:         "127.0.0.1",
		SBA: &config.OperatorSBAConfig{
			Enabled:    true,
			Rate:       0.5,
			AuthMethod: "5G_AKA",
		},
	}
}

// TestNew_HTTP constructs a Client with TLS disabled and verifies the base URL,
// HTTP transport type, and client timeout.
func TestNew_HTTP(t *testing.T) {
	defaults := testSBADefaults()
	op := testOperatorCfg()
	c := New(op, defaults, zerolog.Nop())

	if c == nil {
		t.Fatal("New returned nil")
	}
	if c.baseURL != "http://127.0.0.1:8443" {
		t.Errorf("baseURL=%q, want %q", c.baseURL, "http://127.0.0.1:8443")
	}
	if c.httpClient == nil {
		t.Fatal("httpClient is nil")
	}
	if c.httpClient.Timeout != defaults.RequestTimeout {
		t.Errorf("Timeout=%v, want %v", c.httpClient.Timeout, defaults.RequestTimeout)
	}
	if _, ok := c.httpClient.Transport.(*http.Transport); !ok {
		t.Errorf("expected *http.Transport for non-TLS, got %T", c.httpClient.Transport)
	}
}

// TestNew_TLS constructs a Client with TLS enabled and verifies the base URL
// and that TLS config is set with the expected InsecureSkipVerify.
func TestNew_TLS(t *testing.T) {
	defaults := testSBADefaults()
	defaults.TLSEnabled = true
	defaults.TLSSkipVerify = true
	op := testOperatorCfg()

	c := New(op, defaults, zerolog.Nop())

	if c == nil {
		t.Fatal("New returned nil")
	}
	if c.baseURL != "https://127.0.0.1:8443" {
		t.Errorf("baseURL=%q, want %q", c.baseURL, "https://127.0.0.1:8443")
	}
	t.Log("TLS client constructed successfully")

	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.httpClient.Transport)
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil for TLS-enabled client")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("TLSClientConfig.InsecureSkipVerify should be true when TLSSkipVerify=true")
	}
}

// TestNew_TLS_SkipVerifyFalse verifies InsecureSkipVerify is false when not requested.
func TestNew_TLS_SkipVerifyFalse(t *testing.T) {
	defaults := testSBADefaults()
	defaults.TLSEnabled = true
	defaults.TLSSkipVerify = false
	op := testOperatorCfg()

	c := New(op, defaults, zerolog.Nop())
	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.httpClient.Transport)
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("TLSClientConfig is nil")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify should be false when TLSSkipVerify=false")
	}
}

// TestNew_Fields verifies that the Client fields from config are set correctly.
func TestNew_Fields(t *testing.T) {
	defaults := testSBADefaults()
	defaults.ServingNetworkName = "5G:mnc099.mcc001.3gppnetwork.org"
	defaults.AMFInstanceID = "amf-test-99"
	defaults.DeregCallbackURI = "http://test-amf.local/dereg"
	op := testOperatorCfg()

	c := New(op, defaults, zerolog.Nop())

	if c.operatorCode != "test-op" {
		t.Errorf("operatorCode=%q, want %q", c.operatorCode, "test-op")
	}
	if c.servingNetworkName != defaults.ServingNetworkName {
		t.Errorf("servingNetworkName=%q, want %q", c.servingNetworkName, defaults.ServingNetworkName)
	}
	if c.amfInstanceID != defaults.AMFInstanceID {
		t.Errorf("amfInstanceID=%q, want %q", c.amfInstanceID, defaults.AMFInstanceID)
	}
	if c.deregCallbackURI != defaults.DeregCallbackURI {
		t.Errorf("deregCallbackURI=%q, want %q", c.deregCallbackURI, defaults.DeregCallbackURI)
	}
}

// TestNew_DefaultSlices verifies the client gets a default S-NSSAI slice
// when the operator config does not specify slices.
func TestNew_DefaultSlices(t *testing.T) {
	defaults := testSBADefaults()
	op := testOperatorCfg()

	c := New(op, defaults, zerolog.Nop())

	if len(c.slices) != 1 {
		t.Fatalf("expected 1 default slice, got %d", len(c.slices))
	}
	if c.slices[0].SST != 1 || c.slices[0].SD != "000001" {
		t.Errorf("default slice=%+v, want {SST:1 SD:000001}", c.slices[0])
	}
}

// TestNilGuards verifies that nil ctx/sc inputs do not panic.
// Register guards against nil and returns nil early.
// Authenticate returns nil on nil inputs per its own guard.
func TestNilGuards(t *testing.T) {
	c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())

	if err := c.Authenticate(nil, nil); err != nil {
		t.Errorf("Authenticate nil guard: expected nil, got %v", err)
	}
	if err := c.Register(nil, nil, ""); err != nil {
		t.Errorf("Register nil guard: expected nil, got %v", err)
	}
}

// TestStop_CloseIdleConnections verifies Stop() succeeds without panicking.
func TestStop_CloseIdleConnections(t *testing.T) {
	c := New(testOperatorCfg(), testSBADefaults(), zerolog.Nop())
	if err := c.Stop(nil); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}
