package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPoolEntryDuplicate is returned by IMEIPoolStore.Add when the (tenant_id, imei_or_tac)
// UNIQUE constraint is violated (SQLSTATE 23505 with constraint name imei_*_unique_entry).
var ErrPoolEntryDuplicate = errors.New("store: imei pool entry duplicate")

// ErrPoolEntryNotFound is returned by IMEIPoolStore.Delete when the row does
// not exist for the given tenant — including cross-tenant attempts (the row
// either does not exist OR exists under a different tenant_id; both surface
// the same error to avoid leaking cross-tenant existence).
var ErrPoolEntryNotFound = errors.New("store: imei pool entry not found")

// IMEIPoolStore manages the three imei_* pool tables (whitelist/greylist/blacklist).
// All three tables share a common shape (id, tenant_id, kind, imei_or_tac, …);
// greylist adds quarantine_reason, blacklist adds block_reason + imported_from.
//
// RLS is enabled on all three with a tenant-isolation policy via app.current_tenant.
// The application role (argus_app) has BYPASSRLS, so tenant scoping is enforced
// here in the store layer via tenant_id parameter on every WHERE clause.
type IMEIPoolStore struct {
	db *pgxpool.Pool
}

// NewIMEIPoolStore constructs an IMEIPoolStore.
func NewIMEIPoolStore(db *pgxpool.Pool) *IMEIPoolStore {
	return &IMEIPoolStore{db: db}
}

// PoolEntry is the unified read-model for a row in any of the three pool
// tables. Fields that are absent on whitelist (and partially on greylist) are
// pointer-typed so the same struct can hold any kind of row without losing
// the NULL-vs-empty distinction (PAT-009).
type PoolEntry struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Pool             PoolKind
	Kind             string // EntryKindFullIMEI | EntryKindTACRange
	IMEIOrTAC        string
	DeviceModel      *string
	Description      *string
	QuarantineReason *string // greylist only — NULL on whitelist/blacklist rows
	BlockReason      *string // blacklist only — NULL on whitelist/greylist rows
	ImportedFrom     *string // blacklist only — NULL on whitelist/greylist rows
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time

	// BoundSIMsCount is populated by IMEIPoolStore.List only when
	// ListParams.IncludeBoundCount is true (STORY-096 D-189). Zero when not
	// requested. Counts SIMs whose bound_imei matches the entry per
	// entry.kind: full_imei → exact match; tac_range → first-8 prefix match.
	BoundSIMsCount int
}

// AddEntryParams holds the inputs for IMEIPoolStore.Add. Required-by-pool
// invariants (greylist needs QuarantineReason; blacklist needs BlockReason +
// ImportedFrom) are enforced at the store layer so handler/worker call sites
// cannot accidentally insert rows that violate the table NOT NULL constraints
// at runtime — failing fast in Go produces clearer errors than the pg layer.
type AddEntryParams struct {
	Kind             string  // full_imei | tac_range — must be in ValidEntryKinds
	IMEIOrTAC        string  // 15-digit IMEI for full_imei, 8-digit TAC for tac_range
	DeviceModel      *string // optional
	Description      *string // optional
	QuarantineReason *string // required for greylist
	BlockReason      *string // required for blacklist
	ImportedFrom     *string // required for blacklist; must be in ValidImportedFromValues
	CreatedBy        *uuid.UUID
}

// ListParams controls pagination and optional filters for IMEIPoolStore.List.
type ListParams struct {
	Cursor      string  // opaque; empty for first page
	Limit       int     // default 50, capped at 200
	TAC         *string // optional: exact-match prefix filter on imei_or_tac
	IMEI        *string // optional: exact-match filter on imei_or_tac
	DeviceModel *string // optional: case-insensitive substring (ILIKE %x%)

	// IncludeBoundCount enables the LEFT JOIN onto sims that populates
	// PoolEntry.BoundSIMsCount per row (STORY-096 D-189). Single-query
	// (no N+1): the per-row COUNT is computed inline via FILTER. When false,
	// BoundSIMsCount is left at zero and the join is skipped entirely.
	IncludeBoundCount bool
}

