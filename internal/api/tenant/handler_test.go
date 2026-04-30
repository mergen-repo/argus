package tenant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/audit"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func withChiURLParam(r *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestToTenantResponse(t *testing.T) {
	id := uuid.New()
	domain := "test.example.com"
	phone := "+905551234567"

	resp := tenantResponse{
		ID:           id.String(),
		Name:         "Test Tenant",
		Domain:       &domain,
		ContactEmail: "admin@test.com",
		ContactPhone: &phone,
		MaxSims:      100000,
		MaxApns:      100,
		MaxUsers:     50,
		State:        "active",
		CreatedAt:    "2026-03-20T00:00:00Z",
		UpdatedAt:    "2026-03-20T00:00:00Z",
	}

	if resp.ID != id.String() {
		t.Errorf("ID = %v, want %v", resp.ID, id.String())
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
	if *resp.Domain != "test.example.com" {
		t.Errorf("Domain = %q, want %q", *resp.Domain, "test.example.com")
	}
}

func TestValidTenantStateTransitions(t *testing.T) {
	tests := []struct {
		from    string
		to      string
		allowed bool
	}{
		{"active", "suspended", true},
		{"active", "terminated", false},
		{"active", "active", false},
		{"suspended", "active", true},
		{"suspended", "terminated", true},
		{"terminated", "active", false},
		{"terminated", "suspended", false},
	}

	for _, tt := range tests {
		t.Run(tt.from+"_to_"+tt.to, func(t *testing.T) {
			transitions := validTenantTransitions[tt.from]
			found := false
			for _, s := range transitions {
				if s == tt.to {
					found = true
					break
				}
			}
			if found != tt.allowed {
				t.Errorf("transition %s -> %s: got allowed=%v, want %v", tt.from, tt.to, found, tt.allowed)
			}
		})
	}
}

func TestCreateTenantValidation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, logger)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid json",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   apierr.CodeInvalidFormat,
		},
		{
			name:       "missing name",
			body:       `{"contact_email":"admin@test.com","admin_name":"Admin","admin_email":"admin@test.com","admin_initial_password":"password123"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "missing contact_email",
			body:       `{"name":"Test","admin_name":"Admin","admin_email":"admin@test.com","admin_initial_password":"password123"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "missing admin fields",
			body:       `{"name":"Test","contact_email":"admin@test.com"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "admin password too short",
			body:       `{"name":"Test","contact_email":"admin@test.com","admin_name":"Admin","admin_email":"admin@test.com","admin_initial_password":"short"}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
		{
			name:       "missing all",
			body:       `{}`,
			wantStatus: http.StatusUnprocessableEntity,
			wantCode:   apierr.CodeValidationError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, "super_admin")
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			h.Create(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}

			var resp apierr.ErrorResponse
			json.NewDecoder(rr.Body).Decode(&resp)
			if resp.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", resp.Error.Code, tt.wantCode)
			}
		})
	}
}

