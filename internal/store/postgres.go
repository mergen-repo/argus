package store

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/btopcu/argus/internal/observability/metrics"
)

type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgres creates a pgxpool-backed store without metrics instrumentation.
// Preserved for backward compatibility; prefer NewPostgresWithMetrics in new code
// so DB query spans and the DBQueryDuration histogram are recorded.
func NewPostgres(ctx context.Context, dsn string, maxConns, maxIdleConns int32, connMaxLife time.Duration) (*Postgres, error) {
	return newPostgres(ctx, dsn, maxConns, maxIdleConns, connMaxLife, nil)
}

// NewPostgresWithMetrics creates a pgxpool-backed store with OTel tracing
// (via otelpgx) and a slow-query Prometheus histogram observer wired through
// a composite pgx QueryTracer. When reg is non-nil the tracer:
//   - observes argus_db_query_duration_seconds{operation,table} on every query
//   - tags the active span with argus.db.slow=true when duration > 100ms
func NewPostgresWithMetrics(ctx context.Context, dsn string, maxConns, maxIdleConns int32, connMaxLife time.Duration, reg *metrics.Registry) (*Postgres, error) {
	return newPostgres(ctx, dsn, maxConns, maxIdleConns, connMaxLife, reg)
}

func newPostgres(ctx context.Context, dsn string, maxConns, maxIdleConns int32, connMaxLife time.Duration, reg *metrics.Registry) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: parse config: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.MinConns = maxIdleConns
	cfg.MaxConnLifetime = connMaxLife
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	// FIX-301: pgx v5 defaults to QueryExecModeCacheStatement, which pins relation
	// OIDs in per-connection prepared-statement and description caches. After a
	// DDL change (migrations DROP/CREATE, partition rotation, shadow-swap rebuild),
	// connections that ran a query before the DDL retain stale OIDs and fail with
	// `could not open relation with OID <N>` (SQLSTATE XX000) until recycled.
	// Switching to QueryExecModeExec uses unnamed prepared statements per call —
	// no OID pinning, immune to runtime DDL.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	// Attach tracers. otelpgx provides QueryTracer + BatchTracer + CopyFromTracer
	// + PrepareTracer + ConnectTracer + pgxpool.AcquireTracer semantics; the slow
	// query tracer piggybacks on QueryTracer only. The composite fans callbacks
	// out to every wrapped tracer that implements the relevant interface.
	tracers := []pgx.QueryTracer{
		otelpgx.NewTracer(
			otelpgx.WithTrimSQLInSpanName(),
			// WithIncludeQueryParameters is intentionally NOT set — query args
			// may contain PII (MSISDN, ICCID, tenant-scoped identifiers) and
			// must not leak into trace attributes.
		),
	}
	if reg != nil {
		tracers = append(tracers, newSlowQueryTracer(reg))
	}
	cfg.ConnConfig.Tracer = newCompositeTracer(tracers...)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	return &Postgres{Pool: pool}, nil
}

func (p *Postgres) HealthCheck(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}

func (p *Postgres) Close() {
	p.Pool.Close()
}
