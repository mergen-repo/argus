package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSimBulkOp_HappyPath_Suspend(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/sims/bulk/state-change":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["target_state"] != "suspended" {
				t.Errorf("expected target_state=suspended, got %v", body["target_state"])
			}
			if body["segment_id"] != "seg-1" {
				t.Errorf("expected segment_id=seg-1, got %v", body["segment_id"])
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"job_id":"job-abc","status":"queued","estimated_count":100}}`))

		case r.Method == "GET" && r.URL.Path == "/api/v1/jobs/job-abc":
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":"job-abc","state":"completed","total_items":100,"processed_items":100,"failed_items":0,"progress_pct":100}}`))

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	out, _, err := runRoot(t, "sim", "bulk-op",
		"--tenant=t-1",
		"--operation=suspend",
		"--segment=seg-1",
		"--api-url="+srv.URL,
		"--token=admin-jwt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "job-abc") {
		t.Errorf("expected job-abc in output, got: %s", out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("expected 'completed' in output, got: %s", out)
	}
}

func TestSimBulkOp_HappyPath_Resume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/sims/bulk/state-change":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["target_state"] != "active" {
				t.Errorf("expected target_state=active for resume, got %v", body["target_state"])
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"job_id":"job-resume","status":"queued","estimated_count":50}}`))

		case r.Method == "GET" && r.URL.Path == "/api/v1/jobs/job-resume":
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":"job-resume","state":"completed","total_items":50,"processed_items":50,"failed_items":0,"progress_pct":100}}`))
		}
	}))
	defer srv.Close()

	_, _, err := runRoot(t, "sim", "bulk-op",
		"--tenant=t-1",
		"--operation=resume",
		"--segment=seg-2",
		"--api-url="+srv.URL,
		"--token=x",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSimBulkOp_InvalidOperation(t *testing.T) {
	_, _, err := runRoot(t, "sim", "bulk-op",
		"--tenant=t-1",
		"--operation=delete",
		"--segment=seg-1",
		"--api-url=http://127.0.0.1:1",
		"--token=x",
	)
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}
	if !strings.Contains(err.Error(), "delete") {
		t.Errorf("error should mention invalid operation, got: %v", err)
	}
}

func TestSimBulkOp_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"SEGMENT_NOT_FOUND","message":"Segment not found"}}`))
	}))
	defer srv.Close()

	_, _, err := runRoot(t, "sim", "bulk-op",
		"--tenant=t-1",
		"--operation=suspend",
		"--segment=seg-bad",
		"--api-url="+srv.URL,
		"--token=x",
	)
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if !strings.Contains(err.Error(), "SEGMENT_NOT_FOUND") {
		t.Errorf("error should contain SEGMENT_NOT_FOUND, got: %v", err)
	}
}

func TestSimBulkOp_JobFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST":
			_, _ = w.Write([]byte(`{"status":"success","data":{"job_id":"job-fail","status":"queued","estimated_count":10}}`))
		case r.Method == "GET":
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":"job-fail","state":"failed","total_items":10,"processed_items":0,"failed_items":10,"progress_pct":0}}`))
		}
	}))
	defer srv.Close()

	_, _, err := runRoot(t, "sim", "bulk-op",
		"--tenant=t-1",
		"--operation=activate",
		"--segment=seg-1",
		"--api-url="+srv.URL,
		"--token=x",
	)
	if err == nil {
		t.Fatal("expected error for failed job")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error should mention failed state, got: %v", err)
	}
}

func TestRenderProgressBar(t *testing.T) {
	tests := []struct {
		pct       float64
		processed int
		total     int
		wantBar   string
	}{
		{0, 0, 100, "[--------] 0% (0/100)"},
		{50, 50, 100, "[####----] 50% (50/100)"},
		{100, 100, 100, "[########] 100% (100/100)"},
	}
	for _, tc := range tests {
		got := renderProgressBar(tc.pct, tc.processed, tc.total)
		if !strings.HasPrefix(got, tc.wantBar) {
			t.Errorf("renderProgressBar(%.0f, %d, %d) = %q, want prefix %q",
				tc.pct, tc.processed, tc.total, got, tc.wantBar)
		}
	}
}
