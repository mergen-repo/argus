package bus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

const alertPersistThreshold = 5

// SystemTenantID is the infra-global tenant sentinel used by publishers of
// infra-scope alerts (NATS consumer lag, storage monitor, anomaly batch
// supervisor). FIX-212 D5: authored at the publisher, not subscriber
// fallback. Matches migrations/seed/001_admin_user.sql demo tenant.
var SystemTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// jsConsumerLister is a narrow interface over jetstream.Stream. It covers only
// the ListConsumers call needed for consumer lag polling, enabling
// straightforward unit testing without implementing the full Stream interface.
type jsConsumerLister interface {
	ListConsumers(ctx context.Context) jetstream.ConsumerInfoLister
}

// jsStreamLookup is a narrow interface over jetstream.JetStream used by
// LagPoller. It covers only the Stream lookup call needed for consumer lag
// polling, enabling straightforward unit testing without mocking the entire
// JetStream interface.
type jsStreamLookup interface {
	Stream(ctx context.Context, stream string) (jsConsumerLister, error)
}

// jsStreamLookupAdapter wraps jetstream.JetStream to satisfy jsStreamLookup.
// It narrows the return type of Stream() from jetstream.Stream to
// jsConsumerLister, which is all LagPoller needs.
type jsStreamLookupAdapter struct {
	js jetstream.JetStream
}

func (a *jsStreamLookupAdapter) Stream(ctx context.Context, stream string) (jsConsumerLister, error) {
	s, err := a.js.Stream(ctx, stream)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// NewJSStreamLookup wraps a jetstream.JetStream into the narrow jsStreamLookup
// interface required by LagPoller. Pass the result to NewLagPoller.
func NewJSStreamLookup(js jetstream.JetStream) jsStreamLookup {
	return &jsStreamLookupAdapter{js: js}
}

// EventPublisher is satisfied by bus.EventBus (and any other type that can
// publish a pre-marshalled payload to a named subject).
type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

// lagAlert was the pre-FIX-212 payload published to SubjectAlertTriggered.
// Retained in a comment for archaeology; the publisher now emits a
// bus.Envelope via emitAlert below.

// LagPoller periodically checks the NumPending count for every consumer on
// the configured JetStream streams and emits Prometheus metrics. When a
// consumer's lag remains above alertThreshold for alertPersistThreshold
// consecutive polls an alert event is published and the counter is reset.
type LagPoller struct {
	js             jsStreamLookup
	reg            *metrics.Registry
	streams        []string
	pollInterval   time.Duration
	alertThreshold int
	counters       map[string]int
	eb             EventPublisher
	logger         zerolog.Logger
	stop           chan struct{}
	wg             sync.WaitGroup
}

// NewLagPoller constructs a LagPoller. The poller is not started until Start
// is called.
func NewLagPoller(
	js jsStreamLookup,
	reg *metrics.Registry,
	streams []string,
	poll time.Duration,
	alertThreshold int,
	eb EventPublisher,
	logger zerolog.Logger,
) *LagPoller {
	return &LagPoller{
		js:             js,
		reg:            reg,
		streams:        streams,
		pollInterval:   poll,
		alertThreshold: alertThreshold,
		counters:       make(map[string]int),
		eb:             eb,
		logger:         logger.With().Str("component", "lag_poller").Logger(),
		stop:           make(chan struct{}),
	}
}

// Start spawns the background polling goroutine. It is safe to call once.
func (p *LagPoller) Start(ctx context.Context) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.poll(ctx)
			}
		}
	}()
}

// Stop signals the polling goroutine to exit and waits for it to finish.
// Calling Stop more than once is safe (subsequent calls return immediately).
func (p *LagPoller) Stop() {
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	p.wg.Wait()
}

// poll performs a single lag-collection cycle across all configured streams.
func (p *LagPoller) poll(ctx context.Context) {
	for _, streamName := range p.streams {
		p.pollStream(ctx, streamName)
	}
}

// pollStream fetches all consumers for a single stream and processes their lag.
func (p *LagPoller) pollStream(ctx context.Context, streamName string) {
	stream, err := p.js.Stream(ctx, streamName)
	if err != nil {
		p.logger.Warn().Err(err).Str("stream", streamName).Msg("failed to get stream")
		return
	}

	lister := stream.ListConsumers(ctx)
	for info := range lister.Info() {
		p.processConsumer(ctx, streamName, info)
	}
	if err := lister.Err(); err != nil {
		p.logger.Warn().Err(err).Str("stream", streamName).Msg("consumer list error")
	}
}

// processConsumer records metrics for a single consumer and handles alert logic.
func (p *LagPoller) processConsumer(ctx context.Context, streamName string, info *jetstream.ConsumerInfo) {
	name := info.Name
	pending := info.NumPending

	p.reg.SetNATSConsumerLag(streamName, name, float64(pending))

	if int(pending) > p.alertThreshold {
		p.counters[name]++
	} else {
		p.counters[name] = 0
	}

	if p.counters[name] >= alertPersistThreshold {
		p.counters[name] = 0
		p.emitAlert(ctx, name, pending)
	}
}

// emitAlert publishes a lag alert event and increments the alert counter.
// FIX-212: migrated to bus.Envelope. tenant_id is the systemTenantID
// sentinel (D5 — infra-global alert, publisher-authored).
func (p *LagPoller) emitAlert(ctx context.Context, consumer string, pending uint64) {
	env := NewEnvelope("nats_consumer_lag", SystemTenantID.String(), severity.High).
		WithSource("infra").
		WithTitle(fmt.Sprintf("NATS consumer lag: %s has %d pending", consumer, pending)).
		SetEntity("consumer", consumer, consumer).
		WithMeta("consumer", consumer).
		WithMeta("pending", pending)

	if err := p.eb.Publish(ctx, SubjectAlertTriggered, env); err != nil {
		p.logger.Error().Err(err).Str("consumer", consumer).Msg("failed to publish lag alert")
	}

	p.reg.IncNATSConsumerLagAlert(consumer)
	p.logger.Warn().Str("consumer", consumer).Uint64("pending", pending).Msg("nats consumer lag alert emitted")
}
