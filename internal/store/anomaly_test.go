package store

import (
	"testing"
)

func TestAnomalyStateTransitions(t *testing.T) {
	tests := []struct {
		from    string
		to      string
		allowed bool
	}{
		{"open", "acknowledged", true},
		{"open", "resolved", true},
		{"open", "false_positive", true},
		{"acknowledged", "resolved", true},
		{"acknowledged", "false_positive", true},
		{"acknowledged", "open", false},
		{"resolved", "open", false},
		{"resolved", "acknowledged", false},
		{"false_positive", "open", false},
		{"false_positive", "acknowledged", false},
	}

	for _, tt := range tests {
		valid := false
		for _, allowed := range validAnomalyTransitions[tt.from] {
			if allowed == tt.to {
				valid = true
				break
			}
		}
		if valid != tt.allowed {
			t.Errorf("transition %q->%q: got valid=%v, want %v", tt.from, tt.to, valid, tt.allowed)
		}
	}
}

func TestAnomalyColumns(t *testing.T) {
	if anomalyColumns == "" {
		t.Error("anomalyColumns should not be empty")
	}
}
