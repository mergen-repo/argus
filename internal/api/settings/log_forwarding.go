// Package settings implements HTTP handlers for tenant-level settings
// endpoints, including log forwarding destination management (STORY-098).
package settings

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/notification/syslog"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// LogForwardingHandler serves the log-forwarding destination endpoints
// (API-337 GET, API-338 POST, PATCH enable, DELETE, POST test).
type LogForwardingHandler struct {
	store    *store.SyslogDestinationStore
	auditSvc audit.Auditor
	logger   zerolog.Logger
}

// NewLogForwardingHandler constructs a LogForwardingHandler.
func NewLogForwardingHandler(
	s *store.SyslogDestinationStore,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *LogForwardingHandler {
	return &LogForwardingHandler{
		store:    s,
		auditSvc: auditSvc,
		logger:   logger.With().Str("component", "log_forwarding_handler").Logger(),
	}
}

// ── DTOs ──────────────────────────────────────────────────────────────────────

type syslogDestResponse struct {
	ID                string   `json:"id"`
	TenantID          string   `json:"tenant_id"`
	Name              string   `json:"name"`
	Host              string   `json:"host"`
	Port              int      `json:"port"`
	Transport         string   `json:"transport"`
	Format            string   `json:"format"`
	Facility          int      `json:"facility"`
	SeverityFloor     *int     `json:"severity_floor"`
	FilterCategories  []string `json:"filter_categories"`
	FilterMinSeverity *int     `json:"filter_min_severity"`
	Enabled           bool     `json:"enabled"`
	LastDeliveryAt    *string  `json:"last_delivery_at"`
	LastError         *string  `json:"last_error"`
	CreatedBy         *string  `json:"created_by"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

func toSyslogDestResponse(d store.SyslogDestination) syslogDestResponse {
	r := syslogDestResponse{
		ID:                d.ID.String(),
		TenantID:          d.TenantID.String(),
		Name:              d.Name,
		Host:              d.Host,
		Port:              d.Port,
		Transport:         d.Transport,
		Format:            d.Format,
		Facility:          d.Facility,
		SeverityFloor:     d.SeverityFloor,
		FilterCategories:  d.FilterCategories,
		FilterMinSeverity: d.FilterMinSeverity,
		Enabled:           d.Enabled,
		LastError:         d.LastError,
		CreatedAt:         d.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:         d.UpdatedAt.Format(time.RFC3339Nano),
	}
	if d.LastDeliveryAt != nil {
		s := d.LastDeliveryAt.Format(time.RFC3339Nano)
		r.LastDeliveryAt = &s
	}
	if d.CreatedBy != nil {
		s := d.CreatedBy.String()
		r.CreatedBy = &s
	}
	if r.FilterCategories == nil {
		r.FilterCategories = []string{}
	}
	return r
}

// upsertRequest is the POST body for Upsert. Optional fields use json.RawMessage
// (PAT-031) to distinguish absent from explicit null.
type upsertRequest struct {
	Name              string          `json:"name"`
	Host              string          `json:"host"`
	Port              int             `json:"port"`
	Transport         string          `json:"transport"`
	Format            string          `json:"format"`
	Facility          int             `json:"facility"`
	SeverityFloor     json.RawMessage `json:"severity_floor"`
	FilterCategories  []string        `json:"filter_categories"`
	FilterMinSeverity json.RawMessage `json:"filter_min_severity"`
	TLSCAPEM          json.RawMessage `json:"tls_ca_pem"`
	TLSClientCertPEM  json.RawMessage `json:"tls_client_cert_pem"`
	TLSClientKeyPEM   json.RawMessage `json:"tls_client_key_pem"`
	Enabled           bool            `json:"enabled"`
}

// decodeOptionalInt decodes a json.RawMessage as *int.
// Absent (len==0) → nil. "null" → nil. Number → pointer.
func decodeOptionalInt(raw json.RawMessage) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// decodeOptionalString decodes a json.RawMessage as *string.
func decodeOptionalString(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// validateUpsertRequest validates transport, format, facility, categories,
// and TLS fields. Returns an apierr code + message pair on failure.
//
// STORY-098 Gate F-A3: server-side guards for port range and name/host
// length so a malformed request returns 422 rather than tripping a 500 from
// PostgreSQL VARCHAR(255) overflow.
func validateUpsertRequest(req upsertRequest) (code, msg string, severityFloor *int, filterMinSeverity *int, caPEM *string, clientCertPEM *string, clientKeyPEM *string, err error) {
	if len(req.Name) == 0 || len(req.Name) > 255 {
		return apierr.CodeInvalidFormat, "name must be 1..255 characters", nil, nil, nil, nil, nil, fmt.Errorf("invalid name length")
	}
	if len(req.Host) == 0 || len(req.Host) > 255 {
		return apierr.CodeInvalidFormat, "host must be 1..255 characters", nil, nil, nil, nil, nil, fmt.Errorf("invalid host length")
	}
	if req.Port < 1 || req.Port > 65535 {
		return "INVALID_PORT", "port must be between 1 and 65535", nil, nil, nil, nil, nil, fmt.Errorf("invalid port")
	}
	if !syslog.ValidTransport(req.Transport) {
		return "INVALID_TRANSPORT", fmt.Sprintf("transport must be one of: %v", syslog.Transports), nil, nil, nil, nil, nil, fmt.Errorf("invalid transport")
	}
	if !syslog.ValidFormat(req.Format) {
		return "INVALID_FORMAT", fmt.Sprintf("format must be one of: %v", syslog.Formats), nil, nil, nil, nil, nil, fmt.Errorf("invalid format")
	}
	if req.Facility < 0 || req.Facility > 23 {
		return "INVALID_FACILITY", "facility must be between 0 and 23", nil, nil, nil, nil, nil, fmt.Errorf("invalid facility")
	}
	for _, cat := range req.FilterCategories {
		if !syslog.ValidCategory(cat) {
			return "INVALID_CATEGORY", fmt.Sprintf("unknown category %q; must be one of: %v", cat, syslog.Categories), nil, nil, nil, nil, nil, fmt.Errorf("invalid category")
		}
	}

	sf, decErr := decodeOptionalInt(req.SeverityFloor)
	if decErr != nil {
		return apierr.CodeInvalidFormat, "Invalid severity_floor value", nil, nil, nil, nil, nil, decErr
	}
	fms, decErr := decodeOptionalInt(req.FilterMinSeverity)
	if decErr != nil {
		return apierr.CodeInvalidFormat, "Invalid filter_min_severity value", nil, nil, nil, nil, nil, decErr
	}
	ca, decErr := decodeOptionalString(req.TLSCAPEM)
	if decErr != nil {
		return apierr.CodeInvalidFormat, "Invalid tls_ca_pem value", nil, nil, nil, nil, nil, decErr
	}
	cert, decErr := decodeOptionalString(req.TLSClientCertPEM)
	if decErr != nil {
		return apierr.CodeInvalidFormat, "Invalid tls_client_cert_pem value", nil, nil, nil, nil, nil, decErr
	}
	key, decErr := decodeOptionalString(req.TLSClientKeyPEM)
	if decErr != nil {
		return apierr.CodeInvalidFormat, "Invalid tls_client_key_pem value", nil, nil, nil, nil, nil, decErr
	}

	if req.Transport == syslog.TransportTLS && ca != nil && *ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(*ca)) {
			return "TLS_CONFIG_INVALID", "tls_ca_pem contains no valid PEM certificates", nil, nil, nil, nil, nil, fmt.Errorf("invalid ca pem")
		}
	}

	certSet := cert != nil && *cert != ""
	keySet := key != nil && *key != ""
	if certSet != keySet {
		return "TLS_CONFIG_INVALID", "tls_client_cert_pem and tls_client_key_pem must both be provided or both be absent", nil, nil, nil, nil, nil, fmt.Errorf("partial tls client creds")
	}
	if certSet && keySet {
		if _, tlsErr := tls.X509KeyPair([]byte(*cert), []byte(*key)); tlsErr != nil {
			return "TLS_CONFIG_INVALID", "tls_client_cert_pem / tls_client_key_pem failed to parse: " + tlsErr.Error(), nil, nil, nil, nil, nil, tlsErr
		}
	}

	return "", "", sf, fms, ca, cert, key, nil
}

// ── API-337: GET /api/v1/settings/log-forwarding ──────────────────────────────

// List handles API-337.
func (h *LogForwardingHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	dests, err := h.store.List(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list syslog destinations failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	items := make([]syslogDestResponse, len(dests))
	for i, d := range dests {
		items[i] = toSyslogDestResponse(d)
	}
	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{HasMore: false})
}

// ── API-338: POST /api/v1/settings/log-forwarding ────────────────────────────

// Upsert handles API-338.
func (h *LogForwardingHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	var req upsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	errCode, errMsg, severityFloor, filterMinSev, caPEM, certPEM, keyPEM, valErr := validateUpsertRequest(req)
	if valErr != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, errCode, errMsg)
		return
	}

	categories := req.FilterCategories
	if categories == nil {
		categories = []string{}
	}

	userID := userIDFromCtx(r)

	result, err := h.store.Upsert(r.Context(), tenantID, store.UpsertSyslogDestinationParams{
		Name:              req.Name,
		Host:              req.Host,
		Port:              req.Port,
		Transport:         req.Transport,
		Format:            req.Format,
		Facility:          req.Facility,
		SeverityFloor:     severityFloor,
		FilterCategories:  categories,
		FilterMinSeverity: filterMinSev,
		TLSCAPEM:          caPEM,
		TLSClientCertPEM:  certPEM,
		TLSClientKeyPEM:   keyPEM,
		Enabled:           req.Enabled,
		CreatedBy:         userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("upsert syslog destination failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	h.emitUpsertAudit(r, tenantID, &result.Destination, result.Inserted, userID)

	status := http.StatusOK
	if result.Inserted {
		status = http.StatusCreated
	}
	apierr.WriteSuccess(w, status, toSyslogDestResponse(result.Destination))
}

// ── POST /api/v1/settings/log-forwarding/{id}/enabled ────────────────────────

// SetEnabled handles PATCH-equivalent for enabling/disabling a destination.
// Body: {"enabled": true|false}
func (h *LogForwardingHandler) SetEnabled(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	idStr := chi.URLParam(r, "id")
	destID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid destination ID")
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	updated, err := h.store.SetEnabled(r.Context(), tenantID, destID, body.Enabled)
	if err != nil {
		if errors.Is(err, store.ErrSyslogDestinationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Syslog destination not found")
			return
		}
		h.logger.Error().Err(err).Str("id", idStr).Msg("set syslog destination enabled failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	if !body.Enabled {
		userID := userIDFromCtx(r)
		h.emitDisabledAudit(r, tenantID, updated, userID)
	}

	apierr.WriteSuccess(w, http.StatusOK, toSyslogDestResponse(*updated))
}

// ── DELETE /api/v1/settings/log-forwarding/{id} ───────────────────────────────

// Delete handles DELETE /api/v1/settings/log-forwarding/{id}.
func (h *LogForwardingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	idStr := chi.URLParam(r, "id")
	destID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid destination ID")
		return
	}

	if err := h.store.Delete(r.Context(), tenantID, destID); err != nil {
		if errors.Is(err, store.ErrSyslogDestinationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Syslog destination not found")
			return
		}
		h.logger.Error().Err(err).Str("id", idStr).Msg("delete syslog destination failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	userID := userIDFromCtx(r)
	h.emitDeleteAudit(r, tenantID, destID.String(), userID)

	w.WriteHeader(http.StatusNoContent)
}

// ── POST /api/v1/settings/log-forwarding/test ────────────────────────────────

// testRequest mirrors upsertRequest — same fields, no DB write.
type testRequest = upsertRequest

type testResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Test handles POST /api/v1/settings/log-forwarding/test.
// Validates inputs, dials the configured endpoint, sends a single test packet,
// and returns {ok:true} or {ok:false, error:"..."}.
// NO DB write, NO audit (VAL-098-08).
func (h *LogForwardingHandler) Test(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	errCode, errMsg, _, _, caPEM, certPEM, keyPEM, valErr := validateUpsertRequest(req)
	if valErr != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, errCode, errMsg)
		return
	}

	if reason := blockedTestHost(req.Host); reason != "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_HOST", reason)
		return
	}

	addr := fmt.Sprintf("%s:%d", req.Host, req.Port)
	testErr := dialAndSend(r.Context(), req.Transport, addr, caPEM, certPEM, keyPEM)
	if testErr != nil {
		apierr.WriteSuccess(w, http.StatusOK, testResponse{OK: false, Error: testErr.Error()})
		return
	}
	apierr.WriteSuccess(w, http.StatusOK, testResponse{OK: true})
}

// blockedTestHost rejects probes targeted at well-known cloud-metadata
// endpoints to narrow the SSRF surface a tenant_admin might exploit via the
// Test Connection endpoint. Returns "" if the host is allowed, otherwise a
// human-readable reason. STORY-098 Gate F-A5.
//
// Scope deliberately narrow: only link-local cloud-metadata IPs are blocked.
// RFC1918 ranges remain allowed because legitimate SIEM destinations (Splunk,
// Graylog, on-prem rsyslog) routinely live on 10.x / 192.168.x / 172.16.x.
// Network-level egress filtering is the correct place for site policy.
var blockedMetadataHosts = map[string]struct{}{
	"169.254.169.254": {}, // AWS / GCP / Azure IMDS
	"169.254.170.2":   {}, // AWS ECS task metadata
	"100.100.100.200": {}, // Alibaba Cloud
	"fd00:ec2::254":   {}, // AWS IPv6 IMDS
}

func blockedTestHost(host string) string {
	if host == "" {
		return ""
	}
	if _, ok := blockedMetadataHosts[host]; ok {
		return "host is a cloud metadata endpoint and may not be probed"
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			// Match the IPv4 metadata addresses defensively (already keyed
			// by string above; this catches alternate textual forms like
			// "0xa9.0xfe.0xa9.0xfe" that net.ParseIP normalises).
			s := v4.String()
			if _, ok := blockedMetadataHosts[s]; ok {
				return "host is a cloud metadata endpoint and may not be probed"
			}
		}
	}
	return ""
}

// dialAndSend dials addr via the given transport and sends a minimal test
// payload. Returns non-nil on connection or write failure.
func dialAndSend(ctx context.Context, transport, addr string, caPEM, certPEM, keyPEM *string) error {
	deadline := 5 * time.Second
	testMsg := []byte("<134>argus test-connection\n")

	switch transport {
	case syslog.TransportUDP:
		d := net.Dialer{Timeout: deadline}
		conn, err := d.DialContext(ctx, "udp", addr)
		if err != nil {
			return fmt.Errorf("dial udp %s: %w", addr, err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(deadline))
		if _, err := conn.Write(testMsg); err != nil {
			return fmt.Errorf("write udp %s: %w", addr, err)
		}
		return nil

	case syslog.TransportTCP:
		d := net.Dialer{Timeout: deadline}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("dial tcp %s: %w", addr, err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(deadline))
		if _, err := conn.Write(testMsg); err != nil {
			return fmt.Errorf("write tcp %s: %w", addr, err)
		}
		return nil

	case syslog.TransportTLS:
		tlsCfg := &tls.Config{
			InsecureSkipVerify: false,
		}
		if caPEM != nil && *caPEM != "" {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM([]byte(*caPEM))
			tlsCfg.RootCAs = pool
		}
		if certPEM != nil && keyPEM != nil && *certPEM != "" && *keyPEM != "" {
			cert, err := tls.X509KeyPair([]byte(*certPEM), []byte(*keyPEM))
			if err != nil {
				return fmt.Errorf("parse client cert: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: deadline},
			Config:    tlsCfg,
		}
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("dial tls %s: %w", addr, err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(deadline))
		if _, err := conn.Write(testMsg); err != nil {
			return fmt.Errorf("write tls %s: %w", addr, err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported transport %q", transport)
	}
}

// ── audit helpers ─────────────────────────────────────────────────────────────

func (h *LogForwardingHandler) emitUpsertAudit(r *http.Request, tenantID uuid.UUID, d *store.SyslogDestination, inserted bool, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}
	action := "log_forwarding.destination_updated"
	if inserted {
		action = "log_forwarding.destination_added"
	}

	type payload struct {
		Name      string `json:"name"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		Transport string `json:"transport"`
		Format    string `json:"format"`
		Enabled   bool   `json:"enabled"`
	}
	afterData, _ := json.Marshal(payload{
		Name:      d.Name,
		Host:      d.Host,
		Port:      d.Port,
		Transport: d.Transport,
		Format:    d.Format,
		Enabled:   d.Enabled,
	})

	ip := r.RemoteAddr
	ua := r.UserAgent()
	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    "syslog_destination",
		EntityID:      d.ID.String(),
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("syslog upsert audit failed")
	}
}

func (h *LogForwardingHandler) emitDisabledAudit(r *http.Request, tenantID uuid.UUID, d *store.SyslogDestination, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}

	type payload struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	afterData, _ := json.Marshal(payload{Name: d.Name, Enabled: false})

	ip := r.RemoteAddr
	ua := r.UserAgent()
	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        "log_forwarding.destination_disabled",
		EntityType:    "syslog_destination",
		EntityID:      d.ID.String(),
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Msg("syslog disable audit failed")
	}
}

func (h *LogForwardingHandler) emitDeleteAudit(r *http.Request, tenantID uuid.UUID, destID string, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}

	ip := r.RemoteAddr
	ua := r.UserAgent()
	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        "log_forwarding.destination_deleted",
		EntityType:    "syslog_destination",
		EntityID:      destID,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Msg("syslog delete audit failed")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func userIDFromCtx(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
