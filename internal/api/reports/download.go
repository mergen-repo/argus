// FIX-248 DEV-559: download handler — `GET /api/v1/reports/download/{key_b64}`.
//
// Public route (no JWT). Authentication is the HMAC-signed URL minted by
// `storage.LocalFSUploader.PresignGet`: the FE renders an `<a href>` with
// query params `?expires={unix}&sig={hex}`; this handler:
//
//   1. Decode {key_b64} → original key (URL-safe base64; rejects `..` / abs paths)
//   2. Parse `expires` (unix), reject if past
//   3. Verify HMAC-SHA256(key|expires, REPORT_SIGNING_KEY) constant-time
//   4. Open file from LocalFS Storage; stream via io.Copy with proper
//      Content-Type and Content-Disposition headers
//
// The S3 backend doesn't go through this handler — its presigned URLs
// already point directly at the bucket.

package reports

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/btopcu/argus/internal/storage"
	"github.com/go-chi/chi/v5"
)

// LocalFileOpener is implemented by storage.LocalFSUploader; broken out as an
// interface so the download handler can stay decoupled from the concrete
// storage backend.
type LocalFileOpener interface {
	Open(key string) (*StreamableFile, error)
}

// localFSOpener is an adapter that wraps a *storage.LocalFSUploader so its
// `Open(key) (*os.File, error)` method satisfies LocalFileOpener (which
// promises a StreamableFile interface so tests can supply their own).
type localFSOpener struct {
	u *storage.LocalFSUploader
}

func (a *localFSOpener) Open(key string) (*StreamableFile, error) {
	f, err := a.u.Open(key)
	if err != nil {
		return nil, err
	}
	return &StreamableFile{ReadCloser: f, Name: filepath.Base(key)}, nil
}

// NewLocalFileOpener wraps a *storage.LocalFSUploader for handler use.
// Returns nil when uploader is nil — the route handler returns 503 in that case.
func NewLocalFileOpener(u *storage.LocalFSUploader) LocalFileOpener {
	if u == nil {
		return nil
	}
	return &localFSOpener{u: u}
}

// StreamableFile is what LocalFileOpener.Open returns: a closable reader
// plus the original filename for Content-Disposition.
type StreamableFile struct {
	io.ReadCloser
	Name string
}

// SignatureVerifier validates the (key, expires, sig) tuple. Production
// supplies a closure that calls storage.VerifyKey with the configured
// signing key; tests substitute their own.
type SignatureVerifier func(key, expires, sig string) error

// DownloadDeps wires the download handler. Both fields nil-safe — handler
// returns 503 if storage / verifier are missing.
type DownloadDeps struct {
	Opener   LocalFileOpener
	Verifier SignatureVerifier
}

// Download serves a previously-uploaded report file.
func (d *DownloadDeps) Download(w http.ResponseWriter, r *http.Request) {
	if d == nil || d.Opener == nil || d.Verifier == nil {
		apierr.WriteError(w, http.StatusServiceUnavailable, apierr.CodeInternalError, "Local download backend not configured")
		return
	}

	keyB64 := chi.URLParam(r, "key_b64")
	key, err := storage.DecodeKey(keyB64)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Invalid download key")
		return
	}

	q := r.URL.Query()
	expires := q.Get("expires")
	sig := q.Get("sig")
	if expires == "" || sig == "" {
		apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Missing download token")
		return
	}

	if err := d.Verifier(key, expires, sig); err != nil {
		switch {
		case errors.Is(err, storage.ErrExpiredSignature):
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Download link expired")
		case errors.Is(err, storage.ErrInvalidSignature):
			apierr.WriteError(w, http.StatusUnauthorized, apierr.CodeForbidden, "Invalid download token")
		default:
			apierr.WriteError(w, http.StatusBadRequest, apierr.CodeValidationError, "Bad download token")
		}
		return
	}

	f, err := d.Opener.Open(key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			apierr.WriteError(w, http.StatusNotFound, apierr.CodeNotFound, "Report file not found")
			return
		}
		apierr.WriteError(w, http.StatusInternalServerError, apierr.CodeInternalError, "Failed to open report")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", contentTypeForExt(f.Name))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, f.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")

	if _, err := io.Copy(w, f); err != nil {
		// At this point headers are flushed — best we can do is log; the
		// caller-side request will report a truncated download.
		return
	}
}

func contentTypeForExt(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".json":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
