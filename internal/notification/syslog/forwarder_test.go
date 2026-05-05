package syslog

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/notification/syslog/syslogtest"
	"github.com/btopcu/argus/internal/severity"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ── test doubles ──────────────────────────────────────────────────────────

// fakeSubscriber captures registered handlers and lets tests replay events.
// Implements syslog.BusSubscriber.
type fakeSubscriber struct {
	mu      sync.Mutex
	handler func(string, []byte)
}

func (s *fakeSubscriber) QueueSubscribe(_ string, _ string, h func(string, []byte)) (BusSubscription, error) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
	return &fakeSub{}, nil
}

// Deliver pushes a single envelope as if NATS had delivered it.
func (s *fakeSubscriber) Deliver(subject string, env *bus.Envelope) {
	data, _ := json.Marshal(env)
	s.mu.Lock()
	h := s.handler
	s.mu.Unlock()
	if h != nil {
		h(subject, data)
	}
}

type fakeSub struct{}

func (fakeSub) Unsubscribe() error { return nil }

// listFakeStore wraps fakeStore with a configurable list result.
type listFakeStore struct {
	*fakeStore
	mu    sync.Mutex
	dests []Destination
}

func newListFakeStore(initial []Destination) *listFakeStore {
	cp := make([]Destination, len(initial))
	copy(cp, initial)
	return &listFakeStore{
		fakeStore: &fakeStore{},
		dests:     cp,
	}
}

func (s *listFakeStore) ListAllEnabled(_ context.Context) ([]Destination, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Destination, len(s.dests))
	copy(out, s.dests)
	return out, nil
}

func (s *listFakeStore) Set(dests []Destination) {
	s.mu.Lock()
	s.dests = dests
	s.mu.Unlock()
}

// makeEnv constructs a valid bus.Envelope for forwarder dispatch tests.
func makeEnv(tenantID uuid.UUID, evtType, sev string) *bus.Envelope {
	e := bus.NewEnvelope(evtType, tenantID.String(), sev)
	e.Source = "test"
	e.Title = "t"
	return e
}

// dest builds a destination matching the given UDP listener.
func dest(tenantID uuid.UUID, name, host string, port int, format string, categories []string, minSev *int) Destination {
	return Destination{
		ID:                uuid.New(),
		TenantID:          tenantID,
		Name:              name,
		Host:              host,
		Port:              port,
		Transport:         TransportUDP,
		Format:            format,
		Facility:          16,
		FilterCategories:  categories,
		FilterMinSeverity: minSev,
	}
}

// ── tests ─────────────────────────────────────────────────────────────────

// TestCategoryForSubject covers the plan §"Bus Subject → Category Mapping"
// table verbatim plus an unknown-prefix fallback to "system".
func TestCategoryForSubject(t *testing.T) {
	cases := []struct {
		subject string
		want    string
	}{
		{"argus.events.auth.attempt", CategoryAuth},
		{"argus.events.audit.create", CategoryAudit},
		{"argus.events.alert.triggered", CategoryAlert},
		{"argus.events.session.started", CategorySession},
		{"argus.events.session.ended", CategorySession},
		{"argus.events.policy.changed", CategoryPolicy},
		{"argus.events.policy.rollout_progress", CategoryPolicy},
		{"argus.events.imei.changed", CategoryIMEI},
		{"argus.events.device.binding_grace_expiring", CategoryIMEI},
		{"argus.events.device.binding_mismatch", CategoryIMEI},
		{"argus.events.system.heartbeat", CategorySystem},
		{"argus.events.sim.updated", CategorySystem},
		{"argus.events.operator.health", CategorySystem},
		{"argus.events.notification.dispatch", CategorySystem},
		{"argus.events.fleet.mass_offline", CategorySystem},
		{"argus.events.unknown.foo", CategorySystem},
		{"not.even.our.namespace", CategorySystem},
	}
	for _, c := range cases {
		c := c
		t.Run(c.subject, func(t *testing.T) {
			if got := categoryForSubject(c.subject); got != c.want {
				t.Errorf("categoryForSubject(%q) = %q; want %q", c.subject, got, c.want)
			}
		})
	}
}

