package sba

import (
	"crypto/sha256"
	"encoding/binary"
)

// SessionContext carries the minimal fields the picker needs to make a
// deterministic protocol selection. The engine passes the AcctSessionID from
// the RADIUS session context so the picker does not depend on the full
// radius.SessionContext type.
type SessionContext struct {
	AcctSessionID string
}

// ShouldUseSBA reports whether the given session should use 5G-SBA instead of
// RADIUS for the supplied operator rate.
//
// Selection is deterministic for a given AcctSessionID: the same session always
// lands in the same bucket. The algorithm hashes the AcctSessionID with SHA-256,
// reads the first 8 bytes as a big-endian uint64, takes the value modulo 10 000,
// and compares against rate*10 000. This gives a stable, uniformly-distributed
// assignment with no per-call state.
//
// Edge cases:
//   - rate == 0.0: never returns true.
//   - rate == 1.0: always returns true.
//   - rate outside [0,1]: clamp behaviour — values ≤ 0 return false, ≥ 1 return true.
func ShouldUseSBA(sc SessionContext, rate float64) bool {
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	h := sha256.Sum256([]byte(sc.AcctSessionID))
	bucket := binary.BigEndian.Uint64(h[:8]) % 10_000
	return bucket < uint64(rate*10_000)
}
