package radius

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestRadSecServerNewDefault(t *testing.T) {
	srv := NewRadSecServer(RadSecConfig{}, nil, zerolog.Nop())
	if srv.cfg.Addr != ":2083" {
		t.Errorf("default addr = %q, want %q", srv.cfg.Addr, ":2083")
	}
}

func TestRadSecServerStartNoCert(t *testing.T) {
	srv := NewRadSecServer(RadSecConfig{
		Addr: ":0",
	}, nil, zerolog.Nop())

	if err := srv.Start(); err != nil {
		t.Errorf("Start() should succeed with no cert (disabled): %v", err)
	}

	if srv.IsRunning() {
		t.Error("server should not be running when no cert configured")
	}
}

func TestRadSecServerNotRunningInitially(t *testing.T) {
	srv := NewRadSecServer(RadSecConfig{}, nil, zerolog.Nop())
	if srv.IsRunning() {
		t.Error("server should not be running initially")
	}
}

func TestRadSecServerStopWhenNotRunning(t *testing.T) {
	srv := NewRadSecServer(RadSecConfig{}, nil, zerolog.Nop())
	srv.Stop()
}

func TestTLSVersionString(t *testing.T) {
	tests := []struct {
		version uint16
		want    string
	}{
		{0x0301, "TLS 1.0"},
		{0x0302, "TLS 1.1"},
		{0x0303, "TLS 1.2"},
		{0x0304, "TLS 1.3"},
		{0x0000, "0x0000"},
	}

	for _, tt := range tests {
		got := tlsVersionString(tt.version)
		if got != tt.want {
			t.Errorf("tlsVersionString(0x%04x) = %q, want %q", tt.version, got, tt.want)
		}
	}
}
