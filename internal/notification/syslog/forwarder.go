package syslog

// Top-level forwarder for STORY-098 Task 5 (AC-11, AC-12, AC-14, AC-15).
//
// Responsibilities:
//   - Subscribe to NATS subject `argus.events.>` via QueueSubscribe (queue
//     group "syslog-forwarder") so multi-instance deployments do not duplicate
//     dispatches.
//   - On every envelope:
//       1. Derive category from the subject (categoryForSubject).
//       2. For every enabled destination matching the filter, format the
//          envelope per its Format field and Enqueue to that destination's
//          per-destination worker (drop-on-overflow non-blocking).
//   - Refresh the destination roster every refreshInterval (default 30s),
//     diffing the new list against running workers — adding new ones,
//     stopping removed ones, and replacing transports when host/port/transport
//     changes.
//   - Stop drains all worker queues with a caller-supplied timeout.
//
// PAT-026 RECURRENCE prevention: Forwarder MUST be wired in cmd/argus/main.go
// AND a paired test (forwarder_boot_test.go) verifies that wiring exists.
// Earlier IMEI/binding stories shipped this guard; STORY-098 maintains it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Destination is the forwarder's own view of a syslog destination row.
//
// We deliberately do NOT import the canonical store.SyslogDestination type
// here: store/syslog_destination_test.go imports `internal/notification/syslog`
// for PAT-022 enum-set tests, so a reverse import would create a cycle. The
// adapter in cmd/argus/main.go converts store rows into Destination values at
// the boundary (DestStoreAdapter — see main.go).
type Destination struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Name              string
	Host              string
	Port              int
	Transport         string
	Format            string
	Facility          int
	SeverityFloor     *int
	FilterCategories  []string
	FilterMinSeverity *int
	TLSCAPEM          *string
	TLSClientCertPEM  *string
	TLSClientKeyPEM   *string
}

// Default tunables.
const (
	// SubjectAllEvents is the wildcard NATS subject the forwarder listens on.
	// It matches every `argus.events.<category>.<…>` subject and excludes the
	// `argus.jobs.>` and `argus.cache.invalidate` subjects (per plan).
	SubjectAllEvents = "argus.events.>"

	// QueueGroupForwarder is the NATS queue-group name. Multi-instance
	// deployments share this group so each envelope is dispatched once.
	QueueGroupForwarder = "syslog-forwarder"

	// DefaultRefreshInterval is how often the forwarder polls the store for
	// new/changed/removed destinations. Future enhancement: subscribe to a
	// settings.log_forwarding.changed NATS event for instant refresh.
	DefaultRefreshInterval = 30 * time.Second
)

// ForwarderMetrics is the narrow Prometheus contract the forwarder + workers
// depend on. Implementations live in `internal/observability/metrics`. Nil is
// permitted via noopForwarderMetrics.
type ForwarderMetrics interface {
	IncDelivered(transport, format string)
	IncDropped(transport, format string)
	IncFailures(transport, format string)
}

type noopForwarderMetrics struct{}

func (noopForwarderMetrics) IncDelivered(string, string) {}
func (noopForwarderMetrics) IncDropped(string, string)   {}
func (noopForwarderMetrics) IncFailures(string, string)  {}

// BusSubscriber is the narrow contract the forwarder needs from
// `internal/bus.EventBus`. Mocked in forwarder_test.go.
type BusSubscriber interface {
	QueueSubscribe(subject, queue string, handler func(subject string, data []byte)) (BusSubscription, error)
}

// BusSubscription mirrors bus.Subscription for shutdown.
type BusSubscription interface {
	Unsubscribe() error
}

// DestStore is the narrow store contract the forwarder depends on. The
// concrete implementation lives in cmd/argus/main.go (DestStoreAdapter wrapping
// store.SyslogDestinationStore). Mocked in forwarder_test.go.
type DestStore interface {
	ListAllEnabled(ctx context.Context) ([]Destination, error)
	DestStoreUpdater
}

