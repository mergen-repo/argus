package reactive

import (
	"fmt"
	"sync"
	"testing"
)

func TestRegistry_RegisterLookup(t *testing.T) {
	r := NewRegistry()
	s := &Session{AcctSessionID: "sess-001"}
	r.Register(s)

	got := r.Lookup("sess-001")
	if got != s {
		t.Errorf("Lookup returned %p, want %p", got, s)
	}

	if r.Lookup("nonexistent") != nil {
		t.Error("Lookup of non-existent key should return nil")
	}
}

func TestRegistry_Delete(t *testing.T) {
	r := NewRegistry()
	s := &Session{AcctSessionID: "sess-002"}
	r.Register(s)
	r.Delete("sess-002")

	if got := r.Lookup("sess-002"); got != nil {
		t.Errorf("after Delete, Lookup returned %p, want nil", got)
	}
}

func TestRegistry_ConcurrentInsertDelete(t *testing.T) {
	r := NewRegistry()
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			s := &Session{AcctSessionID: "shared-key"}
			if i%2 == 0 {
				r.Register(s)
			} else {
				r.Delete("shared-key")
			}
		}()
	}
	wg.Wait()
}

func TestRegistry_Len(t *testing.T) {
	r := NewRegistry()

	if got := r.Len(); got != 0 {
		t.Errorf("empty registry Len = %d, want 0", got)
	}

	for i := 0; i < 3; i++ {
		r.Register(&Session{AcctSessionID: fmt.Sprintf("sess-%d", i)})
	}
	if got := r.Len(); got != 3 {
		t.Errorf("after 3 registers Len = %d, want 3", got)
	}

	r.Delete("sess-0")
	if got := r.Len(); got != 2 {
		t.Errorf("after 1 delete Len = %d, want 2", got)
	}
}
