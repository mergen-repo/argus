package sim

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/policy/binding"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var imeiRegexp = regexp.MustCompile(`^[0-9]{15}$`)

var validProtocols = map[string]bool{
	"radius":       true,
	"diameter_s6a": true,
	"5g_sba":       true,
}

// DeviceBindingHandler handles API-327, API-328, API-329, API-330.
type DeviceBindingHandler struct {
	simStore         *store.SIMStore
	imeiHistoryStore *store.IMEIHistoryStore
	auditSvc         audit.Auditor
	notifier         EventPublisher
	logger           zerolog.Logger
}

func NewDeviceBindingHandler(
	simStore *store.SIMStore,
	imeiHistoryStore *store.IMEIHistoryStore,
	auditSvc audit.Auditor,
	notifier EventPublisher,
	logger zerolog.Logger,
) *DeviceBindingHandler {
	return &DeviceBindingHandler{
		simStore:         simStore,
		imeiHistoryStore: imeiHistoryStore,
		auditSvc:         auditSvc,
		notifier:         notifier,
		logger:           logger.With().Str("component", "device_binding_handler").Logger(),
	}
}

// deviceBindingResponse is the DTO for API-327 and API-328.
type deviceBindingResponse struct {
	BoundIMEI             *string `json:"bound_imei"`
	BindingMode           *string `json:"binding_mode"`
	BindingStatus         *string `json:"binding_status"`
	BindingVerifiedAt     *string `json:"binding_verified_at"`
	LastIMEISeenAt        *string `json:"last_imei_seen_at"`
	BindingGraceExpiresAt *string `json:"binding_grace_expires_at"`
	HistoryCount          int     `json:"history_count"`
}

// imeiHistoryRowResponse is the per-row DTO for API-330.
type imeiHistoryRowResponse struct {
	ID                      string  `json:"id"`
	SIMID                   string  `json:"sim_id"`
	TenantID                string  `json:"tenant_id"`
	ObservedIMEI            string  `json:"observed_imei"`
	ObservedSoftwareVersion *string `json:"observed_software_version"`
	ObservedAt              string  `json:"observed_at"`
	CaptureProtocol         string  `json:"capture_protocol"`
	NASIPAddress            *string `json:"nas_ip_address"`
	WasMismatch             bool    `json:"was_mismatch"`
	AlarmRaised             bool    `json:"alarm_raised"`
}

// bindingAuditPayload is the structured before/after shape for audit entries.
type bindingAuditPayload struct {
	BindingMode   *string `json:"binding_mode"`
	BoundIMEI     *string `json:"bound_imei"`
	BindingStatus *string `json:"binding_status"`
}

// bindingPayloadsEqual reports whether two binding payloads are byte-equal
// across all three nullable string fields (treating nil == nil).
func bindingPayloadsEqual(a, b bindingAuditPayload) bool {
	return strPtrEqual(a.BindingMode, b.BindingMode) &&
		strPtrEqual(a.BoundIMEI, b.BoundIMEI) &&
		strPtrEqual(a.BindingStatus, b.BindingStatus)
}

func strPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func toDeviceBindingResponse(b *store.DeviceBinding, historyCount int) deviceBindingResponse {
	resp := deviceBindingResponse{
		BoundIMEI:     b.BoundIMEI,
		BindingMode:   b.BindingMode,
		BindingStatus: b.BindingStatus,
		HistoryCount:  historyCount,
	}
	if b.BindingVerifiedAt != nil {
		v := b.BindingVerifiedAt.Format(time.RFC3339Nano)
		resp.BindingVerifiedAt = &v
	}
	if b.LastIMEISeenAt != nil {
		v := b.LastIMEISeenAt.Format(time.RFC3339Nano)
		resp.LastIMEISeenAt = &v
	}
	if b.BindingGraceExpiresAt != nil {
		v := b.BindingGraceExpiresAt.Format(time.RFC3339Nano)
		resp.BindingGraceExpiresAt = &v
	}
	return resp
}

func toIMEIHistoryRowResponse(r store.IMEIHistoryRow) imeiHistoryRowResponse {
	return imeiHistoryRowResponse{
		ID:                      r.ID.String(),
		SIMID:                   r.SIMID.String(),
		TenantID:                r.TenantID.String(),
		ObservedIMEI:            r.ObservedIMEI,
		ObservedSoftwareVersion: r.ObservedSoftwareVersion,
		ObservedAt:              r.ObservedAt.Format(time.RFC3339Nano),
		CaptureProtocol:         r.CaptureProtocol,
		NASIPAddress:            r.NASIPAddress,
		WasMismatch:             r.WasMismatch,
		AlarmRaised:             r.AlarmRaised,
	}
}

