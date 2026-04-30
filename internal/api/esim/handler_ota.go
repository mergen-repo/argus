package esim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/job"
	"github.com/btopcu/argus/internal/notification"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// otaOperatorStore is the minimal OperatorStore interface needed by this handler.
type otaOperatorStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*store.Operator, error)
}

// otaCommandStore is the minimal EsimOTACommandStore interface needed by this handler.
type otaCommandStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*store.EsimOTACommand, error)
	MarkAcked(ctx context.Context, id uuid.UUID) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error
	ListByEID(ctx context.Context, tenantID uuid.UUID, eid, cursor string, limit int) ([]store.EsimOTACommand, string, error)
}

// otaStockStore is the minimal EsimProfileStockStore interface needed by this handler.
type otaStockStore interface {
	Get(ctx context.Context, tenantID, operatorID uuid.UUID) (*store.EsimProfileStock, error)
	ListSummary(ctx context.Context, tenantID uuid.UUID) ([]store.EsimProfileStock, error)
}

// otaJobStore is the minimal JobStore interface needed by this handler.
type otaJobStore interface {
	Create(ctx context.Context, p store.CreateJobParams) (*store.Job, error)
}

// SetOTADeps wires the dependencies required by BulkSwitch, OTACallback, StockSummary, and OTAHistory.
// Safe to call after construction; these fields are nil by default and guarded where needed.
func (h *Handler) SetOTADeps(
	operatorStore otaOperatorStore,
	commandStore otaCommandStore,
	stockStore otaStockStore,
	jobStore otaJobStore,
	smsrSecret string,
) {
	h.operatorStore = operatorStore
	h.commandStore = commandStore
	h.stockStore = stockStore
	h.jobStore = jobStore
	h.smsrSecret = smsrSecret
}

// ---- BulkSwitch (T11) ----

type bulkSwitchRequest struct {
	Filter           map[string]interface{} `json:"filter,omitempty"`
	EIDs             []string               `json:"eids,omitempty"`
	SimIDs           []string               `json:"sim_ids,omitempty"`
	TargetOperatorID string                 `json:"target_operator_id"`
	Reason           string                 `json:"reason,omitempty"`
}

type bulkSwitchResponse struct {
	JobID         string `json:"job_id"`
	AffectedCount int    `json:"affected_count"`
	Mode          string `json:"mode"`
}

