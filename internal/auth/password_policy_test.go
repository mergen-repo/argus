package auth

import (
	"testing"
)

func TestValidatePasswordPolicy(t *testing.T) {
	defaultPolicy := PasswordPolicy{
		MinLength:     12,
		RequireUpper:  true,
		RequireLower:  true,
		RequireDigit:  true,
		RequireSymbol: true,
		MaxRepeating:  3,
	}

	tests := []struct {
		name     string
		password string
		policy   PasswordPolicy
		wantErr  error
	}{
		{
			name:     "too short",
			password: "short1A!",
			policy:   defaultPolicy,
			wantErr:  ErrPasswordTooShort,
		},
		{
			name:     "valid all classes",
			password: "ValidLongPass1!",
			policy:   defaultPolicy,
			wantErr:  nil,
		},
		{
			name:     "repeating chars exceed max",
			password: "aaaaLongPass1!",
			policy:   defaultPolicy,
			wantErr:  ErrPasswordRepeatingChars,
		},
		{
			name:     "missing upper",
			password: "alllowercase12!",
			policy:   defaultPolicy,
			wantErr:  ErrPasswordMissingClass,
		},
		{
			name:     "missing lower",
			password: "ALLUPPER12345!",
			policy:   defaultPolicy,
			wantErr:  ErrPasswordMissingClass,
		},
		{
			name:     "missing digit",
			password: "NoDigits!Abcdef",
			policy:   defaultPolicy,
			wantErr:  ErrPasswordMissingClass,
		},
		{
			name:     "missing symbol",
			password: "NoSymbols123abcDE",
			policy:   defaultPolicy,
			wantErr:  ErrPasswordMissingClass,
		},
		{
			name:     "unicode password valid",
			password: "Pásswörd1!",
			policy: PasswordPolicy{
				MinLength:     8,
				RequireUpper:  true,
				RequireLower:  true,
				RequireDigit:  true,
				RequireSymbol: true,
				MaxRepeating:  3,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordPolicy(tt.password, tt.policy)
			if err != tt.wantErr {
				t.Errorf("ValidatePasswordPolicy(%q) = %v, want %v", tt.password, err, tt.wantErr)
			}
		})
	}
}

func TestGenerateRandomPolicyCompliant(t *testing.T) {
	policy := PasswordPolicy{
		MinLength:     12,
		RequireUpper:  true,
		RequireLower:  true,
		RequireDigit:  true,
		RequireSymbol: true,
		MaxRepeating:  0,
	}

	for i := 0; i < 10; i++ {
		pwd, err := GenerateRandomPolicyCompliant(policy)
		if err != nil {
			t.Fatalf("GenerateRandomPolicyCompliant returned error: %v", err)
		}
		if len(pwd) < policy.MinLength {
			t.Errorf("generated password too short: len=%d, want>=%d", len(pwd), policy.MinLength)
		}
		if err := ValidatePasswordPolicy(pwd, policy); err != nil {
			t.Errorf("generated password %q fails policy: %v", pwd, err)
		}
	}
}

func TestGenerateRandomPolicyCompliant_MinimalPolicy(t *testing.T) {
	policy := PasswordPolicy{
		MinLength: 8,
	}
	pwd, err := GenerateRandomPolicyCompliant(policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pwd) < 8 {
		t.Errorf("password too short: %d", len(pwd))
	}
}
