package reactive

import (
	"sync"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
)

// RejectTracker records Access-Reject attempts per (operator, imsi) key
// and computes exponential back-off with a sliding 1-hour window cap.
// Thread-safe.
type RejectTracker struct {
	mu          sync.Mutex
	attempts    map[string]*attemptRecord
	baseBackoff time.Duration
	maxBackoff  time.Duration
	maxPerHour  int
	now         func() time.Time
}

type attemptRecord struct {
	count       int
	windowStart time.Time
	nextAllowed time.Time
}

func NewRejectTracker(cfg config.ReactiveDefaults) *RejectTracker {
	return &RejectTracker{
		attempts:    make(map[string]*attemptRecord),
		baseBackoff: cfg.RejectBackoffBase,
		maxBackoff:  cfg.RejectBackoffMax,
		maxPerHour:  cfg.RejectMaxRetriesPerHour,
		now:         time.Now,
	}
}

// Allowed reports whether an auth attempt is permitted at the current time.
// Returns false when the caller is in a back-off sleep or suspended.
// Used BEFORE sending Access-Request to skip suspended SIMs entirely.
func (t *RejectTracker) Allowed(op, imsi string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.attempts[key(op, imsi)]
	if !ok {
		return true
	}
	if t.now().Sub(r.windowStart) >= time.Hour {
		return true
	}
	return !t.now().Before(r.nextAllowed)
}

// NextBackoff records a fresh Access-Reject and returns the sleep duration
// the caller should wait before retrying, plus a suspended flag that means
// retries are capped for the remainder of the 1h window.
//
// Behaviour:
//   - attempt n (starting at 1): sleep = min(base * 2^(n-1), max)
//   - after maxPerHour attempts: sleep = remaining-window; suspended=true
//
// Window slides: a reject more than 1h after the window start resets the count.
func (t *RejectTracker) NextBackoff(op, imsi string) (wait time.Duration, suspended bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(op, imsi)
	now := t.now()
	r, ok := t.attempts[k]
	if !ok || now.Sub(r.windowStart) >= time.Hour {
		r = &attemptRecord{count: 1, windowStart: now}
		t.attempts[k] = r
	} else {
		r.count++
	}

	if r.count > t.maxPerHour {
		wait = time.Hour - now.Sub(r.windowStart)
		if wait < 0 {
			wait = 0
		}
		suspended = true
		r.nextAllowed = now.Add(wait)
		return
	}

	mult := time.Duration(1) << uint(r.count-1)
	wait = t.baseBackoff * mult
	if wait > t.maxBackoff {
		wait = t.maxBackoff
	}
	r.nextAllowed = now.Add(wait)
	return
}

// Reset clears tracker state for (op, imsi) — called on Access-Accept.
func (t *RejectTracker) Reset(op, imsi string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.attempts, key(op, imsi))
}

func key(op, imsi string) string { return op + "|" + imsi }
