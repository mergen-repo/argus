package killswitch

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// osGetenvFn is the underlying env lookup — swapped in tests.
var osGetenvFn = os.Getenv

const defaultTTL = 30 * time.Second

// enabledByDefault maps internal key → whether the kill switch is ACTIVE by
// default. true = switch is ON by default (blocks/suppresses the operation).
// All traffic switches default to OFF (false) — feature permitted.
// read_only_mode defaults to OFF — mutations allowed.
var enabledByDefault = map[string]bool{
	"radius_auth":            false,
	"session_create":         false,
	"bulk_operations":        false,
	"external_notifications": false,
	"read_only_mode":         false,
}

type cacheEntry struct {
	value     bool
	expiresAt time.Time
}

// Service is an env-backed kill-switch service with a per-key TTL cache.
// IsEnabled(key) returns true when the kill switch is ACTIVE (blocks/suppresses
// the operation), mirroring the original DB-backed semantic.
type Service struct {
	cache  map[string]cacheEntry
	mu     sync.RWMutex
	ttl    time.Duration
	clock  func() time.Time
	getenv func(string) string
	log    zerolog.Logger

	warnedUnknown map[string]bool
	warnMu        sync.Mutex
}

// NewService creates an env-backed kill-switch service with a 30s TTL cache.
func NewService(log zerolog.Logger) *Service {
	return &Service{
		cache:         make(map[string]cacheEntry),
		ttl:           defaultTTL,
		clock:         time.Now,
		getenv:        osGetenv,
		log:           log.With().Str("component", "killswitch").Logger(),
		warnedUnknown: make(map[string]bool),
	}
}

// osGetenv wraps os.Getenv so it can be swapped in tests.
func osGetenv(key string) string {
	return osGetenvFn(key)
}

// IsEnabled returns true if the named kill switch is currently ACTIVE
// (i.e. the operation/feature should be blocked or suppressed).
// Unknown keys: log Warn once and return false (never block unknown keys).
func (s *Service) IsEnabled(key string) bool {
	now := s.clock()

	// Fast path: RLock, check cache entry validity.
	s.mu.RLock()
	if entry, ok := s.cache[key]; ok && now.Before(entry.expiresAt) {
		v := entry.value
		s.mu.RUnlock()
		return v
	}
	s.mu.RUnlock()

	// Slow path: acquire write lock, double-check, then resolve from env.
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring lock.
	if entry, ok := s.cache[key]; ok && now.Before(entry.expiresAt) {
		return entry.value
	}

	value := s.resolveFromEnv(key)
	s.cache[key] = cacheEntry{
		value:     value,
		expiresAt: now.Add(s.ttl),
	}
	return value
}

// resolveFromEnv reads the env var for key and returns the kill-switch state.
// Must be called with s.mu held (write lock).
func (s *Service) resolveFromEnv(key string) bool {
	envKey := "KILLSWITCH_" + strings.ToUpper(key)
	raw := strings.TrimSpace(strings.ToLower(s.getenv(envKey)))

	defaultVal, known := enabledByDefault[key]
	if !known {
		s.warnMu.Lock()
		if !s.warnedUnknown[key] {
			s.warnedUnknown[key] = true
			s.log.Warn().Str("key", key).Msg("killswitch: unknown key — defaulting to false (permit)")
		}
		s.warnMu.Unlock()
		return false
	}

	switch raw {
	case "":
		return defaultVal
	case "on", "true", "1":
		return true
	case "off", "false", "0":
		return false
	default:
		s.log.Warn().
			Str("key", key).
			Str("env_key", envKey).
			Str("value", raw).
			Msg("killswitch: unrecognised env value — using default")
		return defaultVal
	}
}
