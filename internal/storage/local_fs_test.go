package storage

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newTestUploader(t *testing.T) (*LocalFSUploader, string) {
	t.Helper()
	dir := t.TempDir()
	u, err := NewLocalFSUploader(dir, bytes.Repeat([]byte("k"), 32), "http://test.local", zerolog.Nop())
	if err != nil {
		t.Fatalf("NewLocalFSUploader: %v", err)
	}
	return u, dir
}

func TestLocalFSUploader_Upload_Roundtrip(t *testing.T) {
	u, dir := newTestUploader(t)
	key := "tenants/abc/reports/job-1/report.pdf"
	body := []byte("PDF-1.7 content")
	if err := u.Upload(context.Background(), "", key, body); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("body mismatch")
	}
}

func TestLocalFSUploader_PresignGet_HMAC(t *testing.T) {
	u, _ := newTestUploader(t)
	key := "tenants/abc/reports/job-1/report.pdf"
	signed, err := u.PresignGet(context.Background(), "", key, time.Hour)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if !strings.HasPrefix(parsed.Path, "/api/v1/reports/download/") {
		t.Errorf("unexpected route: %s", parsed.Path)
	}
	if parsed.Query().Get("expires") == "" || parsed.Query().Get("sig") == "" {
		t.Errorf("missing query params: %s", parsed.RawQuery)
	}

	// Verify the signature roundtrip.
	expires, err := ParseExpiresQS(parsed.Query().Get("expires"))
	if err != nil {
		t.Fatalf("parse expires: %v", err)
	}
	if err := VerifyKey(key, expires, parsed.Query().Get("sig"), u.SigningKey); err != nil {
		t.Errorf("VerifyKey: %v", err)
	}
}

func TestLocalFSUploader_RejectsTraversal(t *testing.T) {
	u, _ := newTestUploader(t)
	cases := []string{"../../etc/passwd", "/etc/passwd", "tenants/../../etc/passwd"}
	for _, k := range cases {
		t.Run(k, func(t *testing.T) {
			err := u.Upload(context.Background(), "", k, []byte("x"))
			if !errors.Is(err, ErrInvalidKey) {
				t.Errorf("expected ErrInvalidKey, got %v", err)
			}
		})
	}
}

func TestLocalFSUploader_Open_NotFound(t *testing.T) {
	u, _ := newTestUploader(t)
	_, err := u.Open("does/not/exist.pdf")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSignKey_VerifyKey_Tampered(t *testing.T) {
	key := []byte("test-key-32-bytes-long-1234567890abcd")
	expires := time.Now().Add(time.Hour)
	sig := SignKey("k", expires, key)
	if err := VerifyKey("k", expires, sig, key); err != nil {
		t.Fatalf("good sig: %v", err)
	}
	if err := VerifyKey("k2", expires, sig, key); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered key: expected ErrInvalidSignature, got %v", err)
	}
	if err := VerifyKey("k", expires, "00"+sig[2:], key); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered sig: expected ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyKey_Expired(t *testing.T) {
	key := []byte("test-key-16-bytes")
	past := time.Now().Add(-time.Minute)
	sig := SignKey("k", past, key)
	if err := VerifyKey("k", past, sig, key); !errors.Is(err, ErrExpiredSignature) {
		t.Errorf("expected ErrExpiredSignature, got %v", err)
	}
}

func TestEncodeDecodeKey(t *testing.T) {
	cases := []string{"a", "tenants/abc/reports/job-1/r.pdf", "with spaces and ✓ unicode"}
	for _, k := range cases {
		enc := EncodeKey(k)
		dec, err := DecodeKey(enc)
		if err != nil {
			t.Errorf("%q: decode: %v", k, err)
			continue
		}
		if dec != k {
			t.Errorf("%q: roundtrip differ: %q", k, dec)
		}
	}
	if _, err := DecodeKey("!!!not-base64!!!"); err == nil {
		t.Errorf("bad input: expected error")
	}
}
