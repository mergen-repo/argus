package violation

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
	"github.com/btopcu/argus/internal/severity"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// minRemediationReasonLen is the minimum trimmed length required for the
// `reason` field on destructive remediate actions (`suspend_sim`, `dismiss`).
// Discourages rubber-stamp confirmations while staying short enough not to
// frustrate the operator. Mirrored client-side in the Remediate dialog.
const minRemediationReasonLen = 3

type Handler struct {
	violationStore *store.PolicyViolationStore
	simStore       *store.SIMStore
	auditSvc       audit.Auditor
	logger         zerolog.Logger
}

func NewHandler(violationStore *store.PolicyViolationStore, logger zerolog.Logger, opts ...HandlerOption) *Handler {
	h := &Handler{
		violationStore: violationStore,
		logger:         logger.With().Str("handler", "violations").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type HandlerOption func(*Handler)

func WithAuditSvc(a audit.Auditor) HandlerOption {
	return func(h *Handler) { h.auditSvc = a }
}

func WithSIMStore(s *store.SIMStore) HandlerOption {
	return func(h *Handler) { h.simStore = s }
}

// violationDTO is the enriched response shape for violation endpoints.
// It embeds all PolicyViolation base fields plus joined display names.
type violationDTO struct {
	ID                  interface{} `json:"id"`
	TenantID            interface{} `json:"tenant_id"`
	SimID               interface{} `json:"sim_id"`
	PolicyID            interface{} `json:"policy_id"`
	VersionID           interface{} `json:"version_id"`
	RuleIndex           int         `json:"rule_index"`
	ViolationType       string      `json:"violation_type"`
	ActionTaken         string      `json:"action_taken"`
	Details             interface{} `json:"details"`
	SessionID           interface{} `json:"session_id,omitempty"`
	OperatorID          interface{} `json:"operator_id,omitempty"`
	APNID               interface{} `json:"apn_id,omitempty"`
	Severity            string      `json:"severity"`
	CreatedAt           interface{} `json:"created_at"`
	AcknowledgedAt      interface{} `json:"acknowledged_at,omitempty"`
	AcknowledgedBy      interface{} `json:"acknowledged_by,omitempty"`
	AcknowledgmentNote  interface{} `json:"acknowledgment_note,omitempty"`
	ICCID               *string     `json:"iccid,omitempty"`
	IMSI                *string     `json:"imsi,omitempty"`
	MSISDN              *string     `json:"msisdn,omitempty"`
	OperatorName        *string     `json:"operator_name,omitempty"`
	OperatorCode        *string     `json:"operator_code,omitempty"`
	APNName             *string     `json:"apn_name,omitempty"`
	PolicyName          *string     `json:"policy_name,omitempty"`
	PolicyVersionNumber *int        `json:"policy_version_number,omitempty"`
}

func toViolationDTO(v *store.PolicyViolationWithNames) violationDTO {
	return violationDTO{
		ID:                  v.ID,
		TenantID:            v.TenantID,
		SimID:               v.SimID,
		PolicyID:            v.PolicyID,
		VersionID:           v.VersionID,
		RuleIndex:           v.RuleIndex,
		ViolationType:       v.ViolationType,
		ActionTaken:         v.ActionTaken,
		Details:             v.Details,
		SessionID:           v.SessionID,
		OperatorID:          v.OperatorID,
		APNID:               v.APNID,
		Severity:            v.Severity,
		CreatedAt:           v.CreatedAt,
		AcknowledgedAt:      v.AcknowledgedAt,
		AcknowledgedBy:      v.AcknowledgedBy,
		AcknowledgmentNote:  v.AcknowledgmentNote,
		ICCID:               v.ICCID,
		IMSI:                v.IMSI,
		MSISDN:              v.MSISDN,
		OperatorName:        v.OperatorName,
		OperatorCode:        v.OperatorCode,
		APNName:             v.APNName,
		PolicyName:          v.PolicyName,
		PolicyVersionNumber: v.PolicyVersionNumber,
	}
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
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid violation ID")
		return
	}

	v, err := h.violationStore.GetByIDEnriched(r.Context(), id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrViolationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
			return
		}
		h.logger.Error().Err(err).Msg("get violation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to get violation")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toViolationDTO(v))
}

type remediateRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (h *Handler) Remediate(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid violation ID")
		return
	}

	var req remediateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	validActions := map[string]bool{"suspend_sim": true, "escalate": true, "dismiss": true}
	if !validActions[req.Action] {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid action. Allowed: suspend_sim, escalate, dismiss",
			[]map[string]interface{}{{"field": "action", "message": "must be suspend_sim, escalate, or dismiss", "code": "invalid_enum"}})
		return
	}

	trimmedReason := strings.TrimSpace(req.Reason)
	if (req.Action == "suspend_sim" || req.Action == "dismiss") && len(trimmedReason) < minRemediationReasonLen {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Reason must be at least 3 characters",
			[]map[string]interface{}{{"field": "reason", "message": "must be at least 3 characters", "code": "min_length"}})
		return
	}
	req.Reason = trimmedReason

	v, err := h.violationStore.GetByIDEnriched(r.Context(), id, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrViolationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
			return
		}
		h.logger.Error().Err(err).Msg("get violation for remediate")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	response := map[string]interface{}{"violation": toViolationDTO(v)}

	switch req.Action {
	case "suspend_sim":
		if h.simStore == nil {
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "SIM store not available")
			return
		}
		reason := req.Reason
		if reason == "" {
			reason = "violation_remediation"
		}
		sim, suspendErr := h.simStore.Suspend(r.Context(), tenantID, v.SimID, &userID, &reason)
		if suspendErr != nil {
			if errors.Is(suspendErr, store.ErrSIMNotFound) {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
				return
			}
			if errors.Is(suspendErr, store.ErrInvalidStateTransition) {
				apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "SIM cannot be suspended in its current state")
				return
			}
			h.logger.Error().Err(suspendErr).Msg("suspend sim for violation remediate")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		response["sim"] = sim

		audit.Emit(r, h.logger, h.auditSvc, "violation.remediated", "policy_violation", id.String(), nil, map[string]interface{}{
			"action":        req.Action,
			"reason":        req.Reason,
			"remediated_by": userID.String(),
			"sim_id":        v.SimID.String(),
		})

		if _, ackErr := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Reason); ackErr != nil && !errors.Is(ackErr, store.ErrAlreadyAcknowledged) {
			h.logger.Warn().Err(ackErr).Msg("acknowledge violation after suspend")
		}

	case "escalate":
		audit.Emit(r, h.logger, h.auditSvc, "violation.escalated", "policy_violation", id.String(), nil, map[string]interface{}{
			"action":       req.Action,
			"reason":       req.Reason,
			"escalated_by": userID.String(),
		})

	case "dismiss":
		updatedV, ackErr := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Reason)
		if ackErr != nil {
			if errors.Is(ackErr, store.ErrAlreadyAcknowledged) {
				apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "Violation already acknowledged")
				return
			}
			if errors.Is(ackErr, store.ErrViolationNotFound) {
				apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
				return
			}
			h.logger.Error().Err(ackErr).Msg("dismiss violation")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}
		response["violation"] = updatedV
		audit.Emit(r, h.logger, h.auditSvc, "violation.dismissed", "policy_violation", id.String(), nil, map[string]interface{}{
			"reason":       req.Reason,
			"dismissed_by": userID.String(),
		})
	}

	// Best-effort: write details->remediation so the FE can derive lifecycle
	// status (remediated / dismissed / escalated) without joining audit_logs.
	// A missing-row error here is non-fatal — the primary action already
	// succeeded above, and a later refetch will simply omit the new key.
	if setErr := h.violationStore.SetRemediationKind(r.Context(), id, tenantID, req.Action); setErr != nil && !errors.Is(setErr, store.ErrViolationNotFound) {
		h.logger.Warn().Err(setErr).Str("action", req.Action).Msg("set remediation kind")
	}

	apierr.WriteSuccess(w, http.StatusOK, response)
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

	var simID *uuid.UUID
	if v := q.Get("sim_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			simID = &id
		}
	}

	var policyID *uuid.UUID
	if v := q.Get("policy_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			policyID = &id
		}
	}

	var ackFilter *bool
	if v := q.Get("acknowledged"); v != "" {
		b := v == "true"
		ackFilter = &b
	}

	sevFilter := q.Get("severity")
	if sevFilter != "" {
		if err := severity.Validate(sevFilter); err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidSeverity,
				"severity must be one of: critical, high, medium, low, info; got '"+sevFilter+"'")
			return
		}
	}

	statusFilter := q.Get("status")
	if statusFilter != "" && !store.IsValidViolationStatus(statusFilter) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"status must be one of: open, acknowledged, remediated, dismissed, escalated",
			[]map[string]interface{}{{"field": "status", "message": "invalid lifecycle status", "code": "invalid_enum"}})
		return
	}

	var dateFrom, dateTo *time.Time
	if v := q.Get("date_from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"date_from must be RFC3339",
				[]map[string]interface{}{{"field": "date_from", "message": "expected RFC3339 timestamp", "code": "invalid_format"}})
			return
		}
		dateFrom = &t
	}
	if v := q.Get("date_to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
				"date_to must be RFC3339",
				[]map[string]interface{}{{"field": "date_to", "message": "expected RFC3339 timestamp", "code": "invalid_format"}})
			return
		}
		dateTo = &t
	}
	if dateFrom != nil && dateTo != nil && dateTo.Before(*dateFrom) {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"date_to must be on or after date_from",
			[]map[string]interface{}{{"field": "date_to", "message": "must be ≥ date_from", "code": "invalid_range"}})
		return
	}

	params := store.ListViolationsParams{
		Cursor:        q.Get("cursor"),
		Limit:         limit,
		ViolationType: q.Get("violation_type"),
		ActionTaken:   q.Get("action_taken"),
		Severity:      sevFilter,
		SimID:         simID,
		PolicyID:      policyID,
		Acknowledged:  ackFilter,
		Status:        statusFilter,
		DateFrom:      dateFrom,
		DateTo:        dateTo,
	}

	enriched, nextCursor, err := h.violationStore.ListEnriched(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list violations")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to list violations")
		return
	}

	dtos := make([]violationDTO, len(enriched))
	for i := range enriched {
		dtos[i] = toViolationDTO(&enriched[i])
	}

	apierr.WriteList(w, http.StatusOK, dtos, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) CountByType(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	counts, err := h.violationStore.CountByType(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("count violations")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to count violations")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, counts)
}

type acknowledgeRequest struct {
	Note string `json:"note"`
}

func (h *Handler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid violation ID")
		return
	}

	var req acknowledgeRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	v, err := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Note)
	if err != nil {
		if errors.Is(err, store.ErrAlreadyAcknowledged) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeConflict, "Violation already acknowledged")
			return
		}
		if errors.Is(err, store.ErrViolationNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Violation not found")
			return
		}
		h.logger.Error().Err(err).Msg("acknowledge violation")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to acknowledge violation")
		return
	}

	audit.Emit(r, h.logger, h.auditSvc, "violation.acknowledge", "policy_violation", id.String(), nil, map[string]interface{}{
		"acknowledged_by": userID.String(),
		"note":            req.Note,
	})

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"id":              v.ID,
		"acknowledged_at": v.AcknowledgedAt,
		"acknowledged_by": v.AcknowledgedBy,
		"note":            v.AcknowledgmentNote,
	})
}

