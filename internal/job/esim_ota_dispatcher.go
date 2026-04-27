package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/smsr"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

// dispatcherCommandStore is the minimal EsimOTACommandStore subset the dispatcher depends on (PAT-019).
type dispatcherCommandStore interface {
	ListQueued(ctx context.Context, limit int, now time.Time) ([]store.EsimOTACommand, error)
	MarkSent(ctx context.Context, id uuid.UUID, smsrCommandID string) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
	IncrementRetry(ctx context.Context, id uuid.UUID, nextRetryAt time.Time) error
}

// dispatcherProfileStore is the minimal ESimProfileStore subset the dispatcher depends on (PAT-019).
type dispatcherProfileStore interface {
	MarkFailed(ctx context.Context, profileID uuid.UUID, errMsg string) error
}

// dispatcherSMSRClient is the minimal smsr.Client subset the dispatcher depends on (PAT-019).
type dispatcherSMSRClient interface {
	Push(ctx context.Context, req smsr.PushRequest) (smsr.PushResponse, error)
}

// dispatcherAuditStore is the minimal audit.Auditor subset the dispatcher depends on (PAT-019).
type dispatcherAuditStore interface {
	CreateEntry(ctx context.Context, p audit.CreateEntryParams) (*audit.Entry, error)
}

// dispatcherEventBus is the minimal bus.EventBus subset the dispatcher depends on (PAT-019).
type dispatcherEventBus interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

// expBackoff returns the retry delay for a given retry_count (1-indexed after increment).
// Sequence: 30s, 60s, 120s, 240s, 480s.
func expBackoff(retryCount int) time.Duration {
	base := 30 * time.Second
	for i := 1; i < retryCount && i < 5; i++ {
		base *= 2
	}
	return base
}

// ESimOTADispatcherProcessor processes queued OTA commands by dispatching them
// to the SM-SR via smsr.Client, with per-operator rate limiting and exponential
// backoff on transient failures.
//
// JobType: JobTypeOTACommand (reused per spec)
// Configuration: rateLimitRPS / batchSize / maxRetries are passed through the
// Config struct (PAT-017 hops 1-5 — env → cfg → constructor → field → use).
type ESimOTADispatcherProcessor struct {
	jobs          *store.JobStore
	commandStore  dispatcherCommandStore
	profileStore  dispatcherProfileStore
	smsrClient    dispatcherSMSRClient
	auditor       dispatcherAuditStore
	eventBus      dispatcherEventBus
	batchSize     int
	rateLimitRPS  int
	maxRetries    int
	rateLimiters  map[string]*rate.Limiter
	rateLimiterMu sync.Mutex
	logger        zerolog.Logger
}

// dispatcherDefaults are the M2M-scale defaults applied when caller passes <= 0.
// They MUST match the Config struct defaults so the runtime is deterministic regardless
// of which side neglects to set the value.
const (
	dispatcherDefaultRateLimitRPS = 100
	dispatcherDefaultBatchSize    = 200
	dispatcherDefaultMaxRetries   = 5
)

// NewESimOTADispatcherProcessor wires the OTA dispatcher.
// Pass cfg.ESimOTARateLimitPerSec / ESimOTABatchSize / ESimOTAMaxRetries from main.go.
// PAT-017: env-var reads happen at Config struct level; constructors must NOT call os.Getenv directly.
func NewESimOTADispatcherProcessor(
	jobs *store.JobStore,
	commandStore dispatcherCommandStore,
	profileStore dispatcherProfileStore,
	smsrClient dispatcherSMSRClient,
	auditor dispatcherAuditStore,
	eventBus dispatcherEventBus,
	rateLimitPerSec int,
	batchSize int,
	maxRetries int,
	logger zerolog.Logger,
) *ESimOTADispatcherProcessor {
	if rateLimitPerSec <= 0 {
		rateLimitPerSec = dispatcherDefaultRateLimitRPS
	}
	if batchSize <= 0 {
		batchSize = dispatcherDefaultBatchSize
	}
	if maxRetries <= 0 {
		maxRetries = dispatcherDefaultMaxRetries
	}
	return &ESimOTADispatcherProcessor{
		jobs:         jobs,
		commandStore: commandStore,
		profileStore: profileStore,
		smsrClient:   smsrClient,
		auditor:      auditor,
		eventBus:     eventBus,
		batchSize:    batchSize,
		rateLimitRPS: rateLimitPerSec,
		maxRetries:   maxRetries,
		rateLimiters: make(map[string]*rate.Limiter),
		logger:       logger.With().Str("processor", JobTypeOTACommand).Logger(),
	}
}

