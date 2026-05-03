package binding

// Buffered async history writer for STORY-096 / Task 2 (AC-11).
//
// imei_history is the forensic trail for every IMEI capture. Per AC-11
// it is stored asynchronously: the AAA hot path enqueues an entry and
// returns immediately; one or more worker goroutines drain the queue
// and call the underlying flush function (typically
// IMEIHistoryStore.Append).
//
// The writer drops on overflow rather than blocking — a stalled DB MUST
// NOT slow RADIUS/Diameter/5G auth. Drops increment a metric counter so
// SRE can alarm on sustained queue saturation.
//
// PAT-026 (inverse-orphan job processor pattern) does NOT apply here:
// this is an in-process goroutine pipeline, not a job table consumer.
// Entries live only in memory between Append and flushFn.

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// DefaultHistoryQueueCap is the default queue capacity. 1024 entries is
// roughly 30 seconds of headroom at 30 auth/s per worker (the steady-state
// IoT/M2M load — bursts beyond this drop the oldest excess).
const DefaultHistoryQueueCap = 1024

// DefaultHistoryWorkers is the default number of consumer goroutines.
// One worker is sufficient for the steady-state load; Task 7 may
// increase it to 2-4 if the bench reveals queue accumulation under
// burst-load.
const DefaultHistoryWorkers = 1

// HistoryFlushFunc is the per-entry flush callback the writer invokes.
// Returning a non-nil error logs + counts but does NOT requeue (the
// entry is lost — same semantics as a dropped-on-overflow entry, which
// keeps the failure model uniform).
type HistoryFlushFunc func(ctx context.Context, entry HistoryEntry) error

// BufferedHistoryWriter is the concrete HistoryWriter implementation.
// Construct with NewBufferedHistoryWriter, call Start to spawn workers,
// call Shutdown to drain and stop. Append is non-blocking and safe for
// concurrent callers.
type BufferedHistoryWriter struct {
	queue   chan HistoryEntry
	flushFn HistoryFlushFunc
	metrics DropCounter
	logger  zerolog.Logger

	workers int
	wg      sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once
	stopped   chan struct{}
}

// NewBufferedHistoryWriter constructs a writer with a buffered channel
// (capacity = cap, falls back to DefaultHistoryQueueCap when ≤0) and N
// consumer goroutines (workers, falls back to DefaultHistoryWorkers when
// ≤0). flushFn is invoked once per entry. metrics + logger may be nil —
// no-op defaults are substituted.
func NewBufferedHistoryWriter(cap int, workers int, flushFn HistoryFlushFunc, metrics DropCounter, logger zerolog.Logger) *BufferedHistoryWriter {
	if cap <= 0 {
		cap = DefaultHistoryQueueCap
	}
	if workers <= 0 {
		workers = DefaultHistoryWorkers
	}
	if metrics == nil {
		metrics = noopDropCounter{}
	}
	return &BufferedHistoryWriter{
		queue:   make(chan HistoryEntry, cap),
		flushFn: flushFn,
		metrics: metrics,
		logger:  logger,
		workers: workers,
		stopped: make(chan struct{}),
	}
}

// Append enqueues an entry. Non-blocking: drops on full queue and
// increments the drop counter. The ctx argument is intentionally ignored
// for the queue write (queueing must never depend on a request-scoped
// ctx; the worker uses a fresh ctx for the flush call).
func (w *BufferedHistoryWriter) Append(ctx context.Context, e HistoryEntry) {
	select {
	case <-w.stopped:
		// Writer is shutting down — drop silently. The drop counter is
		// not incremented because shutdown is a controlled state, not a
		// saturation event.
		return
	default:
	}

	select {
	case w.queue <- e:
		// queued
	default:
		// Queue full — drop and account for it.
		w.metrics.IncHistoryDropped()
		w.logger.Warn().Str("sim_id", e.SIMID.String()).Str("observed_imei", e.ObservedIMEI).Msg("binding history queue full, dropping entry")
	}
}

// Start spawns the worker goroutines. Idempotent — additional calls are
// no-ops. The ctx argument bounds the worker's flush calls; cancel ctx
// to trigger fast shutdown (Shutdown is the preferred mechanism).
func (w *BufferedHistoryWriter) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		for i := 0; i < w.workers; i++ {
			w.wg.Add(1)
			go w.run(ctx)
		}
	})
}

// run is the worker loop. It exits when the queue is closed (drained)
// or when the writer's stopped channel is closed (fast-stop on ctx
// cancel during Shutdown).
func (w *BufferedHistoryWriter) run(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case entry, ok := <-w.queue:
			if !ok {
				return
			}
			w.flush(ctx, entry)
		case <-ctx.Done():
			// Drain whatever is buffered with a fresh background ctx so a
			// cancelled parent doesn't prevent flushing the entries we
			// already accepted. The Shutdown caller's deadline applies via
			// its own select on the wg below.
			w.drainRemaining()
			return
		}
	}
}

// drainRemaining flushes the rest of the queue with a fresh, bounded
// context. Called when the original ctx is cancelled mid-Start.
func (w *BufferedHistoryWriter) drainRemaining() {
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		select {
		case entry, ok := <-w.queue:
			if !ok {
				return
			}
			w.flush(flushCtx, entry)
		default:
			return
		}
	}
}

// flush invokes flushFn and accounts for its outcome.
func (w *BufferedHistoryWriter) flush(ctx context.Context, e HistoryEntry) {
	if w.flushFn == nil {
		w.logger.Debug().Msg("binding history writer: nil flushFn, dropping entry")
		w.metrics.IncHistoryDropped()
		return
	}
	if err := w.flushFn(ctx, e); err != nil {
		// Log + count but do not requeue. Requeue would risk an infinite
		// retry loop on a permanent DB error and would let bursts pile up
		// indefinitely.
		w.logger.Warn().Err(err).Str("sim_id", e.SIMID.String()).Msg("binding history flush failed")
		w.metrics.IncHistoryDropped()
	}
}

// Shutdown closes the queue and waits for workers to drain. If ctx
// expires before drain completes, returns ctx.Err(); the workers
// continue draining in the background until the process exits (or the
// remaining entries are dropped if the workers are blocked on a slow
// flushFn).
//
// Shutdown is idempotent — additional calls return nil immediately.
func (w *BufferedHistoryWriter) Shutdown(ctx context.Context) error {
	var shutdownErr error
	w.stopOnce.Do(func() {
		// Signal Append to stop accepting new entries.
		close(w.stopped)
		// Close the queue so workers exit once it drains.
		close(w.queue)

		done := make(chan struct{})
		go func() {
			w.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			shutdownErr = nil
		case <-ctx.Done():
			shutdownErr = ctx.Err()
		}
	})
	return shutdownErr
}

// QueueLen returns the current depth of the queue (test introspection).
func (w *BufferedHistoryWriter) QueueLen() int {
	return len(w.queue)
}

// ErrShutdown is returned for callers that want to distinguish a clean
// shutdown timeout from other errors. Currently only Shutdown returns
// errors and it surfaces ctx.Err() directly — this constant exists so
// future code paths can check errors.Is.
var ErrShutdown = errors.New("binding: history writer shutting down")
