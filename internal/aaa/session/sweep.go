package session

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	sweepInterval  = 60 * time.Second
	sweepBatchSize = 200
)

type TimeoutSweeper struct {
	manager  *Manager
	dm       *DMSender
	eventBus *bus.EventBus
	redis    *redis.Client
	logger   zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

func NewTimeoutSweeper(
	manager *Manager,
	dm *DMSender,
	eventBus *bus.EventBus,
	redisClient *redis.Client,
	logger zerolog.Logger,
) *TimeoutSweeper {
	return &TimeoutSweeper{
		manager:  manager,
		dm:       dm,
		eventBus: eventBus,
		redis:    redisClient,
		logger:   logger.With().Str("component", "session_sweeper").Logger(),
		stopCh:   make(chan struct{}),
	}
}

func (s *TimeoutSweeper) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}
	s.running = true

	go s.run()
	s.logger.Info().Dur("interval", sweepInterval).Msg("session timeout sweeper started")
}

func (s *TimeoutSweeper) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
	s.logger.Info().Msg("session timeout sweeper stopped")
}

func (s *TimeoutSweeper) run() {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.sweep()
		}
	}
}

func (s *TimeoutSweeper) sweep() {
	ctx := context.Background()
	now := time.Now().UTC()

	var redisCursor uint64
	var idleCount, hardCount int

	for {
		keys, nextCursor, err := s.redis.Scan(ctx, redisCursor, sessionKeyPrefix+"*", sweepBatchSize).Result()
		if err != nil {
			s.logger.Error().Err(err).Msg("sweep scan error")
			return
		}

		for _, key := range keys {
			if !isSessionDataKey(key) {
				continue
			}

			data, err := s.redis.Get(ctx, key).Bytes()
			if err != nil {
				continue
			}

			var sess Session
			if err := json.Unmarshal(data, &sess); err != nil {
				continue
			}

			if sess.SessionState != "active" {
				continue
			}

			idleTimeout := sess.IdleTimeout
			if idleTimeout <= 0 {
				idleTimeout = defaultIdleTimeout
			}
			hardTimeout := sess.SessionTimeout
			if hardTimeout <= 0 {
				hardTimeout = defaultHardTimeout
			}

			if s.checkHardTimeout(&sess, now, hardTimeout) {
				hardCount++
				continue
			}

			if s.checkIdleTimeout(&sess, now, idleTimeout) {
				idleCount++
			}
		}

		redisCursor = nextCursor
		if redisCursor == 0 {
			break
		}
	}

	if idleCount > 0 || hardCount > 0 {
		s.logger.Info().
			Int("idle_timeouts", idleCount).
			Int("hard_timeouts", hardCount).
			Msg("sweep completed")
	}
}

func (s *TimeoutSweeper) checkIdleTimeout(sess *Session, now time.Time, idleTimeoutSec int) bool {
	lastActivity := sess.LastInterimAt
	if lastActivity.IsZero() {
		lastActivity = sess.StartedAt
	}

	deadline := lastActivity.Add(time.Duration(idleTimeoutSec) * time.Second)
	if now.Before(deadline) {
		return false
	}

	s.disconnectSession(sess, "idle_timeout")
	return true
}

func (s *TimeoutSweeper) checkHardTimeout(sess *Session, now time.Time, hardTimeoutSec int) bool {
	deadline := sess.StartedAt.Add(time.Duration(hardTimeoutSec) * time.Second)
	if now.Before(deadline) {
		return false
	}

	s.disconnectSession(sess, "session_timeout")
	return true
}

func (s *TimeoutSweeper) disconnectSession(sess *Session, cause string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.dm != nil && sess.NASIP != "" && sess.AcctSessionID != "" {
		nasIP := sess.NASIP
		if idx := strings.Index(nasIP, ":"); idx > 0 {
			nasIP = nasIP[:idx]
		}
		tenantID, _ := uuid.Parse(sess.TenantID)
		_, err := s.dm.SendDM(ctx, DMRequest{
			NASIP:         nasIP,
			AcctSessionID: sess.AcctSessionID,
			IMSI:          sess.IMSI,
			SessionID:     sess.ID,
			TenantID:      tenantID,
		})
		if err != nil {
			s.logger.Warn().Err(err).
				Str("session_id", sess.ID).
				Str("cause", cause).
				Msg("DM send failed during sweep")
		}
	}

	if err := s.manager.Terminate(ctx, sess.ID, cause); err != nil {
		s.logger.Error().Err(err).
			Str("session_id", sess.ID).
			Msg("terminate session failed during sweep")
		return
	}

	s.publishSessionEnded(ctx, sess, cause)
}

func (s *TimeoutSweeper) publishSessionEnded(ctx context.Context, sess *Session, cause string) {
	if s.eventBus == nil {
		return
	}

	env := bus.NewSessionEnvelope("session.ended", sess.TenantID, sess.SimID, sess.ICCID, "Session ended (sweep)").
		WithMeta("session_id", sess.ID).
		WithMeta("operator_id", sess.OperatorID).
		WithMeta("imsi", sess.IMSI).
		WithMeta("termination_cause", cause).
		WithMeta("ended_at", time.Now().UTC().Format(time.RFC3339))

	if err := s.eventBus.Publish(ctx, bus.SubjectSessionEnded, env); err != nil {
		s.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("publish session.ended event failed")
	}
}
