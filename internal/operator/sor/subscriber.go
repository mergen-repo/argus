package sor

import (
	"context"
	"encoding/json"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/operator"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

type SoRSubscriber struct {
	engine *Engine
	cache  *SoRCache
	logger zerolog.Logger
}

func NewSoRSubscriber(engine *Engine, cache *SoRCache, logger zerolog.Logger) *SoRSubscriber {
	return &SoRSubscriber{
		engine: engine,
		cache:  cache,
		logger: logger.With().Str("component", "sor_subscriber").Logger(),
	}
}

func (s *SoRSubscriber) SubscribeHealthEvents(eventBus *bus.EventBus) (*nats.Subscription, error) {
	return eventBus.QueueSubscribe(bus.SubjectOperatorHealthChanged, "sor_invalidation", func(subject string, data []byte) {
		var evt operator.OperatorHealthEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			s.logger.Error().Err(err).Msg("SoR: unmarshal health event")
			return
		}

		shouldInvalidate := false
		switch {
		case evt.CurrentStatus == "down":
			shouldInvalidate = true
			s.logger.Info().
				Str("operator_id", evt.OperatorID.String()).
				Str("operator_name", evt.OperatorName).
				Msg("SoR: operator down, invalidating cached decisions")
		case evt.PreviousStatus == "down" && (evt.CurrentStatus == "healthy" || evt.CurrentStatus == "degraded"):
			shouldInvalidate = true
			s.logger.Info().
				Str("operator_id", evt.OperatorID.String()).
				Str("operator_name", evt.OperatorName).
				Str("new_status", evt.CurrentStatus).
				Msg("SoR: operator recovered, invalidating cached decisions for re-evaluation")
		}

		if !shouldInvalidate {
			return
		}

		ctx := context.Background()
		s.invalidateForOperator(ctx, evt.OperatorID)
	})
}

func (s *SoRSubscriber) SubscribeCacheInvalidation(eventBus *bus.EventBus) (*nats.Subscription, error) {
	return eventBus.QueueSubscribe(bus.SubjectCacheInvalidate, "sor_cache_invalidation", func(subject string, data []byte) {
		var msg struct {
			Type      string    `json:"type"`
			TenantID  uuid.UUID `json:"tenant_id"`
			OperatorID uuid.UUID `json:"operator_id,omitempty"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			s.logger.Error().Err(err).Msg("SoR: unmarshal cache invalidation")
			return
		}

		if msg.Type != "sor" {
			return
		}

		ctx := context.Background()
		if msg.OperatorID != uuid.Nil {
			s.invalidateForOperator(ctx, msg.OperatorID)
		} else if msg.TenantID != uuid.Nil {
			if err := s.cache.DeleteAllForTenant(ctx, msg.TenantID); err != nil {
				s.logger.Error().Err(err).
					Str("tenant_id", msg.TenantID.String()).
					Msg("SoR: tenant cache invalidation failed")
			}
		}
	})
}

func (s *SoRSubscriber) invalidateForOperator(ctx context.Context, operatorID uuid.UUID) {
	if s.cache == nil || s.cache.client == nil {
		return
	}

	var cursor uint64
	pattern := "sor:result:*"
	deleted := 0

	for {
		keys, nextCursor, err := s.cache.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			s.logger.Error().Err(err).Msg("SoR: scan for operator invalidation")
			return
		}

		opStr := operatorID.String()
		for _, key := range keys {
			data, err := s.cache.client.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}
			if containsOperatorID(data, opStr) {
				s.cache.client.Del(ctx, key)
				deleted++
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	s.logger.Info().
		Str("operator_id", operatorID.String()).
		Int("deleted_keys", deleted).
		Msg("SoR: operator cache invalidation complete")
}

func containsOperatorID(data []byte, opID string) bool {
	var decision SoRDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		return false
	}
	if decision.PrimaryOperatorID.String() == opID {
		return true
	}
	for _, fb := range decision.FallbackOperatorIDs {
		if fb.String() == opID {
			return true
		}
	}
	return false
}
