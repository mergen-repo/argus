package syslog

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strconv"
	"sync"
)

// Transport is the wire-level shim for delivering formatted syslog messages.
// UDP is fire-and-forget (AC-7). TCP/TLS are persistent connections with lazy
// reconnect on error (AC-8, AC-9).
type Transport interface {
	// Send delivers a single message (already formatted by emitter.Format).
	// For UDP: one datagram, no retry.
	// For TCP/TLS: octet-counted frame per RFC 6587, error returned on failure.
	Send(ctx context.Context, msg []byte) error

	// Close releases resources. Idempotent.
	Close() error
}

// TransportConfig carries the connection parameters for any transport type.
type TransportConfig struct {
	Host string
	Port int
	// TLSCAPEM holds a PEM-encoded CA certificate. When non-empty it overrides
	// system trust (RFC 5425 custom CA). When empty, system trust pool is used.
	TLSCAPEM []byte
	// TLSClientCertPEM and TLSClientKeyPEM enable mutual TLS when both non-empty.
	TLSClientCertPEM []byte
	TLSClientKeyPEM  []byte
}

func (c TransportConfig) addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// udpTransport implements Transport over UDP (fire-and-forget, AC-7).
type udpTransport struct {
	conn *net.UDPConn
}

// NewUDPTransport dials a UDP socket to cfg.Host:cfg.Port and returns a
// Transport. The socket is created once and reused for the lifetime of the
// transport.
func NewUDPTransport(cfg TransportConfig) (Transport, error) {
	raddr, err := net.ResolveUDPAddr("udp", cfg.addr())
	if err != nil {
		return nil, fmt.Errorf("syslog/udp: resolve %s: %w", cfg.addr(), err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("syslog/udp: dial %s: %w", cfg.addr(), err)
	}
	return &udpTransport{conn: conn}, nil
}

// Send writes msg as a single UDP datagram. Errors are returned but not retried
// (UDP fire-and-forget per AC-7).
func (t *udpTransport) Send(_ context.Context, msg []byte) error {
	_, err := t.conn.Write(msg)
	if err != nil {
		return fmt.Errorf("syslog/udp: write: %w", err)
	}
	return nil
}

// Close releases the UDP socket. Idempotent.
func (t *udpTransport) Close() error {
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}

// tcpTransport implements Transport over TCP with RFC 6587 octet-counting
// framing (AC-8). Connections are lazy-dialed and re-dialed on error.
type tcpTransport struct {
	cfg  TransportConfig
	dial func() (net.Conn, error)
	mu   sync.Mutex
	conn net.Conn
}

// NewTCPTransport creates a TCP transport with RFC 6587 octet-counting framing.
// The first Send triggers the actual TCP dial (lazy connect).
func NewTCPTransport(cfg TransportConfig) (Transport, error) {
	t := &tcpTransport{cfg: cfg}
	t.dial = func() (net.Conn, error) {
		return net.Dial("tcp", cfg.addr())
	}
	return t, nil
}

// Send frames msg per RFC 6587 octet-counting (`<len> <SP> <body>`) and writes
// it over the persistent TCP connection. On error the connection is closed and
// the error is returned; the caller (worker) schedules reconnect via Backoff.
func (t *tcpTransport) Send(ctx context.Context, msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		conn, err := t.dial()
		if err != nil {
			return fmt.Errorf("syslog/tcp: dial %s: %w", t.cfg.addr(), err)
		}
		t.conn = conn
	}
	if err := writeOctetCounted(t.conn, msg); err != nil {
		t.conn.Close()
		t.conn = nil
		return fmt.Errorf("syslog/tcp: write: %w", err)
	}
	return nil
}

// Close closes the underlying TCP connection. Idempotent.
func (t *tcpTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}

// tlsTransport implements Transport over TLS 1.2+ (RFC 5425, AC-9).
// Wire framing is identical to tcpTransport (RFC 6587 octet-counting).
type tlsTransport struct {
	cfg    TransportConfig
	tlsCfg *tls.Config
	mu     sync.Mutex
	conn   net.Conn
}

// NewTLSTransport creates a TLS transport. TLS 1.2 is the minimum version per
// AC-9. TLSCAPEM overrides system trust; TLSClientCertPEM+Key enable mTLS.
func NewTLSTransport(cfg TransportConfig) (Transport, error) {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: cfg.Host,
	}
	if len(cfg.TLSCAPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.TLSCAPEM) {
			return nil, fmt.Errorf("syslog/tls: failed to parse CA PEM")
		}
		tlsCfg.RootCAs = pool
	}
	if len(cfg.TLSClientCertPEM) > 0 && len(cfg.TLSClientKeyPEM) > 0 {
		cert, err := tls.X509KeyPair(cfg.TLSClientCertPEM, cfg.TLSClientKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("syslog/tls: load client cert: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return &tlsTransport{cfg: cfg, tlsCfg: tlsCfg}, nil
}

// Send frames msg per RFC 6587 and writes it over the TLS connection.
// On error the connection is closed and the error returned (caller retries).
func (t *tlsTransport) Send(_ context.Context, msg []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		conn, err := tls.Dial("tcp", t.cfg.addr(), t.tlsCfg)
		if err != nil {
			return fmt.Errorf("syslog/tls: dial %s: %w", t.cfg.addr(), err)
		}
		t.conn = conn
	}
	if err := writeOctetCounted(t.conn, msg); err != nil {
		t.conn.Close()
		t.conn = nil
		return fmt.Errorf("syslog/tls: write: %w", err)
	}
	return nil
}

// Close closes the TLS connection. Idempotent.
func (t *tlsTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}

// writeOctetCounted writes a single RFC 6587 octet-counted frame:
//
//	<decimal-length> SP <body>
//
// No trailing newline is added; the receiver reads exactly len(msg) bytes after
// the space delimiter.
func writeOctetCounted(conn net.Conn, msg []byte) error {
	header := []byte(strconv.Itoa(len(msg)) + " ")
	if _, err := conn.Write(header); err != nil {
		return err
	}
	if _, err := conn.Write(msg); err != nil {
		return err
	}
	return nil
}
