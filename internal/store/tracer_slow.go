package store

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/btopcu/argus/internal/observability/metrics"
)

const slowQueryThreshold = 100 * time.Millisecond

var sqlTableRegex = regexp.MustCompile(`(?is)^\s*(SELECT|INSERT|UPDATE|DELETE)\s+(?:.*?FROM|INTO)?\s*"?(\w+)`)

type slowQueryTracer struct {
	reg       *metrics.Registry
	threshold time.Duration
	now       func() time.Time
}

func newSlowQueryTracer(reg *metrics.Registry) *slowQueryTracer {
	return &slowQueryTracer{
		reg:       reg,
		threshold: slowQueryThreshold,
		now:       time.Now,
	}
}

type queryMetaKey struct{}

type queryMeta struct {
	start time.Time
	op    string
	table string
}

func (t *slowQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	op, table := t.extract(data.SQL)
	return context.WithValue(ctx, queryMetaKey{}, queryMeta{
		start: t.now(),
		op:    op,
		table: table,
	})
}

func (t *slowQueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	m, ok := ctx.Value(queryMetaKey{}).(queryMeta)
	if !ok {
		return
	}
	dur := t.now().Sub(m.start)
	if t.reg != nil && t.reg.DBQueryDuration != nil {
		t.reg.DBQueryDuration.WithLabelValues(m.op, m.table).Observe(dur.Seconds())
	}
	if dur > t.threshold {
		if span := trace.SpanFromContext(ctx); span.IsRecording() {
			span.SetAttributes(
				attribute.Bool("argus.db.slow", true),
				attribute.String("argus.db.table", m.table),
				attribute.Float64("argus.db.duration_ms", float64(dur.Milliseconds())),
			)
		}
	}
}

func (t *slowQueryTracer) extract(sql string) (op string, table string) {
	m := sqlTableRegex.FindStringSubmatch(sql)
	if len(m) < 3 || m[2] == "" {
		op = "unknown"
		if trimmed := strings.TrimSpace(sql); len(trimmed) > 0 {
			if fields := strings.Fields(trimmed); len(fields) > 0 {
				op = strings.ToLower(fields[0])
			}
		}
		table = "unknown"
		return op, table
	}
	return strings.ToLower(m[1]), strings.ToLower(m[2])
}

// compositeTracer fans out pgx tracer callbacks to multiple wrapped tracers.
// It implements every pgx/pgxpool tracer interface so that wrapping
// otelpgx's full-featured Tracer does not lose batch/copy/prepare/connect/acquire
// instrumentation — pgx type-asserts the Tracer against each of these interfaces
// at runtime, so the composite must satisfy all of them.
type compositeTracer struct {
	tracers []pgx.QueryTracer
}

// Compile-time assertions: compositeTracer must satisfy every pgx/pgxpool
// tracer interface that pgx type-asserts at runtime on ConnConfig.Tracer.
// Dropping any of these silently disables instrumentation downstream.
var (
	_ pgx.QueryTracer       = (*compositeTracer)(nil)
	_ pgx.BatchTracer       = (*compositeTracer)(nil)
	_ pgx.CopyFromTracer    = (*compositeTracer)(nil)
	_ pgx.PrepareTracer     = (*compositeTracer)(nil)
	_ pgx.ConnectTracer     = (*compositeTracer)(nil)
	_ pgxpool.AcquireTracer = (*compositeTracer)(nil)
)

func newCompositeTracer(tracers ...pgx.QueryTracer) *compositeTracer {
	return &compositeTracer{tracers: tracers}
}

// --- pgx.QueryTracer ---

func (c *compositeTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	for _, tr := range c.tracers {
		ctx = tr.TraceQueryStart(ctx, conn, data)
	}
	return ctx
}

func (c *compositeTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	for _, tr := range c.tracers {
		tr.TraceQueryEnd(ctx, conn, data)
	}
}

// --- pgx.BatchTracer ---

