package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type rewriteTransport struct {
	base    string
	inner   http.RoundTripper
	gotPath *string
	gotAuth *string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.gotPath != nil {
		*t.gotPath = req.URL.Path
	}
	if t.gotAuth != nil {
		*t.gotAuth = req.Header.Get("Authorization")
	}
	parsed, _ := url.Parse(t.base + req.URL.Path)
	req.URL = parsed
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	return inner.RoundTrip(req)
}

func makeTwilioClient(srv *httptest.Server, accountID, authToken, from string, extras ...string) *twilioClient {
	rt := &rewriteTransport{
		base:  srv.URL,
		inner: srv.Client().Transport,
	}
	callback := ""
	if len(extras) > 0 {
		callback = extras[0]
	}
	return &twilioClient{
		accountID:      accountID,
		authToken:      authToken,
		fromPhone:      from,
		statusCallback: callback,
		http:           &http.Client{Transport: rt},
		logger:         zerolog.Nop(),
	}
}

func makeTwilioClientCapturing(srv *httptest.Server, accountID, authToken, from string, gotPath, gotAuth *string) *twilioClient {
	rt := &rewriteTransport{
		base:    srv.URL,
		inner:   srv.Client().Transport,
		gotPath: gotPath,
		gotAuth: gotAuth,
	}
	return &twilioClient{
		accountID: accountID,
		authToken: authToken,
		fromPhone: from,
		http:      &http.Client{Transport: rt},
		logger:    zerolog.Nop(),
	}
}

func TestTwilio_Send_HappyPath(t *testing.T) {
	const (
		accountID = "ACtest123"
		authToken = "secret456"
		from      = "+15550001111"
		to        = "+15552223333"
		msgBody   = "Hello world"
	)

	var gotPath, gotAuth string
	var gotFormBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err == nil {
			gotFormBody = r.Form.Encode()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(twilioResponse{SID: "SMabc123", Status: "queued"})
	}))
	defer srv.Close()

	client := makeTwilioClientCapturing(srv, accountID, authToken, from, &gotPath, &gotAuth)

	if err := client.Send(context.Background(), to, msgBody); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	expectedPath := "/2010-04-01/Accounts/" + accountID + "/Messages.json"
	if gotPath != expectedPath {
		t.Errorf("path = %q, want %q", gotPath, expectedPath)
	}

	tmpReq, _ := http.NewRequest("GET", "/", nil)
	tmpReq.SetBasicAuth(accountID, authToken)
	wantAuth := tmpReq.Header.Get("Authorization")
	if gotAuth != wantAuth {
		t.Errorf("auth = %q, want %q", gotAuth, wantAuth)
	}

	vals, err := url.ParseQuery(gotFormBody)
	if err != nil {
		t.Fatalf("parse form body: %v", err)
	}
	if vals.Get("To") != to {
		t.Errorf("To = %q, want %q", vals.Get("To"), to)
	}
	if vals.Get("From") != from {
		t.Errorf("From = %q, want %q", vals.Get("From"), from)
	}
	if vals.Get("Body") != msgBody {
		t.Errorf("Body = %q, want %q", vals.Get("Body"), msgBody)
	}
}

func TestTwilio_Send_400Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(twilioResponse{SID: "", Status: "failed"})
	}))
	defer srv.Close()

	client := makeTwilioClient(srv, "ACtest", "token", "+15550000001")
	err := client.Send(context.Background(), "+15559999999", "test")
	if err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error %q should mention status 400", err.Error())
	}
}

func TestTwilio_Send_500Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := makeTwilioClient(srv, "ACtest", "token", "+15550000001")
	err := client.Send(context.Background(), "+15559999999", "test")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should mention status 500", err.Error())
	}
}

func TestTwilio_Send_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := makeTwilioClient(srv, "ACtest", "token", "+15550000001")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.Send(ctx, "+15559999999", "test")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func computeTwilioSignature(authToken, fullURL string, params url.Values) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(params.Get(k))
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(sb.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestTwilio_VerifyStatusSignature_Valid(t *testing.T) {
	authToken := "myAuthToken"
	fullURL := "https://example.com/webhook/twilio/status"
	params := url.Values{
		"MessageSid":    {"SMabc123"},
		"MessageStatus": {"delivered"},
		"To":            {"+15551234567"},
	}

	sig := computeTwilioSignature(authToken, fullURL, params)

	client := &twilioClient{authToken: authToken, logger: zerolog.Nop()}
	if !client.VerifyStatusSignature(fullURL, params, sig) {
		t.Error("valid signature should return true")
	}
}

func TestTwilio_VerifyStatusSignature_Invalid(t *testing.T) {
	authToken := "myAuthToken"
	fullURL := "https://example.com/webhook/twilio/status"
	params := url.Values{
		"MessageSid":    {"SMabc123"},
		"MessageStatus": {"delivered"},
	}

	client := &twilioClient{authToken: authToken, logger: zerolog.Nop()}
	if client.VerifyStatusSignature(fullURL, params, "invalidsignature") {
		t.Error("invalid signature should return false")
	}
}

func TestTwilio_VerifyStatusSignature_WrongToken(t *testing.T) {
	fullURL := "https://example.com/webhook/twilio/status"
	params := url.Values{
		"MessageSid": {"SMabc123"},
	}

	sig := computeTwilioSignature("correctToken", fullURL, params)

	client := &twilioClient{authToken: "wrongToken", logger: zerolog.Nop()}
	if client.VerifyStatusSignature(fullURL, params, sig) {
		t.Error("signature computed with different token should return false")
	}
}
