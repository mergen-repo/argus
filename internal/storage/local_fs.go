// FIX-248 DEV-556: LocalFSUploader — disk-backed Storage implementation.
//
// File path layout: `{base}/{key}` — `key` is already hierarchical via the
// scheduled-report processor (`tenants/{tenant}/reports/{job}/{filename}`),
// so a flat join produces a clean per-tenant per-job tree on disk.
//
// PresignGet returns a public URL that the download handler verifies on hit:
//   {publicBaseURL}/api/v1/reports/download/{base64(key)}?expires={unix}&sig={hex}
//
// where `sig = HMAC-SHA256(key + "|" + expires, signingKey)`.

package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

const downloadRoutePath = "/api/v1/reports/download/"

// LocalFSUploader writes report artifacts to a base directory and mints
// HMAC-signed download URLs. Safe for concurrent use; each Upload creates
// any missing parent dirs (mode 0750) before writing the file (mode 0640).
type LocalFSUploader struct {
	BasePath      string
	SigningKey    []byte
	PublicBaseURL string
	logger        zerolog.Logger
}

// NewLocalFSUploader validates inputs and returns a ready-to-use uploader.
// basePath must be an absolute path that exists or can be created.
// signingKey must be at least 16 bytes — shorter keys are rejected loudly
// to prevent accidental weak-key configs.
func NewLocalFSUploader(basePath string, signingKey []byte, publicBaseURL string, logger zerolog.Logger) (*LocalFSUploader, error) {
	if basePath == "" {
		return nil, errors.New("storage: local fs base path required")
	}
	if !filepath.IsAbs(basePath) {
		return nil, fmt.Errorf("storage: local fs base path must be absolute, got %q", basePath)
	}
	if len(signingKey) < 16 {
		return nil, fmt.Errorf("storage: signing key must be ≥16 bytes, got %d", len(signingKey))
	}
	if publicBaseURL == "" {
		return nil, errors.New("storage: public base url required")
	}
	if err := os.MkdirAll(basePath, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create base path: %w", err)
	}
	return &LocalFSUploader{
		BasePath:      basePath,
		SigningKey:    signingKey,
		PublicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		logger:        logger.With().Str("storage", "local_fs").Logger(),
	}, nil
}

// Upload writes data to {BasePath}/{key}. The bucket parameter is ignored.
// Concurrent writes to the same key are last-write-wins.
func (u *LocalFSUploader) Upload(_ context.Context, _ /*bucket*/ string, key string, data []byte) error {
	full, err := u.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("storage: mkdir parent: %w", err)
	}
	if err := os.WriteFile(full, data, 0o640); err != nil {
		return fmt.Errorf("storage: write %q: %w", key, err)
	}
	return nil
}

// PresignGet returns a download URL valid for `ttl`. The HMAC covers the
// key and the expiry timestamp; the download handler re-derives the same
// signature with the same signing key to verify the request.
func (u *LocalFSUploader) PresignGet(_ context.Context, _ /*bucket*/ string, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("storage: empty key")
	}
	expires := time.Now().Add(ttl)
	sig := SignKey(key, expires, u.SigningKey)
	encoded := EncodeKey(key)
	q := url.Values{}
	q.Set("expires", fmt.Sprintf("%d", expires.Unix()))
	q.Set("sig", sig)
	return fmt.Sprintf("%s%s%s?%s", u.PublicBaseURL, downloadRoutePath, encoded, q.Encode()), nil
}

// Open opens a file for reading. Returned by the download handler so the
// HTTP layer can stream via io.Copy. Caller is responsible for Close.
//
// `key` is the same value passed to Upload. Path traversal attempts are
// rejected: the resolved absolute path must be a child of BasePath.
func (u *LocalFSUploader) Open(key string) (*os.File, error) {
	full, err := u.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: open %q: %w", key, err)
	}
	return f, nil
}

// resolve joins BasePath with key and validates the result stays inside
// BasePath. Defends against `..` traversal and absolute keys.
func (u *LocalFSUploader) resolve(key string) (string, error) {
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return "", ErrInvalidKey
	}
	full := filepath.Join(u.BasePath, key)
	clean := filepath.Clean(full)
	rel, err := filepath.Rel(u.BasePath, clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrInvalidKey
	}
	return clean, nil
}
