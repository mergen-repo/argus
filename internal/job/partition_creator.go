package job

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// partitionParents enumerates the RANGE-partitioned parent tables whose
// monthly child partitions are maintained by this job.
var partitionParents = []string{"audit_logs", "sim_state_history"}

// parentNameRE validates a parent table identifier: lowercase ASCII,
// underscores, digits — no whitespace, quotes, or semicolons. Parents come
// from the hard-coded partitionParents slice but we validate defensively.
var parentNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// partitionNameRE validates the <parent>_YYYY_MM naming scheme before
// interpolating into DDL. Pattern matches: ^[a-z][a-z0-9_]*_\d{4}_\d{2}$
var partitionNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*_\d{4}_\d{2}$`)

// defaultPartitionMonthsAhead is how many future months of partitions the
// creator ensures on every run. Bootstrap migration covers 9 months; the
// cron ensures a rolling 3-month lookahead so operators never hit the wall.
const defaultPartitionMonthsAhead = 3

// dbExec is the minimal database surface the partition creator needs.
// Abstracted to an interface so the logic is unit-testable without a live
// PostgreSQL connection. Both *pgxpool.Pool and test fakes satisfy it.
type dbExec interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// PartitionCreator idempotently ensures that the next N months of monthly
// partitions exist for RANGE-partitioned parent tables. Uses
// `CREATE TABLE IF NOT EXISTS ... PARTITION OF ... FOR VALUES FROM ... TO ...`
// so the job is safe to re-run and safe to race with midnight rollover.
//
// See STORY-064 DEV-168 for the rationale on rolling our own cron vs pg_partman.
type PartitionCreator struct {
	db  dbExec
	log zerolog.Logger
}

// NewPartitionCreator wires a PartitionCreator to a live pgxpool.Pool.
// Tests should call newPartitionCreatorFromExec directly to inject a fake.
func NewPartitionCreator(db *pgxpool.Pool, log zerolog.Logger) *PartitionCreator {
	return newPartitionCreatorFromExec(db, log)
}

func newPartitionCreatorFromExec(db dbExec, log zerolog.Logger) *PartitionCreator {
	return &PartitionCreator{
		db:  db,
		log: log.With().Str("component", "partition_creator").Logger(),
	}
}

// Run idempotently creates the next `monthsAhead` months of partitions for
// each parent table. Iterates from month 0 (current) to month monthsAhead
// inclusive, so a caller passing 3 gets 4 partitions per parent ensured
// (current + 3 future) — matching the "3 months lookahead" contract.
//
// The job returns on the first error encountered so that an alert fires
// quickly; a subsequent tick will resume where this one left off. Callers
// should treat a non-nil return as a warning to investigate, not data loss.
func (p *PartitionCreator) Run(ctx context.Context, monthsAhead int) error {
	if monthsAhead < 0 {
		return fmt.Errorf("partition_creator: monthsAhead must be >= 0, got %d", monthsAhead)
	}

	now := time.Now().UTC()
	// Anchor to the first-of-month at UTC midnight so partition boundaries
	// are deterministic regardless of the exact moment the cron fires.
	base := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	ensured := 0
	for _, parent := range partitionParents {
		for i := 0; i <= monthsAhead; i++ {
			start := base.AddDate(0, i, 0)
			end := base.AddDate(0, i+1, 0)
			name := partitionName(parent, start)

			if err := p.ensurePartition(ctx, parent, name, start, end); err != nil {
				// Wrap with parent/partition context so log-based triage is one-shot.
				return fmt.Errorf("partition_creator: ensure %s (%s..%s): %w",
					name,
					start.Format("2006-01-02"),
					end.Format("2006-01-02"),
					err,
				)
			}
			ensured++
		}
	}

	p.log.Debug().Int("partitions_ensured", ensured).Int("months_ahead", monthsAhead).Msg("partition creator pass complete")
	return nil
}

// ensurePartition issues a single CREATE TABLE IF NOT EXISTS DDL statement.
// The partition name is validated against a strict regex BEFORE being
// interpolated to prevent any possibility of SQL injection via an unexpected
// parent-table name. Dates are formatted as ISO-8601 date literals; no bind
// parameters are possible because PostgreSQL DDL does not accept them.
func (p *PartitionCreator) ensurePartition(ctx context.Context, parent, name string, start, end time.Time) error {
	if !parentNameRE.MatchString(parent) {
		return fmt.Errorf("invalid parent identifier %q", parent)
	}
	if !partitionNameRE.MatchString(name) {
		return fmt.Errorf("invalid partition name %q", name)
	}

	parentID := pgx.Identifier{parent}.Sanitize()
	partitionID := pgx.Identifier{name}.Sanitize()

	// Dates are derived from time.Now() + fixed offsets — not user input —
	// so the ISO-8601 literal interpolation is safe. We still single-quote
	// them in the SQL string as PostgreSQL DDL requires for date literals.
	sql := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
		partitionID,
		parentID,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)

	if _, err := p.db.Exec(ctx, sql); err != nil {
		// WARN log so operators see a single clear line in the tail; the
		// wrapped error from Run() carries the same context for alerting.
		p.log.Warn().
			Err(err).
			Str("parent", parent).
			Str("partition", name).
			Str("range_start", start.Format("2006-01-02")).
			Str("range_end", end.Format("2006-01-02")).
			Msg("partition ensure failed")
		return err
	}

	p.log.Info().
		Str("parent", parent).
		Str("partition", name).
		Str("range_start", start.Format("2006-01-02")).
		Str("range_end", end.Format("2006-01-02")).
		Msg("partition ensured")
	return nil
}

// partitionName produces `<parent>_YYYY_MM` matching the bootstrap migration
// convention (e.g. audit_logs_2026_07). Must round-trip through partitionNameRE.
func partitionName(parent string, start time.Time) string {
	return fmt.Sprintf("%s_%04d_%02d", parent, start.Year(), int(start.Month()))
}

// ============================================================================
// Processor adapter — lets the cron scheduler dispatch the job via the same
// JobType/Runner pipeline as every other scheduled task.
// ============================================================================

// PartitionCreatorProcessor adapts PartitionCreator to the job.Processor
// interface so the existing cron dispatch pipeline (Scheduler -> JobStore ->
// NATS -> Runner) can invoke it without a parallel code path.
type PartitionCreatorProcessor struct {
	creator     *PartitionCreator
	jobs        *store.JobStore
	eventBus    *bus.EventBus
	monthsAhead int
	logger      zerolog.Logger
}

// NewPartitionCreatorProcessor wires the processor to its dependencies.
// `monthsAhead <= 0` resets to defaultPartitionMonthsAhead.
func NewPartitionCreatorProcessor(
	db *pgxpool.Pool,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	monthsAhead int,
	logger zerolog.Logger,
) *PartitionCreatorProcessor {
	if monthsAhead <= 0 {
		monthsAhead = defaultPartitionMonthsAhead
	}
	return &PartitionCreatorProcessor{
		creator:     NewPartitionCreator(db, logger),
		jobs:        jobs,
		eventBus:    eventBus,
		monthsAhead: monthsAhead,
		logger:      logger.With().Str("processor", JobTypePartitionCreate).Logger(),
	}
}

func (p *PartitionCreatorProcessor) Type() string {
	return JobTypePartitionCreate
}

type partitionCreatorResult struct {
	MonthsAhead int    `json:"months_ahead"`
	Parents     int    `json:"parents"`
	Status      string `json:"status"`
}

// Process runs the partition creator and records the outcome on the job row.
func (p *PartitionCreatorProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Int("months_ahead", p.monthsAhead).
		Msg("starting partition creator run")

	if err := p.creator.Run(ctx, p.monthsAhead); err != nil {
		p.logger.Error().Err(err).Msg("partition creator run failed")
		return fmt.Errorf("run partition creator: %w", err)
	}

	result := partitionCreatorResult{
		MonthsAhead: p.monthsAhead,
		Parents:     len(partitionParents),
		Status:      "completed",
	}
	resultJSON, _ := json.Marshal(result)

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete partition creator job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":       job.ID.String(),
			"tenant_id":    job.TenantID.String(),
			"type":         JobTypePartitionCreate,
			"state":        "completed",
			"months_ahead": p.monthsAhead,
			"parents":      len(partitionParents),
		})
	}

	p.logger.Info().
		Int("months_ahead", p.monthsAhead).
		Int("parents", len(partitionParents)).
		Msg("partition creator run completed")
	return nil
}
