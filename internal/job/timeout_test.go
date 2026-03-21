package job

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestTimeoutDetector_Defaults(t *testing.T) {
	td := NewTimeoutDetector(nil, nil, 0, 0, zerolog.Nop())
	if td.timeout != 30*time.Minute {
		t.Errorf("default timeout = %v, want 30m", td.timeout)
	}
	if td.interval != 5*time.Minute {
		t.Errorf("default interval = %v, want 5m", td.interval)
	}
}

func TestTimeoutDetector_CustomValues(t *testing.T) {
	td := NewTimeoutDetector(nil, nil, 15*time.Minute, 2*time.Minute, zerolog.Nop())
	if td.timeout != 15*time.Minute {
		t.Errorf("timeout = %v, want 15m", td.timeout)
	}
	if td.interval != 2*time.Minute {
		t.Errorf("interval = %v, want 2m", td.interval)
	}
}

func TestTimeoutDetector_StartStop(t *testing.T) {
	td := NewTimeoutDetector(nil, nil, 30*time.Minute, 1*time.Hour, zerolog.Nop())
	td.Start()

	done := make(chan struct{})
	go func() {
		td.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout detector did not stop within 5 seconds")
	}
}
