package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	SubjectSessionStarted       = "argus.events.session.started"
	SubjectSessionUpdated       = "argus.events.session.updated"
	SubjectSessionEnded         = "argus.events.session.ended"
	SubjectSIMUpdated           = "argus.events.sim.updated"
	SubjectPolicyChanged        = "argus.events.policy.changed"
	SubjectOperatorHealthChanged = "argus.events.operator.health"
	SubjectNotification         = "argus.events.notification.dispatch"
	SubjectJobQueue             = "argus.jobs.queue"
	SubjectJobCompleted         = "argus.jobs.completed"
	SubjectJobProgress          = "argus.jobs.progress"
	SubjectCacheInvalidate      = "argus.cache.invalidate"
)

type NATS struct {
	Conn      *nats.Conn
	JetStream jetstream.JetStream
}

func NewNATS(ctx context.Context, url string, maxReconnect int, reconnectWait time.Duration) (*NATS, error) {
	conn, err := nats.Connect(
		url,
		nats.MaxReconnects(maxReconnect),
		nats.ReconnectWait(reconnectWait),
		nats.Name("argus"),
	)
	if err != nil {
		return nil, fmt.Errorf("bus: connect: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("bus: jetstream: %w", err)
	}

	return &NATS{Conn: conn, JetStream: js}, nil
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
	conn *nats.Conn
	js   jetstream.JetStream
}

func NewEventBus(n *NATS) *EventBus {
	return &EventBus{conn: n.Conn, js: n.JetStream}
}

func (eb *EventBus) Publish(_ context.Context, subject string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("bus: marshal: %w", err)
	}
	return eb.conn.Publish(subject, data)
}

type MessageHandler func(subject string, data []byte)

func (eb *EventBus) QueueSubscribe(subject, queue string, handler MessageHandler, opts ...nats.SubOpt) (*nats.Subscription, error) {
	return eb.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		handler(msg.Subject, msg.Data)
	})
}

func (eb *EventBus) Subscribe(subject string, handler MessageHandler) (*nats.Subscription, error) {
	return eb.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Subject, msg.Data)
	})
}
