package killswitch

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const defaultTTL = 15 * time.Second

// Service maintains an in-memory cache of kill-switch flags refreshed every TTL.
// Callers use IsEnabled(key) — it never blocks and defaults to false (safe) on
// cache miss or unknown key.
type Service struct {
	store    *store.KillSwitchStore
	auditSvc audit.Auditor
	logger   zerolog.Logger

	mu       sync.RWMutex
	cache    map[string]bool
	cachedAt time.Time
	ttl      time.Duration
}

func NewService(ks *store.KillSwitchStore, auditSvc audit.Auditor, logger zerolog.Logger) *Service {
	return &Service{
		store:    ks,
		auditSvc: auditSvc,
		logger:   logger.With().Str("component", "killswitch").Logger(),
		cache:    map[string]bool{},
		ttl:      defaultTTL,
	}
}

// IsEnabled returns true if the named kill switch is currently active.
// Unknown keys return false (safe default — don't accidentally block).
func (s *Service) IsEnabled(key string) bool {
	s.mu.RLock()
	if time.Since(s.cachedAt) < s.ttl {
		v := s.cache[key]
		s.mu.RUnlock()
		return v
	}
	s.mu.RUnlock()

	// Cache stale — reload in background; return last known value without blocking.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Reload(ctx); err != nil {
			s.logger.Warn().Err(err).Msg("kill switch cache reload failed")
		}
	}()

	s.mu.RLock()
	v := s.cache[key]
	s.mu.RUnlock()
	return v
}

// Reload fetches all flags from the DB and refreshes the in-memory cache.
func (s *Service) Reload(ctx context.Context) error {
	switches, err := s.store.List(ctx)
	if err != nil {
		return err
	}

	newCache := make(map[string]bool, len(switches))
	for _, ks := range switches {
		newCache[ks.Key] = ks.Enabled
	}

	s.mu.Lock()
	s.cache = newCache
	s.cachedAt = time.Now()
	s.mu.Unlock()
	return nil
}

// GetAll returns a snapshot of all cached flags.
func (s *Service) GetAll() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// Toggle persists the toggle and emits an audit entry, then refreshes the cache.
func (s *Service) Toggle(ctx context.Context, key string, enabled bool, reason *string, actorUserID *uuid.UUID, tenantID uuid.UUID) (*store.KillSwitch, error) {
	ks, err := s.store.Toggle(ctx, key, enabled, reason, actorUserID)
	if err != nil {
		return nil, err
	}

	reasonStr := ""
	if reason != nil {
		reasonStr = *reason
	}

	if s.auditSvc != nil {
		afterData, _ := json.Marshal(map[string]interface{}{"enabled": enabled, "reason": reasonStr})
		_, _ = s.auditSvc.CreateEntry(ctx, audit.CreateEntryParams{
			TenantID:   tenantID,
			UserID:     actorUserID,
			Action:     "killswitch.toggled",
			EntityType: "kill_switch",
			EntityID:   key,
			AfterData:  afterData,
		})
	}

	_ = s.Reload(ctx)
	return ks, nil
}
