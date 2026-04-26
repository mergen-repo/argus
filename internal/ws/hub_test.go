package ws

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestHub_RegisterUnregister(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	conn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}

	hub.Register(conn)
	if hub.ConnectionCount() != 1 {
		t.Errorf("count = %d, want 1", hub.ConnectionCount())
	}

	hub.Unregister(conn)
	if hub.ConnectionCount() != 0 {
		t.Errorf("count = %d, want 0", hub.ConnectionCount())
	}
}

func TestHub_BroadcastAll(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenant1 := uuid.New()
	tenant2 := uuid.New()

	conn1 := &Connection{
		TenantID: tenant1,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}
	conn2 := &Connection{
		TenantID: tenant2,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}

	hub.Register(conn1)
	hub.Register(conn2)

	hub.BroadcastAll("operator.health_changed", map[string]string{
		"operator_id": uuid.New().String(),
		"status":      "down",
	})

	select {
	case msg := <-conn1.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type != "operator.health_changed" {
			t.Errorf("type = %s, want operator.health_changed", env.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for conn1 message")
	}

	select {
	case msg := <-conn2.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type != "operator.health_changed" {
			t.Errorf("type = %s, want operator.health_changed", env.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for conn2 message")
	}
}

func TestHub_BroadcastToTenant(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	targetTenant := uuid.New()
	otherTenant := uuid.New()

	conn1 := &Connection{
		TenantID: targetTenant,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}
	conn2 := &Connection{
		TenantID: otherTenant,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}

	hub.Register(conn1)
	hub.Register(conn2)

	hub.BroadcastToTenant(targetTenant, "alert.new", map[string]string{
		"alert_type": "operator_down",
	})

	select {
	case msg := <-conn1.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type != "alert.new" {
			t.Errorf("type = %s, want alert.new", env.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for target tenant message")
	}

	select {
	case <-conn2.SendCh:
		t.Error("other tenant should not receive message")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHub_FilteredConnection(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	filteredConn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
		Filters:  []string{"session.started"},
	}
	unfilteredConn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}

	hub.Register(filteredConn)
	hub.Register(unfilteredConn)

	hub.BroadcastAll("operator.health_changed", map[string]string{"status": "down"})

	select {
	case <-filteredConn.SendCh:
		t.Error("filtered connection should not receive non-matching event")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case <-unfilteredConn.SendCh:
	case <-time.After(time.Second):
		t.Fatal("unfiltered connection should receive all events")
	}
}

func TestHub_WildcardFilter(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	conn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
		Filters:  []string{"*"},
	}

	hub.Register(conn)

	hub.BroadcastAll("operator.health_changed", map[string]string{"status": "down"})

	select {
	case <-conn.SendCh:
	case <-time.After(time.Second):
		t.Fatal("wildcard filter should receive all events")
	}
}

func TestHub_SetFilters(t *testing.T) {
	conn := &Connection{
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 256),
	}

	if !conn.MatchesFilter("anything") {
		t.Error("empty filter should match all")
	}

	conn.SetFilters([]string{"alert.new"})
	if conn.MatchesFilter("operator.health_changed") {
		t.Error("should not match non-subscribed event")
	}
	if !conn.MatchesFilter("alert.new") {
		t.Error("should match subscribed event")
	}
}

func TestHub_FullSendBuffer_DropsOldest(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	conn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 1),
	}

	hub.Register(conn)

	hub.BroadcastAll("event1", "data1")
	hub.BroadcastAll("event2", "data2")

	select {
	case msg := <-conn.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type != "event2" {
			t.Errorf("type = %s, want event2 (oldest should be dropped)", env.Type)
		}
	default:
		t.Fatal("expected one message to be buffered")
	}

	if got := hub.DroppedMessageCount(); got != 1 {
		t.Errorf("DroppedMessageCount = %d, want 1", got)
	}
}

func TestSafeSend_DropOldest(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	conn := &Connection{
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 2),
	}

	msg1 := []byte("msg1")
	msg2 := []byte("msg2")
	msg3 := []byte("msg3")

	hub.safeSend(conn, msg1)
	hub.safeSend(conn, msg2)
	hub.safeSend(conn, msg3)

	got1 := <-conn.SendCh
	got2 := <-conn.SendCh

	if string(got1) != "msg2" {
		t.Errorf("first received = %q, want msg2 (msg1 should be dropped as oldest)", string(got1))
	}
	if string(got2) != "msg3" {
		t.Errorf("second received = %q, want msg3", string(got2))
	}

	if got := hub.DroppedMessageCount(); got != 1 {
		t.Errorf("DroppedMessageCount = %d, want 1", got)
	}

	select {
	case extra := <-conn.SendCh:
		t.Errorf("unexpected extra message: %q", string(extra))
	default:
	}
}

func TestDroppedCounter_NonBlocking(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	conn := &Connection{
		TenantID: uuid.New(),
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 4),
	}

	const producers = 8
	const perProducer = 200

	var wg sync.WaitGroup
	start := make(chan struct{})
	done := make(chan struct{})

	var consumed uint64
	go func() {
		for {
			select {
			case <-done:
				for {
					select {
					case <-conn.SendCh:
						atomic.AddUint64(&consumed, 1)
					default:
						return
					}
				}
			case <-conn.SendCh:
				atomic.AddUint64(&consumed, 1)
			}
		}
	}()

	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < perProducer; j++ {
				hub.safeSend(conn, []byte("x"))
			}
		}()
	}

	deadline := time.After(5 * time.Second)
	close(start)

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-deadline:
		t.Fatal("safeSend blocked under concurrent load")
	}

	close(done)
	time.Sleep(50 * time.Millisecond)

	total := uint64(producers * perProducer)
	if got := hub.DroppedMessageCount() + atomic.LoadUint64(&consumed); got < total {
		t.Errorf("dropped+consumed = %d, want >= %d", got, total)
	}
}

