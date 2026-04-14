package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Mocks

type mockSessionStore struct {
	CreateFn             func(ctx context.Context, tenantID, startedBy uuid.UUID) (*store.OnboardingSession, error)
	GetByIDFn            func(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error)
	GetLatestByTenantFn  func(ctx context.Context, tenantID uuid.UUID) (*store.OnboardingSession, error)
	UpdateStepFn         func(ctx context.Context, id uuid.UUID, stepN int, stepData []byte, newCurrentStep int) error
	MarkCompletedFn      func(ctx context.Context, id uuid.UUID) error

	UpdateStepCalled bool
}

func (m *mockSessionStore) Create(ctx context.Context, tenantID, startedBy uuid.UUID) (*store.OnboardingSession, error) {
	return m.CreateFn(ctx, tenantID, startedBy)
}
func (m *mockSessionStore) GetByID(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error) {
	return m.GetByIDFn(ctx, id)
}
func (m *mockSessionStore) GetLatestByTenant(ctx context.Context, tenantID uuid.UUID) (*store.OnboardingSession, error) {
	if m.GetLatestByTenantFn != nil {
		return m.GetLatestByTenantFn(ctx, tenantID)
	}
	// Match real store semantics: not-found returns (nil, nil), not an error.
	return nil, nil
}
func (m *mockSessionStore) UpdateStep(ctx context.Context, id uuid.UUID, stepN int, stepData []byte, newCurrentStep int) error {
	m.UpdateStepCalled = true
	if m.UpdateStepFn != nil {
		return m.UpdateStepFn(ctx, id, stepN, stepData, newCurrentStep)
	}
	return nil
}
func (m *mockSessionStore) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	if m.MarkCompletedFn != nil {
		return m.MarkCompletedFn(ctx, id)
	}
	return nil
}

type mockTenantsService struct {
	UpdateFn func(ctx context.Context, id uuid.UUID, p store.UpdateTenantParams) (*store.Tenant, error)
}

func (m *mockTenantsService) Update(ctx context.Context, id uuid.UUID, p store.UpdateTenantParams) (*store.Tenant, error) {
	return m.UpdateFn(ctx, id, p)
}

type mockUsersService struct {
	CreateUserFn func(ctx context.Context, p store.CreateUserParams) (*store.User, error)
}

func (m *mockUsersService) CreateUser(ctx context.Context, p store.CreateUserParams) (*store.User, error) {
	return m.CreateUserFn(ctx, p)
}

type mockOperatorGrantsService struct {
	CreateGrantFn func(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID, supportedRATTypes []string) (*store.OperatorGrant, error)
}

func (m *mockOperatorGrantsService) CreateGrant(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID, supportedRATTypes []string) (*store.OperatorGrant, error) {
	return m.CreateGrantFn(ctx, tenantID, operatorID, grantedBy, supportedRATTypes)
}

type mockAPNService struct {
	CreateFn func(ctx context.Context, tenantID uuid.UUID, p store.CreateAPNParams) (*store.APN, error)
}

func (m *mockAPNService) Create(ctx context.Context, tenantID uuid.UUID, p store.CreateAPNParams) (*store.APN, error) {
	return m.CreateFn(ctx, tenantID, p)
}

type mockBulkImportService struct {
	EnqueueImportFn func(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, csvS3Key string) (string, error)
}

func (m *mockBulkImportService) EnqueueImport(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, csvS3Key string) (string, error) {
	return m.EnqueueImportFn(ctx, tenantID, userID, csvS3Key)
}

type mockNotifierService struct {
	NotifyFn func(ctx context.Context, req NotifyRequest) error
	Called   bool
}

func (m *mockNotifierService) Notify(ctx context.Context, req NotifyRequest) error {
	m.Called = true
	if m.NotifyFn != nil {
		return m.NotifyFn(ctx, req)
	}
	return nil
}

type mockAuditService struct {
	Called bool
}

func (m *mockAuditService) CreateEntry(ctx context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	m.Called = true
	return &audit.Entry{}, nil
}

type mockPolicyService struct {
	AssignDefaultFn func(ctx context.Context, tenantID uuid.UUID) error
	Called          bool
}

func (m *mockPolicyService) AssignDefault(ctx context.Context, tenantID uuid.UUID) error {
	m.Called = true
	if m.AssignDefaultFn != nil {
		return m.AssignDefaultFn(ctx, tenantID)
	}
	return nil
}

// Test helpers

var (
	testTenantID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testUserID   = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	testSessionID = uuid.MustParse("33333333-3333-3333-3333-333333333333")
)

