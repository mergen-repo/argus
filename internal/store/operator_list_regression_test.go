package store

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testListPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Logf("skip: cannot connect to postgres: %v", err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Logf("skip: postgres ping failed: %v", err)
		return nil
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestOperatorColumnsAndScanCountConsistency(t *testing.T) {
	cols := strings.Split(strings.ReplaceAll(operatorColumns, "\n", ""), ",")
	count := 0
	for _, c := range cols {
		if strings.TrimSpace(c) != "" {
			count++
		}
	}
	// FIX-308-ext (2026-04-30): circuit_state added → 21 columns. PAT-006
	// regression invariant still applies — every column count change MUST
	// update the inline rows.Scan loops in List() and ListActive() in
	// lockstep with operatorColumns and scanOperator.
	if count != 21 {
		t.Fatalf("operatorColumns has %d columns; expected 21. If you intentionally changed the column count, update List() and ListActive() inline rows.Scan() destinations to match — see PAT-006 RECURRENCE in bug-patterns.md", count)
	}
}

func TestOperatorStore_List_RegressesOnInlineScanDriftFIX251(t *testing.T) {
	pool := testListPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated List regression test")
	}
	store := NewOperatorStore(pool)
	ctx := context.Background()

	id := uuid.New()
	suffix := id.String()[:8]
	_, err := pool.Exec(ctx,
		`INSERT INTO operators (id, name, code, mcc, mnc, adapter_config, sla_latency_threshold_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id,
		"list-regression-op-"+suffix,
		"l_reg_"+suffix,
		"997",
		"97",
		`{"mock":{"enabled":true}}`,
		1234,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id) })

	results, _, err := store.List(ctx, "", 1000, "")
	if err != nil {
		t.Fatalf("List failed (FIX-215 inline-scan drift regression — see PAT-006 RECURRENCE): %v", err)
	}
	var seen *Operator
	for i := range results {
		if results[i].ID == id {
			seen = &results[i]
			break
		}
	}
	if seen == nil {
		t.Fatalf("seeded operator %s not present in List() results", id)
	}
	if seen.SLALatencyThresholdMs != 1234 {
		t.Fatalf("SLALatencyThresholdMs not populated by List inline scan; got %d want 1234 — inline rows.Scan() is missing &o.SLALatencyThresholdMs", seen.SLALatencyThresholdMs)
	}
}

func TestOperatorStore_ListActive_RegressesOnInlineScanDriftFIX251(t *testing.T) {
	pool := testListPool(t)
	if pool == nil {
		t.Skip("DATABASE_URL not set; skipping DB-gated ListActive regression test")
	}
	store := NewOperatorStore(pool)
	ctx := context.Background()

	id := uuid.New()
	suffix := id.String()[:8]
	_, err := pool.Exec(ctx,
		`INSERT INTO operators (id, name, code, mcc, mnc, adapter_config, state, sla_latency_threshold_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, 'active', $7)`,
		id,
		"listactive-regression-op-"+suffix,
		"la_reg_"+suffix,
		"996",
		"96",
		`{"mock":{"enabled":true}}`,
		2345,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM operators WHERE id = $1`, id) })

	results, err := store.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive failed (FIX-215 inline-scan drift regression — see PAT-006 RECURRENCE): %v", err)
	}
	var seen *Operator
	for i := range results {
		if results[i].ID == id {
			seen = &results[i]
			break
		}
	}
	if seen == nil {
		t.Fatalf("seeded active operator %s not present in ListActive() results", id)
	}
	if seen.SLALatencyThresholdMs != 2345 {
		t.Fatalf("SLALatencyThresholdMs not populated by ListActive inline scan; got %d want 2345", seen.SLALatencyThresholdMs)
	}
}
