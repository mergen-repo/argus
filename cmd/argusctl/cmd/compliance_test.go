package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComplianceExport_HappyPath_CSV(t *testing.T) {
	const fakeCSV = "iccid,msisdn,state\n1234567890,905001234567,active\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/compliance/btk-report" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("format") != "csv" {
			t.Errorf("expected format=csv, got %q", r.URL.Query().Get("format"))
		}
		if r.URL.Query().Get("tenant_id") != "t-1" {
			t.Errorf("expected tenant_id=t-1, got %q", r.URL.Query().Get("tenant_id"))
		}
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(fakeCSV))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "report.csv")

	out, _, err := runRoot(t, "compliance", "export",
		"--tenant=t-1",
		"--format=csv",
		"--from=2026-01-01",
		"--to=2026-03-31",
		"--output="+outFile,
		"--api-url="+srv.URL,
		"--token=admin-jwt",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, outFile) {
		t.Errorf("expected output path in stdout, got: %s", out)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if string(data) != fakeCSV {
		t.Errorf("file content mismatch: got %q, want %q", string(data), fakeCSV)
	}
}

func TestComplianceExport_HappyPath_PDF(t *testing.T) {
	fakeData := []byte("%PDF-1.4 fake")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "pdf" {
			t.Errorf("expected format=pdf, got %q", r.URL.Query().Get("format"))
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(fakeData)
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "report.pdf")

	_, _, err := runRoot(t, "compliance", "export",
		"--tenant=t-1",
		"--format=pdf",
		"--output="+outFile,
		"--api-url="+srv.URL,
		"--token=x",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if string(data) != string(fakeData) {
		t.Errorf("file content mismatch")
	}
}

func TestComplianceExport_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"error","error":{"code":"FORBIDDEN","message":"Tenant context required"}}`))
	}))
	defer srv.Close()

	outFile := filepath.Join(t.TempDir(), "report.pdf")

	_, _, err := runRoot(t, "compliance", "export",
		"--tenant=t-x",
		"--format=pdf",
		"--output="+outFile,
		"--api-url="+srv.URL,
		"--token=x",
	)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "FORBIDDEN") {
		t.Errorf("error should contain FORBIDDEN, got: %v", err)
	}
	if _, statErr := os.Stat(outFile); statErr == nil {
		t.Error("output file should have been cleaned up after error")
	}
}

func TestComplianceExport_InvalidFormat(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "report.xyz")
	_, _, err := runRoot(t, "compliance", "export",
		"--tenant=t-1",
		"--format=xlsx",
		"--output="+outFile,
		"--api-url=http://127.0.0.1:1",
		"--token=x",
	)
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "xlsx") {
		t.Errorf("error should mention format, got: %v", err)
	}
}
