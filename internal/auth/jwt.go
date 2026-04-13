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
	Partial        bool       `json:"partial,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	Impersonated   bool       `json:"impersonated,omitempty"`
	ImpersonatedBy *uuid.UUID `json:"act_sub,omitempty"`
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
