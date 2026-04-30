package gateway

import (
	"net/http"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

type BulkRateLimiter struct {
	rps      float64
	burst    int
	mu       sync.Mutex
	limiters map[uuid.UUID]*limiterEntry
	stop     chan struct{}
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewBulkRateLimiter(rps float64, burst int) *BulkRateLimiter {
	m := &BulkRateLimiter{
		rps:      rps,
		burst:    burst,
		limiters: make(map[uuid.UUID]*limiterEntry),
		stop:     make(chan struct{}),
	}
	go m.cleanup()
	return m
}

func (m *BulkRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
			if !ok || tenantID == uuid.Nil {
				next.ServeHTTP(w, r)
				return
			}

			lim := m.getLimiter(tenantID)
			if !lim.Allow() {
				w.Header().Set("Retry-After", "1")
				apierr.WriteError(w, http.StatusTooManyRequests, apierr.CodeRateLimited,
					"bulk operation rate limit exceeded (1 req/sec per tenant)")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (m *BulkRateLimiter) Shutdown() {
	close(m.stop)
}

func (m *BulkRateLimiter) getLimiter(id uuid.UUID) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.limiters[id]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(rate.Limit(m.rps), m.burst)}
		m.limiters[id] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

func (m *BulkRateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-5 * time.Minute)
			m.mu.Lock()
			for id, e := range m.limiters {
				if e.lastSeen.Before(cutoff) {
					delete(m.limiters, id)
				}
			}
			m.mu.Unlock()
		}
	}
}
