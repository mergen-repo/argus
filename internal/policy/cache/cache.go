package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type PolicyLoader interface {
	ListActiveVersions(ctx context.Context) ([]PolicyVersionRow, error)
	GetCompiledByVersionID(ctx context.Context, versionID uuid.UUID) (*dsl.CompiledPolicy, error)
}

type PolicyVersionRow struct {
	VersionID uuid.UUID
	PolicyID  uuid.UUID
	TenantID  uuid.UUID
	Compiled  json.RawMessage
}

type EventSubscriber interface {
	Subscribe(subject string, handler func(subject string, data []byte)) (Subscription, error)
}

type Subscription interface {
	Unsubscribe() error
}

type entry struct {
	policy   *dsl.CompiledPolicy
	policyID uuid.UUID
	tenantID uuid.UUID
	loadedAt time.Time
}

type Cache struct {
	mu      sync.RWMutex
	entries map[uuid.UUID]*entry // versionID -> entry
	byTen   map[uuid.UUID][]uuid.UUID // tenantID -> []versionID

	loader PolicyLoader
	logger zerolog.Logger

	sub    Subscription
	closed chan struct{}

	stats Stats
}

type Stats struct {
	mu          sync.Mutex
	Hits        int64
	Misses      int64
	Evictions   int64
	Refreshes   int64
	EntryCount  int
	LastRefresh time.Time
}

func (s *Stats) snapshot() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{
		Hits:        s.Hits,
		Misses:      s.Misses,
		Evictions:   s.Evictions,
		Refreshes:   s.Refreshes,
		EntryCount:  s.EntryCount,
		LastRefresh: s.LastRefresh,
	}
}

func New(loader PolicyLoader, logger zerolog.Logger) *Cache {
	return &Cache{
		entries: make(map[uuid.UUID]*entry),
		byTen:   make(map[uuid.UUID][]uuid.UUID),
		loader:  loader,
		logger:  logger.With().Str("component", "policy_cache").Logger(),
		closed:  make(chan struct{}),
	}
}

func (c *Cache) WarmUp(ctx context.Context) error {
	if c.loader == nil {
		return nil
	}

	rows, err := c.loader.ListActiveVersions(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	loaded := 0
	for _, row := range rows {
		var cp dsl.CompiledPolicy
		if err := json.Unmarshal(row.Compiled, &cp); err != nil {
			c.logger.Warn().
				Str("version_id", row.VersionID.String()).
				Err(err).
				Msg("failed to unmarshal compiled policy during warm-up")
			continue
		}

		c.entries[row.VersionID] = &entry{
			policy:   &cp,
			policyID: row.PolicyID,
			tenantID: row.TenantID,
			loadedAt: time.Now(),
		}
		c.byTen[row.TenantID] = append(c.byTen[row.TenantID], row.VersionID)
		loaded++
	}

	c.stats.mu.Lock()
	c.stats.EntryCount = loaded
	c.stats.LastRefresh = time.Now()
	c.stats.mu.Unlock()

	c.logger.Info().Int("loaded", loaded).Msg("policy cache warm-up complete")
	return nil
}

func (c *Cache) SubscribeInvalidation(sub EventSubscriber, subject string) error {
	s, err := sub.Subscribe(subject, func(_ string, data []byte) {
		c.handleInvalidation(data)
	})
	if err != nil {
		return err
	}
	c.sub = s
	return nil
}

func (c *Cache) handleInvalidation(data []byte) {
	var evt struct {
		PolicyID  string `json:"policy_id"`
		VersionID string `json:"version_id"`
		TenantID  string `json:"tenant_id"`
		Action    string `json:"action"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		c.logger.Warn().Err(err).Msg("failed to decode policy invalidation event")
		return
	}

	if evt.VersionID != "" {
		vid, err := uuid.Parse(evt.VersionID)
		if err == nil {
			c.Invalidate(vid)
		}
	}

	if evt.Action == "refresh" && c.loader != nil {
		if evt.VersionID != "" {
			vid, err := uuid.Parse(evt.VersionID)
			if err == nil {
				c.refreshVersion(vid)
			}
		}
	}
}

func (c *Cache) refreshVersion(versionID uuid.UUID) {
	if c.loader == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cp, err := c.loader.GetCompiledByVersionID(ctx, versionID)
	if err != nil {
		c.logger.Warn().Err(err).Str("version_id", versionID.String()).Msg("failed to refresh policy version")
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.entries[versionID]; ok {
		existing.policy = cp
		existing.loadedAt = time.Now()
	}

	c.stats.mu.Lock()
	c.stats.Refreshes++
	c.stats.LastRefresh = time.Now()
	c.stats.mu.Unlock()
}

func (c *Cache) Get(versionID uuid.UUID) (*dsl.CompiledPolicy, bool) {
	c.mu.RLock()
	e, ok := c.entries[versionID]
	c.mu.RUnlock()

	if ok {
		c.stats.mu.Lock()
		c.stats.Hits++
		c.stats.mu.Unlock()
		return e.policy, true
	}

	c.stats.mu.Lock()
	c.stats.Misses++
	c.stats.mu.Unlock()
	return nil, false
}

func (c *Cache) Put(versionID, policyID, tenantID uuid.UUID, cp *dsl.CompiledPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[versionID] = &entry{
		policy:   cp,
		policyID: policyID,
		tenantID: tenantID,
		loadedAt: time.Now(),
	}

	found := false
	for _, vid := range c.byTen[tenantID] {
		if vid == versionID {
			found = true
			break
		}
	}
	if !found {
		c.byTen[tenantID] = append(c.byTen[tenantID], versionID)
	}

	c.stats.mu.Lock()
	c.stats.EntryCount = len(c.entries)
	c.stats.mu.Unlock()
}

func (c *Cache) Invalidate(versionID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[versionID]; ok {
		vids := c.byTen[e.tenantID]
		for i, vid := range vids {
			if vid == versionID {
				c.byTen[e.tenantID] = append(vids[:i], vids[i+1:]...)
				break
			}
		}
		delete(c.entries, versionID)

		c.stats.mu.Lock()
		c.stats.Evictions++
		c.stats.EntryCount = len(c.entries)
		c.stats.mu.Unlock()

		c.logger.Debug().Str("version_id", versionID.String()).Msg("policy cache entry invalidated")
	}
}

func (c *Cache) GetForTenant(tenantID uuid.UUID) []*dsl.CompiledPolicy {
	c.mu.RLock()
	defer c.mu.RUnlock()

	vids := c.byTen[tenantID]
	result := make([]*dsl.CompiledPolicy, 0, len(vids))
	for _, vid := range vids {
		if e, ok := c.entries[vid]; ok {
			result = append(result, e.policy)
		}
	}
	return result
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

func (c *Cache) GetStats() Stats {
	return c.stats.snapshot()
}

func (c *Cache) Stop() {
	select {
	case <-c.closed:
		return
	default:
	}
	close(c.closed)

	if c.sub != nil {
		_ = c.sub.Unsubscribe()
	}
}