// Get handles GET /api/v1/sims/{id}/device-binding (API-327).
func (h *DeviceBindingHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	binding, err := h.simStore.GetDeviceBinding(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("get device binding failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	historyCount, err := h.imeiHistoryStore.Count(r.Context(), tenantID, simID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("count imei history failed (non-fatal)")
		historyCount = 0
	}

	apierr.WriteSuccess(w, http.StatusOK, toDeviceBindingResponse(binding, historyCount))
}

// patchDeviceBindingRequest is the PATCH body. All fields are JSON-optional.
// Note: a non-pointer json.RawMessage is required to distinguish "absent" (zero
// length) from "explicit null" ("null" bytes). With *json.RawMessage the std
// json decoder collapses both to nil pointer, defeating tri-state semantics
// and breaking AC-8 null-clear. (F-LEAD-1 gate fix.)
type patchDeviceBindingRequest struct {
	BindingMode           json.RawMessage `json:"binding_mode"`
	BoundIMEI             json.RawMessage `json:"bound_imei"`
	BindingStatusOverride json.RawMessage `json:"binding_status_override"`
}

// decodeOptionalStringField parses a json.RawMessage that may be:
//   - len(raw)==0 → field absent → return (nil, false, nil): no change
//   - bytes "null" → explicit null → return (nil, true, nil): set to NULL
//   - bytes `"value"` → string → return (&value, true, nil)
func decodeOptionalStringField(raw json.RawMessage) (*string, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false, err
	}
	return &s, true, nil
}

