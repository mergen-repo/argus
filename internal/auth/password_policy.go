package auth

import (
	"crypto/rand"
	"errors"
	"math/big"
	"unicode"
)

var (
	ErrPasswordTooShort       = errors.New("PASSWORD_TOO_SHORT")
	ErrPasswordMissingClass   = errors.New("PASSWORD_MISSING_CLASS")
	ErrPasswordRepeatingChars = errors.New("PASSWORD_REPEATING_CHARS")
)

type PasswordPolicy struct {
	MinLength     int
	RequireUpper  bool
	RequireLower  bool
	RequireDigit  bool
	RequireSymbol bool
	MaxRepeating  int
}

func ValidatePasswordPolicy(password string, p PasswordPolicy) error {
	runes := []rune(password)

	if len(runes) < p.MinLength {
		return ErrPasswordTooShort
	}

	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range runes {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}

	if p.RequireUpper && !hasUpper {
		return ErrPasswordMissingClass
	}
	if p.RequireLower && !hasLower {
		return ErrPasswordMissingClass
	}
	if p.RequireDigit && !hasDigit {
		return ErrPasswordMissingClass
	}
	if p.RequireSymbol && !hasSymbol {
		return ErrPasswordMissingClass
	}

	if p.MaxRepeating > 0 {
		run := 1
		for i := 1; i < len(runes); i++ {
			if runes[i] == runes[i-1] {
				run++
				if run > p.MaxRepeating {
					return ErrPasswordRepeatingChars
				}
			} else {
				run = 1
			}
		}
	}

	return nil
}

const (
	upperChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lowerChars  = "abcdefghjkmnpqrstuvwxyz"
	digitChars  = "23456789"
	symbolChars = "!@#$%^&*()-_=+[]{}|;:,.<>?"
)

func GenerateRandomPolicyCompliant(p PasswordPolicy) (string, error) {
	minLen := p.MinLength
	if minLen < 8 {
		minLen = 8
	}

	var required []byte

	randChar := func(charset string) (byte, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return 0, err
		}
		return charset[n.Int64()], nil
	}

	if p.RequireUpper {
		c, err := randChar(upperChars)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}
	if p.RequireLower {
		c, err := randChar(lowerChars)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}
	if p.RequireDigit {
		c, err := randChar(digitChars)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}
	if p.RequireSymbol {
		c, err := randChar(symbolChars)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}

	fullCharset := lowerChars + upperChars + digitChars + symbolChars
	remaining := minLen - len(required)
	for i := 0; i < remaining; i++ {
		c, err := randChar(fullCharset)
		if err != nil {
			return "", err
		}
		required = append(required, c)
	}

	for i := len(required) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := n.Int64()
		required[i], required[j] = required[j], required[i]
	}

	return string(required), nil
}
