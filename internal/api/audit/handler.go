package audit

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type Handler struct {
	auditStore *store.AuditStore
	auditSvc   *audit.FullService
	logger     zerolog.Logger
}

func NewHandler(auditStore *store.AuditStore, auditSvc *audit.FullService, logger zerolog.Logger) *Handler {
	return &Handler{
		auditStore: auditStore,
		auditSvc:   auditSvc,
		logger:     logger.With().Str("component", "audit_handler").Logger(),
	}
}

type auditLogResponse struct {
	ID         int64           `json:"id"`
	UserID     *uuid.UUID      `json:"user_id"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Diff       json.RawMessage `json:"diff,omitempty"`
	IPAddress  *string         `json:"ip_address,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

type verifyResponse struct {
	Verified       bool   `json:"verified"`
	EntriesChecked int    `json:"entries_checked"`
	FirstInvalid   *int64 `json:"first_invalid"`
}

type exportRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type systemEventRequest struct {
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	AfterData  json.RawMessage `json:"after_data,omitempty"`
}

type systemEventResponse struct {
	Status     string `json:"status"`
	Action     string `json:"action"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
}

func toAuditLogResponse(e audit.Entry) auditLogResponse {
	return auditLogResponse{
		ID:         e.ID,
		UserID:     e.UserID,
		Action:     e.Action,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		Diff:       e.Diff,
		IPAddress:  e.IPAddress,
		CreatedAt:  e.CreatedAt.Format(time.RFC3339Nano),
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	params := store.ListAuditParams{
		Cursor:     r.URL.Query().Get("cursor"),
		Action:     r.URL.Query().Get("action"),
		EntityType: r.URL.Query().Get("entity_type"),
		EntityID:   r.URL.Query().Get("entity_id"),
	}

	if actionsStr := r.URL.Query().Get("actions"); actionsStr != "" {
		parts := strings.Split(actionsStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				params.Actions = append(params.Actions, p)
			}
		}
	}

	params.Limit = 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 100 {
			params.Limit = v
		}
	}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			t, err = time.Parse("2006-01-02", fromStr)
		}
		if err == nil {
			params.From = &t
		}
	}

	if toStr := r.URL.Query().Get("to"); toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			t, err = time.Parse("2006-01-02", toStr)
		}
		if err == nil {
			params.To = &t
		}
	}

	if userIDStr := r.URL.Query().Get("user_id"); userIDStr != "" {
		uid, err := uuid.Parse(userIDStr)
		if err == nil {
			params.UserID = &uid
		}
	}

	entries, nextCursor, err := h.auditStore.List(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list audit logs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]auditLogResponse, 0, len(entries))
	for _, e := range entries {
		items = append(items, toAuditLogResponse(e))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   params.Limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	count := 100
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if v, err := strconv.Atoi(countStr); err == nil && v > 0 && v <= 10000 {
			count = v
		}
	}

	result, err := h.auditSvc.VerifyChain(r.Context(), tenantID, count)
	if err != nil {
		h.logger.Error().Err(err).Msg("verify audit chain")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, verifyResponse{
		Verified:       result.Verified,
		EntriesChecked: result.EntriesChecked,
		FirstInvalid:   result.FirstInvalid,
	})
}

func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req exportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.From == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "from", "message": "From date is required", "code": "required"})
	}
	if req.To == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "to", "message": "To date is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	fromTime, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Invalid from date format, expected YYYY-MM-DD")
		return
	}
	toTime, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Invalid to date format, expected YYYY-MM-DD")
		return
	}
	toTime = toTime.Add(24*time.Hour - time.Nanosecond)

	entries, err := h.auditStore.GetByDateRange(r.Context(), tenantID, fromTime, toTime)
	if err != nil {
		h.logger.Error().Err(err).Msg("export audit logs")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit_logs_%s_%s.csv", req.From, req.To))
	w.WriteHeader(http.StatusOK)

	writer := csv.NewWriter(w)
	defer writer.Flush()

	writer.Write([]string{"id", "user_id", "action", "entity_type", "entity_id", "before_data", "after_data", "diff", "ip_address", "user_agent", "created_at"})

	for _, e := range entries {
		userID := ""
		if e.UserID != nil {
			userID = e.UserID.String()
		}
		ipAddr := ""
		if e.IPAddress != nil {
			ipAddr = *e.IPAddress
		}
		ua := ""
		if e.UserAgent != nil {
			ua = *e.UserAgent
		}

		writer.Write([]string{
			fmt.Sprintf("%d", e.ID),
			userID,
			e.Action,
			e.EntityType,
			e.EntityID,
			string(e.BeforeData),
			string(e.AfterData),
			string(e.Diff),
			ipAddr,
			ua,
			e.CreatedAt.Format(time.RFC3339Nano),
		})
	}
}

// EmitSystemEvent writes a system-level audit entry for infrastructure actions
// (e.g., blue-green flip, rollback) that occur outside tenant context. Uses
// TenantID = uuid.Nil and no UserID. Gated by super_admin role at the router
// layer.
func (h *Handler) EmitSystemEvent(w http.ResponseWriter, r *http.Request) {
	if h.auditSvc == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeInternalError, "Audit service unavailable")
		return
	}

	var req systemEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var validationErrors []map[string]string
	if req.Action == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "action", "message": "Action is required", "code": "required"})
	}
	if req.EntityType == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "entity_type", "message": "Entity type is required", "code": "required"})
	}
	if req.EntityID == "" {
		validationErrors = append(validationErrors, map[string]string{"field": "entity_id", "message": "Entity ID is required", "code": "required"})
	}
	if len(validationErrors) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", validationErrors)
		return
	}

	event := audit.AuditEvent{
		TenantID:   uuid.Nil,
		Action:     req.Action,
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		AfterData:  req.AfterData,
	}

	if err := h.auditSvc.ProcessEntry(r.Context(), event); err != nil {
		h.logger.Error().Err(err).
			Str("action", req.Action).
			Str("entity_type", req.EntityType).
			Str("entity_id", req.EntityID).
			Msg("emit system audit event")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to write audit entry")
		return
	}

	apierr.WriteSuccess(w, http.StatusCreated, systemEventResponse{
		Status:     "recorded",
		Action:     req.Action,
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
	})
}