// helperSetup boots a Forwarder backed by mock UDP listeners.
//
//	dests       — initial destinations (host/port set to listener addrs by caller)
//	subscriber  — captured for hand-driven event delivery
//	cleanup     — defer to stop forwarder + listener
func helperSetup(t *testing.T, dests []Destination) (*Forwarder, *fakeSubscriber, *listFakeStore, func()) {
	t.Helper()
	st := newListFakeStore(dests)
	sub := &fakeSubscriber{}

	f := NewForwarder(st, &recordingAuditor{}, newRecordingMetrics(), zerolog.Nop(),
		WithRefreshInterval(50*time.Millisecond),
	)

	if err := f.Start(context.Background(), sub); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cleanup := func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = f.Stop(stopCtx)
	}
	return f, sub, st, cleanup
}

// TestForwarder_RoundTrip_OneDestination — single UDP destination + 3 events
// → all 3 messages appear at the listener.
func TestForwarder_RoundTrip_OneDestination(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()
	d := dest(tenant, "siem-prod", host, port, FormatRFC5424, nil, nil)

	f, sub, _, cleanup := helperSetup(t, []Destination{d})
	defer cleanup()
	_ = f

	for i := 0; i < 3; i++ {
		sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))
	}

	got := listener.Wait(3, 2*time.Second)
	if len(got) < 3 {
		t.Fatalf("expected >=3 messages at listener, got %d", len(got))
	}
}

// TestForwarder_FilterByCategory_Excluded — destination whitelists audit+alert
// only; a session envelope must NOT reach the listener.
func TestForwarder_FilterByCategory_Excluded(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()
	d := dest(tenant, "audit-only", host, port, FormatRFC5424, []string{CategoryAudit, CategoryAlert}, nil)

	_, sub, _, cleanup := helperSetup(t, []Destination{d})
	defer cleanup()

	sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))

	got := listener.Wait(1, 250*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected 0 messages (category filter), got %d", len(got))
	}
}

// TestForwarder_FilterByMinSeverity — destination floor=high (ordinal 4); an
// info envelope (ordinal 1) must be filtered.
func TestForwarder_FilterByMinSeverity(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()
	floor := severity.Ordinal(severity.High) // 4
	d := dest(tenant, "high-only", host, port, FormatRFC5424, nil, &floor)

	_, sub, _, cleanup := helperSetup(t, []Destination{d})
	defer cleanup()

	sub.Deliver("argus.events.alert.triggered", makeEnv(tenant, "alert.triggered", severity.Info))
	sub.Deliver("argus.events.alert.triggered", makeEnv(tenant, "alert.triggered", severity.Critical))

	got := listener.Wait(1, 1*time.Second)
	if len(got) != 1 {
		t.Fatalf("expected 1 message (only critical passes floor=high), got %d", len(got))
	}
}

// TestForwarder_TwoDestinations_BothReceive — same envelope dispatched to two
// destinations under the same tenant; both listeners must observe it.
func TestForwarder_TwoDestinations_BothReceive(t *testing.T) {
	listenerA, addrA := syslogtest.NewUDPListener(t)
	defer listenerA.Close()
	listenerB, addrB := syslogtest.NewUDPListener(t)
	defer listenerB.Close()

	hostA, portA := splitHostPort(t, addrA)
	hostB, portB := splitHostPort(t, addrB)
	tenant := uuid.New()
	dA := dest(tenant, "a", hostA, portA, FormatRFC5424, nil, nil)
	dB := dest(tenant, "b", hostB, portB, FormatRFC3164, nil, nil)

	_, sub, _, cleanup := helperSetup(t, []Destination{dA, dB})
	defer cleanup()

	sub.Deliver("argus.events.audit.create", makeEnv(tenant, "audit.create", severity.Info))

	if got := listenerA.Wait(1, 1*time.Second); len(got) != 1 {
		t.Fatalf("destination A: expected 1 message, got %d", len(got))
	}
	if got := listenerB.Wait(1, 1*time.Second); len(got) != 1 {
		t.Fatalf("destination B: expected 1 message, got %d", len(got))
	}
}

