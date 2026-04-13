package compliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	compliancesvc "github.com/btopcu/argus/internal/compliance"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// ─── fakes ───────────────────────────────────────────────────────────────────

type fakeJobStore struct {
	createdJob *store.Job
	err        error
}

func (f *fakeJobStore) CreateWithTenantID(_ context.Context, tenantID uuid.UUID, p store.CreateJobParams) (*store.Job, error) {
	if f.err != nil {
		return nil, f.err
	}
	job := &store.Job{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     p.Type,
		State:    "queued",
		Payload:  p.Payload,
	}
	f.createdJob = job
	return job, nil
}

type fakeBus struct {
	published []interface{}
}

func (b *fakeBus) Publish(_ context.Context, _ string, payload interface{}) error {
	b.published = append(b.published, payload)
	return nil
}

// portabilityJobStore / portabilityEventBus alias the prod interfaces so tests
// can pass fakes through setTestDeps.
type portabilityJobStore = handlerJobEnqueuer
type portabilityEventBus = handlerEventPublisher

// ─── tests ───────────────────────────────────────────────────────────────────

func TestRequestDataPortability_Returns202WithJobID(t *testing.T) {
	tenantID := uuid.New()
	callerID := uuid.New()
	targetUserID := uuid.New()

	fjs := &fakeJobStore{}
	fb := &fakeBus{}

	h := buildPortabilityHandler(fjs, fb)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/data-portability/"+targetUserID.String(), strings.NewReader("{}"))
	req = withTenantAndUser(req, tenantID, callerID, "tenant_admin")
	req = withChiParam(req, "user_id", targetUserID.String())

	w := httptest.NewRecorder()
	h.RequestDataPortability(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202", w.Code)
	}

	var resp apierr.SuccessResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("response status = %q, want success", resp.Status)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("resp.Data is not map: %T", resp.Data)
	}
	if data["status"] != "queued" {
		t.Errorf("data.status = %q, want queued", data["status"])
	}
	if _, hasJobID := data["job_id"]; !hasJobID {
		t.Error("response missing job_id")
	}

	if len(fb.published) == 0 {
		t.Error("no event published to job queue")
	}
}

func TestRequestDataPortability_SelfCanRequest(t *testing.T) {
	tenantID := uuid.New()
	callerID := uuid.New()

	fjs := &fakeJobStore{}
	fb := &fakeBus{}
	h := buildPortabilityHandler(fjs, fb)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/data-portability/"+callerID.String(), strings.NewReader("{}"))
	req = withTenantAndUser(req, tenantID, callerID, "api_user")
	req = withChiParam(req, "user_id", callerID.String())

	w := httptest.NewRecorder()
	h.RequestDataPortability(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("self request: status = %d, want 202", w.Code)
	}
}

func TestRequestDataPortability_NonSelfNonAdminReturns403(t *testing.T) {
	tenantID := uuid.New()
	callerID := uuid.New()
	otherUserID := uuid.New()

	fjs := &fakeJobStore{}
	fb := &fakeBus{}
	h := buildPortabilityHandler(fjs, fb)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/data-portability/"+otherUserID.String(), strings.NewReader("{}"))
	req = withTenantAndUser(req, tenantID, callerID, "api_user")
	req = withChiParam(req, "user_id", otherUserID.String())

	w := httptest.NewRecorder()
	h.RequestDataPortability(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("non-self non-admin: status = %d, want 403", w.Code)
	}
}

func TestRequestDataPortability_InvalidUserIDReturns400(t *testing.T) {
	tenantID := uuid.New()
	callerID := uuid.New()

	fjs := &fakeJobStore{}
	fb := &fakeBus{}
	h := buildPortabilityHandler(fjs, fb)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/data-portability/not-a-uuid", strings.NewReader("{}"))
	req = withTenantAndUser(req, tenantID, callerID, "tenant_admin")
	req = withChiParam(req, "user_id", "not-a-uuid")

	w := httptest.NewRecorder()
	h.RequestDataPortability(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid user_id: status = %d, want 400", w.Code)
	}
}

// ─── test helpers ─────────────────────────────────────────────────────────────

// buildPortabilityHandler creates a Handler wired with fake job+bus deps.
func buildPortabilityHandler(js portabilityJobStore, eb portabilityEventBus) *Handler {
	compStore := store.NewComplianceStore(nil)
	auditStore := store.NewAuditStore(nil)
	compSvc := compliancesvc.NewService(compStore, auditStore, nil, zerolog.Nop())
	tenantStore := store.NewTenantStore(nil)
	h := &Handler{
		complianceSvc: compSvc,
		tenantStore:   tenantStore,
		auditSvc:      nil,
		logger:        zerolog.Nop(),
	}
	h.setTestDeps(js, eb)
	return h
}

var _ = time.Now // keep time import used

func withTenantAndUser(r *http.Request, tenantID, userID uuid.UUID, role string) *http.Request {
	ctx := r.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, userID)
	ctx = context.WithValue(ctx, apierr.RoleKey, role)
	return r.WithContext(ctx)
}

func withChiParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
