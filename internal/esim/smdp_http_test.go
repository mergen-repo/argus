package esim

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func newTestAdapter(t *testing.T, srv *httptest.Server) *HTTPSMDPAdapter {
	t.Helper()
	adapter, err := NewHTTPSMDPAdapter(HTTPSMDPConfig{
		BaseURL: srv.URL,
		APIKey:  "test-api-key",
		Timeout: 5 * time.Second,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewHTTPSMDPAdapter: %v", err)
	}
	adapter.client = srv.Client()
	adapter.client.Timeout = 5 * time.Second
	return adapter
}

func TestHTTPSMDP_DownloadProfile_HappyPath(t *testing.T) {
	operatorID := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-api-key" {
			t.Errorf("X-Api-Key header = %q, want %q", r.Header.Get("X-Api-Key"), "test-api-key")
		}
		if r.URL.Path != "/gsma/rsp2/es9plus/downloadOrder" {
			t.Errorf("path = %q, want /gsma/rsp2/es9plus/downloadOrder", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"profileId":  "prof-abc-123",
			"iccid":      "8988211234567890123",
			"smdpPlusId": "smdp.example.com",
		})
	}))
	defer srv.Close()

	adapter := newTestAdapter(t, srv)

	resp, err := adapter.DownloadProfile(context.Background(), DownloadProfileRequest{
		EID:        "eid-001",
		OperatorID: operatorID,
		ICCID:      "8988211234567890123",
		SMDPPlusID: "smdp.example.com",
	})
	if err != nil {
		t.Fatalf("DownloadProfile: unexpected error: %v", err)
	}
	if resp.ProfileID != "prof-abc-123" {
		t.Errorf("ProfileID = %q, want %q", resp.ProfileID, "prof-abc-123")
	}
	if resp.ICCID != "8988211234567890123" {
		t.Errorf("ICCID = %q, want %q", resp.ICCID, "8988211234567890123")
	}
}

func TestHTTPSMDP_GetProfileInfo_HappyPath(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gsma/rsp2/es9plus/getProfileInfo" {
			t.Errorf("path = %q, want /gsma/rsp2/es9plus/getProfileInfo", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"state":      "enabled",
			"iccid":      "8988211234567890123",
			"smdpPlusId": "smdp.example.com",
			"lastSeenAt": now.Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	adapter := newTestAdapter(t, srv)

	resp, err := adapter.GetProfileInfo(context.Background(), GetProfileInfoRequest{
		EID:       "eid-001",
		ICCID:     "8988211234567890123",
		ProfileID: "prof-abc-123",
	})
	if err != nil {
		t.Fatalf("GetProfileInfo: unexpected error: %v", err)
	}
	if resp.State != "enabled" {
		t.Errorf("State = %q, want %q", resp.State, "enabled")
	}
	if resp.ICCID != "8988211234567890123" {
		t.Errorf("ICCID = %q, want %q", resp.ICCID, "8988211234567890123")
	}
	if resp.SMDPPlusID != "smdp.example.com" {
		t.Errorf("SMDPPlusID = %q, want %q", resp.SMDPPlusID, "smdp.example.com")
	}
}

func TestHTTPSMDP_404_ReturnsProfileNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	adapter := newTestAdapter(t, srv)

	_, err := adapter.DownloadProfile(context.Background(), DownloadProfileRequest{
		EID:        "eid-001",
		OperatorID: uuid.New(),
		ICCID:      "8988211234567890123",
	})
	if !errors.Is(err, ErrSMDPProfileNotFound) {
		t.Errorf("err = %v, want ErrSMDPProfileNotFound", err)
	}
}

func TestHTTPSMDP_503_AllRetriesFail_ReturnsConnectionFailed(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	adapter, err := NewHTTPSMDPAdapter(HTTPSMDPConfig{
		BaseURL: srv.URL,
		APIKey:  "test-api-key",
		Timeout: 10 * time.Second,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewHTTPSMDPAdapter: %v", err)
	}
	adapter.client = &http.Client{
		Transport: srv.Client().Transport,
		Timeout:   10 * time.Second,
	}

	_, err = adapter.DownloadProfile(context.Background(), DownloadProfileRequest{
		EID:        "eid-001",
		OperatorID: uuid.New(),
		ICCID:      "8988211234567890123",
	})
	if !errors.Is(err, ErrSMDPConnectionFailed) {
		t.Errorf("err = %v, want ErrSMDPConnectionFailed", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 retry attempts, got %d", attempts)
	}
}

func TestHTTPSMDP_ApiKeyHeader_PresentOnEveryRequest(t *testing.T) {
	const apiKey = "my-secret-api-key"
	calls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Api-Key") != apiKey {
			t.Errorf("request %d: X-Api-Key = %q, want %q", calls, r.Header.Get("X-Api-Key"), apiKey)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{})
	}))
	defer srv.Close()

	adapter, err := NewHTTPSMDPAdapter(HTTPSMDPConfig{
		BaseURL: srv.URL,
		APIKey:  apiKey,
		Timeout: 5 * time.Second,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewHTTPSMDPAdapter: %v", err)
	}
	adapter.client = srv.Client()

	profileID := uuid.New()

	_ = adapter.EnableProfile(context.Background(), EnableProfileRequest{
		EID:       "eid-001",
		ICCID:     "8988211234567890123",
		ProfileID: profileID,
	})
	_ = adapter.DisableProfile(context.Background(), DisableProfileRequest{
		EID:       "eid-001",
		ICCID:     "8988211234567890123",
		ProfileID: profileID,
	})
	_ = adapter.DeleteProfile(context.Background(), DeleteProfileRequest{
		EID:       "eid-001",
		ICCID:     "8988211234567890123",
		ProfileID: profileID,
	})

	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestHTTPSMDP_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	adapter := newTestAdapter(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := adapter.DownloadProfile(ctx, DownloadProfileRequest{
		EID:        "eid-001",
		OperatorID: uuid.New(),
		ICCID:      "8988211234567890123",
	})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
