package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Service interfaces — narrowly defined for what each step needs.

type SessionStore interface {
	Create(ctx context.Context, tenantID, startedBy uuid.UUID) (*store.OnboardingSession, error)
	GetByID(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error)
	UpdateStep(ctx context.Context, id uuid.UUID, stepN int, stepData []byte, newCurrentStep int) error
	MarkCompleted(ctx context.Context, id uuid.UUID) error
}

type TenantsService interface {
	Update(ctx context.Context, id uuid.UUID, p store.UpdateTenantParams) (*store.Tenant, error)
}

type UsersService interface {
	CreateUser(ctx context.Context, p store.CreateUserParams) (*store.User, error)
}

type OperatorGrantsService interface {
	CreateGrant(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID, supportedRATTypes []string) (*store.OperatorGrant, error)
}

type APNService interface {
	Create(ctx context.Context, tenantID uuid.UUID, p store.CreateAPNParams) (*store.APN, error)
}

type BulkImportService interface {
	EnqueueImport(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, csvS3Key string) (string, error)
}

// PolicyService assigns a default policy for a tenant after onboarding.
// This interface is optional — wizard step 5 (Policy Setup) is the primary path
// for tenant policy creation. AssignDefault is invoked only if a PolicyService
// implementation is wired (currently nil — see decisions.md DEC-069-POLICY).
type PolicyService interface {
	AssignDefault(ctx context.Context, tenantID uuid.UUID) error
}

type NotifierService interface {
	Notify(ctx context.Context, req NotifyRequest) error
}

type NotifyRequest struct {
	TenantID  uuid.UUID
	UserID    *uuid.UUID
	EventType string
	Title     string
	Body      string
	Severity  string
}

type AuditService = audit.Auditor

// Handler is the HTTP handler for the onboarding API.
type Handler struct {
	Sessions       SessionStore
	Tenants        TenantsService
	Users          UsersService
	OperatorGrants OperatorGrantsService
	APN            APNService
	BulkImport     BulkImportService
	Policy         PolicyService
	Notifier       NotifierService
	Audit          AuditService
	Logger         zerolog.Logger
}

func New(
	sessions SessionStore,
	tenants TenantsService,
	users UsersService,
	operatorGrants OperatorGrantsService,
	apn APNService,
	bulkImport BulkImportService,
	policy PolicyService,
	notifier NotifierService,
	auditSvc AuditService,
	logger zerolog.Logger,
) *Handler {
	return &Handler{
		Sessions:       sessions,
		Tenants:        tenants,
		Users:          users,
		OperatorGrants: operatorGrants,
		APN:            apn,
		BulkImport:     bulkImport,
		Policy:         policy,
		Notifier:       notifier,
		Audit:          auditSvc,
		Logger:         logger.With().Str("component", "onboarding_handler").Logger(),
	}
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/onboarding", func(r chi.Router) {
		r.Post("/start", h.start)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Post("/step/{n}", h.step)
			r.Post("/complete", h.complete)
		})
	})
}

// POST /api/v1/onboarding/start
func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	sess, err := h.Sessions.Create(r.Context(), tenantID, userID)
	if err != nil {
		h.Logger.Error().Err(err).Msg("create onboarding session")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusCreated, map[string]interface{}{
		"session_id":   sess.ID.String(),
		"current_step": sess.CurrentStep,
		"steps_total":  5,
	})
}

// GET /api/v1/onboarding/:id
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := parseSessionID(r)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid session ID format")
		return
	}

	sess, err := h.Sessions.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Onboarding session not found")
			return
		}
		h.Logger.Error().Err(err).Msg("get onboarding session")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	dataByStep := map[string]json.RawMessage{
		"step_1": sess.StepData[0],
		"step_2": sess.StepData[1],
		"step_3": sess.StepData[2],
		"step_4": sess.StepData[3],
		"step_5": sess.StepData[4],
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"session_id":   sess.ID.String(),
		"current_step": sess.CurrentStep,
		"data_by_step": dataByStep,
		"state":        sess.State,
		"completed":    sess.State == "completed",
	})
}

