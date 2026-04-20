package esim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	aaasession "github.com/btopcu/argus/internal/aaa/session"
	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	esimpkg "github.com/btopcu/argus/internal/esim"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type activeSessionLister interface {
	ListActiveBySIM(ctx context.Context, simID uuid.UUID) ([]store.RadiusSession, error)
}

type dmDispatcher interface {
	SendDM(ctx context.Context, req aaasession.DMRequest) (*aaasession.DMResult, error)
}

type ipPoolReleaser interface {
	GetIPAddressByID(ctx context.Context, id uuid.UUID) (*store.IPAddress, error)
	ReleaseIP(ctx context.Context, poolID, simID uuid.UUID) error
}

type eventPublisher interface {
	Publish(ctx context.Context, subject string, payload interface{}) error
}

type Handler struct {
	esimStore    *store.ESimProfileStore
	simStore     *store.SIMStore
	smdpAdapter  esimpkg.SMDPAdapter
	auditSvc     audit.Auditor
	sessionStore activeSessionLister
	dmSender     dmDispatcher
	ipPoolStore  ipPoolReleaser
	eventBus     eventPublisher
	logger       zerolog.Logger
}

func NewHandler(
	esimStore *store.ESimProfileStore,
	simStore *store.SIMStore,
	smdpAdapter esimpkg.SMDPAdapter,
	auditSvc audit.Auditor,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		esimStore:   esimStore,
		simStore:    simStore,
		smdpAdapter: smdpAdapter,
		auditSvc:    auditSvc,
		logger:      logger.With().Str("component", "esim_handler").Logger(),
	}
}

// SetSessionDeps wires the active session lister and DM dispatcher used by the
// Switch handler to disconnect active sessions before changing eSIM profiles.
// Safe to call after construction; when either dep is nil the DM step is skipped.
func (h *Handler) SetSessionDeps(sessionStore activeSessionLister, dmSender dmDispatcher) {
	h.sessionStore = sessionStore
	h.dmSender = dmSender
}

// SetIPPoolStore wires the IP pool store used by Switch to release IPs after a profile switch.
func (h *Handler) SetIPPoolStore(s ipPoolReleaser) {
	h.ipPoolStore = s
}

// SetEventBus wires the event bus used by Switch to emit profile-switched events.
func (h *Handler) SetEventBus(b eventPublisher) {
	h.eventBus = b
}

