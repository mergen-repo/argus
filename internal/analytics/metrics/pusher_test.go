package metrics

import (
	"sync"
	"testing"
	"time"
)

type mockHub struct {
	mu        sync.Mutex
	events    []string
	payloads  []interface{}
}

func (h *mockHub) BroadcastAll(eventType string, data interface{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, eventType)
	h.payloads = append(h.payloads, data)
}

func (h *mockHub) eventCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.events)
}

func (h *mockHub) lastEvent() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) == 0 {
		return ""
	}
	return h.events[len(h.events)-1]
}

func TestPusher_BroadcastsMetrics(t *testing.T) {
	rdb := newTestRedis(t)
	c := NewCollector(rdb, noopLogger())
	mock := &mockSessionCounter{count: 10}
	c.SetSessionCounter(mock)

	hub := &mockHub{}
	p := NewPusher(c, hub, noopLogger())
	p.Start()

	time.Sleep(2500 * time.Millisecond)
	p.Stop()

	count := hub.eventCount()
	if count < 2 {
		t.Errorf("expected at least 2 broadcasts, got %d", count)
	}

	evt := hub.lastEvent()
	if evt != "metrics.realtime" {
		t.Errorf("last event type = %q, want metrics.realtime", evt)
	}
}
