package auth

import (
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func GenerateTOTPSecret(email string) (string, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Argus",
		AccountName: email,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}

func ValidateTOTPCodeWithWindow(secret, code string) bool {
	valid, _ := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return valid
}
