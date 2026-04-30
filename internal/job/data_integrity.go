package job

// DataIntegrityDetector is the Processor for the data_integrity_scan cron job (FIX-207 AC-5).
//
// Scan scope: queries 1–3 are bounded to the last 24 hours to prevent full-table
// scans on large hypertables. Query 4 (IMSI malformed) is intentionally unbounded
// because malformed IMSIs are a structural defect, not a time-series event.
//
// Quarantine: sessions/CDR violations are inserted into session_quarantine with
// quarantined_by='fix207_scan' for forensic traceability. Idempotent: each INSERT
// uses a NOT EXISTS guard so repeated runs do not double-insert.
// IMSI violations are NOT quarantined here because session_quarantine.original_table
// CHECK only permits 'sessions' and 'cdrs'. Broadening the CHECK + adding a
// sims-quarantine surface is tracked as D-069 in ROUTEMAP.
//
// Notification: notification-store wiring omitted — log-only for now. Per-tenant
// alert wiring is tracked as D-070 in ROUTEMAP.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const (
	kindNegDurationSession  = "neg_duration_session"
	kindNegDurationCDR      = "neg_duration_cdr"
	kindFramedIPOutsidePool = "framed_ip_outside_pool"
	kindIMSIMalformed       = "imsi_malformed"
)

// dataIntegrityJobStore is the minimal store.JobStore surface used by DataIntegrityDetector.
type dataIntegrityJobStore interface {
	Complete(ctx context.Context, jobID uuid.UUID, errorReport json.RawMessage, result json.RawMessage) error
}

// diDB is the minimal DB surface required by DataIntegrityDetector.
type diDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DataIntegrityDetector implements Processor for JobTypeDataIntegrityScan.
type DataIntegrityDetector struct {
	db       diDB
	jobs     dataIntegrityJobStore
	eventBus busPublisher
	metrics  *metrics.Registry
	logger   zerolog.Logger
}

func NewDataIntegrityDetector(
	db *pgxpool.Pool,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	reg *metrics.Registry,
	logger zerolog.Logger,
) *DataIntegrityDetector {
	return &DataIntegrityDetector{
		db:       db,
		jobs:     jobs,
		eventBus: eventBus,
		metrics:  reg,
		logger:   logger.With().Str("processor", JobTypeDataIntegrityScan).Logger(),
	}
}

func (p *DataIntegrityDetector) Type() string {
	return JobTypeDataIntegrityScan
}

// Process runs the 4 data-integrity scans and records results.
func (p *DataIntegrityDetector) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Msg("data integrity scan starting")

	counts, err := p.runScans(ctx)
	if err != nil {
		return fmt.Errorf("data integrity scan: %w", err)
	}

	totalViolations := 0
	for _, kind := range []string{kindNegDurationSession, kindNegDurationCDR, kindFramedIPOutsidePool, kindIMSIMalformed} {
		n := counts[kind]
		p.logger.Debug().Str("kind", kind).Int("count", n).Msg("data integrity scan result")
		if n > 0 {
			p.logger.Warn().Str("kind", kind).Int("count", n).Msg("data integrity violations detected")
			if p.metrics != nil {
				p.metrics.IncDataIntegrity(kind, float64(n))
			}
			totalViolations += n
		}
	}

	if totalViolations > 0 {
		p.logger.Warn().
			Int("total_violations", totalViolations).
			Str("job_id", job.ID.String()).
			Msg("data integrity scan completed with violations — see session_quarantine and logs")
	} else {
		p.logger.Info().Str("job_id", job.ID.String()).Msg("data integrity scan completed — no violations")
	}

	result, _ := json.Marshal(map[string]any{
		"counts":           counts,
		"total_violations": totalViolations,
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, result); err != nil {
		return fmt.Errorf("data integrity scan: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]any{
			"job_id":           job.ID.String(),
			"tenant_id":        job.TenantID.String(),
			"type":             JobTypeDataIntegrityScan,
			"state":            "completed",
			"total_violations": totalViolations,
		})
	}

	return nil
}

// diScanResult holds per-kind counts from a single scan run.
type diScanResult map[string]int