func (p *ESimOTADispatcherProcessor) Type() string {
	return JobTypeOTACommand
}

func (p *ESimOTADispatcherProcessor) getLimiter(operatorID string) *rate.Limiter {
	p.rateLimiterMu.Lock()
	defer p.rateLimiterMu.Unlock()
	if lim, ok := p.rateLimiters[operatorID]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Limit(p.rateLimitRPS), p.rateLimitRPS)
	p.rateLimiters[operatorID] = lim
	return lim
}

// dispatcherResult is the aggregate result JSON written to the caller's context.
type dispatcherResult struct {
	Dispatched int `json:"dispatched"`
	Retried    int `json:"retried"`
	Failed     int `json:"failed"`
}

// Process fetches a batch of queued OTA commands and dispatches them via smsr.Client.
func (p *ESimOTADispatcherProcessor) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().Str("job_id", j.ID.String()).Msg("esim_ota_dispatcher: starting sweep")

	commands, err := p.commandStore.ListQueued(ctx, p.batchSize, time.Now())
	if err != nil {
		return fmt.Errorf("esim_ota_dispatcher: list queued: %w", err)
	}

	var dispatched, retried, failed int

	for _, cmd := range commands {
		p.processCommand(ctx, cmd, &dispatched, &retried, &failed)
	}

	res := dispatcherResult{
		Dispatched: dispatched,
		Retried:    retried,
		Failed:     failed,
	}
	resultJSON, _ := json.Marshal(res)
	if p.jobs != nil {
		if cErr := p.jobs.Complete(ctx, j.ID, nil, resultJSON); cErr != nil {
			p.logger.Error().Err(cErr).Str("job_id", j.ID.String()).Msg("esim_ota_dispatcher: complete job failed")
		}
	}

	p.logger.Info().
		Int("dispatched", dispatched).
		Int("retried", retried).
		Int("failed", failed).
		Msg("esim_ota_dispatcher: sweep completed")

	return nil
}

func (p *ESimOTADispatcherProcessor) processCommand(
	ctx context.Context,
	cmd store.EsimOTACommand,
	dispatched, retried, failed *int,
) {
	operatorID := ""
	if cmd.TargetOperatorID != nil {
		operatorID = cmd.TargetOperatorID.String()
	}

	lim := p.getLimiter(operatorID)
	if !lim.Allow() {
		p.logger.Debug().
			Str("command_id", cmd.ID.String()).
			Str("operator_id", operatorID).
			Msg("esim_ota_dispatcher: rate limited, deferring command")
		return
	}

	sourceProfile := ""
	if cmd.SourceProfileID != nil {
		sourceProfile = cmd.SourceProfileID.String()
	}
	targetProfile := ""
	if cmd.TargetProfileID != nil {
		targetProfile = cmd.TargetProfileID.String()
	}
	corrID := ""
	if cmd.CorrelationID != nil {
		corrID = cmd.CorrelationID.String()
	}

	req := smsr.PushRequest{
		EID:           cmd.EID,
		CommandType:   smsr.CommandType(cmd.CommandType),
		SourceProfile: sourceProfile,
		TargetProfile: targetProfile,
		CommandID:     cmd.ID.String(),
		CorrelationID: corrID,
	}

	resp, pushErr := p.smsrClient.Push(ctx, req)
	if pushErr == nil {
		p.onSuccess(ctx, cmd, resp)
		*dispatched++
		return
	}

	isTransient := errors.Is(pushErr, smsr.ErrSMSRConnectionFailed) || errors.Is(pushErr, smsr.ErrSMSRRateLimit)
	isPermanent := errors.Is(pushErr, smsr.ErrSMSRRejected)

	nextRetryCount := cmd.RetryCount + 1

	if isTransient && nextRetryCount < p.maxRetries {
		backoff := expBackoff(nextRetryCount)
		nextRetryAt := time.Now().Add(backoff)
		if err := p.commandStore.IncrementRetry(ctx, cmd.ID, nextRetryAt); err != nil {
			p.logger.Error().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_dispatcher: increment retry failed")
		}
		p.logger.Debug().
			Str("command_id", cmd.ID.String()).
			Int("retry_count", nextRetryCount).
			Dur("backoff", backoff).
			Msg("esim_ota_dispatcher: transient error, scheduled retry")
		*retried++
		return
	}

	if isTransient && nextRetryCount >= p.maxRetries {
		p.onTerminal(ctx, cmd, pushErr.Error())
		*failed++
		return
	}

	if isPermanent {
		p.onTerminal(ctx, cmd, pushErr.Error())
		*failed++
		return
	}

	p.onTerminal(ctx, cmd, pushErr.Error())
	*failed++
}

