package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPprofGuard_Integration_httptest uses httptest.NewServer (real TCP listener)
// to validate that PprofGuard behaves correctly over the wire — not just with
// ResponseRecorder. Unit-level tests in pprof_guard_test.go already cover the
// logic; this test adds an integration-flavour using a real HTTP server.
//
// Gate: skip when -short so `make test` (unit-only) stays fast.
func TestPprofGuard_Integration_NoToken_401(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}
	t.Parallel()

	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(PprofGuard("testtoken")(pprofMux))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", resp.StatusCode)
	}
}

func TestPprofGuard_Integration_QueryParam_200(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}
	t.Parallel()

	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(PprofGuard("testtoken")(pprofMux))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/pprof/?token=testtoken")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("query param token: got %d, want 200", resp.StatusCode)
	}
}

func TestPprofGuard_Integration_BearerHeader_200(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in short mode")
	}
	t.Parallel()

	pprofMux := http.NewServeMux()
	pprofMux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(PprofGuard("testtoken")(pprofMux))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/debug/pprof/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer testtoken")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("bearer token: got %d, want 200", resp.StatusCode)
	}
}
