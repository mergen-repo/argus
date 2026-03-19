package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/btopcu/argus/internal/apierr"
	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("store: not found")
	ErrNoTenant  = errors.New("store: tenant_id not in context")
)

func TenantIDFromContext(ctx context.Context) (uuid.UUID, error) {
	v, ok := ctx.Value(apierr.TenantIDKey).(uuid.UUID)
	if !ok || v == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w", ErrNoTenant)
	}
	return v, nil
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "23505")
}
