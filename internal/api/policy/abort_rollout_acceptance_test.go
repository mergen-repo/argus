package policy

// FIX-232 T4 — AC-10 acceptance tests for AbortRollout error body assertions.
//
// TestAbortRollout_ServiceErrorPropagation (handler_test.go) already verifies
// the error *code* string appears somewhere in the response body via
// strings.Contains. This file adds the missing AC-10 requirement: assert the
// response JSON carries both a non-empty error.code AND a non-empty
// human-readable error.message for each terminal-state violation.
//
// AC-10 checklist:
//   (a) in_progress → aborted:                COVERED by TestService_AbortRollout_HappyPath (abort_test.go)
//                                              + TestAbortRollout_Success_Returns200_WithEnvelope (handler_test.go)
//   (b) completed  → 422 ROLLOUT_COMPLETED:   COVERED by TestAbortRollout_ServiceErrorPropagation (code check)
//                                              + TestAC10_AbortCompleted_ErrorBody (this file, message check)
//   (c) rolled_back → 422 ROLLOUT_ROLLED_BACK: COVERED by TestAbortRollout_ServiceErrorPropagation (code check)
//                                              + TestAC10_AbortRolledBack_ErrorBody (this file, message check)
//   bonus: aborted → 422 ROLLOUT_ABORTED (idempotency):
//                                              COVERED by TestAbortRollout_ServiceErrorPropagation (code check)
//                                              + TestAC10_AbortAlreadyAborted_ErrorBody (this file, message check)

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// abortRolloutWithErr fires the AbortRollout handler wired to a mock service
// that returns serviceErr, and returns the parsed error response.
func abortRolloutWithErr(t *testing.T, serviceErr error) (statusCode int, errResp apierr.ErrorResponse) {
	t.Helper()
	svc := &mockRolloutService{
		abortFn: func(_ context.Context, _, _ uuid.UUID, _ string) (*store.PolicyRollout, error) {
			return nil, serviceErr
		},
	}
	h := NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
	h.rolloutSvc = svc

	rolloutID := uuid.New().String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policy-rollouts/"+rolloutID+"/abort",
		strings.NewReader(`{"reason":"test"}`))
	req.Header.Set("Content-Length", "17")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", rolloutID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	h.AbortRollout(w, req)

	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v — raw body: %s", err, w.Body.String())
	}
	return w.Code, errResp
}

// TestAC10_AbortCompleted_ErrorBody asserts AC-10(b):
// aborting a completed rollout returns 422 with ROLLOUT_COMPLETED code
// and a non-empty human-readable message.
func TestAC10_AbortCompleted_ErrorBody(t *testing.T) {
	code, resp := abortRolloutWithErr(t, store.ErrRolloutCompleted)

	if code != http.StatusUnprocessableEntity {
		t.Errorf("HTTP status = %d, want 422", code)
	}
	if resp.Error.Code != "ROLLOUT_COMPLETED" {
		t.Errorf("error.code = %q, want %q", resp.Error.Code, "ROLLOUT_COMPLETED")
	}
	if resp.Error.Message == "" {
		t.Error("error.message is empty, want non-empty human-readable description")
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want %q", resp.Status, "error")
	}
}

// TestAC10_AbortRolledBack_ErrorBody asserts AC-10(c):
// aborting a rolled-back rollout returns 422 with ROLLOUT_ROLLED_BACK code
// and a non-empty human-readable message.
func TestAC10_AbortRolledBack_ErrorBody(t *testing.T) {
	code, resp := abortRolloutWithErr(t, store.ErrRolloutRolledBack)

	if code != http.StatusUnprocessableEntity {
		t.Errorf("HTTP status = %d, want 422", code)
	}
	if resp.Error.Code != "ROLLOUT_ROLLED_BACK" {
		t.Errorf("error.code = %q, want %q", resp.Error.Code, "ROLLOUT_ROLLED_BACK")
	}
	if resp.Error.Message == "" {
		t.Error("error.message is empty, want non-empty human-readable description")
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want %q", resp.Status, "error")
	}
}

// TestAC10_AbortAlreadyAborted_ErrorBody asserts idempotency:
// aborting an already-aborted rollout returns 422 with ROLLOUT_ABORTED code
// and a non-empty human-readable message.
func TestAC10_AbortAlreadyAborted_ErrorBody(t *testing.T) {
	code, resp := abortRolloutWithErr(t, store.ErrRolloutAborted)

	if code != http.StatusUnprocessableEntity {
		t.Errorf("HTTP status = %d, want 422", code)
	}
	if resp.Error.Code != "ROLLOUT_ABORTED" {
		t.Errorf("error.code = %q, want %q", resp.Error.Code, "ROLLOUT_ABORTED")
	}
	if resp.Error.Message == "" {
		t.Error("error.message is empty, want non-empty human-readable description")
	}
	if resp.Status != "error" {
		t.Errorf("status = %q, want %q", resp.Status, "error")
	}
}

// TestAC10_AbortCompleted_MessageContent asserts the actual message text is
// descriptive (not a bare code or empty string) per AC-10(b).
// The handler returns "Cannot abort a completed rollout".
func TestAC10_AbortCompleted_MessageContent(t *testing.T) {
	_, resp := abortRolloutWithErr(t, store.ErrRolloutCompleted)
	if !strings.Contains(strings.ToLower(resp.Error.Message), "complet") {
		t.Errorf("error.message %q does not describe a completed state", resp.Error.Message)
	}
}

// TestAC10_AbortRolledBack_MessageContent asserts the actual message text is
// descriptive per AC-10(c).
// The handler returns "Cannot abort a rolled back rollout".
func TestAC10_AbortRolledBack_MessageContent(t *testing.T) {
	_, resp := abortRolloutWithErr(t, store.ErrRolloutRolledBack)
	if !strings.Contains(strings.ToLower(resp.Error.Message), "roll") {
		t.Errorf("error.message %q does not describe a rolled-back state", resp.Error.Message)
	}
}
