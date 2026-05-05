// Package syslogtest provides mock syslog receivers for round-trip tests.
//
// Used by:
//   - STORY-098 Task 3 transport_test.go (UDP/TCP/TLS unit tests)
//   - STORY-098 Task 5 worker_test.go + forwarder_test.go (concurrent dispatch)
//   - STORY-098 Task 7 integration_test.go (end-to-end byte-trace assertions)
//
// All listeners bind to 127.0.0.1:0 so callers receive a free ephemeral port.
package syslogtest

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// UDPListener is a minimal mock UDP syslog receiver. Each datagram becomes a
// single message; UDP has no framing.
type UDPListener struct {
	conn *net.UDPConn
	addr string
	mu   sync.Mutex
	msgs [][]byte
	done chan struct{}
}

// NewUDPListener binds 127.0.0.1:0/UDP and spawns a reader goroutine. The
// returned listener captures every incoming datagram. Caller MUST defer Close.
// Address is in "host:port" form suitable for net.Dial.
func NewUDPListener(t *testing.T) (*UDPListener, string) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("syslogtest: resolve UDP addr: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("syslogtest: listen UDP: %v", err)
	}
	l := &UDPListener{
		conn: conn,
		addr: conn.LocalAddr().String(),
		done: make(chan struct{}),
	}
	go l.read()
	return l, l.addr
}

// Addr returns the bound "host:port" string.
func (l *UDPListener) Addr() string { return l.addr }

func (l *UDPListener) read() {
	defer close(l.done)
	buf := make([]byte, 64*1024) // max UDP datagram
	for {
		n, _, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		cp := make([]byte, n)
		copy(cp, buf[:n])
		l.mu.Lock()
		l.msgs = append(l.msgs, cp)
		l.mu.Unlock()
	}
}

// Messages returns a snapshot of all datagrams received so far. Safe for
// concurrent use.
func (l *UDPListener) Messages() [][]byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([][]byte, len(l.msgs))
	for i, m := range l.msgs {
		cp := make([]byte, len(m))
		copy(cp, m)
		out[i] = cp
	}
	return out
}

// Wait blocks until at least n messages have arrived or the deadline fires.
// Returns the snapshot regardless. Caller asserts len(snapshot) >= n.
func (l *UDPListener) Wait(n int, timeout time.Duration) [][]byte {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := l.Messages(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return l.Messages()
}

// Close stops the listener and releases the port. Idempotent.
func (l *UDPListener) Close() error {
	if l.conn == nil {
		return nil
	}
	err := l.conn.Close()
	<-l.done
	l.conn = nil
	return err
}

// TCPListener is a minimal mock TCP syslog receiver that decodes RFC 6587
// octet-counting framing: `<len-decimal> <SP> <body>`. Use for T3
// transport_test.go and T7 integration tests.
type TCPListener struct {
	listener net.Listener
	addr     string
	mu       sync.Mutex
	msgs     [][]byte
	conns    []net.Conn
	done     chan struct{}
	closing  bool
}

// NewTCPListener binds 127.0.0.1:0/TCP and accepts connections in the
// background. Each connection is decoded via RFC 6587 octet counting; every
// frame is appended to Messages.
func NewTCPListener(t *testing.T) (*TCPListener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("syslogtest: listen TCP: %v", err)
	}
	l := &TCPListener{
		listener: ln,
		addr:     ln.Addr().String(),
		done:     make(chan struct{}),
	}
	go l.acceptLoop()
	return l, l.addr
}

// Addr returns the bound "host:port" string.
func (l *TCPListener) Addr() string { return l.addr }

func (l *TCPListener) acceptLoop() {
	defer close(l.done)
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			l.mu.Lock()
			closing := l.closing
			l.mu.Unlock()
			if closing {
				return
			}
			return
		}
		l.mu.Lock()
		l.conns = append(l.conns, conn)
		l.mu.Unlock()
		go l.handleConn(conn)
	}
}