// DestStoreUpdater is the per-destination delivery-state updater. Same type
// signature as store.SyslogDestinationStore.UpdateDelivery so the adapter in
// main.go can be a thin pass-through.
type DestStoreUpdater interface {
	UpdateDelivery(ctx context.Context, tenantID uuid.UUID, id uuid.UUID, success bool, errMsg string) error
}

// transportFactory builds a Transport for the given destination. Override in
// tests to inject mocks. Nil → defaultTransportFactory.
type transportFactory func(dest Destination) (Transport, error)

// Forwarder is the top-level coordinator. Construct with NewForwarder, then
// call Start to spawn the bus subscriber + initial worker pool.
type Forwarder struct {
	store   DestStore
	auditor audit.Auditor
	metrics ForwarderMetrics
	logger  zerolog.Logger

	hostname string
	pid      int

	refreshInterval time.Duration
	transportFn     transportFactory
	queueCap        int

	mu      sync.RWMutex
	workers map[uuid.UUID]*destinationWorker
	subs    []BusSubscription

	stopCh    chan struct{}
	doneCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

// ForwarderOption configures NewForwarder.
type ForwarderOption func(*Forwarder)

// WithRefreshInterval overrides the default 30s polling cadence.
func WithRefreshInterval(d time.Duration) ForwarderOption {
	return func(f *Forwarder) {
		if d > 0 {
			f.refreshInterval = d
		}
	}
}

// WithTransportFactory injects a custom transport builder (test-only).
func WithTransportFactory(fn transportFactory) ForwarderOption {
	return func(f *Forwarder) { f.transportFn = fn }
}

// WithQueueCap overrides the per-destination worker queue capacity.
func WithQueueCap(n int) ForwarderOption {
	return func(f *Forwarder) {
		if n > 0 {
			f.queueCap = n
		}
	}
}

// NewForwarder constructs a Forwarder. Caller MUST invoke Start to spawn the
// subscriber + worker pool, and Stop on shutdown to drain queues + close
// transports. Hostname falls back to os.Hostname; PID is os.Getpid().
func NewForwarder(s DestStore, auditor audit.Auditor, m ForwarderMetrics, logger zerolog.Logger, opts ...ForwarderOption) *Forwarder {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "argus"
	}
	if m == nil {
		m = noopForwarderMetrics{}
	}
	f := &Forwarder{
		store:           s,
		auditor:         auditor,
		metrics:         m,
		logger:          logger.With().Str("component", "syslog_forwarder").Logger(),
		hostname:        hostname,
		pid:             os.Getpid(),
		refreshInterval: DefaultRefreshInterval,
		queueCap:        DefaultWorkerQueueCap,
		workers:         make(map[uuid.UUID]*destinationWorker),
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
	for _, opt := range opts {
		opt(f)
	}
	if f.transportFn == nil {
		f.transportFn = defaultTransportFactory
	}
	return f
}

// Start subscribes to the NATS bus and spawns the initial worker pool. Idempotent.
//
// The subscriber runs on the NATS client goroutine — handler invocations are
// fast (format → Enqueue) so the subscription does not become a bottleneck.
// A single background goroutine drives the periodic RefreshDestinations call.
func (f *Forwarder) Start(ctx context.Context, sub BusSubscriber) error {
	var startErr error
	f.startOnce.Do(func() {
		// 1. Initial worker pool from the store.
		if err := f.RefreshDestinations(ctx); err != nil {
			startErr = fmt.Errorf("syslog: initial refresh: %w", err)
			return
		}

		// 2. Subscribe to argus.events.> with a queue group.
		busSub, err := sub.QueueSubscribe(SubjectAllEvents, QueueGroupForwarder, func(subject string, data []byte) {
			f.handleEvent(subject, data)
		})
		if err != nil {
			startErr = fmt.Errorf("syslog: bus subscribe: %w", err)
			return
		}
		f.subs = append(f.subs, busSub)

		// 3. Periodic refresh loop.
		go f.refreshLoop()

		f.mu.RLock()
		nWorkers := len(f.workers)
		f.mu.RUnlock()
		f.logger.Info().Int("destinations", nWorkers).Str("subject", SubjectAllEvents).Msg("syslog forwarder started")
	})
	return startErr
}

// refreshLoop periodically calls RefreshDestinations. Exits on stopCh.
func (f *Forwarder) refreshLoop() {
	defer close(f.doneCh)
	tick := time.NewTicker(f.refreshInterval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := f.RefreshDestinations(ctx); err != nil {
				f.logger.Warn().Err(err).Msg("syslog forwarder: refresh failed")
			}
			cancel()
		case <-f.stopCh:
			return
		}
	}
}