// BulkSwitch handles POST /esim-profiles/bulk-switch.
func (h *Handler) BulkSwitch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req bulkSwitchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.TargetOperatorID == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "target_operator_id is required")
		return
	}

	targetOperatorID, err := uuid.Parse(req.TargetOperatorID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid target_operator_id format")
		return
	}

	selectorsSet := 0
	if len(req.Filter) > 0 {
		selectorsSet++
	}
	if len(req.EIDs) > 0 {
		selectorsSet++
	}
	if len(req.SimIDs) > 0 {
		selectorsSet++
	}
	if selectorsSet == 0 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "One of filter, eids, or sim_ids is required")
		return
	}
	if selectorsSet > 1 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Only one of filter, eids, or sim_ids may be specified")
		return
	}

	// FIX-235 Gate (F-A5): the EIDs selector branch is not yet wired through the bulk
	// switch processor (processForward only consumes payload.SimIDs and payload.SegmentID).
	// Reject with 400 instead of silently producing a 202 with zero work — handler tests
	// pin this contract until the processor learns to translate EIDs → SimIDs.
	if len(req.EIDs) > 0 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError,
			"eids selector is not yet supported; use sim_ids or filter")
		return
	}

	if h.operatorStore != nil {
		if _, opErr := h.operatorStore.GetByID(r.Context(), targetOperatorID); opErr != nil {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Target operator not found")
			return
		}
	}

	if h.stockStore != nil {
		stock, stockErr := h.stockStore.Get(r.Context(), tenantID, targetOperatorID)
		if stockErr != nil || (stock != nil && stock.Available <= 0) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, "STOCK_EXHAUSTED", "No stock available for the target operator")
			return
		}
	}

	var simIDs []uuid.UUID
	if len(req.SimIDs) > 0 {
		simIDs = make([]uuid.UUID, 0, len(req.SimIDs))
		for _, s := range req.SimIDs {
			id, parseErr := uuid.Parse(s)
			if parseErr != nil {
				apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, fmt.Sprintf("Invalid sim_id format: %s", s))
				return
			}
			simIDs = append(simIDs, id)
		}
	}

	affectedCount := len(simIDs)
	if len(req.EIDs) > 0 {
		affectedCount = len(req.EIDs)
	}

	payload, _ := json.Marshal(job.BulkEsimSwitchPayload{
		SimIDs:           simIDs,
		TargetOperatorID: targetOperatorID,
		Reason:           req.Reason,
	})

	userID := userIDFromCtx(r)

	var j *store.Job
	if h.jobStore != nil {
		j, err = h.jobStore.Create(r.Context(), store.CreateJobParams{
			Type:       job.JobTypeBulkEsimSwitch,
			Priority:   5,
			Payload:    payload,
			TotalItems: affectedCount,
			CreatedBy:  userID,
		})
		if err != nil {
			h.logger.Error().Err(err).Msg("create bulk esim switch job")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to create bulk switch job")
			return
		}
	} else {
		j = &store.Job{ID: uuid.New()}
	}

	if h.eventBus != nil {
		_ = h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, job.JobMessage{
			JobID:    j.ID,
			TenantID: tenantID,
			Type:     job.JobTypeBulkEsimSwitch,
		})
	}

	beforeData, _ := json.Marshal(map[string]interface{}{
		"target_operator_id": targetOperatorID.String(),
		"affected_count":     affectedCount,
		"reason":             req.Reason,
	})
	afterData, _ := json.Marshal(map[string]interface{}{
		"job_id": j.ID.String(),
		"mode":   "ota",
	})
	h.createAuditEntryRaw(r, "bulk.switch.requested", j.ID.String(), "esim_profile", beforeData, afterData, userID)

	apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
		Status: "success",
		Data: bulkSwitchResponse{
			JobID:         j.ID.String(),
			AffectedCount: affectedCount,
			Mode:          "ota",
		},
	})
}

// ---- OTACallback (T12) ----

type otaCallbackRequest struct {
	CommandID     string `json:"command_id"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message,omitempty"`
	OccurredAt    string `json:"occurred_at"`
}