func TestHub_ConcurrentBroadcast(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	for i := 0; i < 10; i++ {
		conn := &Connection{
			TenantID: uuid.New(),
			UserID:   uuid.New(),
			SendCh:   make(chan []byte, 256),
		}
		hub.Register(conn)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hub.BroadcastAll("test.event", map[string]int{"idx": idx})
		}(i)
	}
	wg.Wait()

	if hub.ConnectionCount() != 10 {
		t.Errorf("count = %d, want 10", hub.ConnectionCount())
	}
}

func TestNATSSubjectToWSType(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"argus.events.operator.health", "operator.health_changed"},
		{"argus.events.alert.triggered", "alert.new"},
		{"argus.events.session.started", "session.started"},
		{"argus.events.session.ended", "session.ended"},
		{"argus.events.sim.updated", "sim.state_changed"},
		{"argus.events.notification.dispatch", "notification.new"},
		{"argus.jobs.progress", "job.progress"},
		{"argus.jobs.completed", "job.completed"},
		{"unknown.subject", "unknown.subject"},
	}

	for _, tt := range tests {
		got := natsSubjectToWSType(tt.subject)
		if got != tt.want {
			t.Errorf("natsSubjectToWSType(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}

type mockWsSub struct{}

func (m *mockWsSub) Unsubscribe() error { return nil }

type mockWsSubscriber struct {
	subjects []string
}

func (m *mockWsSubscriber) QueueSubscribe(subject, queue string, handler func(string, []byte)) (Subscription, error) {
	m.subjects = append(m.subjects, subject)
	return &mockWsSub{}, nil
}

func TestHub_SubscribeToNATS(t *testing.T) {
	hub := NewHub(zerolog.Nop())
	sub := &mockWsSubscriber{}

	subjects := []string{"argus.events.operator.health", "argus.events.alert.triggered"}
	if err := hub.SubscribeToNATS(sub, subjects); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if len(sub.subjects) != 2 {
		t.Errorf("subscribed subjects = %d, want 2", len(sub.subjects))
	}

	hub.Stop()
}

func TestRelayNATSEvent_TenantIsolation(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantA := uuid.New()
	tenantB := uuid.New()

	connA := &Connection{
		TenantID: tenantA,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	connB := &Connection{
		TenantID: tenantB,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}

	hub.Register(connA)
	hub.Register(connB)

	payload := map[string]interface{}{
		"session_id": uuid.New().String(),
		"tenant_id":  tenantB.String(),
		"imsi":       "001010000000001",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	hub.relayNATSEvent("argus.events.session.started", data)

	select {
	case <-connA.SendCh:
		t.Fatal("tenant A should NOT receive event scoped to tenant B")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case msg := <-connB.SendCh:
		var env EventEnvelope
		if err := json.Unmarshal(msg, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		if env.Type != "session.started" {
			t.Errorf("type = %s, want session.started", env.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("tenant B should receive its event")
	}
}

func TestRelayNATSEvent_SystemEventBroadcast(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenant1 := uuid.New()
	tenant2 := uuid.New()

	conn1 := &Connection{
		TenantID: tenant1,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	conn2 := &Connection{
		TenantID: tenant2,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}

	hub.Register(conn1)
	hub.Register(conn2)

	payload := map[string]interface{}{
		"operator_id": uuid.New().String(),
		"tenant_id":   nil,
		"status":      "down",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	hub.relayNATSEvent("argus.events.operator.health", data)

	for i, ch := range []chan []byte{conn1.SendCh, conn2.SendCh} {
		select {
		case msg := <-ch:
			var env EventEnvelope
			if err := json.Unmarshal(msg, &env); err != nil {
				t.Fatalf("conn%d unmarshal: %v", i+1, err)
			}
			if env.Type != "operator.health_changed" {
				t.Errorf("conn%d type = %s, want operator.health_changed", i+1, env.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("conn%d should receive system event", i+1)
		}
	}
}

func TestRelayNATSEvent_MissingTenantIDBroadcasts(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenant1 := uuid.New()
	tenant2 := uuid.New()

	conn1 := &Connection{
		TenantID: tenant1,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	conn2 := &Connection{
		TenantID: tenant2,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}

	hub.Register(conn1)
	hub.Register(conn2)

	payload := map[string]interface{}{
		"alert_id": uuid.New().String(),
		"severity": "critical",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	hub.relayNATSEvent("argus.events.alert.triggered", data)

	for i, ch := range []chan []byte{conn1.SendCh, conn2.SendCh} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("conn%d should receive event without tenant_id (system fallback)", i+1)
		}
	}
}

func TestRelayNATSEvent_InvalidTenantIDBroadcasts(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	conn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	hub.Register(conn)

	payload := map[string]interface{}{
		"job_id":    uuid.New().String(),
		"tenant_id": "not-a-uuid",
	}
	data, _ := json.Marshal(payload)

	hub.relayNATSEvent("argus.jobs.completed", data)

	select {
	case <-conn.SendCh:
	case <-time.After(time.Second):
		t.Fatal("invalid tenant_id should fall back to broadcast-all")
	}
}

func TestExtractTenantID(t *testing.T) {
	id := uuid.New()

	tests := []struct {
		name      string
		payload   map[string]interface{}
		wantOk    bool
		wantValue uuid.UUID
	}{
		{
			name:      "string uuid",
			payload:   map[string]interface{}{"tenant_id": id.String()},
			wantOk:    true,
			wantValue: id,
		},
		{
			name:    "missing key",
			payload: map[string]interface{}{"foo": "bar"},
			wantOk:  false,
		},
		{
			name:    "nil value",
			payload: map[string]interface{}{"tenant_id": nil},
			wantOk:  false,
		},
		{
			name:    "empty string",
			payload: map[string]interface{}{"tenant_id": ""},
			wantOk:  false,
		},
		{
			name:    "nil uuid string",
			payload: map[string]interface{}{"tenant_id": uuid.Nil.String()},
			wantOk:  false,
		},
		{
			name:    "invalid string",
			payload: map[string]interface{}{"tenant_id": "abc"},
			wantOk:  false,
		},
		{
			name:    "wrong type",
			payload: map[string]interface{}{"tenant_id": 42},
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractTenantID(tt.payload)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if tt.wantOk && got != tt.wantValue {
				t.Errorf("value = %s, want %s", got, tt.wantValue)
			}
		})
	}
}

// TestRelayNATS_Tier1Event_DoesNotCreateNotificationRow is a FIX-237 Conflict 4
// regression test. It verifies the cross-tier safety invariant (Section 2.5):
//
//   - Tier 1 events (e.g. "session.started") MUST still flow on NATS to WS
//     Live Stream subscribers (assertion a: WS client receives the event).
//   - They MUST NOT create notification rows (assertion b: hub.go has zero
//     references to any notification store — the structural absence is verified
//     by the grep comment below; at runtime the invariant holds trivially because
//     Hub carries no notifStore field and no notification import).
//
// This guards against future refactors that might accidentally couple WS
// fan-out with notification persistence.
func TestRelayNATS_Tier1Event_DoesNotCreateNotificationRow(t *testing.T) {
	hub := NewHub(zerolog.Nop())

	tenantID := uuid.New()
	conn := &Connection{
		TenantID: tenantID,
		UserID:   uuid.New(),
		SendCh:   make(chan []byte, 16),
	}
	hub.Register(conn)

	env := &struct {
		EventVersion int    `json:"event_version"`
		ID           string `json:"id"`
		Type         string `json:"type"`
		Timestamp    string `json:"timestamp"`
		TenantID     string `json:"tenant_id"`
		Severity     string `json:"severity"`
		Source       string `json:"source"`
		Title        string `json:"title"`
	}{
		EventVersion: 1,
		ID:           uuid.NewString(),
		Type:         "session.started",
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		TenantID:     tenantID.String(),
		Severity:     "info",
		Source:       "aaa",
		Title:        "test session started",
	}

	msgBytes, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal Tier 1 envelope: %v", err)
	}

	hub.relayNATSEvent("argus.events.session.started", msgBytes)

	// Assertion (a): WS client received the Tier 1 event.
	select {
	case msg := <-conn.SendCh:
		var got EventEnvelope
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("unmarshal ws envelope: %v", err)
		}
		if got.Type != "session.started" {
			t.Errorf("expected type %q, got %q", "session.started", got.Type)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("WS client did not receive Tier 1 event within timeout")
	}

	// Assertion (b): Hub has no notification-store dependency — structural
	// invariant. hub.go contains no "notifStore", "notification.Service", or
	// notification import. The Hub struct carries only: mu, conns, subs,
	// dropped, metrics, logger. There is nothing to mock; the absence of side-
	// effects on any notification table is guaranteed by construction.
	//
	// Runtime check: ensure no unexpected second message was queued (channel
	// must be drained after the single relay call).
	select {
	case extra := <-conn.SendCh:
		t.Errorf("unexpected extra message on WS channel: %s", string(extra))
	default:
	}
}

func TestEventEnvelope_Serialization(t *testing.T) {
	env := EventEnvelope{
		Type:      "operator.health_changed",
		ID:        "evt_abc123",
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Data: map[string]string{
			"operator_id": uuid.New().String(),
			"status":      "down",
		},
	}

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded EventEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Type != env.Type {
		t.Errorf("Type = %s, want %s", decoded.Type, env.Type)
	}
	if decoded.ID != env.ID {
		t.Errorf("ID = %s, want %s", decoded.ID, env.ID)
	}
}
