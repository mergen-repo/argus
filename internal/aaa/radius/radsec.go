package radius

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type RadSecConfig struct {
	Addr     string
	CertPath string
	KeyPath  string
	CAPath   string
}

type RadSecServer struct {
	cfg      RadSecConfig
	handler  *Server
	listener net.Listener
	logger   zerolog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func NewRadSecServer(cfg RadSecConfig, handler *Server, logger zerolog.Logger) *RadSecServer {
	if cfg.Addr == "" {
		cfg.Addr = ":2083"
	}
	return &RadSecServer{
		cfg:     cfg,
		handler: handler,
		logger:  logger.With().Str("component", "radsec_server").Logger(),
		stopCh:  make(chan struct{}),
	}
}

func (rs *RadSecServer) Start() error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if rs.running {
		return nil
	}

	if rs.cfg.CertPath == "" || rs.cfg.KeyPath == "" {
		rs.logger.Info().Msg("RadSec disabled: no TLS certificate configured")
		return nil
	}

	cert, err := tls.LoadX509KeyPair(rs.cfg.CertPath, rs.cfg.KeyPath)
	if err != nil {
		return fmt.Errorf("radsec: load keypair: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
	}

	if rs.cfg.CAPath != "" {
		caCert, err := os.ReadFile(rs.cfg.CAPath)
		if err != nil {
			return fmt.Errorf("radsec: read CA cert: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return fmt.Errorf("radsec: failed to parse CA certificate")
		}
		tlsCfg.ClientCAs = caPool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	ln, err := tls.Listen("tcp", rs.cfg.Addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("radsec: listen %s: %w", rs.cfg.Addr, err)
	}

	rs.listener = ln
	rs.running = true

	rs.wg.Add(1)
	go rs.acceptLoop()

	rs.logger.Info().
		Str("addr", rs.cfg.Addr).
		Bool("mutual_tls", rs.cfg.CAPath != "").
		Msg("RadSec server started (RADIUS over TLS, RFC 6614)")

	return nil
}

func (rs *RadSecServer) Stop() {
	rs.mu.Lock()
	if !rs.running {
		rs.mu.Unlock()
		return
	}
	rs.running = false
	close(rs.stopCh)
	rs.mu.Unlock()

	if rs.listener != nil {
		rs.listener.Close()
	}
	rs.wg.Wait()
	rs.logger.Info().Msg("RadSec server stopped")
}

func (rs *RadSecServer) IsRunning() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.running
}

func (rs *RadSecServer) acceptLoop() {
	defer rs.wg.Done()
	for {
		conn, err := rs.listener.Accept()
		if err != nil {
			select {
			case <-rs.stopCh:
				return
			default:
				rs.logger.Error().Err(err).Msg("RadSec accept error")
				continue
			}
		}

		rs.wg.Add(1)
		go rs.handleConnection(conn)
	}
}

func (rs *RadSecServer) handleConnection(conn net.Conn) {
	defer rs.wg.Done()
	defer conn.Close()

	remote := conn.RemoteAddr().String()
	rs.logger.Debug().Str("remote", remote).Msg("RadSec connection accepted")

	if tlsConn, ok := conn.(*tls.Conn); ok {
		tlsConn.SetDeadline(time.Now().Add(10 * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			rs.logger.Warn().Err(err).Str("remote", remote).Msg("RadSec TLS handshake failed")
			return
		}
		tlsConn.SetDeadline(time.Time{})

		state := tlsConn.ConnectionState()
		rs.logger.Info().
			Str("remote", remote).
			Str("tls_version", tlsVersionString(state.Version)).
			Str("cipher_suite", tls.CipherSuiteName(state.CipherSuite)).
			Msg("RadSec TLS handshake complete")
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-rs.stopCh:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				rs.logger.Debug().Str("remote", remote).Msg("RadSec connection idle timeout")
			}
			return
		}

		if n < 20 {
			rs.logger.Warn().Int("bytes", n).Str("remote", remote).Msg("RadSec packet too short")
			continue
		}

		rs.logger.Debug().
			Int("bytes", n).
			Str("remote", remote).
			Msg("RadSec packet received")
	}
}

func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}
