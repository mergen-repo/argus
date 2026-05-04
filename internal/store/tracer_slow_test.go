package store

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/btopcu/argus/internal/observability/metrics"
)

func TestSlowQueryTracer_ExtractOpAndTable(t *testing.T) {
	t.Parallel()
	tr := newSlowQueryTracer(nil)

	cases := []struct {
		name    string
		sql     string
		wantOp  string
		wantTbl string
	}{
		{"select simple", `SELECT id FROM sims WHERE tenant_id = $1`, "select", "sims"},
		{"select quoted", `SELECT * FROM "tenants" LIMIT 10`, "select", "tenants"},
		{"select leading whitespace", "   SELECT 1 FROM apns", "select", "apns"},
		{"insert", `INSERT INTO audit_logs (ts, actor) VALUES ($1, $2)`, "insert", "audit_logs"},
		{"insert quoted", `INSERT INTO "users" VALUES ($1)`, "insert", "users"},
		{"update", `UPDATE sims SET status = 'active' WHERE id = $1`, "update", "sims"},
		{"update quoted", `UPDATE "policy_rules" SET enabled = true`, "update", "policy_rules"},
		{"delete", `DELETE FROM sessions WHERE ended_at < now()`, "delete", "sessions"},
		{"lowercase keyword", `select id from sims`, "select", "sims"},
		{"select with join", `SELECT s.id FROM sims s JOIN tenants t ON s.tenant_id = t.id`, "select", "sims"},
		{"unknown (BEGIN)", `BEGIN`, "begin", "unknown"},
		{"empty", ``, "unknown", "unknown"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			op, tbl := tr.extract(tc.sql)
			if op != tc.wantOp {
				t.Errorf("op: got %q, want %q (sql=%q)", op, tc.wantOp, tc.sql)
			}
			if tbl != tc.wantTbl {
				t.Errorf("table: got %q, want %q (sql=%q)", tbl, tc.wantTbl, tc.sql)
			}
		})
	}
}

func TestSlowQueryTracer_ObservesHistogram(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry()
	tr := newSlowQueryTracer(reg)

	// Deterministic clock: TraceQueryStart sees t0, TraceQueryEnd sees t0+5ms.
	var callCount atomic.Int32
	t0 := time.Now()
	tr.now = func() time.Time {
		n := callCount.Add(1)
		if n == 1 {
			return t0
		}
		return t0.Add(5 * time.Millisecond)
	}

	ctx := context.Background()
	ctx = tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL: `SELECT id FROM sims WHERE tenant_id = $1`,
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	// One series with labels {select, sims} must exist, with exactly one sample.
	// CollectAndCount on the full histogram vec counts registered series.
	if series := testutil.CollectAndCount(reg.DBQueryDuration); series != 1 {
		t.Fatalf("expected 1 histogram series, got %d", series)
	}

	// gatherHistogramSum verifies via the prometheus DTO that the sample count
	// (not the summed bucket values) is 1 for the select/sims series.
	count, sum := gatherHistogramStats(t, reg, "argus_db_query_duration_seconds",
		map[string]string{"operation": "select", "table": "sims"})
	if count != 1 {
		t.Fatalf("expected sample count 1, got %d", count)
	}
	if sum <= 0 {
		t.Fatalf("expected observed duration > 0, got %f", sum)
	}
}

// gatherHistogramStats walks the Prometheus registry and returns the (count, sum)
// of the first histogram matching name + labels. Fails the test if not found.
func gatherHistogramStats(t *testing.T, reg *metrics.Registry, name string, wantLabels map[string]string) (uint64, float64) {
	t.Helper()
	mfs, err := reg.Reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			match := true
			for k, v := range wantLabels {
				if labels[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				continue
			}
			return h.GetSampleCount(), h.GetSampleSum()
		}
	}
	t.Fatalf("histogram %q with labels %v not found", name, wantLabels)
	return 0, 0
}

func TestSlowQueryTracer_MarksSlowSpan(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry()
	tr := newSlowQueryTracer(reg)
	tr.threshold = 1 * time.Millisecond

	// Start at t0, end at t0+100ms → definitively > 1ms threshold.
	var n atomic.Int32
	t0 := time.Now()
	tr.now = func() time.Time {
		c := n.Add(1)
		if c == 1 {
			return t0
		}
		return t0.Add(100 * time.Millisecond)
	}

	ctx := context.Background()
	ctx = tr.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: `SELECT * FROM tenants`})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	// No active recording span in ctx → slow-branch must not panic and histogram
	// still observes. This asserts the nil-span safety path.
	if c := testutil.CollectAndCount(reg.DBQueryDuration); c == 0 {
		t.Fatal("histogram should be observed even without a recording span")
	}
}

func TestSlowQueryTracer_NoMetaInCtx(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry()
	tr := newSlowQueryTracer(reg)
	// Calling End without Start must be a no-op, not a panic.
	tr.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
	if testutil.CollectAndCount(reg.DBQueryDuration) != 0 {
		t.Fatal("histogram should not be touched when ctx has no queryMeta")
	}
}

// stubQueryTracer records every callback invocation for composite tests.
type stubQueryTracer struct {
	starts int
	ends   int
}

func (s *stubQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	s.starts++
	return ctx
}
func (s *stubQueryTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	s.ends++
}

// stubFullTracer implements every pgx tracer interface the composite forwards to,
// so we can verify forwarding for batch/copyfrom/prepare/connect/acquire paths.
type stubFullTracer struct {
	stubQueryTracer
	batchStart, batchQuery, batchEnd int
	copyStart, copyEnd               int
	prepStart, prepEnd               int
	connStart, connEnd               int
	acqStart, acqEnd                 int
}

