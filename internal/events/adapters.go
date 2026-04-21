package events

import (
	"context"
	"errors"

	"github.com/btopcu/argus/internal/store"
	"github.com/google/uuid"
)

// operatorNameAdapter wraps *store.OperatorStore to satisfy OperatorLookup.
type operatorNameAdapter struct {
	store *store.OperatorStore
}

// NewOperatorNameLookup returns an OperatorLookup backed by OperatorStore.
func NewOperatorNameLookup(s *store.OperatorStore) OperatorLookup {
	if s == nil {
		return nil
	}
	return &operatorNameAdapter{store: s}
}

func (a *operatorNameAdapter) GetName(ctx context.Context, id uuid.UUID) (string, error) {
	if a == nil || a.store == nil {
		return "", errors.New("events: operator store not configured")
	}
	op, err := a.store.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	if op == nil {
		return "", nil
	}
	return op.Name, nil
}

// apnNameAdapter wraps *store.APNStore to satisfy APNLookup.
type apnNameAdapter struct {
	store *store.APNStore
}

// NewAPNNameLookup returns an APNLookup backed by APNStore.
func NewAPNNameLookup(s *store.APNStore) APNLookup {
	if s == nil {
		return nil
	}
	return &apnNameAdapter{store: s}
}

func (a *apnNameAdapter) GetName(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	if a == nil || a.store == nil {
		return "", errors.New("events: apn store not configured")
	}
	apn, err := a.store.GetByID(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if apn == nil {
		return "", nil
	}
	return apn.Name, nil
}

// simNameAdapter wraps *store.SIMStore to satisfy SimNameLookup by scanning
// a cross-tenant row via a narrow SQL query — SIM.ICCID is not tenant-
// sensitive, so the resolver is permitted to look up by ID alone.
type simNameAdapter struct {
	store *store.SIMStore
}

// NewSimNameLookup returns a SimNameLookup backed by SIMStore.
func NewSimNameLookup(s *store.SIMStore) SimNameLookup {
	if s == nil {
		return nil
	}
	return &simNameAdapter{store: s}
}

// GetICCID returns the ICCID for the given SIM ID. Uses the store's
// GetICCIDByID cross-tenant helper.
func (a *simNameAdapter) GetICCID(ctx context.Context, id uuid.UUID) (string, error) {
	if a == nil || a.store == nil {
		return "", errors.New("events: sim store not configured")
	}
	return a.store.GetICCIDByID(ctx, id)
}
