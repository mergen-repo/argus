package gateway

import (
	"context"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TenantLabel(ctx context.Context) string {
	if tid, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		return tid.String()
	}
	return "unknown"
}

func LoggerWith(ctx context.Context, base zerolog.Logger) zerolog.Logger {
	return base.With().
		Str("tenant_id", TenantLabel(ctx)).
		Str("correlation_id", GetCorrelationID(ctx)).
		Logger()
}
