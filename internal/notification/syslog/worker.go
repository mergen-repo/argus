package syslog

// Per-destination buffered worker for STORY-098 Task 5 (AC-8, AC-12, AC-14, AC-15).
//
// Each enabled syslog destination owns one destinationWorker goroutine that:
//   1. Receives formatted bytes from a buffered channel (cap 1000 — AC-15).
//   2. Calls the underlying Transport.Send.
//   3. On success: clears Backoff and records UpdateDelivery(success=true).
//   4. On failure: applies Backoff before next attempt and records
//      UpdateDelivery(success=false, errMsg). A delivery_failed audit event is
//      emitted at most once per minute per destination per AC-14 — additional
//      failures within the same window bump a suppressed-count tracker that
//      ships with the next emitted audit row.
//
// The worker drops the OLDEST queued message on overflow (AC-15) so the bus
// subscriber never blocks on a slow SIEM. Drops increment SyslogDroppedTotal.
//
// Pattern reference: internal/policy/binding/history_writer.go (buffered
// async writer + drop-on-overflow + DropCounter). The differences are:
//   - one worker per destination (not a worker pool over a shared queue);
//   - integrated transport + Backoff (each worker has a 1:1 connection);
//   - per-failure audit emission (history writer does not audit individual
//     drops, only counts them).

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/audit"
	"github.com/rs/zerolog"
)

// DefaultWorkerQueueCap is the per-destination queue capacity per AC-15.
const DefaultWorkerQueueCap = 1000

// auditFailureRateLimitWindow is the AC-14 rate-limit interval for
// log_forwarding.delivery_failed audit emissions (one per destination per
// minute). Subsequent failures within the window increment a suppressed
// counter that ships with the next emitted audit entry.
const auditFailureRateLimitWindow = 1 * time.Minute

// destinationWorker drains the per-destination queue and forwards messages
// over its Transport with backoff + audit instrumentation.
type destinationWorker struct {
	dest      Destination
	transport Transport
	queue     chan []byte
	backoff   Backoff
	metrics   ForwarderMetrics
	auditor   audit.Auditor
	store     DestStoreUpdater
	logger    zerolog.Logger

	mu                 sync.Mutex
	lastAuditFailureAt time.Time
	failuresSuppressed int

	stopCh chan struct{}
	doneCh chan struct{}
	once   sync.Once
}

