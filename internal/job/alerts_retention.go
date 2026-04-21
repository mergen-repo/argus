package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/rs/zerolog"
)

const minAlertsRetentionDays = 30

type AlertsRetentionProcessor struct {
	jobs          *store.JobStore
	alertStore    *store.AlertStore
	eventBus      *bus.EventBus
	retentionDays int
	logger        zerolog.Logger
}

func NewAlertsRetentionProcessor(
	jobs *store.JobStore,
	alertStore *store.AlertStore,
	eventBus *bus.EventBus,
	retentionDays int,
	logger zerolog.Logger,
) *AlertsRetentionProcessor {
	if retentionDays < minAlertsRetentionDays {
		retentionDays = minAlertsRetentionDays
	}
	return &AlertsRetentionProcessor{
		jobs:          jobs,
		alertStore:    alertStore,
		eventBus:      eventBus,
		retentionDays: retentionDays,
		logger:        logger.With().Str("processor", JobTypeAlertsRetention).Logger(),
	}
}

func (p *AlertsRetentionProcessor) Type() string {
	return JobTypeAlertsRetention
}

type alertsRetentionResult struct {
	DeletedCount  int64  `json:"deleted_count"`
	Cutoff        string `json:"cutoff"`
	RetentionDays int    `json:"retention_days"`
	Status        string `json:"status"`
}

// buildRetentionResult builds the result JSON. Factored out so unit tests can
// assert the JSON shape without touching the database.
func buildRetentionResult(deleted int64, cutoff time.Time, retentionDays int) ([]byte, error) {
	return json.Marshal(alertsRetentionResult{
		DeletedCount:  deleted,
		Cutoff:        cutoff.UTC().Format(time.RFC3339),
		RetentionDays: retentionDays,
		Status:        "completed",
	})
}

func (p *AlertsRetentionProcessor) Process(ctx context.Context, j *store.Job) error {
	cutoff := time.Now().UTC().Add(-time.Duration(p.retentionDays) * 24 * time.Hour)

	p.logger.Info().
		Str("job_id", j.ID.String()).
		Int("retention_days", p.retentionDays).
		Time("cutoff", cutoff).
		Msg("starting alerts retention purge")

	deleted, err := p.alertStore.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("delete old alerts: %w", err)
	}

	resultJSON, err := buildRetentionResult(deleted, cutoff, p.retentionDays)
	if err != nil {
		return fmt.Errorf("marshal retention result: %w", err)
	}

	if err := p.jobs.Complete(ctx, j.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("complete alerts retention job: %w", err)
	}

	if p.eventBus != nil {
		_ = p.eventBus.Publish(ctx, bus.SubjectJobCompleted, map[string]interface{}{
			"job_id":        j.ID.String(),
			"tenant_id":     j.TenantID.String(),
			"type":          JobTypeAlertsRetention,
			"state":         "completed",
			"deleted_count": deleted,
			"cutoff":        cutoff.UTC().Format(time.RFC3339),
		})
	}

	p.logger.Info().
		Int64("deleted", deleted).
		Time("cutoff", cutoff).
		Msg("alerts retention purge completed")

	return nil
}
