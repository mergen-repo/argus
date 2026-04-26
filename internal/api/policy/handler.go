package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/analytics/aggregates"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/policy/dryrun"
	"github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	undopkg "github.com/btopcu/argus/internal/undo"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// simCounter is the subset of store.SIMStore needed by this handler.
// Defined as an interface to allow mock substitution in tests.
type simCounter interface {
	CountWithPredicate(ctx context.Context, tenantID uuid.UUID, dslPredicate string, dslArgs []interface{}) (int, error)
}

// rolloutServicer is the subset of rollout.Service methods used by this handler.
// Defined as an interface to allow mock substitution in tests.
type rolloutServicer interface {
	StartRollout(ctx context.Context, tenantID, versionID uuid.UUID, stagePcts []int, createdBy *uuid.UUID) (*store.PolicyRollout, error)
	AdvanceRollout(ctx context.Context, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error)
	RollbackRollout(ctx context.Context, tenantID, rolloutID uuid.UUID, reason string) (*store.PolicyRollout, int, error)
	AbortRollout(ctx context.Context, tenantID, rolloutID uuid.UUID, reason string) (*store.PolicyRollout, error)
	GetProgress(ctx context.Context, tenantID, rolloutID uuid.UUID) (*store.PolicyRollout, error)
}

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
	policyStore  *store.PolicyStore
	dryRunSvc    *dryrun.Service
	rolloutSvc   rolloutServicer
	jobStore     *store.JobStore
	eventBus     *bus.EventBus
	auditSvc     audit.Auditor
	undoRegistry *undopkg.Registry
	agg          aggregates.Aggregates
	simStore     simCounter
	logger       zerolog.Logger
}

type HandlerOption func(*Handler)

func WithAggregates(a aggregates.Aggregates) HandlerOption {
	return func(h *Handler) { h.agg = a }
}

func WithSIMStore(s simCounter) HandlerOption {
	return func(h *Handler) { h.simStore = s }
}

func NewHandler(
	policyStore *store.PolicyStore,
	dryRunSvc *dryrun.Service,
	rolloutSvc rolloutServicer,
	jobStore *store.JobStore,
	eventBus *bus.EventBus,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		policyStore: policyStore,
		dryRunSvc:   dryRunSvc,
		rolloutSvc:  rolloutSvc,
		jobStore:    jobStore,
		eventBus:    eventBus,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "policy_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) WithUndoRegistry(r *undopkg.Registry) *Handler {
	h.undoRegistry = r
	return h
}

