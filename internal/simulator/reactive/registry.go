package reactive

import "sync"

// Registry maps AcctSessionID → *Session. Safe for concurrent use.
type Registry struct {
	m sync.Map // map[string]*Session
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(s *Session) {
	r.m.Store(s.AcctSessionID, s)
}

func (r *Registry) Lookup(acctSessionID string) *Session {
	v, ok := r.m.Load(acctSessionID)
	if !ok {
		return nil
	}
	return v.(*Session)
}

func (r *Registry) Delete(acctSessionID string) {
	r.m.Delete(acctSessionID)
}

// Len is O(n) via Range — for tests/debug only. Not hot-path safe.
func (r *Registry) Len() int {
	n := 0
	r.m.Range(func(_, _ any) bool { n++; return true })
	return n
}
