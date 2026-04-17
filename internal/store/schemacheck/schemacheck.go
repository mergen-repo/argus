package schemacheck

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CriticalTables lists the tables that must be present after migrations run.
// Boot fails fast if any is missing — a schema-drift guard for STORY-069 and
// STORY-077 era tables after the 2026-04-17 sms_outbound live-env drift.
// Keep this list sorted alphabetically for stable diffs.
var CriticalTables = []string{
	"announcement_dismissals",
	"announcements",
	"chart_annotations",
	"notification_preferences",
	"notification_templates", // global seed table — no RLS (per STORY-069)
	"onboarding_sessions",
	"scheduled_reports",
	"sms_outbound",
	"user_column_preferences",
	"user_views",
	"webhook_configs",
	"webhook_deliveries",
}

// Verify checks that every table in tables exists in the public schema.
// Returns an error listing the missing tables if any are absent.
// Callers should treat a non-nil return as fatal at boot.
func Verify(ctx context.Context, pool *pgxpool.Pool, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	var missing []string
	for _, name := range tables {
		var exists bool
		err := pool.QueryRow(ctx, "SELECT to_regclass('public.' || $1) IS NOT NULL", name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("schemacheck: probing %q: %w", name, err)
		}
		if !exists {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("schemacheck: critical tables missing from database: %v", missing)
	}
	return nil
}