func TestGetTenantForbiddenForNonSuperAdmin(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, logger)

	otherTenantID := uuid.New()
	callerTenantID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+otherTenantID.String(), nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, callerTenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", otherTenantID.String())

	rr := httptest.NewRecorder()
	h.Get(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestUpdateTenantForbiddenForNonSuperAdminChangingLimits(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, logger)

	tenantID := uuid.New()

	tests := []struct {
		name string
		body string
	}{
		{"change max_sims", `{"max_sims": 500000}`},
		{"change max_apns", `{"max_apns": 200}`},
		{"change max_users", `{"max_users": 100}`},
		{"change state", `{"state": "suspended"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenants/"+tenantID.String(), strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := req.Context()
			ctx = context.WithValue(ctx, apierr.TenantIDKey, tenantID)
			ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
			ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
			req = req.WithContext(ctx)

			req = withChiURLParam(req, "id", tenantID.String())

			rr := httptest.NewRecorder()
			h.Update(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
			}
		})
	}
}

func TestStatsForbiddenForOtherTenant(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, logger)

	otherTenantID := uuid.New()
	callerTenantID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+otherTenantID.String()+"/stats", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, callerTenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "tenant_admin")
	req = req.WithContext(ctx)

	req = withChiURLParam(req, "id", otherTenantID.String())

	rr := httptest.NewRecorder()
	h.Stats(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestToTenantWithCountsResponse_PopulatesCounts(t *testing.T) {
	id := uuid.New()
	twc := store.TenantWithCounts{
		Tenant: store.Tenant{
			ID:           id,
			Name:         "Acme Corp",
			ContactEmail: "admin@acme.com",
			MaxSims:      100000,
			MaxApns:      100,
			MaxUsers:     50,
			State:        "active",
		},
		SimCount:  15,
		UserCount: 3,
	}

	resp := toTenantWithCountsResponse(&twc)

	if resp.ID != id.String() {
		t.Errorf("ID = %q, want %q", resp.ID, id.String())
	}
	if resp.SimCount != 15 {
		t.Errorf("SimCount = %d, want 15", resp.SimCount)
	}
	if resp.UserCount != 3 {
		t.Errorf("UserCount = %d, want 3", resp.UserCount)
	}
	if resp.Slug != "acme-corp" {
		t.Errorf("Slug = %q, want acme-corp", resp.Slug)
	}
}

func TestCreateTenantValidation_AdminPasswordTooShort(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, nil, logger)

	body := `{"name":"Test","contact_email":"contact@test.com","admin_name":"Admin","admin_email":"admin@test.com","admin_initial_password":"short"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.UserIDKey, uuid.New())
	ctx = context.WithValue(ctx, apierr.RoleKey, "super_admin")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.Create(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnprocessableEntity)
	}

	var resp apierr.ErrorResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != apierr.CodeValidationError {
		t.Errorf("code = %q, want %q", resp.Error.Code, apierr.CodeValidationError)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Acme Corp", "acme-corp"},
		{"Hello World 123", "hello-world-123"},
		{"  Leading Trailing  ", "leading-trailing"},
		{"special!@#chars", "special-chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPtrStr(t *testing.T) {
	val := "test"
	if ptrStr(&val) != "test" {
		t.Error("ptrStr should return the string value")
	}
	if ptrStr(nil) != "" {
		t.Error("ptrStr(nil) should return empty string")
	}
}

type captureAuditService struct {
	entries []audit.CreateEntryParams
}

func (m *captureAuditService) CreateEntry(ctx context.Context, p audit.CreateEntryParams) (*audit.Entry, error) {
	m.entries = append(m.entries, p)
	return &audit.Entry{}, nil
}

func TestEmitAuditForTenant_UsesTenantID(t *testing.T) {
	auditor := &captureAuditService{}
	logger := zerolog.New(zerolog.NewTestWriter(t))
	h := NewHandler(nil, auditor, logger)

	callerTenantID := uuid.New()
	newTenantID := uuid.New()
	callerUserID := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", nil)
	ctx := req.Context()
	ctx = context.WithValue(ctx, apierr.TenantIDKey, callerTenantID)
	ctx = context.WithValue(ctx, apierr.UserIDKey, callerUserID)
	req = req.WithContext(ctx)

	h.emitAuditForTenant(req, newTenantID, "tenant.create", "tenant", newTenantID.String(), nil, map[string]string{"name": "Test"})

	if len(auditor.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(auditor.entries))
	}

	entry := auditor.entries[0]
	if entry.TenantID != newTenantID {
		t.Errorf("audit TenantID = %v, want %v (new tenant, not caller's %v)", entry.TenantID, newTenantID, callerTenantID)
	}
	if entry.Action != "tenant.create" {
		t.Errorf("audit Action = %q, want %q", entry.Action, "tenant.create")
	}
	if entry.UserID == nil || *entry.UserID != callerUserID {
		t.Errorf("audit UserID should be caller's ID %v", callerUserID)
	}
}
