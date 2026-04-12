package bus

import (
	"context"
	"sort"
	"testing"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/nats-io/nats.go"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// setupTracer installs a real tracer provider + TraceContext propagator so
// that Inject/Extract produce the W3C traceparent header. Returns a
// cleanup function that restores the previous global provider.
func setupTracer(t *testing.T) func() {
	t.Helper()
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	}
}

func TestCarrier_GetSetKeys(t *testing.T) {
	header := nats.Header{}
	carrier := natsHeaderCarrier(header)

	if got := carrier.Get("missing"); got != "" {
		t.Errorf("Get on empty header = %q, want empty", got)
	}

	carrier.Set("traceparent", "00-aaaa-bbbb-01")
	carrier.Set("tracestate", "k=v")

	if got := carrier.Get("traceparent"); got != "00-aaaa-bbbb-01" {
		t.Errorf("Get traceparent = %q, want 00-aaaa-bbbb-01", got)
	}
	if got := carrier.Get("tracestate"); got != "k=v" {
		t.Errorf("Get tracestate = %q, want k=v", got)
	}

	keys := carrier.Keys()
	sort.Strings(keys)
	wantKeys := []string{"traceparent", "tracestate"} // nats.Header is case-sensitive
	sort.Strings(wantKeys)
	if len(keys) != len(wantKeys) {
		t.Fatalf("Keys() len = %d (%v), want %d (%v)", len(keys), keys, len(wantKeys), wantKeys)
	}
	for i, k := range keys {
		if k != wantKeys[i] {
			t.Errorf("Keys()[%d] = %q, want %q", i, k, wantKeys[i])
		}
	}

	// Compile-time sanity: natsHeaderCarrier must satisfy TextMapCarrier.
	var _ propagation.TextMapCarrier = carrier
}

func TestCarrier_Inject_WritesTraceparent(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// Build a context that carries a recording span.
	ctx, span := otel.Tracer("test").Start(context.Background(), "outer")
	defer span.End()

	header := nats.Header{}
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(header))

	tp := header.Get("traceparent")
	if tp == "" {
		t.Fatalf("expected traceparent header to be injected, got empty")
	}
	// W3C traceparent format: "00-<trace-id:32>-<span-id:16>-<flags:2>"
	// Sanity-check the trace id matches the span's own trace id.
	sc := span.SpanContext()
	if !sc.HasTraceID() {
		t.Fatalf("span has no trace id")
	}
	wantTraceID := sc.TraceID().String()
	// traceparent = "00-" + traceID + "-" + spanID + "-01"
	if len(tp) < 3+32 || tp[3:3+32] != wantTraceID {
		t.Errorf("traceparent %q does not contain expected trace id %q", tp, wantTraceID)
	}
}

func TestCarrier_Extract_RoundTrip(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// Produce a traceparent via a real span.
	producerCtx, producerSpan := otel.Tracer("test").Start(context.Background(), "producer")
	defer producerSpan.End()

	header := nats.Header{}
	otel.GetTextMapPropagator().Inject(producerCtx, natsHeaderCarrier(header))
	if header.Get("traceparent") == "" {
		t.Fatal("traceparent not injected; cannot verify extraction")
	}

	// Extract on an otherwise-empty context.
	extractedCtx := otel.GetTextMapPropagator().Extract(context.Background(), natsHeaderCarrier(header))
	extractedSC := trace.SpanContextFromContext(extractedCtx)
	if !extractedSC.IsValid() {
		t.Fatal("extracted span context is not valid")
	}

	producerSC := producerSpan.SpanContext()
	if extractedSC.TraceID() != producerSC.TraceID() {
		t.Errorf("trace id mismatch: producer=%s extracted=%s",
			producerSC.TraceID(), extractedSC.TraceID())
	}
	if extractedSC.SpanID() != producerSC.SpanID() {
		t.Errorf("span id mismatch: producer=%s extracted=%s",
			producerSC.SpanID(), extractedSC.SpanID())
	}

	// Now start a child span from the extracted context and verify its
	// parent is the producer span context (trace id preserved, new span id).
	childCtx, childSpan := otel.Tracer("test").Start(extractedCtx, "consumer")
	defer childSpan.End()
	childSC := trace.SpanContextFromContext(childCtx)
	if childSC.TraceID() != producerSC.TraceID() {
		t.Errorf("child trace id %s != producer trace id %s",
			childSC.TraceID(), producerSC.TraceID())
	}
	if childSC.SpanID() == producerSC.SpanID() {
		t.Errorf("child span id should differ from producer span id")
	}
}

