package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var JWTVerifyHook func(slot string)

var (
	ErrTokenExpired = errors.New("auth: token expired")
	ErrTokenInvalid = errors.New("auth: token invalid")
)

type Claims struct {
	UserID         uuid.UUID  `json:"sub"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	Role           string     `json:"role"`
	AuthType       string     `json:"auth_type,omitempty"`
	Scopes         []string   `json:"scopes,omitempty"`
	APIKeyID       *uuid.UUID `json:"api_key_id,omitempty"`
	Partial        bool       `json:"partial,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	Impersonated   bool       `json:"impersonated,omitempty"`
	ImpersonatedBy *uuid.UUID `json:"act_sub,omitempty"`
	// ActiveTenantID is a super_admin-only tenant context override. When set
	// and role == "super_admin", tenant-scoped middleware uses this instead
	// of TenantID. TenantID always remains the admin's home tenant.
	ActiveTenantID *uuid.UUID `json:"active_tenant,omitempty"`
	jwt.RegisteredClaims
}

func GenerateImpersonationToken(secret string, targetUserID, targetTenantID uuid.UUID, targetRole string, adminUserID uuid.UUID) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:         targetUserID,
		TenantID:       targetTenantID,
		Role:           targetRole,
		Impersonated:   true,
		ImpersonatedBy: &adminUserID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "argus",
			Subject:   targetUserID.String(),
			ID:        uuid.New().String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func GenerateToken(secret string, userID, tenantID uuid.UUID, role string, expiry time.Duration, partial bool) (string, error) {
	return GeneratePartialToken(secret, userID, tenantID, role, expiry, partial, "")
}

func GeneratePartialToken(secret string, userID, tenantID uuid.UUID, role string, expiry time.Duration, partial bool, reason string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		Partial:  partial,
		Reason:   reason,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "argus",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func GenerateOAuthToken(secret string, apiKeyID, tenantID uuid.UUID, scopes []string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   apiKeyID,
		TenantID: tenantID,
		Role:     "api_user",
		AuthType: "oauth2",
		Scopes:   scopes,
		APIKeyID: &apiKeyID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "argus",
			Subject:   apiKeyID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// GenerateSwitchedToken mints a token identical to a standard access token
// except that ActiveTenantID carries a super_admin-selected tenant context.
// Pass activeTenantID=nil to clear an existing switch (exit tenant context).
// homeTenantID must always be the user's original/home tenant.
func GenerateSwitchedToken(secret string, userID, homeTenantID uuid.UUID, activeTenantID *uuid.UUID, role string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:         userID,
		TenantID:       homeTenantID,
		Role:           role,
		ActiveTenantID: activeTenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "argus",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

func ValidateTokenMulti(tokenString string, secrets ...string) (*Claims, error) {
	var lastErr error
	for i, secret := range secrets {
		if secret == "" {
			continue
		}
		claims, err := ValidateToken(tokenString, secret)
		if err == nil {
			slot := "current"
			if i > 0 {
				slot = "previous"
			}
			if JWTVerifyHook != nil {
				JWTVerifyHook(slot)
			}
			return claims, nil
		}
		if errors.Is(err, ErrTokenExpired) {
			lastErr = err
		} else if lastErr == nil {
			lastErr = err
		}
	}
	if JWTVerifyHook != nil {
		JWTVerifyHook("failed")
	}
	if lastErr == nil {
		lastErr = ErrTokenInvalid
	}
	return nil, lastErr
}
