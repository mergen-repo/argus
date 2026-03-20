package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/crypto"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type CachedHealth struct {
	Status       string `json:"status"`
	LatencyMs    int    `json:"latency_ms"`
	CircuitState string `json:"circuit_state"`
	CheckedAt    string `json:"checked_at"`
}

type HealthChecker struct {
	store         *store.OperatorStore
	registry      *adapter.Registry
	redisClient   *redis.Client
	encryptionKey string
	logger        zerolog.Logger

	mu       sync.Mutex
	breakers map[uuid.UUID]*CircuitBreaker
	stopChs  map[uuid.UUID]chan struct{}
	wg       sync.WaitGroup
	stopped  bool
}

func NewHealthChecker(
	opStore *store.OperatorStore,
	registry *adapter.Registry,
	redisClient *redis.Client,
	encryptionKey string,
	logger zerolog.Logger,
) *HealthChecker {
	return &HealthChecker{
		store:         opStore,
		registry:      registry,
		redisClient:   redisClient,
		encryptionKey: encryptionKey,
		logger:        logger.With().Str("component", "health_checker").Logger(),
		breakers:      make(map[uuid.UUID]*CircuitBreaker),
		stopChs:       make(map[uuid.UUID]chan struct{}),
	}
}

func (hc *HealthChecker) Start(ctx context.Context) error {
	operators, err := hc.store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("health checker: list operators: %w", err)
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	for _, op := range operators {
		hc.startOperatorLoop(op)
	}

	hc.logger.Info().Int("operator_count", len(operators)).Msg("health checker started")
	return nil
}

func (hc *HealthChecker) startOperatorLoop(op store.Operator) {
	cb := NewCircuitBreaker(op.CircuitBreakerThreshold, op.CircuitBreakerRecoverySec)
	hc.breakers[op.ID] = cb

	stopCh := make(chan struct{})
	hc.stopChs[op.ID] = stopCh

	interval := time.Duration(op.HealthCheckIntervalSec) * time.Second
	if interval < time.Second {
		interval = 30 * time.Second
	}

	adapterConfig := op.AdapterConfig
	if hc.encryptionKey != "" {
		if decrypted, err := crypto.DecryptJSON(adapterConfig, hc.encryptionKey); err == nil {
			adapterConfig = decrypted
		}
	}

	hc.wg.Add(1)
	go func(opID uuid.UUID, adapterType string, cfg json.RawMessage, tick time.Duration, intSec int) {
		defer hc.wg.Done()

		ticker := time.NewTicker(tick)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				hc.checkOperator(opID, adapterType, cfg, cb, intSec)
			}
		}
	}(op.ID, op.AdapterType, adapterConfig, interval, op.HealthCheckIntervalSec)
}

func (hc *HealthChecker) checkOperator(opID uuid.UUID, adapterType string, config json.RawMessage, cb *CircuitBreaker, intervalSec int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a, err := hc.registry.GetOrCreate(opID, adapterType, config)
	if err != nil {
		hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("create adapter for health check")
		return
	}

	result := a.HealthCheck(ctx)

	var status string
	if result.Success {
		cb.RecordSuccess()
	} else {
		cb.RecordFailure()
	}

	cbState := cb.State()
	switch cbState {
	case CircuitOpen:
		status = "down"
	case CircuitHalfOpen:
		status = "degraded"
	case CircuitClosed:
		if result.Success {
			status = "healthy"
		} else {
			status = "degraded"
		}
	default:
		status = "unknown"
	}

	var latencyMs *int
	if result.LatencyMs > 0 {
		latencyMs = &result.LatencyMs
	}
	var errorMsg *string
	if result.Error != "" {
		errorMsg = &result.Error
	}

	if err := hc.store.InsertHealthLog(ctx, opID, status, latencyMs, errorMsg, string(cbState)); err != nil {
		hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("insert health log")
	}

	if err := hc.store.UpdateHealthStatus(ctx, opID, status); err != nil {
		hc.logger.Error().Err(err).Str("operator_id", opID.String()).Msg("update health status")
	}

	if hc.redisClient != nil {
		cached := CachedHealth{
			Status:       status,
			LatencyMs:    result.LatencyMs,
			CircuitState: string(cbState),
			CheckedAt:    time.Now().Format(time.RFC3339),
		}
		data, _ := json.Marshal(cached)
		key := fmt.Sprintf("operator:health:%s", opID.String())
		ttl := 2 * time.Duration(intervalSec) * time.Second
		if ttl < 30*time.Second {
			ttl = 60 * time.Second
		}
		hc.redisClient.Set(ctx, key, data, ttl)
	}
}

func (hc *HealthChecker) Stop() {
	hc.mu.Lock()
	if hc.stopped {
		hc.mu.Unlock()
		return
	}
	hc.stopped = true
	for _, ch := range hc.stopChs {
		close(ch)
	}
	hc.mu.Unlock()
	hc.wg.Wait()
	hc.logger.Info().Msg("health checker stopped")
}

func (hc *HealthChecker) RefreshOperator(ctx context.Context, opID uuid.UUID) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if ch, ok := hc.stopChs[opID]; ok {
		close(ch)
		delete(hc.stopChs, opID)
		delete(hc.breakers, opID)
	}

	hc.registry.Remove(opID)

	op, err := hc.store.GetByID(ctx, opID)
	if err != nil {
		return fmt.Errorf("health checker: refresh operator: %w", err)
	}

	if op.State != "active" {
		return nil
	}

	hc.startOperatorLoop(*op)
	return nil
}

func (hc *HealthChecker) GetCachedHealth(ctx context.Context, opID uuid.UUID) (*CachedHealth, error) {
	if hc.redisClient != nil {
		key := fmt.Sprintf("operator:health:%s", opID.String())
		data, err := hc.redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var ch CachedHealth
			if json.Unmarshal(data, &ch) == nil {
				return &ch, nil
			}
		}
	}

	log, err := hc.store.GetLatestHealth(ctx, opID)
	if err != nil {
		return nil, err
	}
	if log == nil {
		return nil, nil
	}

	latency := 0
	if log.LatencyMs != nil {
		latency = *log.LatencyMs
	}

	return &CachedHealth{
		Status:       log.Status,
		LatencyMs:    latency,
		CircuitState: log.CircuitState,
		CheckedAt:    log.CheckedAt.Format(time.RFC3339),
	}, nil
}

func (hc *HealthChecker) GetCircuitBreaker(opID uuid.UUID) *CircuitBreaker {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	return hc.breakers[opID]
}