func withTenantUser(r *http.Request, tenantID, userID uuid.UUID) *http.Request {
	ctx := context.WithValue(r.Context(), apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)
	return r.WithContext(ctx)
}

func newTestHandler(sessions *mockSessionStore) *Handler {
	return New(
		sessions,
		&mockTenantsService{
			UpdateFn: func(ctx context.Context, id uuid.UUID, p store.UpdateTenantParams) (*store.Tenant, error) {
				n := "Acme Corp"
				e := "admin@acme.io"
				return &store.Tenant{ID: testTenantID, Name: n, ContactEmail: e}, nil
			},
		},
		&mockUsersService{
			CreateUserFn: func(ctx context.Context, p store.CreateUserParams) (*store.User, error) {
				return &store.User{
					ID: uuid.New(), TenantID: testTenantID,
					Email: p.Email, Name: p.Name, Role: p.Role,
				}, nil
			},
		},
		&mockOperatorGrantsService{
			CreateGrantFn: func(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID, supportedRATTypes []string) (*store.OperatorGrant, error) {
				return &store.OperatorGrant{
					ID: uuid.New(), TenantID: tenantID, OperatorID: operatorID,
					Enabled: true, SupportedRATTypes: supportedRATTypes, GrantedAt: time.Now(),
				}, nil
			},
		},
		&mockAPNService{
			CreateFn: func(ctx context.Context, tenantID uuid.UUID, p store.CreateAPNParams) (*store.APN, error) {
				return &store.APN{ID: uuid.New(), TenantID: tenantID, Name: p.Name}, nil
			},
		},
		&mockBulkImportService{
			EnqueueImportFn: func(ctx context.Context, tenantID uuid.UUID, userID *uuid.UUID, csvS3Key string) (string, error) {
				return uuid.New().String(), nil
			},
		},
		nil,
		&mockNotifierService{},
		&mockAuditService{},
		zerolog.Nop(),
	)
}

func buildChiRequest(method, path string, body []byte, params map[string]string) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	return req.WithContext(ctx)
}

// Tests