// LookupResult collects matches across all three pools for a single IMEI lookup.
// Each slice is independent — an IMEI may appear in zero, one, or multiple pools
// (a TAC blacklist entry can coexist with a full-IMEI whitelist entry, e.g.).
type LookupResult struct {
	Whitelist []PoolMatch
	Greylist  []PoolMatch
	Blacklist []PoolMatch
}

// PoolMatch is one row inside a LookupResult slice. MatchedVia indicates how
// the row was matched: "exact" for full_imei rows whose imei_or_tac equals
// the queried IMEI, or "tac_range" for tac_range rows whose imei_or_tac
// equals the first 8 digits of the queried IMEI.
type PoolMatch struct {
	EntryID          uuid.UUID
	Kind             string // EntryKindFullIMEI | EntryKindTACRange
	IMEIOrTAC        string
	MatchedVia       string // "exact" | "tac_range"
	DeviceModel      *string
	Description      *string
	QuarantineReason *string // greylist only
	BlockReason      *string // blacklist only
	ImportedFrom     *string // blacklist only
	CreatedAt        time.Time
}

// validateAddParams enforces store-layer invariants on AddEntryParams.
// Handlers/workers should call IsValidEntryKind / IsValidImportedFrom before
// calling Add, but this guard prevents an invalid INSERT from ever hitting
// the database. Returns a plain error (not a sentinel) — callers should treat
// validation failures as 400/422 / row-level outcome at the boundary.
func validateAddParams(pool PoolKind, p AddEntryParams) error {
	if !IsValidPoolKind(pool) {
		return fmt.Errorf("store: invalid pool kind %q", pool)
	}
	if !IsValidEntryKind(p.Kind) {
		return fmt.Errorf("store: invalid entry kind %q", p.Kind)
	}
	if strings.TrimSpace(p.IMEIOrTAC) == "" {
		return errors.New("store: imei_or_tac must not be empty")
	}
	switch p.Kind {
	case EntryKindFullIMEI:
		if len(p.IMEIOrTAC) != 15 {
			return fmt.Errorf("store: full_imei imei_or_tac must be 15 chars, got %d", len(p.IMEIOrTAC))
		}
	case EntryKindTACRange:
		if len(p.IMEIOrTAC) != 8 {
			return fmt.Errorf("store: tac_range imei_or_tac must be 8 chars, got %d", len(p.IMEIOrTAC))
		}
	}
	switch pool {
	case PoolGreylist:
		if p.QuarantineReason == nil || strings.TrimSpace(*p.QuarantineReason) == "" {
			return errors.New("store: greylist add requires non-empty quarantine_reason")
		}
	case PoolBlacklist:
		if p.BlockReason == nil || strings.TrimSpace(*p.BlockReason) == "" {
			return errors.New("store: blacklist add requires non-empty block_reason")
		}
		if p.ImportedFrom == nil || !IsValidImportedFrom(*p.ImportedFrom) {
			got := ""
			if p.ImportedFrom != nil {
				got = *p.ImportedFrom
			}
			return fmt.Errorf("store: blacklist add requires valid imported_from, got %q", got)
		}
	}
	return nil
}

