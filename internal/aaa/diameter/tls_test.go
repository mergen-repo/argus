package diameter

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestNewTLSListenerNoTLS(t *testing.T) {
	ln, err := NewTLSListener(":0", TLSConfig{Enabled: false}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewTLSListener without TLS: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	if addr == "" {
		t.Error("listener should have an address")
	}
}

func TestNewTLSListenerDisabledByDefault(t *testing.T) {
	ln, err := NewTLSListener(":0", TLSConfig{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewTLSListener default: %v", err)
	}
	defer ln.Close()
}

func TestNewTLSListenerInvalidCert(t *testing.T) {
	_, err := NewTLSListener(":0", TLSConfig{
		Enabled:  true,
		CertPath: "/nonexistent/cert.pem",
		KeyPath:  "/nonexistent/key.pem",
	}, zerolog.Nop())
	if err == nil {
		t.Error("expected error with nonexistent cert")
	}
}

func TestTLSConfigStruct(t *testing.T) {
	cfg := TLSConfig{
		CertPath: "/path/to/cert",
		KeyPath:  "/path/to/key",
		CAPath:   "/path/to/ca",
		Enabled:  true,
	}
	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
}
