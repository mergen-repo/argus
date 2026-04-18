package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRADIUSPeer listens on a random UDP port, echoes back a
// synthetic Access-Accept (code=2, length=20) for any incoming packet.
// Behavior flags are LOCKED and set via functional options at
// construction time so the test goroutine and the fake's loop
// goroutine never race on them.
type fakeRADIUSPeer struct {
	conn       *net.UDPConn
	addr       *net.UDPAddr
	wg         sync.WaitGroup
	stop       chan struct{}
	replyCode  byte // when non-zero, reply with this RADIUS code instead of Access-Accept
	dropReply  bool // when true, consume without replying (simulate timeout)
	shortReply bool // when true, reply with <20 bytes
}

type fakeRADIUSOpt func(*fakeRADIUSPeer)

func withReplyCode(code byte) fakeRADIUSOpt { return func(p *fakeRADIUSPeer) { p.replyCode = code } }
func withDropReply() fakeRADIUSOpt          { return func(p *fakeRADIUSPeer) { p.dropReply = true } }
func withShortReply() fakeRADIUSOpt         { return func(p *fakeRADIUSPeer) { p.shortReply = true } }

func newFakeRADIUSPeer(t *testing.T, opts ...fakeRADIUSOpt) *fakeRADIUSPeer {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve udp: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	p := &fakeRADIUSPeer{
		conn: conn,
		addr: conn.LocalAddr().(*net.UDPAddr),
		stop: make(chan struct{}),
	}
	for _, o := range opts {
		o(p)
	}
	p.wg.Add(1)
	go p.loop()
	return p
}

func (p *fakeRADIUSPeer) loop() {
	defer p.wg.Done()
	buf := make([]byte, 4096)
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		_ = p.conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, raddr, err := p.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		if n < 20 {
			continue
		}
		if p.dropReply {
			continue
		}
		code := byte(radiusCodeAccessAccept)
		if p.replyCode != 0 {
			code = p.replyCode
		}
		length := 20
		reply := make([]byte, length)
		reply[0] = code
		reply[1] = buf[1] // echo identifier
		reply[2] = byte(length >> 8)
		reply[3] = byte(length)
		if p.shortReply {
			reply = reply[:10]
		}
		_, _ = p.conn.WriteToUDP(reply, raddr)
	}
}

func (p *fakeRADIUSPeer) Close() {
	close(p.stop)
	p.conn.Close()
	p.wg.Wait()
}

func (p *fakeRADIUSPeer) HostPort() (string, int) {
	return p.addr.IP.String(), p.addr.Port
}

func TestRADIUSAdapter_HealthCheck_LivePeerResponds(t *testing.T) {
	peer := newFakeRADIUSPeer(t)
	defer peer.Close()

	host, port := peer.HostPort()
	cfg, _ := json.Marshal(RADIUSConfig{Host: host, AuthPort: port, SharedSecret: "testing123", TimeoutMs: 2000})
	a, err := NewRADIUSAdapter(cfg)
	if err != nil {
		t.Fatalf("new radius adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if !result.Success {
		t.Fatalf("healthy peer should succeed, got error: %s", result.Error)
	}
}

func TestRADIUSAdapter_HealthCheck_TimeoutWhenPeerDrops(t *testing.T) {
	peer := newFakeRADIUSPeer(t, withDropReply())
	defer peer.Close()

	host, port := peer.HostPort()
	cfg, _ := json.Marshal(RADIUSConfig{Host: host, AuthPort: port, SharedSecret: "s", TimeoutMs: 200})
	a, err := NewRADIUSAdapter(cfg)
	if err != nil {
		t.Fatalf("new radius adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected timeout failure")
	}
	if !strings.Contains(result.Error, "timeout") {
		t.Errorf("expected 'timeout' in error, got %q", result.Error)
	}
}

func TestRADIUSAdapter_HealthCheck_DialFailsWhenPeerDown(t *testing.T) {
	// Reserve a port then close the listener so the dial reaches a
	// closed socket — UDP Dial doesn't fail synchronously on most
	// platforms, so assert the read path instead returns a failure.
	addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	conn, _ := net.ListenUDP("udp", addr)
	host := conn.LocalAddr().(*net.UDPAddr).IP.String()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	conn.Close()

	cfg, _ := json.Marshal(RADIUSConfig{Host: host, AuthPort: port, SharedSecret: "s", TimeoutMs: 200})
	a, err := NewRADIUSAdapter(cfg)
	if err != nil {
		t.Fatalf("new radius adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure when peer is down")
	}
}

func TestRADIUSAdapter_HealthCheck_UnexpectedCode(t *testing.T) {
	peer := newFakeRADIUSPeer(t, withReplyCode(radiusCodeAccessReject))
	defer peer.Close()

	host, port := peer.HostPort()
	cfg, _ := json.Marshal(RADIUSConfig{Host: host, AuthPort: port, SharedSecret: "s", TimeoutMs: 1000})
	a, err := NewRADIUSAdapter(cfg)
	if err != nil {
		t.Fatalf("new radius adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure when peer replies with unexpected code")
	}
	if !strings.Contains(result.Error, fmt.Sprintf("unexpected radius code %d", radiusCodeAccessReject)) {
		t.Errorf("error should mention unexpected code, got %q", result.Error)
	}
}

func TestRADIUSAdapter_HealthCheck_ShortResponse(t *testing.T) {
	peer := newFakeRADIUSPeer(t, withShortReply())
	defer peer.Close()

	host, port := peer.HostPort()
	cfg, _ := json.Marshal(RADIUSConfig{Host: host, AuthPort: port, SharedSecret: "s", TimeoutMs: 1000})
	a, err := NewRADIUSAdapter(cfg)
	if err != nil {
		t.Fatalf("new radius adapter: %v", err)
	}
	result := a.HealthCheck(context.Background())
	if result.Success {
		t.Fatal("expected failure on short response")
	}
	if !strings.Contains(result.Error, "short") {
		t.Errorf("error should mention short response, got %q", result.Error)
	}
}
