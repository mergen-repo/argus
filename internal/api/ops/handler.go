package ops

import (
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Handler struct {
	metricsReg   *metrics.Registry
	pgPool       *pgxpool.Pool
	redisClient  *redis.Client
	natsJS       jetstream.JetStream
	auditStore   *store.AuditStore
	anomalyStore *store.AnomalyStore
	logger       zerolog.Logger
}

func NewHandler(
	metricsReg *metrics.Registry,
	pgPool *pgxpool.Pool,
	redisClient *redis.Client,
	natsJS jetstream.JetStream,
	auditStore *store.AuditStore,
	anomalyStore *store.AnomalyStore,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		metricsReg:   metricsReg,
		pgPool:       pgPool,
		redisClient:  redisClient,
		natsJS:       natsJS,
		auditStore:   auditStore,
		anomalyStore: anomalyStore,
		logger:       logger.With().Str("component", "ops_handler").Logger(),
	}
}
