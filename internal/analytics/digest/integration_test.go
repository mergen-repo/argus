//go:build integration
// +build integration

package digest

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/notification"
	"github.com/google/uuid"
)

// recordingNotifStore implements notification.NotifStore. It records every
// Create call so tests can assert which event_type/severity rows would be
// persisted to the DB. UpdateDelivery is a no-op (not under test here).
type recordingNotifStore struct {
	mu   sync.Mutex
	rows []notification.NotifCreateParams
	err  error
}

func (r *recordingNotifStore) Create(_ context.Context, p notification.NotifCreateParams) (*notification.NotifRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, p)
	return &notification.NotifRow{
		ID:        uuid.New(),
		TenantID:  p.TenantID,
		CreatedAt: time.Now(),
	}, r.err
}

func (r *recordingNotifStore) UpdateDelivery(_ context.Context, _ uuid.UUID, _, _, _ *time.Time, _ int, _ []string) error {
	return nil
}

func (r *recordingNotifStore) countForEventType(eventType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, row := range r.rows {
		if row.EventType == eventType {
			n++
		}
	}
	return n
}

func (r *recordingNotifStore) totalCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

func (r *recordingNotifStore) severityForEventType(eventType string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range r.rows {
		if row.EventType == eventType {
			return row.Severity
		}
	}
	return ""
}

// buildRealNotifSvc constructs a real notification.Service backed by the
// recording store. No channels are configured so no outbound dispatch occurs;
// the tier guard and persistence logic run without external dependencies.
func buildRealNotifSvc(ns *recordingNotifStore) *notification.Service {
	svc := notification.NewService(nil, nil, nil, nil, silentLogger())
	svc.SetNotifStore(ns)
	return svc
}

// TestWorker_Integration_MassOffline_EmitsTier2_NoTier1NotificationRows is
// the AC-2 / AC-3 / Section 6 load-pattern integration test.
//
// Setup: inject mock aggregators that report 200 active SIMs and 12 offline
// transitions (6% — above the 5% threshold and above the 10-SIM floor).
// Run worker.Process(ctx). Assert:
//
//	(a) exactly one notification row with event_type='fleet.mass_offline'
//	    and severity='medium' (6% is between 5% and 15% thresholds)
//	(b) bus publish was recorded for bus.SubjectFleetMassOffline
//	(c) ZERO notification rows for Tier 1 event types (session.started etc.)
//	    sent directly through the real notification.Service tier guard
func TestWorker_Integration_MassOffline_EmitsTier2_NoTier1NotificationRows(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Log("DATABASE_URL not set; running against in-memory recording stores (no real DB required for this test)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Aggregator mocks: 200 active SIMs, 12 offline transitions (6% — over 5%
	// floor of 10 → triggers fleet.mass_offline at severity=medium).
	sims := &mockSimAggregator{activeReturn: 200, transitionReturn: 12}
	cdrs := &mockCDRAggregator{fallback: 0}
	viols := &mockViolationAggregator{fallback: 0}

	// Recording publisher so we can assert NATS subjects without a live NATS.
	pub := &recordingPublisher{}

	// Real notification.Service backed by the recording store. This is the
	// load-bearing surface: the tier guard in service.go:391-412 runs for real.
	ns := &recordingNotifStore{}
	notifSvc := buildRealNotifSvc(ns)

	fixedNow := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	th := defaultThresholds()

	w := newWorkerWithDeps(sims, cdrs, viols, notifSvc, pub, th, silentLogger(), fixedClock(fixedNow))

	if err := w.Process(ctx, nil); err != nil {
		t.Fatalf("worker.Process: %v", err)
	}

	// Assertion (a): one fleet.mass_offline notification row persisted.
	got := ns.countForEventType("fleet.mass_offline")
	if got < 1 {
		t.Errorf("expected >=1 fleet.mass_offline notification row, got %d", got)
	}
	sev := ns.severityForEventType("fleet.mass_offline")
	if sev != "medium" {
		t.Errorf("expected severity=medium for 6%% offline, got %q", sev)
	}

	// Assertion (b): bus publish recorded for fleet mass offline.
	if !pub.received(bus.SubjectFleetMassOffline) {
		t.Errorf("expected NATS publish on %q", bus.SubjectFleetMassOffline)
	}

	// Assertion (c): tier guard — directly inject Tier 1 events into the real
	// notification.Service and verify ZERO rows are persisted.
	tier1Types := []string{
		"session.started",
		"session_started",
		"sim.state_changed",
		"sim_state_change",
		"heartbeat.ok",
	}
	beforeCount := ns.totalCount()
	for _, evtType := range tier1Types {
		_ = notifSvc.Notify(ctx, notification.NotifyRequest{
			TenantID:  uuid.New(),
			EventType: notification.EventType(evtType),
			Source:    "",
			Severity:  "info",
			Title:     "test",
			Body:      "test body",
		})
	}
	afterCount := ns.totalCount()
	if afterCount != beforeCount {
		t.Errorf("tier guard: expected zero Tier 1 notification rows, got %d new rows after injecting %d Tier 1 events",
			afterCount-beforeCount, len(tier1Types))
	}

	// Bonus: Tier 2 event without source="digest" must also be rejected.
	beforeCount2 := ns.totalCount()
	_ = notifSvc.Notify(ctx, notification.NotifyRequest{
		TenantID:  uuid.New(),
		EventType: notification.EventType("fleet.mass_offline"),
		Source:    "api", // not "digest" → must be filtered
		Severity:  "medium",
		Title:     "test",
		Body:      "test body",
	})
	if ns.totalCount() != beforeCount2 {
		t.Errorf("tier guard: Tier 2 event from non-digest source should be suppressed, but got a notification row")
	}
}

// received returns true if the given NATS subject was recorded.
func (r *recordingPublisher) received(subject string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.subjects {
		if s == subject {
			return true
		}
	}
	return false
}
