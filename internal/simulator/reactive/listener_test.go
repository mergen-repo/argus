package reactive

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/rfc3576"

	"github.com/btopcu/argus/internal/simulator/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func init() {
	reg := prometheus.NewRegistry()
	metrics.MustRegister(reg)
}

func newTestListener(t *testing.T) (*Listener, *Registry, func()) {
	t.Helper()
	reg := NewRegistry()
	l := NewListener(ListenerConfig{
		Addr:     "127.0.0.1:0",
		Secret:   []byte("testsecret"),
		Registry: reg,
		Logger:   zerolog.Nop(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	if err := l.Start(ctx); err != nil {
		t.Fatalf("listener start: %v", err)
	}
	<-l.Ready()
	return l, reg, func() {
		cancel()
		_ = l.Stop(context.Background())
	}
}

func sendPacketAndReceive(t *testing.T, listenerAddr *net.UDPAddr, pkt *radius.Packet, timeout time.Duration) (*radius.Packet, bool) {
	t.Helper()
	raw, err := pkt.Encode()
	if err != nil {
		t.Fatalf("encode packet: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, listenerAddr)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(raw); err != nil {
		t.Fatalf("write packet: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, false
	}

	resp, err := radius.Parse(buf[:n], pkt.Secret)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return resp, true
}

func sendPacketNoReceive(t *testing.T, listenerAddr *net.UDPAddr, pkt *radius.Packet, timeout time.Duration) bool {
	t.Helper()
	raw, err := pkt.Encode()
	if err != nil {
		t.Fatalf("encode packet: %v", err)
	}

	conn, err := net.DialUDP("udp", nil, listenerAddr)
	if err != nil {
		t.Fatalf("dial listener: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write(raw); err != nil {
		t.Fatalf("write packet: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	_, err = conn.Read(buf)
	return err == nil
}

func newDMRequest(secret []byte, acctSessionID string) *radius.Packet {
	pkt := radius.New(radius.CodeDisconnectRequest, secret)
	rfc2866.AcctSessionID_SetString(pkt, acctSessionID)
	return pkt
}

func newCoARequest(secret []byte, acctSessionID string, sessionTimeout uint32) *radius.Packet {
	pkt := radius.New(radius.CodeCoARequest, secret)
	rfc2866.AcctSessionID_SetString(pkt, acctSessionID)
	if sessionTimeout > 0 {
		rfc2865.SessionTimeout_Set(pkt, rfc2865.SessionTimeout(sessionTimeout))
	}
	return pkt
}

func newSession(id, operatorCode string) *Session {
	s := &Session{
		ID:           id,
		OperatorCode: operatorCode,
		AcctSessionID: id,
	}
	return s
}

func countMetric(counter *prometheus.CounterVec, labels prometheus.Labels) float64 {
	c, err := counter.GetMetricWith(labels)
	if err != nil {
		return 0
	}
	return testutil.ToFloat64(c)
}

func TestListener_DMAck_CancelsSession(t *testing.T) {
	l, reg, cleanup := newTestListener(t)
	defer cleanup()

	var cancelled atomic.Bool
	sess := newSession("sess-001", "testop")
	ctx, cancel := context.WithCancel(context.Background())
	sess.CancelFn = func() {
		cancelled.Store(true)
		cancel()
	}
	reg.Register(sess)

	pkt := newDMRequest([]byte("testsecret"), "sess-001")
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), pkt, time.Second)
	if !ok {
		t.Fatal("expected DM-ACK response, got none")
	}
	if resp.Code != radius.CodeDisconnectACK {
		t.Errorf("expected CodeDisconnectACK (41), got %v", resp.Code)
	}
	if sess.CurrentDisconnectCause() != CauseDM {
		t.Errorf("expected CauseDM, got %v", sess.CurrentDisconnectCause())
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Error("CancelFn was not called within 1s")
	}
	if !cancelled.Load() {
		t.Error("expected CancelFn to be called")
	}

	count := countMetric(metrics.SimulatorReactiveIncomingTotal, prometheus.Labels{"operator": "testop", "kind": "dm", "result": "ack"})
	if count < 1 {
		t.Errorf("expected metric dm/ack >= 1, got %v", count)
	}
}

func TestListener_DMUnknownSession_Nak(t *testing.T) {
	l, _, cleanup := newTestListener(t)
	defer cleanup()

	pkt := newDMRequest([]byte("testsecret"), "nonexistent-session")
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), pkt, time.Second)
	if !ok {
		t.Fatal("expected DM-NAK response, got none")
	}
	if resp.Code != radius.CodeDisconnectNAK {
		t.Errorf("expected CodeDisconnectNAK (42), got %v", resp.Code)
	}
	ec := rfc3576.ErrorCause_Get(resp)
	if ec != rfc3576.ErrorCause_Value_SessionContextNotFound {
		t.Errorf("expected ErrorCause 503, got %v", ec)
	}

	count := countMetric(metrics.SimulatorReactiveIncomingTotal, prometheus.Labels{"operator": "unknown", "kind": "dm", "result": "unknown_session"})
	if count < 1 {
		t.Errorf("expected metric dm/unknown_session >= 1, got %v", count)
	}
}

func TestListener_CoAAck_UpdatesDeadline(t *testing.T) {
	l, reg, cleanup := newTestListener(t)
	defer cleanup()

	sess := newSession("sess-coa-001", "testop2")
	sess.UpdateDeadline(time.Now().Add(60 * time.Second))
	reg.Register(sess)

	pkt := newCoARequest([]byte("testsecret"), "sess-coa-001", 30)
	before := time.Now()
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), pkt, time.Second)
	if !ok {
		t.Fatal("expected CoA-ACK response, got none")
	}
	if resp.Code != radius.CodeCoAACK {
		t.Errorf("expected CodeCoAACK (44), got %v", resp.Code)
	}

	deadline := sess.CurrentDeadline()
	expected := before.Add(30 * time.Second)
	delta := deadline.Sub(expected)
	if delta < -2*time.Second || delta > 2*time.Second {
		t.Errorf("deadline %v not within ±2s of expected %v (delta %v)", deadline, expected, delta)
	}

	// Metric is bumped asynchronously after the ACK is written; poll
	// briefly to avoid flakes on slow CI runners under -race rather than
	// asserting immediately on the post-response goroutine.
	deadlineWait := time.Now().Add(2 * time.Second)
	var count float64
	for time.Now().Before(deadlineWait) {
		count = countMetric(metrics.SimulatorReactiveIncomingTotal, prometheus.Labels{"operator": "testop2", "kind": "coa", "result": "ack"})
		if count >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if count < 1 {
		t.Errorf("expected metric coa/ack >= 1 within 2s, got %v", count)
	}
}

func TestListener_CoAUnknownSession_Nak(t *testing.T) {
	l, _, cleanup := newTestListener(t)
	defer cleanup()

	pkt := newCoARequest([]byte("testsecret"), "nonexistent-coa-session", 30)
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), pkt, time.Second)
	if !ok {
		t.Fatal("expected CoA-NAK response, got none")
	}
	if resp.Code != radius.CodeCoANAK {
		t.Errorf("expected CodeCoANAK (45), got %v", resp.Code)
	}
	ec := rfc3576.ErrorCause_Get(resp)
	if ec != rfc3576.ErrorCause_Value_SessionContextNotFound {
		t.Errorf("expected ErrorCause 503, got %v", ec)
	}
}

func TestListener_BadSecret_SilentDrop(t *testing.T) {
	l, reg, cleanup := newTestListener(t)
	defer cleanup()

	sess := newSession("sess-badsecret", "testop3")
	reg.Register(sess)

	wrongSecretPkt := newDMRequest([]byte("wrongsecret"), "sess-badsecret")
	gotReply := sendPacketNoReceive(t, l.LocalAddr(), wrongSecretPkt, 500*time.Millisecond)
	if gotReply {
		t.Error("expected no response for bad-secret packet (silent drop)")
	}

	count := countMetric(metrics.SimulatorReactiveIncomingTotal, prometheus.Labels{"operator": "unknown", "kind": "dm", "result": "bad_secret"})
	if count < 1 {
		t.Errorf("expected bad_secret metric >= 1, got %v", count)
	}

	// Verify listener still processes valid packets after the bad-secret drop
	validPkt := newDMRequest([]byte("testsecret"), "sess-badsecret")
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), validPkt, time.Second)
	if !ok {
		t.Fatal("listener stopped working after bad-secret packet")
	}
	if resp.Code != radius.CodeDisconnectACK {
		t.Errorf("expected DM-ACK after recovery, got %v", resp.Code)
	}
}

