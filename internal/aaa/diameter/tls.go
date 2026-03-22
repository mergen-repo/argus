package diameter

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"github.com/rs/zerolog"
)

type TLSConfig struct {
	CertPath string
	KeyPath  string
	CAPath   string
	Enabled  bool
}

func NewTLSListener(addr string, cfg TLSConfig, logger zerolog.Logger) (net.Listener, error) {
	if !cfg.Enabled || cfg.CertPath == "" || cfg.KeyPath == "" {
		logger.Info().Str("addr", addr).Msg("Diameter starting without TLS")
		return net.Listen("tcp", addr)
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("diameter tls: load keypair: %w", err)
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

	if cfg.CAPath != "" {
		caCert, err := os.ReadFile(cfg.CAPath)
		if err != nil {
			return nil, fmt.Errorf("diameter tls: read CA: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("diameter tls: parse CA cert failed")
		}
		tlsCfg.ClientCAs = caPool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("diameter tls: listen %s: %w", addr, err)
	}

	logger.Info().
		Str("addr", addr).
		Bool("mutual_tls", cfg.CAPath != "").
		Msg("Diameter TLS listener started")

	return ln, nil
}

func (s *Server) StartWithTLS(tlsCfg TLSConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("diameter server already running")
	}

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	ln, err := NewTLSListener(addr, tlsCfg, s.logger)
	if err != nil {
		return err
	}

	s.listener = ln
	s.running = true

	s.wg.Add(1)
	go s.acceptLoop()

	s.logger.Info().
		Int("port", s.cfg.Port).
		Str("origin_host", s.cfg.OriginHost).
		Bool("tls", tlsCfg.Enabled).
		Msg("diameter server started")

	return nil
}