func TestStart_ReturnsSessionID(t *testing.T) {
	sessions := &mockSessionStore{
		CreateFn: func(ctx context.Context, tenantID, startedBy uuid.UUID) (*store.OnboardingSession, error) {
			return &store.OnboardingSession{
				ID: testSessionID, TenantID: tenantID, StartedBy: startedBy,
				CurrentStep: 1, State: "in_progress",
			}, nil
		},
	}

	h := newTestHandler(sessions)
	req := httptest.NewRequest(http.MethodPost, "/onboarding/start", nil)
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.start(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp["data"].(map[string]interface{})
	if data["session_id"] != testSessionID.String() {
		t.Errorf("expected session_id %s, got %v", testSessionID.String(), data["session_id"])
	}
	if data["current_step"].(float64) != 1 {
		t.Errorf("expected current_step 1, got %v", data["current_step"])
	}
	if data["steps_total"].(float64) != 5 {
		t.Errorf("expected steps_total 5, got %v", data["steps_total"])
	}
}

func TestStep1_ValidBodyAdvancesStep(t *testing.T) {
	sessions := &mockSessionStore{}

	h := newTestHandler(sessions)

	body, _ := json.Marshal(map[string]string{
		"company_name":  "Acme Corp",
		"contact_email": "admin@acme.io",
		"locale":        "en",
	})

	req := buildChiRequest(http.MethodPost, "/onboarding/"+testSessionID.String()+"/step/1", body, map[string]string{
		"id": testSessionID.String(),
		"n":  "1",
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.step(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp["data"].(map[string]interface{})
	if data["current_step"].(float64) != 2 {
		t.Errorf("expected current_step 2, got %v", data["current_step"])
	}
	if !sessions.UpdateStepCalled {
		t.Error("expected UpdateStep to be called")
	}
}

func TestStep1_InvalidEmailReturns422SessionUnchanged(t *testing.T) {
	sessions := &mockSessionStore{}

	h := newTestHandler(sessions)

	body, _ := json.Marshal(map[string]string{
		"company_name":  "Acme Corp",
		"contact_email": "",
		"locale":        "en",
	})

	req := buildChiRequest(http.MethodPost, "/onboarding/"+testSessionID.String()+"/step/1", body, map[string]string{
		"id": testSessionID.String(),
		"n":  "1",
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.step(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "error" {
		t.Errorf("expected status 'error', got %v", resp["status"])
	}

	if sessions.UpdateStepCalled {
		t.Error("UpdateStep must NOT be called on validation failure")
	}
}

func TestStep3_OperatorGrantFailureRollsBackStepUnchanged(t *testing.T) {
	sessions := &mockSessionStore{}

	h := newTestHandler(sessions)
	h.OperatorGrants = &mockOperatorGrantsService{
		CreateGrantFn: func(ctx context.Context, tenantID, operatorID uuid.UUID, grantedBy *uuid.UUID, supportedRATTypes []string) (*store.OperatorGrant, error) {
			return nil, errors.New("operator connection failed")
		},
	}

	opID := uuid.New()
	body, _ := json.Marshal(map[string]interface{}{
		"operator_grants": []map[string]interface{}{
			{"operator_id": opID.String(), "enabled": true, "rat_types": []string{"LTE"}},
		},
	})

	req := buildChiRequest(http.MethodPost, "/onboarding/"+testSessionID.String()+"/step/3", body, map[string]string{
		"id": testSessionID.String(),
		"n":  "3",
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.step(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
	if sessions.UpdateStepCalled {
		t.Error("UpdateStep must NOT be called when operator grant fails")
	}
}

func TestGet_ReturnsSession(t *testing.T) {
	now := time.Now()
	sessions := &mockSessionStore{
		GetByIDFn: func(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error) {
			return &store.OnboardingSession{
				ID: testSessionID, TenantID: testTenantID,
				CurrentStep: 3, State: "in_progress", CreatedAt: now, UpdatedAt: now,
			}, nil
		},
	}

	h := newTestHandler(sessions)
	req := buildChiRequest(http.MethodGet, "/onboarding/"+testSessionID.String(), nil, map[string]string{
		"id": testSessionID.String(),
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp["data"].(map[string]interface{})
	if data["session_id"] != testSessionID.String() {
		t.Errorf("expected session_id %s, got %v", testSessionID.String(), data["session_id"])
	}
	if data["current_step"].(float64) != 3 {
		t.Errorf("expected current_step 3, got %v", data["current_step"])
	}
	if data["completed"].(bool) != false {
		t.Error("expected completed=false")
	}
}

func TestGet_NotFoundReturns404(t *testing.T) {
	sessions := &mockSessionStore{
		GetByIDFn: func(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error) {
			return nil, store.ErrNotFound
		},
	}

	h := newTestHandler(sessions)
	req := buildChiRequest(http.MethodGet, "/onboarding/"+testSessionID.String(), nil, map[string]string{
		"id": testSessionID.String(),
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestComplete_RequiresAllSteps_Returns409(t *testing.T) {
	sessions := &mockSessionStore{
		GetByIDFn: func(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error) {
			return &store.OnboardingSession{
				ID: testSessionID, TenantID: testTenantID,
				CurrentStep: 3, State: "in_progress",
			}, nil
		},
	}

	h := newTestHandler(sessions)
	req := buildChiRequest(http.MethodPost, "/onboarding/"+testSessionID.String()+"/complete", nil, map[string]string{
		"id": testSessionID.String(),
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.complete(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	errObj := resp["error"].(map[string]interface{})
	if errObj["code"] != "INCOMPLETE_STEPS" {
		t.Errorf("expected code INCOMPLETE_STEPS, got %v", errObj["code"])
	}
}

func TestComplete_SuccessCallsPolicyAndNotification(t *testing.T) {
	auditor := &mockAuditService{}
	notifier := &mockNotifierService{}
	policy := &mockPolicyService{}

	sessions := &mockSessionStore{
		GetByIDFn: func(ctx context.Context, id uuid.UUID) (*store.OnboardingSession, error) {
			return &store.OnboardingSession{
				ID: testSessionID, TenantID: testTenantID,
				CurrentStep: 6, State: "in_progress",
			}, nil
		},
		MarkCompletedFn: func(ctx context.Context, id uuid.UUID) error {
			return nil
		},
	}

	h := newTestHandler(sessions)
	h.Audit = auditor
	h.Notifier = notifier
	h.Policy = policy

	req := buildChiRequest(http.MethodPost, "/onboarding/"+testSessionID.String()+"/complete", nil, map[string]string{
		"id": testSessionID.String(),
	})
	req = withTenantUser(req, testTenantID, testUserID)
	w := httptest.NewRecorder()

	h.complete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !notifier.Called {
		t.Error("expected notifier.Notify to be called")
	}
	if !policy.Called {
		t.Error("expected policy.AssignDefault to be called")
	}
	if !auditor.Called {
		t.Error("expected audit.CreateEntry to be called")
	}
}
