package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/dryrun"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var validScopes = map[string]bool{
	"global":   true,
	"operator": true,
	"apn":      true,
	"sim":      true,
}

var validPolicyStates = map[string]bool{
	"active":   true,
	"disabled": true,
	"archived": true,
}

type Handler struct {
	policyStore *store.PolicyStore
	dryRunSvc   *dryrun.Service
	jobStore    *store.JobStore
	eventBus    *bus.EventBus
	auditSvc    audit.Auditor
	logger      zerolog.Logger
}

func NewHandler(
	policyStore *store.PolicyStore,
	dryRunSvc *dryrun.Service,
	jobStore *store.JobStore,
	eventBus *bus.EventBus,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		policyStore: policyStore,
		dryRunSvc:   dryRunSvc,
		jobStore:    jobStore,
		eventBus:    eventBus,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "policy_handler").Logger(),
	}
}

type policyResponse struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Description      *string             `json:"description"`
	Scope            string              `json:"scope"`
	ScopeRefID       *string             `json:"scope_ref_id,omitempty"`
	CurrentVersionID *string             `json:"current_version_id,omitempty"`
	State            string              `json:"state"`
	CreatedAt        string              `json:"created_at"`
	UpdatedAt        string              `json:"updated_at"`
	Versions         []versionResponse   `json:"versions,omitempty"`
}

type versionResponse struct {
	ID               string          `json:"id"`
	PolicyID         string          `json:"policy_id"`
	Version          int             `json:"version"`
	DSLContent       string          `json:"dsl_content,omitempty"`
	CompiledRules    json.RawMessage `json:"compiled_rules,omitempty"`
	State            string          `json:"state"`
	AffectedSIMCount *int            `json:"affected_sim_count,omitempty"`
	ActivatedAt      *string         `json:"activated_at,omitempty"`
	CreatedAt        string          `json:"created_at"`
}

type policyListItem struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Description      *string `json:"description"`
	Scope            string  `json:"scope"`
	ActiveVersion    *int    `json:"active_version"`
	SimCount         int     `json:"sim_count"`
	CurrentVersionID *string `json:"current_version_id,omitempty"`
	State            string  `json:"state"`
	UpdatedAt        string  `json:"updated_at"`
}

type createPolicyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Scope       string  `json:"scope"`
	ScopeRefID  *string `json:"scope_ref_id"`
	DSLSource   string  `json:"dsl_source"`
}

type updatePolicyRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	State       *string `json:"state"`
}

type createVersionRequest struct {
	DSLSource          string  `json:"dsl_source"`
	CloneFromVersionID *string `json:"clone_from_version_id"`
}

type updateVersionRequest struct {
	DSLSource string `json:"dsl_source"`
}

type diffResponse struct {
	Version1 int      `json:"version_1"`
	Version2 int      `json:"version_2"`
	Lines    []diffLine `json:"lines"`
}

type diffLine struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	LineNum int    `json:"line_num,omitempty"`
}

func toPolicyResponse(p *store.Policy) policyResponse {
	resp := policyResponse{
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		Scope:       p.Scope,
		State:       p.State,
		CreatedAt:   p.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:   p.UpdatedAt.Format(time.RFC3339Nano),
	}
	if p.ScopeRefID != nil {
		s := p.ScopeRefID.String()
		resp.ScopeRefID = &s
	}
	if p.CurrentVersionID != nil {
		s := p.CurrentVersionID.String()
		resp.CurrentVersionID = &s
	}
	return resp
}

func toVersionResponse(v *store.PolicyVersion) versionResponse {
	resp := versionResponse{
		ID:               v.ID.String(),
		PolicyID:         v.PolicyID.String(),
		Version:          v.Version,
		DSLContent:       v.DSLContent,
		CompiledRules:    v.CompiledRules,
		State:            v.State,
		AffectedSIMCount: v.AffectedSIMCount,
		CreatedAt:        v.CreatedAt.Format(time.RFC3339Nano),
	}
	if v.ActivatedAt != nil {
		s := v.ActivatedAt.Format(time.RFC3339Nano)
		resp.ActivatedAt = &s
	}
	return resp
}