// Package-level SQL constants — exported with a test-visible prefix so
// TestDataIntegrityDetector_Run_BoundedScan can assert the 24h window is present.
const (
	qNegSessionSQL = `
		INSERT INTO session_quarantine
		      (original_table, original_id, tenant_id, violation_reason, row_data, quarantined_by)
		SELECT 'sessions',
		       s.id::text,
		       s.tenant_id,
		       'neg_duration_session',
		       to_jsonb(s.*),
		       'fix207_scan'
		FROM sessions s
		WHERE s.ended_at IS NOT NULL
		  AND s.ended_at < s.started_at
		  AND s.started_at >= NOW() - INTERVAL '24 hours'
		  AND NOT EXISTS (
		    SELECT 1 FROM session_quarantine q
		    WHERE q.original_table = 'sessions'
		      AND q.original_id = s.id::text
		      AND q.violation_reason = 'neg_duration_session'
		  )
	`

	qNegCDRSQL = `
		INSERT INTO session_quarantine
		      (original_table, original_id, tenant_id, violation_reason, row_data, quarantined_by)
		SELECT 'cdrs',
		       c.id::text,
		       c.tenant_id,
		       'neg_duration_cdr',
		       to_jsonb(c.*),
		       'fix207_scan'
		FROM cdrs c
		WHERE c.duration_sec < 0
		  AND c.timestamp >= NOW() - INTERVAL '24 hours'
		  AND NOT EXISTS (
		    SELECT 1 FROM session_quarantine q
		    WHERE q.original_table = 'cdrs'
		      AND q.original_id = c.id::text
		      AND q.violation_reason = 'neg_duration_cdr'
		  )
	`

	// PAT-009: m.apn_id IS NOT NULL guard avoids nullable-FK scan issues.
	// Uses PostgreSQL inet <<= cidr containment operator; kept in SQL for performance.
	qIPOutsideSQL = `
		SELECT COUNT(*) FROM sessions s
		  JOIN sims m ON s.sim_id = m.id
		 WHERE s.framed_ip IS NOT NULL
		   AND s.started_at >= NOW() - INTERVAL '24 hours'
		   AND m.apn_id IS NOT NULL
		   AND NOT EXISTS (
		     SELECT 1 FROM ip_pools p
		     WHERE p.apn_id = m.apn_id
		       AND (
		             (p.cidr_v4 IS NOT NULL AND s.framed_ip <<= p.cidr_v4)
		          OR (p.cidr_v6 IS NOT NULL AND s.framed_ip <<= p.cidr_v6)
		       )
		   )
	`

	// Intentionally unbounded — IMSI malformation is structural, not time-series.
	qIMSISQL = `SELECT COUNT(*) FROM sims WHERE imsi !~ '^\d{14,15}$'`
)

// runScans executes all 4 invariant checks and returns counts by kind.
// Queries 1 and 2 also INSERT newly-found violations into session_quarantine.
func (p *DataIntegrityDetector) runScans(ctx context.Context) (diScanResult, error) {
	counts := diScanResult{
		kindNegDurationSession:  0,
		kindNegDurationCDR:      0,
		kindFramedIPOutsidePool: 0,
		kindIMSIMalformed:       0,
	}

	// 1. Sessions with ended_at < started_at (last 24h) — quarantine + count.
	tag, err := p.db.Exec(ctx, qNegSessionSQL)
	if err != nil {
		return nil, fmt.Errorf("neg_duration_session scan: %w", err)
	}
	counts[kindNegDurationSession] = int(tag.RowsAffected())

	// 2. CDRs with duration_sec < 0 (last 24h) — quarantine + count.
	tag, err = p.db.Exec(ctx, qNegCDRSQL)
	if err != nil {
		return nil, fmt.Errorf("neg_duration_cdr scan: %w", err)
	}
	counts[kindNegDurationCDR] = int(tag.RowsAffected())

	// 3. framed_ip outside any APN pool CIDR (last 24h, SIMs with non-null apn_id).
	var ipOutsideCount int
	if err := p.db.QueryRow(ctx, qIPOutsideSQL).Scan(&ipOutsideCount); err != nil {
		return nil, fmt.Errorf("framed_ip_outside_pool scan: %w", err)
	}
	counts[kindFramedIPOutsidePool] = ipOutsideCount

	// 4. Malformed IMSI — structural check, intentionally unbounded.
	// Not quarantined: session_quarantine.original_table CHECK only allows 'sessions'/'cdrs'.
	var imsiCount int
	if err := p.db.QueryRow(ctx, qIMSISQL).Scan(&imsiCount); err != nil {
		return nil, fmt.Errorf("imsi_malformed scan: %w", err)
	}
	counts[kindIMSIMalformed] = imsiCount

	return counts, nil
}
