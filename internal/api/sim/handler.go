package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/aaa/validator"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/cache"
	"github.com/btopcu/argus/internal/store"
	undopkg "github.com/btopcu/argus/internal/undo"
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
	cdrStore      *store.CDRStore
	sessionStore  *store.RadiusSessionStore
	nameCache     *cache.NameCache
	undoRegistry  *undopkg.Registry
	auditSvc      audit.Auditor
	imsiStrict    bool
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

func WithUndoRegistry(r *undopkg.Registry) func(*Handler) {
	return func(h *Handler) {
		h.undoRegistry = r
	}
}

func WithSessionStore(ss *store.RadiusSessionStore) func(*Handler) {
	return func(h *Handler) {
		h.sessionStore = ss
	}
}

func WithCDRStore(cs *store.CDRStore) func(*Handler) {
	return func(h *Handler) {
		h.cdrStore = cs
	}
}

// WithIMSIStrictValidation enables PLMN format validation at the SIM Create
// API boundary (FIX-207 AC-4). Mirrors the IMSI_STRICT_VALIDATION config flag.
func WithIMSIStrictValidation(v bool) func(*Handler) {
	return func(h *Handler) {
		h.imsiStrict = v
	}
}

type simResponse struct {
	ID                    string          `json:"id"`
	TenantID              string          `json:"tenant_id"`
	OperatorID            string          `json:"operator_id"`
	OperatorName          string          `json:"operator_name,omitempty"`
	OperatorCode          string          `json:"operator_code,omitempty"`
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
	PolicyVersionNumber   int             `json:"policy_version_number,omitempty"`
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

func toSIMResponseBase(s *store.SIM) simResponse {
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

func toSIMResponse(s *store.SIMWithNames) simResponse {
	resp := toSIMResponseBase(&s.SIM)
	if s.OperatorName != nil {
		resp.OperatorName = *s.OperatorName
	}
	if s.OperatorCode != nil {
		resp.OperatorCode = *s.OperatorCode
	}
	if s.APNName != nil {
		resp.APNName = *s.APNName
	}
	if s.PolicyName != nil {
		resp.PolicyName = *s.PolicyName
	}
	if s.PolicyVersionNumber != nil {
		resp.PolicyVersionNumber = *s.PolicyVersionNumber
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

	// FIX-207 AC-4: IMSI format validation at API boundary.
	if err := validator.ValidateIMSI(req.IMSI, h.imsiStrict); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidIMSIFormat,
			"IMSI format is invalid",
			[]map[string]string{{"field": "imsi", "value": req.IMSI, "expected": `^\d{14,15}$`}})
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
		var refErr *store.InvalidReferenceError
		if errors.As(err, &refErr) {
			field := refErr.Column
			if field == "" {
				field = "reference"
			}
			// Map column name -> referenced entity for a plan-aligned message
			// (e.g. "operator_id does not reference an existing operator").
			entity := field
			switch field {
			case "operator_id":
				entity = "operator"
			case "apn_id":
				entity = "apn"
			case "ip_address_id":
				entity = "ip_address"
			}
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidReference,
				field+" does not reference an existing "+entity,
				[]map[string]string{{"field": field, "constraint": refErr.Constraint}})
			return
		}
		h.logger.Error().Err(err).Msg("create sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.create", sim.ID.String(), nil, sim, userID)

	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, sim.ID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", sim.ID.String()).Msg("get enriched sim after create")
		apierr.WriteSuccess(w, http.StatusCreated, toSIMResponseBase(sim))
		return
	}
	apierr.WriteSuccess(w, http.StatusCreated, toSIMResponse(enriched))
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

	sim, err := h.simStore.GetByIDEnriched(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(sim))
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

	sims, nextCursor, err := h.simStore.ListEnriched(r.Context(), tenantID, params)
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
	apnPoolNameMap := make(map[uuid.UUID]string) // apn_id -> first pool name
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
	if h.ippoolStore != nil {
		apnIDs := make(map[uuid.UUID]bool)
		for _, s := range sims {
			if s.APNID != nil {
				apnIDs[*s.APNID] = true
			}
		}
		for aID := range apnIDs {
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

// GetCurrentIP returns the SIM's currently-allocated IP with enrichment:
// pool name, CIDR, allocation type, state, allocated_at. Returns an
// explicit null-ish payload when the SIM has no IP (keeps FE branching
// simple). Full historical IP allocation timeline (as distinct from
// "current") is out of scope — tracked as STORY-087 follow-up.
//
// Route: GET /api/v1/sims/:id/ip-current
// Role:  api_user+
func (h *Handler) GetCurrentIP(w http.ResponseWriter, r *http.Request) {
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

	sim, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for ip-current")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if sim.IPAddressID == nil {
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"allocated": false,
		})
		return
	}

	if h.ippoolStore == nil {
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"allocated": true,
			"ip_id":     sim.IPAddressID.String(),
		})
		return
	}

	ipAddr, err := h.ippoolStore.GetIPAddressByID(r.Context(), *sim.IPAddressID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", idStr).Msg("get ip address for current")
		apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
			"allocated": true,
			"ip_id":     sim.IPAddressID.String(),
		})
		return
	}

	resp := map[string]interface{}{
		"allocated":       true,
		"ip_id":           ipAddr.ID.String(),
		"address_v4":      ipAddr.AddressV4,
		"address_v6":      ipAddr.AddressV6,
		"state":           ipAddr.State,
		"allocation_type": ipAddr.AllocationType,
		"allocated_at":    ipAddr.AllocatedAt,
	}

	if pool, err := h.ippoolStore.GetByID(r.Context(), tenantID, ipAddr.PoolID); err == nil {
		resp["pool_id"] = pool.ID.String()
		resp["pool_name"] = pool.Name
		if pool.CIDRv4 != nil {
			resp["pool_cidr_v4"] = *pool.CIDRv4
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

type simSessionResponse struct {
	ID            string  `json:"id"`
	SimID         string  `json:"sim_id"`
	OperatorID    string  `json:"operator_id"`
	APNID         *string `json:"apn_id"`
	NASIP         *string `json:"nas_ip"`
	FramedIP      *string `json:"framed_ip"`
	RATType       *string `json:"rat_type"`
	SessionState  string  `json:"session_state"`
	AcctSessionID *string `json:"acct_session_id"`
	StartedAt     string  `json:"started_at"`
	EndedAt       *string `json:"ended_at"`
	BytesIn       int64   `json:"bytes_in"`
	BytesOut      int64   `json:"bytes_out"`
	DurationSec   int64   `json:"duration_sec"`
	ProtocolType  string  `json:"protocol_type"`
}

func toSimSessionResponse(s *store.RadiusSession) simSessionResponse {
	resp := simSessionResponse{
		ID:            s.ID.String(),
		SimID:         s.SimID.String(),
		OperatorID:    s.OperatorID.String(),
		NASIP:         s.NASIP,
		FramedIP:      s.FramedIP,
		RATType:       s.RATType,
		SessionState:  s.SessionState,
		AcctSessionID: s.AcctSessionID,
		StartedAt:     s.StartedAt.Format(time.RFC3339),
		BytesIn:       s.BytesIn,
		BytesOut:      s.BytesOut,
		ProtocolType:  s.ProtocolType,
	}
	if s.APNID != nil {
		v := s.APNID.String()
		resp.APNID = &v
	}
	if s.EndedAt != nil {
		v := s.EndedAt.Format(time.RFC3339)
		resp.EndedAt = &v
		resp.DurationSec = int64(s.EndedAt.Sub(s.StartedAt).Seconds())
	} else {
		resp.DurationSec = int64(time.Since(s.StartedAt).Seconds())
	}
	return resp
}

func (h *Handler) GetSessions(w http.ResponseWriter, r *http.Request) {
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
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.sessionStore == nil {
		apierr.WriteList(w, http.StatusOK, []simSessionResponse{}, apierr.ListMeta{Limit: 50})
		return
	}

	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	sessions, nextCursor, err := h.sessionStore.ListBySIM(r.Context(), tenantID, simID, store.ListBySIMSessionParams{
		Cursor: q.Get("cursor"),
		Limit:  limit,
		State:  q.Get("state"),
	})
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("list sim sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]simSessionResponse, 0, len(sessions))
	for i := range sessions {
		items = append(items, toSimSessionResponse(&sessions[i]))
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

	// Auto-match policy: find active policy whose DSL references this APN and assign its current version
	if h.policyStore != nil && h.apnStore != nil && sim.APNID != nil && sim.PolicyVersionID == nil {
		if apn, apnErr := h.apnStore.GetByID(r.Context(), tenantID, *sim.APNID); apnErr == nil {
			if policies, _, pErr := h.policyStore.ListReferencingAPN(r.Context(), tenantID, apn.Name, 10, ""); pErr == nil {
				for _, pol := range policies {
					if pol.State == "active" && pol.CurrentVersionID != nil {
						if setErr := h.simStore.SetIPAndPolicy(r.Context(), simID, sim.IPAddressID, pol.CurrentVersionID); setErr == nil {
							sim.PolicyVersionID = pol.CurrentVersionID
							h.createAuditEntry(r, "sim.policy_auto_assigned", simID.String(), nil, map[string]interface{}{"policy_id": pol.ID, "version_id": *pol.CurrentVersionID}, userID)
						}
						break
					}
				}
			}
		}
	}

	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", idStr).Msg("get enriched sim after activate")
		apierr.WriteSuccess(w, http.StatusOK, toSIMResponseBase(sim))
		return
	}
	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(enriched))
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
	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", idStr).Msg("get enriched sim after suspend")
		h.writeSIMWithUndo(w, r, simID, existing.State, toSIMResponseBase(sim))
		return
	}
	h.writeSIMWithUndo(w, r, simID, existing.State, toSIMResponse(enriched))
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
	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", idStr).Msg("get enriched sim after resume")
		h.writeSIMWithUndo(w, r, simID, existing.State, toSIMResponseBase(sim))
		return
	}
	h.writeSIMWithUndo(w, r, simID, existing.State, toSIMResponse(enriched))
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

	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", idStr).Msg("get enriched sim after terminate")
		apierr.WriteSuccess(w, http.StatusOK, toSIMResponseBase(sim))
		return
	}
	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(enriched))
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
	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", idStr).Msg("get enriched sim after report lost")
		h.writeSIMWithUndo(w, r, simID, existing.State, toSIMResponseBase(sim))
		return
	}
	h.writeSIMWithUndo(w, r, simID, existing.State, toSIMResponse(enriched))
}

