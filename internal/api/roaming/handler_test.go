package roaming

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

func minimalAgreement() *store.RoamingAgreement {
	start, _ := time.Parse("2006-01-02", "2025-01-01")
	end, _ := time.Parse("2006-01-02", "2026-01-01")
	now := time.Now()
	return &store.RoamingAgreement{
		ID:                  uuid.New(),
		TenantID:            uuid.New(),
		OperatorID:          uuid.New(),
		PartnerOperatorName: "Vodafone Test",
		AgreementType:       "international",
		SLATerms:            json.RawMessage(`{}`),
		CostTerms:           json.RawMessage(`{}`),
		StartDate:           start,
		EndDate:             end,
		AutoRenew:           true,
		State:               "active",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func TestToResponse_BasicFields(t *testing.T) {
	a := minimalAgreement()
	resp := toResponse(a)

	if resp.ID != a.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, a.ID.String())
	}
	if resp.TenantID != a.TenantID.String() {
		t.Errorf("TenantID = %q, want %q", resp.TenantID, a.TenantID.String())
	}
	if resp.OperatorID != a.OperatorID.String() {
		t.Errorf("OperatorID = %q, want %q", resp.OperatorID, a.OperatorID.String())
	}
	if resp.PartnerOperatorName != "Vodafone Test" {
		t.Errorf("PartnerOperatorName = %q", resp.PartnerOperatorName)
	}
	if resp.AgreementType != "international" {
		t.Errorf("AgreementType = %q", resp.AgreementType)
	}
	if resp.State != "active" {
		t.Errorf("State = %q", resp.State)
	}
	if resp.AutoRenew != true {
		t.Error("AutoRenew should be true")
	}
	if resp.StartDate != "2025-01-01" {
		t.Errorf("StartDate = %q", resp.StartDate)
	}
	if resp.EndDate != "2026-01-01" {
		t.Errorf("EndDate = %q", resp.EndDate)
	}
	if resp.TerminatedAt != nil {
		t.Error("TerminatedAt should be nil")
	}
	if resp.Notes != nil {
		t.Error("Notes should be nil")
	}
}

func TestToResponse_NilCreatedBy(t *testing.T) {
	a := minimalAgreement()
	a.CreatedBy = nil
	resp := toResponse(a)
	if resp.CreatedBy != nil {
		t.Error("CreatedBy should be nil when store field is nil")
	}
}

func TestToResponse_WithNotes(t *testing.T) {
	a := minimalAgreement()
	note := "Test note"
	a.Notes = &note
	resp := toResponse(a)
	if resp.Notes == nil || *resp.Notes != "Test note" {
		t.Error("Notes should be set when store field is set")
	}
}

func TestValidAgreementTypes(t *testing.T) {
	valid := []string{"national", "international", "MVNO"}
	for _, v := range valid {
		if !validAgreementTypes[v] {
			t.Errorf("%q should be a valid agreement type", v)
		}
	}
	if validAgreementTypes["roaming"] {
		t.Error("roaming should not be a valid agreement type")
	}
	if validAgreementTypes[""] {
		t.Error("empty string should not be a valid agreement type")
	}
}

func TestValidStates(t *testing.T) {
	valid := []string{"draft", "active", "expired", "terminated"}
	for _, v := range valid {
		if !validStates[v] {
			t.Errorf("%q should be a valid state", v)
		}
	}
	if validStates["inactive"] {
		t.Error("inactive should not be a valid state")
	}
	if validStates[""] {
		t.Error("empty string should not be a valid state")
	}
}

func TestListMissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/roaming-agreements", nil)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("List without tenant = %d, want 403", w.Code)
	}
}

func TestGetInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/roaming-agreements/not-a-uuid", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-uuid")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Get(invalid id) = %d, want 400", w.Code)
	}
}

func TestGetMissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/roaming-agreements/"+uuid.New().String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Get without tenant = %d, want 403", w.Code)
	}
}

func TestCreateInvalidJSON(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/roaming-agreements", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	ctx := withTenantCtx(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Create(invalid json) = %d, want 400", w.Code)
	}
}

func TestCreateMissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/roaming-agreements", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Create without tenant = %d, want 403", w.Code)
	}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "empty body",
			body:     `{}`,
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name:     "missing partner_operator_name",
			body:     `{"operator_id":"` + uuid.New().String() + `","agreement_type":"international","start_date":"2025-01-01","end_date":"2026-01-01"}`,
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name:     "missing operator_id",
			body:     `{"partner_operator_name":"Test","agreement_type":"international","start_date":"2025-01-01","end_date":"2026-01-01"}`,
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name:     "invalid agreement_type",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"Test","agreement_type":"unknown","start_date":"2025-01-01","end_date":"2026-01-01"}`,
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name:     "missing start_date",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"Test","agreement_type":"national","end_date":"2026-01-01"}`,
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name:     "missing end_date",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"Test","agreement_type":"national","start_date":"2025-01-01"}`,
			wantCode: http.StatusUnprocessableEntity,
		},
		{
			name:     "invalid operator_id uuid",
			body:     `{"operator_id":"not-a-uuid","partner_operator_name":"Test","agreement_type":"national","start_date":"2025-01-01","end_date":"2026-01-01"}`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{logger: zerolog.Nop()}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/roaming-agreements", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Create(%s) = %d, want %d, body: %s", tt.name, w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

func TestCreateInvalidDates(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "bad start_date format",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"T","agreement_type":"national","start_date":"15-01-2025","end_date":"2026-01-01"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "bad end_date format",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"T","agreement_type":"national","start_date":"2025-01-01","end_date":"notadate"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "end_date not after start_date",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"T","agreement_type":"national","start_date":"2026-01-01","end_date":"2025-01-01"}`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "end_date equals start_date",
			body:     `{"operator_id":"` + uuid.New().String() + `","partner_operator_name":"T","agreement_type":"national","start_date":"2025-06-01","end_date":"2025-06-01"}`,
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{logger: zerolog.Nop()}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/roaming-agreements", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("Create(%s) = %d, want %d", tt.name, w.Code, tt.wantCode)
			}
		})
	}
}

func TestCreateInvalidCostTerms(t *testing.T) {
	opID := uuid.New().String()
	baseBody := `{"operator_id":"` + opID + `","partner_operator_name":"T","agreement_type":"national","start_date":"2025-01-01","end_date":"2026-01-01","cost_terms":`

	tests := []struct {
		name      string
		costTerms string
	}{
		{"negative cost_per_mb", `{"cost_per_mb":-1,"currency":"USD","settlement_period":"monthly"}`},
		{"invalid currency short", `{"cost_per_mb":0,"currency":"US","settlement_period":"monthly"}`},
		{"invalid currency lowercase", `{"cost_per_mb":0,"currency":"usd","settlement_period":"monthly"}`},
		{"invalid cost_terms json", `{not valid json}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{logger: zerolog.Nop()}
			body := baseBody + tt.costTerms + `}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/roaming-agreements", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withTenantCtx(req.Context())
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			h.Create(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Create(%s) = %d, want 400, body: %s", tt.name, w.Code, w.Body.String())
			}
		})
	}
}

func TestUpdateInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/roaming-agreements/bad-id", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad-id")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Update(invalid id) = %d, want 400", w.Code)
	}
}

func TestUpdateInvalidJSON(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/roaming-agreements/"+id, strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Update(invalid json) = %d, want 400", w.Code)
	}
}

func TestUpdateInvalidAgreementType(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/roaming-agreements/"+id, strings.NewReader(`{"agreement_type":"bogus"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Update(invalid agreement_type) = %d, want 400", w.Code)
	}
}

func TestUpdateInvalidState(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/roaming-agreements/"+id, strings.NewReader(`{"state":"unknown_state"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Update(invalid state) = %d, want 400", w.Code)
	}
}

func TestUpdateMissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/roaming-agreements/"+id, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Update without tenant = %d, want 403", w.Code)
	}
}

func TestTerminateInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/roaming-agreements/bad", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Terminate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Terminate(invalid id) = %d, want 400", w.Code)
	}
}

func TestTerminateMissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/roaming-agreements/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Terminate(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Terminate without tenant = %d, want 403", w.Code)
	}
}

func TestListForOperatorInvalidID(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/operators/bad/roaming-agreements", nil)
	ctx := withTenantCtx(req.Context())
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "bad")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListForOperator(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ListForOperator(invalid id) = %d, want 400", w.Code)
	}
}

func TestListForOperatorMissingTenant(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/operators/"+id+"/roaming-agreements", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.ListForOperator(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("ListForOperator without tenant = %d, want 403", w.Code)
	}
}

func TestValidateCostTerms(t *testing.T) {
	h := &Handler{logger: zerolog.Nop()}

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"nil raw", "", false},
		{"valid terms", `{"cost_per_mb":0.05,"currency":"USD","settlement_period":"monthly"}`, false},
		{"zero cost", `{"cost_per_mb":0,"currency":"EUR","settlement_period":"quarterly"}`, false},
		{"negative cost", `{"cost_per_mb":-1,"currency":"USD","settlement_period":"monthly"}`, true},
		{"two letter currency", `{"cost_per_mb":0,"currency":"US","settlement_period":"monthly"}`, true},
		{"four letter currency", `{"cost_per_mb":0,"currency":"USDC","settlement_period":"monthly"}`, true},
		{"lowercase currency", `{"cost_per_mb":0,"currency":"usd","settlement_period":"monthly"}`, true},
		{"invalid json", `{not valid`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var raw json.RawMessage
			if tt.raw != "" {
				raw = json.RawMessage(tt.raw)
			}
			err := h.validateCostTerms(raw)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCostTerms(%s) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}
