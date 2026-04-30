package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	dsljson "github.com/btopcu/argus/internal/policy/dsl"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const timeFmt = "2006-01-02T15:04:05Z07:00"

type Handler struct {
	sessionMgr    *session.Manager
	dmSender      *session.DMSender
	eventBus      *bus.EventBus
	auditSvc      audit.Auditor
	auditStore    *store.AuditStore
	jobStore      *store.JobStore
	simStore      *store.SIMStore
	operatorStore *store.OperatorStore
	apnStore      *store.APNStore
	policyStore   *store.PolicyStore
	logger        zerolog.Logger
}

type HandlerOption func(*Handler)

func WithSIMStore(s *store.SIMStore) HandlerOption { return func(h *Handler) { h.simStore = s } }
func WithOperatorStore(s *store.OperatorStore) HandlerOption {
	return func(h *Handler) { h.operatorStore = s }
}
func WithAPNStore(s *store.APNStore) HandlerOption { return func(h *Handler) { h.apnStore = s } }
func WithPolicyStore(s *store.PolicyStore) HandlerOption {
	return func(h *Handler) { h.policyStore = s }
}
func WithAuditStore(s *store.AuditStore) HandlerOption {
	return func(h *Handler) { h.auditStore = s }
}

func NewHandler(
	sessionMgr *session.Manager,
	dmSender *session.DMSender,
	eventBus *bus.EventBus,
	auditSvc audit.Auditor,
	jobStore *store.JobStore,
	logger zerolog.Logger,
	opts ...HandlerOption,
) *Handler {
	h := &Handler{
		sessionMgr: sessionMgr,
		dmSender:   dmSender,
		eventBus:   eventBus,
		auditSvc:   auditSvc,
		jobStore:   jobStore,
		logger:     logger.With().Str("component", "session_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type sessionDTO struct {
	ID                  string  `json:"id"`
	SimID               string  `json:"sim_id"`
	TenantID            string  `json:"tenant_id"`
	OperatorID          string  `json:"operator_id"`
	OperatorName        string  `json:"operator_name,omitempty"`
	OperatorCode        string  `json:"operator_code,omitempty"`
	APNID               string  `json:"apn_id,omitempty"`
	APNName             string  `json:"apn_name,omitempty"`
	ICCID               string  `json:"iccid,omitempty"`
	IMSI                string  `json:"imsi"`
	MSISDN              string  `json:"msisdn,omitempty"`
	PolicyName          string  `json:"policy_name,omitempty"`
	PolicyVersionNumber int     `json:"policy_version_number,omitempty"`
	AcctSessionID       string  `json:"acct_session_id"`
	NASIP               string  `json:"nas_ip"`
	FramedIP            string  `json:"framed_ip,omitempty"`
	RATType             string  `json:"rat_type,omitempty"`
	State               string  `json:"state"`
	BytesIn             uint64  `json:"bytes_in"`
	BytesOut            uint64  `json:"bytes_out"`
	DurationSec         float64 `json:"duration_sec"`
	IPAddress           string  `json:"ip_address,omitempty"`
	StartedAt           string  `json:"started_at"`
}

type sorDecisionDTO struct {
	ChosenOperatorID string          `json:"chosen_operator_id,omitempty"`
	Scoring          []sorScoreEntry `json:"scoring,omitempty"`
}

type sorScoreEntry struct {
	OperatorID string  `json:"operator_id"`
	Score      float64 `json:"score"`
	Reason     string  `json:"reason,omitempty"`
}

type policyAppliedDTO struct {
	PolicyID         string  `json:"policy_id,omitempty"`
	PolicyName       string  `json:"policy_name,omitempty"`
	VersionID        string  `json:"version_id,omitempty"`
	VersionNumber    int     `json:"version_number,omitempty"`
	MatchedRules     []int   `json:"matched_rules"`
	CoAStatus        string  `json:"coa_status,omitempty"`
	CoASentAt        *string `json:"coa_sent_at,omitempty"`
	CoAFailureReason *string `json:"coa_failure_reason,omitempty"`
}

type quotaUsageDTO struct {
	LimitBytes uint64  `json:"limit_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	Pct        float64 `json:"pct"`
	ResetAt    *string `json:"reset_at,omitempty"`
}

type coaEntry struct {
	At              string  `json:"at"`
	Action          string  `json:"action,omitempty"`
	PolicyVersionID *string `json:"policy_version_id,omitempty"`
	Status          *string `json:"status,omitempty"`
}

type sessionDetailDTO struct {
	sessionDTO
	SorDecision   *sorDecisionDTO   `json:"sor_decision,omitempty"`
	PolicyApplied *policyAppliedDTO `json:"policy_applied,omitempty"`
	QuotaUsage    *quotaUsageDTO    `json:"quota_usage,omitempty"`
	CoaHistory    []coaEntry        `json:"coa_history"`
}

type topOperatorDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Code  string `json:"code"`
	Count int64  `json:"count"`
}

type statsDTO struct {
	TotalActive    int64            `json:"total_active"`
	ByOperator     map[string]int64 `json:"by_operator"`
	ByAPN          map[string]int64 `json:"by_apn"`
	ByRATType      map[string]int64 `json:"by_rat_type"`
	AvgDurationSec float64          `json:"avg_duration_sec"`
	AvgBytes       float64          `json:"avg_bytes"`
	TopOperator    *topOperatorDTO  `json:"top_operator,omitempty"`
}

type disconnectRequest struct {
	Reason *string `json:"reason,omitempty"`
}

type disconnectResponse struct {
	ID           string `json:"id"`
	State        string `json:"state"`
	TerminatedBy string `json:"terminated_by"`
}

type bulkDisconnectRequest struct {
	SegmentID *string  `json:"segment_id,omitempty"`
	SimIDs    []string `json:"sim_ids,omitempty"`
	Reason    string   `json:"reason"`
}

type bulkDisconnectResponse struct {
	JobID             *string `json:"job_id,omitempty"`
	DisconnectedCount *int    `json:"disconnected_count,omitempty"`
}

// stripCIDR removes an optional "/N" suffix. PostgreSQL INET columns
// round-trip as "10.0.0.1/32" even for host addresses; downstream
// consumers want the bare IP.
func stripCIDR(ip string) string {
	if i := strings.Index(ip, "/"); i >= 0 {
		return ip[:i]
	}
	return ip
}

func toSessionDTO(s *session.Session) sessionDTO {
	duration := time.Since(s.StartedAt).Seconds()
	if !s.EndedAt.IsZero() {
		duration = s.EndedAt.Sub(s.StartedAt).Seconds()
	}
	framedIP := stripCIDR(s.FramedIP)
	return sessionDTO{
		ID:            s.ID,
		SimID:         s.SimID,
		TenantID:      s.TenantID,
		OperatorID:    s.OperatorID,
		APNID:         s.APNID,
		IMSI:          s.IMSI,
		MSISDN:        s.MSISDN,
		AcctSessionID: s.AcctSessionID,
		NASIP:         stripCIDR(s.NASIP),
		FramedIP:      framedIP,
		RATType:       s.RATType,
		State:         s.SessionState,
		BytesIn:       s.BytesIn,
		BytesOut:      s.BytesOut,
		DurationSec:   duration,
		IPAddress:     framedIP,
		StartedAt:     s.StartedAt.Format(timeFmt),
	}
}

func applyEnrichedSIM(dto *sessionDTO, sim *store.SIMWithNames) {
	if sim.OperatorName != nil {
		dto.OperatorName = *sim.OperatorName
	}
	if sim.OperatorCode != nil {
		dto.OperatorCode = *sim.OperatorCode
	}
	if sim.APNName != nil {
		dto.APNName = *sim.APNName
	}
	if sim.PolicyName != nil {
		dto.PolicyName = *sim.PolicyName
	}
	if sim.PolicyVersionNumber != nil {
		dto.PolicyVersionNumber = *sim.PolicyVersionNumber
	}
	if dto.IMSI == "" {
		dto.IMSI = sim.IMSI
	}
	if dto.ICCID == "" {
		dto.ICCID = sim.ICCID
	}
	if dto.MSISDN == "" && sim.MSISDN != nil {
		dto.MSISDN = *sim.MSISDN
	}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Session ID is required")
		return
	}

	tenantIDStr := ""
	if tid, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		tenantIDStr = tid.String()
	}

	sess, err := h.sessionMgr.Get(r.Context(), sessionID)
	if err != nil {
		h.logger.Error().Err(err).Str("session_id", sessionID).Msg("get session")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if sess == nil {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Session not found")
		return
	}

	if tenantIDStr != "" && sess.TenantID != "" && sess.TenantID != tenantIDStr {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Session not found")
		return
	}

	dto := toSessionDTO(sess)

	if h.simStore != nil && sess.SimID != "" && tenantIDStr != "" {
		simID, parseErr := uuid.Parse(sess.SimID)
		tenantID, tenantParseErr := uuid.Parse(tenantIDStr)
		if parseErr == nil && tenantParseErr == nil {
			if enriched, err := h.simStore.GetByIDEnriched(r.Context(), tenantID, simID); err == nil {
				applyEnrichedSIM(&dto, enriched)
			} else if !errors.Is(err, store.ErrSIMNotFound) {
				h.logger.Warn().Err(err).Str("sim_id", sess.SimID).Msg("enrich session dto")
			}
		}
	}

	detail := h.enrichSessionDetailDTO(r.Context(), dto, sess, tenantIDStr)
	apierr.WriteSuccess(w, http.StatusOK, detail)
}

func (h *Handler) enrichSessionDetailDTO(ctx context.Context, dto sessionDTO, sess *session.Session, tenantIDStr string) sessionDetailDTO {
	detail := sessionDetailDTO{
		sessionDTO: dto,
		CoaHistory: []coaEntry{},
	}

	detail.SorDecision = h.enrichSorDecision(sess)
	detail.PolicyApplied = h.enrichPolicyApplied(ctx, sess, tenantIDStr)
	detail.QuotaUsage = h.enrichQuotaUsage(ctx, sess, detail.PolicyApplied)
	detail.CoaHistory = h.fetchCoaHistory(ctx, sess.ID)

	return detail
}

func (h *Handler) enrichSorDecision(sess *session.Session) *sorDecisionDTO {
	if len(sess.SorDecision) == 0 {
		return nil
	}
	var sor sorDecisionDTO
	if err := json.Unmarshal(sess.SorDecision, &sor); err != nil {
		h.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("enrich sor_decision: unmarshal failed")
		return nil
	}
	return &sor
}

func (h *Handler) enrichPolicyApplied(ctx context.Context, sess *session.Session, tenantIDStr string) *policyAppliedDTO {
	if h.policyStore == nil || sess.SimID == "" || tenantIDStr == "" {
		return nil
	}
	simID, err := uuid.Parse(sess.SimID)
	if err != nil {
		return nil
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return nil
	}
	detail, err := h.policyStore.GetAssignmentDetailBySIM(ctx, tenantID, simID)
	if err != nil {
		if !errors.Is(err, store.ErrAssignmentNotFound) {
			h.logger.Warn().Err(err).Str("sim_id", sess.SimID).Msg("enrich policy_applied: fetch failed")
		}
		return nil
	}
	pa := &policyAppliedDTO{
		PolicyID:     detail.PolicyID.String(),
		PolicyName:   detail.PolicyName,
		VersionID:    detail.PolicyVersionID.String(),
		VersionNumber: detail.VersionNumber,
		MatchedRules: []int{},
		CoAStatus:    detail.CoAStatus,
	}
	if detail.CoASentAt != nil {
		s := detail.CoASentAt.Format(timeFmt)
		pa.CoASentAt = &s
	}
	pa.CoAFailureReason = detail.CoAFailureReason
	return pa
}

func (h *Handler) enrichQuotaUsage(ctx context.Context, sess *session.Session, pa *policyAppliedDTO) *quotaUsageDTO {
	if pa == nil {
		return nil
	}
	if h.policyStore == nil {
		return nil
	}

	versionID, err := uuid.Parse(pa.VersionID)
	if err != nil {
		return nil
	}
	pv, err := h.policyStore.GetVersionByID(ctx, versionID)
	if err != nil {
		h.logger.Warn().Err(err).Str("version_id", pa.VersionID).Msg("enrich quota_usage: get policy version failed")
		return nil
	}

	if len(pv.CompiledRules) == 0 {
		return nil
	}

	var compiled dsljson.CompiledPolicy
	if err := json.Unmarshal(pv.CompiledRules, &compiled); err != nil {
		h.logger.Warn().Err(err).Str("version_id", pa.VersionID).Msg("enrich quota_usage: unmarshal compiled policy failed")
		return nil
	}

	if compiled.Charging == nil || compiled.Charging.Quota <= 0 {
		return nil
	}

	limitBytes := uint64(compiled.Charging.Quota)
	usedBytes := sess.BytesIn + sess.BytesOut

	var pct float64
	if limitBytes > 0 {
		pct = float64(usedBytes) / float64(limitBytes) * 100
		if pct > 100 {
			pct = 100
		}
	}

	return &quotaUsageDTO{
		LimitBytes: limitBytes,
		UsedBytes:  usedBytes,
		Pct:        pct,
	}
}

func (h *Handler) fetchCoaHistory(ctx context.Context, sessionID string) []coaEntry {
	entries := []coaEntry{}
	if h.auditStore == nil || sessionID == "" {
		return entries
	}

	tenantID := uuid.Nil
	if v, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID); ok {
		tenantID = v
	}
	if tenantID == uuid.Nil {
		return entries
	}

	results, _, err := h.auditStore.List(ctx, tenantID, store.ListAuditParams{
		EntityType: "session",
		EntityID:   sessionID,
		Actions:    []string{"session.coa_sent", "session.coa_ack", "session.coa_failed"},
		Limit:      50,
	})
	if err != nil {
		h.logger.Warn().Err(err).Str("session_id", sessionID).Msg("fetch coa_history: list audit failed")
		return entries
	}

	for _, e := range results {
		entry := coaEntry{
			At:     e.CreatedAt.Format(timeFmt),
			Action: e.Action,
		}
		if e.AfterData != nil {
			var meta map[string]interface{}
			if json.Unmarshal(e.AfterData, &meta) == nil {
				if pvid, ok := meta["policy_version_id"].(string); ok && pvid != "" {
					entry.PolicyVersionID = &pvid
				}
				if status, ok := meta["status"].(string); ok && status != "" {
					entry.Status = &status
				}
			}
		}
		entries = append(entries, entry)
	}

	return entries
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cursor := q.Get("cursor")

	limit := 50
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	tenantIDStr := ""
	if tid, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		tenantIDStr = tid.String()
	}

	filter := session.SessionFilter{
		TenantID:   tenantIDStr,
		SimID:      q.Get("sim_id"),
		OperatorID: q.Get("operator_id"),
		APNID:      q.Get("apn_id"),
	}

	if v := q.Get("min_duration"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.MinDuration = &n
		}
	}
	if v := q.Get("min_usage"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			filter.MinUsage = &n
		}
	}

	sessions, nextCursor, err := h.sessionMgr.ListActive(r.Context(), cursor, limit, filter)
	if err != nil {
		h.logger.Error().Err(err).Msg("list sessions")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	var simMap map[uuid.UUID]*store.SIMWithNames
	if h.simStore != nil && tenantIDStr != "" && len(sessions) > 0 {
		tenantID, parseErr := uuid.Parse(tenantIDStr)
		if parseErr == nil {
			seen := make(map[uuid.UUID]struct{}, len(sessions))
			simIDs := make([]uuid.UUID, 0, len(sessions))
			for _, s := range sessions {
				if s.SimID == "" {
					continue
				}
				sid, err := uuid.Parse(s.SimID)
				if err != nil {
					continue
				}
				if _, dup := seen[sid]; !dup {
					seen[sid] = struct{}{}
					simIDs = append(simIDs, sid)
				}
			}
			if len(simIDs) > 0 {
				m, err := h.simStore.GetManyByIDsEnriched(r.Context(), tenantID, simIDs)
				if err != nil {
					h.logger.Warn().Err(err).Msg("batch enrich session list")
				} else {
					simMap = m
				}
			}
		}
	}

	items := make([]sessionDTO, 0, len(sessions))
	for _, s := range sessions {
		dto := toSessionDTO(s)
		if simMap != nil && s.SimID != "" {
			if sid, err := uuid.Parse(s.SimID); err == nil {
				if enriched, ok := simMap[sid]; ok {
					applyEnrichedSIM(&dto, enriched)
				}
			}
		}
		items = append(items, dto)
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
}

func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	tenantIDStr := ""
	if tid, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID); ok && tid != uuid.Nil {
		tenantIDStr = tid.String()
	}

	stats, err := h.sessionMgr.Stats(r.Context(), tenantIDStr)
	if err != nil {
		h.logger.Error().Err(err).Msg("session stats")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dto := statsDTO{
		TotalActive:    stats.TotalActive,
		ByOperator:     stats.ByOperator,
		ByAPN:          stats.ByAPN,
		ByRATType:      stats.ByRATType,
		AvgDurationSec: stats.AvgDurationSec,
		AvgBytes:       stats.AvgBytes,
	}

	if len(stats.ByOperator) > 0 && h.operatorStore != nil {
		keys := make([]string, 0, len(stats.ByOperator))
		for k := range stats.ByOperator {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var topID string
		var topCount int64
		for _, k := range keys {
			if stats.ByOperator[k] > topCount {
				topCount = stats.ByOperator[k]
				topID = k
			}
		}

		if topID != "" {
			opID, parseErr := uuid.Parse(topID)
			if parseErr == nil {
				op, opErr := h.operatorStore.GetByID(r.Context(), opID)
				if opErr == nil {
					dto.TopOperator = &topOperatorDTO{
						ID:    op.ID.String(),
						Name:  op.Name,
						Code:  op.Code,
						Count: topCount,
					}
				} else if !errors.Is(opErr, store.ErrOperatorNotFound) {
					h.logger.Warn().Err(opErr).Str("operator_id", topID).Msg("top_operator lookup")
				}
			}
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, dto)
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Session ID is required")
		return
	}

	var req disconnectRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	sess, err := h.sessionMgr.Get(r.Context(), sessionID)
	if err != nil {
		h.logger.Error().Err(err).Str("session_id", sessionID).Msg("get session for disconnect")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if sess == nil {
		apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Session not found")
		return
	}

	if sess.SessionState != "active" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeConflict, "Session is not active")
		return
	}

	if h.dmSender != nil && sess.NASIP != "" && sess.AcctSessionID != "" {
		nasIP := sess.NASIP
		if idx := strings.Index(nasIP, ":"); idx > 0 {
			nasIP = nasIP[:idx]
		}
		tenantID, _ := uuid.Parse(sess.TenantID)
		_, dmErr := h.dmSender.SendDM(r.Context(), session.DMRequest{
			NASIP:         nasIP,
			AcctSessionID: sess.AcctSessionID,
			IMSI:          sess.IMSI,
			SessionID:     sess.ID,
			TenantID:      tenantID,
		})
		if dmErr != nil {
			h.logger.Warn().Err(dmErr).Str("session_id", sessionID).Msg("DM send failed, terminating locally")
		}
	}

	reason := "admin_disconnect"
	if req.Reason != nil && *req.Reason != "" {
		reason = *req.Reason
	}

	if err := h.sessionMgr.Terminate(r.Context(), sessionID, reason); err != nil {
		h.logger.Error().Err(err).Str("session_id", sessionID).Msg("terminate session")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	terminatedBy := "system"
	userIDStr, _ := r.Context().Value(apierr.UserIDKey).(string)
	if userIDStr != "" {
		terminatedBy = userIDStr
	}

	h.publishSessionEnded(r, sess, reason)
	h.createAuditEntry(r, "session.disconnect", sessionID, reason)

	apierr.WriteSuccess(w, http.StatusOK, disconnectResponse{
		ID:           sessionID,
		State:        "terminated",
		TerminatedBy: terminatedBy,
	})
}

func (h *Handler) BulkDisconnect(w http.ResponseWriter, r *http.Request) {
	var req bulkDisconnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.Reason == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "reason", "message": "Reason is required", "code": "required"}})
		return
	}

	if len(req.SimIDs) == 0 && req.SegmentID == nil {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed",
			[]map[string]interface{}{{"field": "sim_ids", "message": "Either sim_ids or segment_id is required", "code": "required"}})
		return
	}

	if len(req.SimIDs) > 100 || req.SegmentID != nil {
		jobID := h.createBulkDisconnectJob(r, req)
		if jobID != "" {
			apierr.WriteJSON(w, http.StatusAccepted, apierr.SuccessResponse{
				Status: "success",
				Data:   bulkDisconnectResponse{JobID: &jobID},
			})
			return
		}
	}

	count := 0
	for _, simID := range req.SimIDs {
		sessions, err := h.sessionMgr.GetSessionsForSIM(r.Context(), simID)
		if err != nil {
			h.logger.Warn().Err(err).Str("sim_id", simID).Msg("get sessions for sim during bulk disconnect")
			continue
		}

		for _, sess := range sessions {
			if sess.SessionState != "active" {
				continue
			}

			if h.dmSender != nil && sess.NASIP != "" && sess.AcctSessionID != "" {
				nasIP := sess.NASIP
				if idx := strings.Index(nasIP, ":"); idx > 0 {
					nasIP = nasIP[:idx]
				}
				tenantID, _ := uuid.Parse(sess.TenantID)
				h.dmSender.SendDM(r.Context(), session.DMRequest{
					NASIP:         nasIP,
					AcctSessionID: sess.AcctSessionID,
					IMSI:          sess.IMSI,
					SessionID:     sess.ID,
					TenantID:      tenantID,
				})
			}

			if err := h.sessionMgr.Terminate(r.Context(), sess.ID, req.Reason); err != nil {
				h.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("terminate during bulk disconnect")
				continue
			}

			h.publishSessionEnded(r, sess, req.Reason)
			count++
		}
	}

	h.createAuditEntry(r, "session.bulk_disconnect", "", req.Reason)

	apierr.WriteSuccess(w, http.StatusOK, bulkDisconnectResponse{
		DisconnectedCount: &count,
	})
}

func (h *Handler) createBulkDisconnectJob(r *http.Request, req bulkDisconnectRequest) string {
	if h.jobStore == nil {
		return ""
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"sim_ids":    req.SimIDs,
		"segment_id": req.SegmentID,
		"reason":     req.Reason,
	})

	userID := h.userIDFromCtx(r)
	job, err := h.jobStore.Create(r.Context(), store.CreateJobParams{
		Type:      "bulk_session_disconnect",
		Priority:  5,
		Payload:   payload,
		CreatedBy: userID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("create bulk disconnect job")
		return ""
	}

	if h.eventBus != nil {
		h.eventBus.Publish(r.Context(), bus.SubjectJobQueue, map[string]interface{}{
			"job_id":    job.ID.String(),
			"tenant_id": job.TenantID.String(),
			"type":      "bulk_session_disconnect",
		})
	}

	return job.ID.String()
}

func (h *Handler) publishSessionEnded(r *http.Request, sess *session.Session, cause string) {
	if h.eventBus == nil {
		return
	}

	env := bus.NewSessionEnvelope("session.ended", sess.TenantID, sess.SimID, sess.ICCID, "Session ended (operator)").
		WithMeta("session_id", sess.ID).
		WithMeta("operator_id", sess.OperatorID).
		WithMeta("imsi", sess.IMSI).
		WithMeta("termination_cause", cause).
		WithMeta("ended_at", time.Now().UTC().Format(time.RFC3339))

	if err := h.eventBus.Publish(r.Context(), bus.SubjectSessionEnded, env); err != nil {
		h.logger.Warn().Err(err).Str("session_id", sess.ID).Msg("publish session.ended event failed")
	}
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID, reason string) {
	if h.auditSvc == nil {
		return
	}

	tenantID, err := store.TenantIDFromContext(r.Context())
	if err != nil {
		return
	}

	afterData, _ := json.Marshal(map[string]interface{}{
		"reason": reason,
	})

	userID := h.userIDFromCtx(r)
	ip := r.RemoteAddr
	ua := r.UserAgent()

	var correlationID *uuid.UUID
	if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
		if cid, parseErr := uuid.Parse(cidStr); parseErr == nil {
			correlationID = &cid
		}
	}

	_, auditErr := h.auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    "session",
		EntityID:      entityID,
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		h.logger.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}

func (h *Handler) userIDFromCtx(r *http.Request) *uuid.UUID {
	userIDStr, _ := r.Context().Value(apierr.UserIDKey).(string)
	if uid, err := uuid.Parse(userIDStr); err == nil {
		return &uid
	}
	return nil
}
