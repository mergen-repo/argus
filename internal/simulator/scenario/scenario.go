// Package scenario provides a weighted-random picker and per-session
// parameter generator. The RNG is seedable for deterministic test runs.
package scenario

import (
	"math/rand"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/simulator/config"
)

// Sample represents the per-session runtime instance of a chosen scenario.
type Sample struct {
	Name                  string
	SessionDuration       time.Duration
	InterimInterval       time.Duration
	BytesPerInterimIn     int
	BytesPerInterimOut    int
}

// Picker wraps a *rand.Rand guarded by a mutex (thread-safe for the engine
// calling Pick() from multiple session goroutines). Weights are
// accumulated into a cumulative distribution at construction.
type Picker struct {
	mu       sync.Mutex
	rng      *rand.Rand
	scenarios []config.ScenarioConfig
	cumulative []float64
	totalWeight float64
}

// New constructs a Picker. If seed == 0, the RNG is seeded from wall time.
func New(scenarios []config.ScenarioConfig, seed int64) *Picker {
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	p := &Picker{
		rng:       rand.New(rand.NewSource(seed)),
		scenarios: scenarios,
	}
	for _, s := range scenarios {
		p.totalWeight += s.Weight
		p.cumulative = append(p.cumulative, p.totalWeight)
	}
	return p
}

// Pick draws a scenario and produces a concrete Sample with the runtime
// values (session duration, byte counts) sampled within their configured
// ranges.
func (p *Picker) Pick() Sample {
	p.mu.Lock()
	defer p.mu.Unlock()

	r := p.rng.Float64() * p.totalWeight
	idx := 0
	for i, c := range p.cumulative {
		if r <= c {
			idx = i
			break
		}
	}
	s := p.scenarios[idx]
	return Sample{
		Name:               s.Name,
		SessionDuration:    randDuration(p.rng, s.SessionDurationSeconds[0], s.SessionDurationSeconds[1]),
		InterimInterval:    time.Duration(s.InterimIntervalSeconds) * time.Second,
		BytesPerInterimIn:  randInt(p.rng, s.BytesPerInterimInRange[0], s.BytesPerInterimInRange[1]),
		BytesPerInterimOut: randInt(p.rng, s.BytesPerInterimOutRange[0], s.BytesPerInterimOutRange[1]),
	}
}

// JitterDuration returns a random [lo, hi]-second duration — used by the
// engine to spread goroutine starts and add ±jitter to interim intervals.
func (p *Picker) JitterDuration(loSec, hiSec int) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if hiSec <= loSec {
		return time.Duration(loSec) * time.Second
	}
	return time.Duration(loSec+p.rng.Intn(hiSec-loSec+1)) * time.Second
}

func randDuration(r *rand.Rand, loSec, hiSec int) time.Duration {
	if hiSec <= loSec {
		return time.Duration(loSec) * time.Second
	}
	return time.Duration(loSec+r.Intn(hiSec-loSec+1)) * time.Second
}

func randInt(r *rand.Rand, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + r.Intn(hi-lo+1)
}
