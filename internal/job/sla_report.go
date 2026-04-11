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

type slaTenantLister interface {
	List(ctx context.Context, cursor string, limit int, stateFilter string) ([]store.Tenant, string, error)
}

type slaOperatorQuerier interface {
	ListGrantsWithOperators(ctx context.Context, tenantID uuid.UUID) ([]store.GrantWithOperator, error)
	AggregateHealthForSLA(ctx context.Context, operatorID uuid.UUID, from, to time.Time) (*store.SLAAggregate, error)
}

type slaRadiusCounter interface {
	CountInWindow(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (int64, error)
}

type slaReportCreator interface {
	Create(ctx context.Context, row *store.SLAReportRow) (*store.SLAReportRow, error)
}

type SLAReportProcessor struct {
	jobs            jobProgressTracker
	slaStore        slaReportCreator
	operatorStore   slaOperatorQuerier
	tenantStore     slaTenantLister
	radiusSessStore slaRadiusCounter
	eventBus        busPublisher
	logger          zerolog.Logger
}

func NewSLAReportProcessor(
	jobs *store.JobStore,
	slaStore *store.SLAReportStore,
	operatorStore *store.OperatorStore,
	tenantStore *store.TenantStore,
	radiusSessStore *store.RadiusSessionStore,
	eventBus *bus.EventBus,
	logger zerolog.Logger,
) *SLAReportProcessor {
	return &SLAReportProcessor{
		jobs:            jobs,
		slaStore:        slaStore,
		operatorStore:   operatorStore,
		tenantStore:     tenantStore,
		radiusSessStore: radiusSessStore,
		eventBus:        eventBus,
		logger:          logger.With().Str("processor", JobTypeSLAReport).Logger(),
	}
}

func (p *SLAReportProcessor) Type() string {
	return JobTypeSLAReport
}

type slaReportPayload struct {
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"`
}

func (p *SLAReportProcessor) Process(ctx context.Context, job *store.Job) error {
	p.logger.Info().
		Str("job_id", job.ID.String()).
		Str("tenant_id", job.TenantID.String()).
		Msg("starting sla report generation")

	now := time.Now().UTC()
	windowEnd := now
	windowStart := now.Add(-24 * time.Hour)

	if len(job.Payload) > 0 {
		var payload slaReportPayload
		if err := json.Unmarshal(job.Payload, &payload); err == nil {
			if payload.WindowStart != nil {
				windowStart = payload.WindowStart.UTC()
			}
			if payload.WindowEnd != nil {
				windowEnd = payload.WindowEnd.UTC()
			}
		}
	}

	reportsGenerated := 0
	tenantsProcessed := 0
	errorCount := 0

	cursor := ""
	const tenantBatch = 100

	for {
		tenants, nextCursor, err := p.tenantStore.List(ctx, cursor, tenantBatch, "active")
		if err != nil {
			p.logger.Error().Err(err).Msg("list tenants failed")
			errorCount++
			break
		}

		for _, tenant := range tenants {
			tenantsProcessed++

			grants, grantErr := p.operatorStore.ListGrantsWithOperators(ctx, tenant.ID)
			if grantErr != nil {
				p.logger.Error().Err(grantErr).
					Str("tenant_id", tenant.ID.String()).
					Msg("list operator grants failed")
				errorCount++
				continue
			}

			sessions, sessErr := p.radiusSessStore.CountInWindow(ctx, tenant.ID, windowStart, windowEnd)
			if sessErr != nil {
				p.logger.Warn().Err(sessErr).
					Str("tenant_id", tenant.ID.String()).
					Msg("count sessions in window failed")
			}

			for _, grant := range grants {
				opID := grant.OperatorID

				agg, aggErr := p.operatorStore.AggregateHealthForSLA(ctx, opID, windowStart, windowEnd)
				if aggErr != nil {
					p.logger.Error().Err(aggErr).
						Str("tenant_id", tenant.ID.String()).
						Str("operator_id", opID.String()).
						Msg("aggregate health for sla failed")
					errorCount++
					continue
				}

				row := &store.SLAReportRow{
					TenantID:      tenant.ID,
					OperatorID:    &opID,
					WindowStart:   windowStart,
					WindowEnd:     windowEnd,
					UptimePct:     agg.UptimePct,
					LatencyP95Ms:  agg.LatencyP95Ms,
					IncidentCount: agg.IncidentCount,
					MTTRSec:       agg.MTTRSec,
					SessionsTotal: sessions,
					Details:       json.RawMessage(`{}`),
				}

				if _, createErr := p.slaStore.Create(ctx, row); createErr != nil {
					p.logger.Error().Err(createErr).
						Str("tenant_id", tenant.ID.String()).
						Str("operator_id", opID.String()).
						Msg("create sla report failed")
					errorCount++
					continue
				}
				reportsGenerated++

				if p.eventBus != nil {
					if pubErr := p.eventBus.Publish(ctx, bus.SubjectSLAReportGenerated, map[string]any{
						"tenant_id":   tenant.ID,
						"operator_id": opID,
						"uptime_pct":  agg.UptimePct,
					}); pubErr != nil {
						p.logger.Warn().Err(pubErr).Msg("publish sla.report.generated failed")
					}
				}
			}

			_ = p.jobs.UpdateProgress(ctx, job.ID, reportsGenerated, errorCount, tenantsProcessed)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	resultJSON, _ := json.Marshal(map[string]any{
		"reports_generated": reportsGenerated,
		"tenants":           tenantsProcessed,
		"errors":            errorCount,
	})

	if err := p.jobs.Complete(ctx, job.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("sla report: complete job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]any{
			"job_id":            job.ID.String(),
			"tenant_id":         job.TenantID.String(),
			"type":              JobTypeSLAReport,
			"state":             "completed",
			"reports_generated": reportsGenerated,
			"tenants":           tenantsProcessed,
			"errors":            errorCount,
		})
	}

	p.logger.Info().
		Int("reports_generated", reportsGenerated).
		Int("tenants", tenantsProcessed).
		Int("errors", errorCount).
		Msg("sla report generation completed")

	return nil
}
