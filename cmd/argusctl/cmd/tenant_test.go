package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runRoot executes the root command with the given args using an in-memory
// stdout buffer. It also resets the server-bound flag state so tests don't
// leak into each other.
func runRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.SetArgs(args)

	// Reset flag-bound globals mutated by previous tests.
	t.Cleanup(func() {
		apiURL = "http://localhost:8084"
		token = ""
		certFile = ""
		keyFile = ""
		caFile = ""
		outputFmt = "table"
		tenantCreateName = ""
		tenantCreateAdminEmail = ""
		tenantCreateContactPhone = ""
		userPurgeTenantID = ""
		userPurgeUserID = ""
		userPurgeConfirm = false
		// sim flags
		simBulkTenantID = ""
		simBulkOperation = ""
		simBulkSegmentID = ""
		// backup flags
		backupRestoreFrom = ""
		backupRestoreConfirm = false
		backupRestorePITRTarget = ""
		backupRestoreDryRun = false
	})

	rootCmd.SetContext(context.Background())
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestTenantList_TableOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tenants" || r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer admin-jwt" {
			t.Errorf("missing/incorrect auth header: %q", r.Header.Get("Authorization"))
		}
		body := map[string]interface{}{
			"status": "success",
			"data": []map[string]interface{}{
				{
					"id":            "t-1",
					"name":          "Acme",
					"slug":          "acme",
					"state":         "active",
					"contact_email": "ops@acme.io",
					"created_at":    "2026-04-10T10:00:00Z",
				},
				{
					"id":            "t-2",
					"name":          "Globex",
					"slug":          "globex",
					"state":         "suspended",
					"contact_email": "admin@globex.io",
					"created_at":    "2026-03-15T09:00:00Z",
				},
			},
			"meta": map[string]interface{}{"has_more": false, "limit": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	out, _, err := runRoot(t, "tenant", "list", "--api-url="+srv.URL, "--token=admin-jwt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "ID") || !strings.Contains(out, "NAME") || !strings.Contains(out, "STATE") {
		t.Errorf("expected table headers in output, got:\n%s", out)
	}
	if !strings.Contains(out, "t-1") || !strings.Contains(out, "Acme") {
		t.Errorf("expected Acme row in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Globex") || !strings.Contains(out, "suspended") {
		t.Errorf("expected Globex/suspended in output, got:\n%s", out)
	}
}

func TestTenantSuspend_ErrorPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/tenants/tid-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"FORBIDDEN","message":"You cannot suspend this tenant"}}`))
	}))
	defer srv.Close()

	_, _, err := runRoot(t, "tenant", "suspend", "tid-1", "--api-url="+srv.URL, "--token=x")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "FORBIDDEN") || !strings.Contains(err.Error(), "cannot suspend") {
		t.Errorf("error message should surface code+message, got: %v", err)
	}
}

func TestTenantResume_Happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["state"] != "active" {
			t.Errorf("expected state=active in body, got %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":"t-1","name":"Acme","state":"active"}}`))
	}))
	defer srv.Close()

	_, _, err := runRoot(t, "tenant", "resume", "t-1", "--api-url="+srv.URL, "--token=x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUserPurge_RequiresConfirm(t *testing.T) {
	// No HTTP server — command should refuse before attempting a request.
	_, _, err := runRoot(t, "user", "purge",
		"--tenant=tid-1",
		"--user=uid-1",
		"--api-url=http://127.0.0.1:1",
	)
	if err == nil {
		t.Fatal("expected error when --confirm missing")
	}
	if !strings.Contains(err.Error(), "--confirm") {
		t.Errorf("error should mention --confirm, got: %v", err)
	}
}

func TestUserPurge_Confirmed(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/users/uid-1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("gdpr") != "1" {
			t.Errorf("expected gdpr=1 query param, got %q", r.URL.Query().Get("gdpr"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"sessions_revoked":3,"purged_at":"2026-04-12T10:00:00Z"}}`))
	}))
	defer srv.Close()

	out, _, err := runRoot(t, "user", "purge",
		"--tenant=tid-1",
		"--user=uid-1",
		"--confirm",
		"--api-url="+srv.URL,
		"--token=admin-jwt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("server endpoint never called")
	}
	if !strings.Contains(out, "GDPR purge complete") {
		t.Errorf("expected confirmation text in output, got:\n%s", out)
	}
}
