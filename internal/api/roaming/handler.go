package roaming

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	store         *store.RoamingAgreementStore
	operatorStore *store.OperatorStore
	auditSvc      audit.Auditor
	logger        zerolog.Logger
}

func NewHandler(
	store *store.RoamingAgreementStore,
	operatorStore *store.OperatorStore,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		store:         store,
		operatorStore: operatorStore,
		auditSvc:      auditSvc,
		logger:        logger.With().Str("component", "roaming_handler").Logger(),
	}
}

type agreementResponse struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"tenant_id"`
	OperatorID          string          `json:"operator_id"`
	PartnerOperatorName string          `json:"partner_operator_name"`
	AgreementType       string          `json:"agreement_type"`
	SLATerms            json.RawMessage `json:"sla_terms"`
	CostTerms           json.RawMessage `json:"cost_terms"`
	StartDate           string          `json:"start_date"`
	EndDate             string          `json:"end_date"`
	AutoRenew           bool            `json:"auto_renew"`
	State               string          `json:"state"`
	Notes               *string         `json:"notes,omitempty"`
	TerminatedAt        *string         `json:"terminated_at,omitempty"`
	CreatedBy           *string         `json:"created_by,omitempty"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`
}

func toResponse(a *store.RoamingAgreement) agreementResponse {
	resp := agreementResponse{
		ID:                  a.ID.String(),
		TenantID:            a.TenantID.String(),
		OperatorID:          a.OperatorID.String(),
		PartnerOperatorName: a.PartnerOperatorName,
		AgreementType:       a.AgreementType,
		SLATerms:            a.SLATerms,
		CostTerms:           a.CostTerms,
		StartDate:           a.StartDate.Format("2006-01-02"),
		EndDate:             a.EndDate.Format("2006-01-02"),
		AutoRenew:           a.AutoRenew,
		State:               a.State,
		Notes:               a.Notes,
		CreatedAt:           a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:           a.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if a.TerminatedAt != nil {
		s := a.TerminatedAt.UTC().Format(time.RFC3339)
		resp.TerminatedAt = &s
	}
	if a.CreatedBy != nil {
		s := a.CreatedBy.String()
		resp.CreatedBy = &s
	}
	return resp
}

type createRequest struct {
	OperatorID          string          `json:"operator_id"`
	PartnerOperatorName string          `json:"partner_operator_name"`
	AgreementType       string          `json:"agreement_type"`
	SLATerms            json.RawMessage `json:"sla_terms"`
	CostTerms           json.RawMessage `json:"cost_terms"`
	StartDate           string          `json:"start_date"`
	EndDate             string          `json:"end_date"`
	AutoRenew           bool            `json:"auto_renew"`
	State               string          `json:"state"`
	Notes               *string         `json:"notes"`
}

type updateRequest struct {
	PartnerOperatorName *string         `json:"partner_operator_name"`
	AgreementType       *string         `json:"agreement_type"`
	SLATerms            json.RawMessage `json:"sla_terms"`
	CostTerms           json.RawMessage `json:"cost_terms"`
	StartDate           *string         `json:"start_date"`
	EndDate             *string         `json:"end_date"`
	AutoRenew           *bool           `json:"auto_renew"`
	State               *string         `json:"state"`
	Notes               *string         `json:"notes"`
}

var validAgreementTypes = map[string]bool{
	"national":      true,
	"international": true,
	"MVNO":          true,
}

var validStates = map[string]bool{
	"draft":      true,
	"active":     true,
	"expired":    true,
	"terminated": true,
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	f := store.ListRoamingAgreementsFilter{
		Cursor: cursor,
		Limit:  limit,
		State:  r.URL.Query().Get("state"),
	}

	if opIDStr := r.URL.Query().Get("operator_id"); opIDStr != "" {
		if opID, err := uuid.Parse(opIDStr); err == nil {
			f.OperatorID = &opID
		}
	}

	if expiringStr := r.URL.Query().Get("expiring_within_days"); expiringStr != "" {
		if days, err := strconv.Atoi(expiringStr); err == nil && days > 0 {
			f.ExpiringWithinDays = &days
		}
	}

	agreements, nextCursor, err := h.store.List(r.Context(), tenantID, f)
	if err != nil {
		h.logger.Error().Err(err).Msg("list roaming agreements")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := make([]agreementResponse, 0, len(agreements))
	for i := range agreements {
		resp = append(resp, toResponse(&agreements[i]))
	}

	apierr.WriteList(w, http.StatusOK, resp, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	type validationError struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}
	var validationErrors []validationError

	if req.PartnerOperatorName == "" {
		validationErrors = append(validationErrors, validationError{Field: "partner_operator_name", Message: "required"})
	}
	if req.OperatorID == "" {
		validationErrors = append(validationErrors, validationError{Field: "operator_id", Message: "required"})
	}
	if !validAgreementTypes[req.AgreementType] {
		validationErrors = append(validationErrors, validationError{Field: "agreement_type", Message: "must be national, international, or MVNO"})
	}
	if req.StartDate == "" {
		validationErrors = append(validationErrors, validationError{Field: "start_date", Message: "required"})
	}
	if req.EndDate == "" {
		validationErrors = append(validationErrors, validationError{Field: "end_date", Message: "required"})
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

	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeRoamingAgreementInvalidDates, "Invalid start_date format (expected YYYY-MM-DD)")
		return
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeRoamingAgreementInvalidDates, "Invalid end_date format (expected YYYY-MM-DD)")
		return
	}
	if !endDate.After(startDate) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeRoamingAgreementInvalidDates, "end_date must be after start_date")
		return
	}

	if err := h.validateCostTerms(req.CostTerms); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, err.Error())
		return
	}

	if _, err := h.operatorStore.GetByID(r.Context(), operatorID); err != nil {
		if errors.Is(err, store.ErrOperatorNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Operator not found")
			return
		}
		h.logger.Error().Err(err).Msg("get operator for roaming agreement create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	grants, err := h.operatorStore.ListGrants(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list grants for roaming agreement create")
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
		apierr.WriteError(w, http.StatusConflict, apierr.CodeRoamingAgreementOperatorNotGranted, "Operator is not granted to this tenant")
		return
	}

	state := req.State
	if state == "" {
		state = "draft"
	}

	userID := userIDFromContext(r)

	a, err := h.store.Create(r.Context(), tenantID, store.CreateRoamingAgreementParams{
		OperatorID:          operatorID,
		PartnerOperatorName: req.PartnerOperatorName,
		AgreementType:       req.AgreementType,
		SLATerms:            req.SLATerms,
		CostTerms:           req.CostTerms,
		StartDate:           startDate,
		EndDate:             endDate,
		AutoRenew:           req.AutoRenew,
		State:               state,
		Notes:               req.Notes,
		CreatedBy:           userID,
	})
	if err != nil {
		if errors.Is(err, store.ErrRoamingAgreementOverlap) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeRoamingAgreementOverlap,
				"An active roaming agreement already exists for this operator")
			return
		}
		h.logger.Error().Err(err).Msg("create roaming agreement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "roaming_agreement.create", a.ID.String(), nil, a)
	apierr.WriteSuccess(w, http.StatusCreated, toResponse(a))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid agreement id")
		return
	}

	a, err := h.store.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrRoamingAgreementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeRoamingAgreementNotFound, "Roaming agreement not found")
			return
		}
		h.logger.Error().Err(err).Msg("get roaming agreement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toResponse(a))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid agreement id")
		return
	}

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid request body")
		return
	}

	if req.AgreementType != nil && !validAgreementTypes[*req.AgreementType] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "agreement_type must be national, international, or MVNO")
		return
	}
	if req.State != nil && !validStates[*req.State] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "invalid state value")
		return
	}

	existing, err := h.store.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrRoamingAgreementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeRoamingAgreementNotFound, "Roaming agreement not found")
			return
		}
		h.logger.Error().Err(err).Msg("get roaming agreement for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	params := store.UpdateRoamingAgreementParams{
		PartnerOperatorName: req.PartnerOperatorName,
		AgreementType:       req.AgreementType,
		SLATerms:            req.SLATerms,
		CostTerms:           req.CostTerms,
		AutoRenew:           req.AutoRenew,
		State:               req.State,
		Notes:               req.Notes,
	}

	if req.StartDate != nil {
		sd, err := time.Parse("2006-01-02", *req.StartDate)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeRoamingAgreementInvalidDates, "Invalid start_date format")
			return
		}
		params.StartDate = &sd
	}
	if req.EndDate != nil {
		ed, err := time.Parse("2006-01-02", *req.EndDate)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeRoamingAgreementInvalidDates, "Invalid end_date format")
			return
		}
		params.EndDate = &ed
	}

	if len(req.CostTerms) > 0 {
		if err := h.validateCostTerms(req.CostTerms); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, err.Error())
			return
		}
	}

	updated, err := h.store.Update(r.Context(), tenantID, id, params)
	if err != nil {
		if errors.Is(err, store.ErrRoamingAgreementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeRoamingAgreementNotFound, "Roaming agreement not found")
			return
		}
		if errors.Is(err, store.ErrRoamingAgreementOverlap) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeRoamingAgreementOverlap,
				"An active roaming agreement already exists for this operator")
			return
		}
		h.logger.Error().Err(err).Msg("update roaming agreement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "roaming_agreement.update", id.String(), existing, updated)
	apierr.WriteSuccess(w, http.StatusOK, toResponse(updated))
}

func (h *Handler) Terminate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid agreement id")
		return
	}

	existing, err := h.store.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrRoamingAgreementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeRoamingAgreementNotFound, "Roaming agreement not found")
			return
		}
		h.logger.Error().Err(err).Msg("get roaming agreement for terminate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.store.Terminate(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, store.ErrRoamingAgreementNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeRoamingAgreementNotFound, "Roaming agreement not found")
			return
		}
		h.logger.Error().Err(err).Msg("terminate roaming agreement")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "roaming_agreement.terminate", id.String(), existing, nil)
	apierr.WriteSuccess(w, http.StatusOK, map[string]string{"status": "terminated"})
}

func (h *Handler) ListForOperator(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	operatorID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator id")
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	agreements, nextCursor, err := h.store.ListByOperator(r.Context(), tenantID, operatorID, cursor, limit)
	if err != nil {
		h.logger.Error().Err(err).Msg("list roaming agreements for operator")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := make([]agreementResponse, 0, len(agreements))
	for i := range agreements {
		resp = append(resp, toResponse(&agreements[i]))
	}

	apierr.WriteList(w, http.StatusOK, resp, apierr.ListMeta{
		Cursor:  nextCursor,
		HasMore: nextCursor != "",
		Limit:   limit,
	})
}

func (h *Handler) validateCostTerms(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var ct store.CostTerms
	if err := json.Unmarshal(raw, &ct); err != nil {
		return errors.New("invalid cost_terms JSON")
	}
	if ct.CostPerMB < 0 {
		return errors.New("cost_terms.cost_per_mb must be >= 0")
	}
	if ct.Currency != "" && (len(ct.Currency) != 3 || ct.Currency != strings.ToUpper(ct.Currency)) {
		return errors.New("cost_terms.currency must be a 3-letter ISO 4217 code")
	}
	return nil
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
		EntityType:    "roaming_agreement",
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