type policyResponse struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      *string           `json:"description"`
	Scope            string            `json:"scope"`
	ScopeRefID       *string           `json:"scope_ref_id,omitempty"`
	CurrentVersionID *string           `json:"current_version_id,omitempty"`
	State            string            `json:"state"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	Versions         []versionResponse `json:"versions,omitempty"`
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
	Version1 int        `json:"version_1"`
	Version2 int        `json:"version_2"`
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
		item := toPolicyListItem(&p)
		if h.agg != nil && tenantID != uuid.Nil {
			if cnt, cntErr := h.agg.SIMCountByPolicy(r.Context(), tenantID, p.ID); cntErr == nil {
				item.SimCount = cnt
			} else {
				h.logger.Warn().Err(cntErr).Str("policy_id", p.ID.String()).Msg("sim count by policy")
			}
		}
		items = append(items, item)
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
	meta := map[string]string{}
	if h.undoRegistry != nil {
		userID := userIDFromContext(r)
		if userID != nil {
			actionID, regErr := h.undoRegistry.Register(r.Context(), tenantID, *userID, "policy_restore", map[string]string{
				"policy_id": id.String(),
				"state":     existing.State,
			})
			if regErr != nil {
				h.logger.Warn().Err(regErr).Str("policy_id", id.String()).Msg("register undo for policy delete")
			} else {
				meta["undo_action_id"] = actionID
			}
		}
	}

	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data:   map[string]bool{"deleted": true},
		Meta:   meta,
	})
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

	// FIX-230 AC-4: auto-populate affected_sim_count via DSL predicate.
	// Empty match → predicate="TRUE" → counts all active tenant SIMs (AC-5).
	predicate, predArgs, _, predErr := dsl.ToSQLPredicate(&compiled.Match, 1, 2)
	if predErr != nil {
		h.logger.Error().Err(predErr).Msg("dsl predicate translation failed")
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
			"policy match clause: "+predErr.Error())
		return
	}

	var affectedSIMCount *int
	countWarning := false
	if h.simStore != nil {
		count, countErr := h.simStore.CountWithPredicate(r.Context(), tenantID, predicate, predArgs)
		if countErr != nil {
			h.logger.Error().Err(countErr).Msg("count sims with predicate failed")
			// FIX-230 Gate F-A2: non-fatal — version created with nil
			// affected_sim_count; rollout will recompute. Surface a warning
			// in the response meta so callers can distinguish "count pending"
			// from "MATCH genuinely matches zero SIMs".
			countWarning = true
		} else {
			affectedSIMCount = &count
		}
	}

	userID := userIDFromContext(r)

	version, err := h.policyStore.CreateVersion(r.Context(), store.CreateVersionParams{
		PolicyID:         policyID,
		DSLContent:       dslSource,
		CompiledRules:    compiledJSON,
		CreatedBy:        userID,
		AffectedSIMCount: affectedSIMCount,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("policy_id", idStr).Msg("create version")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "policy_version.create", version.ID.String(), nil, version)

	if countWarning {
		apierr.WriteJSON(w, http.StatusCreated, apierr.SuccessResponse{
			Status: "success",
			Data:   toVersionResponse(version),
			Meta: map[string]interface{}{
				"warnings": []string{"affected_sim_count_pending"},
			},
		})
		return
	}
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
	a := strings.Split(text1, "\n")
	b := strings.Split(text2, "\n")

	lcs := lcsTable(a, b)
	var result []diffLine
	buildDiff(a, b, lcs, len(a), len(b), &result)
	return result
}

func lcsTable(a, b []string) [][]int {
	m, n := len(a), len(b)
	table := make([][]int, m+1)
	for i := range table {
		table[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				table[i][j] = table[i-1][j-1] + 1
			} else if table[i-1][j] >= table[i][j-1] {
				table[i][j] = table[i-1][j]
			} else {
				table[i][j] = table[i][j-1]
			}
		}
	}
	return table
}

func buildDiff(a, b []string, lcs [][]int, i, j int, result *[]diffLine) {
	if i > 0 && j > 0 && a[i-1] == b[j-1] {
		buildDiff(a, b, lcs, i-1, j-1, result)
		*result = append(*result, diffLine{Type: "unchanged", Content: a[i-1], LineNum: i})
	} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
		buildDiff(a, b, lcs, i, j-1, result)
		*result = append(*result, diffLine{Type: "added", Content: b[j-1], LineNum: j})
	} else if i > 0 && (j == 0 || lcs[i][j-1] < lcs[i-1][j]) {
		buildDiff(a, b, lcs, i-1, j, result)
		*result = append(*result, diffLine{Type: "removed", Content: a[i-1], LineNum: i})
	}
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

type startRolloutRequest struct {
	Stages []int `json:"stages"`
}

type rolloutResponse struct {
	RolloutID         string          `json:"rollout_id"`
	VersionID         string          `json:"version_id"`
	PolicyID          string          `json:"policy_id,omitempty"`
	PreviousVersionID *string         `json:"previous_version_id,omitempty"`
	Stages            json.RawMessage `json:"stages"`
	CurrentStage      int             `json:"current_stage"`
	TotalSIMs         int             `json:"total_sims"`
	MigratedSIMs      int             `json:"migrated_sims"`
	Errors            []string        `json:"errors"`
	State             string          `json:"state"`
	StartedAt         *string         `json:"started_at,omitempty"`
	CompletedAt       *string         `json:"completed_at,omitempty"`
	RolledBackAt      *string         `json:"rolled_back_at,omitempty"`
	AbortedAt         *string         `json:"aborted_at,omitempty"`
	CreatedAt         string          `json:"created_at"`
	CoaCounts         map[string]int  `json:"coa_counts,omitempty"`
}

type advanceResponse struct {
	RolloutID       string `json:"rollout_id"`
	CurrentStagePct int    `json:"current_stage_pct"`
	MigratedCount   int    `json:"migrated_count"`
	TotalCount      int    `json:"total_count"`
	State           string `json:"state"`
}

type rollbackRequest struct {
	Reason *string `json:"reason"`
}

type rollbackResponse struct {
	RolloutID     string  `json:"rollout_id"`
	State         string  `json:"state"`
	RevertedCount int     `json:"reverted_count"`
	RolledBackAt  *string `json:"rolled_back_at,omitempty"`
}

type abortRequest struct {
	Reason *string `json:"reason,omitempty"`
}

type abortResponse struct {
	Status string          `json:"status"`
	Data   rolloutResponse `json:"data"`
}

func toRolloutResponse(r *store.PolicyRollout) rolloutResponse {
	resp := rolloutResponse{
		RolloutID:    r.ID.String(),
		VersionID:    r.PolicyVersionID.String(),
		Stages:       r.Stages,
		CurrentStage: r.CurrentStage,
		TotalSIMs:    r.TotalSIMs,
		MigratedSIMs: r.MigratedSIMs,
		Errors:       []string{},
		State:        r.State,
		CreatedAt:    r.CreatedAt.Format(time.RFC3339Nano),
	}
	if r.PreviousVersionID != nil {
		s := r.PreviousVersionID.String()
		resp.PreviousVersionID = &s
	}
	if r.StartedAt != nil {
		s := r.StartedAt.Format(time.RFC3339Nano)
		resp.StartedAt = &s
	}
	if r.CompletedAt != nil {
		s := r.CompletedAt.Format(time.RFC3339Nano)
		resp.CompletedAt = &s
	}
	if r.RolledBackAt != nil {
		s := r.RolledBackAt.Format(time.RFC3339Nano)
		resp.RolledBackAt = &s
	}
	if r.AbortedAt != nil {
		s := r.AbortedAt.Format(time.RFC3339Nano)
		resp.AbortedAt = &s
	}
	return resp
}

func (h *Handler) StartRollout(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	versionID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid version ID format")
		return
	}

	if h.rolloutSvc == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Rollout service not available")
		return
	}

	var req startRolloutRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
			return
		}
	}

	if len(req.Stages) > 0 {
		for i, pct := range req.Stages {
			if pct < 1 || pct > 100 {
				apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
					fmt.Sprintf("Stage percentage must be between 1 and 100, got %d", pct))
				return
			}
			if i > 0 && pct <= req.Stages[i-1] {
				apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
					"Stage percentages must be in ascending order")
				return
			}
		}
		if req.Stages[len(req.Stages)-1] != 100 {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError,
				"Last stage must be 100%")
			return
		}
	}

	userID := userIDFromContext(r)
	ro, err := h.rolloutSvc.StartRollout(r.Context(), tenantID, versionID, req.Stages, userID)
	if err != nil {
		if errors.Is(err, store.ErrPolicyVersionNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Policy version not found")
			return
		}
		if errors.Is(err, store.ErrVersionNotDraft) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "VERSION_NOT_DRAFT",
				"Only draft versions can start a rollout")
			return
		}
		if errors.Is(err, store.ErrRolloutInProgress) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_IN_PROGRESS",
				"A rollout is already in progress for this policy")
			return
		}
		h.logger.Error().Err(err).Str("version_id", idStr).Msg("start rollout")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toRolloutResponse(ro)
	if h.policyStore != nil {
		if policyID, pErr := h.policyStore.GetPolicyIDForRollout(r.Context(), ro.ID); pErr == nil {
			resp.PolicyID = policyID.String()
		}
	}
	h.createAuditEntry(r, "policy_rollout.start", ro.ID.String(), nil, resp)
	apierr.WriteSuccess(w, http.StatusCreated, resp)
}

func (h *Handler) AdvanceRollout(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	rolloutID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid rollout ID format")
		return
	}

	if h.rolloutSvc == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Rollout service not available")
		return
	}

	ro, err := h.rolloutSvc.AdvanceRollout(r.Context(), tenantID, rolloutID)
	if err != nil {
		if errors.Is(err, store.ErrRolloutNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Rollout not found")
			return
		}
		if errors.Is(err, store.ErrRolloutCompleted) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_COMPLETED",
				"Rollout already completed")
			return
		}
		if errors.Is(err, store.ErrRolloutRolledBack) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_ROLLED_BACK",
				"Rollout was already rolled back")
			return
		}
		if errors.Is(err, store.ErrStageInProgress) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "STAGE_IN_PROGRESS",
				"Current stage is still processing")
			return
		}
		h.logger.Error().Err(err).Str("rollout_id", idStr).Msg("advance rollout")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var currentStagePct int
	var stages []store.RolloutStage
	if err := json.Unmarshal(ro.Stages, &stages); err == nil && ro.CurrentStage < len(stages) {
		currentStagePct = stages[ro.CurrentStage].Pct
	}

	resp := advanceResponse{
		RolloutID:       ro.ID.String(),
		CurrentStagePct: currentStagePct,
		MigratedCount:   ro.MigratedSIMs,
		TotalCount:      ro.TotalSIMs,
		State:           ro.State,
	}

	h.createAuditEntry(r, "policy_rollout.advance", ro.ID.String(), nil, resp)
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) RollbackRollout(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	rolloutID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid rollout ID format")
		return
	}

	if h.rolloutSvc == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Rollout service not available")
		return
	}

	var req rollbackRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
			return
		}
	}

	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}

	ro, revertedCount, err := h.rolloutSvc.RollbackRollout(r.Context(), tenantID, rolloutID, reason)
	if err != nil {
		if errors.Is(err, store.ErrRolloutNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Rollout not found")
			return
		}
		if errors.Is(err, store.ErrRolloutCompleted) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_COMPLETED",
				"Cannot rollback a completed rollout")
			return
		}
		if errors.Is(err, store.ErrRolloutRolledBack) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_ROLLED_BACK",
				"Rollout was already rolled back")
			return
		}
		h.logger.Error().Err(err).Str("rollout_id", idStr).Msg("rollback rollout")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var rolledBackAt *string
	if ro.RolledBackAt != nil {
		s := ro.RolledBackAt.Format(time.RFC3339Nano)
		rolledBackAt = &s
	}

	resp := rollbackResponse{
		RolloutID:     ro.ID.String(),
		State:         ro.State,
		RevertedCount: revertedCount,
		RolledBackAt:  rolledBackAt,
	}

	h.createAuditEntry(r, "policy_rollout.rollback", ro.ID.String(), nil, resp)
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) AbortRollout(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	rolloutID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid rollout ID format")
		return
	}

	if h.rolloutSvc == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Rollout service not available")
		return
	}

	var req abortRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
			return
		}
	}

	reason := ""
	if req.Reason != nil {
		if len(*req.Reason) > 500 {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Reason must not exceed 500 characters")
			return
		}
		reason = *req.Reason
	}

	ro, err := h.rolloutSvc.AbortRollout(r.Context(), tenantID, rolloutID, reason)
	if err != nil {
		if errors.Is(err, store.ErrRolloutNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Rollout not found")
			return
		}
		if errors.Is(err, store.ErrRolloutCompleted) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_COMPLETED",
				"Cannot abort a completed rollout")
			return
		}
		if errors.Is(err, store.ErrRolloutRolledBack) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_ROLLED_BACK",
				"Cannot abort a rolled back rollout")
			return
		}
		if errors.Is(err, store.ErrRolloutAborted) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "ROLLOUT_ABORTED",
				"Rollout was already aborted")
			return
		}
		h.logger.Error().Err(err).Str("rollout_id", idStr).Msg("abort rollout")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toRolloutResponse(ro)
	if h.policyStore != nil {
		if policyID, pErr := h.policyStore.GetPolicyIDForRollout(r.Context(), ro.ID); pErr == nil {
			resp.PolicyID = policyID.String()
		}
	}
	h.createAuditEntry(r, "policy_rollout.abort", ro.ID.String(), nil, resp)
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) GetRollout(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	idStr := chi.URLParam(r, "id")
	rolloutID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid rollout ID format")
		return
	}

	if h.rolloutSvc == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Rollout service not available")
		return
	}

	ro, err := h.rolloutSvc.GetProgress(r.Context(), tenantID, rolloutID)
	if err != nil {
		if errors.Is(err, store.ErrRolloutNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Rollout not found")
			return
		}
		h.logger.Error().Err(err).Str("rollout_id", idStr).Msg("get rollout")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	resp := toRolloutResponse(ro)
	if h.policyStore != nil {
		if policyID, err := h.policyStore.GetPolicyIDForRollout(r.Context(), rolloutID); err == nil {
			s := policyID.String()
			resp.PolicyID = s
		}
		counts, cErr := h.policyStore.GetCoAStatusCountsByRollout(r.Context(), rolloutID)
		if cErr != nil {
			h.logger.Warn().Err(cErr).Str("rollout_id", rolloutID.String()).Msg("coa counts fetch failed; defaulting to nil")
			counts = nil
		}
		resp.CoaCounts = counts
	}
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

// rolloutSummaryDTO is the wire shape returned by ListRollouts.
type rolloutSummaryDTO struct {
	ID                  string         `json:"id"`
	PolicyID            string         `json:"policy_id"`
	PolicyVersionID     string         `json:"policy_version_id"`
	PolicyName          string         `json:"policy_name"`
	PolicyVersionNumber int            `json:"policy_version"`
	State               string         `json:"state"`
	CurrentStage        int            `json:"current_stage"`
	TotalSIMs           int            `json:"total_sims"`
	MigratedSIMs        int            `json:"migrated_sims"`
	StartedAt           *string        `json:"started_at"`
	CreatedAt           string         `json:"created_at"`
	CoaCounts           map[string]int `json:"coa_counts,omitempty"`
}

var validRolloutStates = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"paused":      true,
	"completed":   true,
	"rolled_back": true,
	"aborted":     true,
}

// ListRollouts handles GET /api/v1/policy-rollouts.
//
// Query params:
//   - state: optional CSV of rollout states (whitelist: pending, in_progress, paused, completed, rolled_back, aborted); default "in_progress,paused"
//   - limit: optional int in [1,100]; default 50
//
// No cursor pagination — active rollouts are bounded (cap 100 per request).
// Returns a standard success envelope with a JSON array (never null — FIX-241 PAT-018).
func (h *Handler) ListRollouts(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)

	var states []string
	if raw := r.URL.Query().Get("state"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if !validRolloutStates[s] {
				apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidParam,
					fmt.Sprintf("state contains invalid value %q", s))
				return
			}
			states = append(states, s)
		}
	}

	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		n, err := strconv.Atoi(rawLimit)
		if err != nil || n < 1 || n > 100 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "limit must be an integer in [1,100]")
			return
		}
		limit = n
	}

	if h.policyStore == nil {
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Policy store not available")
		return
	}

	rs, err := h.policyStore.ListRollouts(r.Context(), tenantID, store.ListRolloutsParams{
		States: states,
		Limit:  limit,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("list rollouts")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dtos := make([]rolloutSummaryDTO, 0, len(rs))
	for _, row := range rs {
		dto := rolloutSummaryDTO{
			ID:                  row.ID.String(),
			PolicyID:            row.PolicyID.String(),
			PolicyVersionID:     row.PolicyVersionID.String(),
			PolicyName:          row.PolicyName,
			PolicyVersionNumber: row.PolicyVersionNumber,
			State:               row.State,
			CurrentStage:        row.CurrentStage,
			TotalSIMs:           row.TotalSIMs,
			MigratedSIMs:        row.MigratedSIMs,
			CreatedAt:           row.CreatedAt.Format(time.RFC3339Nano),
		}
		if row.StartedAt != nil {
			s := row.StartedAt.Format(time.RFC3339Nano)
			dto.StartedAt = &s
		}
		// N+1: one query per rollout — acceptable because active rollouts are bounded
		// (default cap 50). D-XXX candidate for batched query optimisation.
		if counts, cErr := h.policyStore.GetCoAStatusCountsByRollout(r.Context(), row.ID); cErr != nil {
			h.logger.Warn().Err(cErr).Str("rollout_id", row.ID.String()).Msg("coa counts fetch failed; defaulting to nil")
		} else {
			dto.CoaCounts = counts
		}
		dtos = append(dtos, dto)
	}

	apierr.WriteSuccess(w, http.StatusOK, dtos)
}

// Validate is a stateless DSL validator. POST /api/v1/policies/validate.
// Returns 200 + {valid: true, compiled_rules, warnings} on success,
// 422 + {valid: false, errors: [DSLError]} on failure.
//
// FIX-243 Wave A AC-1/AC-2: pure — no DB writes, no state mutation.
// Safe at high frequency. Rate-limited 10/sec per IP at the router level
// (see internal/gateway/router.go).
func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DSLSource string `json:"dsl_source"`
	}
	formatRequested := r.URL.Query().Get("format") == "true"

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}
	if strings.TrimSpace(req.DSLSource) == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "dsl_source is required")
		return
	}

	// Parse → collect errors + warnings.
	allDiags := dsl.Validate(req.DSLSource)
	errSlice := make([]dsl.DSLError, 0, len(allDiags))
	warnSlice := make([]dsl.DSLError, 0, len(allDiags))
	for _, d := range allDiags {
		switch d.Severity {
		case "error":
			errSlice = append(errSlice, d)
		case "warning":
			warnSlice = append(warnSlice, d)
		}
	}

	if len(errSlice) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, "DSL_VALIDATION_FAILED",
			"DSL validation failed", map[string]any{
				"valid":  false,
				"errors": errSlice,
			})
		return
	}

	// No errors — compile to surface compiled rules to the FE.
	compiled, _, compileErr := dsl.CompileSource(req.DSLSource)
	if compileErr != nil {
		// Compile error not surfaced as a parse error — wrap as a synthetic DSLError.
		apierr.WriteError(w, http.StatusUnprocessableEntity, "DSL_VALIDATION_FAILED",
			"DSL compilation failed", map[string]any{
				"valid": false,
				"errors": []dsl.DSLError{{
					Severity: "error",
					Code:     "DSL_COMPILE_ERROR",
					Message:  compileErr.Error(),
				}},
			})
		return
	}

	response := map[string]any{
		"valid":          true,
		"compiled_rules": compiled,
		"warnings":       warnSlice,
	}

	// AC-8 / DEV-515 — auto-format support via ?format=true.
	// dsl.Format normalizes indentation/whitespace without changing
	// semantics. On format failure (e.g. transient panic recovery in
	// the lexer), fall back to echoing the source unchanged so the
	// validate response stays useful.
	if formatRequested {
		formatted, ferr := dsl.Format(req.DSLSource)
		if ferr != nil {
			response["formatted_source"] = req.DSLSource
		} else {
			response["formatted_source"] = formatted
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, response)
}

// Vocab returns the canonical DSL vocabulary snapshot — match fields,
// charging models, overage actions, billing cycles, units, rule
// keywords, and action names — sourced from the parser whitelists.
//
// FIX-243 Wave D — backs the FE autocomplete; replaces the FE-side
// hard-coded fallback list. Read-only, idempotent, cacheable; no rate
// limit needed.
func (h *Handler) Vocab(w http.ResponseWriter, r *http.Request) {
	apierr.WriteSuccess(w, http.StatusOK, dsl.Vocab())
}
