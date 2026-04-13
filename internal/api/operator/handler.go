package operator

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/crypto"
	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var validAdapterTypes = map[string]bool{
	"mock":     true,
	"radius":   true,
	"diameter": true,
	"sba":      true,
}

var validFailoverPolicies = map[string]bool{
	"reject":             true,
	"fallback_to_next":   true,
	"queue_with_timeout": true,
}

var validOperatorStates = map[string]bool{
	"active":   true,
	"disabled": true,
}

type Handler struct {
	operatorStore   *store.OperatorStore
	tenantStore     *store.TenantStore
	simStore        *store.SIMStore
	sessionStore    *store.RadiusSessionStore
	cdrStore        *store.CDRStore
	auditSvc        audit.Auditor
	encryptionKey   string
	adapterRegistry *adapter.Registry
	logger          zerolog.Logger
}

type HandlerOption func(*Handler)

func WithSIMStore(s *store.SIMStore) HandlerOption {
	return func(h *Handler) { h.simStore = s }
}

func WithSessionStore(s *store.RadiusSessionStore) HandlerOption {
	return func(h *Handler) { h.sessionStore = s }
}

func WithCDRStore(cs *store.CDRStore) HandlerOption {
	return func(h *Handler) { h.cdrStore = cs }
}

func NewHandler(
	operatorStore *store.OperatorStore,
	tenantStore *store.TenantStore,
	auditSvc audit.Auditor,
	encryptionKey string,
	adapterRegistry *adapter.Registry,
	logger zerolog.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		operatorStore:   operatorStore,
		tenantStore:     tenantStore,
		auditSvc:        auditSvc,
		encryptionKey:   encryptionKey,
		adapterRegistry: adapterRegistry,
		logger:          logger.With().Str("component", "operator_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type operatorResponse struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Code                      string   `json:"code"`
	MCC                       string   `json:"mcc"`
	MNC                       string   `json:"mnc"`
	AdapterType               string   `json:"adapter_type"`
	SupportedRATTypes         []string `json:"supported_rat_types"`
	HealthStatus              string   `json:"health_status"`
	HealthCheckIntervalSec    int      `json:"health_check_interval_sec"`
	FailoverPolicy            string   `json:"failover_policy"`
	FailoverTimeoutMs         int      `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   int      `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec int      `json:"circuit_breaker_recovery_sec"`
	SLAUptimeTarget           *float64 `json:"sla_uptime_target"`
	State                     string   `json:"state"`
	CreatedAt                 string   `json:"created_at"`
	UpdatedAt                 string   `json:"updated_at"`
	SimCount                  int      `json:"sim_count"`
	ActiveSessions            int64    `json:"active_sessions"`
	TotalTrafficBytes         int64    `json:"total_traffic_bytes"`
	LastHealthCheck           *string  `json:"last_health_check"`
}

type grantResponse struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	OperatorID string  `json:"operator_id"`
	Enabled    bool    `json:"enabled"`
	GrantedAt  string  `json:"granted_at"`
	GrantedBy  *string `json:"granted_by,omitempty"`
}

type healthResponse struct {
	HealthStatus string  `json:"health_status"`
	LatencyMs    *int    `json:"latency_ms"`
	CircuitState string  `json:"circuit_state"`
	LastCheck    *string `json:"last_check"`
	Uptime24h    float64 `json:"uptime_24h"`
	FailureCount int     `json:"failure_count"`
}

