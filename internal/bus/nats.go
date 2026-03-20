package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
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

	StreamEvents = "EVENTS"
	StreamJobs   = "JOBS"
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
		MaxAge:    72 * time.Hour,
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

type EventBus struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	logger zerolog.Logger
}

func NewEventBus(n *NATS) *EventBus {
	return &EventBus{
		conn:   n.Conn,
		js:     n.JetStream,
		logger: n.logger.With().Str("component", "event_bus").Logger(),
	}
}

func (eb *EventBus) Publish(ctx context.Context, subject string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("bus: marshal: %w", err)
	}
	_, err = eb.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("bus: publish %s: %w", subject, err)
	}
	return nil
}

type MessageHandler func(subject string, data []byte)

func (eb *EventBus) Subscribe(subject string, handler MessageHandler) (*nats.Subscription, error) {
	return eb.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Subject, msg.Data)
	})
}

func (eb *EventBus) QueueSubscribe(subject, queue string, handler MessageHandler) (*nats.Subscription, error) {
	return eb.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		handler(msg.Subject, msg.Data)
	})
}