func (c *compositeTracer) TraceBatchStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	for _, tr := range c.tracers {
		if bt, ok := tr.(pgx.BatchTracer); ok {
			ctx = bt.TraceBatchStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *compositeTracer) TraceBatchQuery(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchQueryData) {
	for _, tr := range c.tracers {
		if bt, ok := tr.(pgx.BatchTracer); ok {
			bt.TraceBatchQuery(ctx, conn, data)
		}
	}
}

func (c *compositeTracer) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	for _, tr := range c.tracers {
		if bt, ok := tr.(pgx.BatchTracer); ok {
			bt.TraceBatchEnd(ctx, conn, data)
		}
	}
}

// --- pgx.CopyFromTracer ---

func (c *compositeTracer) TraceCopyFromStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	for _, tr := range c.tracers {
		if ct, ok := tr.(pgx.CopyFromTracer); ok {
			ctx = ct.TraceCopyFromStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *compositeTracer) TraceCopyFromEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceCopyFromEndData) {
	for _, tr := range c.tracers {
		if ct, ok := tr.(pgx.CopyFromTracer); ok {
			ct.TraceCopyFromEnd(ctx, conn, data)
		}
	}
}

// --- pgx.PrepareTracer ---

func (c *compositeTracer) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	for _, tr := range c.tracers {
		if pt, ok := tr.(pgx.PrepareTracer); ok {
			ctx = pt.TracePrepareStart(ctx, conn, data)
		}
	}
	return ctx
}

func (c *compositeTracer) TracePrepareEnd(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareEndData) {
	for _, tr := range c.tracers {
		if pt, ok := tr.(pgx.PrepareTracer); ok {
			pt.TracePrepareEnd(ctx, conn, data)
		}
	}
}

// --- pgx.ConnectTracer ---

func (c *compositeTracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	for _, tr := range c.tracers {
		if ct, ok := tr.(pgx.ConnectTracer); ok {
			ctx = ct.TraceConnectStart(ctx, data)
		}
	}
	return ctx
}

func (c *compositeTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	for _, tr := range c.tracers {
		if ct, ok := tr.(pgx.ConnectTracer); ok {
			ct.TraceConnectEnd(ctx, data)
		}
	}
}

// --- pgxpool.AcquireTracer ---

func (c *compositeTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	for _, tr := range c.tracers {
		if at, ok := tr.(pgxpool.AcquireTracer); ok {
			ctx = at.TraceAcquireStart(ctx, pool, data)
		}
	}
	return ctx
}

func (c *compositeTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	for _, tr := range c.tracers {
		if at, ok := tr.(pgxpool.AcquireTracer); ok {
			at.TraceAcquireEnd(ctx, pool, data)
		}
	}
}

// poolStater is the minimal surface of *pgxpool.Pool used by StartPoolGauge,
// exposed so tests can inject a stub without a live database.
type poolStater interface {
	Stat() *pgxpool.Stat
}

// StartPoolGauge launches a background goroutine that periodically updates the
// DBPoolConnections gauge from the pool's runtime stats. It returns immediately.
// The goroutine exits when ctx is cancelled.
//
// Labels: "idle", "in_use", "total", "max". pgxpool.Stat does NOT expose a
// "waiting" counter directly (only EmptyAcquireCount as a cumulative delta),
// so the plan's {idle, in_use, waiting} label set is mapped to what pgxpool
// can actually report.
func StartPoolGauge(ctx context.Context, pool *pgxpool.Pool, reg *metrics.Registry, interval time.Duration) {
	if reg == nil || pool == nil {
		return
	}
	startPoolGaugeLoop(ctx, pool, reg, interval)
}

func startPoolGaugeLoop(ctx context.Context, pool poolStater, reg *metrics.Registry, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		updatePoolGauge(pool, reg)
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				updatePoolGauge(pool, reg)
			}
		}
	}()
}

func updatePoolGauge(pool poolStater, reg *metrics.Registry) {
	if reg == nil || reg.DBPoolConnections == nil || pool == nil {
		return
	}
	stat := pool.Stat()
	if stat == nil {
		return
	}
	reg.DBPoolConnections.WithLabelValues("idle").Set(float64(stat.IdleConns()))
	reg.DBPoolConnections.WithLabelValues("in_use").Set(float64(stat.AcquiredConns()))
	reg.DBPoolConnections.WithLabelValues("total").Set(float64(stat.TotalConns()))
	reg.DBPoolConnections.WithLabelValues("max").Set(float64(stat.MaxConns()))
}
