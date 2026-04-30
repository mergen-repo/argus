package reports

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/btopcu/argus/internal/storage"
	"github.com/go-chi/chi/v5"
)

type fakeOpener struct {
	files map[string][]byte
	err   error
}

func (f *fakeOpener) Open(key string) (*StreamableFile, error) {
	if f.err != nil {
		return nil, f.err
	}
	body, ok := f.files[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &StreamableFile{ReadCloser: io.NopCloser(bytes.NewReader(body)), Name: key}, nil
}

func newDownloadRouter(d *DownloadDeps) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/reports/download/{key_b64}", d.Download)
	return r
}

func TestDownload_HappyPath_StreamsFile(t *testing.T) {
	op := &fakeOpener{files: map[string][]byte{"tenants/t/reports/j/r.pdf": []byte("PDF-DATA")}}
	d := &DownloadDeps{
		Opener:   op,
		Verifier: func(_, _, _ string) error { return nil },
	}
	enc := storage.EncodeKey("tenants/t/reports/j/r.pdf")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc+"?expires=9999999999&sig=ok", nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "PDF-DATA" {
		t.Errorf("body = %q, want PDF-DATA", got)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want attachment-prefixed", cd)
	}
}

func TestDownload_BadSig_Returns401(t *testing.T) {
	d := &DownloadDeps{
		Opener:   &fakeOpener{},
		Verifier: func(_, _, _ string) error { return storage.ErrInvalidSignature },
	}
	enc := storage.EncodeKey("k")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc+"?expires=9999&sig=bad", nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestDownload_ExpiredSig_Returns401(t *testing.T) {
	d := &DownloadDeps{
		Opener:   &fakeOpener{},
		Verifier: func(_, _, _ string) error { return storage.ErrExpiredSignature },
	}
	enc := storage.EncodeKey("k")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc+"?expires=1&sig=x", nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Body.String(), "expired") {
		t.Errorf("body should mention expired, got %s", w.Body.String())
	}
}

func TestDownload_FileMissing_Returns404(t *testing.T) {
	d := &DownloadDeps{
		Opener:   &fakeOpener{err: storage.ErrNotFound},
		Verifier: func(_, _, _ string) error { return nil },
	}
	enc := storage.EncodeKey("k")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc+"?expires=9999&sig=x", nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDownload_MissingToken_Returns401(t *testing.T) {
	d := &DownloadDeps{
		Opener:   &fakeOpener{},
		Verifier: func(_, _, _ string) error { return nil },
	}
	enc := storage.EncodeKey("k")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc, nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestDownload_NotConfigured_Returns503(t *testing.T) {
	d := &DownloadDeps{Opener: nil, Verifier: nil}
	enc := storage.EncodeKey("k")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc+"?expires=9999&sig=x", nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// Smoke test: storage.VerifyKey wired into DownloadDeps.Verifier behaves
// end-to-end (no fake).
func TestDownload_RealVerifier_Smoke(t *testing.T) {
	signingKey := bytes.Repeat([]byte("k"), 32)
	d := &DownloadDeps{
		Opener: &fakeOpener{files: map[string][]byte{"k": []byte("body")}},
		Verifier: func(key, expires, sig string) error {
			exp, err := storage.ParseExpiresQS(expires)
			if err != nil {
				return err
			}
			return storage.VerifyKey(key, exp, sig, signingKey)
		},
	}
	// Tamper test: bad sig with real verifier → 401.
	enc := storage.EncodeKey("k")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/download/"+enc+"?expires=9999999999&sig=deadbeef", nil)
	w := httptest.NewRecorder()
	newDownloadRouter(d).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (bad sig)", w.Code)
	}
}

// Sanity: errors.Is unwraps custom error chains correctly.
func TestErrorTypeChain(t *testing.T) {
	e := storage.ErrInvalidSignature
	wrapped := errors.New("outer: " + e.Error())
	if errors.Is(wrapped, storage.ErrInvalidSignature) {
		// fmt.Errorf would wrap; plain errors.New does NOT — this asserts our
		// understanding (Verifier must use errors.Is-able sentinels).
		t.Errorf("plain errors.New shouldn't be Is-equal")
	}
}
