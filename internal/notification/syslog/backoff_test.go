package syslog

import (
	"testing"
	"time"
)

func TestBackoff_Sequence(t *testing.T) {
	var b Backoff
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second,
		60 * time.Second,
		60 * time.Second,
		60 * time.Second,
	}
	for i, w := range want {
		got := b.Next()
		if got != w {
			t.Errorf("Next()[%d] = %v, want %v", i, got, w)
		}
	}
}

func TestBackoff_ResetAfterSuccess(t *testing.T) {
	var b Backoff
	for i := 0; i < 5; i++ {
		b.Next()
	}
	b.Reset()
	got := b.Next()
	if got != 1*time.Second {
		t.Errorf("after Reset, Next() = %v, want 1s", got)
	}
}

func TestBackoff_ZeroValueSafe(t *testing.T) {
	var b Backoff
	got := b.Next()
	if got != 1*time.Second {
		t.Errorf("zero-value Backoff.Next() = %v, want 1s", got)
	}
}
