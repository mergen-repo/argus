package session

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestNewDMSender(t *testing.T) {
	logger := zerolog.Nop()
	sender := NewDMSender("testing123", 0, logger)
	if sender == nil {
		t.Fatal("NewDMSender returned nil")
	}
	if sender.port != defaultCoAPort {
		t.Errorf("port = %d, want %d", sender.port, defaultCoAPort)
	}
}

func TestNewDMSender_CustomPort(t *testing.T) {
	logger := zerolog.Nop()
	sender := NewDMSender("secret", 4001, logger)
	if sender.port != 4001 {
		t.Errorf("port = %d, want 4001", sender.port)
	}
}