func toPolicyListItem(p *store.Policy) policyListItem {
	item := policyListItem{
		ID:          p.ID.String(),
		Name:        p.Name,
		Description: p.Description,
		Scope:       p.Scope,
		State:       p.State,
		UpdatedAt:   p.UpdatedAt.Format(time.RFC3339Nano),
	}
	if p.CurrentVersionID != nil {
		s := p.CurrentVersionID.String()
		item.CurrentVersionID = &s
	}
	return item
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	cursor := r.URL.Query().Get("cursor")
	limitStr := r.URL.Query().Get("limit")
	stateFilter := r.URL.Query().Get("status")
	search := r.URL.Query().Get("q")

	limit := 50
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	policies, nextCursor, err := h.policyStore.List(r.Context(), tenantID, cursor, limit, stateFilter, search)
	if err != nil {
		h.logger.Error().Err(err).Msg("list policies")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]policyListItem, 0, len(policies))
	for _, p := range policies {
		items = append(items, toPolicyListItem(&p))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	var req createPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Name == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "name", "message": "Name is required", "code": "required"})
	}
	if req.Scope == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "scope", "message": "Scope is required", "code": "required"})
	} else if !validScopes[req.Scope] {
		validationErrors = append(validationErrors, map[string]string{"field": "scope", "message": "Invalid scope. Allowed: global, operator, apn, sim", "code": "invalid_enum"})
	}
	if req.DSLSource == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "dsl_source", "message": "DSL source is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	compiled, dslErrors, err := dsl.CompileSource(req.DSLSource)
	if err != nil {
		h.logger.Error().Err(err).Msg("compile DSL source")
		apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL", "DSL compilation failed: "+err.Error())
		return
	}
	for _, e := range dslErrors {
		if e.Severity == "error" {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL",
				fmt.Sprintf("DSL error at line %d: %s", e.Line, e.Message))
			return
		}
	}

	compiledJSON, err := json.Marshal(compiled)
	if err != nil {
		h.logger.Error().Err(err).Msg("marshal compiled rules")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var scopeRefID *uuid.UUID
	if req.ScopeRefID != nil && *req.ScopeRefID != "" {
		parsed, parseErr := uuid.Parse(*req.ScopeRefID)
		if parseErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid scope_ref_id format")
			return
		}
		scopeRefID = &parsed
	}

	userID := userIDFromContext(r)

	policy, version, err := h.policyStore.Create(r.Context(), tenantID, store.CreatePolicyParams{
		Name:          req.Name,
		Description:   req.Description,
		Scope:         req.Scope,
		ScopeRefID:    scopeRefID,
		DSLContent:    req.DSLSource,
		CompiledRules: compiledJSON,
		CreatedBy:     userID,
	})
	if err != nil {
		if errors.Is(err, store.ErrPolicyNameExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists,
				"A policy with this name already exists in this tenant",
				[]map[string]string{{"field": "name", "value": req.Name}})
			return
		}
		h.logger.Error().Err(err).Msg("create policy")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toPolicyResponse(policy)
	resp.Versions = []versionResponse{toVersionResponse(version)}

	h.createAuditEntry(r, "policy.create", policy.ID.String(), nil, resp)

	apierr.WriteSuccess(w, http.StatusCreated, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid policy ID format")
		return
	}

	policy, err := h.policyStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy not found")
			return
		}
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("get policy")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	versions, err := h.policyStore.GetVersionsByPolicyID(r.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("get policy versions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toPolicyResponse(policy)
	resp.Versions = make([]versionResponse, 0, len(versions))
	for _, v := range versions {
		resp.Versions = append(resp.Versions, toVersionResponse(&v))
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid policy ID format")
		return
	}

	var req updatePolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.State != nil && !validPolicyStates[*req.State] {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Invalid state. Allowed: active, disabled, archived",
			[]map[string]string{{"field": "state", "message": "Invalid state", "code": "invalid_enum"}})
		return
	}

	existing, err := h.policyStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy not found")
			return
		}
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("get policy for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	updated, err := h.policyStore.Update(r.Context(), tenantID, id, store.UpdatePolicyParams{
		Name:        req.Name,
		Description: req.Description,
		State:       req.State,
	})
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy not found")
			return
		}
		if errors.Is(err, store.ErrPolicyNameExists) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeAlreadyExists, "A policy with this name already exists")
			return
		}
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("update policy")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "policy.update", id.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toPolicyResponse(updated))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid policy ID format")
		return
	}

	existing, err := h.policyStore.GetByID(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy not found")
			return
		}
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("get policy for delete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if err := h.policyStore.SoftDelete(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, store.ErrPolicyInUse) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "POLICY_IN_USE",
				"Cannot delete policy: SIMs are currently assigned to this policy")
			return
		}
		if errors.Is(err, store.ErrPolicyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy not found")
			return
		}
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("delete policy")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "policy.delete", id.String(), existing, nil)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	policyID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid policy ID format")
		return
	}

	_, err = h.policyStore.GetByID(r.Context(), tenantID, policyID)
	if err != nil {
		if errors.Is(err, store.ErrPolicyNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy not found")
			return
		}
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("get policy for create version")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var req createVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	dslSource := req.DSLSource
	if dslSource == "" && req.CloneFromVersionID != nil && *req.CloneFromVersionID != "" {
		cloneID, parseErr := uuid.Parse(*req.CloneFromVersionID)
		if parseErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid clone_from_version_id format")
			return
		}
		sourceVersion, getErr := h.policyStore.GetVersionByID(r.Context(), cloneID)
		if getErr != nil {
			if errors.Is(getErr, store.ErrPolicyVersionNotFound) {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Source version not found")
				return
			}
			h.logger.Error().Err(getErr).Msg("get source version for clone")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		dslSource = sourceVersion.DSLContent
	}

	if dslSource == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"Either dsl_source or clone_from_version_id is required",
			[]map[string]string{{"field": "dsl_source", "message": "DSL source or clone source is required", "code": "required"}})
		return
	}

	compiled, dslErrors, compileErr := dsl.CompileSource(dslSource)
	if compileErr != nil {
		h.logger.Error().Err(compileErr).Msg("compile DSL for new version")
		apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL", "DSL compilation failed: "+compileErr.Error())
		return
	}
	for _, e := range dslErrors {
		if e.Severity == "error" {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL",
				fmt.Sprintf("DSL error at line %d: %s", e.Line, e.Message))
			return
		}
	}

	compiledJSON, err := json.Marshal(compiled)
	if err != nil {
		h.logger.Error().Err(err).Msg("marshal compiled rules")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromContext(r)

	version, err := h.policyStore.CreateVersion(r.Context(), store.CreateVersionParams{
		PolicyID:      policyID,
		DSLContent:    dslSource,
		CompiledRules: compiledJSON,
		CreatedBy:     userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("create version")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "policy_version.create", version.ID.String(), nil, version)

	apierr.WriteSuccess(w, http.StatusCreated, toVersionResponse(version))
}

func (h *Handler) ActivateVersion(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	versionID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid version ID format")
		return
	}

	existing, err := h.policyStore.GetVersionByID(r.Context(), versionID)
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("get version for activation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if existing.State != "draft" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, "VERSION_NOT_DRAFT",
			"Only draft versions can be activated")
		return
	}

	dslErrors := dsl.Validate(existing.DSLContent)
	for _, e := range dslErrors {
		if e.Severity == "error" {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL",
				fmt.Sprintf("DSL validation failed at line %d: %s", e.Line, e.Message))
			return
		}
	}

	activated, err := h.policyStore.ActivateVersion(r.Context(), versionID)
	if err != nil {
		if errors.Is(err, store.ErrVersionNotDraft) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "VERSION_NOT_DRAFT",
				"Only draft versions can be activated")
			return
		}
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("activate version")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "policy_version.activate", versionID.String(), existing, activated)

	apierr.WriteSuccess(w, http.StatusOK, toVersionResponse(activated))
}

