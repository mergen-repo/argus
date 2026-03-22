package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/btopcu/argus/internal/auth"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 10 * time.Second
	pingPeriod     = 30 * time.Second
	authTimeout    = 5 * time.Second
	maxMessageSize = 4096

	CloseCodeUnauthorized  = 4001
	CloseCodeMaxConns      = 4002
	CloseCodeAuthTimeout   = 4003
	CloseCodeInternalError = 4004
)

type ServerConfig struct {
	Addr              string
	JWTSecret         string
	MaxConnsPerTenant int
}

type Server struct {
	hub      *Hub
	cfg      ServerConfig
	upgrader websocket.Upgrader
	srv      *http.Server
	logger   zerolog.Logger
	stopOnce sync.Once
}

func NewServer(hub *Hub, cfg ServerConfig, logger zerolog.Logger) *Server {
	if cfg.MaxConnsPerTenant <= 0 {
		cfg.MaxConnsPerTenant = 100
	}

	s := &Server{
		hub: hub,
		cfg: cfg,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
		logger: logger.With().Str("component", "ws_server").Logger(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws/v1/events", s.handleWS)

	s.srv = &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	s.logger.Info().Str("addr", s.cfg.Addr).Msg("ws server listening")
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("ws server error")
		}
	}()
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	var err error
	s.stopOnce.Do(func() {
		s.logger.Info().Msg("ws server shutting down")

		s.hub.mu.RLock()
		for _, conns := range s.hub.conns {
			for conn := range conns {
				closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
				if conn.ws != nil {
					_ = conn.ws.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(2*time.Second))
				}
			}
		}
		s.hub.mu.RUnlock()

		err = s.srv.Shutdown(ctx)
	})
	return err
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	var claims *auth.Claims

	tokenStr := r.URL.Query().Get("token")
	if tokenStr != "" {
		var err error
		claims, err = auth.ValidateToken(tokenStr, s.cfg.JWTSecret)
		if err != nil {
			s.logger.Warn().Err(err).Msg("ws auth failed (query param)")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	wsConn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("ws upgrade failed")
		return
	}

	if claims == nil {
		claims = s.waitForAuthMessage(wsConn)
		if claims == nil {
			return
		}
	} else {
		s.sendAuthOK(wsConn, claims)
	}

	tenantCount := s.hub.TenantConnectionCount(claims.TenantID)
	if tenantCount >= s.cfg.MaxConnsPerTenant {
		s.logger.Warn().
			Str("tenant_id", claims.TenantID.String()).
			Int("count", tenantCount).
			Int("max", s.cfg.MaxConnsPerTenant).
			Msg("ws max connections per tenant reached")
		closeMsg := websocket.FormatCloseMessage(CloseCodeMaxConns, "max connections per tenant reached")
		_ = wsConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait))
		wsConn.Close()
		return
	}

	conn := &Connection{
		TenantID: claims.TenantID,
		UserID:   claims.UserID,
		SendCh:   make(chan []byte, 256),
		ws:       wsConn,
		done:     make(chan struct{}),
	}

	s.hub.Register(conn)

	go s.writePump(conn)
	go s.readPump(conn)
}

func (s *Server) waitForAuthMessage(wsConn *websocket.Conn) *auth.Claims {
	wsConn.SetReadLimit(maxMessageSize)
	_ = wsConn.SetReadDeadline(time.Now().Add(authTimeout))

	_, msg, err := wsConn.ReadMessage()
	if err != nil {
		closeMsg := websocket.FormatCloseMessage(CloseCodeAuthTimeout, "auth timeout")
		_ = wsConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait))
		wsConn.Close()
		return nil
	}

	var authMsg struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(msg, &authMsg); err != nil || authMsg.Type != "auth" || authMsg.Token == "" {
		s.sendAuthError(wsConn, "INVALID_MESSAGE", "Expected auth message with type and token")
		wsConn.Close()
		return nil
	}

	claims, err := auth.ValidateToken(authMsg.Token, s.cfg.JWTSecret)
	if err != nil {
		code := "TOKEN_INVALID"
		message := "Access token is invalid"
		if err == auth.ErrTokenExpired {
			code = "TOKEN_EXPIRED"
			message = "Access token has expired"
		}
		s.sendAuthError(wsConn, code, message)
		wsConn.Close()
		return nil
	}

	s.sendAuthOK(wsConn, claims)
	return claims
}

func (s *Server) sendAuthOK(wsConn *websocket.Conn, claims *auth.Claims) {
	resp := map[string]interface{}{
		"type": "auth.ok",
		"data": map[string]interface{}{
			"tenant_id": claims.TenantID.String(),
			"user_id":   claims.UserID.String(),
			"role":      claims.Role,
		},
	}
	data, _ := json.Marshal(resp)
	_ = wsConn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = wsConn.WriteMessage(websocket.TextMessage, data)
}

func (s *Server) sendAuthError(wsConn *websocket.Conn, code, message string) {
	resp := map[string]interface{}{
		"type": "auth.error",
		"data": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(resp)
	_ = wsConn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = wsConn.WriteMessage(websocket.TextMessage, data)

	closeMsg := websocket.FormatCloseMessage(CloseCodeUnauthorized, message)
	_ = wsConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait))
}

func (s *Server) readPump(conn *Connection) {
	defer func() {
		s.hub.Unregister(conn)
		conn.ws.Close()
		close(conn.done)
	}()

	conn.ws.SetReadLimit(maxMessageSize)
	_ = conn.ws.SetReadDeadline(time.Now().Add(pingPeriod + pongWait))
	conn.ws.SetPongHandler(func(string) error {
		_ = conn.ws.SetReadDeadline(time.Now().Add(pingPeriod + pongWait))
		return nil
	})

	for {
		_, msg, err := conn.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Debug().Err(err).
					Str("tenant_id", conn.TenantID.String()).
					Str("user_id", conn.UserID.String()).
					Msg("ws read error")
			}
			return
		}

		s.handleClientMessage(conn, msg)
	}
}

func (s *Server) handleClientMessage(conn *Connection, msg []byte) {
	var clientMsg struct {
		Type   string   `json:"type"`
		Events []string `json:"events"`
	}
	if err := json.Unmarshal(msg, &clientMsg); err != nil {
		s.sendError(conn, "PARSE_ERROR", "Invalid JSON message")
		return
	}

	switch clientMsg.Type {
	case "subscribe":
		if len(clientMsg.Events) == 0 {
			s.sendError(conn, "INVALID_SUBSCRIBE", "Events list cannot be empty")
			return
		}
		conn.SetFilters(clientMsg.Events)
		resp := map[string]interface{}{
			"type": "subscribe.ok",
			"data": map[string]interface{}{
				"events": clientMsg.Events,
			},
		}
		data, _ := json.Marshal(resp)
		select {
		case conn.SendCh <- data:
		default:
		}
	default:
		s.sendError(conn, "UNKNOWN_MESSAGE", fmt.Sprintf("Unknown message type: %s", clientMsg.Type))
	}
}

func (s *Server) sendError(conn *Connection, code, message string) {
	resp := map[string]interface{}{
		"type": "error",
		"data": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(resp)
	select {
	case conn.SendCh <- data:
	default:
	}
}

func (s *Server) writePump(conn *Connection) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.ws.Close()
	}()

	for {
		select {
		case msg, ok := <-conn.SendCh:
			if !ok {
				_ = conn.ws.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
					time.Now().Add(writeWait),
				)
				return
			}

			_ = conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			_ = conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-conn.done:
			return
		}
	}
}