func (p *ESimOTADispatcherProcessor) onSuccess(ctx context.Context, cmd store.EsimOTACommand, resp smsr.PushResponse) {
	if err := p.commandStore.MarkSent(ctx, cmd.ID, resp.SMSRCommandID); err != nil {
		p.logger.Error().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_dispatcher: MarkSent failed")
	}

	p.emitAudit(ctx, cmd, "ota.dispatch", nil)

	payload := map[string]interface{}{
		"command_id":      cmd.ID.String(),
		"eid":             cmd.EID,
		"smsr_command_id": resp.SMSRCommandID,
		"command_type":    cmd.CommandType,
	}
	if cmd.TargetOperatorID != nil {
		payload["target_operator_id"] = cmd.TargetOperatorID.String()
	}
	if cmd.TenantID != uuid.Nil {
		payload["tenant_id"] = cmd.TenantID.String()
	}
	_ = p.eventBus.Publish(ctx, bus.SubjectESimCommandIssued, payload)
}

func (p *ESimOTADispatcherProcessor) onTerminal(ctx context.Context, cmd store.EsimOTACommand, errMsg string) {
	if err := p.commandStore.MarkFailed(ctx, cmd.ID, errMsg); err != nil {
		p.logger.Error().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_dispatcher: MarkFailed command failed")
	}

	if cmd.ProfileID != nil {
		if err := p.profileStore.MarkFailed(ctx, *cmd.ProfileID, errMsg); err != nil {
			p.logger.Error().Err(err).Str("profile_id", cmd.ProfileID.String()).Msg("esim_ota_dispatcher: MarkFailed profile failed")
		}
	}

	afterData, _ := json.Marshal(map[string]any{"error": errMsg, "status": "failed"})
	p.emitAudit(ctx, cmd, "ota.dispatch.failed", afterData)

	payload := map[string]interface{}{
		"command_id":   cmd.ID.String(),
		"eid":          cmd.EID,
		"command_type": cmd.CommandType,
		"error":        errMsg,
	}
	if cmd.TenantID != uuid.Nil {
		payload["tenant_id"] = cmd.TenantID.String()
	}
	_ = p.eventBus.Publish(ctx, bus.SubjectESimCommandFailed, payload)
}

func (p *ESimOTADispatcherProcessor) emitAudit(ctx context.Context, cmd store.EsimOTACommand, action string, afterData json.RawMessage) {
	if p.auditor == nil {
		return
	}
	entityID := ""
	if cmd.ProfileID != nil {
		entityID = cmd.ProfileID.String()
	} else {
		entityID = cmd.EID
	}

	beforeData, _ := json.Marshal(map[string]any{
		"eid":          cmd.EID,
		"command_type": cmd.CommandType,
		"retry_count":  cmd.RetryCount,
	})

	params := audit.CreateEntryParams{
		TenantID:   cmd.TenantID,
		Action:     action,
		EntityType: "esim_profile",
		EntityID:   entityID,
		BeforeData: beforeData,
		AfterData:  afterData,
	}
	if cmd.JobID != nil {
		params.CorrelationID = cmd.JobID
	}

	if _, err := p.auditor.CreateEntry(ctx, params); err != nil {
		p.logger.Warn().Err(err).Str("command_id", cmd.ID.String()).Msg("esim_ota_dispatcher: audit write failed")
	}
}