// POST /api/v1/onboarding/:id/step/:n
func (h *Handler) step(w http.ResponseWriter, r *http.Request) {
	id, err := parseSessionID(r)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid session ID format")
		return
	}

	nStr := chi.URLParam(r, "n")
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 || n > 5 {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Step number must be 1..5")
		return
	}

	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)

	var result interface{}
	var stepDataBytes []byte

	switch n {
	case 1:
		result, stepDataBytes, err = h.handleStep1(r, tenantID, userID)
	case 2:
		result, stepDataBytes, err = h.handleStep2(r, tenantID)
	case 3:
		result, stepDataBytes, err = h.handleStep3(r, tenantID, userID)
	case 4:
		result, stepDataBytes, err = h.handleStep4(r, tenantID, userID)
	case 5:
		result, stepDataBytes, err = h.handleStep5(r, tenantID, userID)
	}

	if err != nil {
		var ve *validationError
		if errors.As(err, &ve) {
			apierr.WriteError(w, http.StatusUnprocessableEntity, apierr.CodeValidationError, ve.Message, ve.Details)
			return
		}
		h.Logger.Error().Err(err).Int("step", n).Msg("handle onboarding step")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	newCurrentStep := n + 1
	if err := h.Sessions.UpdateStep(r.Context(), id, n, stepDataBytes, newCurrentStep); err != nil {
		h.Logger.Error().Err(err).Msg("update onboarding session step")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"session_id":   id.String(),
		"current_step": newCurrentStep,
		"step_result":  result,
	})
}

// POST /api/v1/onboarding/:id/complete
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	id, err := parseSessionID(r)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeInvalidFormat, "Invalid session ID format")
		return
	}

	tenantID, ok := r.Context().Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || tenantID == uuid.Nil {
		apierr.WriteError(w, http.StatusForbidden, apierr.CodeForbidden, "Tenant context required")
		return
	}

	sess, err := h.Sessions.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Onboarding session not found")
			return
		}
		h.Logger.Error().Err(err).Msg("get onboarding session for complete")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	if sess.CurrentStep < 6 {
		apierr.WriteError(w, http.StatusConflict, "INCOMPLETE_STEPS", "All 5 steps must be completed before finalizing")
		return
	}

	// Optional default-policy assignment for tenants that skipped wizard step 5.
	// When wizard step 5 (Policy Setup) is completed, the user-defined policy
	// supersedes the default. See decisions.md DEC-069-POLICY.
	if h.Policy != nil {
		if pErr := h.Policy.AssignDefault(r.Context(), tenantID); pErr != nil {
			h.Logger.Warn().Err(pErr).Msg("assign default policy failed (non-fatal)")
		}
	}

	if h.Notifier != nil {
		userID, _ := r.Context().Value(apierr.UserIDKey).(uuid.UUID)
		var uid *uuid.UUID
		if userID != uuid.Nil {
			uid = &userID
		}
		_ = h.Notifier.Notify(r.Context(), NotifyRequest{
			TenantID:  tenantID,
			UserID:    uid,
			EventType: "onboarding_completed",
			Title:     "Onboarding complete",
			Body:      "Your account onboarding has been completed successfully.",
			Severity:  "info",
		})
	}

	audit.Emit(r, h.Logger, h.Audit, "onboarding.completed", "onboarding_session", id.String(), nil, map[string]string{"state": "completed"})

	if err := h.Sessions.MarkCompleted(r.Context(), id); err != nil {
		h.Logger.Error().Err(err).Msg("mark onboarding session completed")
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "An unexpected error occurred")
		return
	}

	apierr.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"session_id": id.String(),
		"state":      "completed",
	})
}

// Step handlers

type step1Request struct {
	CompanyName   string  `json:"company_name"`
	ContactEmail  string  `json:"contact_email"`
	ContactPhone  *string `json:"contact_phone"`
	Locale        string  `json:"locale"`
}

