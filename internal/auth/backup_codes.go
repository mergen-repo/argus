package auth

import (
	"crypto/rand"
	"strings"
)

const backupCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateBackupCodeFormat() (string, error) {
	buf := make([]byte, 8)
	b := make([]byte, 1)
	for i := range buf {
		for {
			if _, err := rand.Read(b); err != nil {
				return "", err
			}
			if int(b[0]) < len(backupCodeAlphabet)*(256/len(backupCodeAlphabet)) {
				buf[i] = backupCodeAlphabet[int(b[0])%len(backupCodeAlphabet)]
				break
			}
		}
	}
	return string(buf[:4]) + "-" + string(buf[4:]), nil
}

func NormalizeBackupCode(raw string) string {
	cleaned := strings.ReplaceAll(raw, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	upper := strings.ToUpper(cleaned)
	if len(upper) == 8 {
		return upper[:4] + "-" + upper[4:]
	}
	return strings.ToUpper(strings.TrimSpace(raw))
}
