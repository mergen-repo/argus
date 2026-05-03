package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SIMIMEIAllowlistStore manages the sim_imei_allowlist table.
// Cross-tenant guards are enforced at the store layer (AC-6): every mutating
// and read method first verifies that simID belongs to tenantID via SIMStore.GetByID.
// sim_imei_allowlist has RLS ENABLED but NO permissive policy — the application
// role (argus_app) has BYPASSRLS; store-layer tenant checks are the authoritative guard.
type SIMIMEIAllowlistStore struct {
	db  *pgxpool.Pool
	sim *SIMStore
}

func NewSIMIMEIAllowlistStore(db *pgxpool.Pool, sim *SIMStore) *SIMIMEIAllowlistStore {
	return &SIMIMEIAllowlistStore{db: db, sim: sim}
}

// verifyOwnership checks that simID belongs to tenantID. Returns ErrSIMNotFound for
// cross-tenant or missing SIM IDs so callers never touch the allowlist for foreign SIMs.
func (s *SIMIMEIAllowlistStore) verifyOwnership(ctx context.Context, tenantID, simID uuid.UUID) error {
	_, err := s.sim.GetByID(ctx, tenantID, simID)
	return err
}

// Add inserts (sim_id, imei) into the allowlist. Cross-tenant returns ErrSIMNotFound.
// Duplicate entries are silently ignored (ON CONFLICT DO NOTHING).
// imei must be non-empty; format validation is the caller's responsibility.
func (s *SIMIMEIAllowlistStore) Add(ctx context.Context, tenantID, simID uuid.UUID, imei string, addedBy *uuid.UUID) error {
	if strings.TrimSpace(imei) == "" {
		return errors.New("store: imei must not be empty")
	}
	if err := s.verifyOwnership(ctx, tenantID, simID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx,
		`INSERT INTO sim_imei_allowlist (sim_id, imei, added_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (sim_id, imei) DO NOTHING`,
		simID, imei, addedBy,
	)
	if err != nil {
		return fmt.Errorf("store: add imei allowlist: %w", err)
	}
	return nil
}

// Remove deletes (sim_id, imei) from the allowlist. Cross-tenant returns ErrSIMNotFound.
// No-op when the entry does not exist.
func (s *SIMIMEIAllowlistStore) Remove(ctx context.Context, tenantID, simID uuid.UUID, imei string) error {
	if err := s.verifyOwnership(ctx, tenantID, simID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx,
		`DELETE FROM sim_imei_allowlist WHERE sim_id = $1 AND imei = $2`,
		simID, imei,
	)
	if err != nil {
		return fmt.Errorf("store: remove imei allowlist: %w", err)
	}
	return nil
}

// List returns all IMEIs allowlisted for the given SIM. Cross-tenant returns ErrSIMNotFound.
func (s *SIMIMEIAllowlistStore) List(ctx context.Context, tenantID, simID uuid.UUID) ([]string, error) {
	if err := s.verifyOwnership(ctx, tenantID, simID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx,
		`SELECT imei FROM sim_imei_allowlist WHERE sim_id = $1 ORDER BY added_at`,
		simID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list imei allowlist: %w", err)
	}
	defer rows.Close()

	var imeis []string
	for rows.Next() {
		var imei string
		if err := rows.Scan(&imei); err != nil {
			return nil, fmt.Errorf("store: scan imei allowlist: %w", err)
		}
		imeis = append(imeis, imei)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("store: list imei allowlist rows: %w", rows.Err())
	}
	return imeis, nil
}

// IsAllowed reports whether (sim_id, imei) is present in the allowlist.
// Cross-tenant SIM IDs return (false, nil) — not an error.
func (s *SIMIMEIAllowlistStore) IsAllowed(ctx context.Context, tenantID, simID uuid.UUID, imei string) (bool, error) {
	if err := s.verifyOwnership(ctx, tenantID, simID); err != nil {
		if errors.Is(err, ErrSIMNotFound) {
			return false, nil
		}
		return false, err
	}
	var exists bool
	err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM sim_imei_allowlist WHERE sim_id = $1 AND imei = $2)`,
		simID, imei,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("store: is imei allowed: %w", err)
	}
	return exists, nil
}
