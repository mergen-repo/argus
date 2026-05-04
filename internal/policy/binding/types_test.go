package binding

import "testing"

func TestBindingSessionContext_PEIRaw_ZeroValueSafe(t *testing.T) {
	sc := SessionContext{}
	if sc.PEIRaw != "" {
		t.Errorf("zero-value PEIRaw: got %q, want empty string", sc.PEIRaw)
	}
}
