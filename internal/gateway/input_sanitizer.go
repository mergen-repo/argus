package gateway

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

var (
	scriptTagRe = regexp.MustCompile(`(?i)<\s*script[^>]*>[\s\S]*?<\s*/\s*script\s*>`)
	htmlTagRe   = regexp.MustCompile(`<[^>]+>`)
	onEventRe   = regexp.MustCompile(`(?i)\bon\w+\s*=\s*["'][^"']*["']`)
	jsProtoRe   = regexp.MustCompile(`(?i)javascript\s*:`)
)

func InputSanitizer(logger zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.ContentLength != 0 && isJSONContent(r) {
				bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
				r.Body.Close()
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}

				original := string(bodyBytes)
				sanitized := sanitizeString(original)
				if original != sanitized {
					logger.Warn().
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Str("remote_addr", r.RemoteAddr).
						Msg("input sanitized: potentially dangerous content removed")
				}

				r.Body = io.NopCloser(bytes.NewReader([]byte(sanitized)))
				r.ContentLength = int64(len(sanitized))
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isJSONContent(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	return strings.Contains(ct, "application/json") || ct == ""
}

func sanitizeString(s string) string {
	s = scriptTagRe.ReplaceAllString(s, "")
	s = onEventRe.ReplaceAllString(s, "")
	s = jsProtoRe.ReplaceAllString(s, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	return s
}

func SanitizeValue(s string) string {
	return sanitizeString(s)
}
