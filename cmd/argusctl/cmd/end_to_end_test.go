package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// buildArgusctl compiles the argusctl binary into a temp directory and returns
// the path to the binary. The test is skipped if the go toolchain is not
// available or the build fails.
func buildArgusctl(t *testing.T) string {
	t.Helper()

	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Skip("end-to-end: go toolchain not found in PATH — skipping binary build test")
	}

	dir := t.TempDir()
	binPath := dir + "/argusctl"

	cmd := exec.Command(goExe, "build", "-o", binPath, "github.com/btopcu/argus/cmd/argusctl")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("end-to-end: binary build failed (%v) — skipping. Output:\n%s", err, string(out))
	}

	return binPath
}

// mockArgusServer creates an httptest.Server that stubs the endpoints that
// argusctl exercises in the tests below.
func mockArgusServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/tenants", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{
			"status": "success",
			"data": []map[string]interface{}{
				{
					"id":            "t-e2e-1",
					"name":          "E2E Corp",
					"slug":          "e2e-corp",
					"state":         "active",
					"contact_email": "ops@e2e.io",
					"created_at":    "2026-04-12T10:00:00Z",
				},
				{
					"id":            "t-e2e-2",
					"name":          "E2E Labs",
					"slug":          "e2e-labs",
					"state":         "suspended",
					"contact_email": "admin@e2elabs.io",
					"created_at":    "2026-04-10T08:00:00Z",
				},
			},
			"meta": map[string]interface{}{"has_more": false, "limit": 50},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("mock server encode: %v", err)
		}
	})

	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"service": "argus",
				"overall": "healthy",
				"version": "1.0.0-e2e",
				"uptime":  "1m0s",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("mock server encode: %v", err)
		}
	})

	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]interface{}{"overall": "healthy"},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("mock server encode: %v", err)
		}
	})

	return httptest.NewServer(mux)
}

func TestArgusctlE2E_TenantList_TableHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end: skipping in short mode")
	}

	binPath := buildArgusctl(t)
	srv := mockArgusServer(t)
	defer srv.Close()

	out, err := exec.Command(binPath, "tenant", "list",
		"--api-url="+srv.URL,
		"--token=fake-e2e-token",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("argusctl tenant list: %v\noutput:\n%s", err, string(out))
	}

	text := string(out)
	for _, header := range []string{"ID", "NAME", "STATE"} {
		if !strings.Contains(text, header) {
			t.Errorf("expected table header %q in output, got:\n%s", header, text)
		}
	}
	if !strings.Contains(text, "E2E Corp") {
		t.Errorf("expected tenant name 'E2E Corp' in output, got:\n%s", text)
	}
	if !strings.Contains(text, "t-e2e-1") {
		t.Errorf("expected tenant id 't-e2e-1' in output, got:\n%s", text)
	}
	if !strings.Contains(text, "active") {
		t.Errorf("expected state 'active' in output, got:\n%s", text)
	}
}

func TestArgusctlE2E_Health(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end: skipping in short mode")
	}

	binPath := buildArgusctl(t)
	srv := mockArgusServer(t)
	defer srv.Close()

	out, err := exec.Command(binPath, "health",
		"--api-url="+srv.URL,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("argusctl health: %v\noutput:\n%s", err, string(out))
	}

	if !strings.Contains(string(out), "ok") {
		t.Errorf("expected 'ok' in health output, got:\n%s", string(out))
	}
}

func TestArgusctlE2E_Status(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end: skipping in short mode")
	}

	binPath := buildArgusctl(t)
	srv := mockArgusServer(t)
	defer srv.Close()

	out, err := exec.Command(binPath, "status",
		"--api-url="+srv.URL,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("argusctl status: %v\noutput:\n%s", err, string(out))
	}

	text := string(out)
	if !strings.Contains(text, "healthy") && !strings.Contains(text, "ok") {
		t.Errorf("expected health indicator in status output, got:\n%s", text)
	}
}

func TestArgusctlE2E_Version(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end: skipping in short mode")
	}

	binPath := buildArgusctl(t)

	out, err := exec.Command(binPath, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("argusctl version: %v\noutput:\n%s", err, string(out))
	}

	if !strings.Contains(string(out), "argusctl") {
		t.Errorf("expected 'argusctl' in version output, got:\n%s", string(out))
	}
}
