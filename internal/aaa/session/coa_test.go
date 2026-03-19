package session

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestNewCoASender(t *testing.T) {
	logger := zerolog.Nop()
	sender := NewCoASender("testing123", 0, logger)
	if sender == nil {
		t.Fatal("NewCoASender returned nil")
	}
	if sender.port != defaultCoAPort {
		t.Errorf("port = %d, want %d", sender.port, defaultCoAPort)
	}
}

func TestNewCoASender_CustomPort(t *testing.T) {
	logger := zerolog.Nop()
	sender := NewCoASender("secret", 4000, logger)
	if sender.port != 4000 {
		t.Errorf("port = %d, want 4000", sender.port)
	}
}