// OTACallback handles POST /esim-profiles/callbacks/ota-status.
// This endpoint performs its own HMAC authentication — no JWT required.
// It must be registered OUTSIDE the protected route group (T15 wires this).
func (h *Handler) OTACallback(w http.ResponseWriter, r *http.Request) {
	tsHeader := r.Header.Get("X-SMSR-Timestamp")
	sigHeader := r.Header.Get("X-SMSR-Signature")

	if tsHeader == "" || sigHeader == "" {
		h.rejectCallback(r, "missing_headers", "OTA callback rejected")
		apierr.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing SMSR signature headers")
		return
	}

	tsUnix, tsErr := strconv.ParseInt(tsHeader, 10, 64)
	if tsErr != nil {
		h.rejectCallback(r, "invalid_timestamp", "OTA callback rejected")
		apierr.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid timestamp format")
		return
	}

	now := time.Now().Unix()
	if abs64(now-tsUnix) > 300 {
		h.rejectCallback(r, "timestamp_drift", fmt.Sprintf("OTA callback rejected drift_s=%d", abs64(now-tsUnix)))
		apierr.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Timestamp outside replay window")
		return
	}

	rawBody, readErr := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if readErr != nil {
		h.rejectCallback(r, "read_error", fmt.Sprintf("OTA callback rejected: %v", readErr))
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Failed to read request body")
		return
	}

	secret := h.smsrSecret
	if secret == "" {
		secret = os.Getenv("SMSR_CALLBACK_SECRET")
	}

	message := fmt.Sprintf("%s.%s", tsHeader, string(rawBody))
	cleanSig := sigHeader
	if len(cleanSig) > 7 && cleanSig[:7] == "sha256=" {
		cleanSig = cleanSig[7:]
	}

	if !notification.VerifyHMAC(message, secret, cleanSig) {
		h.rejectCallback(r, "invalid_signature", "OTA callback rejected")
		apierr.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid SMSR signature")
		return
	}

	var req otaCallbackRequest
	if unmarshalErr := json.Unmarshal(rawBody, &req); unmarshalErr != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	commandID, idErr := uuid.Parse(req.CommandID)
	if idErr != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid command_id format")
		return
	}

	cmd, lookupErr := h.commandStore.GetByID(r.Context(), commandID)
	if lookupErr != nil {
		if errors.Is(lookupErr, store.ErrEsimOTACommandNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "OTA command not found")
			return
		}
		h.logger.Error().Err(lookupErr).Str("command_id", req.CommandID).Msg("ota callback: get command")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	switch req.Status {
	case "acked":
		markErr := h.commandStore.MarkAcked(r.Context(), commandID)
		if errors.Is(markErr, store.ErrEsimOTAInvalidTransition) {
			apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{Status: "success", Data: map[string]bool{"ok": true}})
			return
		}
		if markErr != nil {
			h.logger.Error().Err(markErr).Str("command_id", req.CommandID).Msg("ota callback: mark acked")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}

		if cmd.SourceProfileID != nil && cmd.TargetProfileID != nil {
			_, switchErr := h.esimStore.Switch(r.Context(), cmd.TenantID, *cmd.SourceProfileID, *cmd.TargetProfileID, nil)
			if switchErr != nil && !errors.Is(switchErr, store.ErrInvalidProfileState) && !errors.Is(switchErr, store.ErrSameProfile) {
				h.logger.Warn().Err(switchErr).Str("command_id", req.CommandID).Msg("ota callback: apply switch (non-blocking)")
			}
		}

		if h.eventBus != nil {
			_ = h.eventBus.Publish(r.Context(), bus.SubjectESimCommandAcked, map[string]interface{}{
				"command_id": commandID.String(),
				"eid":        cmd.EID,
				"tenant_id":  cmd.TenantID.String(),
			})
		}

		h.createAuditEntryRaw(r, "ota.callback_acked", commandID.String(), "esim_ota_command",
			nil, mustMarshal(map[string]interface{}{"command_id": commandID.String(), "eid": cmd.EID}), nil)

	case "failed":
		errMsg := req.ErrorMessage
		if errMsg == "" {
			errMsg = "callback reported failure"
		}
		markErr := h.commandStore.MarkFailed(r.Context(), commandID, errMsg)
		if errors.Is(markErr, store.ErrEsimOTAInvalidTransition) {
			apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{Status: "success", Data: map[string]bool{"ok": true}})
			return
		}
		if markErr != nil {
			h.logger.Error().Err(markErr).Str("command_id", req.CommandID).Msg("ota callback: mark failed")
			apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
			return
		}

		if cmd.ProfileID != nil {
			pfErr := h.esimStore.MarkFailed(r.Context(), *cmd.ProfileID, errMsg)
			if pfErr != nil {
				h.logger.Warn().Err(pfErr).Str("profile_id", cmd.ProfileID.String()).Msg("ota callback: mark profile failed (non-blocking)")
			}
		}

		if h.eventBus != nil {
			_ = h.eventBus.Publish(r.Context(), bus.SubjectESimCommandFailed, map[string]interface{}{
				"command_id":    commandID.String(),
				"eid":           cmd.EID,
				"tenant_id":     cmd.TenantID.String(),
				"error_message": errMsg,
			})
		}

		h.createAuditEntryRaw(r, "ota.callback_failed", commandID.String(), "esim_ota_command",
			nil, mustMarshal(map[string]interface{}{"command_id": commandID.String(), "eid": cmd.EID, "error": errMsg}), nil)

	default:
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "status must be 'acked' or 'failed'")
		return
	}

	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{Status: "success", Data: map[string]bool{"ok": true}})
}

// ---- StockSummary + OTAHistory (T13) ----

type stockSummaryItem struct {
	OperatorID   string `json:"operator_id"`
	OperatorName string `json:"operator_name"`
	Total        int64  `json:"total"`
	Allocated    int64  `json:"allocated"`
	Available    int64  `json:"available"`
}