type testResponse struct {
	Success   bool   `json:"success"`
	LatencyMs int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type createOperatorRequest struct {
	Name                      string          `json:"name"`
	Code                      string          `json:"code"`
	MCC                       string          `json:"mcc"`
	MNC                       string          `json:"mnc"`
	AdapterType               string          `json:"adapter_type"`
	AdapterConfig             json.RawMessage `json:"adapter_config"`
	SupportedRATTypes         []string        `json:"supported_rat_types"`
	FailoverPolicy            *string         `json:"failover_policy"`
	FailoverTimeoutMs         *int            `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   *int            `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec *int            `json:"circuit_breaker_recovery_sec"`
	HealthCheckIntervalSec    *int            `json:"health_check_interval_sec"`
	SLAUptimeTarget           *float64        `json:"sla_uptime_target"`
	SMDPPlusURL               *string         `json:"sm_dp_plus_url"`
	SMDPPlusConfig            json.RawMessage `json:"sm_dp_plus_config"`
}

type updateOperatorRequest struct {
	Name                      *string         `json:"name"`
	AdapterConfig             json.RawMessage `json:"adapter_config"`
	SupportedRATTypes         []string        `json:"supported_rat_types"`
	FailoverPolicy            *string         `json:"failover_policy"`
	FailoverTimeoutMs         *int            `json:"failover_timeout_ms"`
	CircuitBreakerThreshold   *int            `json:"circuit_breaker_threshold"`
	CircuitBreakerRecoverySec *int            `json:"circuit_breaker_recovery_sec"`
	HealthCheckIntervalSec    *int            `json:"health_check_interval_sec"`
	SLAUptimeTarget           *float64        `json:"sla_uptime_target"`
	SMDPPlusURL               *string         `json:"sm_dp_plus_url"`
	SMDPPlusConfig            json.RawMessage `json:"sm_dp_plus_config"`
	State                     *string         `json:"state"`
}

type createGrantRequest struct {
	TenantID          string   `json:"tenant_id"`
	OperatorID        string   `json:"operator_id"`
	SupportedRATTypes []string `json:"supported_rat_types"`
}

func toOperatorResponse(o *store.Operator) operatorResponse {
	rats := o.SupportedRATTypes
	if rats == nil {
		rats = []string{}
	}
	return operatorResponse{
		ID:                        o.ID.String(),
		Name:                      o.Name,
		Code:                      o.Code,
		MCC:                       o.MCC,
		MNC:                       o.MNC,
		AdapterType:               o.AdapterType,
		SupportedRATTypes:         rats,
		HealthStatus:              o.HealthStatus,
		HealthCheckIntervalSec:    o.HealthCheckIntervalSec,
		FailoverPolicy:            o.FailoverPolicy,
		FailoverTimeoutMs:         o.FailoverTimeoutMs,
		CircuitBreakerThreshold:   o.CircuitBreakerThreshold,
		CircuitBreakerRecoverySec: o.CircuitBreakerRecoverySec,
		SLAUptimeTarget:           o.SLAUptimeTarget,
		State:                     o.State,
		CreatedAt:                 o.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:                 o.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func toGrantResponse(g *store.OperatorGrant) grantResponse {
	resp := grantResponse{
		ID:         g.ID.String(),
		TenantID:   g.TenantID.String(),
		OperatorID: g.OperatorID.String(),
		Enabled:    g.Enabled,
		GrantedAt:  g.GrantedAt.Format(time.RFC3339Nano),
	}
	if g.GrantedBy != nil {
		s := g.GrantedBy.String()
		resp.GrantedBy = &s
	}
	return resp
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	stateFilter := r.URL.Query().Get("state")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	operators, nextCursor, err := h.operatorStore.List(r.Context(), cursor, limit, stateFilter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list operators")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	ctx := r.Context()
	tenantID, _ := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
	role, _ := ctx.Value(apierr.RoleKey).(string)

	if role != "super_admin" && tenantID != uuid.Nil {
		grants, gErr := h.operatorStore.ListGrants(ctx, tenantID)
		if gErr != nil {
			h.logger.Warn().Err(gErr).Msg("list grants for operator filter")
		} else {
			allowed := make(map[uuid.UUID]bool, len(grants))
			for _, g := range grants {
				allowed[g.OperatorID] = true
			}
			filtered := operators[:0]
			for _, o := range operators {
				if allowed[o.ID] {
					filtered = append(filtered, o)
				}
			}
			operators = filtered
			nextCursor = ""
		}
	}

	var simCounts map[uuid.UUID]int
	if h.simStore != nil && tenantID != uuid.Nil {
		simCounts, err = h.simStore.CountByOperator(ctx, tenantID)
		if err != nil {
			h.logger.Warn().Err(err).Msg("count sims by operator")
		}
	}

	var sessionCounts map[string]int64
	var trafficMap map[uuid.UUID]int64
	if h.sessionStore != nil {
		var tid *uuid.UUID
		if tenantID != uuid.Nil {
			tid = &tenantID
		}
		if stats, err2 := h.sessionStore.GetActiveStats(ctx, tid); err2 == nil {
			sessionCounts = stats.ByOperator
		} else {
			h.logger.Warn().Err(err2).Msg("get session stats for operator list")
		}
		trafficMap, err = h.sessionStore.TrafficByOperator(ctx, tid)
		if err != nil {
			h.logger.Warn().Err(err).Msg("get traffic by operator")
		}
	}

	healthTimes, err := h.operatorStore.LatestHealthByOperator(ctx)
	if err != nil {
		h.logger.Warn().Err(err).Msg("get latest health times by operator")
	}

	items := make([]operatorResponse, 0, len(operators))
	for _, o := range operators {
		resp := toOperatorResponse(&o)
		if simCounts != nil {
			resp.SimCount = simCounts[o.ID]
		}
		if sessionCounts != nil {
			resp.ActiveSessions = sessionCounts[o.ID.String()]
		}
		if trafficMap != nil {
			resp.TotalTrafficBytes = trafficMap[o.ID]
		}
		if healthTimes != nil {
			if t, ok := healthTimes[o.ID]; ok {
				ts := t.Format(time.RFC3339Nano)
				resp.LastHealthCheck = &ts
			}
		}
		items = append(items, resp)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createOperatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	}
	if req.Code == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "code", "message": "Code is required", "code": "required"})
	}
	if req.MCC == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "mcc", "message": "MCC is required", "code": "required"})
	} else if len(req.MCC) != 3 {
		validationErrors = append(validationErrors, map[string]string{"field": "mcc", "message": "MCC must be 3 digits", "code": "format"})
	}
	if req.MNC == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "mnc", "message": "MNC is required", "code": "required"})
	} else if len(req.MNC) < 2 || len(req.MNC) > 3 {
		validationErrors = append(validationErrors, map[string]string{"field": "mnc", "message": "MNC must be 2-3 digits", "code": "format"})
	}
	if req.AdapterType == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "adapter_type", "message": "Adapter type is required", "code": "required"})
	} else if !validAdapterTypes[req.AdapterType] {
		validationErrors = append(validationErrors, map[string]string{"field": "adapter_type", "message": "Invalid adapter type. Allowed: mock, radius, diameter, sba", "code": "invalid_enum"})
	}
	if req.FailoverPolicy != nil && !validFailoverPolicies[*req.FailoverPolicy] {
		validationErrors = append(validationErrors, map[string]string{"field": "failover_policy", "message": "Invalid failover policy. Allowed: reject, fallback_to_next, queue_with_timeout", "code": "invalid_enum"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	adapterConfig := req.AdapterConfig
	if adapterConfig != nil && len(adapterConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(adapterConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt adapter config")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		adapterConfig = encrypted
	}

	smDPConfig := req.SMDPPlusConfig
	if smDPConfig != nil && len(smDPConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(smDPConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt sm-dp+ config")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		smDPConfig = encrypted
	}

	o, err := h.operatorStore.Create(r.Context(), store.CreateOperatorParams{
		Name:                      req.Name,
		Code:                      req.Code,
		MCC:                       req.MCC,
		MNC:                       req.MNC,
		AdapterType:               req.AdapterType,
		AdapterConfig:             adapterConfig,
		SMDPPlusURL:               req.SMDPPlusURL,
		SMDPPlusConfig:            smDPConfig,
		SupportedRATTypes:         req.SupportedRATTypes,
		FailoverPolicy:            req.FailoverPolicy,
		FailoverTimeoutMs:         req.FailoverTimeoutMs,
		CircuitBreakerThreshold:   req.CircuitBreakerThreshold,
		CircuitBreakerRecoverySec: req.CircuitBreakerRecoverySec,
		HealthCheckIntervalSec:    req.HealthCheckIntervalSec,
		SLAUptimeTarget:           req.SLAUptimeTarget,
	})
	if err != nil {
		if errors.Is(err, store.ErrOperatorCodeExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"An operator with this code or name already exists",
				[]map[string]string{{"field": "code", "value": req.Code}})
			return
		}
		if errors.Is(err, store.ErrMCCMNCExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"An operator with this MCC+MNC combination already exists",
				[]map[string]string{{"field": "mcc", "value": req.MCC}, {"field": "mnc", "value": req.MNC}})
			return
		}
		h.logger.Error().Err(err).Msg("create operator")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "operator.create", o.ID.String(), nil, o)

	apierr.WriteSuccess(w, http.StatusCreated, toOperatorResponse(o))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	var req updateOperatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.FailoverPolicy != nil && !validFailoverPolicies[*req.FailoverPolicy] {
		validationErrors = append(validationErrors, map[string]string{"field": "failover_policy", "message": "Invalid failover policy", "code": "invalid_enum"})
	}
	if req.State != nil && !validOperatorStates[*req.State] {
		validationErrors = append(validationErrors, map[string]string{"field": "state", "message": "Invalid state. Allowed: active, disabled", "code": "invalid_enum"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	existing, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	adapterConfig := req.AdapterConfig
	if adapterConfig != nil && len(adapterConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(adapterConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt adapter config on update")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		adapterConfig = encrypted
	}

	smDPConfig := req.SMDPPlusConfig
	if smDPConfig != nil && len(smDPConfig) > 0 && h.encryptionKey != "" {
		encrypted, err := crypto.EncryptJSON(smDPConfig, h.encryptionKey)
		if err != nil {
			h.logger.Error().Err(err).Msg("encrypt sm-dp+ config on update")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		smDPConfig = encrypted
	}

	updated, err := h.operatorStore.Update(r.Context(), id, store.UpdateOperatorParams{
		Name:                      req.Name,
		AdapterConfig:             adapterConfig,
		SMDPPlusURL:               req.SMDPPlusURL,
		SMDPPlusConfig:            smDPConfig,
		SupportedRATTypes:         req.SupportedRATTypes,
		FailoverPolicy:            req.FailoverPolicy,
		FailoverTimeoutMs:         req.FailoverTimeoutMs,
		CircuitBreakerThreshold:   req.CircuitBreakerThreshold,
		CircuitBreakerRecoverySec: req.CircuitBreakerRecoverySec,
		HealthCheckIntervalSec:    req.HealthCheckIntervalSec,
		SLAUptimeTarget:           req.SLAUptimeTarget,
		State:                     req.State,
	})
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("update operator")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if req.AdapterConfig != nil || req.State != nil {
		h.adapterRegistry.Remove(id)
	}

	h.createAuditEntry(r, "operator.update", id.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toOperatorResponse(updated))
}

func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for health")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	latestLog, err := h.operatorStore.GetLatestHealth(r.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get latest health")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	total, failures, err := h.operatorStore.CountFailures24h(r.Context(), id)
	if err != nil {
		h.logger.Warn().Err(err).Str("operator_id", idStr).Msg("count failures 24h")
	}

	resp := healthResponse{
		HealthStatus: op.HealthStatus,
		CircuitState: "closed",
		Uptime24h:    100.0,
		FailureCount: failures,
	}

	if latestLog != nil {
		resp.LatencyMs = latestLog.LatencyMs
		resp.CircuitState = latestLog.CircuitState
		ts := latestLog.CheckedAt.Format(time.RFC3339Nano)
		resp.LastCheck = &ts
	}

	if total > 0 {
		resp.Uptime24h = float64(total-failures) / float64(total) * 100.0
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) TestConnection(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for test")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	adapterConfig := op.AdapterConfig
	if h.encryptionKey != "" {
		decrypted, err := crypto.DecryptJSON(adapterConfig, h.encryptionKey)
		if err != nil {
			h.logger.Warn().Err(err).Msg("decrypt adapter config for test — using raw")
		} else {
			adapterConfig = decrypted
		}
	}

	a, err := h.adapterRegistry.GetOrCreate(id, op.AdapterType, adapterConfig)
	if err != nil {
		h.logger.Error().Err(err).Str("adapter_type", op.AdapterType).Msg("create adapter for test")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create adapter")
		return
	}

	result := a.HealthCheck(r.Context())

	resp := testResponse{
		Success:   result.Success,
		LatencyMs: result.LatencyMs,
		Error:     result.Error,
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	var req createGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.TenantID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "tenant_id", "message": "Tenant ID is required", "code": "required"})
	}
	if req.OperatorID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "operator_id", "message": "Operator ID is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant_id format")
		return
	}
	operatorID, err := uuid.Parse(req.OperatorID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
		return
	}

	if _, err := h.tenantStore.GetByID(r.Context(), tenantID); err != nil {
		if errors.Is(err, store.ErrTenantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Tenant not found")
			return
		}
		h.logger.Error().Err(err).Msg("get tenant for grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Msg("get operator for grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromContext(r)

	g, err := h.operatorStore.CreateGrant(r.Context(), tenantID, operatorID, userID, req.SupportedRATTypes)
	if err != nil {
		if errors.Is(err, store.ErrGrantExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"This operator is already granted to this tenant")
			return
		}
		h.logger.Error().Err(err).Msg("create operator grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "operator_grant.create", g.ID.String(), nil, g)

	apierr.WriteSuccess(w, http.StatusCreated, toGrantResponse(g))
}

func (h *Handler) ListGrants(w http.ResponseWriter, r *http.Request) {
	role, _ := r.Context().Value(apierr.RoleKey).(string)
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	targetTenantID := tenantID
	if role == "super_admin" {
		if tid := r.URL.Query().Get("tenant_id"); tid != "" {
			parsed, err := uuid.Parse(tid)
			if err != nil {
				apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid tenant_id format")
				return
			}
			targetTenantID = parsed
		}
	}

	if !apierr.HasRole(role, "tenant_admin") && role != "super_admin" {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeInsufficientRole,
			"This action requires tenant_admin role or higher")
		return
	}

	grants, err := h.operatorStore.ListGrants(r.Context(), targetTenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list operator grants")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]grantResponse, 0, len(grants))
	for _, g := range grants {
		items = append(items, toGrantResponse(&g))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Limit: len(items),
	})
}

func (h *Handler) DeleteGrant(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid grant ID format")
		return
	}

	existing, err := h.operatorStore.GetGrantByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrGrantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator grant not found")
			return
		}
		h.logger.Error().Err(err).Str("grant_id", idStr).Msg("get grant for delete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.operatorStore.DeleteGrant(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrGrantNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator grant not found")
			return
		}
		h.logger.Error().Err(err).Str("grant_id", idStr).Msg("delete grant")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "operator_grant.delete", id.String(), existing, nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}) {
	if h.auditSvc == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	userID := userIDFromContext(r)
	ip := r.RemoteAddr
	ua := r.UserAgent()

	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, err := uuid.Parse(cidStr); err == nil {
			correlationID = &cid
		}
	}

	var beforeData, afterData json.RawMessage
	if before != nil {
		beforeData, _ = json.Marshal(before)
	}
	if after != nil {
		afterData, _ = json.Marshal(after)
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    "operator",
		EntityID:      entityID,
		BeforeData:    beforeData,
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}

