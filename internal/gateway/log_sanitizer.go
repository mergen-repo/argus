package gateway

import (
	"strings"

	"github.com/rs/zerolog"
)

var sensitiveKeys = map[string]bool{
	"password":       true,
	"secret":         true,
	"token":          true,
	"api_key":        true,
	"apikey":         true,
	"api-key":        true,
	"authorization":  true,
	"cookie":         true,
	"x-api-key":      true,
	"refresh_token":  true,
	"access_token":   true,
	"jwt":            true,
	"totp_secret":    true,
	"encryption_key": true,
	"private_key":    true,
	"radius_secret":  true,
	"smtp_password":  true,
	"credentials":    true,
}

const maskedValue = "***"

type LogSanitizer struct{}

func NewLogSanitizer() LogSanitizer {
	return LogSanitizer{}
}

func (h LogSanitizer) Run(e *zerolog.Event, level zerolog.Level, msg string) {
}

func SanitizeLogField(key, value string) string {
	if isSensitiveKey(key) {
		return maskedValue
	}
	return value
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	if sensitiveKeys[lower] {
		return true
	}
	for k := range sensitiveKeys {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func SanitizedStr(e *zerolog.Event, key, value string) *zerolog.Event {
	if isSensitiveKey(key) {
		return e.Str(key, maskedValue)
	}
	return e.Str(key, value)
}

func NewSanitizedLogger(base zerolog.Logger) zerolog.Logger {
	return base.Hook(LogSanitizer{})
}

func MaskSecret(s string) string {
	if len(s) <= 4 {
		return maskedValue
	}
	return s[:2] + "***" + s[len(s)-2:]
}
