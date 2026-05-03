package diameter

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNewTLSListenerNoTLS(t *testing.T) {
	ln, err := NewTLSListener(":0", TLSConfig{Enabled: false}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewTLSListener without TLS: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	if addr == "" {
		t.Error("listener should have an address")
	}
}

func TestNewTLSListenerDisabledByDefault(t *testing.T) {
	ln, err := NewTLSListener(":0", TLSConfig{}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewTLSListener default: %v", err)
	}
	defer ln.Close()
}

func TestNewTLSListenerInvalidCert(t *testing.T) {
	_, err := NewTLSListener(":0", TLSConfig{
		Enabled:  true,
		CertPath: "/nonexistent/cert.pem",
		KeyPath:  "/nonexistent/key.pem",
	}, zerolog.Nop())
	if err == nil {
		t.Error("expected error with nonexistent cert")
	}
}

func TestTLSConfigStruct(t *testing.T) {
	cfg := TLSConfig{
		CertPath: "/path/to/cert",
		KeyPath:  "/path/to/key",
		CAPath:   "/path/to/ca",
		Enabled:  true,
	}
	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
}

// generateSelfSignedCert creates a self-signed RSA cert/key pair in tmpDir.
// Returns (certPath, keyPath). The cert covers 127.0.0.1 and ::1.
func generateSelfSignedCert(t *testing.T, tmpDir string) (certPath, keyPath string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "diameter-test"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}

	certPath = filepath.Join(tmpDir, "cert.pem")
	keyPath = filepath.Join(tmpDir, "key.pem")

	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("pem encode cert: %v", err)
	}
	certFile.Close()

	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(priv)
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("pem encode key: %v", err)
	}
	keyFile.Close()

	return certPath, keyPath
}

// TestTLSHandshakeInterop verifies the TLS transport layer of NewTLSListener.
// Note: This test focuses on TLS transport correctness only, not Diameter
// CER/CEA protocol exchange (no CER/CEA helpers are available in this package).
func TestTLSHandshakeInterop(t *testing.T) {
	tmpDir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, tmpDir)

	t.Run("plain_tls_handshake", func(t *testing.T) {
		ln, err := NewTLSListener(":0", TLSConfig{
			Enabled:  true,
			CertPath: certPath,
			KeyPath:  keyPath,
		}, zerolog.Nop())
		if err != nil {
			t.Fatalf("NewTLSListener: %v", err)
		}
		defer ln.Close()

		addr := ln.Addr().String()

		// Accept connections in background; drain so the server side doesn't block.
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			if tc, ok := conn.(*tls.Conn); ok {
				_ = tc.Handshake()
			}
		}()

		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Fatalf("tls.Dial failed: %v", err)
		}
		if err := conn.Handshake(); err != nil {
			t.Fatalf("TLS handshake failed: %v", err)
		}
		conn.Close()
		<-done
	})

	t.Run("mtls_required_no_client_cert", func(t *testing.T) {
		// Use the same self-signed cert as CA — since it's self-signed it is its own CA.
		ln, err := NewTLSListener(":0", TLSConfig{
			Enabled:  true,
			CertPath: certPath,
			KeyPath:  keyPath,
			CAPath:   certPath,
		}, zerolog.Nop())
		if err != nil {
			t.Fatalf("NewTLSListener with mTLS: %v", err)
		}
		defer ln.Close()

		addr := ln.Addr().String()

		// Capture the server-side handshake error. In Go TLS, when the server
		// requires a client certificate (RequireAndVerifyClientCert) and the
		// client sends none, the server rejects with a "bad certificate" alert.
		// The client-side Handshake() may succeed or fail depending on Go version;
		// what is guaranteed is the server returns an error.
		serverErrCh := make(chan error, 1)
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				serverErrCh <- err
				return
			}
			defer conn.Close()
			tc, ok := conn.(*tls.Conn)
			if !ok {
				serverErrCh <- nil
				return
			}
			serverErrCh <- tc.Handshake()
		}()

		// Client connects without presenting a certificate.
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			InsecureSkipVerify: true,
		})
		if err == nil {
			conn.Handshake()
			conn.Close()
		}

		serverErr := <-serverErrCh
		if serverErr == nil {
			t.Error("expected server-side TLS handshake to fail when client provides no cert but mTLS is required")
		}
	})
}
