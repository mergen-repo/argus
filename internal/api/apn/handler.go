package apn

import (
	"encoding/json"
	"errors"
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

var validAPNTypes = map[string]bool{
	"private_managed":  true,
	"operator_managed": true,
	"customer_managed": true,
}

var validRATTypes = map[string]bool{
	"nb_iot": true,
	"lte_m":  true,
	"lte":    true,
	"nr_5g":  true,
}

type Handler struct {
	apnStore      *store.APNStore
	operatorStore *store.OperatorStore
	auditSvc      audit.Auditor
	logger        zerolog.Logger
}

func NewHandler(
	apnStore *store.APNStore,
	operatorStore *store.OperatorStore,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		apnStore:      apnStore,
		operatorStore: operatorStore,
		auditSvc:      auditSvc,
		logger:        logger.With().Str("component", "apn_handler").Logger(),
	}
}

type apnResponse struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	OperatorID        string          `json:"operator_id"`
	Name              string          `json:"name"`
	DisplayName       *string         `json:"display_name"`
	APNType           string          `json:"apn_type"`
	SupportedRATTypes []string        `json:"supported_rat_types"`
	DefaultPolicyID   *string         `json:"default_policy_id,omitempty"`
	State             string          `json:"state"`
	Settings          json.RawMessage `json:"settings"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	CreatedBy         *string         `json:"created_by,omitempty"`
	UpdatedBy         *string         `json:"updated_by,omitempty"`
}

type createAPNRequest struct {
	Name              string          `json:"name"`
	OperatorID        string          `json:"operator_id"`
	APNType           string          `json:"apn_type"`
	SupportedRATTypes []string        `json:"supported_rat_types"`
	DisplayName       *string         `json:"display_name"`
	DefaultPolicyID   *string         `json:"default_policy_id"`
	Settings          json.RawMessage `json:"settings"`
}

type updateAPNRequest struct {
	DisplayName       *string         `json:"display_name"`
	SupportedRATTypes []string        `json:"supported_rat_types"`
	DefaultPolicyID   *string         `json:"default_policy_id"`
	Settings          json.RawMessage `json:"settings"`
}

func toAPNResponse(a *store.APN) apnResponse {
	rats := a.SupportedRATTypes
	if rats == nil {
		rats = []string{}
	}
	resp := apnResponse{
		ID:                a.ID.String(),
		TenantID:          a.TenantID.String(),
		OperatorID:        a.OperatorID.String(),
		Name:              a.Name,
		DisplayName:       a.DisplayName,
		APNType:           a.APNType,
		SupportedRATTypes: rats,
		State:             a.State,
		Settings:          a.Settings,
		CreatedAt:         a.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:         a.UpdatedAt.Format(time.RFC3339Nano),
	}
	if a.DefaultPolicyID != nil {
		s := a.DefaultPolicyID.String()
		resp.DefaultPolicyID = &s
	}
	if a.CreatedBy != nil {
		s := a.CreatedBy.String()
		resp.CreatedBy = &s
	}
	if a.UpdatedBy != nil {
		s := a.UpdatedBy.String()
		resp.UpdatedBy = &s
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
	stateFilter := r.URL.Query().Get("state")
	operatorIDStr := r.URL.Query().Get("operator_id")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	var operatorIDFilter *uuid.UUID
	if operatorIDStr != "" {
		parsed, err := uuid.Parse(operatorIDStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
			return
		}
		operatorIDFilter = &parsed
	}

	apns, nextCursor, err := h.apnStore.List(r.Context(), tenantID, cursor, limit, stateFilter, operatorIDFilter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list apns")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]apnResponse, 0, len(apns))
	for _, a := range apns {
		items = append(items, toAPNResponse(&a))
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

	var req createAPNRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	} else if len(req.Name) > 100 {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name must be at most 100 characters", "code": "max_length"})
	}
	if req.OperatorID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "operator_id", "message": "Operator ID is required", "code": "required"})
	}
	if req.APNType == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "apn_type", "message": "APN type is required", "code": "required"})
	} else if !validAPNTypes[req.APNType] {
		validationErrors = append(validationErrors, map[string]string{"field": "apn_type", "message": "Invalid APN type. Allowed: private_managed, operator_managed, customer_managed", "code": "invalid_enum"})
	}
	for _, rat := range req.SupportedRATTypes {
		if !validRATTypes[rat] {
			validationErrors = append(validationErrors, map[string]string{"field": "supported_rat_types", "message": "Invalid RAT type: " + rat + ". Allowed: nb_iot, lte_m, lte, nr_5g", "code": "invalid_enum"})
			break
		}
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

	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Msg("get operator for apn create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	grants, err := h.operatorStore.ListGrants(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list grants for apn create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	granted := false
	for _, g := range grants {
		if g.OperatorID == operatorID && g.Enabled {
			granted = true
			break
		}
	}
	if !granted {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Operator is not granted to this tenant")
		return
	}

	var defaultPolicyID *uuid.UUID
	if req.DefaultPolicyID != nil && *req.DefaultPolicyID != "" {
		parsed, err := uuid.Parse(*req.DefaultPolicyID)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid default_policy_id format")
			return
		}
		defaultPolicyID = &parsed
	}

	userID := userIDFromContext(r)

	a, err := h.apnStore.Create(r.Context(), tenantID, store.CreateAPNParams{
		Name:              req.Name,
		OperatorID:        operatorID,
		APNType:           req.APNType,
		SupportedRATTypes: req.SupportedRATTypes,
		DisplayName:       req.DisplayName,
		DefaultPolicyID:   defaultPolicyID,
		Settings:          req.Settings,
		CreatedBy:         userID,
	})
	if err != nil {
		if errors.Is(err, store.ErrAPNNameExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"An APN with this name already exists for this operator",
				[]map[string]string{{"field": "name", "value": req.Name}})
			return
		}
		h.logger.Error().Err(err).Msg("create apn")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "apn.create", a.ID.String(), nil, a)

	apierr.WriteSuccess(w, http.StatusCreated, toAPNResponse(a))
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
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid APN ID format")
		return
	}

	a, err := h.apnStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Str("apn_id", idStr).Msg("get apn")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toAPNResponse(a))
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
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid APN ID format")
		return
	}

	var req updateAPNRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	for _, rat := range req.SupportedRATTypes {
		if !validRATTypes[rat] {
			validationErrors = append(validationErrors, map[string]string{"field": "supported_rat_types", "message": "Invalid RAT type: " + rat, "code": "invalid_enum"})
			break
		}
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	existing, err := h.apnStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Str("apn_id", idStr).Msg("get apn for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var defaultPolicyID *uuid.UUID
	if req.DefaultPolicyID != nil && *req.DefaultPolicyID != "" {
		parsed, err := uuid.Parse(*req.DefaultPolicyID)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid default_policy_id format")
			return
		}
		defaultPolicyID = &parsed
	}

	userID := userIDFromContext(r)

	updated, err := h.apnStore.Update(r.Context(), tenantID, id, store.UpdateAPNParams{
		DisplayName:       req.DisplayName,
		SupportedRATTypes: req.SupportedRATTypes,
		DefaultPolicyID:   defaultPolicyID,
		Settings:          req.Settings,
		UpdatedBy:         userID,
	})
	if err != nil {
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Str("apn_id", idStr).Msg("update apn")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "apn.update", id.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toAPNResponse(updated))
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid APN ID format")
		return
	}

	existing, err := h.apnStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Str("apn_id", idStr).Msg("get apn for archive")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.apnStore.Archive(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, store.ErrAPNHasActiveSIMs) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeAPNHasActiveSIMs,
				"Cannot archive APN with active SIMs. Remove or reassign SIMs first.")
			return
		}
		if errors.Is(err, store.ErrAPNNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "APN not found")
			return
		}
		h.logger.Error().Err(err).Str("apn_id", idStr).Msg("archive apn")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "apn.archive", id.String(), existing, nil)

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
		EntityType:    "apn",
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
