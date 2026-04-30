package job

import (
	"context"
	"encoding/json"

	"github.com/btopcu/argus/internal/audit"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// auditCreator is the minimal interface required by ArchiveRoamingKeywordPolicyVersions.
type auditCreator interface {
	CreateEntry(ctx context.Context, p audit.CreateEntryParams) (*audit.Entry, error)
}

type roamingPolicyVersionRow struct {
	id         string
	policyID   string
	tenantID   string
	version    int
	dslContent string
}

// ArchiveRoamingKeywordPolicyVersions is a boot-time one-shot that archives any
// policy_version rows whose dsl_content contains the 'roaming' keyword. It is
// idempotent: a second run finds zero matching rows and returns 0 immediately.
//
// Errors from individual row updates or audit entries are logged but do not
// abort the sweep — the function always continues to the next row.
// The returned count reflects successfully archived rows.
func ArchiveRoamingKeywordPolicyVersions(
	ctx context.Context,
	db *pgxpool.Pool,
	auditSvc auditCreator,
	log zerolog.Logger,
) (count int, err error) {
	rows, err := db.Query(ctx, `
		SELECT pv.id, pv.policy_id, pv.version, pv.dsl_content, p.tenant_id
		FROM policy_versions pv
		JOIN policies p ON p.id = pv.policy_id
		WHERE pv.dsl_content ILIKE '%roaming%'
		  AND pv.state != 'archived'
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var targets []roamingPolicyVersionRow
	for rows.Next() {
		var r roamingPolicyVersionRow
		if scanErr := rows.Scan(&r.id, &r.policyID, &r.version, &r.dslContent, &r.tenantID); scanErr != nil {
			log.Warn().Err(scanErr).Msg("roaming_keyword_archiver: scan row failed; skipping")
			continue
		}
		targets = append(targets, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, r := range targets {
		_, updateErr := db.Exec(ctx,
			`UPDATE policy_versions SET state = 'archived' WHERE id = $1`,
			r.id,
		)
		if updateErr != nil {
			log.Error().Err(updateErr).
				Str("policy_version_id", r.id).
				Msg("roaming_keyword_archiver: update state failed; skipping")
			continue
		}

		excerpt := r.dslContent
		if len(excerpt) > 200 {
			excerpt = excerpt[:200]
		}

		detailsJSON, marshalErr := json.Marshal(map[string]interface{}{
			"policy_id":   r.policyID,
			"version":     r.version,
			"dsl_excerpt": excerpt,
		})
		if marshalErr != nil {
			detailsJSON = []byte("{}")
		}

		_, auditErr := auditSvc.CreateEntry(ctx, audit.CreateEntryParams{
			Action:     "policy_version.archived_roaming_removed",
			EntityType: "policy_version",
			EntityID:   r.id,
			AfterData:  json.RawMessage(detailsJSON),
		})
		if auditErr != nil {
			log.Warn().Err(auditErr).
				Str("policy_version_id", r.id).
				Msg("roaming_keyword_archiver: audit entry failed; continuing")
		}

		count++
	}

	log.Info().Int("count", count).Msg("roaming_keyword_archiver: archived policy_versions containing roaming keyword")
	return count, nil
}
