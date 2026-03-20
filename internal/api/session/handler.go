package session

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/bus"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const timeFmt = "2006-01-02T15:04:05Z07:00"

type Handler struct {
	sessionMgr *session.Manager
	dmSender   *session.DMSender
	eventBus   *bus.EventBus
	auditSvc   audit.Auditor
	jobStore   *store.JobStore
	logger     zerolog.Logger
}

func NewHandler(
	sessionMgr *session.Manager,
	dmSender *session.DMSender,
	eventBus *bus.EventBus,
	auditSvc audit.Auditor,
	jobStore *store.JobStore,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		sessionMgr: sessionMgr,
		dmSender:   dmSender,
		eventBus:   eventBus,
		auditSvc:   auditSvc,
		jobStore:   jobStore,
		logger:     logger.With().Str("component", "session_handler").Logger(),
	}
}

type sessionDTO struct {
	ID            string  `json:"id"`
	SimID         string  `json:"sim_id"`
	TenantID      string  `json:"tenant_id"`
	OperatorID    string  `json:"operator_id"`
	APNID         string  `json:"apn_id,omitempty"`
	IMSI          string  `json:"imsi"`
	MSISDN        string  `json:"msisdn,omitempty"`
	AcctSessionID string  `json:"acct_session_id"`
	NASIP         string  `json:"nas_ip"`
	FramedIP      string  `json:"framed_ip,omitempty"`
	RATType       string  `json:"rat_type,omitempty"`
	State         string  `json:"state"`
	BytesIn       uint64  `json:"bytes_in"`
	BytesOut      uint64  `json:"bytes_out"`
	DurationSec   float64 `json:"duration_sec"`
	IPAddress     string  `json:"ip_address,omitempty"`
	StartedAt     string  `json:"started_at"`
}

type statsDTO struct {
	TotalActive    int64            `json:"total_active"`
	ByOperator     map[string]int64 `json:"by_operator"`
	ByAPN          map[string]int64 `json:"by_apn"`
	ByRATType      map[string]int64 `json:"by_rat_type"`
	AvgDurationSec float64          `json:"avg_duration_sec"`
	AvgBytes       float64          `json:"avg_bytes"`
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

func toSessionDTO(s *session.Session) sessionDTO {
	duration := time.Since(s.StartedAt).Seconds()
	if !s.EndedAt.IsZero() {
		duration = s.EndedAt.Sub(s.StartedAt).Seconds()
	}
	return sessionDTO{
		ID:            s.ID,
		SimID:         s.SimID,
		TenantID:      s.TenantID,
		OperatorID:    s.OperatorID,
		APNID:         s.APNID,
		IMSI:          s.IMSI,
		MSISDN:        s.MSISDN,
		AcctSessionID: s.AcctSessionID,
		NASIP:         s.NASIP,
		FramedIP:      s.FramedIP,
		RATType:       s.RATType,
		State:         s.SessionState,
		BytesIn:       s.BytesIn,
		BytesOut:      s.BytesOut,
		DurationSec:   duration,
		IPAddress:     s.FramedIP,
		StartedAt:     s.StartedAt.Format(timeFmt),
	}
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

	items := make([]sessionDTO, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, toSessionDTO(s))
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
		_, dmErr := h.dmSender.SendDM(r.Context(), session.DMRequest{
			NASIP:         nasIP,
			AcctSessionID: sess.AcctSessionID,
			IMSI:          sess.IMSI,
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
				h.dmSender.SendDM(r.Context(), session.DMRequest{
					NASIP:         nasIP,
					AcctSessionID: sess.AcctSessionID,
					IMSI:          sess.IMSI,
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
			"job_id": job.ID.String(),
			"type":   "bulk_session_disconnect",
		})
	}

	return job.ID.String()
}

func (h *Handler) publishSessionEnded(r *http.Request, sess *session.Session, cause string) {
	if h.eventBus == nil {
		return
	}

	payload := map[string]interface{}{
		"session_id":      sess.ID,
		"sim_id":          sess.SimID,
		"tenant_id":       sess.TenantID,
		"operator_id":     sess.OperatorID,
		"imsi":            sess.IMSI,
		"terminate_cause": cause,
		"ended_at":        time.Now().UTC().Format(time.RFC3339),
	}

	if err := h.eventBus.Publish(r.Context(), bus.SubjectSessionEnded, payload); err != nil {
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
