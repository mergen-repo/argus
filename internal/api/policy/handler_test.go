package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btopcu/argus/internal/policy/rollout"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestToPolicyResponse(t *testing.T) {
	now := time.Now()
	scopeRef := uuid.New()
	currentVer := uuid.New()
	createdBy := uuid.New()
	desc := "Test description"

	p := &store.Policy{
		ID:               uuid.New(),
		TenantID:         uuid.New(),
		Name:             "test-policy",
		Description:      &desc,
		Scope:            "global",
		ScopeRefID:       &scopeRef,
		CurrentVersionID: &currentVer,
		State:            "active",
		CreatedAt:        now,
		UpdatedAt:        now,
		CreatedBy:        &createdBy,
	}

	resp := toPolicyResponse(p)

	if resp.ID != p.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, p.ID.String())
	}
	if resp.Name != "test-policy" {
		t.Errorf("Name = %q, want %q", resp.Name, "test-policy")
	}
	if resp.Description == nil || *resp.Description != desc {
		t.Errorf("Description = %v, want %q", resp.Description, desc)
	}
	if resp.Scope != "global" {
		t.Errorf("Scope = %q, want %q", resp.Scope, "global")
	}
	if resp.ScopeRefID == nil || *resp.ScopeRefID != scopeRef.String() {
		t.Error("ScopeRefID should be set")
	}
	if resp.CurrentVersionID == nil || *resp.CurrentVersionID != currentVer.String() {
		t.Error("CurrentVersionID should be set")
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
}

func TestToPolicyResponseNilOptionals(t *testing.T) {
	p := &store.Policy{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "simple",
		Scope:     "global",
		State:     "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	resp := toPolicyResponse(p)

	if resp.ScopeRefID != nil {
		t.Error("ScopeRefID should be nil")
	}
	if resp.CurrentVersionID != nil {
		t.Error("CurrentVersionID should be nil")
	}
	if resp.Description != nil {
		t.Error("Description should be nil")
	}
}

func TestToVersionResponse(t *testing.T) {
	now := time.Now()
	activatedAt := time.Now()
	simCount := 42
	createdBy := uuid.New()

	v := &store.PolicyVersion{
		ID:               uuid.New(),
		PolicyID:         uuid.New(),
		Version:          3,
		DSLContent:       `POLICY "test" { RULES { bandwidth_down = 1mbps } }`,
		CompiledRules:    json.RawMessage(`{"name":"test"}`),
		State:            "active",
		AffectedSIMCount: &simCount,
		ActivatedAt:      &activatedAt,
		CreatedAt:        now,
		CreatedBy:        &createdBy,
	}

	resp := toVersionResponse(v)

	if resp.ID != v.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, v.ID.String())
	}
	if resp.PolicyID != v.PolicyID.String() {
		t.Errorf("PolicyID = %q, want %q", resp.PolicyID, v.PolicyID.String())
	}
	if resp.Version != 3 {
		t.Errorf("Version = %d, want %d", resp.Version, 3)
	}
	if resp.State != "active" {
		t.Errorf("State = %q, want %q", resp.State, "active")
	}
	if resp.AffectedSIMCount == nil || *resp.AffectedSIMCount != 42 {
		t.Error("AffectedSIMCount should be 42")
	}
	if resp.ActivatedAt == nil {
		t.Error("ActivatedAt should be set")
	}
}

func TestToVersionResponseDraft(t *testing.T) {
	v := &store.PolicyVersion{
		ID:        uuid.New(),
		PolicyID:  uuid.New(),
		Version:   1,
		State:     "draft",
		CreatedAt: time.Now(),
	}

	resp := toVersionResponse(v)

	if resp.ActivatedAt != nil {
		t.Error("ActivatedAt should be nil for draft")
	}
	if resp.AffectedSIMCount != nil {
		t.Error("AffectedSIMCount should be nil")
	}
}

func TestToPolicyListItem(t *testing.T) {
	now := time.Now()
	currentVer := uuid.New()
	desc := "A description"

	p := &store.Policy{
		ID:               uuid.New(),
		TenantID:         uuid.New(),
		Name:             "list-policy",
		Description:      &desc,
		Scope:            "apn",
		CurrentVersionID: &currentVer,
		State:            "active",
		UpdatedAt:        now,
	}

	item := toPolicyListItem(p)

	if item.ID != p.ID.String() {
		t.Errorf("ID = %q, want %q", item.ID, p.ID.String())
	}
	if item.Name != "list-policy" {
		t.Errorf("Name = %q, want %q", item.Name, "list-policy")
	}
	if item.Scope != "apn" {
		t.Errorf("Scope = %q, want %q", item.Scope, "apn")
	}
	if item.CurrentVersionID == nil || *item.CurrentVersionID != currentVer.String() {
		t.Error("CurrentVersionID should be set")
	}
}

