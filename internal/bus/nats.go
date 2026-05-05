package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	SubjectSessionStarted        = "argus.events.session.started"
	SubjectSessionUpdated        = "argus.events.session.updated"
	SubjectSessionEnded          = "argus.events.session.ended"
	SubjectSIMUpdated            = "argus.events.sim.updated"
	SubjectPolicyChanged         = "argus.events.policy.changed"
	SubjectOperatorHealthChanged = "argus.events.operator.health"
	SubjectNotification          = "argus.events.notification.dispatch"
	SubjectAlertTriggered        = "argus.events.alert.triggered"
	SubjectJobQueue              = "argus.jobs.queue"
	SubjectJobCompleted          = "argus.jobs.completed"
	SubjectJobProgress           = "argus.jobs.progress"
	SubjectCacheInvalidate       = "argus.cache.invalidate"
	SubjectAuditCreate           = "argus.events.audit.create"
	SubjectPolicyRolloutProgress = "argus.events.policy.rollout_progress"
	SubjectAnomalyDetected       = "argus.events.anomaly.detected"
	SubjectAuthAttempt           = "argus.events.auth.attempt"
	SubjectIPReclaimed           = "argus.events.ip.reclaimed"
	SubjectIPReleased            = "argus.events.ip.released"
	SubjectSLAReportGenerated    = "argus.events.sla.report.generated"
	SubjectBackupCompleted       = "argus.events.backup.completed"
	SubjectBackupVerified        = "argus.events.backup.verified"

	SubjectFleetMassOffline      = "argus.events.fleet.mass_offline"
	SubjectFleetTrafficSpike     = "argus.events.fleet.traffic_spike"
	SubjectFleetQuotaBreachCount = "argus.events.fleet.quota_breach_count"
	SubjectFleetViolationSurge   = "argus.events.fleet.violation_surge"

	SubjectBulkJobCompleted  = "argus.events.bulk_job.completed"
	SubjectBulkJobFailed     = "argus.events.bulk_job.failed"
	SubjectWebhookDeadLetter = "argus.events.webhook.dead_letter"

	SubjectESimCommandIssued = "argus.events.esim.command.issued"
	SubjectESimCommandAcked  = "argus.events.esim.command.acked"
	SubjectESimCommandFailed = "argus.events.esim.command.failed"

	StreamEvents = "EVENTS"
	StreamJobs   = "JOBS"

	tracerName = "argus.bus"
)

type NATS struct {
	Conn      *nats.Conn
	JetStream jetstream.JetStream
	logger    zerolog.Logger
}

func NewNATS(ctx context.Context, url string, maxReconnect int, reconnectWait time.Duration, logger zerolog.Logger) (*NATS, error) {
	l := logger.With().Str("component", "bus").Logger()

	conn, err := nats.Connect(
		url,
		nats.MaxReconnects(maxReconnect),
		nats.ReconnectWait(reconnectWait),
		nats.Name("argus"),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			l.Warn().Err(err).Msg("nats disconnected")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			l.Info().Str("url", nc.ConnectedUrl()).Msg("nats reconnected")
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			l.Info().Msg("nats connection closed")
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("bus: connect: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("bus: jetstream: %w", err)
	}

	l.Info().Str("url", conn.ConnectedUrl()).Msg("nats connected")

	return &NATS{Conn: conn, JetStream: js, logger: l}, nil
}

func (n *NATS) EnsureStreams(ctx context.Context) error {
	eventsConfig := jetstream.StreamConfig{
		Name:      StreamEvents,
		Subjects:  []string{"argus.events.>"},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    168 * time.Hour,
		Storage:   jetstream.FileStorage,
		Replicas:  1,
		Discard:   jetstream.DiscardOld,
	}
	if _, err := n.JetStream.CreateOrUpdateStream(ctx, eventsConfig); err != nil {
		return fmt.Errorf("bus: create EVENTS stream: %w", err)
	}
	n.logger.Info().Str("stream", StreamEvents).Msg("jetstream stream ready")

	jobsConfig := jetstream.StreamConfig{
		Name:      StreamJobs,
		Subjects:  []string{"argus.jobs.>"},
		Retention: jetstream.WorkQueuePolicy,
		MaxAge:    24 * time.Hour,
		Storage:   jetstream.FileStorage,
		Replicas:  1,
		Discard:   jetstream.DiscardOld,
	}
	if _, err := n.JetStream.CreateOrUpdateStream(ctx, jobsConfig); err != nil {
		return fmt.Errorf("bus: create JOBS stream: %w", err)
	}
	n.logger.Info().Str("stream", StreamJobs).Msg("jetstream stream ready")

	return nil
}

func (n *NATS) HealthCheck(_ context.Context) error {
	if !n.Conn.IsConnected() {
		return fmt.Errorf("bus: not connected")
	}
	return nil
}

func (n *NATS) Close() {
	n.Conn.Close()
}

// natsHeaderCarrier adapts nats.Header to the OpenTelemetry
// propagation.TextMapCarrier interface, allowing the W3C traceparent
// header to be injected into and extracted from NATS messages.
type natsHeaderCarrier nats.Header

// Get returns the first value stored under key, or the empty string.
func (c natsHeaderCarrier) Get(key string) string {
	return nats.Header(c).Get(key)
}

// Set stores value under key, replacing any prior value.
func (c natsHeaderCarrier) Set(key, value string) {
	nats.Header(c).Set(key, value)
}

// Keys returns the list of keys present in the carrier.
func (c natsHeaderCarrier) Keys() []string {
	h := nats.Header(c)
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

// Compile-time check that natsHeaderCarrier implements TextMapCarrier.
var _ propagation.TextMapCarrier = (natsHeaderCarrier)(nil)

type EventBus struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	logger zerolog.Logger
	reg    *metrics.Registry
}

func NewEventBus(n *NATS) *EventBus {
	return &EventBus{
		conn:   n.Conn,
		js:     n.JetStream,
		logger: n.logger.With().Str("component", "event_bus").Logger(),
	}
}

// SetMetrics wires the Prometheus registry into the EventBus so that
// publish/consume counters are incremented. Safe to call with nil to
// disable metric emission (default).
func (eb *EventBus) SetMetrics(reg *metrics.Registry) {
	eb.reg = reg
}

// publishMsg builds a nats.Msg with an empty Header map, injects the
// current trace context into that header, opens a producer span, and
// hands the message to core NATS. Core publish is used instead of
// jetstream.JetStream.PublishMsg because the new jetstream API writes
// directly to stream storage and does NOT fan out to core subscribers.
// All existing event consumers (ws hub, CDR consumer, notification
// service, policy matcher) use core NATS QueueSubscribe; JetStream-only
// publish would silently drop events for all of them.
//
// The EVENTS JetStream stream (Subjects: ["argus.events.>"]) still
// persists messages automatically: JetStream streams subscribe to their
// configured subjects via core NATS, so core publishes are captured and
// stored for future durable-consumer replay.
func (eb *EventBus) publishMsg(ctx context.Context, subject string, data []byte) error {
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  nats.Header{},
	}
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(msg.Header))

	_, span := otel.Tracer(tracerName).Start(ctx, "nats.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			semconv.MessagingDestinationName(subject),
			semconv.MessagingOperationTypePublish,
		),
	)
	defer span.End()

	if err := eb.conn.PublishMsg(msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("bus: publish %s: %w", subject, err)
	}

	if eb.reg != nil {
		eb.reg.NATSPublishedTotal.WithLabelValues(subject).Inc()
	}
	return nil
}

