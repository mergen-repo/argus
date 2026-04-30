// Package sim — POST /api/v1/sims/compare handler.
//
// Cross-tenant lookup: simStore.GetByID is scoped by tenantID extracted from the
// JWT context. If either SIM does not exist within the caller's tenant the store
// returns ErrSIMNotFound. The handler maps that to 404 SIM_NOT_FOUND rather than
// 403 FORBIDDEN_CROSS_TENANT to prevent ID enumeration. FORBIDDEN_CROSS_TENANT is
// reserved for future explicit cross-tenant disclosure flows and is documented in
// ERROR_CODES.md.
package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type compareSimStore interface {
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*store.SIM, error)
}

type compareSessionStore interface {
	GetLastSessionBySIM(ctx context.Context, tenantID, simID uuid.UUID) (*store.RadiusSession, error)
}

type compareRequest struct {
	SimIDA string `json:"sim_id_a"`
	SimIDB string `json:"sim_id_b"`
}

type fieldDiff struct {
	Field  string `json:"field"`
	ValueA any    `json:"value_a"`
	ValueB any    `json:"value_b"`
	Equal  bool   `json:"equal"`
}

type compareResponse struct {
	SimA       simResponse `json:"sim_a"`
	SimB       simResponse `json:"sim_b"`
	Diff       []fieldDiff `json:"diff"`
	ComparedAt time.Time   `json:"compared_at"`
}

func diff(field string, a, b any) fieldDiff {
	eq := fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	return fieldDiff{Field: field, ValueA: a, ValueB: b, Equal: eq}
}

func stateChangedAt(s *store.SIM) *string {
	var t *time.Time
	if s.ActivatedAt != nil {
		t = s.ActivatedAt
	}
	if s.SuspendedAt != nil && (t == nil || s.SuspendedAt.After(*t)) {
		t = s.SuspendedAt
	}
	if s.TerminatedAt != nil && (t == nil || s.TerminatedAt.After(*t)) {
		t = s.TerminatedAt
	}
	if t == nil {
		v := s.UpdatedAt.Format(time.RFC3339)
		return &v
	}
	v := t.Format(time.RFC3339)
	return &v
}

func buildDiff(rA, rB simResponse, simA, simB *store.SIM, sessA, sessB *store.RadiusSession) []fieldDiff {
	diffs := []fieldDiff{
		diff("iccid", rA.ICCID, rB.ICCID),
		diff("imsi", rA.IMSI, rB.IMSI),
		diff("msisdn", rA.MSISDN, rB.MSISDN),
		diff("state", rA.State, rB.State),
		diff("state_changed_at", stateChangedAt(simA), stateChangedAt(simB)),
		diff("operator_id", rA.OperatorID, rB.OperatorID),
		diff("operator_name", rA.OperatorName, rB.OperatorName),
		diff("apn_id", rA.APNID, rB.APNID),
		diff("apn_name", rA.APNName, rB.APNName),
		diff("policy_version_id", rA.PolicyVersionID, rB.PolicyVersionID),
		diff("static_ip", rA.IPAddress, rB.IPAddress),
		diff("esim_profile_id", rA.ESimProfileID, rB.ESimProfileID),
	}

	var lastSessA, lastSessB any
	if sessA != nil {
		lastSessA = sessA.ID.String()
	}
	if sessB != nil {
		lastSessB = sessB.ID.String()
	}
	diffs = append(diffs, diff("last_session_id", lastSessA, lastSessB))

	return diffs
}

func (h *Handler) Compare(w http.ResponseWriter, r *http.Request) {
	doCompare(w, r, h.simStore, h.sessionStore, h.auditSvc, h, h.logger)
}

type simEnricher interface {
	enrichSIMResponse(ctx context.Context, tenantID uuid.UUID, sim *store.SIM, resp *simResponse)
}

func doCompare(
	w http.ResponseWriter,
	r *http.Request,
	ss compareSimStore,
	sessionStore compareSessionStore,
	auditSvc audit.Auditor,
	enricher simEnricher,
	logger zerolog.Logger,
) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req compareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	var valErrs []map[string]string
	if req.SimIDA == "" {
		valErrs = append(valErrs, map[string]string{"field": "sim_id_a", "message": "sim_id_a is required", "code": "required"})
	}
	if req.SimIDB == "" {
		valErrs = append(valErrs, map[string]string{"field": "sim_id_b", "message": "sim_id_b is required", "code": "required"})
	}
	if len(valErrs) > 0 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "Request validation failed", valErrs)
		return
	}

	idA, err := uuid.Parse(req.SimIDA)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id_a format")
		return
	}
	idB, err := uuid.Parse(req.SimIDB)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id_b format")
		return
	}

	if idA == idB {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "sim_id_a and sim_id_b must be different")
		return
	}

	simA, err := ss.GetByID(r.Context(), tenantID, idA)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found",
				[]map[string]string{{"field": "sim_id_a", "value": idA.String()}})
			return
		}
		logger.Error().Err(err).Str("sim_id_a", idA.String()).Msg("get sim A for compare")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	simB, err := ss.GetByID(r.Context(), tenantID, idB)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found",
				[]map[string]string{{"field": "sim_id_b", "value": idB.String()}})
			return
		}
		logger.Error().Err(err).Str("sim_id_b", idB.String()).Msg("get sim B for compare")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	rA := toSIMResponseBase(simA)
	rB := toSIMResponseBase(simB)
	if enricher != nil {
		enricher.enrichSIMResponse(r.Context(), tenantID, simA, &rA)
		enricher.enrichSIMResponse(r.Context(), tenantID, simB, &rB)
	}

	var sessA, sessB *store.RadiusSession
	if sessionStore != nil {
		sessA, _ = sessionStore.GetLastSessionBySIM(r.Context(), tenantID, idA)
		sessB, _ = sessionStore.GetLastSessionBySIM(r.Context(), tenantID, idB)
	}

	diffs := buildDiff(rA, rB, simA, simB, sessA, sessB)

	afterData, _ := json.Marshal(map[string]string{"sim_id_b": idB.String()})
	if auditSvc != nil {
		tenantIDVal := tenantID
		userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
		var userIDPtr *uuid.UUID
		if userID != uuid.Nil {
			userIDPtr = &userID
		}
		ip := r.RemoteAddr
		ua := r.UserAgent()
		var correlationID *uuid.UUID
		if cidStr, ok := r.Context().Value(apierr.CorrelationIDKey).(string); ok && cidStr != "" {
			if cid, err2 := uuid.Parse(cidStr); err2 == nil {
				correlationID = &cid
			}
		}
		_, auditErr := auditSvc.CreateEntry(r.Context(), audit.CreateEntryParams{
			TenantID:      tenantIDVal,
			UserID:        userIDPtr,
			Action:        "sim.compare",
			EntityType:    "sim",
			EntityID:      idA.String(),
			AfterData:     json.RawMessage(afterData),
			IPAddress:     &ip,
			UserAgent:     &ua,
			CorrelationID: correlationID,
		})
		if auditErr != nil {
			logger.Warn().Err(auditErr).Msg("audit entry failed for sim.compare")
		}
	}

	apierr.WriteSuccess(w, http.StatusOK, compareResponse{
		SimA:       rA,
		SimB:       rB,
		Diff:       diffs,
		ComparedAt: time.Now().UTC(),
	})
}
