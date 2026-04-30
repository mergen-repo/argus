// FIX-248 DEV-555: report-storage abstraction.
//
// The pre-FIX-248 codebase only had `S3Uploader`, hardcoded into the
// scheduled-report processor via the `nullReportStorage` wrapper in
// cmd/argus/main.go. In Docker dev environments without EC2 IMDS the
// upload always failed with the noisy "no EC2 IMDS role found" error.
//
// `Storage` is the minimal contract every backend implements; today
// `S3Uploader` and `LocalFSUploader` satisfy it. Cleanup is filesystem-side
// (LocalFS) or lifecycle-rule-side (S3) — neither requires a method here.
//
// `bucket` is retained as a parameter for S3 backwards compat; LocalFS
// ignores it.

package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Storage is the interface implemented by every report-storage backend.
type Storage interface {
	Upload(ctx context.Context, bucket, key string, data []byte) error
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
}

// SignKey produces a hex HMAC-SHA256 over `key|expires` using signingKey.
// Used by LocalFSUploader to mint signed download URLs and by the
// download handler to verify them.
func SignKey(key string, expires time.Time, signingKey []byte) string {
	h := hmac.New(sha256.New, signingKey)
	fmt.Fprintf(h, "%s|%d", key, expires.Unix())
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyKey returns nil if `sig` matches the HMAC for (key, expires).
// Constant-time comparison; error categories: ErrInvalidSignature,
// ErrExpiredSignature.
func VerifyKey(key string, expires time.Time, sig string, signingKey []byte) error {
	if time.Now().After(expires) {
		return ErrExpiredSignature
	}
	expected := SignKey(key, expires, signingKey)
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return fmt.Errorf("storage: decode expected: %w", err)
	}
	gotBytes, err := hex.DecodeString(sig)
	if err != nil {
		return ErrInvalidSignature
	}
	if !hmac.Equal(expectedBytes, gotBytes) {
		return ErrInvalidSignature
	}
	return nil
}

// EncodeKey wraps key in URL-safe base64 (no padding) so it survives a
// `{key_b64}` chi route param.
func EncodeKey(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

// DecodeKey reverses EncodeKey. Reject any decoded key that contains
// `..` segments or is absolute — those would let the download handler
// escape its base directory.
func DecodeKey(s string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("storage: decode key: %w", err)
	}
	key := string(raw)
	if strings.Contains(key, "..") {
		return "", ErrInvalidKey
	}
	if strings.HasPrefix(key, "/") {
		return "", ErrInvalidKey
	}
	return key, nil
}

// ParseExpiresQS parses the `?expires=` URL query value (unix seconds).
func ParseExpiresQS(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, ErrInvalidSignature
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, ErrInvalidSignature
	}
	return time.Unix(n, 0), nil
}

var (
	ErrInvalidSignature = errors.New("storage: invalid signature")
	ErrExpiredSignature = errors.New("storage: signature expired")
	ErrInvalidKey       = errors.New("storage: invalid key")
	ErrNotFound         = errors.New("storage: not found")
)