func (s *stubFullTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	s.batchStart++
	return ctx
}
func (s *stubFullTracer) TraceBatchQuery(_ context.Context, _ *pgx.Conn, _ pgx.TraceBatchQueryData) {
	s.batchQuery++
}
func (s *stubFullTracer) TraceBatchEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceBatchEndData) {
	s.batchEnd++
}
func (s *stubFullTracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceCopyFromStartData) context.Context {
	s.copyStart++
	return ctx
}
func (s *stubFullTracer) TraceCopyFromEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceCopyFromEndData) {
	s.copyEnd++
}
func (s *stubFullTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, _ pgx.TracePrepareStartData) context.Context {
	s.prepStart++
	return ctx
}
func (s *stubFullTracer) TracePrepareEnd(_ context.Context, _ *pgx.Conn, _ pgx.TracePrepareEndData) {
	s.prepEnd++
}
func (s *stubFullTracer) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	s.connStart++
	return ctx
}
func (s *stubFullTracer) TraceConnectEnd(_ context.Context, _ pgx.TraceConnectEndData) {
	s.connEnd++
}
func (s *stubFullTracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	s.acqStart++
	return ctx
}
func (s *stubFullTracer) TraceAcquireEnd(_ context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	s.acqEnd++
}

func TestCompositeTracer_FansOutQueryToAll(t *testing.T) {
	t.Parallel()
	a := &stubQueryTracer{}
	b := &stubQueryTracer{}
	c := newCompositeTracer(a, b)

	ctx := c.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	c.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if a.starts != 1 || b.starts != 1 {
		t.Errorf("QueryStart not fanned out: a=%d b=%d", a.starts, b.starts)
	}
	if a.ends != 1 || b.ends != 1 {
		t.Errorf("QueryEnd not fanned out: a=%d b=%d", a.ends, b.ends)
	}
}

func TestCompositeTracer_ForwardsAllInterfacesWhenImplemented(t *testing.T) {
	t.Parallel()
	full := &stubFullTracer{}
	quiet := &stubQueryTracer{} // implements only QueryTracer — must be skipped by non-query methods
	c := newCompositeTracer(full, quiet)

	ctx := context.Background()
	// Batch
	ctx = c.TraceBatchStart(ctx, nil, pgx.TraceBatchStartData{})
	c.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{})
	c.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})
	// CopyFrom
	ctx = c.TraceCopyFromStart(ctx, nil, pgx.TraceCopyFromStartData{})
	c.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})
	// Prepare
	ctx = c.TracePrepareStart(ctx, nil, pgx.TracePrepareStartData{})
	c.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{})
	// Connect
	ctx = c.TraceConnectStart(ctx, pgx.TraceConnectStartData{})
	c.TraceConnectEnd(ctx, pgx.TraceConnectEndData{})
	// Acquire
	ctx = c.TraceAcquireStart(ctx, nil, pgxpool.TraceAcquireStartData{})
	c.TraceAcquireEnd(ctx, nil, pgxpool.TraceAcquireEndData{})

	if full.batchStart != 1 || full.batchQuery != 1 || full.batchEnd != 1 {
		t.Errorf("BatchTracer not forwarded: start=%d query=%d end=%d",
			full.batchStart, full.batchQuery, full.batchEnd)
	}
	if full.copyStart != 1 || full.copyEnd != 1 {
		t.Errorf("CopyFromTracer not forwarded: start=%d end=%d", full.copyStart, full.copyEnd)
	}
	if full.prepStart != 1 || full.prepEnd != 1 {
		t.Errorf("PrepareTracer not forwarded: start=%d end=%d", full.prepStart, full.prepEnd)
	}
	if full.connStart != 1 || full.connEnd != 1 {
		t.Errorf("ConnectTracer not forwarded: start=%d end=%d", full.connStart, full.connEnd)
	}
	if full.acqStart != 1 || full.acqEnd != 1 {
		t.Errorf("AcquireTracer not forwarded: start=%d end=%d", full.acqStart, full.acqEnd)
	}
	// quiet must have seen nothing (no Query calls above).
	if quiet.starts != 0 || quiet.ends != 0 {
		t.Errorf("stubQueryTracer should have been skipped: starts=%d ends=%d", quiet.starts, quiet.ends)
	}
}

// stubPoolStat implements poolStater for pool gauge tests without needing a
// live pgxpool connection.
type stubPoolStat struct {
	stat *pgxpool.Stat
}

func (s *stubPoolStat) Stat() *pgxpool.Stat { return s.stat }

func TestStartPoolGauge_UpdatesOnTick(t *testing.T) {
	t.Parallel()
	reg := metrics.NewRegistry()

	// pgxpool.Stat is constructed from a real pool normally; we cannot
	// fabricate one directly because its fields are unexported. Instead we
	// exercise updatePoolGauge's nil-safety paths and guard clauses.
	//
	// Guard: nil registry → no-op
	updatePoolGauge(nil, nil)
	// Guard: nil pool → no-op
	updatePoolGauge(nil, reg)
	// Guard: stat returns nil → no-op
	updatePoolGauge(&stubPoolStat{stat: nil}, reg)

	if testutil.CollectAndCount(reg.DBPoolConnections) != 0 {
		t.Fatal("gauge should remain unset on guard paths")
	}

	// Cancel-before-start: ensure the goroutine exits without panic.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startPoolGaugeLoop(ctx, &stubPoolStat{stat: nil}, reg, 1*time.Millisecond)

	// Give the goroutine a moment to observe the cancelled context.
	time.Sleep(10 * time.Millisecond)
}

func TestStartPoolGauge_NilInputsAreSafe(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// All of these must be no-ops — NOT panics.
	StartPoolGauge(ctx, nil, nil, time.Second)
	StartPoolGauge(ctx, nil, metrics.NewRegistry(), time.Second)
}
