package job

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const defaultOrphanSessionInterval = 30 * time.Minute

// resolveOrphanSessionInterval returns the scan interval, honouring ORPHAN_SESSION_CHECK_INTERVAL
// (Go duration form, e.g. "15m", "1h") when set to a parseable positive value; falls back to the
// 30-minute default otherwise.
func resolveOrphanSessionInterval() time.Duration {
	if v := os.Getenv("ORPHAN_SESSION_CHECK_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultOrphanSessionInterval
}

type sessionQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type OrphanSessionDetector struct {
	db       sessionQuerier
	logger   zerolog.Logger
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func NewOrphanSessionDetector(pool *pgxpool.Pool, logger zerolog.Logger) *OrphanSessionDetector {
	return &OrphanSessionDetector{
		db:       pool,
		logger:   logger.With().Str("component", "orphan_session_detector").Logger(),
		interval: resolveOrphanSessionInterval(),
		stopCh:   make(chan struct{}),
	}
}

func (d *OrphanSessionDetector) Start() {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.run()
	}()
	d.logger.Info().Dur("interval", d.interval).Msg("orphan session detector started")
}

func (d *OrphanSessionDetector) Stop() {
	close(d.stopCh)
	d.wg.Wait()
	d.logger.Info().Msg("orphan session detector stopped")
}

func (d *OrphanSessionDetector) run() {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			if err := d.Run(context.Background()); err != nil {
				d.logger.Error().Err(err).Msg("orphan session detector run failed")
			}
		}
	}
}

// Run queries for active sessions with NULL apn_id (data integrity violation — active sessions
// should always have APN). Logs a warning per tenant; no auto-repair.
func (d *OrphanSessionDetector) Run(ctx context.Context) error {
	const q = `
		SELECT tenant_id::text, COUNT(*)
		FROM sessions
		WHERE apn_id IS NULL AND session_state = 'active'
		GROUP BY tenant_id
		HAVING COUNT(*) > 0
	`

	rows, err := d.db.Query(ctx, q)
	if err != nil {
		return err
	}
	defer rows.Close()

	totalCount := 0
	for rows.Next() {
		var tenantID string
		var count int
		if err := rows.Scan(&tenantID, &count); err != nil {
			return err
		}
		d.logger.Warn().
			Str("tenant_id", tenantID).
			Int("count", count).
			Msg("orphan sessions detected — active sessions with NULL apn_id")
		totalCount += count
	}

	if totalCount > 0 {
		d.logger.Warn().
			Int("total_count", totalCount).
			Msg("orphan session detector completed with warnings")
	}

	return rows.Err()
}
