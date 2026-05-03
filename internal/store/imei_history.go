package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IMEIHistoryStore manages the imei_history table.
// imei_history has RLS enabled with a tenant-isolation policy; the application
// role (argus_app) has BYPASSRLS. Tenant scoping is additionally enforced by
// passing tenant_id in all WHERE clauses and by verifying SIM ownership via SIMStore.
type IMEIHistoryStore struct {
	db  *pgxpool.Pool
	sim *SIMStore
}

func NewIMEIHistoryStore(db *pgxpool.Pool, sim *SIMStore) *IMEIHistoryStore {
	return &IMEIHistoryStore{db: db, sim: sim}
}

// IMEIHistoryRow is the read-model for a single imei_history entry.
// NASIPAddress is exposed as *string (SELECT nas_ip_address::text) for DTO simplicity.
type IMEIHistoryRow struct {
	ID                      uuid.UUID
	TenantID                uuid.UUID
	SIMID                   uuid.UUID
	ObservedIMEI            string
	ObservedSoftwareVersion *string
	ObservedAt              time.Time
	CaptureProtocol         string
	NASIPAddress            *string
	WasMismatch             bool
	AlarmRaised             bool
}

// ListIMEIHistoryParams controls pagination and optional filters for IMEIHistoryStore.List.
type ListIMEIHistoryParams struct {
	Cursor   string     // opaque; empty for first page
	Limit    int        // default 50, capped at 200
	Since    *time.Time // optional lower-bound on observed_at (inclusive)
	Protocol *string    // optional: "radius" / "diameter_s6a" / "5g_sba"
}

// AppendIMEIHistoryParams holds the fields for a new imei_history row.
type AppendIMEIHistoryParams struct {
	SIMID                   uuid.UUID
	ObservedIMEI            string
	ObservedSoftwareVersion *string
	CaptureProtocol         string
	NASIPAddress            *string
	WasMismatch             bool
	AlarmRaised             bool
}

// encodeCursor produces an opaque base64 cursor from (observed_at, id).
func encodeCursor(observedAt time.Time, id uuid.UUID) string {
	raw := observedAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses an opaque base64 cursor into (observed_at, id).
func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("store: invalid cursor encoding: %w", err)
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("store: malformed cursor")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("store: invalid cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("store: invalid cursor uuid: %w", err)
	}
	return t, id, nil
}

// List returns imei_history rows for a SIM, ordered by (observed_at DESC, id DESC)
// with cursor-based pagination. Cross-tenant simID returns ErrSIMNotFound.
func (s *IMEIHistoryStore) List(ctx context.Context, tenantID, simID uuid.UUID, params ListIMEIHistoryParams) ([]IMEIHistoryRow, string, error) {
	if params.Limit <= 0 || params.Limit > 200 {
		params.Limit = 50
	}

	if _, err := s.sim.GetByID(ctx, tenantID, simID); err != nil {
		return nil, "", err
	}

	conditions := []string{
		"tenant_id = $1",
		"sim_id = $2",
	}
	args := []interface{}{tenantID, simID}
	argIdx := 3

	if params.Cursor != "" {
		cursorTime, cursorID, err := decodeCursor(params.Cursor)
		if err != nil {
			return nil, "", err
		}
		conditions = append(conditions, fmt.Sprintf("(observed_at, id) < ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, cursorTime, cursorID)
		argIdx += 2
	}

	if params.Since != nil {
		conditions = append(conditions, fmt.Sprintf("observed_at >= $%d", argIdx))
		args = append(args, *params.Since)
		argIdx++
	}

	if params.Protocol != nil {
		conditions = append(conditions, fmt.Sprintf("capture_protocol = $%d", argIdx))
		args = append(args, *params.Protocol)
		argIdx++
	}

	args = append(args, params.Limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	query := fmt.Sprintf(`
		SELECT id, tenant_id, sim_id, observed_imei, observed_software_version,
		       observed_at, capture_protocol, nas_ip_address::text, was_mismatch, alarm_raised
		FROM imei_history
		WHERE %s
		ORDER BY observed_at DESC, id DESC
		LIMIT %s
	`, strings.Join(conditions, " AND "), limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list imei history: %w", err)
	}
	defer rows.Close()

	var results []IMEIHistoryRow
	for rows.Next() {
		var r IMEIHistoryRow
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.SIMID, &r.ObservedIMEI, &r.ObservedSoftwareVersion,
			&r.ObservedAt, &r.CaptureProtocol, &r.NASIPAddress, &r.WasMismatch, &r.AlarmRaised,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan imei history row: %w", err)
		}
		results = append(results, r)
	}
	if rows.Err() != nil {
		return nil, "", fmt.Errorf("store: imei history rows: %w", rows.Err())
	}

	nextCursor := ""
	if len(results) > params.Limit {
		last := results[params.Limit-1]
		nextCursor = encodeCursor(last.ObservedAt, last.ID)
		results = results[:params.Limit]
	}

	return results, nextCursor, nil
}

// Count returns the total number of imei_history rows for a SIM.
// Cross-tenant simID returns ErrSIMNotFound.
func (s *IMEIHistoryStore) Count(ctx context.Context, tenantID, simID uuid.UUID) (int, error) {
	if _, err := s.sim.GetByID(ctx, tenantID, simID); err != nil {
		return 0, err
	}
	var n int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM imei_history WHERE tenant_id = $1 AND sim_id = $2`,
		tenantID, simID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count imei history: %w", err)
	}
	return n, nil
}

// Append inserts a new imei_history row and returns its generated ID.
// STORY-094 stub — STORY-096 (binding enforcement) is the real consumer.
func (s *IMEIHistoryStore) Append(ctx context.Context, tenantID uuid.UUID, params AppendIMEIHistoryParams) (uuid.UUID, error) {
	if strings.TrimSpace(params.ObservedIMEI) == "" {
		return uuid.Nil, errors.New("store: observed_imei must not be empty")
	}

	var id uuid.UUID
	err := s.db.QueryRow(ctx,
		`INSERT INTO imei_history
		 (tenant_id, sim_id, observed_imei, observed_software_version,
		  capture_protocol, nas_ip_address, was_mismatch, alarm_raised)
		 VALUES ($1, $2, $3, $4, $5, $6::inet, $7, $8)
		 RETURNING id`,
		tenantID,
		params.SIMID,
		params.ObservedIMEI,
		params.ObservedSoftwareVersion,
		params.CaptureProtocol,
		params.NASIPAddress,
		params.WasMismatch,
		params.AlarmRaised,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: append imei history: %w", err)
	}
	return id, nil
}