func (h *Handler) writeSIMWithUndo(w http.ResponseWriter, r *http.Request, simID uuid.UUID, previousState string, data interface{}) {
	meta := map[string]string{}
	if h.undoRegistry != nil {
		userID := userIDFromCtx(r)
		tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
		if userID != nil && tenantID != uuid.Nil {
			actionID, err := h.undoRegistry.Register(r.Context(), tenantID, *userID, "sim_state_restore", map[string]string{
				"sim_id": simID.String(),
				"state":  previousState,
			})
			if err != nil {
				h.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("register undo for sim state change")
			} else {
				meta["undo_action_id"] = actionID
			}
		}
	}

	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data:   data,
		Meta:   meta,
	})
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
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

	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	patch := make(map[string]interface{})
	var validationErrors []map[string]string

	if v, exists := body["label"]; exists {
		if v == nil {
			patch["label"] = nil
		} else if s, ok := v.(string); ok {
			if len(s) > 255 {
				validationErrors = append(validationErrors, map[string]string{"field": "label", "message": "Label must be at most 255 characters", "code": "max_length"})
			} else {
				patch["label"] = s
			}
		} else {
			validationErrors = append(validationErrors, map[string]string{"field": "label", "message": "Label must be a string", "code": "invalid_type"})
		}
	}

	if v, exists := body["notes"]; exists {
		if v == nil {
			patch["notes"] = nil
		} else if s, ok := v.(string); ok {
			if len(s) > 2000 {
				validationErrors = append(validationErrors, map[string]string{"field": "notes", "message": "Notes must be at most 2000 characters", "code": "max_length"})
			} else {
				patch["notes"] = s
			}
		} else {
			validationErrors = append(validationErrors, map[string]string{"field": "notes", "message": "Notes must be a string", "code": "invalid_type"})
		}
	}

	if v, exists := body["custom_attributes"]; exists {
		if v == nil {
			patch["custom_attributes"] = nil
		} else if m, ok := v.(map[string]interface{}); ok {
			if len(m) > 50 {
				validationErrors = append(validationErrors, map[string]string{"field": "custom_attributes", "message": "Custom attributes must have at most 50 keys", "code": "max_keys"})
			} else {
				patch["custom_attributes"] = m
			}
		} else {
			validationErrors = append(validationErrors, map[string]string{"field": "custom_attributes", "message": "Custom attributes must be a JSON object", "code": "invalid_type"})
		}
	}

	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	if len(patch) == 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "No valid fields to update")
		return
	}

	existing, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for patch")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if existing.State == "terminated" || existing.State == "purged" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
			fmt.Sprintf("Cannot update SIM in '%s' state", existing.State))
		return
	}

	userID := userIDFromCtx(r)

	sim, err := h.simStore.PatchMetadata(r.Context(), tenantID, simID, patch)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		if errors.Is(err, store.ErrSIMStateBlocked) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidStateTransition,
				fmt.Sprintf("Cannot update SIM in '%s' state", existing.State))
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("patch sim metadata")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "sim.patch_metadata", simID.String(), existing, sim, userID)

	enriched, enrichErr := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID)
	if enrichErr != nil {
		h.logger.Warn().Err(enrichErr).Str("sim_id", idStr).Msg("get enriched sim after patch")
		apierr.WriteSuccess(w, http.StatusOK, toSIMResponseBase(sim))
		return
	}
	apierr.WriteSuccess(w, http.StatusOK, toSIMResponse(enriched))
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

