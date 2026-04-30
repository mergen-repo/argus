package eap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	eapSessionKeyPrefix = "eap:session:"
	eapSessionTTL       = 30 * time.Second
)

type RedisStateStore struct {
	client *redis.Client
	logger zerolog.Logger
	ttl    time.Duration
}

func NewRedisStateStore(client *redis.Client, logger zerolog.Logger) *RedisStateStore {
	return &RedisStateStore{
		client: client,
		logger: logger.With().Str("component", "eap_redis_store").Logger(),
		ttl:    eapSessionTTL,
	}
}

func (s *RedisStateStore) Save(ctx context.Context, session *EAPSession) error {
	key := eapSessionKeyPrefix + session.ID

	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("eap redis store: marshal session: %w", err)
	}

	if err := s.client.Set(ctx, key, data, s.ttl).Err(); err != nil {
		return fmt.Errorf("eap redis store: save session %s: %w", session.ID, err)
	}

	s.logger.Debug().
		Str("session_id", session.ID).
		Str("state", string(session.State)).
		Dur("ttl", s.ttl).
		Msg("EAP session saved to Redis")

	return nil
}

func (s *RedisStateStore) Get(ctx context.Context, id string) (*EAPSession, error) {
	key := eapSessionKeyPrefix + id

	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("eap redis store: get session %s: %w", id, err)
	}

	var session EAPSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("eap redis store: unmarshal session %s: %w", id, err)
	}

	return &session, nil
}

func (s *RedisStateStore) Delete(ctx context.Context, id string) error {
	key := eapSessionKeyPrefix + id

	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("eap redis store: delete session %s: %w", id, err)
	}

	s.logger.Debug().
		Str("session_id", id).
		Msg("EAP session deleted from Redis")

	return nil
}

func (s *RedisStateStore) GetAndDelete(ctx context.Context, id string) (*EAPSession, error) {
	key := eapSessionKeyPrefix + id

	data, err := s.client.GetDel(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("eap redis store: getdel session %s: %w", id, err)
	}

	var session EAPSession
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("eap redis store: unmarshal session %s: %w", id, err)
	}

	s.logger.Debug().
		Str("session_id", id).
		Msg("EAP session atomically consumed from Redis")

	return &session, nil
}
