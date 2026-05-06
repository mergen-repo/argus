package syslog

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/notification/syslog/syslogtest"
)

// ---- helpers ----------------------------------------------------------------

// genSelfSignedCert generates an ECDSA self-signed certificate for the given
// SANs. Returns the tls.Certificate, the DER-encoded cert, and its PEM.
func genSelfSignedCert(t *testing.T, dnsNames []string, ipAddrs []net.IP) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genSelfSignedCert: generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "syslogtest"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              dnsNames,
		IPAddresses:           ipAddrs,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("genSelfSignedCert: create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("genSelfSignedCert: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("genSelfSignedCert: X509KeyPair: %v", err)
	}
	return tlsCert, certDER, certPEM
}

// genSignedClientCert generates a client cert signed by the given CA key/cert.
func genSignedClientCert(t *testing.T, caCert *x509.Certificate, caKey *ecdsa.PrivateKey) ([]byte, []byte) {
	t.Helper()
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genSignedClientCert: generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "syslogtest-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("genSignedClientCert: create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		t.Fatalf("genSignedClientCert: marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// hostPort splits "host:port" into host and port int.
func hostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("hostPort SplitHostPort %q: %v", addr, err)
	}
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	return host, port
}

// ---- UDP tests --------------------------------------------------------------

