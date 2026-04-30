package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// reaperCommandStore is the minimal EsimOTACommandStore subset the reaper depends on (PAT-019).
type reaperCommandStore interface {
	ListSentBefore(ctx context.Context, cutoff time.Time) ([]store.EsimOTACommand, error)
	MarkTimeout(ctx context.Context, id uuid.UUID) error
	IncrementRetry(ctx context.Context, id uuid.UUID, nextRetryAt time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
}

// reaperProfileStore is the minimal ESimProfileStore subset the reaper depends on (PAT-019).
type reaperProfileStore interface {
	MarkFailed(ctx context.Context, profileID uuid.UUID, errMsg string) error
}

// reaperAuditStore is the minimal audit.Auditor subset the reaper depends on (PAT-019).
type reaperAuditStore interface {
	CreateEntry(ctx context.Context, p audit.CreateEntryParams) (*audit.Entry, error)
}

// reaperEventBus is the minimal event bus subset the reaper depends on (PAT-019).
type reaperEventBus interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

const (
	// reaperDefaultTimeoutMinutes is the M2M-scale default applied when caller passes <= 0.
	// MUST match cfg.ESimOTATimeoutMinutes default.
	reaperDefaultTimeoutMinutes = 10
	reaperMaxRetries            = 5
)

// reaperResult is the aggregate result JSON written to the job context.
type reaperResult struct {
	Requeued int `json:"requeued"`
	Failed   int `json:"failed"`
}

// ESimOTATimeoutReaperProcessor sweeps sent OTA commands that have not been
// acknowledged within the timeout window and either re-queues them (if retry
// budget remains) or marks them terminal.
//
// JobType: JobTypeESimOTATimeoutReaper
// Cron: */2 * * * *
// Env: ESIM_OTA_TIMEOUT_MINUTES (default 10)
//
// State transition design:
//   - sent → timeout (MarkTimeout): used only when re-queuing (retries remain).
//     IncrementRetry then moves timeout → queued (no state filter in SQL).
//   - sent → failed (MarkFailed): direct transition for terminal case, bypassing
//     timeout state because MarkFailed requires status IN ('queued','sent').
type ESimOTATimeoutReaperProcessor struct {
	jobs           *store.JobStore
	commandStore   reaperCommandStore
	profileStore   reaperProfileStore
	auditor        reaperAuditStore
	eventBus       reaperEventBus
	timeoutMinutes int
	maxRetries     int
	logger         zerolog.Logger
}

// NewESimOTATimeoutReaperProcessor wires the OTA timeout reaper.
// Pass cfg.ESimOTATimeoutMinutes from main.go (PAT-017 hops 1-5).
func NewESimOTATimeoutReaperProcessor(
	jobs *store.JobStore,
	commandStore reaperCommandStore,
	profileStore reaperProfileStore,
	auditor reaperAuditStore,
	eventBus reaperEventBus,
	timeoutMinutes int,
	logger zerolog.Logger,
) *ESimOTATimeoutReaperProcessor {
	if timeoutMinutes <= 0 {
		timeoutMinutes = reaperDefaultTimeoutMinutes
	}
	return &ESimOTATimeoutReaperProcessor{
		jobs:           jobs,
		commandStore:   commandStore,
		profileStore:   profileStore,
		auditor:        auditor,
		eventBus:       eventBus,
		timeoutMinutes: timeoutMinutes,
		maxRetries:     reaperMaxRetries,
		logger:         logger.With().Str("processor", JobTypeESimOTATimeoutReaper).Logger(),
	}
}

func (p *ESimOTATimeoutReaperProcessor) Type() string {
	return JobTypeESimOTATimeoutReaper
}

// Process implements the timeout reaper sweep for *store.Job (called by scheduler).
func (p *ESimOTATimeoutReaperProcessor) Process(ctx context.Context, j *store.Job) error {
	res, err := p.sweep(ctx)
	if err != nil {
		return err
	}

	resultJSON, _ := json.Marshal(res)
	if j != nil {
		if cErr := p.completeJob(ctx, j, resultJSON); cErr != nil {
			return cErr
		}
	}

	p.logger.Info().
		Int("requeued", res.Requeued).
		Int("failed", res.Failed).
		Msg("esim_ota_timeout_reaper: sweep completed")
	return nil
}

func (p *ESimOTATimeoutReaperProcessor) completeJob(ctx context.Context, j *store.Job, resultJSON []byte) error {
	if p.jobs == nil {
		return nil
	}
	return p.jobs.Complete(ctx, j.ID, nil, resultJSON)
}

// sweep is the core reaper logic, separated for unit testing without a real Job.
func (p *ESimOTATimeoutReaperProcessor) sweep(ctx context.Context) (reaperResult, error) {
	cutoff := time.Now().Add(-time.Duration(p.timeoutMinutes) * time.Minute)

	commands, err := p.commandStore.ListSentBefore(ctx, cutoff)
	if err != nil {
		return reaperResult{}, fmt.Errorf("esim_ota_timeout_reaper: list sent before: %w", err)
	}

	var res reaperResult

	for _, cmd := range commands {
		if cmd.RetryCount < p.maxRetries {
			p.requeue(ctx, cmd, &res)
		} else {
			p.terminal(ctx, cmd, &res)
		}
	}

	return res, nil
}

func (p *ESimOTATimeoutReaperProcessor) requeue(ctx context.Context, cmd store.EsimOTACommand, res *reaperResult) {
	// Transition: sent → timeout (only when retries remain — timeout is a transient state).
	if err := p.commandStore.MarkTimeout(ctx, cmd.ID); err != nil {
		p.logger.Error().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_timeout_reaper: MarkTimeout failed")
		return
	}

	// Transition: timeout → queued via IncrementRetry (no status filter in SQL).
	nextRetryCount := cmd.RetryCount + 1
	backoff := expBackoff(nextRetryCount)
	nextRetryAt := time.Now().Add(backoff)

	if err := p.commandStore.IncrementRetry(ctx, cmd.ID, nextRetryAt); err != nil {
		p.logger.Error().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_timeout_reaper: IncrementRetry failed")
		return
	}

	p.logger.Debug().
		Str("command_id", cmd.ID.String()).
		Int("retry_count", nextRetryCount).
		Dur("backoff", backoff).
		Msg("esim_ota_timeout_reaper: requeued timed-out command")

	res.Requeued++
}

func (p *ESimOTATimeoutReaperProcessor) terminal(ctx context.Context, cmd store.EsimOTACommand, res *reaperResult) {
	const errMsg = "ota command timed out after max retries"

	// Direct sent → failed (bypass timeout state, as MarkFailed requires status IN ('queued','sent')).
	if err := p.commandStore.MarkFailed(ctx, cmd.ID, errMsg); err != nil {
		p.logger.Error().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_timeout_reaper: MarkFailed command failed")
	}

	if cmd.ProfileID != nil {
		if err := p.profileStore.MarkFailed(ctx, *cmd.ProfileID, errMsg); err != nil {
			p.logger.Error().Err(err).Str("profile_id", cmd.ProfileID.String()).Msg("esim_ota_timeout_reaper: MarkFailed profile failed")
		}
	}

	p.emitAudit(ctx, cmd, errMsg)

	payload := map[string]interface{}{
		"command_id":   cmd.ID.String(),
		"eid":          cmd.EID,
		"command_type": cmd.CommandType,
		"error":        errMsg,
	}
	if cmd.TenantID != uuid.Nil {
		payload["tenant_id"] = cmd.TenantID.String()
	}
	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectESimCommandFailed, payload)
	}

	res.Failed++
}

func (p *ESimOTATimeoutReaperProcessor) emitAudit(ctx context.Context, cmd store.EsimOTACommand, errMsg string) {
	if p.auditor == nil {
		return
	}
	entityID := cmd.EID
	if cmd.ProfileID != nil {
		entityID = cmd.ProfileID.String()
	}
	afterData, _ := json.Marshal(map[string]any{"error": errMsg, "status": "failed"})
	params := audit.CreateEntryParams{
		TenantID:   cmd.TenantID,
		Action:     "ota.timeout.failed",
		EntityType: "esim_profile",
		EntityID:   entityID,
		AfterData:  afterData,
	}
	if cmd.JobID != nil {
		params.CorrelationID = cmd.JobID
	}
	if _, err := p.auditor.CreateEntry(ctx, params); err != nil {
		p.logger.Warn().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_timeout_reaper: audit write failed")
	}
}