// Add inserts a new entry into the pool table identified by `pool`. Returns
// ErrPoolEntryDuplicate on UNIQUE (tenant_id, imei_or_tac) violation.
// Validation errors are returned as plain errors (not sentinels).
func (s *IMEIPoolStore) Add(ctx context.Context, tenantID uuid.UUID, pool PoolKind, p AddEntryParams) (*PoolEntry, error) {
	if err := validateAddParams(pool, p); err != nil {
		return nil, err
	}
	table := tableNameForKind(pool)
	if table == "" {
		return nil, fmt.Errorf("store: invalid pool kind %q", pool)
	}

	var (
		query string
		args  []interface{}
	)
	switch pool {
	case PoolWhitelist:
		// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
		query = fmt.Sprintf(`
			INSERT INTO %s
			  (tenant_id, kind, imei_or_tac, device_model, description, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, tenant_id, kind, imei_or_tac, device_model, description,
			          created_by, created_at, updated_at`, table)
		args = []interface{}{tenantID, p.Kind, p.IMEIOrTAC, p.DeviceModel, p.Description, p.CreatedBy}
	case PoolGreylist:
		// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
		query = fmt.Sprintf(`
			INSERT INTO %s
			  (tenant_id, kind, imei_or_tac, device_model, description,
			   quarantine_reason, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, tenant_id, kind, imei_or_tac, device_model, description,
			          quarantine_reason, created_by, created_at, updated_at`, table)
		args = []interface{}{tenantID, p.Kind, p.IMEIOrTAC, p.DeviceModel, p.Description, p.QuarantineReason, p.CreatedBy}
	case PoolBlacklist:
		// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
		query = fmt.Sprintf(`
			INSERT INTO %s
			  (tenant_id, kind, imei_or_tac, device_model, description,
			   block_reason, imported_from, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, tenant_id, kind, imei_or_tac, device_model, description,
			          block_reason, imported_from, created_by, created_at, updated_at`, table)
		args = []interface{}{tenantID, p.Kind, p.IMEIOrTAC, p.DeviceModel, p.Description, p.BlockReason, p.ImportedFrom, p.CreatedBy}
	}

	entry := &PoolEntry{Pool: pool}
	row := s.db.QueryRow(ctx, query, args...)

	var err error
	switch pool {
	case PoolWhitelist:
		err = row.Scan(
			&entry.ID, &entry.TenantID, &entry.Kind, &entry.IMEIOrTAC,
			&entry.DeviceModel, &entry.Description,
			&entry.CreatedBy, &entry.CreatedAt, &entry.UpdatedAt,
		)
	case PoolGreylist:
		err = row.Scan(
			&entry.ID, &entry.TenantID, &entry.Kind, &entry.IMEIOrTAC,
			&entry.DeviceModel, &entry.Description, &entry.QuarantineReason,
			&entry.CreatedBy, &entry.CreatedAt, &entry.UpdatedAt,
		)
	case PoolBlacklist:
		err = row.Scan(
			&entry.ID, &entry.TenantID, &entry.Kind, &entry.IMEIOrTAC,
			&entry.DeviceModel, &entry.Description, &entry.BlockReason, &entry.ImportedFrom,
			&entry.CreatedBy, &entry.CreatedAt, &entry.UpdatedAt,
		)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			strings.HasSuffix(pgErr.ConstraintName, "_unique_entry") {
			return nil, ErrPoolEntryDuplicate
		}
		return nil, fmt.Errorf("store: add imei pool entry: %w", err)
	}
	return entry, nil
}