func TestUDPTransport_RoundTrip(t *testing.T) {
	l, addr := syslogtest.NewUDPListener(t)
	defer l.Close()

	host, port := hostPort(t, addr)
	tr, err := NewUDPTransport(TransportConfig{Host: host, Port: port})
	if err != nil {
		t.Fatalf("NewUDPTransport: %v", err)
	}
	defer tr.Close()

	msgs := [][]byte{
		[]byte("hello syslog 1"),
		[]byte("hello syslog 2"),
		[]byte("hello syslog 3"),
	}
	ctx := context.Background()
	for _, m := range msgs {
		if err := tr.Send(ctx, m); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	got := l.Wait(3, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("want 3 messages, got %d", len(got))
	}
	for i, m := range msgs {
		if !bytes.Equal(got[i], m) {
			t.Errorf("msg[%d]: got %q, want %q", i, got[i], m)
		}
	}
}

func TestUDPTransport_CloseIdempotent(t *testing.T) {
	l, addr := syslogtest.NewUDPListener(t)
	defer l.Close()

	host, port := hostPort(t, addr)
	tr, err := NewUDPTransport(TransportConfig{Host: host, Port: port})
	if err != nil {
		t.Fatalf("NewUDPTransport: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

// ---- TCP tests --------------------------------------------------------------

func TestTCPTransport_RoundTrip_OctetCounting(t *testing.T) {
	l, addr := syslogtest.NewTCPListener(t)
	defer l.Close()

	host, port := hostPort(t, addr)
	tr, err := NewTCPTransport(TransportConfig{Host: host, Port: port})
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}
	defer tr.Close()

	msgs := [][]byte{
		[]byte("<134>May  4 12:00:00 host argus[1]: msg one"),
		[]byte("<134>May  4 12:00:01 host argus[1]: msg two"),
		[]byte("<134>May  4 12:00:02 host argus[1]: msg three"),
	}
	ctx := context.Background()
	for _, m := range msgs {
		if err := tr.Send(ctx, m); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	got := l.Wait(3, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("want 3 frames, got %d", len(got))
	}
	for i, m := range msgs {
		if !bytes.Equal(got[i], m) {
			t.Errorf("frame[%d]: got %q, want %q", i, got[i], m)
		}
	}
}

func TestTCPTransport_FramingFormat(t *testing.T) {
	// Capture raw bytes to verify the wire format is "<len> <body>" exactly.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()

	rawCh := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain until read deadline: TCP loopback may split the framed
		// message into multiple Read returns under CI scheduling pressure;
		// single Read can deliver empty/partial buf and fail body assertion.
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var all []byte
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				all = append(all, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		rawCh <- all
	}()

	host, port := hostPort(t, ln.Addr().String())
	tr, err := NewTCPTransport(TransportConfig{Host: host, Port: port})
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}
	defer tr.Close()

	body := []byte("<134>May  4 12:00:00 host argus[1]: test framing")
	if err := tr.Send(context.Background(), body); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case raw := <-rawCh:
		// Verify the wire format is exactly "<decimal-len> <body>".
		spIdx := bytes.IndexByte(raw, ' ')
		if spIdx < 0 {
			t.Fatalf("no space delimiter in wire bytes: %q", raw)
		}
		lenField := string(raw[:spIdx])
		bodyField := raw[spIdx+1:]
		wantLen := len(body)
		gotLen := 0
		for _, c := range lenField {
			gotLen = gotLen*10 + int(c-'0')
		}
		if gotLen != wantLen {
			t.Errorf("length field %q decoded to %d, want %d", lenField, gotLen, wantLen)
		}
		if !bytes.Equal(bodyField, body) {
			t.Errorf("body mismatch:\ngot:  %q\nwant: %q", bodyField, body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for raw bytes")
	}
}

func TestTCPTransport_ReconnectAfterError(t *testing.T) {
	l, addr := syslogtest.NewTCPListener(t)
	defer l.Close()

	host, port := hostPort(t, addr)
	tr, err := NewTCPTransport(TransportConfig{Host: host, Port: port})
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	msg1 := []byte("before kick")
	if err := tr.Send(ctx, msg1); err != nil {
		t.Fatalf("Send before kick: %v", err)
	}
	got := l.Wait(1, 2*time.Second)
	if len(got) == 0 {
		t.Fatal("first message not received")
	}

	// Kick all server-side connections; next Send should fail and return error.
	l.KickConnections()
	time.Sleep(20 * time.Millisecond) // let FIN propagate

	// TCP half-close detection is racy: the first Write after a server-side
	// RST may silently succeed (kernel buffers) while the second Write fails.
	// The transport's contract is "on Write error, reset conn; next Send
	// re-dials". So we drive a short loop: ≥1 send must error (resetting
	// the conn) and a subsequent send must succeed and reach the listener.
	msg3 := []byte("after reconnect")
	deliveredMsg3 := false
	for attempt := 0; attempt < 5 && !deliveredMsg3; attempt++ {
		_ = tr.Send(ctx, []byte("during kick — may fail"))
		if err := tr.Send(ctx, msg3); err != nil {
			// Send still failing — loop tries again so the transport's
			// reset+re-dial path gets exercised.
			continue
		}
		allMsgs := l.Wait(2, 2*time.Second)
		for _, m := range allMsgs {
			if bytes.Equal(m, msg3) {
				deliveredMsg3 = true
				break
			}
		}
	}
	if !deliveredMsg3 {
		t.Errorf("msg3 %q not received after reconnect", msg3)
	}
}

func TestTCPTransport_CloseIdempotent(t *testing.T) {
	l, addr := syslogtest.NewTCPListener(t)
	defer l.Close()

	host, port := hostPort(t, addr)
	tr, err := NewTCPTransport(TransportConfig{Host: host, Port: port})
	if err != nil {
		t.Fatalf("NewTCPTransport: %v", err)
	}
	_ = tr.Send(context.Background(), []byte("ping"))
	if err := tr.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

// ---- TLS tests --------------------------------------------------------------

func TestTLSTransport_SelfSignedCA_Succeeds(t *testing.T) {
	serverCert, _, caPEM := genSelfSignedCert(t,
		nil,
		[]net.IP{net.ParseIP("127.0.0.1")},
	)
	l, addr := syslogtest.NewTLSListener(t, serverCert, nil)
	defer l.Close()

	host, port := hostPort(t, addr)
	tr, err := NewTLSTransport(TransportConfig{
		Host:     host,
		Port:     port,
		TLSCAPEM: caPEM,
	})
	if err != nil {
		t.Fatalf("NewTLSTransport: %v", err)
	}
	defer tr.Close()

	msg := []byte("<134>May  4 12:00:00 host argus[1]: tls round-trip")
	if err := tr.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := l.Wait(1, 2*time.Second)
	if len(got) == 0 {
		t.Fatal("no message received over TLS")
	}
	if !bytes.Equal(got[0], msg) {
		t.Errorf("got %q, want %q", got[0], msg)
	}
}

func TestTLSTransport_NoCA_FailsCertVerify(t *testing.T) {
	serverCert, _, _ := genSelfSignedCert(t,
		nil,
		[]net.IP{net.ParseIP("127.0.0.1")},
	)
	l, addr := syslogtest.NewTLSListener(t, serverCert, nil)
	defer l.Close()

	host, port := hostPort(t, addr)
	// No TLSCAPEM → system trust → self-signed cert should fail.
	tr, err := NewTLSTransport(TransportConfig{
		Host: host,
		Port: port,
	})
	if err != nil {
		t.Fatalf("NewTLSTransport: %v", err)
	}
	defer tr.Close()

	err = tr.Send(context.Background(), []byte("should fail"))
	if err == nil {
		t.Fatal("expected certificate verification error, got nil")
	}
}

func TestTLSTransport_HostnameMismatch_Fails(t *testing.T) {
	// Cert is valid for "wrong.example.com" only, but we connect to 127.0.0.1.
	serverCert, _, caPEM := genSelfSignedCert(t,
		[]string{"wrong.example.com"},
		nil,
	)
	l, addr := syslogtest.NewTLSListener(t, serverCert, nil)
	defer l.Close()

	_, port := hostPort(t, addr)
	// Connect to 127.0.0.1 — ServerName = "127.0.0.1", cert has no matching SAN.
	tr, err := NewTLSTransport(TransportConfig{
		Host:     "127.0.0.1",
		Port:     port,
		TLSCAPEM: caPEM,
	})
	if err != nil {
		t.Fatalf("NewTLSTransport: %v", err)
	}
	defer tr.Close()

	err = tr.Send(context.Background(), []byte("should fail"))
	if err == nil {
		t.Fatal("expected hostname mismatch error, got nil")
	}
}

func TestTLSTransport_MutualTLS_Succeeds(t *testing.T) {
	// Generate server cert (self-signed CA) valid for 127.0.0.1.
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(10),
		Subject:               pkix.Name{CommonName: "syslogtest-server-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, serverTemplate, &serverKey.PublicKey, serverKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		t.Fatalf("marshal server key: %v", err)
	}
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: serverKeyDER})
	serverTLSCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair server: %v", err)
	}

	// Generate a separate CA for client certs, then sign a client cert with it.
	clientCACertPEM, clientCertPEM, clientKeyPEM := genClientCA(t)

	// NewTLSListener with mTLS (requires client cert signed by clientCACertPEM).
	l, addr := syslogtest.NewTLSListener(t, serverTLSCert, clientCACertPEM)
	defer l.Close()

	_, port := hostPort(t, addr)

	tr, err := NewTLSTransport(TransportConfig{
		Host:             "127.0.0.1",
		Port:             port,
		TLSCAPEM:         serverCertPEM,
		TLSClientCertPEM: clientCertPEM,
		TLSClientKeyPEM:  clientKeyPEM,
	})
	if err != nil {
		t.Fatalf("NewTLSTransport (mTLS): %v", err)
	}
	defer tr.Close()

	msg := []byte("<134>May  4 12:00:00 host argus[1]: mutual TLS round-trip")
	if err := tr.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send (mTLS): %v", err)
	}
	got := l.Wait(1, 2*time.Second)
	if len(got) == 0 {
		t.Fatal("no message received over mTLS")
	}
	if !bytes.Equal(got[0], msg) {
		t.Errorf("mTLS got %q, want %q", got[0], msg)
	}
}

// genClientCA generates a self-signed CA and signs a client cert with it.
// Returns (caCertPEM, clientCertPEM, clientKeyPEM).
func genClientCA(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen client CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(20),
		Subject:               pkix.Name{CommonName: "syslogtest-client-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create client CA cert: %v", err)
	}
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caX509, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse client CA cert: %v", err)
	}
	clientCertPEM, clientKeyPEM := genSignedClientCert(t, caX509, caKey)
	return caCertPEM, clientCertPEM, clientKeyPEM
}
