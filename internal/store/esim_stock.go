package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrStockExhausted = errors.New("esim_stock: exhausted")

type EsimProfileStock struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	OperatorID uuid.UUID `json:"operator_id"`
	Total      int64     `json:"total"`
	Allocated  int64     `json:"allocated"`
	Available  int64     `json:"available"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func scanEsimProfileStock(row pgx.Row) (*EsimProfileStock, error) {
	var s EsimProfileStock
	err := row.Scan(&s.TenantID, &s.OperatorID, &s.Total, &s.Allocated, &s.Available, &s.UpdatedAt)
	return &s, err
}

type EsimProfileStockStore struct {
	db *pgxpool.Pool
}

func NewEsimProfileStockStore(db *pgxpool.Pool) *EsimProfileStockStore {
	return &EsimProfileStockStore{db: db}
}

func (s *EsimProfileStockStore) Allocate(ctx context.Context, tenantID, operatorID uuid.UUID) (*EsimProfileStock, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE esim_profile_stock
		SET allocated = allocated + 1, updated_at = NOW()
		WHERE tenant_id = $1 AND operator_id = $2 AND (total - allocated) > 0
		RETURNING tenant_id, operator_id, total, allocated, available, updated_at`,
		tenantID, operatorID,
	)
	stock, err := scanEsimProfileStock(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStockExhausted
	}
	if err != nil {
		return nil, fmt.Errorf("store: allocate esim stock: %w", err)
	}
	return stock, nil
}

func (s *EsimProfileStockStore) Deallocate(ctx context.Context, tenantID, operatorID uuid.UUID) (*EsimProfileStock, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE esim_profile_stock
		SET allocated = allocated - 1, updated_at = NOW()
		WHERE tenant_id = $1 AND operator_id = $2 AND allocated >= 1
		RETURNING tenant_id, operator_id, total, allocated, available, updated_at`,
		tenantID, operatorID,
	)
	stock, err := scanEsimProfileStock(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("store: deallocate esim stock: nothing to deallocate")
	}
	if err != nil {
		return nil, fmt.Errorf("store: deallocate esim stock: %w", err)
	}
	return stock, nil
}

func (s *EsimProfileStockStore) SetTotal(ctx context.Context, tenantID, operatorID uuid.UUID, total int64) (*EsimProfileStock, error) {
	row := s.db.QueryRow(ctx,
		`INSERT INTO esim_profile_stock (tenant_id, operator_id, total, allocated, updated_at)
		VALUES ($1, $2, $3, 0, NOW())
		ON CONFLICT (tenant_id, operator_id) DO UPDATE
		SET total = $3, updated_at = NOW()
		RETURNING tenant_id, operator_id, total, allocated, available, updated_at`,
		tenantID, operatorID, total,
	)
	stock, err := scanEsimProfileStock(row)
	if err != nil {
		return nil, fmt.Errorf("store: set total esim stock: %w", err)
	}
	return stock, nil
}

func (s *EsimProfileStockStore) Get(ctx context.Context, tenantID, operatorID uuid.UUID) (*EsimProfileStock, error) {
	row := s.db.QueryRow(ctx,
		`SELECT tenant_id, operator_id, total, allocated, available, updated_at
		FROM esim_profile_stock
		WHERE tenant_id = $1 AND operator_id = $2`,
		tenantID, operatorID,
	)
	stock, err := scanEsimProfileStock(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("store: get esim stock: not found for tenant=%s operator=%s", tenantID, operatorID)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get esim stock: %w", err)
	}
	return stock, nil
}

func (s *EsimProfileStockStore) ListSummary(ctx context.Context, tenantID uuid.UUID) ([]EsimProfileStock, error) {
	rows, err := s.db.Query(ctx,
		`SELECT tenant_id, operator_id, total, allocated, available, updated_at
		FROM esim_profile_stock
		WHERE tenant_id = $1
		ORDER BY operator_id`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list esim stock summary: %w", err)
	}
	defer rows.Close()

	var results []EsimProfileStock
	for rows.Next() {
		stock, err := scanEsimProfileStock(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan esim stock: %w", err)
		}
		results = append(results, *stock)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iter esim stock: %w", err)
	}
	return results, nil
}