// maxBulkIDs caps a single bulk request so iteration time stays bounded and
// per-id audit fidelity (one row per success) does not blow up at scale.
// Larger jobs should go through filter-based bulk in FIX-236.
const maxBulkIDs = 100

type bulkAcknowledgeRequest struct {
	IDs  []string `json:"ids"`
	Note string   `json:"note"`
}

type bulkDismissRequest struct {
	IDs    []string `json:"ids"`
	Reason string   `json:"reason"`
}

type bulkFailure struct {
	ID        string `json:"id"`
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

type bulkResult struct {
	Succeeded []string      `json:"succeeded"`
	Failed    []bulkFailure `json:"failed"`
}

// parseBulkIDs normalises the incoming string ids and enforces 1..maxBulkIDs.
// Duplicates are de-duplicated; malformed ids are returned as failures rather
// than rejecting the whole request, matching the partial-success contract.
func parseBulkIDs(rawIDs []string) (ids []uuid.UUID, failed []bulkFailure, sizeErr error) {
	if len(rawIDs) == 0 {
		return nil, nil, errors.New("ids must be non-empty")
	}
	if len(rawIDs) > maxBulkIDs {
		return nil, nil, fmt.Errorf("ids must be ≤ %d per request", maxBulkIDs)
	}
	seen := make(map[uuid.UUID]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		id, parseErr := uuid.Parse(strings.TrimSpace(raw))
		if parseErr != nil {
			failed = append(failed, bulkFailure{ID: raw, ErrorCode: "validation_error", Message: "invalid uuid"})
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, failed, nil
}

func (h *Handler) BulkAcknowledge(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	var req bulkAcknowledgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	ids, failed, sizeErr := parseBulkIDs(req.IDs)
	if sizeErr != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, sizeErr.Error(),
			[]map[string]interface{}{{"field": "ids", "message": sizeErr.Error(), "code": "invalid_size"}})
		return
	}

	result := bulkResult{Succeeded: []string{}, Failed: failed}

	for _, id := range ids {
		v, err := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, req.Note)
		if err != nil {
			code, msg := bulkErrorMessage(err)
			result.Failed = append(result.Failed, bulkFailure{ID: id.String(), ErrorCode: code, Message: msg})
			continue
		}
		result.Succeeded = append(result.Succeeded, v.ID.String())
		audit.Emit(r, h.logger, h.auditSvc, "violation.acknowledge", "policy_violation", v.ID.String(), nil, map[string]interface{}{
			"acknowledged_by": userID.String(),
			"note":            req.Note,
			"bulk":            true,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}

func (h *Handler) BulkDismiss(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}
	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	var req bulkDismissRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) < minRemediationReasonLen {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"Reason must be at least 3 characters",
			[]map[string]interface{}{{"field": "reason", "message": "must be at least 3 characters", "code": "min_length"}})
		return
	}

	ids, failed, sizeErr := parseBulkIDs(req.IDs)
	if sizeErr != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, sizeErr.Error(),
			[]map[string]interface{}{{"field": "ids", "message": sizeErr.Error(), "code": "invalid_size"}})
		return
	}

	result := bulkResult{Succeeded: []string{}, Failed: failed}

	for _, id := range ids {
		v, err := h.violationStore.Acknowledge(r.Context(), id, tenantID, userID, reason)
		if err != nil {
			code, msg := bulkErrorMessage(err)
			result.Failed = append(result.Failed, bulkFailure{ID: id.String(), ErrorCode: code, Message: msg})
			continue
		}
		// Best-effort: tag the row as dismissed for FE-side status derivation.
		// On failure here we keep the id as succeeded — the audit emit + ack
		// already happened, and the only consequence is that the FE shows
		// the row as Acknowledged rather than Dismissed until manual repair.
		if setErr := h.violationStore.SetRemediationKind(r.Context(), id, tenantID, "dismiss"); setErr != nil && !errors.Is(setErr, store.ErrViolationNotFound) {
			h.logger.Warn().Err(setErr).Str("id", id.String()).Msg("bulk dismiss: set remediation kind")
		}
		result.Succeeded = append(result.Succeeded, v.ID.String())
		audit.Emit(r, h.logger, h.auditSvc, "violation.dismissed", "policy_violation", v.ID.String(), nil, map[string]interface{}{
			"dismissed_by": userID.String(),
			"reason":       reason,
			"bulk":         true,
		})
	}

	apierr.WriteSuccess(w, http.StatusOK, result)
}

// bulkErrorMessage maps store-level errors to the partial-success contract.
// Unknown errors are reported as "internal_error" with a generic message so
// stack traces never leak in the bulk response payload.
func bulkErrorMessage(err error) (code, msg string) {
	switch {
	case errors.Is(err, store.ErrAlreadyAcknowledged):
		return "already_acknowledged", "violation already acknowledged"
	case errors.Is(err, store.ErrViolationNotFound):
		return "not_found", "violation not found"
	default:
		return "internal_error", "an unexpected error occurred"
	}
}
