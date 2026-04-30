package audit

import (
	"encoding/json"
	"net/http"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func Emit(r *http.Request, log zerolog.Logger, auditor Auditor, action, entityType, entityID string, before, after any) {
	if auditor == nil {
		return
	}

	tenantID, _ := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	ip := r.RemoteAddr
	ua := r.UserAgent()

	var userID *uuid.UUID
	if ok && uid != uuid.Nil {
		userID = &uid
	}

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

	_, auditErr := auditor.CreateEntry(r.Context(), CreateEntryParams{
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		EntityType:    entityType,
		EntityID:      entityID,
		BeforeData:    beforeData,
		AfterData:     afterData,
		IPAddress:     &ip,
		UserAgent:     &ua,
		CorrelationID: correlationID,
	})
	if auditErr != nil {
		log.Warn().Err(auditErr).Str("action", action).Msg("audit entry failed")
	}
}
