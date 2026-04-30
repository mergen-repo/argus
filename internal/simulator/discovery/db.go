// Package discovery loads the simulator's SIM inventory directly from the
// Argus PostgreSQL database using a read-only role (argus_sim). This is the
// pragmatic choice for a dev-only tool sharing the docker-compose network;
// see STORY-082 plan for the rationale.
package discovery

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SIM is the minimal projection the simulator needs to run a session.
// Fields come from joining sims + operators + apns.
type SIM struct {
	ID           string
	TenantID     string
	OperatorID   string
	OperatorCode string
	MCC          string
	MNC          string
	APNID        *string
	APNName      *string
	IMSI         string
	MSISDN       *string
	ICCID        string
}

// Store wraps a read-only pgx pool bound to the argus_sim role.
type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, dbURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	// Bounded pool — discovery is light-touch.
	cfg.MaxConns = 2
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// ListActiveSIMs returns every SIM with state='active' whose operator is
// NOT the 'mock' upstream adapter. Ordered deterministically for easier
// log/test diffing.
func (s *Store) ListActiveSIMs(ctx context.Context) ([]SIM, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id::text, s.tenant_id::text, s.operator_id::text,
		       o.code, o.mcc, o.mnc,
		       s.apn_id::text, a.name,
		       s.imsi, s.msisdn, s.iccid
		FROM sims s
		JOIN operators o ON o.id = s.operator_id
		LEFT JOIN apns a ON a.id = s.apn_id
		WHERE s.state = 'active' AND o.code <> 'mock'
		ORDER BY o.code, s.imsi
	`)
	if err != nil {
		return nil, fmt.Errorf("query sims: %w", err)
	}
	defer rows.Close()

	var out []SIM
	for rows.Next() {
		var si SIM
		var apnID, apnName *string
		if err := rows.Scan(&si.ID, &si.TenantID, &si.OperatorID,
			&si.OperatorCode, &si.MCC, &si.MNC,
			&apnID, &apnName,
			&si.IMSI, &si.MSISDN, &si.ICCID); err != nil {
			return nil, fmt.Errorf("scan sim row: %w", err)
		}
		si.APNID = apnID
		si.APNName = apnName
		out = append(out, si)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sims: %w", err)
	}
	return out, nil
}
