package sba

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/bus"
	"github.com/rs/zerolog"
)

type ServerConfig struct {
	Port        int
	TLSCertPath string
	TLSKeyPath  string
	EnableMTLS  bool
}

type ServerDeps struct {
	SessionMgr *session.Manager
	EventBus   *bus.EventBus
	Logger     zerolog.Logger
}

type Server struct {
	cfg        ServerConfig
	httpServer *http.Server
	sessionMgr *session.Manager
	eventBus   *bus.EventBus
	logger     zerolog.Logger

	ausfHandler *AUSFHandler
	udmHandler  *UDMHandler

	mu      sync.Mutex
	running bool
}

func NewServer(cfg ServerConfig, deps ServerDeps) *Server {
	logger := deps.Logger.With().Str("component", "sba_server").Logger()

	s := &Server{
		cfg:        cfg,
		sessionMgr: deps.SessionMgr,
		eventBus:   deps.EventBus,
		logger:     logger,
	}

	s.ausfHandler = NewAUSFHandler(deps.SessionMgr, deps.EventBus, logger)
	s.udmHandler = NewUDMHandler(deps.SessionMgr, deps.EventBus, logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/nausf-auth/v1/ue-authentications", s.ausfHandler.HandleAuthentication)
	mux.HandleFunc("/nausf-auth/v1/ue-authentications/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/5g-aka-confirmation") {
			s.ausfHandler.HandleConfirmation(w, r)
			return
		}
		writeProblem(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Unknown AUSF endpoint")
	})

	mux.HandleFunc("/nudm-ueau/v1/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/security-information") {
			s.udmHandler.HandleSecurityInfo(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/auth-events") {
			s.udmHandler.HandleAuthEvents(w, r)
			return
		}
		writeProblem(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Unknown UDM UEAU endpoint")
	})

	mux.HandleFunc("/nudm-uecm/v1/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/registrations/") {
			s.udmHandler.HandleRegistration(w, r)
			return
		}
		writeProblem(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "Unknown UDM UECM endpoint")
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy","service":"argus-sba"}`))
	})

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("sba server already running")
	}

	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.httpServer.Addr, err)
	}

	if s.cfg.TLSCertPath != "" && s.cfg.TLSKeyPath != "" {
		tlsCfg := &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"},
		}
		if s.cfg.EnableMTLS {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		}
		s.httpServer.TLSConfig = tlsCfg

		go func() {
			s.logger.Info().
				Int("port", s.cfg.Port).
				Bool("mtls", s.cfg.EnableMTLS).
				Msg("starting SBA HTTP/2 server with TLS")
			if err := s.httpServer.ServeTLS(ln, s.cfg.TLSCertPath, s.cfg.TLSKeyPath); err != nil && err != http.ErrServerClosed {
				s.logger.Error().Err(err).Msg("SBA server TLS error")
			}
		}()
	} else {
		go func() {
			s.logger.Info().
				Int("port", s.cfg.Port).
				Msg("starting SBA HTTP/2 server (no TLS — development mode)")
			if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
				s.logger.Error().Err(err).Msg("SBA server error")
			}
		}()
	}

	s.running = true
	s.logger.Info().
		Int("port", s.cfg.Port).
		Msg("SBA server started")

	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Error().Err(err).Msg("SBA server shutdown error")
	}

	s.running = false
	s.logger.Info().Msg("SBA server stopped")
}

func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Server) HealthCheck() error {
	if !s.IsRunning() {
		return fmt.Errorf("sba server not running")
	}
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("sba port unreachable: %w", err)
	}
	conn.Close()
	return nil
}

func (s *Server) SessionManager() *session.Manager {
	return s.sessionMgr
}

func (s *Server) AUSFHandler() *AUSFHandler {
	return s.ausfHandler
}

func (s *Server) UDMHandler() *UDMHandler {
	return s.udmHandler
}

