package ippool

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var validPoolStates = map[string]bool{
	"active":   true,
	"disabled": true,
}

type Handler struct {
	ippoolStore *store.IPPoolStore
	apnStore    *store.APNStore
	auditSvc    audit.Auditor
	logger      zerolog.Logger
}

func NewHandler(
	ippoolStore *store.IPPoolStore,
	apnStore *store.APNStore,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		ippoolStore: ippoolStore,
		apnStore:    apnStore,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "ippool_handler").Logger(),
	}
}

type poolResponse struct {
	ID                      string   `json:"id"`
	TenantID                string   `json:"tenant_id"`
	APNID                   string   `json:"apn_id"`
	Name                    string   `json:"name"`
	CIDRv4                  *string  `json:"cidr_v4"`
	CIDRv6                  *string  `json:"cidr_v6"`
	TotalAddresses          int      `json:"total_addresses"`
	UsedAddresses           int      `json:"used_addresses"`
	AvailableAddresses      int      `json:"available_addresses"`
	UtilizationPct          float64  `json:"utilization_pct"`
	AlertThresholdWarning   int      `json:"alert_threshold_warning"`
	AlertThresholdCritical  int      `json:"alert_threshold_critical"`
	ReclaimGracePeriodDays  int      `json:"reclaim_grace_period_days"`
	State                   string   `json:"state"`
	CreatedAt               string   `json:"created_at"`
}

type addressResponse struct {
	ID             string  `json:"id"`
	PoolID         string  `json:"pool_id"`
	AddressV4      *string `json:"address_v4"`
	AddressV6      *string `json:"address_v6"`
	AllocationType string  `json:"allocation_type"`
	SimID          *string `json:"sim_id,omitempty"`
	State          string  `json:"state"`
	AllocatedAt    *string `json:"allocated_at,omitempty"`
	ReclaimAt      *string `json:"reclaim_at,omitempty"`
}

type createPoolRequest struct {
	APNID                   string `json:"apn_id"`
	Name                    string `json:"name"`
	CIDRv4                  *string `json:"cidr_v4"`
	CIDRv6                  *string `json:"cidr_v6"`
	AlertThresholdWarning   *int   `json:"alert_threshold_warning"`
	AlertThresholdCritical  *int   `json:"alert_threshold_critical"`
	ReclaimGracePeriodDays  *int   `json:"reclaim_grace_period_days"`
}

type updatePoolRequest struct {
	Name                    *string `json:"name"`
	AlertThresholdWarning   *int    `json:"alert_threshold_warning"`
	AlertThresholdCritical  *int    `json:"alert_threshold_critical"`
	ReclaimGracePeriodDays  *int    `json:"reclaim_grace_period_days"`
	State                   *string `json:"state"`
}

type reserveIPRequest struct {
	SimID     string  `json:"sim_id"`
	AddressV4 *string `json:"address_v4"`
}

func toPoolResponse(p *store.IPPool) poolResponse {
	utilPct := 0.0
	available := p.TotalAddresses - p.UsedAddresses
	if available < 0 {
		available = 0
	}
	if p.TotalAddresses > 0 {
		utilPct = float64(p.UsedAddresses) / float64(p.TotalAddresses) * 100.0
	}
	return poolResponse{
		ID:                      p.ID.String(),
		TenantID:                p.TenantID.String(),
		APNID:                   p.APNID.String(),
		Name:                    p.Name,
		CIDRv4:                  p.CIDRv4,
		CIDRv6:                  p.CIDRv6,
		TotalAddresses:          p.TotalAddresses,
		UsedAddresses:           p.UsedAddresses,
		AvailableAddresses:      available,
		UtilizationPct:          utilPct,
		AlertThresholdWarning:   p.AlertThresholdWarning,
		AlertThresholdCritical:  p.AlertThresholdCritical,
		ReclaimGracePeriodDays:  p.ReclaimGracePeriodDays,
		State:                   p.State,
		CreatedAt:               p.CreatedAt.Format(time.RFC3339Nano),
	}
}