func TestListener_Malformed_Dropped(t *testing.T) {
	l, reg, cleanup := newTestListener(t)
	defer cleanup()

	sess := newSession("sess-malformed-follow", "testop4")
	reg.Register(sess)

	// Send a 6-byte garbage datagram (too short to be a RADIUS packet)
	conn, err := net.DialUDP("udp", nil, l.LocalAddr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	garbage := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	if _, err := conn.Write(garbage); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 4096)
	conn.Read(buf)
	conn.Close()

	// Give listener a moment to process
	time.Sleep(50 * time.Millisecond)

	count := countMetric(metrics.SimulatorReactiveIncomingTotal, prometheus.Labels{"operator": "unknown", "kind": "unknown", "result": "malformed"})
	if count < 1 {
		t.Errorf("expected malformed metric >= 1, got %v", count)
	}

	// Verify listener still handles valid packets
	validPkt := newDMRequest([]byte("testsecret"), "sess-malformed-follow")
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), validPkt, time.Second)
	if !ok {
		t.Fatal("listener stopped working after malformed packet")
	}
	if resp.Code != radius.CodeDisconnectACK {
		t.Errorf("expected DM-ACK after malformed packet, got %v", resp.Code)
	}
}

func TestListener_Concurrent100Packets(t *testing.T) {
	l, reg, cleanup := newTestListener(t)
	defer cleanup()

	const n = 100
	cancelFlags := make([]atomic.Bool, n)
	sessions := make([]*Session, n)

	for i := 0; i < n; i++ {
		idx := i
		id := fmt.Sprintf("sess-concurrent-%03d", idx)
		ctx, cancel := context.WithCancel(context.Background())
		_ = ctx
		sess := &Session{
			ID:            id,
			OperatorCode:  "concop",
			AcctSessionID: id,
		}
		sess.CancelFn = func() {
			cancelFlags[idx].Store(true)
			cancel()
		}
		sessions[idx] = sess
		reg.Register(sess)
	}

	var wg sync.WaitGroup
	acks := make(chan struct{}, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("sess-concurrent-%03d", idx)
			pkt := newDMRequest([]byte("testsecret"), id)
			resp, ok := sendPacketAndReceive(t, l.LocalAddr(), pkt, 3*time.Second)
			if ok && resp.Code == radius.CodeDisconnectACK {
				acks <- struct{}{}
			}
		}(i)
	}

	wg.Wait()
	close(acks)

	ackCount := 0
	for range acks {
		ackCount++
	}
	if ackCount != n {
		t.Errorf("expected %d DM-ACKs, got %d", n, ackCount)
	}

	for i := 0; i < n; i++ {
		if !cancelFlags[i].Load() {
			t.Errorf("CancelFn not called for session %d", i)
		}
	}
}