func (h *Handler) UpdateVersion(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	versionID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid version ID format")
		return
	}

	var req updateVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.DSLSource == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "DSL source is required",
			[]map[string]string{{"field": "dsl_source", "message": "DSL source is required", "code": "required"}})
		return
	}

	existing, err := h.policyStore.GetVersionByID(r.Context(), versionID)
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("get version for update")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if existing.State != "draft" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, "VERSION_NOT_DRAFT",
			"Only draft versions can be edited")
		return
	}

	compiled, dslErrors, compileErr := dsl.CompileSource(req.DSLSource)
	if compileErr != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL", "DSL compilation failed: "+compileErr.Error())
		return
	}
	for _, e := range dslErrors {
		if e.Severity == "error" {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL",
				fmt.Sprintf("DSL error at line %d: %s", e.Line, e.Message))
			return
		}
	}

	compiledJSON, err := json.Marshal(compiled)
	if err != nil {
		h.logger.Error().Err(err).Msg("marshal compiled rules")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	updated, err := h.policyStore.UpdateVersion(r.Context(), versionID, req.DSLSource, compiledJSON)
	if err != nil {
		if errors.Is(err, store.ErrVersionNotDraft) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "VERSION_NOT_DRAFT", "Only draft versions can be edited")
			return
		}
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("update version")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "policy_version.update", versionID.String(), existing, updated)

	apierr.WriteSuccess(w, http.StatusOK, toVersionResponse(updated))
}

