package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound         = errors.New("store: not found")
	ErrNoTenant         = errors.New("store: tenant_id not in context")
	ErrInvalidReference = errors.New("store: invalid foreign-key reference")
)

// InvalidReferenceError wraps ErrInvalidReference with the offending FK
// constraint name so handlers can translate it to a field-level 400.
type InvalidReferenceError struct {
	Constraint string
	Column     string
}

func (e *InvalidReferenceError) Error() string {
	if e.Column != "" {
		return fmt.Sprintf("store: invalid foreign-key reference (column=%s, constraint=%s)", e.Column, e.Constraint)
	}
	return fmt.Sprintf("store: invalid foreign-key reference (constraint=%s)", e.Constraint)
}

func (e *InvalidReferenceError) Unwrap() error { return ErrInvalidReference }

// constraintToColumn maps sims FK constraint names (from
// migrations/20260420000002_sims_fk_constraints) to the request field that
// caused the violation. Callers in sim.go pass a PG error here; other stores
// that add FK constraints can extend this map or provide their own mapping.
//
// PAT-006 reminder (FIX-206 Gate F-A8): when adding new FK constraints to
// other partitioned or non-partitioned tables (see ROUTEMAP tech debt
// D-062 sessions, D-063 cdrs, D-064 operator_health_logs, D-065 prod
// 10M-row cutover runbook), you MUST also:
//  1. Add a sibling <table>FKConstraintColumn map here (or extend this
//     one if the columns are disambiguable by constraint name alone).
//  2. Route store-layer Create/Update INSERT errors through
//     asInvalidReference() with that map, so handlers can surface a
//     field-level 400 INVALID_REFERENCE via *InvalidReferenceError.
//  3. Update docs/architecture/ERROR_CODES.md INVALID_REFERENCE row if
//     new field values are emitted in details.
//
// Otherwise the raw pgx error propagates as 500 INTERNAL_ERROR and the
// caller loses the hint about which field was invalid. There is no lint
// check enforcing this; the pattern is opt-in per store.
var simsFKConstraintColumn = map[string]string{
	"fk_sims_operator":   "operator_id",
	"fk_sims_apn":        "apn_id",
	"fk_sims_ip_address": "ip_address_id",
}

// asInvalidReference inspects err for a PG SQLSTATE 23503 (foreign_key_violation)
// and, when found, returns an *InvalidReferenceError wrapping ErrInvalidReference.
// Returns (nil, false) for non-FK errors so callers can fall through.
func asInvalidReference(err error, constraintColumn map[string]string) (*InvalidReferenceError, bool) {
	if err == nil {
		return nil, false
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		return nil, false
	}
	col := ""
	if constraintColumn != nil {
		col = constraintColumn[pgErr.ConstraintName]
	}
	return &InvalidReferenceError{Constraint: pgErr.ConstraintName, Column: col}, true
}

func TenantIDFromContext(ctx context.Context) (uuid.UUID, error) {
	v, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || v == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w", ErrNoTenant)
	}
	return v, nil
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "23505")
}