// TestForwarder_DisabledDestination_NoDelivery — a disabled destination is
// excluded by ListAllEnabled (the WHERE clause); confirm no delivery.
func TestForwarder_DisabledDestination_NoDelivery(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	_ = addr
	tenant := uuid.New()

	// The forwarder only sees rows returned by ListAllEnabled, which mirrors
	// the production `WHERE enabled = TRUE` filter. A "disabled destination"
	// is therefore one absent from the returned slice. We pass an empty list
	// to confirm no worker is created and no envelope reaches the listener.
	_, sub, _, cleanup := helperSetup(t, []Destination{})
	defer cleanup()

	sub.Deliver("argus.events.alert.triggered", makeEnv(tenant, "alert.triggered", severity.Info))

	got := listener.Wait(1, 200*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("expected 0 messages (disabled destination), got %d", len(got))
	}
}

// TestForwarder_RefreshAddsNewDestination — start with empty, then mutate the
// store + wait for periodic refresh + send envelope → message arrives.
func TestForwarder_RefreshAddsNewDestination(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()

	f, sub, st, cleanup := helperSetup(t, []Destination{})
	defer cleanup()

	// Add a destination after the forwarder is running.
	st.Set([]Destination{
		dest(tenant, "late", host, port, FormatRFC5424, nil, nil),
	})

	// Wait for the periodic refresh to pick up the change. Refresh interval
	// in helperSetup is 50ms.
	if !waitFor(2*time.Second, func() bool {
		f.mu.RLock()
		n := len(f.workers)
		f.mu.RUnlock()
		return n == 1
	}) {
		t.Fatalf("expected refresh to add 1 worker; never saw it")
	}

	sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))
	got := listener.Wait(1, 1*time.Second)
	if len(got) != 1 {
		t.Fatalf("expected 1 message after refresh, got %d", len(got))
	}
}

// TestForwarder_RefreshRemovesStale — start with a destination, drop it from
// the store, wait for refresh → worker is gone, no further deliveries.
func TestForwarder_RefreshRemovesStale(t *testing.T) {
	listener, addr := syslogtest.NewUDPListener(t)
	defer listener.Close()

	host, port := splitHostPort(t, addr)
	tenant := uuid.New()
	d := dest(tenant, "doomed", host, port, FormatRFC5424, nil, nil)

	f, sub, st, cleanup := helperSetup(t, []Destination{d})
	defer cleanup()

	// Confirm delivery works first.
	sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))
	if got := listener.Wait(1, 1*time.Second); len(got) != 1 {
		t.Fatalf("baseline: expected 1 msg, got %d", len(got))
	}

	// Now remove and wait for the worker pool to drain.
	st.Set([]Destination{})
	if !waitFor(2*time.Second, func() bool {
		f.mu.RLock()
		n := len(f.workers)
		f.mu.RUnlock()
		return n == 0
	}) {
		t.Fatalf("expected refresh to remove worker; still present")
	}

	// New events must not reach the listener.
	pre := len(listener.Messages())
	sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))
	time.Sleep(150 * time.Millisecond)
	post := len(listener.Messages())
	if post != pre {
		t.Fatalf("expected no new deliveries after removal, got %d new", post-pre)
	}
}

// TestForwarder_OneSlowDestinationDoesNotBlockOthers — 2 destinations: one
// slow (TCP listener that closes quickly so Send fails repeatedly) and one
// fast (UDP). Fast destination must continue receiving.
func TestForwarder_OneSlowDestinationDoesNotBlockOthers(t *testing.T) {
	fastListener, fastAddr := syslogtest.NewUDPListener(t)
	defer fastListener.Close()

	fastHost, fastPort := splitHostPort(t, fastAddr)
	tenant := uuid.New()
	fast := dest(tenant, "fast", fastHost, fastPort, FormatRFC5424, nil, nil)

	// Slow: a destination pointed at a closed port. The transport build will
	// succeed (UDP doesn't dial-verify) but Send may or may not fail. To
	// guarantee per-destination isolation we use a custom transport factory
	// that returns a stalling transport for the slow destination and the
	// real UDP transport for the fast one.
	slowID := uuid.New()
	slow := Destination{
		ID:        slowID,
		TenantID:  tenant,
		Name:      "slow",
		Host:      "127.0.0.1",
		Port:      1, // unreachable
		Transport: TransportUDP,
		Format:    FormatRFC5424,
		Facility:  16,
	}

	st := newListFakeStore([]Destination{fast, slow})
	sub := &fakeSubscriber{}

	slowTr := &fakeTransport{delay: 500 * time.Millisecond}

	f := NewForwarder(st, &recordingAuditor{}, newRecordingMetrics(), zerolog.Nop(),
		WithRefreshInterval(1*time.Second),
		WithTransportFactory(func(d Destination) (Transport, error) {
			if d.ID == slowID {
				return slowTr, nil
			}
			return defaultTransportFactory(d)
		}),
	)
	if err := f.Start(context.Background(), sub); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = f.Stop(ctx)
	}()

	// Hammer 5 envelopes — the slow worker stalls but the fast UDP listener
	// must receive them all promptly.
	for i := 0; i < 5; i++ {
		sub.Deliver("argus.events.session.started", makeEnv(tenant, "session.started", severity.Info))
	}

	got := fastListener.Wait(5, 1500*time.Millisecond)
	if len(got) != 5 {
		t.Fatalf("fast destination should have received 5 quickly; got %d", len(got))
	}
}