// Delete removes an entry from the pool table identified by `pool`. Returns
// ErrPoolEntryNotFound when the row does not exist for the tenant (including
// cross-tenant attempts — the row is invisible to other tenants).
func (s *IMEIPoolStore) Delete(ctx context.Context, tenantID uuid.UUID, pool PoolKind, entryID uuid.UUID) error {
	table := tableNameForKind(pool)
	if table == "" {
		return fmt.Errorf("store: invalid pool kind %q", pool)
	}
	// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
	query := fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND tenant_id = $2`, table)
	tag, err := s.db.Exec(ctx, query, entryID, tenantID)
	if err != nil {
		return fmt.Errorf("store: delete imei pool entry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPoolEntryNotFound
	}
	return nil
}

// List returns rows from the pool table identified by `pool`, ordered by
// (created_at DESC, id DESC) with cursor-based pagination. The cursor scheme
// matches imei_history (base64 of `<created_at_RFC3339Nano>|<id>`).
func (s *IMEIPoolStore) List(ctx context.Context, tenantID uuid.UUID, pool PoolKind, params ListParams) ([]PoolEntry, string, error) {
	table := tableNameForKind(pool)
	if table == "" {
		return nil, "", fmt.Errorf("store: invalid pool kind %q", pool)
	}
	if params.Limit <= 0 || params.Limit > 200 {
		params.Limit = 50
	}

	conditions := []string{"tenant_id = $1"}
	args := []interface{}{tenantID}
	argIdx := 2

	if params.Cursor != "" {
		cursorTime, cursorID, err := decodeCursor(params.Cursor)
		if err != nil {
			return nil, "", err
		}
		conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d)", argIdx, argIdx+1))
		args = append(args, cursorTime, cursorID)
		argIdx += 2
	}
	if params.IMEI != nil && *params.IMEI != "" {
		conditions = append(conditions, fmt.Sprintf("imei_or_tac = $%d", argIdx))
		args = append(args, *params.IMEI)
		argIdx++
	}
	if params.TAC != nil && *params.TAC != "" {
		// TAC filter matches tac_range rows whose imei_or_tac equals the TAC
		// AND full_imei rows whose imei_or_tac starts with the TAC.
		conditions = append(conditions, fmt.Sprintf(
			"((kind = 'tac_range' AND imei_or_tac = $%d) OR (kind = 'full_imei' AND imei_or_tac LIKE $%d))",
			argIdx, argIdx+1,
		))
		args = append(args, *params.TAC, *params.TAC+"%")
		argIdx += 2
	}
	if params.DeviceModel != nil && *params.DeviceModel != "" {
		conditions = append(conditions, fmt.Sprintf("device_model ILIKE $%d", argIdx))
		args = append(args, "%"+*params.DeviceModel+"%")
		argIdx++
	}

	limitArg := params.Limit + 1
	args = append(args, limitArg)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	// Build per-pool projection — whitelist has no quarantine/block columns; we
	// pad those with NULL casts so the same scan path serves all three. The
	// projection is qualified with `e.` so the optional LEFT JOIN onto sims
	// (IncludeBoundCount) does not disambiguate-conflict on the shared
	// tenant_id column.
	var projection string
	switch pool {
	case PoolWhitelist:
		projection = `e.id, e.tenant_id, e.kind, e.imei_or_tac, e.device_model, e.description,
		              NULL::text AS quarantine_reason,
		              NULL::text AS block_reason,
		              NULL::text AS imported_from,
		              e.created_by, e.created_at, e.updated_at`
	case PoolGreylist:
		projection = `e.id, e.tenant_id, e.kind, e.imei_or_tac, e.device_model, e.description,
		              e.quarantine_reason,
		              NULL::text AS block_reason,
		              NULL::text AS imported_from,
		              e.created_by, e.created_at, e.updated_at`
	case PoolBlacklist:
		projection = `e.id, e.tenant_id, e.kind, e.imei_or_tac, e.device_model, e.description AS desc_alias,
		              NULL::text AS quarantine_reason,
		              e.block_reason,
		              e.imported_from,
		              e.created_by, e.created_at, e.updated_at`
	}

	// Re-qualify the WHERE conditions: build a parallel slice that prefixes
	// known column names with `e.` so the LEFT JOIN form does not raise an
	// ambiguous-column error on tenant_id / kind / imei_or_tac.
	whereConds := make([]string, 0, len(conditions))
	for _, c := range conditions {
		// The conditions are constructed locally above; tenant_id is at index
		// 0, the cursor uses the (created_at, id) pair, and the optional
		// filters reference imei_or_tac / device_model. All these are columns
		// on the pool table, so a blanket `e.` prefix on bare column names is
		// safe. Replace the leading bare-token references via simple swaps.
		c = strings.Replace(c, "tenant_id =", "e.tenant_id =", 1)
		c = strings.Replace(c, "(created_at, id)", "(e.created_at, e.id)", 1)
		c = strings.Replace(c, "imei_or_tac =", "e.imei_or_tac =", 2)
		c = strings.Replace(c, "imei_or_tac LIKE", "e.imei_or_tac LIKE", 1)
		c = strings.Replace(c, "kind = 'tac_range'", "e.kind = 'tac_range'", 1)
		c = strings.Replace(c, "kind = 'full_imei'", "e.kind = 'full_imei'", 1)
		c = strings.Replace(c, "device_model ILIKE", "e.device_model ILIKE", 1)
		whereConds = append(whereConds, c)
	}

	// Prefix the order-by cursor with the alias too.
	orderBy := "ORDER BY e.created_at DESC, e.id DESC"

	if params.IncludeBoundCount {
		// Single-query (no N+1) bound_sims_count: LEFT JOIN sims with both
		// match predicates and aggregate via COUNT() FILTER + GROUP BY. The
		// projection becomes `... , COALESCE(...) AS bound_count` and the
		// projection's `e.description` is preserved verbatim for the blacklist
		// path (we restore the column name below).
		boundCountExpr := `COALESCE(COUNT(s.id) FILTER (WHERE s.bound_imei IS NOT NULL), 0) AS bound_count`
		// For Blacklist the projection above uses an alias `desc_alias` for
		// description to keep the column ordering uniform; restore it here.
		if pool == PoolBlacklist {
			projection = strings.Replace(projection, "e.description AS desc_alias", "e.description", 1)
		}
		// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
		query := fmt.Sprintf(`
			SELECT %s, %s
			FROM %s e
			LEFT JOIN sims s
			       ON s.tenant_id = e.tenant_id
			      AND s.bound_imei IS NOT NULL
			      AND ( (e.kind = 'full_imei' AND s.bound_imei = e.imei_or_tac)
			         OR (e.kind = 'tac_range' AND LEFT(s.bound_imei, 8) = e.imei_or_tac) )
			WHERE %s
			GROUP BY e.id
			%s
			LIMIT %s
		`, projection, boundCountExpr, table, strings.Join(whereConds, " AND "), orderBy, limitPlaceholder)

		rows, err := s.db.Query(ctx, query, args...)
		if err != nil {
			return nil, "", fmt.Errorf("store: list imei pool entries with bound count: %w", err)
		}
		defer rows.Close()

		var results []PoolEntry
		for rows.Next() {
			e := PoolEntry{Pool: pool}
			var boundCount int64
			if err := rows.Scan(
				&e.ID, &e.TenantID, &e.Kind, &e.IMEIOrTAC,
				&e.DeviceModel, &e.Description,
				&e.QuarantineReason, &e.BlockReason, &e.ImportedFrom,
				&e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
				&boundCount,
			); err != nil {
				return nil, "", fmt.Errorf("store: scan imei pool entry with bound count: %w", err)
			}
			e.BoundSIMsCount = int(boundCount)
			results = append(results, e)
		}
		if rows.Err() != nil {
			return nil, "", fmt.Errorf("store: list imei pool rows with bound count: %w", rows.Err())
		}

		nextCursor := ""
		if len(results) > params.Limit {
			last := results[params.Limit-1]
			nextCursor = encodeCursor(last.CreatedAt, last.ID)
			results = results[:params.Limit]
		}
		return results, nextCursor, nil
	}

	// Restore the blacklist's `e.description` so the non-bound-count path
	// scans back into Description correctly.
	if pool == PoolBlacklist {
		projection = strings.Replace(projection, "e.description AS desc_alias", "e.description", 1)
	}

	// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
	query := fmt.Sprintf(`
		SELECT %s
		FROM %s e
		WHERE %s
		%s
		LIMIT %s
	`, projection, table, strings.Join(whereConds, " AND "), orderBy, limitPlaceholder)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("store: list imei pool entries: %w", err)
	}
	defer rows.Close()

	var results []PoolEntry
	for rows.Next() {
		e := PoolEntry{Pool: pool}
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.Kind, &e.IMEIOrTAC,
			&e.DeviceModel, &e.Description,
			&e.QuarantineReason, &e.BlockReason, &e.ImportedFrom,
			&e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan imei pool entry: %w", err)
		}
		results = append(results, e)
	}
	if rows.Err() != nil {
		return nil, "", fmt.Errorf("store: list imei pool rows: %w", rows.Err())
	}

	nextCursor := ""
	if len(results) > params.Limit {
		last := results[params.Limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
		results = results[:params.Limit]
	}
	return results, nextCursor, nil
}

// lookupPoolMatches runs a single-pool lookup and returns the matching rows.
// `imei` is the full 15-digit IMEI; `tac` is the first 8 digits. When `imei`
// is shorter than 15, only the TAC branch is queried (`tac` must still be 8).
func (s *IMEIPoolStore) lookupPoolMatches(ctx context.Context, tenantID uuid.UUID, pool PoolKind, imei, tac string) ([]PoolMatch, error) {
	table := tableNameForKind(pool)
	if table == "" {
		return nil, fmt.Errorf("store: invalid pool kind %q", pool)
	}

	// matched_via projection: prefer "exact" when the imei_or_tac equals the
	// full IMEI AND kind=full_imei; else "tac_range" when it equals the TAC
	// AND kind=tac_range. The CASE order matches the WHERE branches.
	var projection string
	switch pool {
	case PoolWhitelist:
		projection = `id, kind, imei_or_tac, device_model, description,
		              NULL::text AS quarantine_reason,
		              NULL::text AS block_reason,
		              NULL::text AS imported_from,
		              created_at`
	case PoolGreylist:
		projection = `id, kind, imei_or_tac, device_model, description,
		              quarantine_reason,
		              NULL::text AS block_reason,
		              NULL::text AS imported_from,
		              created_at`
	case PoolBlacklist:
		projection = `id, kind, imei_or_tac, device_model, description,
		              NULL::text AS quarantine_reason,
		              block_reason,
		              imported_from,
		              created_at`
	}

	// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
	query := fmt.Sprintf(`
		SELECT %s,
		       CASE
		         WHEN imei_or_tac = $1 AND kind = 'full_imei' THEN 'exact'
		         WHEN imei_or_tac = $2 AND kind = 'tac_range' THEN 'tac_range'
		       END AS matched_via
		FROM %s
		WHERE tenant_id = $3
		  AND (
		        (imei_or_tac = $1 AND kind = 'full_imei')
		     OR (imei_or_tac = $2 AND kind = 'tac_range')
		      )
	`, projection, table)

	rows, err := s.db.Query(ctx, query, imei, tac, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: lookup imei pool: %w", err)
	}
	defer rows.Close()

	var matches []PoolMatch
	for rows.Next() {
		var m PoolMatch
		if err := rows.Scan(
			&m.EntryID, &m.Kind, &m.IMEIOrTAC, &m.DeviceModel, &m.Description,
			&m.QuarantineReason, &m.BlockReason, &m.ImportedFrom,
			&m.CreatedAt, &m.MatchedVia,
		); err != nil {
			return nil, fmt.Errorf("store: scan lookup match: %w", err)
		}
		matches = append(matches, m)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("store: lookup rows: %w", rows.Err())
	}
	return matches, nil
}

// Lookup queries all three pools for matches against `imei`. Matches are
// returned partitioned by pool. `imei` MUST be a 15-digit IMEI; the first
// 8 digits are derived as the TAC for tac_range comparison. An IMEI that
// is not 15 digits returns an error (callers should validate at the boundary).
func (s *IMEIPoolStore) Lookup(ctx context.Context, tenantID uuid.UUID, imei string) (*LookupResult, error) {
	if len(imei) != 15 {
		return nil, fmt.Errorf("store: lookup imei must be 15 digits, got %d", len(imei))
	}
	tac := imei[:8]

	result := &LookupResult{}
	type chanResp struct {
		pool    PoolKind
		matches []PoolMatch
		err     error
	}
	ch := make(chan chanResp, 3)
	for _, pool := range []PoolKind{PoolWhitelist, PoolGreylist, PoolBlacklist} {
		go func(p PoolKind) {
			matches, err := s.lookupPoolMatches(ctx, tenantID, p, imei, tac)
			ch <- chanResp{pool: p, matches: matches, err: err}
		}(pool)
	}
	for i := 0; i < 3; i++ {
		resp := <-ch
		if resp.err != nil {
			return nil, resp.err
		}
		switch resp.pool {
		case PoolWhitelist:
			result.Whitelist = resp.matches
		case PoolGreylist:
			result.Greylist = resp.matches
		case PoolBlacklist:
			result.Blacklist = resp.matches
		}
	}
	return result, nil
}

// LookupKind returns whether `imei` matches at least one row in the named
// pool, either as a full_imei exact match or as a tac_range match (first
// 8 digits). Designed for the DSL evaluator (Task 6) — single boolean
// existence check, no detail. Empty/short imei returns (false, nil).
func (s *IMEIPoolStore) LookupKind(ctx context.Context, tenantID uuid.UUID, pool PoolKind, imei string) (bool, error) {
	if len(imei) != 15 {
		return false, nil
	}
	table := tableNameForKind(pool)
	if table == "" {
		return false, fmt.Errorf("store: invalid pool kind %q", pool)
	}
	tac := imei[:8]
	// #nosec G201 — table is one of three whitelisted constants from tableNameForKind.
	query := fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1 FROM %s
			WHERE tenant_id = $3
			  AND (
			        (imei_or_tac = $1 AND kind = 'full_imei')
			     OR (imei_or_tac = $2 AND kind = 'tac_range')
			      )
		)`, table)
	var exists bool
	err := s.db.QueryRow(ctx, query, imei, tac, tenantID).Scan(&exists)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("store: lookup kind: %w", err)
	}
	return exists, nil
}