// handleConn reads RFC 6587 octet-counting frames until EOF or error.
// Frame format: `<decimal-len> <SP> <body of N bytes>`.
func (l *TCPListener) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := newOctetCountingReader(conn)
	for {
		frame, err := reader.NextFrame()
		if err != nil {
			return
		}
		l.mu.Lock()
		l.msgs = append(l.msgs, frame)
		l.mu.Unlock()
	}
}

// Messages returns a snapshot of all decoded frames.
func (l *TCPListener) Messages() [][]byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([][]byte, len(l.msgs))
	for i, m := range l.msgs {
		cp := make([]byte, len(m))
		copy(cp, m)
		out[i] = cp
	}
	return out
}

// Wait blocks until at least n frames arrive or the deadline fires.
func (l *TCPListener) Wait(n int, timeout time.Duration) [][]byte {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := l.Messages(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return l.Messages()
}

// KickConnections closes all currently accepted connections without stopping
// the listener. Reconnect tests use this to simulate a mid-stream server reset.
func (l *TCPListener) KickConnections() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range l.conns {
		c.Close()
	}
	l.conns = nil
}

// Close stops accepting new connections and shuts down the listener.
func (l *TCPListener) Close() error {
	l.mu.Lock()
	l.closing = true
	l.mu.Unlock()
	if l.listener == nil {
		return nil
	}
	err := l.listener.Close()
	<-l.done
	l.listener = nil
	return err
}

// TLSListener is a mock TLS syslog receiver. It wraps TCPListener over a
// tls.Listener so callers can test RFC 5425 transports including mTLS.
type TLSListener struct {
	*TCPListener
}

// NewTLSListener binds 127.0.0.1:0/TCP with TLS using serverCert.
// Pass clientCACertPEM (PEM-encoded CA) to enable mutual TLS: the server will
// RequireAndVerifyClientCert signed by that CA. Pass nil to disable mTLS.
func NewTLSListener(t *testing.T, serverCert tls.Certificate, clientCACertPEM []byte) (*TLSListener, string) {
	t.Helper()
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}
	if len(clientCACertPEM) > 0 {
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		tlsCfg.ClientCAs = buildCertPool(t, clientCACertPEM)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatalf("syslogtest: listen TLS: %v", err)
	}
	l := &TCPListener{
		listener: ln,
		addr:     ln.Addr().String(),
		done:     make(chan struct{}),
	}
	go l.acceptLoop()
	return &TLSListener{TCPListener: l}, l.addr
}

// buildCertPool parses a PEM-encoded certificate and returns an x509.CertPool.
func buildCertPool(t *testing.T, certPEM []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatalf("syslogtest: failed to parse cert PEM into pool")
	}
	return pool
}

// octetCountingReader decodes RFC 6587 octet-counted frames.
type octetCountingReader struct {
	r    io.Reader
	buf  []byte
	head int
	tail int
}

func newOctetCountingReader(r io.Reader) *octetCountingReader {
	return &octetCountingReader{r: r, buf: make([]byte, 64*1024)}
}

// NextFrame reads one frame: `<decimal-len> SP <body[len]>`. Returns body only.
func (o *octetCountingReader) NextFrame() ([]byte, error) {
	// Read until we see a SP, the delimiter between length and body.
	var lenBuf strings.Builder
	for {
		b, err := o.readByte()
		if err != nil {
			return nil, err
		}
		if b == ' ' {
			break
		}
		if b < '0' || b > '9' {
			return nil, fmt.Errorf("syslogtest: invalid octet-count digit %q", b)
		}
		lenBuf.WriteByte(b)
		if lenBuf.Len() > 9 { // arbitrary sanity ceiling on frame size
			return nil, errors.New("syslogtest: octet-count too long")
		}
	}
	if lenBuf.Len() == 0 {
		return nil, errors.New("syslogtest: empty octet-count")
	}
	n, err := strconv.Atoi(lenBuf.String())
	if err != nil || n < 0 {
		return nil, fmt.Errorf("syslogtest: parse octet-count: %v", err)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(o.r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (o *octetCountingReader) readByte() (byte, error) {
	one := make([]byte, 1)
	if _, err := io.ReadFull(o.r, one); err != nil {
		return 0, err
	}
	return one[0], nil
}