// TestWorkerAccepts_TenantIsolation — worker only accepts envelopes from its
// own tenant. Pure unit test on the filter helper.
func TestWorkerAccepts_TenantIsolation(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	d := dest(tenantA, "x", "127.0.0.1", 514, FormatRFC5424, nil, nil)
	envA := makeEnv(tenantA, "session.started", severity.Info)
	envB := makeEnv(tenantB, "session.started", severity.Info)

	if !workerAccepts(d, envA, CategorySession) {
		t.Errorf("destination must accept its own tenant's envelope")
	}
	if workerAccepts(d, envB, CategorySession) {
		t.Errorf("destination must NOT accept other-tenant envelope")
	}
}

// TestWorkerAccepts_SeverityFloor — destination.SeverityFloor gates dispatch
// just like FilterMinSeverity. STORY-098 Gate F-A2 / VAL-076.
func TestWorkerAccepts_SeverityFloor(t *testing.T) {
	tenantID := uuid.New()
	high := severity.Ordinal(severity.High) // 4
	d := dest(tenantID, "x", "127.0.0.1", 514, FormatRFC5424, nil, nil)
	d.SeverityFloor = &high

	cases := []struct {
		sev  string
		want bool
	}{
		{severity.Info, false},
		{severity.Low, false},
		{severity.Medium, false},
		{severity.High, true},
		{severity.Critical, true},
	}
	for _, c := range cases {
		env := makeEnv(tenantID, "session.started", c.sev)
		got := workerAccepts(d, env, CategorySession)
		if got != c.want {
			t.Errorf("severity %q: got=%v want=%v (floor=high)", c.sev, got, c.want)
		}
	}
}

// TestWorkerAccepts_BothFloorsAreANDed — when SeverityFloor and
// FilterMinSeverity are both set, an envelope must satisfy BOTH (AND
// semantics). STORY-098 Gate F-A2 / VAL-076.
func TestWorkerAccepts_BothFloorsAreANDed(t *testing.T) {
	tenantID := uuid.New()
	medium := severity.Ordinal(severity.Medium) // 3
	high := severity.Ordinal(severity.High)     // 4
	d := dest(tenantID, "x", "127.0.0.1", 514, FormatRFC5424, nil, &medium)
	d.SeverityFloor = &high

	// Severity=high: passes filter_min_severity (medium) AND severity_floor (high) -> accept.
	envHigh := makeEnv(tenantID, "session.started", severity.High)
	if !workerAccepts(d, envHigh, CategorySession) {
		t.Error("severity=high must pass when both floors are at-or-below high")
	}
	// Severity=medium: passes filter_min_severity but NOT severity_floor -> reject.
	envMedium := makeEnv(tenantID, "session.started", severity.Medium)
	if workerAccepts(d, envMedium, CategorySession) {
		t.Error("severity=medium must be rejected (severity_floor=high)")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

// splitHostPort splits "host:port" returned by syslogtest listeners.
func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := splitHostPortStr(addr)
	if err != nil {
		t.Fatalf("splitHostPort %q: %v", addr, err)
	}
	port, err := atoi(portStr)
	if err != nil {
		t.Fatalf("atoi %q: %v", portStr, err)
	}
	return host, port
}

func splitHostPortStr(addr string) (host, port string, err error) {
	// Avoid net package to keep this helper free of platform-specific
	// behavior. The syslogtest listeners use net.JoinHostPort so the format
	// is stable.
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return "", "", errors.New("no colon")
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("non-digit")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