type usageResponse struct {
	SimID         string            `json:"sim_id"`
	Period        string            `json:"period"`
	TotalBytesIn  int64             `json:"total_bytes_in"`
	TotalBytesOut int64             `json:"total_bytes_out"`
	TotalCost     float64           `json:"total_cost"`
	Series        []usageBucketResp `json:"series"`
	TopSessions   []topSessionResp  `json:"top_sessions"`
}

type usageBucketResp struct {
	Bucket   string  `json:"bucket"`
	BytesIn  int64   `json:"bytes_in"`
	BytesOut int64   `json:"bytes_out"`
	Cost     float64 `json:"cost"`
}

type topSessionResp struct {
	SessionID   string `json:"session_id"`
	StartedAt   string `json:"started_at"`
	BytesTotal  int64  `json:"bytes_total"`
	DurationSec int    `json:"duration_sec"`
}

func (h *Handler) GetUsage(w http.ResponseWriter, r *http.Request) {
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

	if h.cdrStore == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Usage data unavailable")
		return
	}

	if _, err := h.simStore.GetByID(r.Context(), tenantID, simID); err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", idStr).Msg("get sim for usage")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	period := r.URL.Query().Get("period")
	validPeriods := map[string]bool{"24h": true, "7d": true, "30d": true}
	if !validPeriods[period] {
		period = "30d"
	}

	result, err := h.cdrStore.GetSIMUsage(r.Context(), tenantID, simID, period)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", idStr).Str("period", period).Msg("get sim usage")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	series := make([]usageBucketResp, 0, len(result.Series))
	for _, b := range result.Series {
		series = append(series, usageBucketResp{
			Bucket:   b.Bucket.Format(time.RFC3339),
			BytesIn:  b.BytesIn,
			BytesOut: b.BytesOut,
			Cost:     b.Cost,
		})
	}

	topSessions := make([]topSessionResp, 0, len(result.TopSessions))
	for _, t := range result.TopSessions {
		topSessions = append(topSessions, topSessionResp{
			SessionID:   t.SessionID.String(),
			StartedAt:   t.StartedAt.Format(time.RFC3339),
			BytesTotal:  t.BytesTotal,
			DurationSec: t.DurationSec,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, usageResponse{
		SimID:         result.SimID.String(),
		Period:        result.Period,
		TotalBytesIn:  result.TotalBytesIn,
		TotalBytesOut: result.TotalBytesOut,
		TotalCost:     result.TotalCost,
		Series:        series,
		TopSessions:   topSessions,
	})
}