func (h *Handler) handleStep1(r *http.Request, tenantID, _ uuid.UUID) (interface{}, []byte, error) {
	var req step1Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, &validationError{Message: "Request body is not valid JSON"}
	}

	var details []map[string]string
	if req.CompanyName == "" {
		details = append(details, map[string]string{"field": "company_name", "message": "required"})
	}
	if req.ContactEmail == "" {
		details = append(details, map[string]string{"field": "contact_email", "message": "required"})
	}
	if req.Locale != "tr" && req.Locale != "en" {
		details = append(details, map[string]string{"field": "locale", "message": "must be 'tr' or 'en'"})
	}
	if len(details) > 0 {
		return nil, nil, &validationError{Message: "Validation failed", Details: details}
	}

	name := req.CompanyName
	email := req.ContactEmail
	updParams := store.UpdateTenantParams{
		Name:         &name,
		ContactEmail: &email,
		ContactPhone: req.ContactPhone,
	}

	updated, err := h.Tenants.Update(r.Context(), tenantID, updParams)
	if err != nil {
		return nil, nil, err
	}

	stepData, _ := json.Marshal(req)
	return map[string]interface{}{
		"tenant_id":     updated.ID.String(),
		"company_name":  updated.Name,
		"contact_email": updated.ContactEmail,
	}, stepData, nil
}

type step2Request struct {
	AdminEmail    string `json:"admin_email"`
	AdminName     string `json:"admin_name"`
	AdminPassword string `json:"admin_password"`
	TOTPEnabled   bool   `json:"totp_enabled"`
}

func (h *Handler) handleStep2(r *http.Request, _ uuid.UUID) (interface{}, []byte, error) {
	var req step2Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, &validationError{Message: "Request body is not valid JSON"}
	}

	var details []map[string]string
	if req.AdminEmail == "" {
		details = append(details, map[string]string{"field": "admin_email", "message": "required"})
	}
	if req.AdminName == "" {
		details = append(details, map[string]string{"field": "admin_name", "message": "required"})
	}
	if len(req.AdminPassword) < 8 {
		details = append(details, map[string]string{"field": "admin_password", "message": "must be at least 8 characters"})
	}
	if len(details) > 0 {
		return nil, nil, &validationError{Message: "Validation failed", Details: details}
	}

	// NOTE(STORY-069): store.CreateUserParams does not have a password field.
	// The user is created in 'invited' state with empty password_hash.
	// admin_password is validated above but not persisted; password setup is deferred
	// to the user's first login flow. This is a deviation from the spec.
	user, err := h.Users.CreateUser(r.Context(), store.CreateUserParams{
		Email: req.AdminEmail,
		Name:  req.AdminName,
		Role:  "tenant_admin",
	})
	if err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			return nil, nil, &validationError{Message: "Email already exists", Details: []map[string]string{
				{"field": "admin_email", "message": "already exists"},
			}}
		}
		return nil, nil, err
	}

	// Omit password from stored step data
	storedData := map[string]interface{}{
		"admin_email":  req.AdminEmail,
		"admin_name":   req.AdminName,
		"totp_enabled": req.TOTPEnabled,
	}
	stepData, _ := json.Marshal(storedData)

	return map[string]interface{}{
		"user_id":      user.ID.String(),
		"admin_email":  user.Email,
		"admin_name":   user.Name,
		"totp_enabled": req.TOTPEnabled,
	}, stepData, nil
}

type operatorGrantInput struct {
	OperatorID string   `json:"operator_id"`
	Enabled    bool     `json:"enabled"`
	RATTypes   []string `json:"rat_types"`
}

type step3Request struct {
	OperatorGrants []operatorGrantInput `json:"operator_grants"`
}

