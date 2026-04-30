package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestListEnriched_Explain_IndexScan_NoSeqScan verifies that the ListEnriched
// query plan does not degrade to a root-level Seq Scan on the sims table, and
// that total execution time stays below 100 ms for a 1,000-row data set.
//
// Gate conditions:
//   - testing.Short() → skip (fast CI)
//   - DATABASE_URL unset → skip (no live DB)
func TestListEnriched_Explain_IndexScan_NoSeqScan(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping explain integration test in short mode")
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("skipping explain integration test: DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("postgres ping failed: %v", err)
	}

	f := seedExplainFixture(t, pool)

	bulkInsertSIMs(t, pool, f, 1000)

	_, _ = pool.Exec(ctx, `ANALYZE sims`)
	_, _ = pool.Exec(ctx, `ANALYZE operators`)
	_, _ = pool.Exec(ctx, `ANALYZE apns`)
	_, _ = pool.Exec(ctx, `ANALYZE policies`)
	_, _ = pool.Exec(ctx, `ANALYZE policy_versions`)

	explainSQL := fmt.Sprintf(
		`EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) SELECT %s FROM sims s %s WHERE s.tenant_id = $1 ORDER BY s.created_at DESC, s.id DESC LIMIT 51`,
		simEnrichedSelect,
		simEnrichedJoin,
	)

	rows, err := pool.Query(ctx, explainSQL, f.tenantID)
	if err != nil {
		t.Fatalf("EXPLAIN query failed: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if scanErr := rows.Scan(&line); scanErr != nil {
			rows.Close()
			t.Fatalf("scan explain row: %v", scanErr)
		}
		lines = append(lines, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows iteration: %v", err)
	}

	output := strings.Join(lines, "\n")

	var failures []string

	if matchSeqScanRoot(output, "sims") {
		failures = append(failures, "FAIL: Seq Scan on sims root (not a partition child) — missing index on tenant_id")
	}

	execMs, parseOK := parseExecTimeMS(output)
	if !parseOK {
		failures = append(failures, "FAIL: could not parse 'Execution Time' from EXPLAIN output")
	} else if execMs >= 100.0 {
		failures = append(failures, fmt.Sprintf("FAIL: Execution Time %.2f ms >= 100 ms threshold", execMs))
	}

	if len(failures) > 0 {
		writeExplainEvidence(t, output)
		for _, f := range failures {
			t.Error(f)
		}
	}

	if !t.Failed() {
		t.Logf("EXPLAIN ANALYZE passed: execution=%.2f ms, plan lines=%d", execMs, len(lines))
	}
}

// explainFixture holds IDs seeded for the EXPLAIN test.
type explainFixture struct {
	tenantID        uuid.UUID
	operatorIDs     []uuid.UUID
	apnIDs          []uuid.UUID
	policyVersionID uuid.UUID
	policyIDs       []uuid.UUID
}

// seedExplainFixture creates:
//   - 1 tenant
//   - up to 3 existing operators (global, no INSERT needed)
//   - 10 APNs across the operators
//   - 10 policies + 1 active policy_version each
//
// Seeding enough parent rows gives the planner reason to prefer index scans
// over seq scans on those tables, making the plan more representative.
func seedExplainFixture(t *testing.T, pool *pgxpool.Pool) explainFixture {
	t.Helper()
	ctx := context.Background()
	var f explainFixture

	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, contact_email)
		VALUES ('explain-'||gen_random_uuid()::text, 'explain@test.argus')
		RETURNING id`).Scan(&f.tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	operatorRows, err := pool.Query(ctx, `SELECT id FROM operators LIMIT 3`)
	if err != nil {
		t.Fatalf("fetch operators: %v", err)
	}
	for operatorRows.Next() {
		var opID uuid.UUID
		if err := operatorRows.Scan(&opID); err != nil {
			operatorRows.Close()
			t.Fatalf("scan operator: %v", err)
		}
		f.operatorIDs = append(f.operatorIDs, opID)
	}
	operatorRows.Close()
	if err := operatorRows.Err(); err != nil {
		t.Fatalf("operator rows err: %v", err)
	}
	if len(f.operatorIDs) == 0 {
		t.Fatal("no operator rows available in DB — seed DB first")
	}

	for i := 0; i < 10; i++ {
		opID := f.operatorIDs[i%len(f.operatorIDs)]
		var apnID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO apns (tenant_id, operator_id, name, display_name, apn_type, state)
			VALUES ($1, $2, 'explain-apn-'||$3||'-'||gen_random_uuid()::text, 'Explain APN '||$3, 'iot', 'active')
			RETURNING id`, f.tenantID, opID, i).Scan(&apnID); err != nil {
			t.Fatalf("seed apn %d: %v", i, err)
		}
		f.apnIDs = append(f.apnIDs, apnID)
	}

	for i := 0; i < 10; i++ {
		var policyID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO policies (tenant_id, name, scope, state)
			VALUES ($1, 'explain-policy-'||$2||'-'||gen_random_uuid()::text, 'global', 'active')
			RETURNING id`, f.tenantID, i).Scan(&policyID); err != nil {
			t.Fatalf("seed policy %d: %v", i, err)
		}
		f.policyIDs = append(f.policyIDs, policyID)

		var pvID uuid.UUID
		if err := pool.QueryRow(ctx, `
			INSERT INTO policy_versions (policy_id, version, dsl_content, compiled_rules, state)
			VALUES ($1, 1, 'allow all;', '{}', 'active')
			RETURNING id`, policyID).Scan(&pvID); err != nil {
			t.Fatalf("seed policy_version %d: %v", i, err)
		}
		if i == 0 {
			f.policyVersionID = pvID
		}
	}

	t.Cleanup(func() {
		cctx := context.Background()
		_, _ = pool.Exec(cctx, `DELETE FROM sims WHERE tenant_id = $1`, f.tenantID)
		for _, policyID := range f.policyIDs {
			_, _ = pool.Exec(cctx, `DELETE FROM policy_versions WHERE policy_id = $1`, policyID)
		}
		_, _ = pool.Exec(cctx, `DELETE FROM policies WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM apns WHERE tenant_id = $1`, f.tenantID)
		_, _ = pool.Exec(cctx, `DELETE FROM tenants WHERE id = $1`, f.tenantID)
	})

	return f
}

// bulkInsertSIMs inserts n SIMs distributed across the fixture's operators and
// APNs using pgx CopyFrom for performance.
func bulkInsertSIMs(t *testing.T, pool *pgxpool.Pool, f explainFixture, n int) {
	t.Helper()

	type simRow struct {
		tenantID        uuid.UUID
		operatorID      uuid.UUID
		apnID           uuid.UUID
		iccid           string
		imsi            string
		policyVersionID uuid.UUID
	}

	rows := make([]simRow, n)
	for i := 0; i < n; i++ {
		opID := f.operatorIDs[i%len(f.operatorIDs)]
		apnID := f.apnIDs[i%len(f.apnIDs)]
		nonce := uuid.New().ID() % 1_000_000_000
		rows[i] = simRow{
			tenantID:        f.tenantID,
			operatorID:      opID,
			apnID:           apnID,
			iccid:           fmt.Sprintf("89931%02d%09d", i%100, nonce),
			imsi:            fmt.Sprintf("28631%02d%08d", i%100, nonce%100_000_000),
			policyVersionID: f.policyVersionID,
		}
	}

	copySource := pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
		r := rows[i]
		return []any{
			r.tenantID,
			r.operatorID,
			r.apnID,
			r.iccid,
			r.imsi,
			"physical",
			"ordered",
			r.policyVersionID,
		}, nil
	})

	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for COPY: %v", err)
	}
	defer conn.Release()

	n64, err := conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{"sims"},
		[]string{"tenant_id", "operator_id", "apn_id", "iccid", "imsi", "sim_type", "state", "policy_version_id"},
		copySource,
	)
	if err != nil {
		t.Fatalf("CopyFrom sims: %v", err)
	}
	if n64 != int64(n) {
		t.Fatalf("CopyFrom inserted %d rows, want %d", n64, n)
	}
}

// matchSeqScanRoot returns true when the EXPLAIN output contains a Seq Scan on
// the root (non-partition) table name.  Partition-child scans like
// "Seq Scan on sims_default" are intentional and acceptable.
func matchSeqScanRoot(output, tableName string) bool {
	re := regexp.MustCompile(`(?i)Seq Scan on ` + regexp.QuoteMeta(tableName) + `\s`)
	return re.MatchString(output)
}

// parseExecTimeMS extracts the float millisecond value from:
//
//	"Execution Time: 42.37 ms"
func parseExecTimeMS(output string) (float64, bool) {
	re := regexp.MustCompile(`Execution Time:\s*([\d.]+)\s*ms`)
	m := re.FindStringSubmatch(output)
	if len(m) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// writeExplainEvidence writes the full EXPLAIN output to
// testdata/explain-listenriched-failure.txt alongside this file, so the CI
// artifact can be inspected without re-running the test.
func writeExplainEvidence(t *testing.T, output string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Logf("(could not locate test file path to write evidence)")
		return
	}
	dir := filepath.Join(filepath.Dir(thisFile), "testdata")
	_ = os.MkdirAll(dir, 0o755)
	dest := filepath.Join(dir, "explain-listenriched-failure.txt")
	if err := os.WriteFile(dest, []byte(output), 0o644); err != nil {
		t.Logf("warning: could not write EXPLAIN evidence: %v", err)
	} else {
		t.Logf("EXPLAIN output written to %s", dest)
	}
}
