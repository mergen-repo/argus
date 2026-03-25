package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/cache"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var validSIMTypes = map[string]bool{
	"physical": true,
	"esim":     true,
}

var validRATTypes = map[string]bool{
	"nb_iot": true,
	"lte_m":  true,
	"lte":    true,
	"nr_5g":  true,
}

type Handler struct {
	simStore      *store.SIMStore
	apnStore      *store.APNStore
	operatorStore *store.OperatorStore
	ippoolStore   *store.IPPoolStore
	tenantStore   *store.TenantStore
	policyStore   *store.PolicyStore
	nameCache     *cache.NameCache
	auditSvc      audit.Auditor
	logger        zerolog.Logger
}

func NewHandler(
	simStore *store.SIMStore,
	apnStore *store.APNStore,
	operatorStore *store.OperatorStore,
	ippoolStore *store.IPPoolStore,
	tenantStore *store.TenantStore,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
	opts ...func(*Handler),
) *Handler {
	h := &Handler{
		simStore:      simStore,
		apnStore:      apnStore,
		operatorStore: operatorStore,
		ippoolStore:   ippoolStore,
		tenantStore:   tenantStore,
		auditSvc:      auditSvc,
		logger:        logger.With().Str("component", "sim_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func WithPolicyStore(ps *store.PolicyStore) func(*Handler) {
	return func(h *Handler) {
		h.policyStore = ps
	}
}

func WithNameCache(nc *cache.NameCache) func(*Handler) {
	return func(h *Handler) {
		h.nameCache = nc
	}
}

type simResponse struct {
	ID                    string          `json:"id"`
	TenantID              string          `json:"tenant_id"`
	OperatorID            string          `json:"operator_id"`
	OperatorName          string          `json:"operator_name,omitempty"`
	APNID                 *string         `json:"apn_id,omitempty"`
	APNName               string          `json:"apn_name,omitempty"`
	ICCID                 string          `json:"iccid"`
	IMSI                  string          `json:"imsi"`
	MSISDN                *string         `json:"msisdn,omitempty"`
	IPAddressID           *string         `json:"ip_address_id,omitempty"`
	IPAddress             string          `json:"ip_address,omitempty"`
	IPPoolName            string          `json:"ip_pool_name,omitempty"`
	PolicyVersionID       *string         `json:"policy_version_id,omitempty"`
	PolicyName            string          `json:"policy_name,omitempty"`
	ESimProfileID         *string         `json:"esim_profile_id,omitempty"`
	SimType               string          `json:"sim_type"`
	State                 string          `json:"state"`
	RATType               *string         `json:"rat_type,omitempty"`
	MaxConcurrentSessions int             `json:"max_concurrent_sessions"`
	SessionIdleTimeoutSec int             `json:"session_idle_timeout_sec"`
	SessionHardTimeoutSec int             `json:"session_hard_timeout_sec"`
	Metadata              json.RawMessage `json:"metadata"`
	ActivatedAt           *string         `json:"activated_at,omitempty"`
	SuspendedAt           *string         `json:"suspended_at,omitempty"`
	TerminatedAt          *string         `json:"terminated_at,omitempty"`
	PurgeAt               *string         `json:"purge_at,omitempty"`
	CreatedAt             string          `json:"created_at"`
	UpdatedAt             string          `json:"updated_at"`
}

type simHistoryResponse struct {
	ID          int64   `json:"id"`
	SimID       string  `json:"sim_id"`
	FromState   *string `json:"from_state"`
	ToState     string  `json:"to_state"`
	Reason      *string `json:"reason,omitempty"`
	TriggeredBy string  `json:"triggered_by"`
	UserID      *string `json:"user_id,omitempty"`
	JobID       *string `json:"job_id,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type createSIMRequest struct {
	ICCID      string          `json:"iccid"`
	IMSI       string          `json:"imsi"`
	MSISDN     *string         `json:"msisdn"`
	OperatorID string          `json:"operator_id"`
	APNID      string          `json:"apn_id"`
	SimType    string          `json:"sim_type"`
	RATType    *string         `json:"rat_type"`
	Metadata   json.RawMessage `json:"metadata"`
}

type reasonRequest struct {
	Reason *string `json:"reason"`
}

func toSIMResponse(s *store.SIM) simResponse {
	resp := simResponse{
		ID:                    s.ID.String(),
		TenantID:              s.TenantID.String(),
		OperatorID:            s.OperatorID.String(),
		ICCID:                 s.ICCID,
		IMSI:                  s.IMSI,
		MSISDN:                s.MSISDN,
		SimType:               s.SimType,
		State:                 s.State,
		RATType:               s.RATType,
		MaxConcurrentSessions: s.MaxConcurrentSessions,
		SessionIdleTimeoutSec: s.SessionIdleTimeoutSec,
		SessionHardTimeoutSec: s.SessionHardTimeoutSec,
		Metadata:              s.Metadata,
		CreatedAt:             s.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:             s.UpdatedAt.Format(time.RFC3339Nano),
	}
	if s.APNID != nil {
		v := s.APNID.String()
		resp.APNID = &v
	}
	if s.IPAddressID != nil {
		v := s.IPAddressID.String()
		resp.IPAddressID = &v
	}
	if s.PolicyVersionID != nil {
		v := s.PolicyVersionID.String()
		resp.PolicyVersionID = &v
	}
	if s.ESimProfileID != nil {
		v := s.ESimProfileID.String()
		resp.ESimProfileID = &v
	}
	if s.ActivatedAt != nil {
		v := s.ActivatedAt.Format(time.RFC3339Nano)
		resp.ActivatedAt = &v
	}
	if s.SuspendedAt != nil {
		v := s.SuspendedAt.Format(time.RFC3339Nano)
		resp.SuspendedAt = &v
	}
	if s.TerminatedAt != nil {
		v := s.TerminatedAt.Format(time.RFC3339Nano)
		resp.TerminatedAt = &v
	}
	if s.PurgeAt != nil {
		v := s.PurgeAt.Format(time.RFC3339Nano)
		resp.PurgeAt = &v
	}
	return resp
}

func toHistoryResponse(h *store.SimStateHistory) simHistoryResponse {
	resp := simHistoryResponse{
		ID:          h.ID,
		SimID:       h.SimID.String(),
		FromState:   h.FromState,
		ToState:     h.ToState,
		Reason:      h.Reason,
		TriggeredBy: h.TriggeredBy,
		CreatedAt:   h.CreatedAt.Format(time.RFC3339Nano),
	}
	if h.UserID != nil {
		v := h.UserID.String()
		resp.UserID = &v
	}
	if h.JobID != nil {
		v := h.JobID.String()
		resp.JobID = &v
	}
	return resp
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req createSIMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.ICCID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "iccid", "message": "ICCID is required", "code": "required"})
	} else if len(req.ICCID) > 22 {
		validationErrors = append(validationErrors, map[string]string{"field": "iccid", "message": "ICCID must be at most 22 characters", "code": "max_length"})
	}
	if req.IMSI == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "imsi", "message": "IMSI is required", "code": "required"})
	} else if len(req.IMSI) > 15 {
		validationErrors = append(validationErrors, map[string]string{"field": "imsi", "message": "IMSI must be at most 15 characters", "code": "max_length"})
	}
	if req.OperatorID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "operator_id", "message": "Operator ID is required", "code": "required"})
	}
	if req.APNID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "apn_id", "message": "APN ID is required", "code": "required"})
	}
	if req.SimType == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "sim_type", "message": "SIM type is required", "code": "required"})
	} else if !validSIMTypes[req.SimType] {
		validationErrors = append(validationErrors, map[string]string{"field": "sim_type", "message": "Invalid SIM type. Allowed: physical, esim", "code": "invalid_enum"})
	}
	if req.RATType != nil && *req.RATType != "" && !validRATTypes[*req.RATType] {
		validationErrors = append(validationErrors, map[string]string{"field": "rat_type", "message": "Invalid RAT type. Allowed: nb_iot, lte_m, lte, nr_5g", "code": "invalid_enum"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	operatorID, err := uuid.Parse(req.OperatorID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
		return
	}

	apnID, err := uuid.Parse(req.APNID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
		return
	}

	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Msg("get operator for sim create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if _, err := h.apnStore.GetByID(r.Context(), tenantID, apnID); err != nil {
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Msg("get apn for sim create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromCtx(r)

	sim, err := h.simStore.Create(r.Context(), tenantID, store.CreateSIMParams{
		ICCID:      req.ICCID,
		IMSI:       req.IMSI,
		MSISDN:     req.MSISDN,
		OperatorID: operatorID,
		APNID:      apnID,
		SimType:    req.SimType,
		RATType:    req.RATType,
		Metadata:   req.Metadata,
	})
	if err != nil {
		if errors.Is(err, store.ErrICCIDExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeICCIDExists,
				"A SIM with this ICCID already exists",
				[]map[string]string{{"field": "iccid", "value": req.ICCID}})
			return
		}
		if errors.Is(err, store.ErrIMSIExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeIMSIExists,
				"A SIM with this IMSI already exists",
				[]map[string]string{{"field": "imsi", "value": req.IMSI}})
			return
		}
		h.logger.Error().Err(err).Msg("create sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.create", sim.ID.String(), nil, sim, userID)

	apierr.WriteSuccess(w, http.StatusCreated, toSIMResponse(sim))
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
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	sim, err := h.simStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toSIMResponse(sim)
	h.enrichSIMResponse(r.Context(), tenantID, sim, &resp)
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) enrichSIMResponse(ctx context.Context, tenantID uuid.UUID, sim *store.SIM, resp *simResponse) {
	if op, err := h.operatorStore.GetByID(ctx, sim.OperatorID); err == nil {
		resp.OperatorName = op.Name
	}

	if sim.APNID != nil {
		if apn, err := h.apnStore.GetByID(ctx, tenantID, *sim.APNID); err == nil {
			if apn.DisplayName != nil && *apn.DisplayName != "" {
				resp.APNName = *apn.DisplayName
			} else {
				resp.APNName = apn.Name
			}
		}
	}

	if sim.IPAddressID != nil && h.ippoolStore != nil {
		if addr, err := h.ippoolStore.GetAddressByID(ctx, *sim.IPAddressID); err == nil {
			if addr.AddressV4 != nil {
				resp.IPAddress = *addr.AddressV4
			} else if addr.AddressV6 != nil {
				resp.IPAddress = *addr.AddressV6
			}
			if pool, err := h.ippoolStore.GetByID(ctx, tenantID, addr.PoolID); err == nil {
				resp.IPPoolName = pool.Name
			}
		}
	}

	if sim.PolicyVersionID != nil && h.policyStore != nil {
		if pv, err := h.policyStore.GetVersionByID(ctx, *sim.PolicyVersionID); err == nil {
			if p, err := h.policyStore.GetByID(ctx, tenantID, pv.PolicyID); err == nil {
				resp.PolicyName = fmt.Sprintf("%s (v%d)", p.Name, pv.Version)
			}
		}
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
			return
		}
		operatorID = &parsed
	}

	var apnID *uuid.UUID
	if v := q.Get("apn_id"); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid apn_id format")
			return
		}
		apnID = &parsed
	}

	params := store.ListSIMsParams{
		Cursor:     q.Get("cursor"),
		Limit:      limit,
		ICCID:      q.Get("iccid"),
		IMSI:       q.Get("imsi"),
		MSISDN:     q.Get("msisdn"),
		IPAddress:  q.Get("ip"),
		OperatorID: operatorID,
		APNID:      apnID,
		State:      q.Get("state"),
		RATType:    q.Get("rat_type"),
		Q:          q.Get("q"),
	}

	sims, nextCursor, err := h.simStore.List(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list sims")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	ipIDs := make([]uuid.UUID, 0)
	for _, s := range sims {
		if s.IPAddressID != nil {
			ipIDs = append(ipIDs, *s.IPAddressID)
		}
	}
	ipMap := make(map[uuid.UUID]string)
	ipPoolIDMap := make(map[uuid.UUID]uuid.UUID) // ip_address_id -> pool_id
	if len(ipIDs) > 0 && h.ippoolStore != nil {
		for _, ipID := range ipIDs {
			if addr, err := h.ippoolStore.GetAddressByID(r.Context(), ipID); err == nil {
				if addr.AddressV4 != nil {
					ipMap[ipID] = *addr.AddressV4
				} else if addr.AddressV6 != nil {
					ipMap[ipID] = *addr.AddressV6
				}
				ipPoolIDMap[ipID] = addr.PoolID
			}
		}
	}
	poolNameMap := make(map[uuid.UUID]string)
	if len(ipPoolIDMap) > 0 && h.ippoolStore != nil {
		seen := make(map[uuid.UUID]bool)
		for _, poolID := range ipPoolIDMap {
			if seen[poolID] {
				continue
			}
			seen[poolID] = true
			if h.nameCache != nil {
				if name, ok := h.nameCache.GetPoolName(r.Context(), poolID); ok {
					poolNameMap[poolID] = name
					continue
				}
			}
			if pool, err := h.ippoolStore.GetByID(r.Context(), tenantID, poolID); err == nil {
				poolNameMap[poolID] = pool.Name
				if h.nameCache != nil {
					h.nameCache.SetPoolName(r.Context(), poolID, pool.Name)
				}
			}
		}
	}

	opIDs := make(map[uuid.UUID]bool)
	apnIDs := make(map[uuid.UUID]bool)
	for _, s := range sims {
		opIDs[s.OperatorID] = true
		if s.APNID != nil {
			apnIDs[*s.APNID] = true
		}
	}
	opNameMap := make(map[uuid.UUID]string)
	for opID := range opIDs {
		if h.nameCache != nil {
			if name, ok := h.nameCache.GetOperatorName(r.Context(), opID); ok {
				opNameMap[opID] = name
				continue
			}
		}
		if op, err := h.operatorStore.GetByID(r.Context(), opID); err == nil {
			opNameMap[opID] = op.Name
			if h.nameCache != nil {
				h.nameCache.SetOperatorName(r.Context(), opID, op.Name)
			}
		}
	}
	apnNameMap := make(map[uuid.UUID]string)
	apnPoolNameMap := make(map[uuid.UUID]string) // apn_id -> first pool name
	for aID := range apnIDs {
		cached := false
		if h.nameCache != nil {
			if name, ok := h.nameCache.GetAPNName(r.Context(), aID); ok {
				apnNameMap[aID] = name
				cached = true
			}
		}
		if !cached {
			if apn, err := h.apnStore.GetByID(r.Context(), tenantID, aID); err == nil {
				if apn.DisplayName != nil && *apn.DisplayName != "" {
					apnNameMap[aID] = *apn.DisplayName
				} else {
					apnNameMap[aID] = apn.Name
				}
				if h.nameCache != nil {
					h.nameCache.SetAPNName(r.Context(), aID, apnNameMap[aID])
				}
			}
		}
		if h.ippoolStore != nil {
			if pools, _, err := h.ippoolStore.List(r.Context(), tenantID, "", 1, &aID); err == nil && len(pools) > 0 {
				apnPoolNameMap[aID] = pools[0].Name
			}
		}
	}

	items := make([]simResponse, 0, len(sims))
	for _, s := range sims {
		resp := toSIMResponse(&s)
		if s.IPAddressID != nil {
			if ip, ok := ipMap[*s.IPAddressID]; ok {
				resp.IPAddress = ip
			}
			if poolID, ok := ipPoolIDMap[*s.IPAddressID]; ok {
				if pname, ok := poolNameMap[poolID]; ok {
					resp.IPPoolName = pname
				}
			}
		}
		if resp.IPPoolName == "" && s.APNID != nil {
			if pname, ok := apnPoolNameMap[*s.APNID]; ok {
				resp.IPPoolName = pname
			}
		}
		if name, ok := opNameMap[s.OperatorID]; ok {
			resp.OperatorName = name
		}
		if s.APNID != nil {
			if name, ok := apnNameMap[*s.APNID]; ok {
				resp.APNName = name
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

func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	if _, err := h.simStore.GetByID(r.Context(), tenantID, simID); err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	history, nextCursor, err := h.simStore.ListStateHistory(r.Context(), simID, q.Get("cursor"), limit)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("list sim history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]simHistoryResponse, 0, len(history))
	for _, entry := range history {
		items = append(items, toHistoryResponse(&entry))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	existing, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for activate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if existing.APNID == nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "SIM has no APN assigned")
		return
	}

	pools, _, err := h.ippoolStore.List(r.Context(), tenantID, "", 1, existing.APNID)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("list ip pools for activate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var allocatedIP *store.IPAddress
	if len(pools) > 0 {
		allocatedIP, err = h.ippoolStore.AllocateIP(r.Context(), pools[0].ID, simID)
		if err != nil {
			if errors.Is(err, store.ErrPoolExhausted) {
				apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodePoolExhausted,
					"No IP addresses available in the pool")
				return
			}
			h.logger.Error().Err(err).Str("sim_id", idStr).Msg("allocate ip for activate")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
	}

	var ipAddressID uuid.UUID
	if allocatedIP != nil {
		ipAddressID = allocatedIP.ID
	}

	userID := userIDFromCtx(r)

	sim, err := h.simStore.Activate(r.Context(), tenantID, simID, ipAddressID, userID)
	if err != nil {
		if allocatedIP != nil {
			if releaseErr := h.ippoolStore.ReleaseIP(r.Context(), pools[0].ID, simID); releaseErr != nil {
				h.logger.Error().Err(releaseErr).Str("sim_id", idStr).Msg("rollback ip allocation")
			}
		}
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		if errors.Is(err, store.ErrInvalidStateTransition) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
				fmt.Sprintf("Cannot activate SIM in '%s' state", existing.State))
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("activate sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.activate", simID.String(), existing, sim, userID)

	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(sim))
}

func (h *Handler) Suspend(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	existing, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for suspend")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var req reasonRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	userID := userIDFromCtx(r)

	sim, err := h.simStore.Suspend(r.Context(), tenantID, simID, userID, req.Reason)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStateTransition) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
				fmt.Sprintf("Cannot suspend SIM in '%s' state", existing.State))
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("suspend sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.suspend", simID.String(), existing, sim, userID)

	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(sim))
}

func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	existing, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for resume")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromCtx(r)

	sim, err := h.simStore.Resume(r.Context(), tenantID, simID, userID)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStateTransition) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
				fmt.Sprintf("Cannot resume SIM in '%s' state", existing.State))
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("resume sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.resume", simID.String(), existing, sim, userID)

	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(sim))
}

func (h *Handler) Terminate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	existing, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for terminate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var req reasonRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	tenant, err := h.tenantStore.GetByID(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("get tenant for purge retention")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromCtx(r)

	sim, err := h.simStore.Terminate(r.Context(), tenantID, simID, userID, req.Reason, tenant.PurgeRetentionDays)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStateTransition) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
				fmt.Sprintf("Cannot terminate SIM in '%s' state", existing.State))
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("terminate sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.terminate", simID.String(), existing, sim, userID)

	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(sim))
}

func (h *Handler) ReportLost(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	existing, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for report lost")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var req reasonRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	userID := userIDFromCtx(r)

	sim, err := h.simStore.ReportLost(r.Context(), tenantID, simID, userID, req.Reason)
	if err != nil {
		if errors.Is(err, store.ErrInvalidStateTransition) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
				fmt.Sprintf("Cannot report SIM as lost in '%s' state", existing.State))
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("report sim lost")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.report_lost", simID.String(), existing, sim, userID)

	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(sim))
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}, userID *uuid.UUID) {
	if h.auditSvc == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
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
		EntityType:    "sim",
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

func userIDFromCtx(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
