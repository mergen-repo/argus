package bus

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/observability/metrics"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

// --- Fakes ---

type fakeConsumerInfoLister struct {
	infos []*jetstream.ConsumerInfo
	err   error
}

func (f *fakeConsumerInfoLister) Info() <-chan *jetstream.ConsumerInfo {
	ch := make(chan *jetstream.ConsumerInfo, len(f.infos))
	for _, info := range f.infos {
		ch <- info
	}
	close(ch)
	return ch
}

func (f *fakeConsumerInfoLister) Err() error { return f.err }

// fakeConsumerLister satisfies the narrow jsConsumerLister interface.
type fakeConsumerLister struct {
	lister *fakeConsumerInfoLister
}

func (f *fakeConsumerLister) ListConsumers(_ context.Context) jetstream.ConsumerInfoLister {
	return f.lister
}

// fakeStreamLookup satisfies the narrow jsStreamLookup interface.
type fakeStreamLookup struct {
	stream jsConsumerLister
}

func (f *fakeStreamLookup) Stream(_ context.Context, _ string) (jsConsumerLister, error) {
	return f.stream, nil
}

// fakePublisher records all Publish calls for assertion.
type fakePublisher struct {
	mu       sync.Mutex
	messages []publishedMsg
}

type publishedMsg struct {
	subject string
	payload interface{}
}

func (f *fakePublisher) Publish(_ context.Context, subject string, payload interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, publishedMsg{subject: subject, payload: payload})
	return nil
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

// --- Helpers ---

func buildPoller(js jsStreamLookup, pub *fakePublisher, threshold int) (*LagPoller, *metrics.Registry) {
	reg := metrics.NewRegistry()
	poller := NewLagPoller(
		js,
		reg,
		[]string{StreamEvents},
		100*time.Millisecond,
		threshold,
		pub,
		zerolog.Nop(),
	)
	return poller, reg
}

func makeLookup(infos ...*jetstream.ConsumerInfo) jsStreamLookup {
	return &fakeStreamLookup{
		stream: &fakeConsumerLister{
			lister: &fakeConsumerInfoLister{infos: infos},
		},
	}
}

func consumerInfo(name string, pending uint64) *jetstream.ConsumerInfo {
	return &jetstream.ConsumerInfo{
		Name:       name,
		NumPending: pending,
	}
}

// --- Tests ---

// TestLagPoller_GaugeUpdated verifies that a single poll with NumPending=5000
// sets the argus_nats_consumer_lag gauge to 5000.
func TestLagPoller_GaugeUpdated(t *testing.T) {
	js := makeLookup(consumerInfo("worker", 5000))
	pub := &fakePublisher{}
	poller, reg := buildPoller(js, pub, 10000)

	poller.poll(context.Background())

	gathered, err := reg.Reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	var lagValue float64
	var found bool
	for _, mf := range gathered {
		if mf.GetName() == "argus_nats_consumer_lag" {
			for _, m := range mf.GetMetric() {
				lagValue = m.GetGauge().GetValue()
				found = true
			}
		}
	}
	if !found {
		t.Fatal("argus_nats_consumer_lag metric not found")
	}
	if lagValue != 5000 {
		t.Errorf("expected lag=5000, got %v", lagValue)
	}
}

// TestLagPoller_AlertAfterFiveConsecutivePolls verifies that exactly one alert
// is published after 5 consecutive polls above the threshold, and that the
// alert payload has the correct fields.
func TestLagPoller_AlertAfterFiveConsecutivePolls(t *testing.T) {
	js := makeLookup(consumerInfo("slow-consumer", 20000))
	pub := &fakePublisher{}
	poller, _ := buildPoller(js, pub, 10000)

	for i := 0; i < 4; i++ {
		poller.poll(context.Background())
		if pub.count() != 0 {
			t.Fatalf("expected no alert before 5 polls, got one at poll %d", i+1)
		}
	}

	poller.poll(context.Background())
	if pub.count() != 1 {
		t.Errorf("expected exactly 1 alert after 5 polls, got %d", pub.count())
	}

	msg := pub.messages[0]
	if msg.subject != SubjectAlertTriggered {
		t.Errorf("expected subject %s, got %s", SubjectAlertTriggered, msg.subject)
	}

	raw, err := json.Marshal(msg.payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.EventVersion != CurrentEventVersion {
		t.Errorf("event_version = %d, want %d", env.EventVersion, CurrentEventVersion)
	}
	if env.Severity != "high" {
		t.Errorf("expected severity=high (post-FIX-212), got %s", env.Severity)
	}
	if env.Source != "infra" {
		t.Errorf("expected source=infra, got %s", env.Source)
	}
	if env.Type != "nats_consumer_lag" {
		t.Errorf("expected type=nats_consumer_lag, got %s", env.Type)
	}
	if env.Entity == nil || env.Entity.ID != "slow-consumer" {
		t.Errorf("expected entity.id=slow-consumer, got %+v", env.Entity)
	}
	if env.TenantID != SystemTenantID.String() {
		t.Errorf("expected tenant_id=SystemTenantID, got %s", env.TenantID)
	}
}

// TestLagPoller_CounterResetsAfterAlert verifies that after an alert is
// emitted the counter resets to 0: the next 4 polls produce no alert, and the
// 5th produces a second alert.
func TestLagPoller_CounterResetsAfterAlert(t *testing.T) {
	js := makeLookup(consumerInfo("slow-consumer", 20000))
	pub := &fakePublisher{}
	poller, _ := buildPoller(js, pub, 10000)

	for i := 0; i < 5; i++ {
		poller.poll(context.Background())
	}
	if pub.count() != 1 {
		t.Fatalf("expected 1 alert after first 5 polls, got %d", pub.count())
	}

	for i := 0; i < 4; i++ {
		poller.poll(context.Background())
		if pub.count() != 1 {
			t.Fatalf("expected no new alert at poll %d after reset, got %d total", i+1, pub.count())
		}
	}

	poller.poll(context.Background())
	if pub.count() != 2 {
		t.Errorf("expected 2 total alerts after second 5-poll cycle, got %d", pub.count())
	}
}

// TestLagPoller_StopIsIdempotentAndFast verifies that Stop() is idempotent
// (safe to call twice) and returns within 2 seconds.
func TestLagPoller_StopIsIdempotentAndFast(t *testing.T) {
	js := makeLookup()
	pub := &fakePublisher{}
	poller, _ := buildPoller(js, pub, 10000)
	poller.Start(context.Background())

	done := make(chan struct{})
	go func() {
		poller.Stop()
		poller.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2 seconds")
	}
}