// RefreshDestinations reloads the enabled-destination roster from the store
// and reconciles it against the running worker pool. Adds new workers, stops
// removed ones, and replaces workers whose connection-relevant fields changed
// (host, port, transport, TLS material).
func (f *Forwarder) RefreshDestinations(ctx context.Context) error {
	dests, err := f.store.ListAllEnabled(ctx)
	if err != nil {
		return fmt.Errorf("list enabled destinations: %w", err)
	}

	desired := make(map[uuid.UUID]Destination, len(dests))
	for _, d := range dests {
		desired[d.ID] = d
	}

	f.mu.Lock()
	// Detect removals + replacements.
	for id, w := range f.workers {
		newDest, stillEnabled := desired[id]
		if !stillEnabled {
			f.logger.Info().Str("destination", w.dest.Name).Msg("syslog forwarder: removing worker")
			delete(f.workers, id)
			go func(worker *destinationWorker) {
				stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = worker.Stop(stopCtx)
			}(w)
			continue
		}
		if connectionChanged(w.dest, newDest) || filterChanged(w.dest, newDest) {
			// Workers treat their dest as immutable. Any change — connection
			// or filter — replaces the worker so the bus subscriber's
			// concurrent read of w.dest in handleEvent sees a consistent
			// snapshot for the lifetime of that worker. The replacement
			// transport reuses the existing dial cost amortized over time;
			// destination edits are rare (admin action), reads are hot.
			f.logger.Info().Str("destination", w.dest.Name).Msg("syslog forwarder: replacing worker (config changed)")
			delete(f.workers, id)
			go func(worker *destinationWorker) {
				stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = worker.Stop(stopCtx)
			}(w)
			// Fall through to the additions step which inserts the replacement.
		}
	}
	// Add new workers.
	for id, d := range desired {
		if _, exists := f.workers[id]; exists {
			continue
		}
		t, err := f.transportFn(d)
		if err != nil {
			f.logger.Warn().Err(err).Str("destination", d.Name).Msg("syslog forwarder: transport build failed")
			continue
		}
		w := newDestinationWorker(d, t, f.queueCap, f.metrics, f.auditor, f.store, f.logger)
		w.Start()
		f.workers[id] = w
		f.logger.Info().Str("destination", d.Name).Str("transport", d.Transport).Str("format", d.Format).Msg("syslog forwarder: worker added")
	}
	f.mu.Unlock()
	return nil
}

// connectionChanged reports whether two destination snapshots differ on any
// field that requires re-dialing the underlying transport.
func connectionChanged(a, b Destination) bool {
	if a.Host != b.Host || a.Port != b.Port || a.Transport != b.Transport {
		return true
	}
	if !ptrStringEqual(a.TLSCAPEM, b.TLSCAPEM) {
		return true
	}
	if !ptrStringEqual(a.TLSClientCertPEM, b.TLSClientCertPEM) {
		return true
	}
	if !ptrStringEqual(a.TLSClientKeyPEM, b.TLSClientKeyPEM) {
		return true
	}
	return false
}