func TestListener_StopCleansUp(t *testing.T) {
	reg := NewRegistry()
	l := NewListener(ListenerConfig{
		Addr:     "127.0.0.1:0",
		Secret:   []byte("testsecret"),
		Registry: reg,
		Logger:   zerolog.Nop(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := l.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-l.Ready()

	sess := newSession("sess-stop-test", "stopop")
	reg.Register(sess)

	pkt := newDMRequest([]byte("testsecret"), "sess-stop-test")
	resp, ok := sendPacketAndReceive(t, l.LocalAddr(), pkt, time.Second)
	if !ok {
		t.Fatal("expected DM-ACK before stop")
	}
	if resp.Code != radius.CodeDisconnectACK {
		t.Errorf("expected DM-ACK, got %v", resp.Code)
	}

	savedAddr := l.LocalAddr()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := l.Stop(stopCtx); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}

	// After stop, sending a packet should get no response
	conn, err := net.DialUDP("udp", nil, savedAddr)
	if err == nil {
		pkt2 := newDMRequest([]byte("testsecret"), "sess-stop-test")
		raw, _ := pkt2.Encode()
		conn.Write(raw)
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		buf := make([]byte, 4096)
		_, readErr := conn.Read(buf)
		conn.Close()
		if readErr == nil {
			t.Error("expected no response after Stop, but got one")
		}
	}
}
