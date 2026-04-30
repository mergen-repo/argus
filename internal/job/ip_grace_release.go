package job

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type IPGraceReleaseResult struct {
	ReleasedCount int      `json:"released_count"`
	Errors        []string `json:"errors,omitempty"`
}

type ipGraceReleaser interface {
	ListGraceExpired(ctx context.Context, limit int) ([]store.GraceExpiredIPAddress, error)
	ReleaseGraceIP(ctx context.Context, ipID uuid.UUID) error
}

type metricsIPGraceReleaser interface {
	IncIPGraceReleased(n int)
}

type IPGraceReleaseProcessor struct {
	jobs     jobProgressTracker
	ippools  ipGraceReleaser
	eventBus busPublisher
	audit    AuditRecorder
	metrics  metricsIPGraceReleaser
	logger   zerolog.Logger
}

func NewIPGraceReleaseProcessor(
	jobs *store.JobStore,
	ippools *store.IPPoolStore,
	eventBus *bus.EventBus,
	audit AuditRecorder,
	reg *metrics.Registry,
	logger zerolog.Logger,
) *IPGraceReleaseProcessor {
	return &IPGraceReleaseProcessor{
		jobs:     jobs,
		ippools:  ippools,
		eventBus: eventBus,
		audit:    audit,
		metrics:  reg,
		logger:   logger.With().Str("processor", JobTypeIPGraceRelease).Logger(),
	}
}

func (p *IPGraceReleaseProcessor) Type() string {
	return JobTypeIPGraceRelease
}

func (p *IPGraceReleaseProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Str("tenant_id", job.TenantID.String()).
		Msg("starting ip grace release")

	const batchLimit = 1000

	candidates, err := p.ippools.ListGraceExpired(ctx, batchLimit)
	if err != nil {
		return fmt.Errorf("ip grace release: list grace expired: %w", err)
	}

	releasedCount := 0
	var errs []string

	for _, ip := range candidates {
		if releaseErr := p.ippools.ReleaseGraceIP(ctx, ip.ID); releaseErr != nil {
			p.logger.Error().Err(releaseErr).
				Str("ip_id", ip.ID.String()).
				Msg("release grace ip failed")
			errs = append(errs, fmt.Sprintf("ip_id=%s: %s", ip.ID, releaseErr.Error()))
			continue
		}

		releasedCount++

		if p.eventBus != nil {
			addr := ""
			if ip.AddressV4 != nil {
				addr = *ip.AddressV4
			} else if ip.AddressV6 != nil {
				addr = *ip.AddressV6
			}
			env := bus.NewEnvelope("ip.released", ip.TenantID.String(), "info").
				WithSource("job").
				WithTitle("IP released").
				SetEntity("ip", ip.ID.String(), addr).
				WithMeta("ip_id", ip.ID.String()).
				WithMeta("address", addr).
				WithMeta("reason", "grace_expired")
			if pubErr := p.eventBus.Publish(ctx, bus.SubjectIPReleased, env); pubErr != nil {
				p.logger.Warn().Err(pubErr).Str("ip_id", ip.ID.String()).Msg("publish ip.released failed")
			}
		}
	}

	if p.metrics != nil {
		p.metrics.IncIPGraceReleased(releasedCount)
	}

	if p.audit != nil {
		if auditErr := p.audit.Record(
			ctx,
			job.TenantID,
			"ip.grace.swept",
			"ip_address",
			job.ID.String(),
			nil,
			map[string]any{"released_count": releasedCount},
		); auditErr != nil {
			p.logger.Warn().Err(auditErr).Msg("audit record failed")
		}
	}

	result := IPGraceReleaseResult{
		ReleasedCount: releasedCount,
		Errors:        errs,
	}
	resultJSON, _ := json.Marshal(result)

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("ip grace release: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]any{
			"job_id":         job.ID.String(),
			"tenant_id":      job.TenantID.String(),
			"type":           JobTypeIPGraceRelease,
			"state":          "completed",
			"released_count": releasedCount,
		})
	}

	p.logger.Info().
		Int("released_count", releasedCount).
		Int("error_count", len(errs)).
		Msg("ip grace release completed")

	return nil
}
