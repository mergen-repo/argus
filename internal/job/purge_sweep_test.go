package job

import (
	"testing"
)

func TestPurgeSweepProcessor_Type(t *testing.T) {
	proc := &PurgeSweepProcessor{}
	if got := proc.Type(); got != JobTypePurgeSweep {
		t.Fatalf("Type() = %s, want %s", got, JobTypePurgeSweep)
	}
}

func TestPurgeSweepProcessor_TypeRegistered(t *testing.T) {
	found := false
	for _, jt := range AllJobTypes {
		if jt == JobTypePurgeSweep {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("purge_sweep not in AllJobTypes")
	}
}
