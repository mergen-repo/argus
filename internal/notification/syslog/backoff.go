package syslog

import "time"

// Backoff tracks consecutive failures and returns the next delay per AC-8.
// Sequence: 1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, ... (capped at 60s).
// Zero value is safe to use without initialization.
type Backoff struct {
	consecutiveFailures int
}

// Next returns the delay for the current failure count, then increments the
// counter. Sequence is 1<<min(N,6) seconds, capped at 60s.
func (b *Backoff) Next() time.Duration {
	n := b.consecutiveFailures
	b.consecutiveFailures++
	shift := n
	if shift > 6 {
		shift = 6
	}
	secs := 1 << uint(shift)
	if secs > 60 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

// Reset clears the failure counter, e.g. after a successful send.
func (b *Backoff) Reset() {
	b.consecutiveFailures = 0
}
