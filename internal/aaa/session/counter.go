package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	sessionCountKeyPrefix = "sessions:active:count:"
	sessionCountTTL       = 2 * time.Hour
	reconcileInterval     = 1 * time.Hour
)

type SessionStore interface {
	ListActiveTenantCounts(ctx context.Context) (map[string]int64, error)
}

type Counter struct {
	rc     *redis.Client
	store  SessionStore
	logger zerolog.Logger
}

func RegisterSessionCounter(eb *bus.EventBus, rc *redis.Client, s SessionStore, logger zerolog.Logger) (*Counter, error) {
	c := &Counter{
		rc:     rc,
		store:  s,
		logger: logger.With().Str("component", "session_counter").Logger(),
	}

	if _, err := eb.QueueSubscribeCtx(bus.SubjectSessionStarted, "session-counter", func(ctx context.Context, _ string, data []byte) {
		var payload struct {
			TenantID string `json:"tenant_id"`
		}
		if err := json.Unmarshal(data, &payload); err != nil || payload.TenantID == "" {
			c.logger.Debug().Msg("session counter: no tenant_id in started event, skipping")
			return
		}
		key := sessionCountKeyPrefix + payload.TenantID
		if err := rc.Incr(ctx, key).Err(); err != nil {
			c.logger.Warn().Err(err).Str("key", key).Msg("session counter: INCR failed")
			return
		}
		rc.Expire(ctx, key, sessionCountTTL)
	}); err != nil {
		return nil, fmt.Errorf("session counter: subscribe started: %w", err)
	}

	if _, err := eb.QueueSubscribeCtx(bus.SubjectSessionEnded, "session-counter", func(ctx context.Context, _ string, data []byte) {
		var payload struct {
			TenantID string `json:"tenant_id"`
		}
		if err := json.Unmarshal(data, &payload); err != nil || payload.TenantID == "" {
			c.logger.Debug().Msg("session counter: no tenant_id in ended event, skipping")
			return
		}
		key := sessionCountKeyPrefix + payload.TenantID
		val, err := rc.Decr(ctx, key).Result()
		if err != nil {
			c.logger.Warn().Err(err).Str("key", key).Msg("session counter: DECR failed")
			return
		}
		if val < 0 {
			rc.Set(ctx, key, 0, sessionCountTTL)
		} else {
			rc.Expire(ctx, key, sessionCountTTL)
		}
	}); err != nil {
		return nil, fmt.Errorf("session counter: subscribe ended: %w", err)
	}

	return c, nil
}

func (c *Counter) GetActiveCount(ctx context.Context, tenantID string) (int64, error) {
	val, err := c.rc.Get(ctx, sessionCountKeyPrefix+tenantID).Int64()
	if err == redis.Nil {
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	return val, nil
}

func (c *Counter) Reconcile(ctx context.Context) error {
	counts, err := c.store.ListActiveTenantCounts(ctx)
	if err != nil {
		return fmt.Errorf("session counter reconcile: %w", err)
	}
	for tenantID, count := range counts {
		key := sessionCountKeyPrefix + tenantID
		if err := c.rc.Set(ctx, key, count, sessionCountTTL).Err(); err != nil {
			c.logger.Warn().Err(err).Str("tenant_id", tenantID).Msg("session counter: reconcile SET failed")
		}
	}
	c.logger.Info().Int("tenants", len(counts)).Msg("session counter reconciled")
	return nil
}

func (c *Counter) Start(ctx context.Context) {
	go func() {
		if err := c.Reconcile(ctx); err != nil {
			c.logger.Warn().Err(err).Msg("session counter: initial reconcile failed")
		}
		ticker := time.NewTicker(reconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Reconcile(ctx); err != nil {
					c.logger.Warn().Err(err).Msg("session counter: periodic reconcile failed")
				}
			}
		}
	}()
}