// Patch handles PATCH /api/v1/sims/{id}/device-binding (API-328).
func (h *DeviceBindingHandler) Patch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	var req patchDeviceBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	modeVal, modePresent, err := decodeOptionalStringField(req.BindingMode)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidBindingMode, "Invalid binding_mode value")
		return
	}
	if modePresent && modeVal != nil && !store.IsValidBindingMode(*modeVal) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidBindingMode, "Invalid binding_mode value")
		return
	}

	imeiVal, imeiPresent, err := decodeOptionalStringField(req.BoundIMEI)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidIMEI, "Invalid bound_imei value")
		return
	}
	if imeiPresent && imeiVal != nil && !imeiRegexp.MatchString(*imeiVal) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidIMEI, "bound_imei must be exactly 15 digits")
		return
	}

	statusVal, statusPresent, err := decodeOptionalStringField(req.BindingStatusOverride)
	if err != nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidBindingStatus, "Invalid binding_status_override value")
		return
	}
	if statusPresent && statusVal != nil && !store.IsValidBindingStatus(*statusVal) {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidBindingStatus, "Invalid binding_status_override value")
		return
	}

	// GET current state, then merge absent fields so SetDeviceBinding doesn't
	// overwrite unspecified columns with NULL.
	current, err := h.simStore.GetDeviceBinding(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("get device binding (pre-patch) failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	mergedMode := current.BindingMode
	if modePresent {
		mergedMode = modeVal
	}
	mergedIMEI := current.BoundIMEI
	if imeiPresent {
		mergedIMEI = imeiVal
	}
	mergedStatus := current.BindingStatus
	if statusPresent {
		mergedStatus = statusVal
	}

	beforePayload := bindingAuditPayload{
		BindingMode:   current.BindingMode,
		BoundIMEI:     current.BoundIMEI,
		BindingStatus: current.BindingStatus,
	}

	updated, err := h.simStore.SetDeviceBinding(r.Context(), tenantID, simID, mergedMode, mergedIMEI, mergedStatus)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("set device binding failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	afterPayload := bindingAuditPayload{
		BindingMode:   updated.BindingMode,
		BoundIMEI:     updated.BoundIMEI,
		BindingStatus: updated.BindingStatus,
	}

	// F-A6 (STORY-094 Gate): skip audit emission on no-op PATCH (before == after)
	// to keep the hash chain free of pollution. AC-14 still satisfied — audit is
	// emitted on every state-changing operation.
	if !bindingPayloadsEqual(beforePayload, afterPayload) {
		userID := userIDFromCtx(r)
		h.createDeviceBindingAuditEntry(r, simID.String(), "sim.binding_mode_changed", beforePayload, afterPayload, userID)
	}

	historyCount, err := h.imeiHistoryStore.Count(r.Context(), tenantID, simID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("count imei history after patch failed (non-fatal)")
		historyCount = 0
	}

	apierr.WriteSuccess(w, http.StatusOK, toDeviceBindingResponse(updated, historyCount))
}

// RePair handles POST /api/v1/sims/{id}/device-binding/re-pair (API-329).
// Idempotent per AC-3: if bound_imei IS NULL AND binding_status='pending' BEFORE
// the UPDATE, returns 200 with current DTO and no audit/notification re-emission.
func (h *DeviceBindingHandler) RePair(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	current, err := h.simStore.GetDeviceBinding(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("get device binding (pre-repaint) failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	isAlreadyCleared := current.BoundIMEI == nil
	isPending := current.BindingStatus != nil && *current.BindingStatus == "pending"
	if isAlreadyCleared && isPending {
		historyCount, _ := h.imeiHistoryStore.Count(r.Context(), tenantID, simID)
		apierr.WriteSuccess(w, http.StatusOK, toDeviceBindingResponse(current, historyCount))
		return
	}

	var previousBoundIMEI *string
	if current.BoundIMEI != nil {
		v := *current.BoundIMEI
		previousBoundIMEI = &v
	}

	pendingStatus := "pending"
	updated, err := h.simStore.SetDeviceBinding(r.Context(), tenantID, simID, current.BindingMode, nil, &pendingStatus)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("set device binding (re-pair) failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	userID := userIDFromCtx(r)
	beforePayload := bindingAuditPayload{
		BoundIMEI:     previousBoundIMEI,
		BindingStatus: current.BindingStatus,
	}
	afterPayload := bindingAuditPayload{
		BoundIMEI:     nil,
		BindingStatus: &pendingStatus,
	}
	h.createDeviceBindingAuditEntry(r, simID.String(), "sim.imei_repaired", beforePayload, afterPayload, userID)

	if h.notifier != nil {
		prevIMEIStr := ""
		if previousBoundIMEI != nil {
			prevIMEIStr = *previousBoundIMEI
		}
		// API-329 notification payload per STORY-097 plan §T6:
		// {sim_id, iccid, previous_bound_imei, actor_user_id}.
		// iccid lookup is best-effort: failure does not block the re-pair
		// response (the persistence already committed). Severity is
		// encoded by the bus.Envelope (FIX-212), not the payload.
		iccid, _ := h.simStore.GetICCIDByID(r.Context(), simID)
		_ = h.notifier.Publish(r.Context(), binding.NotifSubjectBindingRePaired, map[string]interface{}{
			"sim_id":              simID,
			"iccid":               iccid,
			"previous_bound_imei": prevIMEIStr,
			"actor_user_id":       userID,
		})
	}

	historyCount, err := h.imeiHistoryStore.Count(r.Context(), tenantID, simID)
	if err != nil {
		h.logger.Warn().Err(err).Str("sim_id", simID.String()).Msg("count imei history after re-pair failed (non-fatal)")
		historyCount = 0
	}

	apierr.WriteSuccess(w, http.StatusOK, toDeviceBindingResponse(updated, historyCount))
}

// GetIMEIHistory handles GET /api/v1/sims/{id}/imei-history (API-330).
func (h *DeviceBindingHandler) GetIMEIHistory(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context missing")
		return
	}

	idStr := chi.URLParam(r, "id")
	simID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid SIM ID format")
		return
	}

	q := r.URL.Query()

	cursor := q.Get("cursor")

	limit := 50
	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n <= 0 {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidParam, "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}

	var since *time.Time
	if sinceStr := q.Get("since"); sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidParam, "since must be RFC3339 format")
			return
		}
		since = &t
	}

	var protocol *string
	if proto := q.Get("protocol"); proto != "" {
		if !validProtocols[proto] {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidParam, "protocol must be one of: radius, diameter_s6a, 5g_sba")
			return
		}
		protocol = &proto
	}

	rows, nextCursor, err := h.imeiHistoryStore.List(r.Context(), tenantID, simID, store.ListIMEIHistoryParams{
		Cursor:   cursor,
		Limit:    limit,
		Since:    since,
		Protocol: protocol,
	})
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", simID.String()).Msg("list imei history failed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Internal server error")
		return
	}

	items := make([]imeiHistoryRowResponse, len(rows))
	for i, row := range rows {
		items[i] = toIMEIHistoryRowResponse(row)
	}
	if items == nil {
		items = []imeiHistoryRowResponse{}
	}

	type historyMeta struct {
		NextCursor string `json:"next_cursor"`
		Limit      int    `json:"limit"`
		HasMore    bool   `json:"has_more"`
	}
	apierr.WriteJSON(w, http.StatusOK, apierr.SuccessResponse{
		Status: "success",
		Data:   items,
		Meta: historyMeta{
			NextCursor: nextCursor,
			Limit:      limit,
			HasMore:    nextCursor != "",
		},
	})
}

func (h *DeviceBindingHandler) createDeviceBindingAuditEntry(r *http.Request, entityID string, action string, before, after bindingAuditPayload, userID *uuid.UUID) {
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

	beforeData, _ := json.Marshal(before)
	afterData, _ := json.Marshal(after)

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
		h.logger.Warn().Err(auditErr).Msg("device binding audit entry failed")
	}
}
