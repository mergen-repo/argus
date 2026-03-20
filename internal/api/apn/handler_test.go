package apn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func withTenantCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, apierr.TenantIDKey, uuid.New())
}

func TestToAPNResponse(t *testing.T) {
	now := time.Now()
	policyID := uuid.New()
	createdBy := uuid.New()
	displayName := "IoT Fleet"

	a := &store.APN{
		ID:                uuid.New(),
		TenantID:          uuid.New(),
		OperatorID:        uuid.New(),
		Name:              "iot.fleet",
		DisplayName:       &displayName,
		APNType:           "private_managed",
		SupportedRATTypes: []string{"lte", "nr_5g"},
		DefaultPolicyID:   &policyID,
		State:             "active",
		Settings:          json.RawMessage(`{}`),
		CreatedAt:         now,
		UpdatedAt:         now,
		CreatedBy:         &createdBy,
	}

	resp := toAPNResponse(a)

	if resp.ID != a.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, a.ID.String())
	}
	if resp.TenantID != a.TenantID.String() {
		t.Errorf("TenantID = %q, want %q", resp.TenantID, a.TenantID.String())
	}
	if resp.OperatorID != a.OperatorID.String() {
		t.Errorf("OperatorID = %q, want %q", resp.OperatorID, a.OperatorID.String())
	}
	if resp.Name != "iot.fleet" {
		t.Errorf("Name = %q, want %q", resp.Name, "iot.fleet")
	}
	if resp.DisplayName == nil || *resp.DisplayName != "IoT Fleet" {
		t.Error("DisplayName should be 'IoT Fleet'")
	}
	if resp.APNType != "private_managed" {
		t.Errorf("APNType = %q, want %q", resp.APNType, "private_managed")
	}
	if len(resp.SupportedRATTypes) != 2 {
		t.Errorf("SupportedRATTypes len = %d, want 2", len(resp.SupportedRATTypes))
	}
	if resp.DefaultPolicyID == nil || *resp.DefaultPolicyID != policyID.String() {
		t.Error("DefaultPolicyID should match")
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
	if resp.CreatedBy == nil || *resp.CreatedBy != createdBy.String() {
		t.Error("CreatedBy should match")
	}
	if resp.UpdatedBy != nil {
		t.Error("UpdatedBy should be nil when not set")
	}
}

func TestToAPNResponseNilRATTypes(t *testing.T) {
	a := &store.APN{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Settings:  json.RawMessage(`{}`),
	}

	resp := toAPNResponse(a)
	if resp.SupportedRATTypes == nil {
		t.Error("SupportedRATTypes should never be nil in response")
	}
	if len(resp.SupportedRATTypes) != 0 {
		t.Errorf("SupportedRATTypes len = %d, want 0", len(resp.SupportedRATTypes))
	}
}

func TestValidAPNTypes(t *testing.T) {
	valid := []string{"private_managed", "operator_managed", "customer_managed"}
	for _, v := range valid {
		if !validAPNTypes[v] {
			t.Errorf("%q should be a valid APN type", v)
		}
	}

	if validAPNTypes["public"] {
		t.Error("public should not be a valid APN type")
	}
	if validAPNTypes[""] {
		t.Error("empty string should not be a valid APN type")
	}
}

func TestValidRATTypes(t *testing.T) {
	valid := []string{"nb_iot", "lte_m", "lte", "nr_5g"}
	for _, v := range valid {
		if !validRATTypes[v] {
			t.Errorf("%q should be a valid RAT type", v)
		}
	}

	if validRATTypes["3g"] {
		t.Error("3g should not be a valid RAT type")
	}
	if validRATTypes[""] {
		t.Error("empty string should not be a valid RAT type")
	}
}

func TestCreateAPNValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: 422,
		},
		{
			name:     "missing name",
			body:     `{"operator_id":"` + uuid.New().String() + `","apn_type":"private_managed"}`,
			wantCode: 422,
		},
		{
			name:     "missing operator_id",
			body:     `{"name":"test","apn_type":"private_managed"}`,
			wantCode: 422,
		},
		{
			name:     "missing apn_type",
			body:     `{"name":"test","operator_id":"` + uuid.New().String() + `"}`,
			wantCode: 422,
		},
		{
			name:     "invalid apn_type",
			body:     `{"name":"test","operator_id":"` + uuid.New().String() + `","apn_type":"public"}`,
			wantCode: 422,
		},
		{
			name:     "invalid rat_type",
			body:     `{"name":"test","operator_id":"` + uuid.New().String() + `","apn_type":"private_managed","supported_rat_types":["3g"]}`,
			wantCode: 422,
		},
		{
			name:     "name too long",
			body:     `{"name":"` + strings.Repeat("a", 101) + `","operator_id":"` + uuid.New().String() + `","apn_type":"private_managed"}`,
			wantCode: 422,
		},
		{
			name:     "invalid json",
			body:     `not json`,
			wantCode: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/apns", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Create(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestUpdateAPNValidation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		body     string
		wantCode int
	}{
		{
			name:     "invalid id format",
			id:       "not-a-uuid",
			body:     `{"display_name":"test"}`,
			wantCode: 400,
		},
		{
			name:     "invalid json",
			id:       uuid.New().String(),
			body:     `not json`,
			wantCode: 400,
		},
		{
			name:     "invalid rat_type in update",
			id:       uuid.New().String(),
			body:     `{"supported_rat_types":["unknown"]}`,
			wantCode: 422,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				logger: zerolog.Nop(),
			}

			req := httptest.NewRequest(http.MethodPatch, "/api/v1/apns/"+tt.id, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Update(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Update(%s) status = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestArchiveAPNInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/apns/not-uuid", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Archive(w, req)

	if w.Code != 400 {
		t.Errorf("Archive(invalid id) status = %d, want 400", w.Code)
	}
}

func TestGetAPNInvalidID(t *testing.T) {
	h := &Handler{
		logger: zerolog.Nop(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apns/not-uuid", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != 400 {
		t.Errorf("Get(invalid id) status = %d, want 400", w.Code)
	}
}