func (h *Handler) GetHealthHistory(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	q := r.URL.Query()
	hours := 24
	if v := q.Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 168 {
			hours = n
		} else if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "hours must be between 1 and 168")
			return
		}
	}
	limit := 100
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		} else if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "limit must be between 1 and 500")
			return
		}
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for health history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	_ = op

	logs, err := h.operatorStore.GetHealthLogs(r.Context(), id, limit)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get health logs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to retrieve health history")
		return
	}

	window := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	filtered := make([]store.OperatorHealthLog, 0, len(logs))
	for _, l := range logs {
		if l.CheckedAt.After(window) {
			filtered = append(filtered, l)
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, filtered)
}

func (h *Handler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator ID format")
		return
	}

	window := r.URL.Query().Get("window")
	validWindows := map[string]bool{"15m": true, "1h": true, "6h": true, "24h": true}
	if window == "" {
		window = "1h"
	}
	if !validWindows[window] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "window must be one of: 15m, 1h, 6h, 24h")
		return
	}

	op, err := h.operatorStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator for metrics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	_ = op

	if h.cdrStore == nil {
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"window":  window,
			"buckets": []store.OperatorMetricBucket{},
		})
		return
	}

	buckets, err := h.cdrStore.GetOperatorMetrics(r.Context(), tenantID, id, window)
	if err != nil {
		h.logger.Error().Err(err).Str("operator_id", idStr).Msg("get operator metrics")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to retrieve operator metrics")
		return
	}

	if buckets == nil {
		buckets = []store.OperatorMetricBucket{}
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"window":  window,
		"buckets": buckets,
	})
}

func userIDFromContext(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