type profileResponse struct {
	ID                string  `json:"id"`
	SimID             string  `json:"sim_id"`
	EID               string  `json:"eid"`
	SMDPPlusID        *string `json:"sm_dp_plus_id,omitempty"`
	ProfileID         *string `json:"profile_id,omitempty"`
	OperatorID        string  `json:"operator_id"`
	OperatorName      string  `json:"operator_name,omitempty"`
	OperatorCode      string  `json:"operator_code,omitempty"`
	ProfileState      string  `json:"profile_state"`
	ICCIDOnProfile    *string `json:"iccid_on_profile,omitempty"`
	LastProvisionedAt *string `json:"last_provisioned_at,omitempty"`
	LastError         *string `json:"last_error,omitempty"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type switchResponse struct {
	SimID                string                   `json:"sim_id"`
	OldProfile           profileResponse          `json:"old_profile"`
	NewProfile           profileResponse          `json:"new_profile"`
	NewOperatorID        string                   `json:"new_operator_id"`
	DisconnectedSessions []map[string]interface{} `json:"disconnected_sessions,omitempty"`
	IPReleased           bool                     `json:"ip_released"`
	PolicyCleared        bool                     `json:"policy_cleared"`
}

type switchRequest struct {
	TargetProfileID string `json:"target_profile_id"`
}

type createProfileRequest struct {
	SimID          string  `json:"sim_id"`
	EID            string  `json:"eid"`
	OperatorID     string  `json:"operator_id"`
	ICCIDOnProfile *string `json:"iccid_on_profile"`
	ProfileID      *string `json:"profile_id"`
}

func toProfileResponse(p *store.ESimProfile) profileResponse {
	resp := profileResponse{
		ID:             p.ID.String(),
		SimID:          p.SimID.String(),
		EID:            p.EID,
		SMDPPlusID:     p.SMDPPlusID,
		ProfileID:      p.ProfileID,
		OperatorID:     p.OperatorID.String(),
		ProfileState:   p.ProfileState,
		ICCIDOnProfile: p.ICCIDOnProfile,
		LastError:      p.LastError,
		CreatedAt:      p.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:      p.UpdatedAt.Format(time.RFC3339Nano),
	}
	if p.LastProvisionedAt != nil {
		v := p.LastProvisionedAt.Format(time.RFC3339Nano)
		resp.LastProvisionedAt = &v
	}
	return resp
}

func toProfileResponseEnriched(p *store.ESimProfileWithNames) profileResponse {
	resp := toProfileResponse(&p.ESimProfile)
	if p.OperatorName != nil {
		resp.OperatorName = *p.OperatorName
	}
	if p.OperatorCode != nil {
		resp.OperatorCode = *p.OperatorCode
	}
	return resp
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
		parsed, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format")
			return
		}
		simID = &parsed
	}

	var operatorID *uuid.UUID
	if v := q.Get("operator_id"); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
			return
		}
		operatorID = &parsed
	}

	params := store.ListESimProfilesParams{
		Cursor:     q.Get("cursor"),
		Limit:      limit,
		SimID:      simID,
		OperatorID: operatorID,
		State:      q.Get("state"),
	}

	profiles, nextCursor, err := h.esimStore.ListEnriched(r.Context(), tenantID, params)
	if err != nil {
		h.logger.Error().Err(err).Msg("list esim profiles")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	items := make([]profileResponse, 0, len(profiles))
	for _, p := range profiles {
		items = append(items, toProfileResponseEnriched(&p))
	}

	apierr.WriteList(w, http.StatusOK, items, apierr.ListMeta{
		Cursor:  nextCursor,
		Limit:   limit,
		HasMore: nextCursor != "",
	})
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
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid eSIM profile ID format")
		return
	}

	profile, err := h.esimStore.GetByIDEnriched(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("get esim profile")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, toProfileResponseEnriched(profile))
}

func (h *Handler) Enable(w http.ResponseWriter, r *http.Request) {
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

	existing, err := h.esimStore.GetByID(r.Context(), tenantID, profileID)
	if err != nil {
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("get esim profile for enable")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	sim, err := h.simStore.GetByID(r.Context(), tenantID, existing.SimID)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", existing.SimID.String()).Msg("get sim for esim enable")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if sim.SimType != "esim" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeNotESIM, "SIM is not an eSIM type")
		return
	}

	if h.smdpAdapter != nil {
		smdpErr := h.smdpAdapter.EnableProfile(r.Context(), esimpkg.EnableProfileRequest{
			EID:       existing.EID,
			ProfileID: profileID,
		})
		if smdpErr != nil {
			h.logger.Warn().Err(smdpErr).Str("profile_id", idStr).Msg("SM-DP+ enable profile failed (continuing)")
		}
	}

	userID := userIDFromCtx(r)

	profile, err := h.esimStore.Enable(r.Context(), tenantID, profileID, userID)
	if err != nil {
		if errors.Is(err, store.ErrProfileAlreadyEnabled) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeProfileAlreadyEnabled,
				"Another profile is already enabled for this SIM")
			return
		}
		if errors.Is(err, store.ErrInvalidProfileState) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidProfileState,
				fmt.Sprintf("Cannot enable profile in '%s' state", existing.ProfileState))
			return
		}
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("enable esim profile")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "esim_profile.enable", profileID.String(), existing, profile, userID)

	// TODO(FIX-216+): switch to GetByIDEnriched for consistency
	apierr.WriteSuccess(w, http.StatusOK, toProfileResponse(profile))
}

func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
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

	existing, err := h.esimStore.GetByID(r.Context(), tenantID, profileID)
	if err != nil {
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("get esim profile for disable")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.smdpAdapter != nil {
		smdpErr := h.smdpAdapter.DisableProfile(r.Context(), esimpkg.DisableProfileRequest{
			EID:       existing.EID,
			ProfileID: profileID,
		})
		if smdpErr != nil {
			h.logger.Warn().Err(smdpErr).Str("profile_id", idStr).Msg("SM-DP+ disable profile failed (continuing)")
		}
	}

	userID := userIDFromCtx(r)

	profile, err := h.esimStore.Disable(r.Context(), tenantID, profileID, userID)
	if err != nil {
		if errors.Is(err, store.ErrInvalidProfileState) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidProfileState,
				fmt.Sprintf("Cannot disable profile in '%s' state", existing.ProfileState))
			return
		}
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("disable esim profile")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	h.createAuditEntry(r, "esim_profile.disable", profileID.String(), existing, profile, userID)

	// TODO(FIX-216+): switch to GetByIDEnriched for consistency
	apierr.WriteSuccess(w, http.StatusOK, toProfileResponse(profile))
}

func (h *Handler) Switch(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	idStr := chi.URLParam(r, "id")
	sourceProfileID, err := uuid.Parse(idStr)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid eSIM profile ID format")
		return
	}

	var req switchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	if req.TargetProfileID == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "target_profile_id is required")
		return
	}

	targetProfileID, err := uuid.Parse(req.TargetProfileID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid target_profile_id format")
		return
	}

	sourceProfile, err := h.esimStore.GetByID(r.Context(), tenantID, sourceProfileID)
	if err != nil {
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Source eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("get source esim profile for switch")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	sim, err := h.simStore.GetByID(r.Context(), tenantID, sourceProfile.SimID)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", sourceProfile.SimID.String()).Msg("get sim for esim switch")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if sim.SimType != "esim" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeNotESIM, "SIM is not an eSIM type")
		return
	}

	force := r.URL.Query().Get("force") == "true"
	dmResults, nakSessionID, dmErr := h.disconnectActiveSessionsForSwitch(r.Context(), sim.ID, sim.IMSI, force)
	if dmErr != nil {
		h.logger.Error().Err(dmErr).Str("sim_id", sim.ID.String()).Msg("list active sessions for esim switch")
	}
	if nakSessionID != "" {
		apierr.WriteError(w, http.StatusConflict, apierr.CodeSessionDisconnectFailed,
			fmt.Sprintf("NAS refused DM for session %s; pass force=true to override", nakSessionID))
		return
	}

	if h.smdpAdapter != nil {
		smdpErr := h.smdpAdapter.DisableProfile(r.Context(), esimpkg.DisableProfileRequest{
			EID:       sourceProfile.EID,
			ProfileID: sourceProfileID,
		})
		if smdpErr != nil {
			h.logger.Warn().Err(smdpErr).Str("profile_id", idStr).Msg("SM-DP+ disable source profile failed (continuing)")
		}

		targetProfile, _ := h.esimStore.GetByID(r.Context(), tenantID, targetProfileID)
		if targetProfile != nil {
			smdpErr = h.smdpAdapter.EnableProfile(r.Context(), esimpkg.EnableProfileRequest{
				EID:       targetProfile.EID,
				ProfileID: targetProfileID,
			})
			if smdpErr != nil {
				h.logger.Warn().Err(smdpErr).Str("profile_id", targetProfileID.String()).Msg("SM-DP+ enable target profile failed (continuing)")
			}
		}
	}

	oldIPAddressID := sim.IPAddressID

	userID := userIDFromCtx(r)

	result, err := h.esimStore.Switch(r.Context(), tenantID, sourceProfileID, targetProfileID, userID)
	if err != nil {
		if errors.Is(err, store.ErrSameProfile) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeSameProfile,
				"Source and target profiles are the same")
			return
		}
		if errors.Is(err, store.ErrDifferentSIM) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeDifferentSIM,
				"Source and target profiles belong to different SIMs")
			return
		}
		if errors.Is(err, store.ErrInvalidProfileState) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeInvalidProfileState,
				"Invalid profile state for switch operation")
			return
		}
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("source", idStr).Str("target", req.TargetProfileID).Msg("switch esim profiles")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	ipReleased := false
	if oldIPAddressID != nil && h.ipPoolStore != nil {
		addr, addrErr := h.ipPoolStore.GetIPAddressByID(r.Context(), *oldIPAddressID)
		if addrErr != nil {
			h.logger.Warn().Err(addrErr).Str("ip_address_id", oldIPAddressID.String()).Msg("get ip address for release after switch (skipping)")
		} else {
			if releaseErr := h.ipPoolStore.ReleaseIP(r.Context(), addr.PoolID, result.SimID); releaseErr != nil {
				h.logger.Warn().Err(releaseErr).Str("pool_id", addr.PoolID.String()).Str("sim_id", result.SimID.String()).Msg("release IP after esim switch failed (non-blocking)")
			} else {
				ipReleased = true
			}
		}
	}

	if h.eventBus != nil {
		evtErr := h.eventBus.Publish(r.Context(), "esim.profile.switched", map[string]interface{}{
			"sim_id":          result.SimID.String(),
			"old_profile_id":  sourceProfileID.String(),
			"new_profile_id":  targetProfileID.String(),
			"new_operator_id": result.NewOperatorID.String(),
			"timestamp":       time.Now().UTC(),
		})
		if evtErr != nil {
			h.logger.Warn().Err(evtErr).Msg("publish esim.profile.switched event failed (non-blocking)")
		}
	}

	auditAfter := map[string]interface{}{"switch_result": result, "ip_released": ipReleased}
	if len(dmResults) > 0 {
		auditAfter["disconnected_sessions"] = dmResults
	}
	h.createAuditEntry(r, "esim_profile.switch", sourceProfileID.String(),
		map[string]interface{}{"source_profile_id": sourceProfileID, "target_profile_id": targetProfileID, "force": force},
		auditAfter, userID)

	resp := switchResponse{
		SimID:                result.SimID.String(),
		OldProfile:           toProfileResponse(result.OldProfile),
		NewProfile:           toProfileResponse(result.NewProfile),
		NewOperatorID:        result.NewOperatorID.String(),
		DisconnectedSessions: dmResults,
		IPReleased:           ipReleased,
		PolicyCleared:        true,
	}

	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	var req createProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Request body is not valid JSON")
		return
	}

	simID, err := uuid.Parse(req.SimID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid sim_id format")
		return
	}

	if req.EID == "" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, "eid is required")
		return
	}

	operatorID, err := uuid.Parse(req.OperatorID)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid operator_id format")
		return
	}

	sim, err := h.simStore.GetByID(r.Context(), tenantID, simID)
	if err != nil {
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", req.SimID).Msg("get sim for esim create")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if sim.SimType != "esim" {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeNotESIM, "SIM is not an eSIM type")
		return
	}

	count, err := h.esimStore.CountBySIM(r.Context(), tenantID, simID)
	if err != nil {
		h.logger.Error().Err(err).Str("sim_id", req.SimID).Msg("count esim profiles for sim")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}
	if count >= 8 {
		apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeProfileLimitExceeded, "Maximum profile limit (8) reached for this SIM")
		return
	}

	if h.smdpAdapter != nil {
		iccid := ""
		if req.ICCIDOnProfile != nil {
			iccid = *req.ICCIDOnProfile
		}
		_, smdpErr := h.smdpAdapter.DownloadProfile(r.Context(), esimpkg.DownloadProfileRequest{
			EID:        req.EID,
			OperatorID: operatorID,
			ICCID:      iccid,
		})
		if smdpErr != nil {
			h.logger.Warn().Err(smdpErr).Str("sim_id", req.SimID).Msg("SM-DP+ download profile failed (continuing)")
		}
	}

	params := store.CreateESimProfileParams{
		SimID:          simID,
		OperatorID:     operatorID,
		EID:            req.EID,
		ICCIDOnProfile: req.ICCIDOnProfile,
		ProfileID:      req.ProfileID,
	}

	profile, err := h.esimStore.Create(r.Context(), tenantID, params)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateProfile) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeDuplicateProfile, "A profile with this ICCID already exists for this SIM")
			return
		}
		if errors.Is(err, store.ErrSIMNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "SIM not found")
			return
		}
		h.logger.Error().Err(err).Str("sim_id", req.SimID).Msg("create esim profile")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromCtx(r)
	h.createAuditEntry(r, "esim_profile.create", profile.ID.String(), nil, profile, userID)

	// TODO(FIX-216+): switch to GetByIDEnriched for consistency
	apierr.WriteSuccess(w, http.StatusCreated, toProfileResponse(profile))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
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

	existing, err := h.esimStore.GetByID(r.Context(), tenantID, profileID)
	if err != nil {
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("get esim profile for delete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if h.smdpAdapter != nil {
		smdpErr := h.smdpAdapter.DeleteProfile(r.Context(), esimpkg.DeleteProfileRequest{
			EID:       existing.EID,
			ProfileID: profileID,
		})
		if smdpErr != nil {
			h.logger.Warn().Err(smdpErr).Str("profile_id", idStr).Msg("SM-DP+ delete profile failed (continuing)")
		}
	}

	deleted, err := h.esimStore.SoftDelete(r.Context(), tenantID, profileID)
	if err != nil {
		if errors.Is(err, store.ErrCannotDeleteEnabled) {
			apierr.WriteError(w, http.StatusConflict, apierr.CodeCannotDeleteEnabled, "Cannot delete an enabled profile; disable it first")
			return
		}
		if errors.Is(err, store.ErrESimProfileNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "eSIM profile not found")
			return
		}
		h.logger.Error().Err(err).Str("profile_id", idStr).Msg("soft delete esim profile")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	userID := userIDFromCtx(r)
	h.createAuditEntry(r, "esim_profile.delete", profileID.String(), existing, deleted, userID)

	// TODO(FIX-216+): switch to GetByIDEnriched for consistency
	apierr.WriteSuccess(w, http.StatusOK, toProfileResponse(deleted))
}

// disconnectActiveSessionsForSwitch sends DMs for every active session on the SIM
// before an eSIM profile switch. Returns the per-session DM result list, the
// acct_session_id of the first session that NAK'd (empty if none), and any lookup
// error from ListActiveBySIM (non-fatal). When force is true, DM is skipped entirely.
// When either the session store or DM sender is unset, the call is a no-op.
func (h *Handler) disconnectActiveSessionsForSwitch(ctx context.Context, simID uuid.UUID, imsi string, force bool) ([]map[string]interface{}, string, error) {
	if force || h.sessionStore == nil || h.dmSender == nil {
		return nil, "", nil
	}

	sessions, listErr := h.sessionStore.ListActiveBySIM(ctx, simID)
	if listErr != nil {
		return nil, "", listErr
	}

	var dmResults []map[string]interface{}
	for _, sess := range sessions {
		if sess.NASIP == nil || *sess.NASIP == "" || sess.AcctSessionID == nil || *sess.AcctSessionID == "" {
			h.logger.Warn().Str("session_id", sess.ID.String()).Msg("skip DM: missing NAS IP or acct session ID")
			continue
		}
		nasIP := *sess.NASIP
		if idx := strings.Index(nasIP, ":"); idx > 0 {
			nasIP = nasIP[:idx]
		}
		dmRes, dmErr := h.dmSender.SendDM(ctx, aaasession.DMRequest{
			NASIP:         nasIP,
			AcctSessionID: *sess.AcctSessionID,
			IMSI:          imsi,
			SessionID:     sess.ID.String(),
			TenantID:      sess.TenantID,
		})
		status := aaasession.DMResultError
		if dmErr == nil && dmRes != nil {
			status = dmRes.Status
		}
		dmResults = append(dmResults, map[string]interface{}{
			"session_id":      sess.ID.String(),
			"acct_session_id": *sess.AcctSessionID,
			"dm_status":       status,
		})
		if dmErr == nil && dmRes != nil && dmRes.Status == aaasession.DMResultNAK {
			return dmResults, *sess.AcctSessionID, nil
		}
	}
	return dmResults, "", nil
}

func (h *Handler) createAuditEntry(r *http.Request, action, entityID string, before, after interface{}, userID *uuid.UUID) {
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
		EntityType:    "esim_profile",
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

func userIDFromCtx(r *http.Request) *uuid.UUID {
	uid, ok := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
	if !ok || uid == uuid.Nil {
		return nil
	}
	return &uid
}