func (h *Handler) DiffVersions(w http.ResponseWriter, r *http.Request) {
	id1Str := chi.URLParam(r, "id1")
	id2Str := chi.URLParam(r, "id2")

	id1, err := uuid.Parse(id1Str)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid version ID format (id1)")
		return
	}
	id2, err := uuid.Parse(id2Str)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid version ID format (id2)")
		return
	}

	v1, err := h.policyStore.GetVersionByID(r.Context(), id1)
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Version 1 not found")
			return
		}
		h.logger.Error().Err(err).Msg("get version 1 for diff")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	v2, err := h.policyStore.GetVersionByID(r.Context(), id2)
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Version 2 not found")
			return
		}
		h.logger.Error().Err(err).Msg("get version 2 for diff")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	diff := computeDiff(v1.DSLContent, v2.DSLContent)

	resp := diffResponse{
		Version1: v1.Version,
		Version2: v2.Version,
		Lines:    diff,
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func computeDiff(text1, text2 string) []diffLine {
	lines1 := strings.Split(text1, "\n")
	lines2 := strings.Split(text2, "\n")

	var result []diffLine
	max := len(lines1)
	if len(lines2) > max {
		max = len(lines2)
	}

	for i := 0; i < max; i++ {
		var l1, l2 string
		if i < len(lines1) {
			l1 = lines1[i]
		}
		if i < len(lines2) {
			l2 = lines2[i]
		}

		if i >= len(lines1) {
			result = append(result, diffLine{Type: "added", Content: l2, LineNum: i + 1})
		} else if i >= len(lines2) {
			result = append(result, diffLine{Type: "removed", Content: l1, LineNum: i + 1})
		} else if l1 != l2 {
			result = append(result, diffLine{Type: "removed", Content: l1, LineNum: i + 1})
			result = append(result, diffLine{Type: "added", Content: l2, LineNum: i + 1})
		} else {
			result = append(result, diffLine{Type: "unchanged", Content: l1, LineNum: i + 1})
		}
	}

	return result
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
		EntityType:    "policy",
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

type dryRunRequest struct {
	SegmentID *string `json:"segment_id"`
}

type dryRunAsyncResponse struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

func (h *Handler) DryRun(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	versionID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid version ID format")
		return
	}

	var req dryRunRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
			return
		}
	}

	var segmentID *uuid.UUID
	if req.SegmentID != nil && *req.SegmentID != "" {
		parsed, parseErr := uuid.Parse(*req.SegmentID)
		if parseErr != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid segment_id format")
			return
		}
		segmentID = &parsed
	}

	if h.dryRunSvc == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Dry-run service not available")
		return
	}

	count, err := h.dryRunSvc.CountMatchingSIMs(r.Context(), tenantID, versionID, segmentID)
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		if dryrun.IsDSLError(err) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL", err.Error())
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("count matching sims for dry-run")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if count > dryrun.AsyncThreshold() {
		if h.jobStore == nil || h.eventBus == nil {
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Async processing not available")
			return
		}

		payload := map[string]interface{}{
			"version_id": versionID.String(),
			"segment_id": nil,
		}
		if segmentID != nil {
			payload["segment_id"] = segmentID.String()
		}
		payloadJSON, _ := json.Marshal(payload)

		userID := userIDFromContext(r)

		job, createErr := h.jobStore.CreateWithTenantID(r.Context(), tenantID, store.CreateJobParams{
			Type:       "policy_dry_run",
			Priority:   5,
			Payload:    payloadJSON,
			TotalItems: count,
			CreatedBy:  userID,
		})
		if createErr != nil {
			h.logger.Error().Err(createErr).Msg("create dry-run job")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}

		msgData, _ := json.Marshal(map[string]interface{}{
			"job_id":    job.ID.String(),
			"tenant_id": tenantID.String(),
			"type":      "policy_dry_run",
		})
		_ = h.eventBus.PublishRaw(r.Context(), bus.SubjectJobQueue, msgData)

		apierr.WriteSuccess(w, http.StatusAccepted, dryRunAsyncResponse{
			JobID:   job.ID.String(),
			Message: "Dry-run queued for async processing. Use GET /api/v1/jobs/" + job.ID.String() + " to check status.",
		})
		return
	}

	result, err := h.dryRunSvc.Execute(r.Context(), dryrun.DryRunRequest{
		VersionID: versionID,
		TenantID:  tenantID,
		SegmentID: segmentID,
	})
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		if dryrun.IsDSLError(err) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "INVALID_DSL", err.Error())
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("execute dry-run")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}

func userIDFromContext(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