func (eb *EventBus) Publish(ctx context.Context, subject string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("bus: marshal: %w", err)
	}
	return eb.publishMsg(ctx, subject, data)
}

func (eb *EventBus) PublishRaw(ctx context.Context, subject string, data []byte) error {
	return eb.publishMsg(ctx, subject, data)
}

// MessageHandler is the legacy handler type used by existing subscribers
// throughout cmd/argus/main.go. It is intentionally kept so that Task 9
// does not require changes at call sites.
type MessageHandler func(subject string, data []byte)

// MessageHandlerCtx is the new context-aware handler type. Subscribers
// that migrate to SubscribeCtx / QueueSubscribeCtx receive the extracted
// trace context as the first argument so that downstream spans are
// children of the producer span.
type MessageHandlerCtx func(ctx context.Context, subject string, data []byte)

// consumeSpan starts a consumer span by extracting the W3C traceparent
// from the message header (if any) and returns the derived context plus
// the span (caller must End it).
func (eb *EventBus) consumeSpan(msg *nats.Msg) (context.Context, trace.Span) {
	ctx := context.Background()
	if msg.Header != nil {
		ctx = otel.GetTextMapPropagator().Extract(ctx, natsHeaderCarrier(msg.Header))
	}
	return otel.Tracer(tracerName).Start(ctx, "nats.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			semconv.MessagingDestinationName(msg.Subject),
			semconv.MessagingOperationTypeDeliver,
		),
	)
}

// recordConsumed increments the consumed counter for the given subject
// when a metrics registry has been attached.
func (eb *EventBus) recordConsumed(subject string) {
	if eb.reg != nil {
		eb.reg.NATSConsumedTotal.WithLabelValues(subject).Inc()
	}
}

// SubscribeCtx subscribes to a subject and invokes the given context-aware
// handler. The traceparent header (if any) is extracted and a consumer
// span is started before the handler runs. The counter
// argus_nats_consumed_total is incremented for every delivery regardless
// of handler outcome.
func (eb *EventBus) SubscribeCtx(subject string, handler MessageHandlerCtx) (*nats.Subscription, error) {
	return eb.conn.Subscribe(subject, func(msg *nats.Msg) {
		ctx, span := eb.consumeSpan(msg)
		defer span.End()
		eb.recordConsumed(msg.Subject)
		handler(ctx, msg.Subject, msg.Data)
	})
}

// QueueSubscribeCtx is the queue-group variant of SubscribeCtx.
func (eb *EventBus) QueueSubscribeCtx(subject, queue string, handler MessageHandlerCtx) (*nats.Subscription, error) {
	return eb.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		ctx, span := eb.consumeSpan(msg)
		defer span.End()
		eb.recordConsumed(msg.Subject)
		handler(ctx, msg.Subject, msg.Data)
	})
}

// Subscribe preserves the legacy (non-context) handler signature so that
// existing call sites in cmd/argus/main.go continue to work unchanged.
// It still extracts the traceparent, starts a consumer span, and records
// metrics — the trace context is simply discarded before the legacy
// handler is invoked.
func (eb *EventBus) Subscribe(subject string, handler MessageHandler) (*nats.Subscription, error) {
	return eb.SubscribeCtx(subject, func(_ context.Context, subj string, data []byte) {
		handler(subj, data)
	})
}

// QueueSubscribe preserves the legacy (non-context) handler signature.
// See Subscribe for rationale.
func (eb *EventBus) QueueSubscribe(subject, queue string, handler MessageHandler) (*nats.Subscription, error) {
	return eb.QueueSubscribeCtx(subject, queue, func(_ context.Context, subj string, data []byte) {
		handler(subj, data)
	})
}
