package metrics

import (
	"sync"
	"testing"
	"time"
)

type mockHub struct {
	mu       sync.Mutex
	events   []string
	payloads []interface{}
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
	defer p.Stop()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for 2 broadcasts, got %d", hub.eventCount())
		default:
			if hub.eventCount() >= 2 {
				evt := hub.lastEvent()
				if evt != "metrics.realtime" {
					t.Errorf("last event type = %q, want metrics.realtime", evt)
				}
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}
