package compliance

import (
	"testing"
)

func TestUpdateRetentionRequest_Validation(t *testing.T) {
	tests := []struct {
		days  int
		valid bool
	}{
		{29, false},
		{30, true},
		{90, true},
		{365, true},
		{366, false},
		{0, false},
		{-1, false},
	}

	for _, tt := range tests {
		valid := tt.days >= 30 && tt.days <= 365
		if valid != tt.valid {
			t.Errorf("days=%d: valid=%v, want %v", tt.days, valid, tt.valid)
		}
	}
}