func toAddressResponse(a *store.IPAddress) addressResponse {
	resp := addressResponse{
		ID:             a.ID.String(),
		PoolID:         a.PoolID.String(),
		AddressV4:      a.AddressV4,
		AddressV6:      a.AddressV6,
		AllocationType: a.AllocationType,
		State:          a.State,
	}
	if a.SimID != nil {
		s := a.SimID.String()
		resp.SimID = &s
	}
	if a.AllocatedAt != nil {
		s := a.AllocatedAt.Format(time.RFC3339Nano)
		resp.AllocatedAt = &s
	}
	if a.ReclaimAt != nil {
		s := a.ReclaimAt.Format(time.RFC3339Nano)
		resp.ReclaimAt = &s
	}
	return resp
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	apnIDStr := r.URL.Query().Get("apn_id")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	var apnIDFilter *uuid.UUID
	if apnIDStr != "" {
		parsed, err := uuid.Parse(apnIDStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
			return
		}
		apnIDFilter = &parsed
	}

	pools, nextCursor, err := h.ippoolStore.List(r.Context(), tenantID, cursor, limit, apnIDFilter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list ip pools")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]poolResponse, 0, len(pools))
	for _, p := range pools {
		items = append(items, toPoolResponse(&p))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req createPoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.APNID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "apn_id", "message": "APN ID is required", "code": "required"})
	}
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	} else if len(req.Name) > 100 {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name must be at most 100 characters", "code": "max_length"})
	}
	hasCIDR := false
	if req.CIDRv4 != nil && *req.CIDRv4 != "" {
		hasCIDR = true
		if _, _, err := net.ParseCIDR(*req.CIDRv4); err != nil {
			validationErrors = append(validationErrors, map[string]string{"field": "cidr_v4", "message": "Invalid IPv4 CIDR format", "code": "format"})
		}
	}
	if req.CIDRv6 != nil && *req.CIDRv6 != "" {
		hasCIDR = true
		if _, _, err := net.ParseCIDR(*req.CIDRv6); err != nil {
			validationErrors = append(validationErrors, map[string]string{"field": "cidr_v6", "message": "Invalid IPv6 CIDR format", "code": "format"})
		}
	}
	if !hasCIDR {
		validationErrors = append(validationErrors, map[string]string{"field": "cidr_v4", "message": "At least one of cidr_v4 or cidr_v6 is required", "code": "required"})
	}
	if req.AlertThresholdWarning != nil && (*req.AlertThresholdWarning < 0 || *req.AlertThresholdWarning > 100) {
		validationErrors = append(validationErrors, map[string]string{"field": "alert_threshold_warning", "message": "Must be between 0 and 100", "code": "range"})
	}
	if req.AlertThresholdCritical != nil && (*req.AlertThresholdCritical < 0 || *req.AlertThresholdCritical > 100) {
		validationErrors = append(validationErrors, map[string]string{"field": "alert_threshold_critical", "message": "Must be between 0 and 100", "code": "range"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	apnID, err := uuid.Parse(req.APNID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
		return
	}

	if _, err := h.apnStore.GetByID(r.Context(), tenantID, apnID); err != nil {
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Msg("get apn for pool create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	pool, err := h.ippoolStore.Create(r.Context(), tenantID, store.CreateIPPoolParams{
		APNID:                   apnID,
		Name:                    req.Name,
		CIDRv4:                  req.CIDRv4,
		CIDRv6:                  req.CIDRv6,
		AlertThresholdWarning:   req.AlertThresholdWarning,
		AlertThresholdCritical:  req.AlertThresholdCritical,
		ReclaimGracePeriodDays:  req.ReclaimGracePeriodDays,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create ip pool")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "ip_pool.create", pool.ID.String(), nil, pool)

	apierr.WriteSuccess(w, http.StatusCreated, toPoolResponse(pool))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid pool ID format")
		return
	}

	pool, err := h.ippoolStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrIPPoolNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "IP pool not found")
			return
		}
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("get ip pool")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toPoolResponse(pool))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid pool ID format")
		return
	}

	var req updatePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.State != nil && !validPoolStates[*req.State] {
		validationErrors = append(validationErrors, map[string]string{"field": "state", "message": "Invalid state. Allowed: active, disabled", "code": "invalid_enum"})
	}
	if req.AlertThresholdWarning != nil && (*req.AlertThresholdWarning < 0 || *req.AlertThresholdWarning > 100) {
		validationErrors = append(validationErrors, map[string]string{"field": "alert_threshold_warning", "message": "Must be between 0 and 100", "code": "range"})
	}
	if req.AlertThresholdCritical != nil && (*req.AlertThresholdCritical < 0 || *req.AlertThresholdCritical > 100) {
		validationErrors = append(validationErrors, map[string]string{"field": "alert_threshold_critical", "message": "Must be between 0 and 100", "code": "range"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	existing, err := h.ippoolStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrIPPoolNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "IP pool not found")
			return
		}
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("get pool for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	updated, err := h.ippoolStore.Update(r.Context(), tenantID, id, store.UpdateIPPoolParams{
		Name:                    req.Name,
		AlertThresholdWarning:   req.AlertThresholdWarning,
		AlertThresholdCritical:  req.AlertThresholdCritical,
		ReclaimGracePeriodDays:  req.ReclaimGracePeriodDays,
		State:                   req.State,
	})
	if err != nil {
		if errors.Is(err, store.ErrIPPoolNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "IP pool not found")
			return
		}
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("update pool")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "ip_pool.update", id.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toPoolResponse(updated))
}

func (h *Handler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	poolID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid pool ID format")
		return
	}

	if _, err := h.ippoolStore.GetByID(r.Context(), tenantID, poolID); err != nil {
		if errors.Is(err, store.ErrIPPoolNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "IP pool not found")
			return
		}
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("get pool for list addresses")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	stateFilter := r.URL.Query().Get("state")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	addresses, nextCursor, err := h.ippoolStore.ListAddresses(r.Context(), poolID, cursor, limit, stateFilter)
	if err != nil {
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("list addresses")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]addressResponse, 0, len(addresses))
	for _, a := range addresses {
		items = append(items, toAddressResponse(&a))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) ReserveIP(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	poolID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid pool ID format")
		return
	}

	var req reserveIPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.SimID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "sim_id", "message": "SIM ID is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	simID, err := uuid.Parse(req.SimID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format")
		return
	}

	if _, err := h.ippoolStore.GetByID(r.Context(), tenantID, poolID); err != nil {
		if errors.Is(err, store.ErrIPPoolNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "IP pool not found")
			return
		}
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("get pool for reserve")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	addr, err := h.ippoolStore.ReserveStaticIP(r.Context(), poolID, simID, req.AddressV4)
	if err != nil {
		if errors.Is(err, store.ErrPoolExhausted) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePoolExhausted,
				"IP pool is exhausted. No available addresses.")
			return
		}
		if errors.Is(err, store.ErrIPAlreadyAllocated) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeIPAlreadyAllocated,
				"The requested IP address is not available")
			return
		}
		h.logger.Error().Err(err).Str("pool_id", idStr).Msg("reserve ip")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "ip_address.reserve", addr.ID.String(), nil, addr)

	apierr.WriteSuccess(w, http.StatusCreated, toAddressResponse(addr))
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
		EntityType:    "ip_pool",
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

func userIDFromContext(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