// filterChanged reports whether the destination snapshot differs on any
// non-connection field (filters/format/facility/etc.). When only filters
// change we still replace the worker — workers treat dest as immutable to
// avoid a write-while-read race against the bus subscriber goroutine.
func filterChanged(a, b Destination) bool {
	if a.Format != b.Format || a.Facility != b.Facility {
		return true
	}
	if !ptrIntEqual(a.SeverityFloor, b.SeverityFloor) {
		return true
	}
	if !ptrIntEqual(a.FilterMinSeverity, b.FilterMinSeverity) {
		return true
	}
	if !stringSliceEqual(a.FilterCategories, b.FilterCategories) {
		return true
	}
	return false
}

func ptrIntEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func ptrStringEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// handleEvent is invoked once per delivered NATS message. It strict-parses
// the envelope, derives category, and dispatches to every matching enabled
// worker. Handler errors are logged but never propagate — a malformed event
// must NOT kill the subscriber.
func (f *Forwarder) handleEvent(subject string, data []byte) {
	var env bus.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		f.logger.Debug().Err(err).Str("subject", subject).Msg("syslog forwarder: envelope unmarshal failed")
		return
	}
	if err := env.Validate(); err != nil {
		// Legacy-shape / invalid envelopes are skipped silently — the bus
		// owns the legacy-shape counter; we are a downstream consumer.
		f.logger.Debug().Err(err).Str("subject", subject).Msg("syslog forwarder: envelope validation failed")
		return
	}

	category := categoryForSubject(subject)

	f.mu.RLock()
	workers := make([]*destinationWorker, 0, len(f.workers))
	for _, w := range f.workers {
		workers = append(workers, w)
	}
	f.mu.RUnlock()

	for _, w := range workers {
		if !workerAccepts(w.dest, &env, category) {
			continue
		}
		cfg := DestConfig{
			Format:     w.dest.Format,
			Hostname:   f.hostname,
			PID:        f.pid,
			Facility:   w.dest.Facility,
			Enterprise: EnterprisePEN,
		}
		msg, err := Format(&env, cfg)
		if err != nil {
			f.logger.Debug().Err(err).Str("destination", w.dest.Name).Msg("syslog forwarder: format failed")
			continue
		}
		w.Enqueue(msg)
	}
}