// StockSummary handles GET /esim-profiles/stock-summary.
func (h *Handler) StockSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	if h.stockStore == nil {
		apierr.WriteSuccess(w, http.StatusOK, []stockSummaryItem{})
		return
	}

	stocks, err := h.stockStore.ListSummary(r.Context(), tenantID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list esim stock summary")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]stockSummaryItem, 0, len(stocks))
	for _, s := range stocks {
		item := stockSummaryItem{
			OperatorID: s.OperatorID.String(),
			Total:      s.Total,
			Allocated:  s.Allocated,
			Available:  s.Available,
		}
		if h.operatorStore != nil {
			if op, opErr := h.operatorStore.GetByID(r.Context(), s.OperatorID); opErr == nil {
				item.OperatorName = op.Name
			}
		}
		items = append(items, item)
	}

	apierr.WriteSuccess(w, http.StatusOK, items)
}

type otaHistoryItem struct {
	ID               string  `json:"id"`
	EID              string  `json:"eid"`
	CommandType      string  `json:"command_type"`
	Status           string  `json:"status"`
	SMSRCommandID    *string `json:"smsr_command_id,omitempty"`
	TargetOperatorID *string `json:"target_operator_id,omitempty"`
	RetryCount       int     `json:"retry_count"`
	LastError        *string `json:"last_error,omitempty"`
	CreatedAt        string  `json:"created_at"`
	AckedAt          *string `json:"acked_at,omitempty"`
}

// OTAHistory handles GET /esim-profiles/{id}/ota-history.
func (h *Handler) OTAHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	profileID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid eSIM profile ID format")
		return
	}

	profile, err := h.esimStore.GetByID(r.Context(), tenantID, profileID)
	if err != nil {
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("get esim profile for ota history")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	q := r.URL.Query()
	cursor := q.Get("cursor")
	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err2 := strconv.Atoi(v); err2 == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	if h.commandStore == nil {
		apierr.WriteList(w, http.StatusOK, []otaHistoryItem{}, apierr.ListMeta{Cursor: "", Limit: limit, HasMore: false})
		return
	}

	commands, nextCursor, listErr := h.commandStore.ListByEID(r.Context(), tenantID, profile.EID, cursor, limit)
	if listErr != nil {
		h.logger.Error().Err(listErr).Str("eid", profile.EID).Msg("list ota commands by eid")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]otaHistoryItem, 0, len(commands))
	for _, c := range commands {
		item := otaHistoryItem{
			ID:          c.ID.String(),
			EID:         c.EID,
			CommandType: c.CommandType,
			Status:      c.Status,
			SMSRCommandID: c.SMSRCommandID,
			RetryCount:  c.RetryCount,
			LastError:   c.LastError,
			CreatedAt:   c.CreatedAt.Format(time.RFC3339Nano),
		}
		if c.TargetOperatorID != nil {
			s := c.TargetOperatorID.String()
			item.TargetOperatorID = &s
		}
		if c.AckedAt != nil {
			v := c.AckedAt.Format(time.RFC3339Nano)
			item.AckedAt = &v
		}
		items = append(items, item)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

// ---- helpers ----

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// rejectCallback writes a structured log + an audit_logs row for a rejected OTA callback.
// Plan T12 + AC-11: rejected callbacks (HMAC mismatch, replay-window, missing headers,
// read error) MUST leave a tamper-proof audit trail — log-only is insufficient.
//
// Pre-body-parse rejections have no tenant context (the secret is shared per-instance).
// We populate TenantID=uuid.Nil; downstream review/queries should filter on action.
func (h *Handler) rejectCallback(r *http.Request, reason, msg string) {
	h.logger.Warn().Str("action", "ota.callback_rejected").Str("reason", reason).Msg(msg)

	if h.auditSvc == nil {
		return
	}

	afterData, _ := json.Marshal(map[string]interface{}{
		"reason":     reason,
		"remote_ip":  r.RemoteAddr,
		"user_agent": r.UserAgent(),
	})

	ip := r.RemoteAddr
	ua := r.UserAgent()
	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:   uuid.Nil,
		Action:     "ota.callback_rejected",
		EntityType: "esim_ota_command",
		EntityID:   "",
		AfterData:  afterData,
		IPAddress:  &ip,
		UserAgent:  &ua,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("reason", reason).Msg("ota.callback_rejected audit entry failed")
	}
}

func (h *Handler) createAuditEntryRaw(r *http.Request, action, entityID, entityType string, before, after json.RawMessage, userID *uuid.UUID) {
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

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    entityType,
		EntityID:      entityID,
		BeforeData:    before,
		AfterData:     after,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}
