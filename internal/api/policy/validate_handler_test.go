package policy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// validHandler builds a Handler with all-nil deps. The Validate endpoint is
// stateless (FIX-243 AC-2 — no DB writes), so nil stores are safe. If a future
// change starts touching h.policyStore from Validate, these tests will panic
// and we want to catch that immediately.
func validHandler() *Handler {
	return NewHandler(nil, nil, nil, nil, nil, nil, zerolog.Nop())
}

func TestValidate_EmptyBody(t *testing.T) {
	h := validHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate", strings.NewReader(""))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("response should have error object")
	}
	if errObj["code"] != "INVALID_FORMAT" {
		t.Errorf("error code = %q, want INVALID_FORMAT", errObj["code"])
	}
}

func TestValidate_EmptySourceField(t *testing.T) {
	h := validHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate",
		strings.NewReader(`{"dsl_source": ""}`))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	errObj, _ := resp["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", errObj)
	}
}

func TestValidate_ValidDSL_Returns200(t *testing.T) {
	h := validHandler()
	src := `POLICY "p1" {
  MATCH { apn = "internet" }
  RULES { bandwidth_down = 10mbps }
}`
	body, _ := json.Marshal(map[string]string{"dsl_source": src})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("response missing data object")
	}
	if data["valid"] != true {
		t.Errorf("data.valid = %v, want true", data["valid"])
	}
	if data["compiled_rules"] == nil {
		t.Error("data.compiled_rules should be populated")
	}
	if _, ok := data["warnings"]; !ok {
		t.Error("data.warnings key should be present (even if empty)")
	}
}

func TestValidate_InvalidDSL_Returns422_WithErrors(t *testing.T) {
	h := validHandler()
	// Missing closing brace + missing RULES block.
	src := `POLICY "broken" {`
	body, _ := json.Marshal(map[string]string{"dsl_source": src})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Status = %d, want %d, body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("response missing error object")
	}
	if errObj["code"] != "DSL_VALIDATION_FAILED" {
		t.Errorf("error.code = %q, want DSL_VALIDATION_FAILED", errObj["code"])
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatal("error.details missing")
	}
	if details["valid"] != false {
		t.Errorf("details.valid = %v, want false", details["valid"])
	}
	errs, ok := details["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Errorf("details.errors should be non-empty array, got %v", details["errors"])
	}
}

func TestValidate_SuggestionAppended(t *testing.T) {
	h := validHandler()
	// Trigger an "unknown action" error via a misspelled action name close
	// enough to a valid one that Suggest returns a hit.
	src := `POLICY "p1" {
  MATCH { apn = "internet" }
  RULES {
    WHEN usage > 1mb {
      ACTION thrtle(1mbps)
    }
  }
}`
	body, _ := json.Marshal(map[string]string{"dsl_source": src})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Status = %d, want %d, body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	errObj, _ := resp["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	errs, _ := details["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("expected at least one error, got: %s", w.Body.String())
	}
	found := false
	for _, raw := range errs {
		e, _ := raw.(map[string]any)
		msg, _ := e["message"].(string)
		if strings.Contains(msg, "did you mean") && strings.Contains(msg, "throttle") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a 'did you mean \"throttle\"' suggestion in errors, got: %v", errs)
	}
}

func TestValidate_FormatQueryParam(t *testing.T) {
	h := validHandler()
	src := `POLICY "p1" {
  MATCH { apn = "internet" }
  RULES { bandwidth_down = 10mbps }
}`
	body, _ := json.Marshal(map[string]string{"dsl_source": src})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate?format=true", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	data, _ := resp["data"].(map[string]any)
	if _, ok := data["formatted_source"]; !ok {
		t.Error("formatted_source should be present when ?format=true")
	}
}

// FIX-243 Wave D — confirms the handler now invokes dsl.Format (not the
// pre-Wave-D placeholder echo). Mangled input must come back canonicalised.
func TestValidate_FormatActuallyReformats(t *testing.T) {
	h := validHandler()
	mangled := `POLICY    "p1"   {
MATCH{apn="internet"}
RULES{bandwidth_down=10mbps}
}`
	body, _ := json.Marshal(map[string]string{"dsl_source": mangled})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate?format=true", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	data, _ := resp["data"].(map[string]any)
	formatted, _ := data["formatted_source"].(string)
	if formatted == "" {
		t.Fatal("formatted_source missing")
	}
	if formatted == mangled {
		t.Errorf("formatted_source equals raw source — formatter not wired (placeholder still in place)")
	}
	if !strings.Contains(formatted, `POLICY "p1" {`) || !strings.Contains(formatted, "  MATCH {") {
		t.Errorf("formatted_source not canonical:\n%s", formatted)
	}
}

func TestValidate_NoFormatByDefault(t *testing.T) {
	h := validHandler()
	src := `POLICY "p1" {
  MATCH { apn = "internet" }
  RULES { bandwidth_down = 10mbps }
}`
	body, _ := json.Marshal(map[string]string{"dsl_source": src})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	h.Validate(w, req)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	data, _ := resp["data"].(map[string]any)
	if _, ok := data["formatted_source"]; ok {
		t.Error("formatted_source should NOT be present without ?format=true")
	}
}

// FIX-243 Wave D — Vocab handler returns the canonical DSL vocabulary.
func TestVocab_ReturnsAllLists(t *testing.T) {
	h := validHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/policies/vocab", nil)
	w := httptest.NewRecorder()

	h.Vocab(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatal("response missing data object")
	}
	for _, key := range []string{
		"match_fields", "charging_models", "overage_actions",
		"billing_cycles", "units", "rule_keywords", "actions",
	} {
		arr, ok := data[key].([]any)
		if !ok {
			t.Errorf("data.%s should be an array, got %T", key, data[key])
			continue
		}
		if len(arr) == 0 {
			t.Errorf("data.%s should be non-empty", key)
		}
	}
}

// Note: AC-1 rate-limiting (10/sec/IP) is enforced at the router level
// via httprate.LimitByIP. It is intentionally NOT exercised here — the
// handler unit tests bypass the router middleware chain. Router-level
// integration coverage would belong in a gateway-level test.
func TestValidate_NoDBWrite_Sentinel(t *testing.T) {
	// All deps are nil. If Validate ever starts calling a store method,
	// the nil deref will panic this test. This is the sentinel for AC-2.
	h := validHandler()
	src := `POLICY "p1" {
  MATCH { apn = "internet" }
  RULES { bandwidth_down = 10mbps }
}`
	body, _ := json.Marshal(map[string]string{"dsl_source": src})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies/validate", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Validate panicked — likely touched a nil store. AC-2 violation: %v", r)
		}
	}()
	h.Validate(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}
}