// workerAccepts reports whether the destination's filter accepts this
// envelope. Filter rules:
//   - destination.FilterCategories: list of canonical categories. Empty ->
//     accept any category. Non-empty -> category MUST be in the list.
//   - destination.TenantID: cross-tenant fan-out. Each destination only
//     receives envelopes from its own tenant_id.
//   - destination.FilterMinSeverity: ordinal floor (1=info..5=critical).
//     Nil -> no floor. Non-nil -> envelope ordinal MUST be >= floor.
//   - destination.SeverityFloor: identical semantics to FilterMinSeverity.
//     Both gates apply (AND): an envelope must satisfy BOTH floors when both
//     are set. Rationale (VAL-076): SeverityFloor is the persisted
//     destination-level cutoff exposed via API/UI; FilterMinSeverity is the
//     filter-block override the FE may set independently. v1 ships them as
//     two parallel floors so a future UX may expose the distinction.
func workerAccepts(dest Destination, env *bus.Envelope, category string) bool {
	if env.TenantID != dest.TenantID.String() {
		return false
	}
	if len(dest.FilterCategories) > 0 {
		matched := false
		for _, c := range dest.FilterCategories {
			if c == category {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	envOrd := severity.Ordinal(env.Severity)
	if dest.FilterMinSeverity != nil {
		if envOrd < *dest.FilterMinSeverity {
			return false
		}
	}
	if dest.SeverityFloor != nil {
		if envOrd < *dest.SeverityFloor {
			return false
		}
	}
	return true
}

// categoryForSubject derives the canonical category from the NATS subject.
// Subject shape: `argus.events.<category-or-domain>.<…>`. The mapping mirrors
// the plan §"Bus Subject → Category Mapping" table verbatim:
//
//	auth.*               -> auth
//	audit.*              -> audit
//	alert.*              -> alert
//	session.*            -> session
//	policy.*             -> policy
//	imei.*               -> imei
//	device.binding_*     -> imei  (STORY-097 binding subjects fold into imei)
//	(everything else)    -> system
//
// Returns "system" for malformed subjects so the dispatcher always has a
// usable label (defensive; the subscriber filter `argus.events.>` already
// guarantees the prefix).
func categoryForSubject(subject string) string {
	const prefix = "argus.events."
	if !strings.HasPrefix(subject, prefix) {
		return CategorySystem
	}
	rest := subject[len(prefix):]
	// Second segment is the canonical-category bucket. Edge case: device.binding_*
	// is folded into imei per VAL-098-05.
	idx := strings.IndexByte(rest, '.')
	var head, tail string
	if idx < 0 {
		head = rest
	} else {
		head = rest[:idx]
		tail = rest[idx+1:]
	}
	switch head {
	case "auth":
		return CategoryAuth
	case "audit":
		return CategoryAudit
	case "alert":
		return CategoryAlert
	case "session":
		return CategorySession
	case "policy":
		return CategoryPolicy
	case "imei":
		return CategoryIMEI
	case "device":
		// device.binding_* -> imei; other device.* fall through to system.
		if strings.HasPrefix(tail, "binding_") || strings.HasPrefix(tail, "binding.") {
			return CategoryIMEI
		}
		return CategorySystem
	default:
		return CategorySystem
	}
}

// Stop tears down the subscriber + drains every worker. Idempotent. The ctx
// bounds the drain budget; on timeout, undelivered queued messages are lost.
func (f *Forwarder) Stop(ctx context.Context) error {
	var stopErr error
	f.stopOnce.Do(func() {
		// Unsubscribe first so no new envelopes arrive.
		for _, sub := range f.subs {
			if err := sub.Unsubscribe(); err != nil {
				f.logger.Warn().Err(err).Msg("syslog forwarder: unsubscribe failed")
			}
		}
		f.subs = nil

		// Stop the refresh loop.
		select {
		case <-f.stopCh:
		default:
			close(f.stopCh)
		}
		select {
		case <-f.doneCh:
		case <-ctx.Done():
			stopErr = ctx.Err()
		}

		// Drain workers in parallel — give every worker the same residual
		// budget instead of serializing.
		f.mu.Lock()
		workers := make([]*destinationWorker, 0, len(f.workers))
		for _, w := range f.workers {
			workers = append(workers, w)
		}
		f.workers = make(map[uuid.UUID]*destinationWorker)
		f.mu.Unlock()

		var wg sync.WaitGroup
		for _, w := range workers {
			wg.Add(1)
			go func(worker *destinationWorker) {
				defer wg.Done()
				_ = worker.Stop(ctx)
			}(w)
		}
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			if stopErr == nil {
				stopErr = ctx.Err()
			}
		}
		f.logger.Info().Msg("syslog forwarder stopped")
	})
	return stopErr
}

// defaultTransportFactory builds the appropriate Transport for dest.
// Returns an error when the transport type is unknown — the worker is then
// not added to the pool until the destination is corrected.
func defaultTransportFactory(dest Destination) (Transport, error) {
	cfg := TransportConfig{
		Host: dest.Host,
		Port: dest.Port,
	}
	if dest.TLSCAPEM != nil {
		cfg.TLSCAPEM = []byte(*dest.TLSCAPEM)
	}
	if dest.TLSClientCertPEM != nil {
		cfg.TLSClientCertPEM = []byte(*dest.TLSClientCertPEM)
	}
	if dest.TLSClientKeyPEM != nil {
		cfg.TLSClientKeyPEM = []byte(*dest.TLSClientKeyPEM)
	}
	switch dest.Transport {
	case TransportUDP:
		return NewUDPTransport(cfg)
	case TransportTCP:
		return NewTCPTransport(cfg)
	case TransportTLS:
		return NewTLSTransport(cfg)
	default:
		return nil, fmt.Errorf("syslog: unknown transport %q", dest.Transport)
	}
}