func (h *Handler) handleStep3(r *http.Request, tenantID uuid.UUID, userID uuid.UUID) (interface{}, []byte, error) {
	var req step3Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, &validationError{Message: "Request body is not valid JSON"}
	}

	if len(req.OperatorGrants) == 0 {
		return nil, nil, &validationError{Message: "Validation failed", Details: []map[string]string{
			{"field": "operator_grants", "message": "at least one operator grant required"},
		}}
	}

	var uid *uuid.UUID
	if userID != uuid.Nil {
		uid = &userID
	}

	var createdGrants []map[string]interface{}
	for i, og := range req.OperatorGrants {
		opID, err := uuid.Parse(og.OperatorID)
		if err != nil {
			return nil, nil, &validationError{
				Message: "Validation failed",
				Details: []map[string]string{
					{"field": "operator_grants[" + strconv.Itoa(i) + "].operator_id", "message": "invalid UUID"},
				},
			}
		}

		grant, err := h.OperatorGrants.CreateGrant(r.Context(), tenantID, opID, uid, og.RATTypes)
		if err != nil {
			if errors.Is(err, store.ErrGrantExists) {
				return nil, nil, &validationError{
					Message: "Validation failed",
					Details: []map[string]string{
						{"field": "operator_grants[" + strconv.Itoa(i) + "].operator_id", "message": "grant already exists for this operator"},
					},
				}
			}
			return nil, nil, err
		}

		createdGrants = append(createdGrants, map[string]interface{}{
			"grant_id":    grant.ID.String(),
			"operator_id": grant.OperatorID.String(),
			"enabled":     grant.Enabled,
			"rat_types":   grant.SupportedRATTypes,
		})
	}

	stepData, _ := json.Marshal(req)
	return map[string]interface{}{
		"grants_created": len(createdGrants),
		"grants":         createdGrants,
	}, stepData, nil
}

type step4Request struct {
	APNName    string `json:"apn_name"`
	Realm      string `json:"realm"`
	IPPoolCIDR string `json:"ip_pool_cidr"`
	AuthType   string `json:"auth_type"`
}

func (h *Handler) handleStep4(r *http.Request, tenantID uuid.UUID, userID uuid.UUID) (interface{}, []byte, error) {
	var req step4Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, &validationError{Message: "Request body is not valid JSON"}
	}

	var details []map[string]string
	if req.APNName == "" {
		details = append(details, map[string]string{"field": "apn_name", "message": "required"})
	}
	if req.Realm == "" {
		details = append(details, map[string]string{"field": "realm", "message": "required"})
	}
	if req.IPPoolCIDR == "" {
		details = append(details, map[string]string{"field": "ip_pool_cidr", "message": "required"})
	}
	if req.AuthType == "" {
		details = append(details, map[string]string{"field": "auth_type", "message": "required"})
	}
	if len(details) > 0 {
		return nil, nil, &validationError{Message: "Validation failed", Details: details}
	}

	var uid *uuid.UUID
	if userID != uuid.Nil {
		uid = &userID
	}

	settings, _ := json.Marshal(map[string]string{
		"realm":        req.Realm,
		"ip_pool_cidr": req.IPPoolCIDR,
		"auth_type":    req.AuthType,
	})

	apn, err := h.APN.Create(r.Context(), tenantID, store.CreateAPNParams{
		Name:      req.APNName,
		APNType:   "standard",
		Settings:  settings,
		CreatedBy: uid,
	})
	if err != nil {
		return nil, nil, err
	}

	stepData, _ := json.Marshal(req)
	return map[string]interface{}{
		"apn_id":   apn.ID.String(),
		"apn_name": apn.Name,
		"realm":    req.Realm,
	}, stepData, nil
}

type step5Request struct {
	CSVS3Key string `json:"csv_s3_key"`
}

func (h *Handler) handleStep5(r *http.Request, tenantID uuid.UUID, userID uuid.UUID) (interface{}, []byte, error) {
	var req step5Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, nil, &validationError{Message: "Request body is not valid JSON"}
	}

	if req.CSVS3Key == "" {
		return nil, nil, &validationError{
			Message: "Validation failed",
			Details: []map[string]string{{"field": "csv_s3_key", "message": "required"}},
		}
	}

	var uid *uuid.UUID
	if userID != uuid.Nil {
		uid = &userID
	}

	jobID, err := h.BulkImport.EnqueueImport(r.Context(), tenantID, uid, req.CSVS3Key)
	if err != nil {
		return nil, nil, err
	}

	stepData, _ := json.Marshal(req)
	return map[string]interface{}{
		"job_id":     jobID,
		"csv_s3_key": req.CSVS3Key,
		"status":     "queued",
	}, stepData, nil
}

// Helpers

func parseSessionID(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "id"))
}

type validationError struct {
	Message string
	Details interface{}
}

func (e *validationError) Error() string {
	return e.Message
}
