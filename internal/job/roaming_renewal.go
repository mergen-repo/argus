package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/notification"
	sev "github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	roamingRenewalRedisTTL = 35 * 24 * time.Hour
)

type roamingRenewalAgreementStore interface {
	ListExpiringWithin(ctx context.Context, days int) ([]store.RoamingAgreement, error)
}

type roamingRenewalUserStore interface {
	ListByRole(ctx context.Context, tenantID uuid.UUID, role string) ([]store.User, error)
}

type RoamingRenewalSweeper struct {
	agreements roamingRenewalAgreementStore
	users      roamingRenewalUserStore
	jobs       jobProgressTracker
	eventBus   busPublisher
	redis      *redis.Client
	alertDays  int
	logger     zerolog.Logger
}

func NewRoamingRenewalSweeper(
	agreements *store.RoamingAgreementStore,
	users *store.UserStore,
	jobs *store.JobStore,
	eventBus *bus.EventBus,
	redisClient *redis.Client,
	alertDays int,
	logger zerolog.Logger,
) *RoamingRenewalSweeper {
	return &RoamingRenewalSweeper{
		agreements: agreements,
		users:      users,
		jobs:       jobs,
		eventBus:   eventBus,
		redis:      redisClient,
		alertDays:  alertDays,
		logger:     logger.With().Str("processor", JobTypeRoamingRenewal).Logger(),
	}
}

func (p *RoamingRenewalSweeper) Type() string {
	return JobTypeRoamingRenewal
}

func (p *RoamingRenewalSweeper) Process(ctx context.Context, j *store.Job) error {
	p.logger.Info().Str("job_id", j.ID.String()).Msg("roaming renewal sweep starting")

	agreements, err := p.agreements.ListExpiringWithin(ctx, p.alertDays)
	if err != nil {
		return fmt.Errorf("roaming_renewal: list expiring agreements: %w", err)
	}

	now := time.Now().UTC()
	notified := 0
	skipped := 0

	for _, ag := range agreements {
		dedupKey := fmt.Sprintf("argus:roaming:renewal:%s:%s", ag.ID.String(), now.Format("2006-01"))

		set, err := p.redis.SetNX(ctx, dedupKey, "1", roamingRenewalRedisTTL).Result()
		if err != nil {
			p.logger.Warn().Err(err).Str("agreement_id", ag.ID.String()).Msg("roaming renewal: redis SetNX error")
		}
		if !set {
			skipped++
			continue
		}

		daysToExpiry := int(time.Until(ag.EndDate).Hours() / 24)
		if daysToExpiry < 0 {
			daysToExpiry = 0
		}

		severity := sev.Medium
		if daysToExpiry <= 7 {
			severity = sev.Critical
		}

		alertPayload := notification.AlertPayload{
			AlertID:     fmt.Sprintf("roaming-renewal-%s-%s", ag.ID.String(), now.Format("2006-01")),
			AlertType:   "roaming.agreement.renewal_due",
			Severity:    severity,
			Title:       fmt.Sprintf("Roaming agreement expiring in %d days", daysToExpiry),
			Description: fmt.Sprintf("Agreement with %s expires on %s. Review terms and renew if needed.", ag.PartnerOperatorName, ag.EndDate.Format("2006-01-02")),
			EntityType:  "roaming_agreement",
			EntityID:    ag.ID,
			Metadata: map[string]interface{}{
				"operator_id":           ag.OperatorID.String(),
				"partner_operator_name": ag.PartnerOperatorName,
				"end_date":              ag.EndDate.Format("2006-01-02"),
				"days_to_expiry":        daysToExpiry,
				"auto_renew":            ag.AutoRenew,
			},
			Timestamp: now,
		}

		payload, marshalErr := json.Marshal(alertPayload)
		if marshalErr != nil {
			p.logger.Error().Err(marshalErr).Str("agreement_id", ag.ID.String()).Msg("roaming renewal: marshal alert payload")
			continue
		}

		if p.eventBus != nil {
			if publishErr := p.eventBus.Publish(ctx, bus.SubjectAlertTriggered, json.RawMessage(payload)); publishErr != nil {
				p.logger.Error().Err(publishErr).
					Str("agreement_id", ag.ID.String()).
					Str("tenant_id", ag.TenantID.String()).
					Msg("roaming renewal: publish alert event")
			}
		}

		notified++
	}

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"total":    len(agreements),
		"notified": notified,
		"skipped":  skipped,
	})

	if err := p.jobs.Complete(ctx, j.ID, nil, resultJSON); err != nil {
		return fmt.Errorf("roaming_renewal: complete job: %w", err)
	}

	p.logger.Info().
		Int("total", len(agreements)).
		Int("notified", notified).
		Int("skipped", skipped).
		Msg("roaming renewal sweep completed")

	return nil
}