func TestComputeDiff(t *testing.T) {
	text1 := "line1\nline2\nline3"
	text2 := "line1\nline2_modified\nline3\nline4"

	diff := computeDiff(text1, text2)

	if len(diff) == 0 {
		t.Fatal("Diff should not be empty")
	}

	unchangedCount := 0
	addedCount := 0
	removedCount := 0
	for _, d := range diff {
		switch d.Type {
		case "unchanged":
			unchangedCount++
		case "added":
			addedCount++
		case "removed":
			removedCount++
		}
	}

	if unchangedCount < 1 {
		t.Error("Should have at least 1 unchanged line")
	}
	if addedCount < 1 {
		t.Error("Should have at least 1 added line")
	}
	if removedCount < 1 {
		t.Error("Should have at least 1 removed line")
	}
}

func TestComputeDiffIdentical(t *testing.T) {
	text := "line1\nline2\nline3"
	diff := computeDiff(text, text)

	for _, d := range diff {
		if d.Type != "unchanged" {
			t.Errorf("All lines should be unchanged, got %q", d.Type)
		}
	}
}

func TestComputeDiffEmpty(t *testing.T) {
	diff := computeDiff("", "")
	if len(diff) != 1 {
		t.Errorf("Diff of two empty strings should produce 1 line (the empty line), got %d", len(diff))
	}
}

