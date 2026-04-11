package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type AuditRecorder interface {
	Record(ctx context.Context, tenantID uuid.UUID, action, entityType, entityID string, before, after any) error
}

type ipPoolReclaimer interface {
	ListExpiredReclaim(ctx context.Context, now time.Time, limit int) ([]store.ExpiredIPAddress, error)
	FinalizeReclaim(ctx context.Context, ipID uuid.UUID) error
}

type jobProgressTracker interface {
	UpdateProgress(ctx context.Context, jobID uuid.UUID, processed, failed, total int) error
	CheckCancelled(ctx context.Context, jobID uuid.UUID) (bool, error)
	Complete(ctx context.Context, jobID uuid.UUID, errorReport json.RawMessage, result json.RawMessage) error
}

type busPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type IPReclaimProcessor struct {
	jobs     jobProgressTracker
	ippools  ipPoolReclaimer
	eventBus busPublisher
	audit    AuditRecorder
	logger   zerolog.Logger
}

func NewIPReclaimProcessor(
	jobs *store.JobStore,
	ippools *store.IPPoolStore,
	eventBus *bus.EventBus,
	audit AuditRecorder,
	logger zerolog.Logger,
) *IPReclaimProcessor {
	return &IPReclaimProcessor{
		jobs:     jobs,
		ippools:  ippools,
		eventBus: eventBus,
		audit:    audit,
		logger:   logger.With().Str("processor", JobTypeIPReclaim).Logger(),
	}
}

func (p *IPReclaimProcessor) Type() string {
	return JobTypeIPReclaim
}

type ipReclaimPayload struct {
	Now *time.Time `json:"now,omitempty"`
}

func (p *IPReclaimProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Str("tenant_id", job.TenantID.String()).
		Msg("starting ip reclaim")

	now := time.Now().UTC()
	if len(job.Payload) > 0 {
		var payload ipReclaimPayload
		if err := json.Unmarshal(job.Payload, &payload); err == nil && payload.Now != nil {
			now = payload.Now.UTC()
		}
	}

	const batchLimit = 1000
	const progressEvery = 50

	expired, err := p.ippools.ListExpiredReclaim(ctx, now, batchLimit)
	if err != nil {
		return fmt.Errorf("ip reclaim: list expired: %w", err)
	}

	total := len(expired)
	reclaimed := 0
	failed := 0

	for i, ip := range expired {
		if err := p.ippools.FinalizeReclaim(ctx, ip.ID); err != nil {
			p.logger.Error().Err(err).
				Str("ip_id", ip.ID.String()).
				Msg("finalize reclaim failed")
			failed++
		} else {
			reclaimed++

			before := map[string]any{
				"sim_id": ip.PreviousSimID,
			}
			if ip.AddressV4 != nil {
				before["addr"] = *ip.AddressV4
			} else if ip.AddressV6 != nil {
				before["addr"] = *ip.AddressV6
			}

			if p.audit != nil {
				if auditErr := p.audit.Record(ctx, ip.TenantID, "ip.reclaimed", "ip_address", ip.ID.String(), before, nil); auditErr != nil {
					p.logger.Warn().Err(auditErr).Str("ip_id", ip.ID.String()).Msg("audit record failed")
				}
			}

			if p.eventBus != nil {
				addr := ""
				if ip.AddressV4 != nil {
					addr = *ip.AddressV4
				} else if ip.AddressV6 != nil {
					addr = *ip.AddressV6
				}
				if pubErr := p.eventBus.Publish(ctx, bus.SubjectIPReclaimed, map[string]any{
					"tenant_id": ip.TenantID,
					"pool_id":   ip.PoolID,
					"ip_id":     ip.ID,
					"addr":      addr,
				}); pubErr != nil {
					p.logger.Warn().Err(pubErr).Str("ip_id", ip.ID.String()).Msg("publish ip.reclaimed failed")
				}
			}
		}

		if (i+1)%progressEvery == 0 {
			_ = p.jobs.UpdateProgress(ctx, job.ID, reclaimed, failed, total)
		}

		cancelled, checkErr := p.jobs.CheckCancelled(ctx, job.ID)
		if checkErr == nil && cancelled {
			p.logger.Info().Msg("ip reclaim cancelled")
			break
		}
	}

	resultJSON, _ := json.Marshal(map[string]any{
		"reclaimed": reclaimed,
		"failed":    failed,
		"total":     total,
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("ip reclaim: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]any{
			"job_id":    job.ID.String(),
			"tenant_id": job.TenantID.String(),
			"type":      JobTypeIPReclaim,
			"state":     "completed",
			"reclaimed": reclaimed,
			"failed":    failed,
			"total":     total,
		})
	}

	p.logger.Info().
		Int("reclaimed", reclaimed).
		Int("failed", failed).
		Int("total", total).
		Msg("ip reclaim completed")

	return nil
}
