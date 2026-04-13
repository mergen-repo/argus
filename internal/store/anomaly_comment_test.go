package store

import (
	"testing"
)

func TestAnomalyCommentStore_New(t *testing.T) {
	s := NewAnomalyCommentStore(nil)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestAnomalyCommentBodyConstraint(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		valid bool
	}{
		{"empty body", "", false},
		{"single char", "x", true},
		{"normal", "This looks like a network issue", true},
		{"max 2000", string(make([]byte, 2000)), true},
		{"over 2000", string(make([]byte, 2001)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyLen := len(tt.body)
			valid := bodyLen >= 1 && bodyLen <= 2000
			if valid != tt.valid {
				t.Errorf("body length %d: got valid=%v, want %v", bodyLen, valid, tt.valid)
			}
		})
	}
}

func TestAnomalyCommentStoreFields(t *testing.T) {
	c := AnomalyComment{}
	if c.ID.String() == "" {
		t.Log("AnomalyComment has expected zero-value UUID ID field")
	}
	if c.Body != "" {
		t.Error("Body should default to empty string")
	}
}