// newDestinationWorker constructs (but does not start) a worker for dest.
// `transport` MUST already be dialed; the worker takes ownership and Closes
// it on Stop. Pass `cap <= 0` to use DefaultWorkerQueueCap.
func newDestinationWorker(
	dest Destination,
	transport Transport,
	queueCap int,
	metrics ForwarderMetrics,
	auditor audit.Auditor,
	st DestStoreUpdater,
	logger zerolog.Logger,
) *destinationWorker {
	if queueCap <= 0 {
		queueCap = DefaultWorkerQueueCap
	}
	if metrics == nil {
		metrics = noopForwarderMetrics{}
	}
	return &destinationWorker{
		dest:      dest,
		transport: transport,
		queue:     make(chan []byte, queueCap),
		metrics:   metrics,
		auditor:   auditor,
		store:     st,
		logger:    logger.With().Str("component", "syslog_worker").Str("destination", dest.Name).Logger(),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start spawns the worker goroutine. Idempotent — additional calls are no-ops.
func (w *destinationWorker) Start() {
	w.once.Do(func() {
		go w.run()
	})
}

// Enqueue pushes a formatted message onto the queue. Non-blocking. When the
// queue is full it drops the OLDEST queued message (per AC-15 "drop oldest"),
// increments SyslogDroppedTotal, and accepts the new message — keeping the
// freshest events in the pipeline. Returns true when an old message was
// evicted to make room.
//
// Drop loop is bounded: at most queueCap+1 attempts so a sustained
// producer/consumer race cannot livelock here.
func (w *destinationWorker) Enqueue(msg []byte) (dropped bool) {
	select {
	case <-w.stopCh:
		return false
	default:
	}

	maxAttempts := cap(w.queue) + 1
	for i := 0; i < maxAttempts; i++ {
		select {
		case w.queue <- msg:
			return dropped
		default:
			// Queue is full — try to drop the oldest then retry the send.
			select {
			case <-w.queue:
				dropped = true
				w.metrics.IncDropped(w.dest.Transport, w.dest.Format)
				w.logger.Warn().Msg("syslog worker queue full — dropped oldest message")
			default:
				// Race: a consumer drained between our two selects. Retry.
			}
		}
	}
	// Pathological case: queue full AND we lost every drop race. Account
	// for the new message itself as a drop and return.
	w.metrics.IncDropped(w.dest.Transport, w.dest.Format)
	w.logger.Warn().Msg("syslog worker queue persistently full — dropped current message")
	return true
}

// run is the main worker loop. Exits when stopCh closes (Stop) or queue is
// closed via the same path. On exit, drains remaining queue if Stop's ctx
// permits, then closes the transport.
func (w *destinationWorker) run() {
	defer close(w.doneCh)

	for {
		select {
		case msg, ok := <-w.queue:
			if !ok {
				return
			}
			w.send(msg)
		case <-w.stopCh:
			// Drain whatever is buffered with bounded best-effort. The Stop
			// caller's context governs the overall budget; the per-send call
			// here uses a fresh background ctx because the queue items were
			// accepted before shutdown.
			w.drainAfterStop()
			return
		}
	}
}

// drainAfterStop empties the queue with best-effort sends. The Stop ctx in
// Stop() governs how long we wait for workers; this loop simply exits on the
// first non-available read.
func (w *destinationWorker) drainAfterStop() {
	for {
		select {
		case msg, ok := <-w.queue:
			if !ok {
				return
			}
			w.send(msg)
		default:
			return
		}
	}
}

// send delivers a single message and updates delivery state. On failure it
// sleeps for the next backoff interval (or aborts on stopCh) before returning
// — this provides per-destination backpressure without coupling to the bus
// subscriber.
func (w *destinationWorker) send(msg []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := w.transport.Send(ctx, msg)
	cancel()

	if err == nil {
		w.metrics.IncDelivered(w.dest.Transport, w.dest.Format)
		w.backoff.Reset()
		// Best-effort audit-trail update; transient DB errors are logged but
		// do not stop the worker. Use a fresh ctx because the request-scoped
		// ctx may already be cancelled.
		updateCtx, updateCancel := context.WithTimeout(context.Background(), 2*time.Second)
		if updateErr := w.store.UpdateDelivery(updateCtx, w.dest.TenantID, w.dest.ID, true, ""); updateErr != nil {
			w.logger.Warn().Err(updateErr).Msg("syslog worker: update delivery success failed")
		}
		updateCancel()
		return
	}

	w.metrics.IncFailures(w.dest.Transport, w.dest.Format)
	w.recordFailure(err)

	delay := w.backoff.Next()
	select {
	case <-time.After(delay):
	case <-w.stopCh:
	}
}

// recordFailure persists the latest failure on the destination row and emits
// a rate-limited audit entry per AC-14.
func (w *destinationWorker) recordFailure(err error) {
	updateCtx, updateCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer updateCancel()
	if updateErr := w.store.UpdateDelivery(updateCtx, w.dest.TenantID, w.dest.ID, false, err.Error()); updateErr != nil {
		w.logger.Warn().Err(updateErr).Msg("syslog worker: update delivery failure failed")
	}

	// AC-14: rate-limit delivery_failed audit events to once per minute per
	// destination. The first failure in a window emits immediately and resets
	// the suppressed counter; subsequent failures bump the counter and the
	// next emission ships the suppressed count as meta.
	now := time.Now()
	var emit bool
	var suppressed int
	w.mu.Lock()
	if w.lastAuditFailureAt.IsZero() || now.Sub(w.lastAuditFailureAt) >= auditFailureRateLimitWindow {
		emit = true
		suppressed = w.failuresSuppressed
		w.lastAuditFailureAt = now
		w.failuresSuppressed = 0
	} else {
		w.failuresSuppressed++
	}
	w.mu.Unlock()

	if !emit {
		return
	}
	w.emitFailureAudit(err, suppressed)
}

// emitFailureAudit writes a single log_forwarding.delivery_failed audit row.
// Failures here are logged but never block the worker — the next attempt will
// proceed regardless.
func (w *destinationWorker) emitFailureAudit(sendErr error, suppressed int) {
	if w.auditor == nil {
		return
	}
	after := map[string]interface{}{
		"destination_id":   w.dest.ID.String(),
		"destination_name": w.dest.Name,
		"transport":        w.dest.Transport,
		"format":           w.dest.Format,
		"error":            sendErr.Error(),
		"suppressed_count": suppressed,
	}
	afterJSON, _ := json.Marshal(after)

	auditCtx, auditCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer auditCancel()
	if _, err := w.auditor.CreateEntry(auditCtx, audit.CreateEntryParams{
		TenantID:   w.dest.TenantID,
		Action:     "log_forwarding.delivery_failed",
		EntityType: "syslog_destination",
		EntityID:   w.dest.ID.String(),
		AfterData:  afterJSON,
	}); err != nil {
		w.logger.Warn().Err(err).Msg("syslog worker: audit emit failed")
	}
}

// Stop signals the worker to exit, then waits up to ctx's deadline for
// remaining queued messages to drain. Closes the underlying transport
// regardless of ctx outcome. Idempotent.
func (w *destinationWorker) Stop(ctx context.Context) error {
	select {
	case <-w.stopCh:
		// already stopped
	default:
		close(w.stopCh)
	}

	var stopErr error
	select {
	case <-w.doneCh:
	case <-ctx.Done():
		stopErr = ctx.Err()
	}

	if w.transport != nil {
		if err := w.transport.Close(); err != nil && stopErr == nil {
			w.logger.Warn().Err(err).Msg("syslog worker: transport close failed")
		}
	}
	return stopErr
}

// QueueLen returns the current queue depth (test introspection).
func (w *destinationWorker) QueueLen() int {
	return len(w.queue)
}