func TestNewHandler(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func TestValidScopes(t *testing.T) {
	valid := []string{"global", "operator", "apn", "sim"}
	for _, s := range valid {
		if !validScopes[s] {
			t.Errorf("Scope %q should be valid", s)
		}
	}
	if validScopes["invalid"] {
		t.Error("Scope 'invalid' should not be valid")
	}
}

func TestValidPolicyStates(t *testing.T) {
	valid := []string{"active", "disabled", "archived"}
	for _, s := range valid {
		if !validPolicyStates[s] {
			t.Errorf("State %q should be valid", s)
		}
	}
	if validPolicyStates["draft"] {
		t.Error("Policy state 'draft' should not be valid (draft is for versions, not policies)")
	}
}

func TestHandlerCreateInvalidJSON(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["status"] != "error" {
		t.Errorf("status = %q, want %q", resp["status"], "error")
	}
}

func TestHandlerCreateMissingRequiredFields(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	body := `{"description": "only description"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("Response should have error object")
	}
	if errObj["code"] != "VALIDATION_ERROR" {
		t.Errorf("error code = %q, want %q", errObj["code"], "VALIDATION_ERROR")
	}
}

func TestHandlerCreateInvalidScope(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	body := `{"name": "test", "scope": "invalid_scope", "dsl_source": "x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.Create(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandlerGetInvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/policies/not-a-uuid", nil)
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerDeleteInvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/policies/bad-id", nil)
	w := httptest.NewRecorder()

	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerUpdateInvalidJSON(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/policies/"+uuid.New().String(), strings.NewReader("bad"))
	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerActivateVersionInvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/bad/activate", nil)
	w := httptest.NewRecorder()

	h.ActivateVersion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerUpdateVersionInvalidJSON(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/policy-versions/"+uuid.New().String(), strings.NewReader("bad"))
	w := httptest.NewRecorder()

	h.UpdateVersion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerUpdateVersionEmptyDSL(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	versionID := uuid.New().String()
	body := `{"dsl_source": ""}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/policy-versions/"+versionID, strings.NewReader(body))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", versionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.UpdateVersion(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandlerDiffInvalidID1(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-versions/bad/diff/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()

	h.DiffVersions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerCreateVersionInvalidPolicyID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/bad-id/versions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h.CreateVersion(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerUpdateInvalidState(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	policyID := uuid.New().String()
	invalidState := "invalid_state"
	body := `{"state": "` + invalidState + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/policies/"+policyID, strings.NewReader(body))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", policyID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.Update(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandlerDryRunInvalidVersionID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/bad-id/dry-run", nil)
	w := httptest.NewRecorder()

	h.DryRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerDryRunInvalidSegmentID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	versionID := uuid.New().String()
	body := `{"segment_id": "not-a-uuid"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/"+versionID+"/dry-run", strings.NewReader(body))
	req.Header.Set("Content-Length", "30")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", versionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.DryRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if resp["status"] != "error" {
		t.Errorf("status = %q, want %q", resp["status"], "error")
	}
}

func TestHandlerDryRunNoDryRunService(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	versionID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/"+versionID+"/dry-run", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", versionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.DryRun(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandlerDryRunInvalidJSON(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	versionID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/"+versionID+"/dry-run", strings.NewReader("not-json"))
	req.Header.Set("Content-Length", "8")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", versionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()

	h.DryRun(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerStartRolloutInvalidVersionID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/bad-id/rollout", nil)
	w := httptest.NewRecorder()

	h.StartRollout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerStartRolloutNoService(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	versionID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/"+versionID+"/rollout", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", versionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.StartRollout(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandlerStartRolloutInvalidStages(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	h.rolloutSvc = &rollout.Service{}

	tests := []struct {
		name string
		body string
	}{
		{"stage out of range", `{"stages": [0, 10, 100]}`},
		{"stage > 100", `{"stages": [1, 10, 150]}`},
		{"not ascending", `{"stages": [10, 5, 100]}`},
		{"last not 100", `{"stages": [1, 10, 50]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versionID := uuid.New().String()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-versions/"+versionID+"/rollout", strings.NewReader(tt.body))
			req.Header.Set("Content-Length", "30")

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", versionID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			w := httptest.NewRecorder()
			h.StartRollout(w, req)

			if w.Code != http.StatusUnprocessableEntity {
				t.Errorf("Status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
			}
		})
	}
}

func TestHandlerAdvanceRolloutInvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-rollouts/bad-id/advance", nil)
	w := httptest.NewRecorder()

	h.AdvanceRollout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerAdvanceRolloutNoService(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	rolloutID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-rollouts/"+rolloutID+"/advance", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", rolloutID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.AdvanceRollout(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandlerRollbackRolloutInvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-rollouts/bad-id/rollback", nil)
	w := httptest.NewRecorder()

	h.RollbackRollout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerRollbackRolloutNoService(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	rolloutID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-rollouts/"+rolloutID+"/rollback", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", rolloutID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.RollbackRollout(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandlerGetRolloutInvalidID(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-rollouts/bad-id", nil)
	w := httptest.NewRecorder()

	h.GetRollout(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandlerGetRolloutNoService(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())

	rolloutID := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/policy-rollouts/"+rolloutID, nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", rolloutID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.GetRollout(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestToRolloutResponse(t *testing.T) {
	now := time.Now()
	prevID := uuid.New()
	startedAt := now.Add(-1 * time.Hour)
	completedAt := now
	rolledBackAt := now

	ro := &store.PolicyRollout{
		ID:                uuid.New(),
		PolicyVersionID:   uuid.New(),
		PreviousVersionID: &prevID,
		Strategy:          "canary",
		Stages:            json.RawMessage(`[{"pct":1,"status":"completed"},{"pct":10,"status":"pending"}]`),
		CurrentStage:      0,
		TotalSIMs:         10000,
		MigratedSIMs:      100,
		State:             "in_progress",
		StartedAt:         &startedAt,
		CompletedAt:       &completedAt,
		RolledBackAt:      &rolledBackAt,
		CreatedAt:         now,
	}

	resp := toRolloutResponse(ro)

	if resp.RolloutID != ro.ID.String() {
		t.Errorf("RolloutID = %q, want %q", resp.RolloutID, ro.ID.String())
	}
	if resp.VersionID != ro.PolicyVersionID.String() {
		t.Errorf("VersionID = %q, want %q", resp.VersionID, ro.PolicyVersionID.String())
	}
	if resp.PreviousVersionID == nil || *resp.PreviousVersionID != prevID.String() {
		t.Error("PreviousVersionID should be set")
	}
	if resp.TotalSIMs != 10000 {
		t.Errorf("TotalSIMs = %d, want 10000", resp.TotalSIMs)
	}
	if resp.MigratedSIMs != 100 {
		t.Errorf("MigratedSIMs = %d, want 100", resp.MigratedSIMs)
	}
	if resp.State != "in_progress" {
		t.Errorf("State = %q, want %q", resp.State, "in_progress")
	}
	if resp.StartedAt == nil {
		t.Error("StartedAt should be set")
	}
	if resp.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}
	if resp.RolledBackAt == nil {
		t.Error("RolledBackAt should be set")
	}
}

func TestToRolloutResponseNilOptionals(t *testing.T) {
	ro := &store.PolicyRollout{
		ID:              uuid.New(),
		PolicyVersionID: uuid.New(),
		Stages:          json.RawMessage(`[]`),
		State:           "pending",
		CreatedAt:       time.Now(),
	}

	resp := toRolloutResponse(ro)

	if resp.PreviousVersionID != nil {
		t.Error("PreviousVersionID should be nil")
	}
	if resp.StartedAt != nil {
		t.Error("StartedAt should be nil")
	}
	if resp.CompletedAt != nil {
		t.Error("CompletedAt should be nil")
	}
	if resp.RolledBackAt != nil {
		t.Error("RolledBackAt should be nil")
	}
}
