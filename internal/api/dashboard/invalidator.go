package dashboard

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/btopcu/argus/internal/bus"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const invalidatorQueue = "dashboard-invalidator"

var invalidationSubjects = []string{
	bus.SubjectSIMUpdated,
	bus.SubjectSessionStarted,
	bus.SubjectSessionEnded,
	bus.SubjectOperatorHealthChanged,
}

func RegisterDashboardInvalidator(eb *bus.EventBus, rc *redis.Client, logger zerolog.Logger) error {
	l := logger.With().Str("component", "dashboard_invalidator").Logger()

	for _, subject := range invalidationSubjects {
		subj := subject
		_, err := eb.QueueSubscribeCtx(subj, invalidatorQueue, func(ctx context.Context, _ string, data []byte) {
			var payload struct {
				TenantID string `json:"tenant_id"`
			}
			if err := json.Unmarshal(data, &payload); err != nil || payload.TenantID == "" {
				l.Debug().Str("subject", subj).Msg("dashboard invalidator: no tenant_id in payload, skipping")
				return
			}
			key := fmt.Sprintf("dashboard:%s", payload.TenantID)
			if delErr := rc.Del(ctx, key).Err(); delErr != nil {
				l.Warn().Err(delErr).Str("key", key).Msg("dashboard invalidator: redis DEL failed")
			}
		})
		if err != nil {
			return fmt.Errorf("dashboard: register invalidator for %s: %w", subj, err)
		}
		l.Info().Str("subject", subj).Msg("dashboard cache invalidator subscribed")
	}

	return nil
}