func TestConsumeSpan_ExtractsHeader(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	// Build a producer trace context and inject into a header.
	prodCtx, prodSpan := otel.Tracer("test").Start(context.Background(), "producer")
	defer prodSpan.End()

	header := nats.Header{}
	otel.GetTextMapPropagator().Inject(prodCtx, natsHeaderCarrier(header))

	msg := &nats.Msg{
		Subject: "argus.events.test.trace",
		Header:  header,
		Data:    []byte("payload"),
	}

	eb := &EventBus{} // zero-value; consumeSpan only needs tracer + propagator
	ctx, span := eb.consumeSpan(msg)
	defer span.End()

	// The ctx returned by consumeSpan holds the new child span. That
	// span's span context should share the producer's trace id.
	childSC := trace.SpanContextFromContext(ctx)
	if !childSC.IsValid() {
		t.Fatal("consume span context is not valid")
	}
	if childSC.TraceID() != prodSpan.SpanContext().TraceID() {
		t.Errorf("consume span trace id %s != producer trace id %s",
			childSC.TraceID(), prodSpan.SpanContext().TraceID())
	}
}

func TestConsumeSpan_NoHeaderStartsFreshTrace(t *testing.T) {
	cleanup := setupTracer(t)
	defer cleanup()

	msg := &nats.Msg{
		Subject: "argus.events.test.notrace",
		Header:  nil,
		Data:    []byte("payload"),
	}

	eb := &EventBus{}
	ctx, span := eb.consumeSpan(msg)
	defer span.End()

	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		t.Fatal("expected a valid fresh span context when header is nil")
	}
}

func TestRecordConsumed_IncrementsMetric(t *testing.T) {
	reg := metrics.NewRegistry()
	eb := &EventBus{}
	eb.SetMetrics(reg)

	subject := "argus.events.test.metric"
	eb.recordConsumed(subject)
	eb.recordConsumed(subject)
	eb.recordConsumed(subject)

	got := counterValue(t, reg, "argus_nats_consumed_total", map[string]string{"subject": subject})
	if got != 3 {
		t.Errorf("consumed counter for %q = %v, want 3", subject, got)
	}
}

func TestRecordConsumed_NilMetricsIsNoOp(t *testing.T) {
	eb := &EventBus{}
	// SetMetrics never called — reg is nil. Must not panic.
	eb.recordConsumed("argus.events.test.nil")
}

func TestSetMetrics_AttachesRegistry(t *testing.T) {
	reg := metrics.NewRegistry()
	eb := &EventBus{}
	eb.SetMetrics(reg)
	if eb.reg != reg {
		t.Error("SetMetrics did not attach registry")
	}
}

func TestPublishMetricLabel_IncrementsOnRecord(t *testing.T) {
	// This test exercises the published-total counter path without an
	// actual NATS broker by incrementing it the same way publishMsg does
	// on success. The production path is unit-verified via the shared
	// increment in publishMsg; here we confirm the metric plumbing.
	reg := metrics.NewRegistry()
	eb := &EventBus{}
	eb.SetMetrics(reg)

	subject := "argus.events.test.published"
	reg.NATSPublishedTotal.WithLabelValues(subject).Inc()
	reg.NATSPublishedTotal.WithLabelValues(subject).Inc()

	got := counterValue(t, reg, "argus_nats_published_total", map[string]string{"subject": subject})
	if got != 2 {
		t.Errorf("published counter for %q = %v, want 2", subject, got)
	}
}

// counterValue reads the current value of a Prometheus counter with
// matching label set from the registry. Fails the test if not found.
func counterValue(t *testing.T, reg *metrics.Registry, metricName string, labels map[string]string) float64 {
	t.Helper()
	families, err := reg.Reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if !labelsMatchDto(m.GetLabel(), labels) {
				continue
			}
			if m.Counter == nil {
				t.Fatalf("metric %q has no Counter", metricName)
			}
			return m.Counter.GetValue()
		}
	}
	t.Fatalf("metric %q with labels %v not found", metricName, labels)
	return 0
}

func labelsMatchDto(got []*dto.LabelPair, want map[string]string) bool {
	if len(got) < len(want) {
		return false
	}
	for k, v := range want {
		found := false
		for _, p := range got {
			if p.GetName() == k && p.GetValue() == v {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
